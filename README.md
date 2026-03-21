# kiro-discord-bot

**建立日期：** 2026-03-21  
**語言：** Go  
**目的：** 以 Discord channel 為單位，透過 acp-bridge 與 kiro-cli 進行 agent 對話，支援任務排隊、進度回饋。

---

## 一、系統架構

```
Discord User
    │ 發訊息
    ▼
Discord Bot (Go)
    ├── ChannelManager          每個 channel 一個 Session + Queue
    │     ├── Session           { agentName, sessionId, cwd }
    │     └── JobQueue          chan Job（buffered，FIFO）
    │           └── worker goroutine（每 channel 一個，依序執行）
    │
    └── AcpClient               HTTP client → acp-bridge daemon
          └── SSE stream parser
```

```
acp-bridge daemon (port 7800)
    └── kiro-cli acp (per channel agent process)
          └── AWS Bedrock / Anthropic
```

---

## 二、Channel Session 生命週期

```
第一次收到訊息（channel 無 session）
  → POST /agents { type:"kiro", name:"ch-{channelId}", cwd }
  → 儲存 { agentName, sessionId } 到 sessions.json

後續訊息
  → GET /agents/{agentName} 確認 alive
  → 若 404 → 重新 start → session/load {sessionId}（kiro 支援）

Bot 重啟
  → 讀取 sessions.json
  → 對每個 channel 重新 start agent + load session

!reset 指令
  → DELETE /agents/{agentName}
  → 清空 queue
  → 重新建立新 session
```

---

## 三、Job 狀態機

```
[收到訊息]
    │
    ▼
 QUEUED  ──→ 原訊息加 ⏳ reaction
    │
    │ queue worker 取出
    ▼
 RUNNING ──→ 原訊息 reaction 換成 🔄
    │         bot 發一則「🔄 處理中...」回覆訊息
    │         每 3 秒 edit 回覆訊息顯示串流內容
    │
    ├── 成功 → 原訊息 reaction 換成 ✅，edit 回覆為完整結果
    ├── 失敗 → 原訊息 reaction 換成 ❌，edit 回覆為錯誤說明
    └── 逾時 → 原訊息 reaction 換成 ⚠️，POST /agents/:name/cancel
```

---

## 四、Emoji 回壓對照

| 狀態 | 原訊息 reaction | bot 回覆內容 |
|------|----------------|-------------|
| 排隊中 | ⏳ | — |
| 執行中 | 🔄 | 「🔄 處理中...\n\n{目前串流}」（每 3 秒更新） |
| 完成 | ✅ | 完整回覆（超過 2000 字自動分段） |
| 失敗 | ❌ | 錯誤說明 |
| 逾時 | ⚠️ | 「任務超時（{N}s），已取消」 |

reaction 切換：先 remove 舊的，再 add 新的。

---

## 五、特殊指令

| 指令 | 行為 |
|------|------|
| `!reset` | 停止 agent、清空 queue、建新 session |
| `!status` | 顯示 queue 長度、agent 狀態、session ID |
| `!cancel` | 取消目前執行中任務（POST /agents/:name/cancel） |
| `!cwd <path>` | 設定此 channel 的工作目錄（下次 reset 後生效） |

---

## 六、持久化

```
/data/
├── sessions.json    channel session 對應表（bot 重啟後恢復）
└── config.json      各 channel 的 cwd 設定
```

### sessions.json 格式

```json
{
  "1234567890": {
    "agentName": "ch-1234567890",
    "sessionId": "sess_abc123",
    "cwd": "/projects/taipeidialysis-php"
  }
}
```

---

## 七、專案目錄結構

```
kiro-discord-bot/
├── main.go                 程式進入點
├── config.go               設定讀取（env + config.json）
├── bot/
│   ├── bot.go              Discord bot 初始化、事件監聽
│   ├── handler.go          訊息處理、指令路由
│   └── reaction.go         reaction 管理（add/remove/swap）
├── channel/
│   ├── manager.go          ChannelManager：session + queue 管理
│   ├── session.go          Session 結構、持久化
│   └── worker.go           per-channel queue worker goroutine
├── acp/
│   ├── client.go           acp-bridge HTTP client
│   └── sse.go              SSE stream 解析
├── Dockerfile
├── docker-compose.yml
├── .env.example
└── README.md
```

---

## 八、設定（環境變數）

| 變數 | 說明 | 預設值 |
|------|------|--------|
| `DISCORD_TOKEN` | Discord bot token | 必填 |
| `ACP_BRIDGE_URL` | acp-bridge daemon URL | `http://localhost:7800` |
| `KIRO_CLI_PATH` | kiro-cli 完整路徑 | `kiro-cli` |
| `DEFAULT_CWD` | 預設工作目錄 | `/projects` |
| `ASK_TIMEOUT_SEC` | 任務逾時秒數 | `300` |
| `QUEUE_BUFFER_SIZE` | 每 channel queue 最大長度 | `20` |
| `DATA_DIR` | sessions.json 存放目錄 | `/data` |
| `STREAM_UPDATE_SEC` | 串流更新間隔秒數 | `3` |

---

## 九、部署（Docker）

```yaml
# docker-compose.yml
services:
  acp-bridge:
    image: node:20-alpine
    working_dir: /app
    command: sh -c "npm install -g acp-bridge && acp-bridged"
    environment:
      - ACP_BRIDGE_PORT=7800
    ports:
      - "7800:7800"
    volumes:
      - ~/.kiro:/root/.kiro          # kiro sessions 持久化
      - ~/projects:/projects         # 工作目錄

  discord-bot:
    build: .
    environment:
      - DISCORD_TOKEN=${DISCORD_TOKEN}
      - ACP_BRIDGE_URL=http://acp-bridge:7800
      - KIRO_CLI_PATH=/usr/local/bin/kiro-cli
      - DEFAULT_CWD=/projects
    volumes:
      - ./data:/data                 # sessions.json
      - ~/.kiro:/root/.kiro          # kiro sessions
      - ~/projects:/projects
    depends_on:
      - acp-bridge
    restart: unless-stopped
```

---

## 十、關鍵實作細節

### Queue Worker（per channel）

```go
// 每個 channel 啟動一個 goroutine
func (w *Worker) run() {
    for job := range w.queue {
        w.execute(job)  // 同步執行，完成才取下一個
    }
}
```

### SSE 串流解析

```
GET /agents/:name/ask?stream=true
Content-Type: text/event-stream

event: chunk
data: {"chunk": "部分回覆..."}

event: done
data: {"response": "完整回覆", "stopReason": "end_turn"}

event: error
data: {"error": "...", "statusCode": 500}
```

用 `bufio.Scanner` 逐行讀，遇到 `event: done` 結束。

### Discord 訊息長度限制

Discord 單則訊息上限 2000 字。超過時自動分段發送，每段加上 `(1/N)` 標記。

### Agent 重連邏輯

```go
func (m *Manager) ensureAgent(channelID string) error {
    sess := m.sessions[channelID]
    _, err := m.acp.GetAgent(sess.AgentName)
    if err != nil {  // 404
        return m.acp.StartAgent(sess.AgentName, sess.Cwd, sess.SessionID)
        // StartAgent 內部：POST /agents，然後 session/load
    }
    return nil
}
```

---

## 十一、開發順序

1. `acp/client.go` — HTTP client + SSE 解析
2. `channel/session.go` — Session 結構 + JSON 持久化
3. `channel/worker.go` — Queue worker goroutine
4. `channel/manager.go` — ChannelManager 整合
5. `bot/handler.go` — 訊息處理 + 指令路由
6. `bot/bot.go` — Discord 初始化
7. `main.go` — 組裝啟動
8. `Dockerfile` + `docker-compose.yml`
