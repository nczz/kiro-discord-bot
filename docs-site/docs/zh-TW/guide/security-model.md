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
| WebShare relay | Self-host relay host token、URL-fragment link secrets、E2E encrypted frames、opener permission rechecks、明確 `via WebShare` display。 |

## 最小權限預設

每個 channel 應獨立初始化，只啟用該 channel 真的需要的 MCP tools。內建 `bot-tools` server 只有小範圍安全預設 allowlist；`bot_query_audit` 這類敏感工具與 `bot_delete_cron` 這類 destructive tools 不在預設集合內。

外部 MCP servers 也應有自己的 environment-level policy。Discord MCP server 支援 guild allowlist、channel allowlist、read-only mode、write-tool allowlist 與 destructive-operation blocking。

## WebShare 委派

WebShare control links 是單一 channel 或 thread target 的 delegated capabilities。Relay 是 content-blind 且必須 self-host；它只轉送 encrypted frames，不應接收 Discord user tokens、raw local paths、command secrets 或 plaintext prompts。Bot access 維持 outbound-only，透過 relay host WebSocket 連線。

每個 WebShare write action 都以 opener 身分授權，並在執行前重新檢查 Discord access。Share active 期間，opener 在該 target 的 direct Discord prompt 與 bot-command paths 會被 lockout；必須使用 browser link 或 `/webshare stop`。Browser-originated Discord messages 必須明確標示 `via WebShare`，v1 mentions 只限明確選取 users 與 bot。Role mentions、parse-all mentions、`@everyone` 與 `@here` 均停用。

## Secrets

Tokens 與 provider keys 應放在 service environment，不要放進 repository files。`/doctor` 會遮蔽已知敏感值，但 logs、shell history、process manager 與 crash report 仍應視為敏感面。

WebShare relay host tokens 與完整 control/view links 都是 secrets。Production 優先使用 `WEBSHARE_HOST_TOKEN_FILE` 與 `RELAY_HOST_TOKEN_FILE`。完整 WebShare URLs 包含 fragment secrets；fragment 不會送到 relay，但 URL 仍可能留在 browser history、screenshots、chat 與 support logs 中。

## 公開與私密 Discord 回覆

Admin panels 與敏感 slash responses 會在 Discord 支援時使用 private interaction responses。Text commands 無法保證私密性，所以 audit rows、audit prompt reports、usage reports/history 僅支援 slash command。

Agent final answers 預設是一般 Discord responses，除非該 command path 明確使用 private response。不要把 secrets 放進 prompts 或 channel messages。

## Audit 與隱私

Audit 預設啟用，而且可以記錄 message content。若部署環境有更嚴格隱私要求，請設定 `AUDIT_LOG_RECORD_CONTENT=false` 與 `AUDIT_LOG_RETENTION_DAYS`。

`/audit <prompt>` 會使用私密短生命週期 agent，只授權 audit query tool，停用 Discord egress tools，並把 usage 記在觸發的 Discord 使用者身上。

## 網路與主機環境

macOS launchd 的 proxy 與 `NO_PROXY` 設定可能不同於互動 terminal。如果 MCP servers 位於 private `192.168.0.0/16` 網路，應先正確設定 host process environment，再考慮 relay。詳見 [macOS MCP 網路](macos-mcp-networking.md)。
