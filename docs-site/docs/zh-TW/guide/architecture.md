# 架構

`kiro-discord-bot` 是 Discord gateway bot，負責管理 ACP agent sessions、channel state、thread state、MCP policy、cron jobs、audit events 與 delivery behavior。Kiro CLI 與 OMP 是同一套 manager、command、usage、audit layers 後面的可替換 agent engines。

## Runtime Components

```text
Discord Gateway
  -> command/message router
  -> Channel Manager
       -> channel agent
       -> thread agents
       -> temp agents for private audit/cron flows
  -> MCP Policy Store
       -> catalog discovery
       -> channel policy
       -> policy proxy
  -> bot-tools MCP
  -> audit recorder
  -> cron/reminder scheduler
```

## Agent Runtime Isolation

bot 會把使用者 Kiro MCP settings 視為 catalog，而不是直接 runtime inheritance。Kiro agent sessions 使用 `DATA_DIR/kiro-agent-runtime` 底下的隔離 runtime home；runtime MCP config 保持空白，除非 bot 透過 ACP 注入 channel-approved servers。

這可以避免使用者全域 Kiro MCP 設定默默暴露給所有 Discord 頻道。

OMP sessions 走同一套 ACP transport 與 MCP injection path，但不會收到 `KIRO_HOME` 或 `KIRO_MCP_CONFIG`。bot 會對 OMP child process 傳入 `--session-dir DATA_DIR/omp-agent-runtime/sessions`，讓 ACP session files 由 bot 管理，但不搬移既有 OMP auth/model database。當設定 `OMP_PROFILE` 時，auth/settings/cache state 也會用該 profile 隔離；留空則沿用 OMP default profile，避免破壞既有安裝。OMP 的 model 與 mode catalog 來自 ACP `session/new`，因此 OMP 的 model listing 需要 active agent session。

## Channel and Thread State

Parent channel 擁有：

- CWD binding。
- Persistent session metadata。
- Memory 與 flash memory blocks。
- MCP policy。
- Cron jobs。
- Default thread/listen settings。

Threads 可以用 parent channel context 與 bounded thread transcript 建立獨立 agents。Idle cleanup 可以停止 inactive thread agents，但 active work 不會被 capacity cleanup evict。

## MCP Policy Proxy

啟用的 MCP servers 會透過 bot policy proxy 啟動。Proxy 會：

- 過濾 `tools/list`，讓 agent 只看見 allowed tools。
- 拒絕未授權的 `tools/call`。
- 套用 channel allowlist。
- 把 policy enforcement 放在 agent prompt behavior 之外。

各 engine 自己的 disabled-tool 設定不被視為安全邊界。

## Delivery and Redaction

一般 agent final answer 由 bot 負責送出。bot 處理 secret redaction、message splitting、file egress policy 與 Discord delivery errors。`bot_send_message` 不是 final answer 的預設路徑；它是明確通知或 handoff 用的受控額外 egress tool。

ACP prompt result 可能包含 `stopReason`。正常 `end_turn` completion 不會額外提示；`max_tokens`、`refusal`、`cancelled` 這類 abnormal reasons 會以本地化 notice 附加在 final answer，並寫入 job audit metadata。除非 ACP 本身回傳 error，bot 不會把這類 turn 重新分類成 delivery failure。

Kiro subagent progress notification 會保守呈現。Bot 只信任已驗證的 top-level subagent 與 pending-stage counts；只有 notification 內含可辨識 name/status 時，才顯示 best-effort labels。

這個呈現屬於 Kiro-specific behavior。OMP tool 與 progress updates 會在可用時走共用 ACP update path。

## Audit

Audit storage 記錄 command calls、command responses、agent job lifecycle、final response delivery 等 semantic bot events。Audit prompt investigations 使用短生命週期 private agents，且只注入 audit query tool。

這個架構上層的詳細行為與 trust boundaries 見 [Agent Engines](agent-engines.md)、[Bot Tools MCP](bot-tools.md)、[Audit、用量與隱私](audit-usage-privacy.md)、[安全模型](security-model.md)。
