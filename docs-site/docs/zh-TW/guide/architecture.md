# 架構

`kiro-discord-bot` 是 Discord gateway bot，負責管理 Kiro CLI ACP sessions、channel state、thread state、MCP policy、cron jobs、audit events 與 delivery behavior。

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

bot 會把使用者 Kiro MCP settings 視為 catalog，而不是直接 runtime inheritance。Agent sessions 使用 `DATA_DIR/kiro-agent-runtime` 底下的隔離 runtime home；runtime MCP config 保持空白，除非 bot 透過 ACP 注入 channel-approved servers。

這可以避免使用者全域 Kiro MCP 設定默默暴露給所有 Discord 頻道。

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
- 把 policy enforcement 放在 Kiro prompt behavior 之外。

Kiro `disabledTools` 不被視為安全邊界。

## Delivery and Redaction

一般 agent final answer 由 bot 負責送出。bot 處理 secret redaction、message splitting、file egress policy 與 Discord delivery errors。`bot_send_message` 不是 final answer 的預設路徑；它是明確通知或 handoff 用的受控額外 egress tool。

## Audit

Audit storage 記錄 command calls、command responses、agent job lifecycle、final response delivery 等 semantic bot events。Audit prompt investigations 使用短生命週期 private agents，且只注入 audit query tool。

這個架構上層的詳細行為與 trust boundaries 見 [Bot Tools MCP](bot-tools.md)、[Audit、用量與隱私](audit-usage-privacy.md)、[安全模型](security-model.md)。
