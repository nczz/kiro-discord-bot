# kiro-discord-bot

[English README](README.md) | [完整文件網站][docs-zh]

**一個給專案型 ACP agents 使用的 Discord bot 與 control plane。**

`kiro-discord-bot` 透過 stdio 把 Discord 頻道與討論串連到 ACP-compatible coding agents。Kiro CLI 是預設 engine，OMP 可作為可選替代 engine 啟用，並共用同一套 Discord 指令、MCP policy、audit、usage、cron、memory 與 thread-agent 控制面。

每個完成初始化的頻道都可以綁定真實專案目錄、維持自己的 agent session、累積專案指引，並透過明確的頻道 policy 安全開放 Model Context Protocol（MCP）tools。

這份 README 刻意保持精簡。完整使用指南、管理指南、MCP 設定、release runbook 與疑難排解都放在 [完整文件網站][docs-zh]。

## 為什麼不只是聊天機器人

- **綁定專案的 ACP agents**：每個 Discord 頻道對應工作目錄與 agent session。
- **Engine 彈性**：保留 Kiro 作為預設，也可針對指定 channel/thread 啟用 OMP，而不改變 Discord 操作流程。
- **可累積的脈絡**：memory、flash memory、steering files、對話歷史與專案知識不必每次從零開始。
- **安全擴充工具**：MCP server 先作為 catalog 被發現，再依頻道 policy 與 proxy 控制可見與可呼叫 tools。
- **維運控制面**：管理面板與敏感診斷在 Discord 支援時使用私密回覆。
- **自動化**：cron jobs 與 reminders 可讓 agent 在頻道 owner 脈絡下執行排程工作。

## 快速開始

1. 安裝並認證至少一個 ACP engine：`kiro-cli` 或 `omp`。
2. 建立 Discord bot，包含 `bot` 與 `applications.commands` scopes、Message Content Intent，以及必要 channel/message 權限。
3. 下載 latest release archive，或從原始碼建置。
4. 提供 `DISCORD_TOKEN`、`DISCORD_GUILD_ID`、`DEFAULT_CWD`、`DATA_DIR` 等 environment variables。
5. 先用 foreground 啟動一次，確認 bot 登入。
6. 在 Discord 頻道執行 `/cwd` 綁定專案。
7. 執行 `/doctor` 確認權限與已啟用 engine 的狀態。

下一步請看 [完整安裝指南][install-doc-zh]。

## 常用文件

| 主題 | 從這裡開始 |
| --- | --- |
| 初次設定 | [快速開始][getting-started-zh] · [安裝][install-doc-zh] |
| 日常使用 | [指令參考][commands-zh] · [監聽模式][listen-modes-zh] |
| Agent engines | [Agent Engines][agent-engines-zh] · [環境變數][environment-zh] |
| Agent 脈絡 | [Steering 檔案][steering-zh] · [核心概念][core-concepts-zh] |
| 工具權限 | [MCP 權限][mcp-zh] · [Bot Tools][bot-tools-zh] · [Discord MCP Server][mcp-discord-zh] |
| 維運 | [環境變數][environment-zh] · [部署][deployment-zh] · [Release Runbook][release-zh] |
| 安全與審查 | [安全模型][security-zh] · [Audit、用量與隱私][audit-usage-zh] |
| 支援 | [疑難排解][troubleshooting-zh] · [macOS MCP 網路][macos-networking-zh] |

## 從原始碼建置

```bash
VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
go build -ldflags "-X main.Version=$VERSION" -o kiro-discord-bot .
go build -o mcp-discord-server ./cmd/mcp-discord
go build -o mcp-media-server ./cmd/mcp-media
```

## Release 與維運

Tag 或部署前先跑：

```bash
scripts/release-preflight.sh
```

Tag、發布、部署與 rollback 請照 [Release Runbook][release-zh]。

## License

MIT

[docs-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/
[getting-started-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/getting-started.html
[install-doc-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/installation.html
[agent-engines-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/agent-engines.html
[commands-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/commands.html
[listen-modes-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/listen-modes.html
[core-concepts-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/core-concepts.html
[steering-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/steering.html
[mcp-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/mcp.html
[bot-tools-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/bot-tools.html
[mcp-discord-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/mcp-discord.html
[environment-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/environment.html
[deployment-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/deployment.html
[release-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/release.html
[security-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/security-model.html
[audit-usage-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/audit-usage-privacy.html
[troubleshooting-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/troubleshooting.html
[macos-networking-zh]: https://nczz.github.io/kiro-discord-bot/zh-TW/guide/macos-mcp-networking.html
