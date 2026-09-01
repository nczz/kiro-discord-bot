# 部署

## 本機 Foreground 啟動

建立 service 前，先用 foreground 啟動：

```bash
set -a
. ./.env
set +a
./kiro-discord-bot
```

確認 bot 登入、slash commands 註冊成功，並能回應 `/doctor`。把 foreground command 轉成服務前，先檢查 [環境變數參考](environment.md)。

## macOS launchd

macOS 建議用 LaunchAgent，明確透過 shell source `.env` 後執行 release binary。若 private LAN MCP server 在互動 shell 可連，但 launchd 下 `/mcp manage` scan 失敗，請檢查 proxy 變數、Local Network 權限與 service identity。完整 runbook 請看 [macOS MCP 網路](macos-mcp-networking.html)。

## Linux systemd

Linux host 使用 service unit 設定 `WorkingDirectory`、`EnvironmentFile` 與 release binary 路徑。先 build/test，再 stop service、替換 binary、start service，最後用 `/doctor` 驗證。

## Docker

Compose 設定使用 host networking，掛載所選 engine 的 authentication state 與 project roots，並讓 runtime MCP config 和全域 catalog sources 隔離。Catalog servers 仍需透過 `/mcp` 依頻道啟用。

## A2A NATS 部署

第一次設定請先看 [使用 NATS 啟用 A2A](a2a-nats-setup.md)，內容從 NATS server、`.env` 到 Discord policy 操作。啟用 bot A2A 變數前，先部署 NATS/JetStream。內部輕量部署使用一個 private JetStream node，搭配 TLS、token authentication、persistent storage、localhost-only monitoring 與 host/network firewalling；強化部署則應使用 `NATS_CREDS_FILE` NKey/JWT credentials、選用 TLS CA validation、每個穩定 bot/base identity 對應一組 credential，且 ACL 必須授權由該 base identity 衍生的 runtime ID subjects，並設定 `A2A_PRODUCTION_SECURITY=true`。透過 service manager 或 container environment 注入 A2A 變數，restart 或 drain bot，然後用 `/doctor` 與 rollout smoke checks 驗證。Setup 與 rollout gates 準備好前，保持 `NATS_URL` 為空。Identity、subject、task、policy 與 delivery model 見 [A2A 協議模型](a2a-protocol.md)。

## WebShare Relay 部署

啟用 WebShare 後會多一個 process：純 Go `webshare-relay` binary。Relay 可放在 nginx、Caddy 或 Traefik 後方，同時提供 TypeScript web UI 與 WebSocket room endpoint。Bot 會以 authenticated host 主動 outbound 連到 relay，因此不要為 WebShare 開 inbound bot port。

Production deployment 應該：

- 在 reverse proxy 或 relay 終止 TLS；
- 將 `RELAY_HOST_TOKEN_FILE` 與 matching `WEBSHARE_HOST_TOKEN_FILE` 放在 repository 外；
- 透過 proxy 保留 WebSocket `Upgrade`、`Connection`、`Authorization`、`Host` 與 `X-Forwarded-Proto` headers；
- 使用足夠長的 WebSocket read/send timeouts；
- 除非已實作 sticky room routing，否則每個 public hostname 只跑一個 relay process；
- restart 後執行 `/webshare status` 與 `/doctor`。

nginx、systemd、Docker Compose、bot env 與 relay env 範例見 [WebShare](webshare.md)。

## Release 更新

tag 或部署 release 前，先執行：

```bash
scripts/release-preflight.sh
```

若有修改 ACP、MCP policy、bot tools、cron 或部署行為，請執行 [Release Runbook](release.html) 中對應的 smoke checks。
