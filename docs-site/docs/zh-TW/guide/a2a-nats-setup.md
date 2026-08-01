# 使用 NATS 啟用 A2A

A2A 讓一個 `kiro-discord-bot` runtime 可以透過 NATS 與 JetStream 把工作委派給另一個 bot runtime。這是選用功能；當 `NATS_URL` 為空時，A2A 會完全停用，既有 Discord bot 行為不變。

想實際開啟功能時請從本頁開始。協議實作模型與關鍵字見 [A2A 協議模型](a2a-protocol.md)。Release gates、ACL hardening 與 rollback smoke checks 見英文 [A2A NATS Rollout](../../guide/a2a-nats-rollout.md)。完整環境變數見 [環境變數參考](environment.md)。

## A2A 能做什麼

A2A 提供 durable cross-bot task delegation：

- 一個 channel runtime 可以要求另一個 runtime 執行任務。
- task、status、result 與 artifact metadata 透過 NATS/JetStream subjects 傳遞；較大的 artifact bytes 使用 A2A object store。
- bot 仍會執行 Discord 權限、channel policy、MCP policy、safe egress、audit 與 confirmation gates。
- Peer discovery 與 trust 都是明確動作；看到 peer online 不代表可以委派。

A2A 不會取代一般 Discord 回覆。只有在 NATS 已設定且 channel policy 允許 peer 與 skill 時才會執行。

## 部署型態

| 型態 | 適用情境 | NATS 驗證 | 備註 |
| --- | --- | --- | --- |
| 本機開發 | 單機測兩個 bot | 無驗證或 dev token | 最快確認流程。 |
| 內部輕量部署 | 私有可信網路、低維運負擔 | TLS 加 `NATS_TOKEN` | 只能搭配 private/firewalled listener 與 `A2A_PRODUCTION_SECURITY=false`。 |
| 強化正式環境 | 較嚴格 production 或多 host 暴露 | `NATS_CREDS_FILE` NKey/JWT | `A2A_PRODUCTION_SECURITY=true` 時必須使用。 |
| HA 正式環境 | NATS 可用性是關鍵 | NKey/JWT 加 JetStream cluster | 選用；只有在可承擔維運成本時使用。 |

多數私有部署可以先從一個 private JetStream node 開始，搭配持久化儲存、TLS、token authentication、localhost-only monitoring 與 host firewall。風險模型需要時再升級到 NKey/JWT 或 cluster。

## 前置條件

啟用 A2A 前：

1. 每個 bot 都必須已經能作為一般 Discord bot 正常運作。
2. 每個 bot 必須有自己的 `DISCORD_TOKEN`。
3. 每個 bot 建議有自己的 `DATA_DIR`；不要在不同 bot identity 之間共用狀態。
4. 每個 bot 都需要穩定的 `A2A_AGENT_ID`。
5. NATS 必須啟用 JetStream。
6. bot process environment 必須注入 A2A 變數。bot 不會自行載入 `.env`。
7. 要使用 A2A 的 Discord channel 必須已用 `/cwd` 或 `/start` 初始化。

## 安裝 NATS Server 與 CLI

依照官方文件安裝 NATS server 與 CLI：

- NATS Server: https://docs.nats.io/running-a-nats-service/introduction/installation
- NATS CLI: https://docs.nats.io/using-nats/nats-tools/nats_cli

確認指令可用：

```bash
nats-server --version
nats --version
```

## 本機開發 NATS

使用 repository 內的開發設定啟動：

```bash
nats-server -c dev/nats.conf
```

確認 JetStream：

```bash
nats --server nats://127.0.0.1:4222 server check jetstream
nats --server nats://127.0.0.1:4222 stream ls
```

當 NATS credential 允許 JetStream setup 時，第一次 bot 啟動會建立 task、control、event streams 與 runtime consumers。Peer publish 會建立或更新 peer KV bucket。Object store 會在寫入 A2A artifact 時 lazy create。

## 正式環境 NATS Server

### 內部輕量 profile

只適合 private/internal deployments，且 operator 明確接受 single-node 與 shared-token tradeoff。

最低要求：

- 啟用 JetStream。
- JetStream 使用持久化儲存。
- listener 使用 TLS。
- 使用 token authentication。
- monitoring 綁定 `127.0.0.1`。
- 透過 firewall、VPN 或 private routing 限制只有可信 bot hosts 可連線。

NATS config skeleton 範例：

```conf
server_name: a2a-nats
port: 4222
http: 127.0.0.1:8222

jetstream {
  store_dir: "/var/lib/nats/jetstream"
}

authorization {
  token: "<set-a-random-token>"
}

tls {
  cert_file: "/etc/nats/certs/server.crt"
  key_file: "/etc/nats/certs/server.key"
  ca_file: "/etc/nats/certs/ca.pem"
}
```

這個 profile 的安全性低於 per-agent credentials。不要把 NATS listener 廣泛暴露。

### 強化正式環境 profile

強化正式環境請使用 NKey/JWT credentials：

```env
A2A_PRODUCTION_SECURITY=true
NATS_CREDS_FILE=/etc/kiro-discord-bot/nats/bot.creds
NATS_TOKEN=
NATS_TLS_CA_FILE=/etc/kiro-discord-bot/nats/ca.pem
```

`NATS_TLS_CA_FILE` 只負責驗證 NATS server certificate，不是 client authentication。當 `A2A_PRODUCTION_SECURITY=true` 時，`NATS_TOKEN` 不能作為唯一 production credential。

## 設定 Bot A

每個 bot process 使用一個穩定 base identity。Runtime mode 會針對每個 enabled/discoverable Discord channel 或 thread policy 發布 runtime peer。

Bot A 的內部輕量 `.env` 範例：

```env
DISCORD_TOKEN=<bot-a-discord-token>
DISCORD_GUILD_ID=<guild-id>
DEFAULT_CWD=/projects
DATA_DIR=/var/lib/kiro-discord-bot/bot-a

NATS_URL=tls://nats.example.internal:4222
NATS_TOKEN=<internal-token>
NATS_CREDS_FILE=
NATS_TLS_CA_FILE=/etc/kiro-discord-bot/nats/ca.pem

A2A_CONFIRMATION_SECRET=<random-secret>
A2A_AGENT_ID=adam-n200
A2A_RUNTIME_ID_MODE=runtime
A2A_AGENT_NAME=Adam
A2A_AGENT_DESCRIPTION=General project assistant. No secrets, paths, hosts, or user data.
A2A_PRODUCTION_SECURITY=false
A2A_REQUIRE_CONFIRMATION_FOR_REMOTE=true
A2A_AUTO_DELEGATE_ENABLED=false
A2A_MAX_DELEGATION_DEPTH=1
```

## 設定 Bot B

Bot B 必須使用不同的 Discord token、`DATA_DIR` 與 `A2A_AGENT_ID`。

```env
DISCORD_TOKEN=<bot-b-discord-token>
DISCORD_GUILD_ID=<guild-id-or-other-guild-id>
DEFAULT_CWD=/projects
DATA_DIR=/var/lib/kiro-discord-bot/bot-b

NATS_URL=tls://nats.example.internal:4222
NATS_TOKEN=<internal-token>
NATS_CREDS_FILE=
NATS_TLS_CA_FILE=/etc/kiro-discord-bot/nats/ca.pem

A2A_CONFIRMATION_SECRET=<random-secret>
A2A_AGENT_ID=eve-local
A2A_RUNTIME_ID_MODE=runtime
A2A_AGENT_NAME=Eve
A2A_AGENT_DESCRIPTION=Backend review assistant. No secrets, paths, hosts, or user data.
A2A_PRODUCTION_SECURITY=false
A2A_REQUIRE_CONFIRMATION_FOR_REMOTE=true
A2A_AUTO_DELEGATE_ENABLED=false
A2A_MAX_DELEGATION_DEPTH=1
```

不要把 raw credentials、private paths、hostnames、Discord IDs 或 internal topology 放進 `A2A_AGENT_DESCRIPTION`；peer cards 會被其他 A2A participants 發現。

## 啟動或重啟 Bot

Foreground smoke：

```bash
set -a
. ./.env
set +a
./kiro-discord-bot
```

Systemd 範例：

```bash
sudo systemctl restart kiro-discord-bot
sudo systemctl status kiro-discord-bot
```

預期 log markers 包含 NATS enabled、transport consumers started 與 bot running。疑難排解時不要印出完整 environment 或 secrets。

## 用 `/doctor` 驗證

在每個要使用 A2A 的 Discord channel 執行：

```text
/doctor
```

預期：

- A2A 顯示 enabled。
- Runtime mode 是 `runtime`。
- NATS auth mode 有設定。
- Transport consumers 已啟動。
- 不會印出 raw tokens、credential file contents 或 TLS material。
- Known A2A peers 會在 AgentCards 發布後出現；heartbeats 只會更新已知 peers 的 liveness。

若 `/doctor` 顯示 A2A disabled，先檢查 service environment。`NATS_URL` 是開關。

## 在 Discord 啟用 Channel Policy

環境變數只是在 process 層啟用 A2A；每個 Discord channel 仍需要明確 policy。

先列出可見 peers：

```text
/a2a peers
```

信任一個 peer runtime 來執行 general tasks：

```text
/a2a trust peer_agent:<peer-runtime-agent-id>
```

預設 trust flow 是 bidirectional、general-task only，且會先產生 plan。確認後才會套用。

若要較安全的 proxy-only delivery：

```text
/a2a trust peer_agent:<peer-runtime-agent-id> mode:safe
```

若要直接同 channel 或同 thread 回覆，使用 co-present mode：

```text
/a2a trust peer_agent:<peer-runtime-agent-id> mode:co_present
```

Co-present 需要雙方 policy、Discord send permissions 與 delivery readiness 都成立。`trusted=true` 本身不夠。

進階 agent 也可以使用內建 `bot-tools` MCP tools：

- `bot_a2a_peers`
- `bot_a2a_policy_get`
- `bot_a2a_trust_peer`
- `bot_a2a_policy_plan`
- `bot_a2a_policy_apply`
- `bot_a2a_delegate`
- `bot_a2a_task_status`

不要直接編輯 `data/a2a/*.sqlite`。

## 發送測試任務

套用 trust 後，發送 delegated task：

```text
/a2a ask peer_agent:<peer-runtime-agent-id> message:"Please reply with a short A2A smoke-test confirmation."
```

或指定 skill：

```text
/a2a delegate target_agent:<peer-runtime-agent-id> skill_id:task message:"Review this channel setup and reply with OK." reason:"A2A smoke test"
```

查看狀態：

```text
/a2a status
```

成功送出只代表 task 已 durable queued，不代表遠端 bot 已接受或完成。請使用 `/a2a status` 或 `bot_a2a_task_status` 查看權威狀態。

常見 TaskStore 狀態：

| 狀態 | 意義 |
| --- | --- |
| `TASK_STATE_SUBMITTED` | 本地 bot 已排入 task。 |
| `TASK_STATE_WORKING` | 遠端 runtime 已接受 task 或送出進度。 |
| `TASK_STATE_COMPLETED` | 遠端 runtime 已成功完成。 |
| `TASK_STATE_FAILED` | Runtime 或 execution failure。 |
| `TASK_STATE_CANCELED` | Task 被取消。 |
| `TASK_STATE_REJECTED` | Policy、skill、quota、auth 或 runtime validation 拒絕 task。 |

`accepted` event 會作為 task progress 記錄，並把 durable task state 移到 `TASK_STATE_WORKING`；它不是獨立的 TaskStore state。

## Delivery Modes

| 模式 | 行為 |
| --- | --- |
| `proxy` 或 safe | requester bot 轉送遠端結果。這是跨 server 最安全的預設。 |
| `transparent` | 結果更直接暴露，但仍受 policy 控制。 |
| `co_present` | 兩個 bot 可以在同一個 Discord channel 或 thread 發言。需要雙方 policy 與 Discord 權限。 |

先使用 safe/proxy。只有在雙方 operator 都預期直接同 channel 協作時才切到 co-present。

## 疑難排解

| 症狀 | 可能原因 | 檢查 |
| --- | --- | --- |
| `/doctor` 顯示 A2A disabled | `NATS_URL` 為空或未注入 service | 檢查 process manager environment。 |
| Startup 因缺少 `A2A_AGENT_ID` 失敗 | 設了 `NATS_URL` 但沒有穩定 agent ID | 設定 `A2A_AGENT_ID`。 |
| Startup 拒絕 token-only production | `A2A_PRODUCTION_SECURITY=true` 但沒有 `NATS_CREDS_FILE` | 使用 NKey/JWT creds，或明確選擇 internal lightweight profile。 |
| 看不到 peer | peer card 未發布、ACL 問題、KV stale、runtime mode 錯誤 | 執行 `/doctor`、`/a2a peers`，並檢查 NATS logs。 |
| Delegation 被拒絕 | Policy 不允許 sender、skill 或 target | 執行 `/a2a peers` 與 `/a2a trust`，或用 bot-tools 檢查 policy。 |
| Co-present 不生效 | 缺少 `co_present_from_runtimes`、target channel allowlist 或 Discord send permission | 查看 `/doctor` 或 `/a2a peers` 的 delivery readiness。 |
| Events 延遲 | JetStream redelivery 或 remote runtime 延遲 | 查看 `/a2a status`；idempotency 應避免重複 terminal delivery。 |

## 回滾

停用 A2A 且不改變一般 Discord bot 行為：

1. 設定 `NATS_URL=""`。
2. 重啟或 drain bot。
3. 執行 `/doctor`。
4. 發送一般非 A2A Discord 訊息，確認 bot 正常回覆。
5. 除非 retention/postmortem 流程允許，保留 `DATA_DIR`。

Rollback 不需要刪除 A2A SQLite rows 或 JetStream state。
