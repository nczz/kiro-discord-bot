# kiro-discord-bot × NATS A2A-like Custom Binding 整合開發規格

> 版本: 2.1 | 日期: 2026-07-31 | 狀態: runtime-first 規劃修正版，待實作
> 目的: 將 A2A/NATS binding 從 bot-level discovery 修正為 **bot + Discord channel/thread runtime = NATS 可發現 agent** 的模型，保留可逐步遷移與可驗收工程規格。

## 0. 決策摘要

本專案要做的是 **A2A-inspired 的 NATS custom binding**，不是直接暴露官方 A2A HTTP/JSON-RPC/gRPC server。

原因：

1. `kiro-discord-bot` 的天然 agent 邊界是 Discord channel runtime，不是單一 HTTP agent endpoint。
2. 多台 bot 常位於不同內網；NATS 能提供 location transparency，不需要 bot 互相直連。
3. NATS/JetStream 適合 durable asynchronous task/event transport。
4. 官方 A2A v1.0 的 canonical data model 仍要保留，避免未來無法接外部 A2A client 或 adapter。

因此本規格採用：

- **內部 transport**: NATS + JetStream
- **對外語義**: A2A v1.0 canonical objects where applicable
- **定位**: `urn:kiro-discord-bot:a2a:nats:v1` custom binding
- **不實作**: 公開 HTTP A2A endpoint、SSE streaming、HTTP push notification server
- **保留**: 未來可加 read-only `.well-known/agent-card.json` 或 A2A gateway adapter

### 0.1 Runtime-first 修正

本規格的 routing/discovery 單位是 **runtime agent**，不是 Discord bot process。

```text
Discord bot process/account = transport host / runtime container
Discord bot + guild + channel/thread + A2A policy = runtime agent
NATS AgentID = runtime_agent_id
```

同一個 bot process 可發布多個 runtime cards，例如：

```text
d80-chunbot-erp-support
d80-chunbot-backend
m5bot-main
```

`A2A_AGENT_ID` 只作 bot/process base identity 與 runtime ID namespace。Task/control/event/card/heartbeat subjects 使用 runtime ID。`channel_ref` 是 runtime alias/display/migration metadata，不是主要路由 key。

## 1. 設計目標與非目標

### 1.1 目標

為 `kiro-discord-bot` 新增可選的 NATS A2A-like 協作層，使多個 bot instance 能：

1. 在 NATS mesh 中發布自己的 channel-level skills。
2. 發現其他 bot/channel runtime 暴露的 skills。
3. 將 task durable 委派給另一個 bot 的特定 channel runtime。
4. 由接收方在正確 channel runtime 內執行，沿用該 channel 的 CWD、MCP policy、agent profile、queue、安全邊界。
5. 將 status/result durable 回傳給委派方。
6. 由委派方負責 Discord user-facing delivery，除非接收方 channel 明確 opt in `transparent` delivery。
7. 對 task lifecycle、auth/input-required、cancel、retry、audit、idempotency 有明確可驗收行為。

### 1.2 非目標

本階段不做：

- 官方 A2A HTTP server。
- SSE streaming。
- HTTP push notification webhook。
- 取代現有 Discord `BOT_PEERS` peer discovery。
- 讓 Discord bot 直接寫入 Kiro 內建 knowledge base。
- 讓 remote task 直接繞過 channel.Manager / MCP policy / safe egress。
- shared-token production deployment。

### 1.3 核心原則

1. **Bot 不等於 Agent**  
   Discord bot account 是 process identity；真正的 agent runtime 是 channel/thread runtime。

2. **A2A policy 屬於 channel runtime**
   每個 channel 可獨立 enable、expose skill、accept remote sender、限制 concurrency。

3. **NATS identity 必須是 runtime 且穩定**
   durable subject / durable consumer 使用 runtime agent ID；restart 後不能變。

4. **JetStream 是 correctness boundary**  
   task/control/event/result 不走 Core NATS fire-and-forget。

5. **At-least-once + durable idempotency**  
   不宣稱 exactly-once。所有 side effects 必須可重放或可去重。

6. **Result proxy by default**  
   接收方預設不直接發 Discord；委派方做 user-facing delivery。

7. **Remote task 不獲得額外 Discord 權限**  
   A2A policy 只決定 remote task 是否可進入 channel runtime；MCP policy 仍決定 tool visibility/call permission。

## 2. 參考標準與官方限制

### 2.1 A2A v1.0 必須尊重的語義

官方 A2A v1.0 的 normative model 來自 proto，而非本文件自創 structs。

本實作需保留以下 canonical concepts：

- `AgentCard`
- `AgentSkill`
- `Message`
- `Part`
- `Task`
- `TaskStatus`
- `Artifact`
- `TaskStatusUpdateEvent`
- `TaskArtifactUpdateEvent`
- `SendMessage`
- `GetTask`
- `ListTasks`
- `CancelTask`

本實作不提供 HTTP/gRPC/JSON-RPC binding，但 NATS binding 必須文件化 operation mapping、state mapping、auth mapping、error mapping。

### 2.2 TaskState mapping

Wire-level compatibility layer 必須使用官方 enum 名稱；UI 可顯示小寫 aliases。

| Internal alias | Canonical A2A state | Terminal | 說明 |
|---|---|---:|---|
| `submitted` | `TASK_STATE_SUBMITTED` | no | 已接收但尚未執行 |
| `working` | `TASK_STATE_WORKING` | no | 執行中 |
| `input-required` | `TASK_STATE_INPUT_REQUIRED` | no | 需要使用者補資料 |
| `auth-required` | `TASK_STATE_AUTH_REQUIRED` | no | 需要授權、credential、destructive confirmation |
| `completed` | `TASK_STATE_COMPLETED` | yes | 成功完成 |
| `failed` | `TASK_STATE_FAILED` | yes | 執行失敗 |
| `canceled` | `TASK_STATE_CANCELED` | yes | 已取消 |
| `rejected` | `TASK_STATE_REJECTED` | yes | policy/capability/security 拒絕 |

### 2.3 Task ID ownership

A2A canonical `Task.id` 由接收方 agent 建立。委派方不得把自己產生的 UUID 當作 remote `Task.id`。

本規格使用：

- `messageId`: 委派方產生，作 publish idempotency 與 correlation。
- `clientTaskRef`: 委派方本地 tracking ID，可等於 Discord interaction/message correlation。
- `task.id`: 接收方 accepted 後產生，作 A2A task identity。

接收方拒絕前可只回 `rejected` event with `messageId/clientTaskRef`；接收方 accepted 後必須回 canonical `Task` with `task.id`。

## 3. 現有專案邊界

### 3.1 模組責任

```text
main.go / config.go
  - env parsing
  - optional A2A startup

bot/
  - Discord message/slash/component handling
  - user-facing delivery
  - peer display
  - safe egress drain
  - command permissions and audit

channel/
  - channel.Manager owns channel/thread runtime
  - Worker owns ACP execution queue
  - MCP policy injection
  - per-channel CWD/profile/session behavior

acp/
  - ACP JSON-RPC over stdio
  - no A2A awareness

audit/
  - SQLite audit events

internal/botmcp, internal/botegress
  - bot-tools MCP
  - safe egress pending actions
```

### 3.2 A2A 新增責任

```text
a2a/
  - NATS connection lifecycle
  - JetStream stream/consumer handles
  - envelope validation
  - canonical A2A payload adapter
  - peer card store
  - durable task store
  - policy evaluation helper
  - no Discord API calls
  - no direct ACP session calls
```

### 3.3 強制 ingress rule

Remote task 只能經過：

```go
channel.Manager.ExecuteA2ATask(ctx, req)
```

`a2a/` 不得直接：

- 建立 ACP session。
- 呼叫 `acp.StartAgent`。
- 呼叫 Discord API。
- 寫入 memory / knowledge base。
- 讀寫 remote 指定的 local filesystem path。

## 4. Identity model

### 4.1 Bot base identity

`A2A_AGENT_ID` 是 bot process/base identity。它用來：

- 產生 runtime ID namespace。
- 綁定 process-level credential owner。
- 在 audit/doctor 中顯示 bot host identity。

它 **不再是 task/control/event/card/heartbeat 的主要 routing identity**。

```text
A2A_AGENT_ID=d80-chunbot
```

格式仍為：

```text
[A-Za-z0-9_-]{1,64}
```

### 4.2 Runtime agent ID

每個 Discord bot + guild + channel/thread + A2A policy 組合都必須有穩定 runtime agent ID。Runtime ID 是 NATS-visible AgentID：

```text
runtime_agent_id = <bot_base_slug>-<channel_ref_slug>
```

例：

```text
d80-chunbot-erp-support
d80-chunbot-backend
m5bot-main
```

限制：

- 格式為 `[A-Za-z0-9_-]{1,64}`。
- 不含 PID、boot timestamp、random suffix。
- restart 後不變。
- 不直接包含 raw Discord guild/channel/thread snowflake。
- 不包含 private channel name，除非 manager 明確用該名稱作 public alias。
- 太長時使用 `<bot_base_slug>-rt-<short_hash(channel_ref)>`。
- 首次 enable/discoverable 時產生後 immutable；`channel_ref` 後續若改名，只更新 alias/display metadata，不改變既有 `runtime_agent_id`。如果部署者需要換 runtime ID，必須建立新 runtime、重新授權 trust/ACL，並 drain 舊 runtime。

### 4.3 Ephemeral instance ID

Heartbeat 使用 instance ID：

```text
instanceID = <runtime_agent_id>-<startUnixNano>-<random6>
```

只用於 observability，不用於 durable routing。

### 4.4 Channel reference

`channel_ref` 是 runtime alias、display、migration metadata。它不再是主要 routing key。

格式：

```text
[A-Za-z0-9_-]{1,64}
```

限制：

- 同一 bot base identity 內唯一。
- 不可直接使用 Discord channel ID/name，除非部署者接受它被其他 A2A peers 看見。
- 不可含 `.`, `*`, `>`, `/`, space。
- 必須可由 manager 在 Discord UX 中理解。

### 4.5 Runtime authority record

The local authority for a runtime is the channel A2A policy row. There is no separate runtime registry database in v1. A runtime authority record is derived from `channel_a2a_policy` fields:

```go
type RuntimeRecord struct {
    RuntimeAgentID string
    BotAgentID     string
    GuildID        string
    ChannelID      string
    ThreadID       string
    ChannelRef     string
    DisplayName    string
    RuntimeKind    string // channel|thread
    Enabled        bool
    Discoverable   bool
    CreatedAt      time.Time
    UpdatedAt      time.Time
}
```

`RuntimeRecord` is an in-memory DTO used to build cards, status, and validation context from the policy store. The persisted authority remains `channel_a2a_policy`; enabled or discoverable policy rows must have stable non-empty `runtime_agent_id`, `bot_agent_id`, and `channel_ref`.

Runtime policy data is Discord private state. Public peer cards may output only sanitized runtime label, `runtime_agent_id`, `bot_agent_id`, `channel_ref`, and capability summary.

Do not introduce a second persisted ownership table unless a later migration defines conflict resolution between policy and registry state. One authoritative policy row per guild/channel/thread prevents duplicate source-of-truth drift.

### 4.6 Skill slug

`skill_slug` 格式：

```text
[A-Za-z0-9_-]{1,64}
```

Canonical skill ID：

```text
<skill_slug>
```

Legacy `channel_ref/skill_slug` 可讀取與 migration，但 runtime-first routing 下 runtime ID 已經代表 channel scope，新寫入 policy 不再依賴 skill ID prefix 來分辨 channel。

## 5. NATS subject schema

所有 production task/control/event 使用 JetStream。

Prefix：

```text
a2a.v1
```

### 5.1 Subjects

| Subject | Stream | Publisher | Consumer | 用途 |
|---|---|---|---|---|
| `a2a.v1.task.<from_runtime>.<to_runtime>.<messageId>` | `A2A_TASKS` | source runtime | target runtime durable | 指名委派 |
| `a2a.v1.control.<from_runtime>.<executor_runtime>.<taskId>.<kind>` | `A2A_CONTROLS` | delegator runtime | executor runtime durable | cancel/input/auth/status controls after accepted |
| `a2a.v1.event.<executor_runtime>.<delegator_runtime>.<taskKey>.<kind>` | `A2A_EVENTS` | executor runtime | delegator runtime durable | accepted/rejected/status/result/artifact events |
| `a2a.v1.card.<runtime_agent_id>` | KV or stream | runtime self | peers | Runtime AgentCard update |
| `a2a.v1.heartbeat.<runtime_agent_id>.<instance>` | Core or KV | runtime self | peers/monitor | ephemeral runtime liveness |

`taskKey` is `taskId` after acceptance。For pre-accept `rejected` events, use `msg_<messageId>` where `messageId` is already subject-safe。The payload still carries canonical fields and `messageId` for correlation。

`kind` values：

```text
status
artifact
result
cancel
input_reply
auth_reply
accepted
rejected
```

### 5.2 不使用的舊 subjects

不得使用：

```text
a2a.task.{agent-id}
a2a.task.pool.{skill}
a2a.status.{task-id}
a2a.result.{task-id}
a2a.cancel.{task-id}
a2a.announce
```

理由：

- 無 version token。
- cancel 無 executor identity。
- per-task status/result subjects 鼓勵大量 one-off subscription。
- `pool` 容易被多 durable consumer 重複消費。
- `a2a.task.adam-n200-*` 類 substring wildcard 在 NATS 無效。

## 6. JetStream topology

### 6.1 Streams

Default retention is permanent, so the base stream commands omit `--max-age`。If `A2A_TASK_RETENTION_DAYS` or `A2A_OBJECT_RETENTION_DAYS` is set to a positive value, deployment tooling adds the matching `--max-age <days>d` and cleanup jobs purge local TaskStore/Object Store rows on the same schedule。

```bash
nats stream add A2A_TASKS \
  --subjects "a2a.v1.task.>" \
  --storage file \
  --retention limits \
  --max-msg-size 1048576 \
  --discard new \
  --dupe-window 24h

nats stream add A2A_CONTROLS \
  --subjects "a2a.v1.control.>" \
  --storage file \
  --retention limits \
  --max-msg-size 262144 \
  --discard new \
  --dupe-window 24h

nats stream add A2A_EVENTS \
  --subjects "a2a.v1.event.>" \
  --storage file \
  --retention limits \
  --max-msg-size 1048576 \
  --discard new \
  --dupe-window 72h
```

Production cluster：

- 3-node JetStream cluster recommended。
- stream replicas: 3。
- single-node only for local/dev。
- account resource limits must be configured before production。

### 6.2 Targeted task consumer

Per runtime durable consumer（runtime-first production mode）：

```bash
nats consumer add A2A_TASKS a2a_tasks_d80-chunbot-erp-support \
  --filter "a2a.v1.task.*.d80-chunbot-erp-support.>" \
  --deliver all \
  --ack explicit \
  --max-deliver 5 \
  --ack-wait 2m \
  --max-ack-pending 10
```

同一 bot process 可管理多個 runtime consumers。早期 `dual` rollout 可用 process-level wildcard consumer 加 app-level runtime ownership validation，但 runtime mode 的規格驗收以 per-runtime consumer/filter 為 canonical。

### 6.3 Control consumer

```bash
nats consumer add A2A_CONTROLS a2a_controls_d80-chunbot-erp-support \
  --filter "a2a.v1.control.*.d80-chunbot-erp-support.>" \
  --deliver all \
  --ack explicit \
  --max-deliver 10 \
  --ack-wait 30s
```

### 6.4 Event consumer

```bash
nats consumer add A2A_EVENTS a2a_events_d80-chunbot-erp-support \
  --filter "a2a.v1.event.*.d80-chunbot-erp-support.>" \
  --deliver all \
  --ack explicit \
  --max-deliver 10 \
  --ack-wait 30s
```

### 6.5 Pool dispatch

Pool dispatch is deferred from NATS binding v1。

First implementation MUST use targeted delegation to a known peer from the peer store。This keeps TaskStore ownership, subject authorization, acceptance bootstrap, and cancellation routing deterministic。

Future pool dispatch requires a separate design before implementation：

- explicit target model, such as `target_kind=pool` plus `pool_skill_slug` and no known executor until acceptance。
- first-valid-accept bootstrap that atomically binds executor by `(direction,message_id)` without requiring a preknown `to_agent`。
- per-skill/per-sender NATS consumer permissions。
- pool-specific Ack policy where local ineligibility or capacity misses do not terminally reject a task for the whole pool。
- tests proving exactly one executor admits a task under duplicate delivery and competing consumers。

### 6.6 Ack policy

Task message ack rule：

1. Parse NATS message。
2. Validate subject tokens。
3. Validate envelope/payload。
4. Validate authenticated principal matches subject `<from>` and expected target rules。
5. If validation/policy/capability failure is deterministic and delegator is known, durably record one terminal `rejected` inbound row keyed by `(direction,messageId)`, publish or queue one rejected event, then Ack/AckSync。
6. If storage or rejected-event publication cannot be durably recorded, do not ack; let JetStream redeliver。
7. If accepted, persist local task admission in durable TaskStore before Ack/AckSync。

Long-running execution does not hold the original task message unacked after durable admission. Execution progress/result is represented by durable local TaskStore + A2A_EVENTS.

## 7. Publish idempotency

Every JetStream publish MUST set stable `Nats-Msg-Id`.

| Message kind | `Nats-Msg-Id` |
|---|---|
| targeted task | `task:<from>:<to>:<messageId>` |
| control | `control:<from>:<executor>:<taskId>:<kind>:<revision>` |
| accepted/rejected event before task id | `event:<executor>:<delegator>:msg_<messageId>:<kind>` |
| status event | `event:<executor>:<delegator>:<taskId>:status:<revision>` |
| artifact event | `event:<executor>:<delegator>:<taskId>:artifact:<artifactId>:<revision>` |
| result event | `event:<executor>:<delegator>:<taskId>:result` |

Rules：

- `messageId` is immutable per attempted SendMessage。
- `revision` is monotonic per task in local TaskStore。
- redelivery checks TaskStore before executing。
- terminal tasks are immutable。
- repeated result publish returns previous result, not re-execute。

## 8. Envelope and canonical payloads

### 8.1 Envelope

Envelope carries transport metadata only。Payload carries canonical or adapter-defined body。

```go
type Envelope struct {
    Version       string          `json:"v"` // "1"
    Binding       string          `json:"binding"` // "urn:kiro-discord-bot:a2a:nats:v1"
    MessageID     string          `json:"messageId"`
    CorrelationID string          `json:"correlationId,omitempty"`
    From          string          `json:"from"`
    To            string          `json:"to,omitempty"`
    Type          string          `json:"type"`
    ContextID     string          `json:"contextId,omitempty"`
    TaskID        string          `json:"taskId,omitempty"`
    Revision      int64           `json:"revision,omitempty"`
    Timestamp     string          `json:"timestamp"` // RFC3339 UTC
    ExpiresAt     string          `json:"expiresAt,omitempty"`
    Payload       json.RawMessage `json:"payload"`
}
```

Required validation：

- `Version == "1"`
- `Binding == "urn:kiro-discord-bot:a2a:nats:v1"`
- `MessageID` non-empty and subject-safe when used in subject。
- `From` equals authenticated NATS principal mapping。
- `To` equals subject target where applicable。
- `Timestamp` parseable RFC3339 UTC。
- `ExpiresAt` if present must be future at execution time。
- `Type` known。
- `TaskID` when used in subject tokens must match `[A-Za-z0-9_-]{1,96}`; receiver-generated `Task.id` values must use this grammar, or a separate encoded subject token must be introduced before implementation。
- `Payload` schema-valid for `Type`。

### 8.2 Message types

```text
send_message
accepted
rejected
status_request
cancel_task
input_reply
auth_reply
task_status_update
task_artifact_update
task_result
agent_card
heartbeat
```

### 8.3 Canonical payload rule

Where official A2A v1.0 has an equivalent object, payload uses canonical ProtoJSON-compatible field names and enum values。

Examples：

- `send_message.payload.a2a` = `SendMessageRequest`-equivalent body。
- `task_status_update.payload` = `TaskStatusUpdateEvent`-equivalent body。
- `task_artifact_update.payload` = `TaskArtifactUpdateEvent`-equivalent body。
- `task_result.payload` = final canonical `Task` including status/artifacts。

Internal helper structs may exist, but must convert at boundary。

NATS binding payloads MAY wrap canonical A2A data with a versioned adapter object。Adapter fields must never be mixed into canonical A2A objects。

```go
type NatsSendMessagePayload struct {
    A2A      json.RawMessage       `json:"a2a"`
    Delivery A2ADeliveryOptions   `json:"delivery,omitempty"`
}

type A2ADeliveryOptions struct {
    ResultVisibility      string             `json:"resultVisibility,omitempty"`
    DiscordTranscriptMode string             `json:"discordTranscriptMode,omitempty"`
    DiscordContext        *A2ADiscordContext `json:"discordContext,omitempty"`
}
```

Parser requirements：

- validate `payload.a2a` as canonical `SendMessageRequest`。
- validate `payload.delivery` against channel policy before TaskStore persistence。
- persist accepted delivery options on the task row。
- pass parsed delivery options to `channel.Manager.ExecuteA2ATask`。
- drop or reject unknown adapter fields; do not silently pass them to prompts or Discord output。

### 8.4 Operation mapping

| A2A operation | NATS binding behavior | Durable subject | Response/event |
|---|---|---|---|
| `SendMessage` targeted | delegator runtime publishes `send_message` envelope to a specific target runtime | `a2a.v1.task.<from_runtime>.<to_runtime>.<messageId>` | executor runtime publishes `accepted` with `taskKey=<taskId>` or pre-accept `rejected` with `taskKey=msg_<messageId>`, then status/artifact/result events after acceptance |
| `SendMessage` pool | not implemented in NATS binding v1; caller must select a concrete runtime from the peer store | none | return `unsupported_operation` with `unknown_skill` or `channel_not_enabled` context when applicable |
| `SendStreamingMessage` | not implemented as an A2A streaming binding; use durable events instead | none | return `unsupported_operation` |
| `GetTask` | delegator reads local outbound TaskStore; if stale, it may request refresh through control message | `a2a.v1.control.<from_runtime>.<executor_runtime>.<taskId>.status` with `status_request` | executor replies by publishing latest `task_status_update` event |
| `ListTasks` | local-only query over TaskStore scoped to requesting Discord user/channel/runtime unless manager | none | local Discord response |
| `CancelTask` | delegator publishes cancel control after executor runtime is known | `a2a.v1.control.<from_runtime>.<executor_runtime>.<taskId>.cancel` with `cancel_task` | executor publishes `task_status_update` with `TASK_STATE_CANCELED`, or current/failed state plus `error_code=cancel_not_allowed` |
| `SubscribeToTask` | not implemented as a streaming subscription; delegator runtime maintains durable event consumer | `a2a.v1.event.<executor_runtime>.<delegator_runtime>.<taskId>.*` via consumer filter | status/artifact/result events are replayable from `A2A_EVENTS` |
| `CreateTaskPushNotificationConfig` | not implemented in NATS binding v1 | none | return `unsupported_operation` |
| `GetTaskPushNotificationConfig` | not implemented in NATS binding v1 | none | return `unsupported_operation` |
| `ListTaskPushNotificationConfig` | not implemented in NATS binding v1 | none | return `unsupported_operation` |
| `DeleteTaskPushNotificationConfig` | not implemented in NATS binding v1 | none | return `unsupported_operation` |
| `GetExtendedAgentCard` | authenticated peers may read the sanitized extended card from peer store | `a2a.v1.card.<runtime_agent_id>` or KV lookup | return extended AgentCard, or `unsupported_operation` if extended cards are disabled |

No operation may require a direct network connection from one bot to another bot. All cross-agent traffic goes through NATS subjects plus JetStream persistence.

## 9. AgentCard model

### 9.1 Public canonical AgentCard

Public peer card must not leak CWD、Discord channel ID、internal filesystem paths、secret-like MCP names、tokens、private URLs。

Fields：

```json
{
  "name": "d80-chunbot-erp-support",
  "description": "ERP support runtime",
  "version": "2.30.0",
  "supportedInterfaces": [
    {
      "url": "nats://nats.example.internal",
      "protocolBinding": "urn:kiro-discord-bot:a2a:nats:v1",
      "protocolVersion": "1.0"
    }
  ],
  "capabilities": {
    "streaming": false,
    "pushNotifications": false,
    "extendedAgentCard": true
  },
  "defaultInputModes": ["text/plain", "application/json"],
  "defaultOutputModes": ["text/plain", "application/json"],
  "skills": []
}
```

### 9.2 Skill representation

Canonical skill ID：

```text
<skill_slug>
```

Skill fields：

```json
{
  "id": "general/task",
  "name": "General task",
  "description": "General task execution inside this runtime.",
  "tags": ["code-review", "go", "backend"],
  "inputModes": ["text/plain", "text/x-go", "application/json"],
  "outputModes": ["text/plain", "application/json"],
  "examples": ["Review this Go handler for race conditions"]
}
```

Trigger guidance for local LLM prompt is not part of public canonical card. Store it in internal peer context or custom extension only after sanitization。

### 9.3 Extended card

Extended card is optional and only for authenticated peers。It MAY include：

- `runtime_agent_id`
- `bot_agent_id`
- `channel_ref`
- sanitized runtime display label
- runtime kind (`channel` or `thread`)
- trigger guidance
- result visibility support
- max task duration class

It MUST NOT include：

- CWD absolute path
- raw Discord channel ID
- secret names/values
- full MCP config
- internal network URLs unless required and authorized

## 10. A2A policy store

Do not store A2A policy in `bot/data/channel_metadata.json` as authority。

Use dedicated SQLite-backed store, modeled after MCP policy durability。

Package：

```text
channel/a2a_policy.go
```

Table：

```sql
CREATE TABLE IF NOT EXISTS channel_a2a_policy (
  guild_id TEXT NOT NULL,
  channel_id TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 0,
  discoverable INTEGER NOT NULL DEFAULT 0,
  runtime_agent_id TEXT,
  bot_agent_id TEXT,
  channel_ref TEXT,
  accept_from_runtimes_json TEXT NOT NULL DEFAULT '[]',
  accept_skills_json TEXT NOT NULL DEFAULT '[]',
  expose_skills_json TEXT NOT NULL DEFAULT '[]',
  delegate_targets_json TEXT NOT NULL DEFAULT '[]',
  delegate_media_json TEXT NOT NULL DEFAULT '{}',
  max_concurrent INTEGER NOT NULL DEFAULT 0,
  result_visibility TEXT NOT NULL DEFAULT 'proxy',
  discord_transcript_mode TEXT NOT NULL DEFAULT 'delegator',
  share_discord_context INTEGER NOT NULL DEFAULT 0,
  co_present_from_runtimes_json TEXT NOT NULL DEFAULT '[]',
  auto_delegate_enabled INTEGER NOT NULL DEFAULT 0,
  remote_tool_policy_json TEXT NOT NULL DEFAULT '{"allow_memory_write":false}',
  legacy_accept_from_json TEXT NOT NULL DEFAULT '[]',
  legacy_delegate_to_json TEXT NOT NULL DEFAULT '[]',
  legacy_delegate_skills_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  PRIMARY KEY (guild_id, channel_id),
  UNIQUE (runtime_agent_id)
);
```

Validation：

- `runtime_agent_id` is nullable for disabled pre-migration rows, but required, immutable, and globally unique before `enabled=1` or `discoverable=1` can be saved。
- `bot_agent_id` is nullable for disabled pre-migration rows, but must equal local `A2A_AGENT_ID` for enabled local runtime records。
- `channel_ref` is nullable for disabled pre-migration rows, but required when enabled and unique per `bot_agent_id`。
- `discoverable` must be explicitly enabled before publishing a runtime card。
- `result_visibility in ('proxy','transparent')`。
- `discord_transcript_mode in ('delegator','mirror','co_present')`。
- `share_discord_context` may be true only when `discord_transcript_mode='co_present'`。
- `co_present_from_runtimes` entries are runtime IDs or explicit wildcard `*`; default empty means no executor-side direct transcript posting。
- `max_concurrent` range: 0..64; `0` means unlimited。
- `accept_from_runtimes` entries are runtime IDs or explicit wildcard `*`。
- `accept_skills` entries are skill slugs, not arbitrary text。
- `delegate_targets` entries are `{runtime_agent_id, skill_id}` pairs chosen by manager。
- `remote_tool_policy_json.allow_memory_write` defaults false and is the only policy field that may enable remote A2A jobs to use memory-write bot tools。
- legacy `accept_from`/`delegate_to`/`delegate_skills` are read for compatibility only; new setup writes canonical runtime fields。
- `expose_skills[].id` subject-safe skill slug。
- `inputModes/outputModes` must be MIME types。

## 11. Configuration

### 11.1 Environment variables
| Variable | Default | Required | Description |
|---|---|---:|---|
| `NATS_URL` | empty | no | Empty disables A2A completely |
| `NATS_CREDS_FILE` | empty | prod yes | NATS NKey/JWT creds file |
| `NATS_TOKEN` | empty | dev only | Shared token; forbidden in production mode |
| `NATS_TLS_CA_FILE` | empty | prod yes if private CA | CA bundle for server verification |
| `A2A_AGENT_ID` | empty | if enabled yes | Bot/process base identity; not a runtime route by itself |
| `A2A_RUNTIME_ID_MODE` | `legacy` | no | `legacy`, `dual`, or `runtime`; production cutover target is `runtime` |
| `A2A_AGENT_NAME` | Discord bot username | no | Bot display name; runtime cards may override with sanitized runtime display label |
| `A2A_AGENT_DESCRIPTION` | empty | no | Public card description default |
| `A2A_TASK_TIMEOUT_SEC` | 300 | no | Per task execution timeout |
| `A2A_MAX_DELEGATION_DEPTH` | 3 | no | Loop prevention |
| `A2A_AUTO_DELEGATE_ENABLED` | false | no | Allow LLM-initiated delegation tool |
| `A2A_REQUIRE_CONFIRMATION_FOR_REMOTE` | true | no | Require confirmation for sensitive/remote delegation |
| `A2A_PRODUCTION_SECURITY` | false | no | If true, reject token-only auth |
| `A2A_TASK_RETENTION_DAYS` | 0 | no | Task/event retention in days; 0 means permanent until manual purge |
| `A2A_OBJECT_RETENTION_DAYS` | 0 | no | A2A object/artifact retention in days; 0 means permanent until manual purge |
| `A2A_MAX_PENDING_TASKS` | 0 | no | Per-runtime pending task cap; 0 means unlimited |
| `A2A_MAX_OUTBOUND_TASKS_PER_CHANNEL` | 0 | no | Per-channel outbound pending cap; 0 means unlimited |
| `A2A_MAX_INBOUND_TASKS_PER_CHANNEL` | 0 | no | Per-channel inbound pending cap; 0 means unlimited |
| `A2A_MAX_EVENT_RATE_PER_MIN` | 0 | no | Per-runtime A2A event rate cap; 0 means unlimited |

### 11.2 Enabled rule

A2A enabled iff：

```go
NATS_URL != "" && A2A_AGENT_ID != ""
```

If `NATS_URL != ""` but `A2A_AGENT_ID == ""`，startup fails with actionable error。If runtime mode is `dual` or `runtime`, enabled local runtime policies must resolve to valid runtime authority records before publishing cards or accepting tasks.

If `A2A_PRODUCTION_SECURITY=true` and only `NATS_TOKEN` is set，startup fails。

## 12. Security model

### 12.1 Authentication

Production supported：

- NKey/JWT creds file。
- mTLS mapped identity。

Dev-only：

- shared token。

### 12.2 Authorization

NATS subject permissions must enforce sender identity at subject level。

For runtime `d80-chunbot-erp-support`：

Publish allow：

```text
a2a.v1.task.d80-chunbot-erp-support.>
a2a.v1.control.d80-chunbot-erp-support.>
a2a.v1.event.d80-chunbot-erp-support.>
a2a.v1.card.d80-chunbot-erp-support
a2a.v1.heartbeat.d80-chunbot-erp-support.>
```

Subscribe allow：

```text
a2a.v1.task.*.d80-chunbot-erp-support.>
a2a.v1.control.*.d80-chunbot-erp-support.>
a2a.v1.event.*.d80-chunbot-erp-support.>
a2a.v1.card.>
a2a.v1.heartbeat.>
```

Response/inbox permissions must be narrow. Avoid blanket `_INBOX.>` in production unless account isolation is already enforced。

### 12.3 Envelope sender binding

On receive：

```text
authenticatedPrincipal -> allowed runtime IDs
subject token from/to -> expected runtime IDs
envelope.From -> must equal subject source runtime and an allowed runtime for this credential
envelope.To -> must equal subject target runtime where present
payload channelRef -> must match the local policy-derived runtime record for envelope.To when present
```

Reject if any mismatch。

### 12.4 Task ownership binding

Subject/envelope checks are necessary but not sufficient。Every control/event receive path must also bind the message to a TaskStore row before mutating state。

Control receive rule：

```text
local runtime == subject <executor_runtime>
subject <from_runtime> == stored inbound task.from_agent
subject <executor_runtime> == stored inbound task.executor_agent or local runtime_agent_id
subject <taskId> == stored inbound task.task_id
task is nonterminal unless the control is an idempotent status refresh
```

Event receive rule：

```text
local runtime == subject <delegator_runtime>
subject <executor_runtime> == stored outbound task.executor_agent, except accepted bootstrap may bind to stored to_agent first
subject <delegator_runtime> == stored outbound task.from_agent or local runtime_agent_id
subject <taskKey> == stored outbound task.task_id, except `msg_<message_id>` for pre-accept rejected events and accepted bootstrap below
event revision > stored revision unless it is an idempotent replay
```

Accepted bootstrap rule：

```text
if kind == accepted and stored outbound task.task_id is empty:
  bind by direction='outbound' and message_id from payload/envelope
  require subject <executor_runtime> == stored outbound task.to_agent or stored outbound task.executor_agent
  require subject <delegator_runtime> == local runtime_agent_id
  require payload Task.id == subject <taskKey>
  require payload Task.id matches subject-safe TaskID grammar
  atomically set stored task_id and executor_agent before applying the accepted state
```

After this bootstrap, all later status/artifact/result/control messages must bind by stored `task_id`。

Reject and audit any ownership mismatch before applying cancel/input/auth/status/result/artifact state。

### 12.5 Payload redaction

Audit and logs must not include full remote payload by default when `AUDIT_LOG_RECORD_CONTENT=false`。

Log metadata：

- task ID
- message ID
- from/to
- skill ID
- channel ref
- state
- error code
- payload size
- artifact count

Do not log：

- full prompt
- secrets
- attachment bytes
- raw file paths from remote agents

## 13. Durable TaskStore

### 13.1 Storage

Use SQLite under `DATA_DIR/a2a/tasks.sqlite`。

Tables：

```sql
CREATE TABLE IF NOT EXISTS a2a_tasks (
  local_id TEXT PRIMARY KEY,
  task_id TEXT,
  client_task_ref TEXT,
  message_id TEXT NOT NULL,
  context_id TEXT,
  direction TEXT NOT NULL,
  role TEXT NOT NULL,
  from_agent TEXT NOT NULL,
  to_agent TEXT NOT NULL,
  executor_agent TEXT,
  channel_id TEXT,
  guild_id TEXT,
  channel_ref TEXT,
  skill_id TEXT,
  state TEXT NOT NULL,
  terminal INTEGER NOT NULL DEFAULT 0,
  revision INTEGER NOT NULL DEFAULT 0,
  result_visibility TEXT NOT NULL DEFAULT 'proxy',
  discord_transcript_mode TEXT NOT NULL DEFAULT 'delegator',
  discord_context_json TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  expires_at TEXT,
  error_code TEXT,
  error_message TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_a2a_tasks_message_direction
ON a2a_tasks(direction, message_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_a2a_tasks_remote_task
ON a2a_tasks(direction, task_id)
WHERE task_id IS NOT NULL AND task_id <> '';

CREATE INDEX IF NOT EXISTS idx_a2a_tasks_client_ref
ON a2a_tasks(client_task_ref);

CREATE INDEX IF NOT EXISTS idx_a2a_tasks_context
ON a2a_tasks(context_id);

CREATE INDEX IF NOT EXISTS idx_a2a_tasks_state
ON a2a_tasks(state, terminal);
```

`direction` values：

```text
outbound
inbound
```

`role` values：

```text
delegator
executor
```

`local_id` is generated locally at first durable send/receive attempt。Outbound rows exist before the executor accepts and before remote `task_id` is known。Rejected-before-accepted responses are correlated by `(direction,message_id)` and `client_task_ref`, not by remote `task_id`。

### 13.2 Event table

```sql
CREATE TABLE IF NOT EXISTS a2a_task_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL,
  revision INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  state TEXT,
  payload_json TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(task_id, revision)
);
```

### 13.3 State transition rules

Allowed transitions：

```text
submitted -> working
submitted -> rejected
submitted -> canceled
working -> input-required
working -> auth-required
working -> completed
working -> failed
working -> canceled
input-required -> working
input-required -> canceled
auth-required -> working
auth-required -> canceled
```

Terminal states reject further mutation：

```text
completed
failed
canceled
rejected
```

Redelivery behavior：

- Existing inbound `message_id` with terminal task: re-publish previous terminal result/event; ack message。
- Existing inbound `message_id` with nonterminal task: ack duplicate; do not enqueue second execution。
- New `message_id`: validate and admit。

## 14. Channel runtime ingress

### 14.1 Manager API

```go
type A2ATaskRequest struct {
    TaskID           string
    ClientTaskRef    string
    MessageID        string
    ContextID        string
    FromRuntime      string
    ToRuntime        string
    BotAgentID       string
    ChannelRef       string // metadata; must match ToRuntime policy-derived record when present
    SkillID          string
    Parts            []A2APart
    ResultVisibility string
    TranscriptMode   string
    DiscordContext   *A2ADiscordContext
    Deadline         time.Time
}

type A2ATaskResult struct {
    TaskID    string
    State     string
    Artifacts []A2AArtifact
    Error     *A2AError
}

func (m *Manager) ExecuteA2ATask(ctx context.Context, req A2ATaskRequest) (A2ATaskResult, error)
```

### 14.2 Execution semantics

`ExecuteA2ATask` must：

1. Resolve `ToRuntime -> RuntimeRecord` from the channel A2A policy store.
2. Validate runtime policy enabled.
3. Validate subject/envelope/policy-derived runtime consistency.
4. Validate `AcceptFromRuntimes`。
5. Validate `accept_skills` and exposed/accepted skill mapping。
6. Reject or drop non-nil `DiscordContext` unless `TranscriptMode=co_present`, executor policy has `discord_transcript_mode='co_present'`, `co_present_from_runtimes` allows `FromRuntime`, the referenced guild/channel/thread resolves to the executor's own channel runtime, and Discord view/send permissions pass。
7. Enforce channel-level `max_concurrent` for inbound A2A tasks。
8. Build a normal `channel.Job` using existing Worker queue。
9. Set `DeliveryInline` / final reply capture。
10. Set `DisableBotEgress=true` for proxy mode。
11. Preserve channel CWD/MCP/profile/session behavior。
12. Return captured final response as text artifact。

### 14.3 Bot-tools policy inside A2A job

Proxy mode：

- bot-tools Discord egress must be disabled。
- memory write tools must be disabled for remote tasks unless explicitly allowed by channel policy。
- destructive MCP tools follow existing MCP policy; A2A does not grant extra rights。

`DiscordContext` is optional and only describes a Discord location already shared by both bots。It contains guild/channel/thread IDs, but never CWD, local paths, prompts, attachments, or secrets。The executor may use it only for standardized transcript notices after co-presence validation。

Transparent mode：

- allowed only if channel policy says `result_visibility=transparent`。
- requires Discord permission validation。
- every Discord send/file action must pass existing safe egress path and audit。

## 15. Delegation invocation model

### 15.1 Preferred: explicit MCP-like delegation tool

Do not rely on free-text `[delegate:...]` markers as the primary mechanism。

Expose an internal structured capability to the local agent when A2A is enabled：

```json
{
  "name": "a2a_delegate",
  "description": "Delegate a task to an approved remote A2A peer skill.",
  "inputSchema": {
    "type": "object",
    "required": ["targetAgent", "skillId", "message", "reason"],
    "properties": {
      "targetAgent": {"type": "string"},
      "skillId": {"type": "string"},
      "message": {"type": "string"},
      "reason": {"type": "string"},
      "requiresUserConfirmation": {"type": "boolean"}
    }
  }
}
```

The tool implementation validates：

- target exists in peer store。
- skill exists and input modes match。
- current channel policy `delegate_targets` allows the target runtime + skill pair。
- legacy `delegate_to`/`delegate_skills` may populate a migration preview, but cannot authorize a different runtime on the same bot。
- attachment/media use matches `delegate_media`。
- channel policy allows outbound delegation from this Discord channel。
- max delegation depth not exceeded。
- sensitive skill requires confirmation。
- message does not include disallowed attachments or raw secrets by policy。

### 15.2 Marker fallback

If marker fallback is kept, it is disabled by default and must follow：

- parse only from trusted assistant control block。
- never parse markers from user content。
- never parse markers from remote agent result。
- validate target/skill/depth/policy exactly like `a2a_delegate`。
- strip markers before Discord output。

Legacy free-text format is not part of the durable protocol。

## 16. Payload and artifacts

### 16.1 Inline payload limits

Inline text max：64 KiB per part。  
Inline binary is forbidden in production。

`TaskPart.Data base64` is not accepted as production design。

### 16.2 Artifact references

Large files/images/binary artifacts use JetStream Object Store or external approved object reference。

```json
{
  "artifactId": "artifact-uuid",
  "name": "review-report.txt",
  "parts": [
    {
      "kind": "object_ref",
      "bucket": "a2a-artifacts",
      "key": "tasks/<taskId>/review-report.txt",
      "digest": "sha256:...",
      "size": 12345,
      "mediaType": "text/plain",
      "expiresAt": "2026-07-30T00:00:00Z"
    }
  ]
}
```

Rules：

- receiver validates digest and size before use。
- object keys are generated locally, not supplied raw by remote。
- no remote local filesystem paths in results。
- object retention must match task/event retention。
- Object Store permissions scoped by agent/account。

## 17. Discovery and presence

### 17.1 Peer store

Use JetStream KV bucket when available：

```text
bucket: A2A_PEERS
key: <runtime_agent_id>
value: public runtime AgentCard + instance metadata + expiresAt
TTL: heartbeat interval * 3
```

Bot watches KV changes for peer card updates。

Fallback：

- Core NATS request-reply multi-response collector。
- collect replies until deadline。
- handle no responders and timeout explicitly。

### 17.2 Heartbeat

Heartbeat payload：

```json
{
  "agentId": "d80-chunbot-erp-support",
  "instanceId": "d80-chunbot-erp-support-1234-...",
  "status": "online",
  "activeTasks": 2,
  "startedAt": "2026-07-29T00:00:00Z",
  "version": "2.30.0"
}
```

Heartbeat is advisory only。Task routing must not depend only on heartbeat。

## 18. Discord UX

### 18.1 Natural-language first UX

A2A is too complex for users to operate primarily through slash commands。The primary UX is normal Discord natural language, with the channel agent driving server-side bot MCP tools。Slash commands remain fallback/bootstrap/admin shortcuts, not the main workflow。

First-run setup uses a **manager-only button wizard**。Natural language may start the wizard, but only a user with Discord ManageChannels may confirm policy changes。

Examples users should be able to type：

```text
幫我看看有哪些遠端 agent 可以協作。
把這個頻道開成 A2A backend-review，允許 eve-agent 委派 code-review 給我。
以後這個頻道可以委派 security-review 給 eve-agent，但附件只允許文字和 PNG。
請 eve-agent 幫我 review 上面這段錯誤 log。
這個任務取消。
如果 eve-agent 也在這個頻道，讓大家看得到兩個 bot 的協作進度。
```

The local agent translates these requests into structured bot-tools calls。The tool layer, not the LLM, enforces Discord permissions, channel scope, A2A policy, confirmation, and audit。

#### Natural-language implementation contract

The Discord happy path is conversational:

1. A user asks in the project channel/thread using ordinary language.
2. The local channel agent resolves peer/runtime names, skills, policy state, and task status through `bot_a2a_*` MCP tools.
3. The bot-tools service returns structured data plus user-facing summaries; the agent explains the outcome in Discord language.
4. Server-side tool code enforces guild/channel binding, requester identity, Discord permissions, A2A policy, confirmation, idempotency, and audit. The LLM may choose and explain tools, but cannot grant capability by wording.

User-facing copy must prefer localized product terms from Section 26 over protocol field names. Raw identifiers such as `runtime_agent_id`, `accept_from_runtimes`, `delegate_targets`, `result_visibility`, and `discord_transcript_mode` may appear in diagnostic/details blocks for managers, but not as the primary label in normal conversation or confirmation prompts.

Policy setup flow:

```text
natural-language request
→ bot_a2a_policy_plan
→ human-readable preview with risk/egress labels
→ signed Discord button/modal confirmation by a ManageChannels user
→ bot_a2a_policy_apply
→ localized audit/status summary
```

Delegation flow:

```text
natural-language request
→ bot_a2a_peers / bot_a2a_policy_get as needed
→ bot_a2a_delegate
→ confirmation challenge when remote egress, attachments, sensitive skills, transparent delivery, or co-present transcript sharing require it
→ proxy result delivery by default
```

Slash commands are not the primary UX. They remain explicit bootstrap, troubleshooting, and admin fallbacks that call the same bot-tools-backed service methods. They must not introduce a slash-only policy path or require users to learn NATS/A2A protocol fields before normal collaboration works.


### 18.2 A2A bot-tools contract

Add A2A tools to the built-in `bot-tools` MCP server。They follow existing bot-tools scope rules: every call is bound to current guild/channel/thread context, validates `requested_by_id`, and returns structured JSON for the agent to explain to the user。

| Tool | Type | Purpose |
|---|---|---|
| `bot_a2a_peers` | Read | list known peer agents and exposed skills, redacted for current channel policy |
| `bot_a2a_policy_get` | Read | show current channel A2A policy in user-friendly terms |
| `bot_a2a_task_status` | Read | show one task or recent channel A2A tasks |
| `bot_a2a_policy_plan` | Read/propose | convert a natural-language setup request into a policy diff and required confirmations |
| `bot_a2a_policy_apply` | Write, security-sensitive | apply a confirmed policy diff after server-side ManageChannels check |
| `bot_a2a_delegate` | Write, security-sensitive | publish a policy-gated A2A task or return a confirmation challenge |
| `bot_a2a_cancel` | Write, destructive | cancel a task after requester/manager check |
| `bot_a2a_input_reply` | Write, security-sensitive | send user-provided input for a task currently in `TASK_STATE_INPUT_REQUIRED` |
| `bot_a2a_auth_reply` | Write, security-sensitive | approve or deny a task currently in `TASK_STATE_AUTH_REQUIRED` after confirmation checks |

Tool safety rules：

1. Tools require explicit `guild_id`, `channel_id`, `requested_by`, and `requested_by_id` from Discord context; user-supplied IDs are rejected if they do not match bound context。
2. `bot_a2a_policy_apply` requires ManageChannels and a fresh confirmation token generated by `bot_a2a_policy_plan`。
3. `bot_a2a_delegate` requires outbound `delegate_targets` policy and may require confirmation for remote data egress, attachments, sensitive skills, or transparent/co-present delivery。
4. `bot_a2a_cancel` accepts requester or manager only。
5. `bot_a2a_input_reply` accepts requester or manager only, requires the task to be in `TASK_STATE_INPUT_REQUIRED`, redacts/logs metadata under the same egress policy, and publishes one idempotent `input_reply` control。
6. `bot_a2a_auth_reply` accepts requester or manager only, requires the task to be in `TASK_STATE_AUTH_REQUIRED`, never carries raw long-lived credentials, and publishes one idempotent `auth_reply` control with approve/deny plus scoped confirmation metadata。
7. Tool responses must include `requiresConfirmation`, `confirmationSummary`, `riskLabels`, and `expiresAt` when confirmation is needed。
8. Policy diffs are applied atomically and idempotently by server-generated `changeId`。
9. The agent may explain proposed changes, but cannot bypass confirmation or permission checks by wording。

### 18.3 Slash command fallback

Slash commands remain available for bootstrap, troubleshooting, and users who prefer explicit commands。They call the same internal service methods as bot-tools; no separate policy path is allowed。

| Command | Permission | Visibility | Effect |
|---|---|---|---|
| `/a2a peers` | normal user | ephemeral | list known peer skills |
| `/a2a status [task]` | requester or manager | ephemeral | show task state |
| `/a2a delegate` | normal user, policy-gated | public ack + thread updates | manual delegation |
| `/a2a cancel` | requester or manager | ephemeral/public status update | cancel task |
| `/a2a reply` | requester or manager | ephemeral/public status update | provide input for an input-required task |
| `/a2a authorize` | requester or manager | ephemeral/public status update | approve or deny an auth-required task |
| `/a2a enable` | ManageChannels | ephemeral | enable current channel |
| `/a2a disable` | ManageChannels | ephemeral | disable current channel |
| `/a2a ref` | ManageChannels | ephemeral | set channel_ref |
| `/a2a expose` | ManageChannels | ephemeral | add exposed skill |
| `/a2a unexpose` | ManageChannels | ephemeral | remove exposed skill |
| `/a2a accept-from` | ManageChannels | ephemeral | add inbound sender allow |
| `/a2a deny-from` | ManageChannels | ephemeral | remove inbound sender allow |
| `/a2a delegate-to` | ManageChannels | ephemeral | add outbound target/skill allow |
| `/a2a undelegate-to` | ManageChannels | ephemeral | remove outbound target/skill allow |
| `/a2a max-concurrent` | ManageChannels | ephemeral | set concurrency |
| `/a2a transcript-mode` | ManageChannels | ephemeral | set Discord transcript mode |
| `/a2a transcript-from` | ManageChannels | ephemeral | allow or deny co-present transcript sender |

### 18.4 Buttons

Button custom IDs must encode signed state：

```text
a2a:<action>:<taskId>:<requesterId>:<nonce>:<mac>
```

Validation：

- requester or manager only。
- task still nonterminal。
- nonce exists and unexpired。
- MAC valid。

Actions：

```text
cancel
refresh
confirm
deny
expand
retry
reply
authorize
```

### 18.5 Input/auth continuation flow

When an executor publishes `task_status_update` with `TASK_STATE_INPUT_REQUIRED` or `TASK_STATE_AUTH_REQUIRED`, the delegator MUST create or update the task status surface in the original channel/thread and include localized next-action controls。

Flow：

1. Persist the interrupted state, `revision`, `task_id`, requester, manager eligibility, and requested input/auth metadata in TaskStore。
2. Show a localized prompt that names the remote collaborator, task summary, requested action, risk labels, expiry, and whether the reply will be sent as text, attachment reference, Discord context, or authorization metadata。
3. Accept continuation only through `bot_a2a_input_reply`, `bot_a2a_auth_reply`, `/a2a reply`, `/a2a authorize`, or signed buttons/modal submission。
4. Validate requester or ManageChannels permission, task nonterminal state, expected interrupted state, nonce/confirmation freshness, and channel policy before publishing the control。
5. Publish exactly one idempotent control using `Nats-Msg-Id=control:<from>:<executor>:<taskId>:<kind>:<revision>`。
6. For auth-required work, send approve/deny plus scoped confirmation metadata only。Long-lived credentials stay out-of-band under the manual credential lifecycle decision。
7. For input-required work, run the same redaction, attachment/object-reference, audit, and safe-egress labeling used for initial delegation。
8. On timeout, denial, or permission failure, keep the remote task unresolved only when the executor expects a later reply; otherwise publish the matching denial control and show a localized status update。

Acceptance coverage MUST include one task that leaves `TASK_STATE_INPUT_REQUIRED` via `input_reply` and one task that leaves `TASK_STATE_AUTH_REQUIRED` via `auth_reply` or explicit denial。

### 18.6 Thread policy

- Short tasks may update the original response。
- Long tasks create or reuse a thread。
- Parallel tasks use one orchestration thread。
- Result content must respect Discord length/file limits and existing safe output paths。

### 18.7 Discord transcript modes

Users may want visible bot-to-bot collaboration when both bot accounts are present in the same Discord server, and especially when both can write in the same channel/thread。This is supported without weakening the default proxy delivery boundary。

`discord_transcript_mode` values：

| Mode | Who posts visible collaboration records | Works across different Discord servers | Executor may post directly | Intended use |
|---|---|---:|---:|---|
| `delegator` | delegator bot only | yes | no | default; safe minimal transcript |
| `mirror` | delegator bot mirrors executor A2A events with labels | yes | no | users see bot-to-bot timeline without giving executor Discord egress |
| `co_present` | delegator posts delegation; executor may post standardized status in the same shared Discord thread/channel | no | yes, status only unless `result_visibility=transparent` | same guild/channel visible collaboration |

Default is `delegator`。

Co-present mode requirements：

1. Delegator channel policy sets `discord_transcript_mode='co_present'` and `share_discord_context=true`。
2. Executor inbound channel policy sets `discord_transcript_mode='co_present'` and `co_present_from_runtimes` allows the delegator runtime。
3. Both bot accounts are members of the same guild and can resolve the same channel/thread。
4. Both bot accounts have Discord permission to view and send in the target location。
5. Delegator includes `DiscordContext` only after user action originates from that Discord location。
6. Executor validates `DiscordContext` before posting and rejects direct transcript if guild/channel/thread mismatch。
7. Executor direct posts are templated orchestration notices only: accepted, working, input-required, auth-required, completed/failed/canceled summary。
8. Final answer body is still delivered by delegator in `result_visibility=proxy`。Executor may post final content only when `result_visibility=transparent` and the same safe egress/audit/AllowedMentions controls pass。
9. Every direct or mirrored transcript post records audit event `a2a_transcript_posted` with actor bot, mode, task ID, channel ID, and message ID。
10. If any co-present check fails, fall back to `mirror` if allowed, otherwise `delegator`。

Co-present mode never grants remote tasks access to bot-tools Discord egress。It is a delivery-layer feature owned by `bot/` after A2A policy, Discord permission, and safe output checks。

## 19. Audit events

Add BotEvent types：

```text
a2a_peer_card_updated
a2a_policy_change_planned
a2a_policy_change_applied
a2a_policy_change_denied
a2a_task_send_requested
a2a_task_publish_failed
a2a_task_received
a2a_task_rejected
a2a_task_admitted
a2a_task_started
a2a_task_status_published
a2a_task_artifact_published
a2a_task_completed
a2a_task_failed
a2a_task_canceled
a2a_result_received
a2a_result_delivered
a2a_control_received
a2a_auth_required
a2a_input_required
a2a_transcript_posted
```

Required metadata：

```text
task_id
client_task_ref
message_id
context_id
from_agent
to_agent
executor_agent
channel_id
guild_id
channel_ref
skill_id
state
revision
result_visibility
discord_transcript_mode
discord_message_id
actor_agent_id
actor_discord_user_id
transcript_delivery_kind
source_event_revision
source_event_id
error_code
payload_size
artifact_count
```

Content retention follows `AUDIT_LOG_RECORD_CONTENT`。

## 20. Error model

Canonical error codes：

```text
invalid_envelope
invalid_subject
unsupported_version
unsupported_binding
unsupported_operation
unauthorized_sender
unauthorized_target
unknown_agent
unknown_skill
channel_not_enabled
sender_not_allowed
skill_not_allowed
overloaded
task_not_found
task_terminal
cancel_not_allowed
input_not_expected
auth_not_satisfied
payload_too_large
unsupported_media_type
artifact_fetch_failed
timeout
execution_failed
nats_publish_failed
store_error
```

Every remote failure uses an existing routed event type: pre-accept failures publish `rejected`; accepted-task failures publish `task_status_update` or `task_result` with canonical failed/canceled state and `error_code`。There is no standalone cross-agent `error` envelope in NATS binding v1。

### 20.1 Failure UX and i18n

All user-facing A2A failures are localized through the existing i18n architecture and delivered to the relevant channel/thread status surface。The response must identify enough context for the user to understand the failure without exposing secrets：

```text
a2a.error.nats_offline
a2a.error.peer_offline
a2a.error.task_timeout
a2a.error.executor_rejected
a2a.error.result_delayed
a2a.error.object_fetch_failed
a2a.error.permission_denied
a2a.error.version_mismatch
a2a.error.policy_confirmation_required
a2a.error.co_present_unavailable
```

Error copy should distinguish bot-local failures, peer-agent failures, setup/policy failures, and user-actionable confirmation failures。

## 21. Startup and shutdown

### 21.1 Startup

1. Load config。
2. If A2A disabled, continue no-op。
3. Validate `A2A_AGENT_ID` slug。
4. Validate security mode。
5. Open A2A policy store.
6. Resolve enabled local runtime records from `channel_a2a_policy`; in `dual`/`runtime` mode fail startup if an enabled policy cannot produce a valid owned runtime record.
7. Connect NATS with creds/TLS。
8. Create JetStream context。
9. Ensure global streams exist, or verify externally managed mode。
10. Ensure per-runtime task/control/event consumers for resolved local runtimes。
11. Open A2A TaskStore。
12. Build public AgentCards from enabled + discoverable policy-derived runtime records。
13. Start task/control/event consumers。
14. Start peer KV watch / discovery。
15. Publish runtime cards and runtime heartbeats。
16. Continue bot startup。

### 21.2 Shutdown

1. Stop accepting new inbound A2A tasks。
2. Publish draining/offline heartbeat。
3. Let in-flight tasks finish until graceful timeout。
4. Mark unfinished inbound tasks failed/canceled according to shutdown reason。
5. Flush pending result events。
6. Drain NATS subscriptions/connection。
7. Close stores。
8. Stop bot。

## 22. Testing plan

### 22.1 Unit tests

- runtime ID generation: subject-safe, stable, <=64 chars, no raw Discord snowflake。
- policy-derived runtime record validation and disabled/private runtime non-publication。
- subject parser for task/control/event with runtime IDs and explicit rejection of deferred pool subjects。
- envelope validation including `subject.to == envelope.To` and runtime/channel_ref consistency。
- A2A state mapping。
- AgentCard adapter emits one card per discoverable runtime and includes sanitized extended runtime metadata。
- policy store CRUD and validation for `AcceptFromRuntimes` and `DelegateTargets`。
- migration reads legacy bot-level policy without automatically trusting all runtimes on that bot。
- TaskStore state transitions with source/target runtime columns。
- idempotent redelivery handling。
- object reference validation。
- button custom ID signing/validation。
- audit metadata redaction and requester attribution source。

### 22.2 Integration tests

Use embedded `nats-server/v2/server` with JetStream。

Scenarios：

1. same bot publishes two discoverable runtime cards for two enabled channels。
2. disabled/private channel policy does not publish a peer card。
3. targeted runtime delegation admitted once under duplicate publish。
4. duplicate JetStream delivery does not execute ACP twice。
5. cancel routes to executor runtime。
6. result routes to delegator runtime after delegator reconnect。
7. pool delegation returns `unsupported_operation` without publishing a pool subject。
8. unauthorized sender runtime rejected。
9. runtime/channel_ref confused deputy rejected。
10. token-only production mode fails startup。
11. large attachment uses object reference。
12. A2A disabled leaves existing bot behavior unchanged。
13. input-required task resumes through `input_reply` after requester/manager validation。
14. auth-required task resumes or is denied through `auth_reply` without sending raw long-lived credentials。
15. same-bot cross-channel runtime setup only trusts the selected runtime。

### 22.3 Manual smoke test

Local：

```bash
nats-server -js -c ./dev/nats.conf
A2A_AGENT_ID=adam-local NATS_URL=nats://127.0.0.1:4222 ./kiro-discord-bot
A2A_AGENT_ID=eve-local NATS_URL=nats://127.0.0.1:4222 ./kiro-discord-bot
```

Discord natural language：

```text
幫我看看有哪些遠端 agent 可以協作。
把這個頻道開成 backend，暴露 code-review，允許 eve-local 委派給我。
允許這個頻道把 security-review 委派給 eve-local，附件只准文字和 PNG。
請 eve-local 用 security-review 看這段小 snippet。
顯示剛剛那個 A2A 任務的狀態。
取消剛剛那個 A2A 任務。
```

Slash fallback：

```text
/a2a peers
/a2a status <task>
```

Acceptance：

- peer appears。
- task state transitions visible。
- duplicate publish not duplicated。
- result delivered by delegator。
- audit events present。
- worktree behavior unchanged when `NATS_URL` empty。

## 23. Implementation milestones

### Phase 0: Spec hardening only

- This document reviewed and accepted。
- No production code changes。

### Phase 1: Foundation, no Discord UX

- Add `a2a/` package。
- Config parsing。
- subject/envelope/state validation。
- AgentCard adapter。
- TaskStore SQLite。
- Policy store SQLite。
- Embedded NATS tests。

### Phase 2: Node transport

- NATS connect/reconnect/drain。
- JetStream stream/consumer setup。
- publish with `Nats-Msg-Id`。
- consume task/control/event。
- peer KV/discovery。

### Phase 3: Channel ingress

- `channel.Manager.ExecuteA2ATask`。
- Worker inline execution capture。
- proxy-mode egress disabled。
- audit events。
- idempotent execution tests。

### Phase 4: Natural-language manual UX

- A2A bot-tools in `internal/botmcp`。
- natural-language peer/status/delegate/cancel/input-reply/auth-reply flows。
- confirmation buttons。
- slash fallback for peers/status/delegate/cancel/reply/authorize。
- no autonomous delegation yet。

### Phase 5: Natural-language admin policy UX

- `bot_a2a_policy_plan` and `bot_a2a_policy_apply`。
- natural-language setup for enable/ref/expose/accept/delegate/transcript policy。
- slash fallback for explicit admin commands。
- policy audit。
- docs update。

### Phase 6: Artifact references

- Object Store bucket setup。
- object ref validation。
- attachment flow。
- retention cleanup。

### Phase 7: Controlled autonomous delegation

- structured `a2a_delegate` tool。
- no free-text marker default。
- confirmation policy。
- loop/depth protection。

### Phase 8: Production hardening

- NKey/JWT or mTLS deployment docs。
- subject ACL templates。
- 3-node JetStream notes。
- advisories/dead-letter monitoring。
- release/deploy smoke matrix。

## 24. File change plan

| Operation | Path | Purpose |
|---|---|---|
| Add | `a2a/types.go` | envelope/canonical adapter types |
| Add | `a2a/subjects.go` | subject build/parse/validate |
| Add | `a2a/state.go` | state mapping/transition validation |
| Add | `a2a/node.go` | Node lifecycle interface |
| Add | `a2a/nats.go` | connection and JetStream integration |
| Add | `a2a/task_store.go` | SQLite durable task store |
| Add | `a2a/peer_store.go` | KV/discovery peer cards |
| Add | `a2a/card.go` | AgentCard build/sanitize |
| Add | `a2a/policy.go` | policy evaluation helpers |
| Add | `channel/a2a_policy.go` | channel A2A policy store |
| Modify | `config.go` | A2A env parsing |
| Modify | `main.go` | optional A2A startup/shutdown |
| Modify | `channel/manager.go` | A2A ingress API |
| Modify | `channel/worker.go` | inline result capture / egress disable wiring if missing |
| Modify | `bot/handler.go` | peer context and result delivery hooks |
| Modify | `internal/botmcp/server.go` | A2A bot-tools for natural-language policy, discovery, delegation, status, and cancel |
| Modify | `bot/commands.go` or equivalent | slash commands |
| Modify | `bot/interaction_policy.go` | `/a2a` permission/visibility |
| Modify | `audit/` | A2A bot event metadata support if needed |
| Modify | `docs-site/` | user/admin documentation after implementation |
| Modify | `go.mod` | `github.com/nats-io/nats.go` and test server dependency |

## 25. Acceptance criteria before implementation starts

Implementation may start only when this checklist is accepted：

- [ ] Document calls the protocol A2A-like custom NATS binding, not generic A2A compliance。
- [ ] AgentCard canonical/public vs internal/extended fields are separated。
- [ ] TaskState mapping uses official `TASK_STATE_*` at wire boundary。
- [ ] Task ID ownership is corrected。
- [ ] JetStream topology is subject-token-safe。
- [ ] Pool dispatch is explicitly deferred from v1, with targeted delegation required for executable implementation。
- [ ] Cancel routing contains executor identity。
- [ ] Status/result are durable。
- [ ] Idempotency uses `Nats-Msg-Id` + durable TaskStore。
- [ ] Stable runtime agent ID is separated from bot base identity and ephemeral instance ID。
- [ ] Per-runtime auth/authorization is required for production。
- [ ] A2A ingress goes through `channel.Manager`。
- [ ] Proxy result delivery disables remote Discord egress by default。
- [ ] Audit events are listed。
- [ ] Attachment/object reference design exists before manual attachment UX。
- [ ] Autonomous delegation is delayed until structured tool + confirmation policy。
- [ ] Primary Discord UX is natural language through bot MCP tools; slash commands are fallback only。
- [ ] A2A policy changes require server-side permission checks, confirmation tokens, idempotent change IDs, and audit。
- [ ] Same-guild/same-channel visible transcript has explicit `delegator`/`mirror`/`co_present` modes and preserves proxy safe-egress default。
- [ ] Input-required and auth-required continuation flows have user-facing bot-tools/slash/buttons, permission checks, idempotent controls, and acceptance coverage。

## 26. Product delivery decisions

The following product decisions are accepted for the first implementation：

1. **Onboarding default**: first-run A2A setup uses a manager-only button wizard。Natural language may initiate it; confirmation requires ManageChannels。
2. **Peer trust model**: admin review displays runtime display name, bot base identity, signature status, credential issuer, and credential/public-key fingerprint before adding a runtime to `accept_from_runtimes` or `delegate_targets`。
3. **Credential lifecycle**: credential issuance, rotation, and revocation are manual operations for v1。The bot documents current credential identity and fails closed when credentials are missing or revoked。
4. **Data egress labels**: every confirmation shows explicit labels for prompt text, attachments, Discord context sharing, transparent delivery, and co-present transcript posting。
5. **Retention policy**: default retention is permanent。Retention is configurable in days; `0` means keep until manual purge。
6. **Failure UX**: bot replies through i18n to the relevant channel/thread/status surface using contextual error messages。
7. **Quota/backpressure**: default quotas are unlimited。All quota knobs remain configurable; `0` means unlimited。
8. **Version compatibility**: incompatible or unknown peer `binding` / `protocolVersion` values are displayed, not hidden。Unsupported operations degrade with localized explanation。
9. **Admin review surface**: required。Managers must be able to inspect who may delegate to this channel, who this channel may delegate to, transcript mode, egress labels, and audit history。
10. **Rollout gates**: required before production enablement: local two-bot smoke, same-channel co-present smoke, cross-server proxy smoke, NATS restart smoke, credential revocation smoke。
11. **Naming UX**: expose localized user-facing terms, not raw protocol fields。
12. **Support boundary**: show contextual errors that explain whether the failure is local bot, peer bot, network/NATS, policy, permission, credential, or user-actionable confirmation。

Localized naming terms：

| Protocol term | zh-TW user-facing term | en user-facing term |
|---|---|---|
| runtime / peer runtime | 協作執行環境 | Collaborator runtime |
| bot_agent_id | Bot 身分 | Bot identity |
| skill | 能力 | Capability |
| channel_ref | 頻道協作名稱 | Channel collaboration name |
| accept_from_runtimes | 允許哪些協作執行環境請本頻道協作 | Which runtimes may ask this channel for help |
| delegate_targets | 本頻道可請哪些協作執行環境 | Which runtimes this channel may ask for help |
| skill_id | 可委派的能力 | Allowed remote capability |
| result_visibility=proxy | 由發起方播報結果 | Report result through requester bot |
| result_visibility=transparent | 由執行方直接回覆結果 | Let executor bot reply directly |
| transcript delegator | 發起方簡要播報 | Requester bot summary |
| transcript mirror | 發起方詳細播報 | Requester bot detailed timeline |
| transcript co_present | 同頻道雙 Bot 互動 | Same-channel bot interaction |

Discord interaction rule：

- Only `co_present` with both bot accounts in the same Discord channel/thread creates visible two-bot interaction where users see executor bot status replies。
- Same guild but different channel, different guild, or unresolved Discord context must use delegator-generated reporting, similar to tool-call progress messages。
- `mirror` is preferred when users want transparency but executor direct Discord posting is unavailable or not approved。

## 27. References

- A2A Protocol Specification: https://a2a-protocol.org/latest/specification/
- A2A v1.0.0 Specification: https://a2a-protocol.org/v1.0.0/specification/
- A2A normative proto: https://github.com/a2aproject/A2A/blob/main/specification/a2a.proto
- NATS Subjects: https://docs.nats.io/nats-concepts/subjects
- NATS Request-Reply: https://docs.nats.io/nats-concepts/core-nats/reqreply
- NATS Queue Groups: https://docs.nats.io/nats-concepts/core-nats/queue
- NATS JetStream: https://docs.nats.io/nats-concepts/jetstream
- NATS JetStream Consumers: https://docs.nats.io/nats-concepts/jetstream/consumers
- NATS Object Store: https://docs.nats.io/nats-concepts/jetstream/obj_store
- NATS Authentication: https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_intro
- NATS Authorization: https://docs.nats.io/running-a-nats-service/configuration/securing_nats/authorization
- NATS TLS: https://docs.nats.io/running-a-nats-service/configuration/securing_nats/tls
