# 快速開始

`kiro-discord-bot` 透過 ACP over stdio 把 Discord 連到 ACP agent。Kiro CLI 是預設 engine，OMP 可作為可替換 ACP engine 啟用，並共用同一套 Discord 指令、MCP policy、audit、usage、cron、memory 與 thread-agent 控制面。它適合希望 agent 理解專案、累積脈絡，並從 Discord 安全執行開發或維運工作的團隊。

## 需求

- Discord bot application，scope 包含 `bot` 與 `applications.commands`。
- Discord 權限：View Channels、Send Messages、Add Reactions、Read Message History。
- Discord Developer Portal 中啟用 Message Content Intent。
- 至少一個已安裝並完成認證的 ACP engine：`kiro-cli` 或 `omp`。
- 一個允許 bot 作為頻道工作目錄的專案路徑。

## 安裝流程

1. 從 GitHub Releases 下載 release archive，或從原始碼建置。
2. 建立 `.env`，填入 Discord token、guild ID、default CWD、data directory 與 engine 設定。
3. 先在 foreground 啟動 bot，確認 slash commands 有註冊。
4. 依環境設定 launchd、systemd 或 Docker。
5. 在 Discord 用 `/cwd` 初始化頻道並綁定專案。
6. 在目標頻道執行 `/doctor`，確認 Discord 權限與 ACP 狀態。

## 第一個頻道

未初始化的頻道不會直接開始 agent 工作。bot 會暫停普通訊息，並要求頻道管理員開啟私密的 `/cwd` setup panel。Setup 會在 `DEFAULT_CWD` 下選擇或建立專案，需要時準備 agent context 檔案，並用安全預設 allowlist 啟用內建 `bot-tools` MCP。

初始化後，頻道就可以開始一般 agent 工作。用 `/pause` 切到 mention-only 模式，用 `/back` 回到 full-listen 並恢復新任務開 thread。

## 下一步

- 用 `/memory` 加入持久偏好。
- 用 `/flashmemory` 加入目前 session 的臨時重點。
- 透過 `/steering create` 或 `/steering edit` 把共用專案規則、架構與流程放進 `AGENTS.md`。
- 用 `/mcp manage` 依頻道啟用 MCP 工具。
- 啟用 OMP 或調整 `AGENT_ENGINES_ENABLED` 前，先閱讀 [Agent Engines](agent-engines.md)。
