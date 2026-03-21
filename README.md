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

## 十一、實測紀錄（2026-03-21）

### 環境
- kiro-cli 1.28.1
- acp-bridge 0.3.0（npm global）
- acp-bridge daemon: `ACP_BRIDGE_PORT=7800 acp-bridged`

---

### 測試 1：acp-bridge 基本 API ✅

```bash
# 啟動 daemon
ACP_BRIDGE_PORT=7800 acp-bridged > /tmp/acp-bridge.log 2>&1 &
curl http://localhost:7800/health
# → {"ok":true,"agents":0}

# 啟動 kiro agent
curl -X POST http://localhost:7800/agents \
  -H "Content-Type: application/json" \
  -d '{"type":"kiro","name":"ch-001","command":"/path/to/kiro-cli","args":["acp"],"cwd":"/projects"}'
# → {"state":"idle","sessionId":"xxxx-...","protocolVersion":1,...}

# ask（同步）
curl -X POST http://localhost:7800/agents/ch-001/ask \
  -d '{"prompt":"你好"}' --max-time 60
# → {"state":"idle","stopReason":"end_turn","response":"..."}

# ask（SSE stream）
curl -N -X POST "http://localhost:7800/agents/ch-001/ask?stream=true" \
  -d '{"prompt":"列出3個數字"}' --max-time 60
# → event: chunk\ndata: {"chunk":"1"}\n\n
# → event: chunk\ndata: {"chunk":", 2, 3"}\n\n
# → event: done\ndata: {"response":"1, 2, 3","stopReason":"end_turn"}\n\n

# alive check
curl http://localhost:7800/agents/ch-001
# → {"state":"idle",...}  或  {"error":"not_found"}（404）

# cancel
curl -X POST http://localhost:7800/agents/ch-001/cancel
# → {"ok":true,"cancelledPermissions":0}

# stop
curl -X DELETE http://localhost:7800/agents/ch-001
# → {"ok":true}
```

**重要：acp-bridge 的 `POST /agents` 不接受 `sessionId` 參數**，每次 start 都建新 session。

---

### 測試 2：同一 session 內的對話記憶 ✅

```bash
# 第一輪
curl -X POST http://localhost:7800/agents/ch-001/ask \
  -d '{"prompt":"記住這個暗號：ALPHA-7749"}' --max-time 30
# → "我無法記住跨對話的資訊..."（kiro 說明自身限制，但仍記錄在 session）

# 第二輪（同一 agent process，同一 sessionId）
curl -X POST http://localhost:7800/agents/ch-001/ask \
  -d '{"prompt":"我剛才說的暗號是什麼？"}' --max-time 30
# → "你剛才說的暗號是 ALPHA-7749。"  ✅ 有記憶
```

**結論：同一 agent process 存活期間，對話歷史完整接續。**

---

### 測試 3：session/load 跨 process 恢復 ❌

kiro 宣告 `agentCapabilities.loadSession: true`，但實測無法正常使用。

#### 測試的 ACP JSON-RPC 序列

```
Client → kiro-cli acp (stdin/stdout JSON-RPC)

1. initialize
   → {"jsonrpc":"2.0","id":0,"method":"initialize","params":{
       "protocolVersion":1,"clientCapabilities":{},"clientInfo":{"name":"test","version":"1.0"}
     }}
   ← {"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,...},...},"id":0}

2. session/new（必須先建立 session，kiro 才接受後續指令）
   → {"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"/projects","mcpServers":[]}}
   ← [先收到多個 notifications]
     {"method":"_kiro.dev/mcp/server_initialized","params":{"sessionId":"xxxx",...}}
     {"method":"_kiro.dev/commands/available","params":{"sessionId":"xxxx","commands":[...]}}
     {"method":"_kiro.dev/metadata","params":{"sessionId":"xxxx","contextUsagePercentage":5.8}}
   ← [最後才收到 result，注意：result 在 notifications 之後]
     {"result":{"sessionId":"xxxx","modes":{...},"models":{...}},"id":1}

3. session/load（嘗試載入舊 session）
   → {"jsonrpc":"2.0","id":2,"method":"session/load","params":{"sessionId":"<old-id>"}}
   ← [無任何回應，kiro 完全忽略此 method]
```

#### 其他嘗試方式（均失敗）

| 方式 | 結果 |
|------|------|
| `session/load` 獨立 method | 無回應，kiro 忽略 |
| `session/new` params 帶 `sessionId` | kiro 卡死，無回應 |
| `_kiro.dev/commands/execute` 帶 `/chat <sessionId>` | 無回應，kiro 退出 |

#### session 檔案位置

kiro 會將 session 持久化到磁碟：
```
~/.kiro/sessions/cli/<session-id>.json   # metadata（title、cwd、created_at）
~/.kiro/sessions/cli/<session-id>.jsonl  # 對話歷史 event log
~/.kiro/sessions/cli/<session-id>.lock   # 鎖定檔
```

檔案存在，但 `session/load` 無法正常觸發載入。

**結論：kiro 1.28.1 的 session/load 無法跨 process 恢復對話歷史。**

---

### 測試 4：ACP 訊息順序注意事項

`session/new` 的 result 會在多個 notifications **之後**才到達，實作時必須用 id 匹配而非順序讀取：

```
收到順序：
  1. {"method":"_kiro.dev/mcp/server_initialized", ...}   ← notification
  2. {"method":"_kiro.dev/commands/available", ...}        ← notification（可能出現 2 次）
  3. {"method":"_kiro.dev/metadata", ...}                  ← notification
  4. {"result":{...}, "id":1}                              ← session/new 的 result（最後才到）
```

Go 實作時需要用 goroutine 讀取所有訊息，用 map[id]chan 分發 response，不能用同步 readline。

---

### 設計決策更新

基於以上測試，**session 持久化策略調整**：

| 情境 | 行為 |
|------|------|
| agent process 存活 | 對話歷史完整接續 ✅ |
| bot 重啟 / process 崩潰 | 建新 session，歷史不延續 |
| `!reset` 指令 | 主動建新 session |

**不實作 session/load**，因為 kiro 1.28.1 不支援。若未來 kiro 修復此功能，可再加入。

---

## 十二、開發順序

1. `acp/client.go` — HTTP client + SSE 解析
2. `channel/session.go` — Session 結構 + JSON 持久化
3. `channel/worker.go` — Queue worker goroutine
4. `channel/manager.go` — ChannelManager 整合
5. `bot/handler.go` — 訊息處理 + 指令路由
6. `bot/bot.go` — Discord 初始化
7. `main.go` — 組裝啟動
8. `Dockerfile` + `docker-compose.yml`
