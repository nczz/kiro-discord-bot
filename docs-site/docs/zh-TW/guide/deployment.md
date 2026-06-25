# 部署

## 本機 Foreground 啟動

建立 service 前，先用 foreground 啟動：

```bash
set -a
. ./.env
set +a
./kiro-discord-bot
```

確認 bot 登入、slash commands 註冊成功，並能回應 `/doctor`。

## macOS launchd

macOS 建議用 LaunchAgent，明確透過 shell source `.env` 後執行 release binary。若 private LAN MCP server 在互動 shell 可連，但 launchd 下 `/mcp manage` scan 失敗，請檢查 proxy 變數、Local Network 權限與 service identity。完整 runbook 請看 [macOS MCP 網路](macos-mcp-networking.html)。

## Linux systemd

Linux host 使用 service unit 設定 `WorkingDirectory`、`EnvironmentFile` 與 release binary 路徑。先 build/test，再 stop service、替換 binary、start service，最後用 `/doctor` 驗證。

## Docker

Compose 設定使用 host networking，掛載 Kiro authentication 與 project roots，並讓 runtime MCP config 和全域 Kiro MCP settings 隔離。Catalog servers 仍需透過 `/mcp` 依頻道啟用。

## Release 更新

tag 或部署 release 前，先執行：

```bash
scripts/release-preflight.sh
```

若有修改 ACP、MCP policy、bot tools、cron 或部署行為，請執行 [Release Runbook](release.html) 中對應的 smoke checks。
