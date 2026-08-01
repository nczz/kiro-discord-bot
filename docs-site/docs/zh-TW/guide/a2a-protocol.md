# A2A 協議模型

本頁說明 `kiro-discord-bot` 如何實作 A2A-like 整合。它是 [使用 NATS 啟用 A2A](a2a-nats-setup.md) 的概念補充。Operator 可用本頁理解 `/doctor`、`/a2a peers`、`/a2a status` 與內建 `bot-tools` MCP tools 中出現的關鍵字。

本專案沒有公開 A2A HTTP server。它實作的是內部 custom NATS binding，但保留重要 A2A task concepts：agent cards、skills、tasks、status、artifacts、cancellation、input/auth replies、durable state 與 explicit authorization。

## 設計摘要

| 領域 | 決策 |
| --- | --- |
| Transport | NATS 加 JetStream。 |
| Public HTTP A2A | 不實作。 |
| Routing identity | Runtime agent ID，不是 Discord bot account。 |
| Discovery | Runtime AgentCards 透過 JetStream KV；heartbeats 透過 NATS liveness subjects。 |
| Correctness boundary | JetStream 加 SQLite task/policy stores。 |
| Delivery default | Executor-owned Discord transcript，加 safe proxy result visibility。 |
| Security default | `NATS_URL` 為空時 A2A 停用。Remote work 需要 policy，通常也需要 confirmation。 |

## 心智模型

```text
Discord bot process/account = transport host 與 runtime container
Discord bot + guild + channel/thread + A2A policy = runtime agent
NATS-visible AgentID = runtime_agent_id
```

一個 bot process 可以發布多個 runtime peers，每個 enabled/discoverable channel 或 thread policy 各一個。遠端 peer 應委派到 runtime peer，而不是整個 bot process。

## 關鍵字表

| 關鍵字 | 意義 |
| --- | --- |
| A2A | 本專案實作的選用 cross-bot delegation layer。 |
| NATS | bot processes 之間使用的外部 message transport。 |
| JetStream | NATS persistence layer，用於 tasks、controls、events、peer KV 與 object artifacts。 |
| `A2A_AGENT_ID` | 穩定的 bot/process base identity。用於 runtime ID namespace 與 credential ownership；runtime mode 中不是主要 routing identity。 |
| Runtime agent | 可 expose skills 並接收 delegated work 的 Discord channel 或 thread runtime。 |
| `runtime_agent_id` | 單一 runtime agent 的穩定 NATS-visible identity。用於 task/control/event/card/heartbeat subjects。 |
| `channel_ref` | Subject-safe public channel runtime alias。用於 display、migration metadata 與 skill context。 |
| AgentCard | Runtime peer 的公開 sanitized 描述，包含 supported binding、skills 與 display metadata。 |
| AgentSkill | Runtime expose 的能力，例如 `task` 或更細的 skill ID。 |
| Peer | 已發現的 runtime AgentCard 加上本地 trust/status metadata。 |
| Trust | 本地 policy，允許 peer runtime 進行 inbound、outbound 或 bidirectional task。 |
| Policy | Per-channel A2A 規則：enabled、discoverable、accepted senders、exposed skills、delegate targets、visibility、transcript mode、quotas 與 tool policy。 |
| `delegate_targets` | Outbound allowlist，由 `{runtime_agent_id, skill_id}` pairs 組成。 |
| `accept_from_runtimes` | Inbound allowlist，列出可送 task 進來的 remote runtime IDs。 |
| `co_present_from_runtimes` | 其他 delivery gates 也通過時，可分享 Discord context 與 co-present replies 的 runtimes。 |
| Confirmation token | Plan step 回傳並簽章的 token；套用 policy 或敏感 remote delegation 前需要。 |
| TaskStore | 本地 SQLite durable task/event state。`/a2a status` 與 `bot_a2a_task_status` 讀這裡。 |
| PeerStore | 本地 SQLite peer view，包含 known peers、trust display data、staleness 與 skills。 |
| Object store | JetStream Object Store bucket，用於較大的 A2A artifacts。 |
| `Nats-Msg-Id` | JetStream publish 上的穩定 idempotency header。用於 redelivery 下避免重複 effects。 |
| `proxy` | 安全 result mode，由 requester bot 轉送遠端結果。 |
| `transparent` | 更直接暴露遠端結果的 result mode，但仍受 policy 控制。 |
| `co_present` | Transcript mode；policy 與 Discord permission 通過時，兩個 bot 可在同一個 Discord channel 或 thread 發言。 |
| `legacy` | 使用 bot-level routing 的 migration mode，不是 production target。 |
| `dual` | 可同時 drain legacy 與 runtime subjects 的有限 migration mode。 |
| `runtime` | Production target mode。新 routing 使用 exact runtime IDs。 |

## Identity Model

### Bot Base Identity

`A2A_AGENT_ID` 代表 bot process 與 credential owner：

```env
A2A_AGENT_ID=adam-n200
```

它必須穩定且 subject-safe。它會用來產生 runtime IDs，並在 audit/doctor 中顯示 bot host identity。Runtime mode 中，它本身不是 user-facing peer route。

### Runtime Agent ID

Runtime ID 代表一個 channel 或 thread runtime：

```text
runtime_agent_id = <bot-prefix>-<public-channel-alias-slug>
```

如果 alias 不安全、太長、隱私風險、碰撞，或包含 raw Discord snowflake-like digits，實作會 fallback 到短 hash：

```text
runtime_agent_id = <bot-prefix>-rt-<short-hash>
```

範例：

```text
remote-bot-erp-support
remote-bot-backend
m5bot-main
m5bot-rt-4f8a9c01
```

Runtime IDs 必須在 restart 後保持穩定。不得包含 PID、boot timestamp、random suffix、raw Discord snowflake、private host path 或 secret material。

### Channel Reference

`channel_ref` 是 operator-readable、subject-safe 的 runtime channel alias。Runtime mode 中它不是主要 durable route，但會出現在 peer cards、policy display 與 skill context。

允許格式：

```text
[A-Za-z0-9_-]{1,64}
```

避免 dots、spaces、slashes、wildcards 與 private channel names，除非 manager 明確要把該 alias 公開。

## Runtime ID Modes

| Mode | 行為 | 用途 |
| --- | --- | --- |
| `legacy` | Bot-level identity 與 legacy fields 仍 active。 | Migration compatibility only。 |
| `dual` | 發布 runtime cards，同時可 drain 舊 legacy consumers。 | 短期 migration window。 |
| `runtime` | 新 routing 使用 exact runtime IDs。Legacy bot-level target asks 會被拒絕或要求 migration。 | Production target。 |

新部署應使用 `A2A_RUNTIME_ID_MODE=runtime`。

## NATS Subject Schema

所有 production task/control/event traffic 都使用 JetStream。Subjects 使用 `a2a.v1` prefix。

| Subject | Stream | 用途 |
| --- | --- | --- |
| `a2a.v1.task.<from_runtime>.<to_runtime>.<messageId>` | `A2A_TASKS` | Delegator 將 task 發給特定 runtime。 |
| `a2a.v1.control.<from_runtime>.<executor_runtime>.<taskId>.<kind>` | `A2A_CONTROLS` | Accepted 後的 cancel、input reply、auth reply 或其他 controls。 |
| `a2a.v1.event.<executor_runtime>.<delegator_runtime>.<taskKey>.<kind>` | `A2A_EVENTS` | Accepted、rejected、status、result 與 artifact events。 |
| `a2a.v1.card.<runtime_agent_id>` | KV or stream | Runtime AgentCard update。 |
| `a2a.v1.heartbeat.<runtime_agent_id>.<instance>` | Core or KV | Ephemeral liveness signal。 |

常見 event/control kinds：

```text
accepted
rejected
status
artifact
result
cancel
input_reply
auth_reply
```

不使用舊的 unversioned subjects，例如 `a2a.task.{agent-id}`、`a2a.status.{task-id}` 與 `a2a.announce`。

## JetStream Topology

實作使用這些 streams：

| Stream | Subjects | 用途 |
| --- | --- | --- |
| `A2A_TASKS` | `a2a.v1.task.>` | Durable task submissions。 |
| `A2A_CONTROLS` | `a2a.v1.control.>` | Accepted 後的 durable control messages。 |
| `A2A_EVENTS` | `a2a.v1.event.>` | Durable accepted/rejected/status/result/artifact events。 |
| `A2A_PEERS` KV | runtime peer keys | Peer cards 與 discovery metadata。 |
| `a2a-artifacts` object store | generated object keys | 較大的 task artifacts。 |

Consumers 是 runtime-targeted。一個 runtime 只 consume address 到自己 exact runtime ID 的 task/control/event subjects。

## Task Lifecycle

1. Delegator 驗證本地 outbound policy。
2. Delegator 用穩定 `Nats-Msg-Id` publish task message。
3. Executor 驗證 inbound subject、envelope、sender、policy、skill、quota 與 runtime context。
4. Executor 在 ack JetStream message 前，先 durable store admitted task。
5. Executor 在自己的 Discord channel 或 thread runtime 執行 task。
6. Executor publish accepted、status、artifact 與 result events。
7. Delegator 儲存收到的 events，並透過 `/a2a status` 或 `bot_a2a_task_status` 回報狀態。

系統是 at-least-once，不宣稱 exactly-once。NATS redeliver 時，durable idempotency 會避免重複 terminal effects。

## Task States

| State | Terminal | 意義 |
| --- | --- | --- |
| `TASK_STATE_SUBMITTED` | no | Delegator 已排入 task。 |
| `TASK_STATE_WORKING` | no | Executor 已接受 task 或送出進度。 |
| `TASK_STATE_INPUT_REQUIRED` | no | Executor 需要 requester input。 |
| `TASK_STATE_AUTH_REQUIRED` | no | Executor 需要 authorization decision。 |
| `TASK_STATE_COMPLETED` | yes | Task 成功完成。 |
| `TASK_STATE_FAILED` | yes | Runtime 或 execution failure。 |
| `TASK_STATE_CANCELED` | yes | Task 被取消。 |
| `TASK_STATE_REJECTED` | yes | Policy、auth、skill、quota 或 validation 拒絕 task。 |

`accepted` event 是 event 與 state transition，會讓 TaskStore 進入 `TASK_STATE_WORKING`；它不是獨立的 TaskStore state。Queued tool call 不代表 remote task 完成。回報完成前務必檢查 task state。

## Policy Model

A2A policy 由 channel runtime 擁有。重要 fields：

| Field | 意義 |
| --- | --- |
| `enabled` | 允許 runtime 參與 A2A。 |
| `discoverable` | 發布 runtime card 供 discovery。 |
| `runtime_agent_id` | 穩定 runtime route。Enabled/discoverable runtime policy 儲存前必須存在。 |
| `accept_from_runtimes` | 可送 inbound tasks 的 runtime IDs。 |
| `accept_skills` | Inbound 接受的 skills。 |
| `expose_skills` | Runtime card 中顯示的 local skills。 |
| `delegate_targets` | 這個 channel 可委派到的 runtime 與 skill pairs。 |
| `result_visibility` | Proxy 或 transparent result behavior。 |
| `discord_transcript_mode` | Delegator、mirror 或 co-present transcript behavior。 |
| `share_discord_context` | 只有 transcript mode 允許時才可分享 co-present context。 |
| `co_present_from_runtimes` | 允許 co-present transcript 的 runtimes。 |
| `co_present_target_channels` | 允許 same-guild co-present replies 的 target channels/threads。 |
| `remote_tool_policy_json.allow_memory_write` | 預設 false；只有此欄位可允許 remote jobs 使用 memory-write bot tools。 |

Legacy fields 如 `accept_from`、`delegate_to`、`delegate_skills` 只作 compatibility inputs。新 setup 寫入 canonical runtime fields。

## Delivery 與 Transcript Modes

| Mode | 責任 |
| --- | --- |
| Delegator/proxy | Requester bot 回報 remote status/result。安全跨 server 預設。 |
| Executor-owned | Executor bot 在自己的 channel/thread 執行真正 worker transcript。 |
| Transparent | Result visibility 較少中介，但仍受 policy 控制。 |
| Co-present | Policy 與 permissions 都允許時，executor 與 delegator 可共享 Discord channel/thread transcript。 |

`trusted=true` 不代表 transparent 或 co-present ready。若要 direct same-thread replies，對方 bot 的 inbound policy、co-present allowlist、target channel policy 與 Discord send permissions 都必須通過。

## Security Boundaries

A2A 不會繞過既有 bot 邊界：

- Remote tasks 不會得到額外 MCP tools，除非 executor channel policy 有 expose。
- Remote tasks 不會得到 memory-write 權限，除非明確設定 `allow_memory_write=true`。
- Discord delivery 仍使用 safe egress 與 AllowedMentions guards。
- Confirmation tokens 保護 policy changes 與敏感 remote delegation。
- `/doctor` 會遮蔽 tokens、credentials、TLS material、private paths 與敏感 environment values。
- Heartbeat 只代表 liveness，不是 authorization。

## Operator Surfaces

| Surface | 用途 |
| --- | --- |
| `/doctor` | 檢查 A2A enabled state、auth mode、runtime mode、peer status 與 readiness，且不暴露 secrets。 |
| `/a2a peers` | 列出可見 peer runtimes、skills、trust、staleness 與 delivery readiness。 |
| `/a2a trust` | 高階 trust setup，需 confirmation。 |
| `/a2a ask` | 對 trusted peer 送出 general task。 |
| `/a2a delegate` | 對指定 target runtime 與 skill 送出 task。 |
| `/a2a status` | 查看 durable local task state 與 events。 |
| `bot_a2a_*` tools | Agent-facing MCP surface，用於 policy、delegation、status 與 peer inspection。 |

一般操作不要檢查或編輯 raw `data/a2a/*.sqlite` files。

## 相關頁面

- [使用 NATS 啟用 A2A](a2a-nats-setup.md)
- [A2A NATS Rollout](../../guide/a2a-nats-rollout.md)
- [環境變數參考](environment.md)
- [Bot Tools MCP](bot-tools.md)
- [安全模型](security-model.md)
