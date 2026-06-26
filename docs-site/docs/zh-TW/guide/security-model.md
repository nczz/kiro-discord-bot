# 安全模型

`kiro-discord-bot` 是 Discord、Kiro CLI、本機專案檔案與 MCP servers 之間的橋接。安全性取決於每一層是否被明確配置。

## 信任邊界

| 邊界 | 控制 |
| --- | --- |
| Discord access | Bot token、guild/channel permissions、privileged intents、slash command permissions。 |
| Channel workspace | `/cwd`、`DEFAULT_CWD`、`ALLOWED_CWD_ROOTS`、channel metadata。 |
| Agent tools | Kiro CLI authentication、ACP tool permission decisions、`TRUST_ALL_TOOLS`、`TRUST_TOOLS`。 |
| MCP tools | `/mcp manage`、channel policy DB、外部 MCP server environment guards、per-tool allowlists。 |
| Audit data | `AUDIT_LOG_*` settings、SQLite file permissions、retention policy。 |
| Generated egress | Discord send permissions、`bot-tools` safe egress queues、MCP server write restrictions。 |

## 最小權限預設

每個 channel 應獨立初始化，只啟用該 channel 真的需要的 MCP tools。內建 `bot-tools` server 只有小範圍安全預設 allowlist；`bot_query_audit` 這類敏感工具與 `bot_delete_cron` 這類 destructive tools 不在預設集合內。

外部 MCP servers 也應有自己的 environment-level policy。Discord MCP server 支援 guild allowlist、channel allowlist、read-only mode、write-tool allowlist 與 destructive-operation blocking。

## Secrets

Tokens 與 provider keys 應放在 service environment，不要放進 repository files。`/doctor` 會遮蔽已知敏感值，但 logs、shell history、process manager 與 crash report 仍應視為敏感面。

## 公開與私密 Discord 回覆

Admin panels 與敏感 slash responses 會在 Discord 支援時使用 private interaction responses。Text commands 無法保證私密性，所以 audit rows 與 audit prompt reports 僅支援 slash command。

Agent final answers 預設是一般 Discord responses，除非該 command path 明確使用 private response。不要把 secrets 放進 prompts 或 channel messages。

## Audit 與隱私

Audit 預設啟用，而且可以記錄 message content。若部署環境有更嚴格隱私要求，請設定 `AUDIT_LOG_RECORD_CONTENT=false` 與 `AUDIT_LOG_RETENTION_DAYS`。

`/audit <prompt>` 會使用私密短生命週期 agent，只授權 audit query tool，停用 Discord egress tools，並把 usage 記在觸發的 Discord 使用者身上。

## 網路與主機環境

macOS launchd 的 proxy 與 `NO_PROXY` 設定可能不同於互動 terminal。如果 MCP servers 位於 private `192.168.0.0/16` 網路，應先正確設定 host process environment，再考慮 relay。詳見 [macOS MCP 網路](macos-mcp-networking.md)。
