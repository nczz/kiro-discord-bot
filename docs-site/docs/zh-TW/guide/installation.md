# 安裝

這是 canonical 安裝指南。repo 裡的 `INSTALL.md` 保留為 agent-friendly checklist，詳細說明以本頁為準。

## 1. 準備 ACP Engine

啟動 bot 前，至少要安裝並完成一個支援的 ACP engine 認證。

Kiro CLI 與 OMP 都是外部 agent CLI。`kiro-discord-bot` 不打包、也不維護這兩個 CLI；它只會把它們作為 ACP engines 啟動，並在外層套用 Discord policy、audit、usage 與 delivery behavior。若官方安裝方式與下方範例不同，請以各自 upstream 文件為準。

Kiro CLI：

```bash
curl -fsSL https://cli.kiro.dev/install | bash
export PATH="$HOME/.local/bin:$PATH"
kiro-cli --version
```

互動式主機可用：

```bash
kiro-cli login
```

Headless 主機請在 bot service environment 設定 `KIRO_API_KEY`。

OMP 請從官方專案網站 [omp.sh](https://omp.sh/) 開始。先安裝並完成 `omp` 認證；若不在 `PATH`，用 `OMP_PATH` 指定。新的 production 部署建議使用 bot 專屬 profile，並在啟用 OMP 前先完成此 profile 認證：

```bash
omp --version
omp setup
```

隔離 production profile：

```bash
OMP_PROFILE=kiro-discord-bot omp setup
```

CLI 更新請使用各自指令：

```bash
kiro-cli update -y
omp update --check
omp update
```

更新任一 CLI 後請重啟 bot，讓 ACP preflight 與後續 agent sessions 使用新的 binary。

### 建議 Engine 路徑

| 路徑 | 適合情境 | 注意事項 |
| --- | --- | --- |
| Kiro-only | 第一次安裝，或希望採用最保守的 production path。 | 不需要 OMP setup。 |
| Dual-engine | 想保留 Kiro 作為預設，但讓特定 channel 或 thread 可切換到 OMP。 | 啟用 `/engine` 切換前，兩個 engines 都要先安裝並認證。 |
| OMP-only | 這個 bot process 只應使用 OMP。 | 確認 `omp` 已用執行 bot 的同一個 OS service user 完成認證。 |

## 2. 建立 Discord Bot

在 Discord Developer Portal 建立 application，並設定：

| 區域 | 必要設定 |
| --- | --- |
| OAuth2 scopes | `bot`, `applications.commands` |
| 基礎權限 | View Channels, Send Messages, Send Messages in Threads, Create Public Threads, Read Message History, Add Reactions, Use Slash Commands |
| 選用權限 | Manage Threads, Embed Links, Attach Files，依啟用功能決定 |
| Privileged intents | Message Content Intent |

General Information 的 Interactions Endpoint URL 必須保持空白。若設定 URL，Discord slash commands 會送到該 endpoint，而不是 gateway bot，導致 command timeout。

## 3. 下載或建置

依 OS/architecture 下載 latest release archive：

| OS | Arch | Archive |
| --- | --- | --- |
| macOS | arm64 | `kiro-discord-bot_darwin_arm64.tar.gz` |
| macOS | amd64 | `kiro-discord-bot_darwin_amd64.tar.gz` |
| Linux | amd64 | `kiro-discord-bot_linux_amd64.tar.gz` |
| Linux | arm64 | `kiro-discord-bot_linux_arm64.tar.gz` |

範例：

```bash
curl -fsSL https://github.com/nczz/kiro-discord-bot/releases/latest/download/kiro-discord-bot_darwin_arm64.tar.gz | tar xz
```

從原始碼建置：

```bash
VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
go build -ldflags "-X main.Version=$VERSION" -o kiro-discord-bot .
go build -o mcp-discord-server ./cmd/mcp-discord
go build -o mcp-media-server ./cmd/mcp-media
```

## 4. 設定 Environment

bot 不會自動載入 `.env`。foreground shell、launchd、systemd 或 Docker 必須注入 environment variables。

最小設定：

```env
DISCORD_TOKEN=your-bot-token
DISCORD_GUILD_ID=your-guild-id
DEFAULT_CWD=/projects
DATA_DIR=./data
BOT_LOCALE=zh-TW
```

正式環境建議加上：

```env
KIRO_API_KEY=your-headless-key
AGENT_ENGINE=kiro
AGENT_ENGINES_ENABLED=kiro
ALLOWED_CWD_ROOTS=/projects
CRON_TIMEZONE=Asia/Taipei
USAGE_TIMEZONE=Asia/Taipei
PREFLIGHT_MODE=warn
THREAD_AGENT_MAX=5
THREAD_AGENT_IDLE_SEC=900
```

Dual-engine bot：

```env
AGENT_ENGINE=kiro
AGENT_ENGINES_ENABLED=kiro,omp
OMP_PATH=omp
```

OMP-only bot：

```env
AGENT_ENGINE=omp
AGENT_ENGINES_ENABLED=omp
OMP_PATH=omp
```

啟動後用 `/doctor` 查看有效 runtime 設定；敏感值會被遮蔽。完整變數與預設值見 [環境變數參考](environment.md)。

啟用 OMP 或允許 production 使用 `/engine` 切換前，請先閱讀 [Agent Engines](agent-engines.md)。

## 5. Foreground 啟動一次

```bash
set -a
. ./.env
set +a
./kiro-discord-bot
```

預期 log：

1. ACP preflight 執行並回報每個已啟用 engine 的狀態。
2. `kiro-discord-bot <version> starting`。
3. Slash commands 註冊。
4. `Bot running as <name>#<discriminator>`。

## 6. 初始化 Discord 頻道

在 Discord 頻道執行 `/cwd`。Setup panel 會讓頻道管理員在 `DEFAULT_CWD` 下選擇或建立專案。完成後，該頻道可以開始 agent 工作，並以安全預設 allowlist 啟用內建 `bot-tools` MCP。

在初始化後的頻道執行 `/doctor`，確認 bot 可以 view/send/create thread/read history，並可連到已啟用的 ACP engine。

## 7. 決定下一步

基礎使用不需要外部 MCP server。請逐步啟用能力：

- 用 `/memory` 與 `/flashmemory` 管理輕量 prompt rules。
- 用 `/steering create` 建立版本化專案指引。
- 用 `/mcp manage` 依頻道啟用 MCP 工具。
- 只有在頻道有明確 owner 時，才啟用 `/cron` 排程工作。
