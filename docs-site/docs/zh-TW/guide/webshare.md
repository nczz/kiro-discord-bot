# WebShare

WebShare 可讓 channel 或 thread manager 為單一 Discord target 開啟 browser 控制介面。Browser 連到 self-host relay，bot 只需要 outbound 連線；每個 write action 都會被視為開啟 share 的 Discord 使用者所委派的操作。

當 operator 需要臨時用 web UI 操作原本在 Discord channel/thread 內的 project-bound agent session 時，使用 WebShare。不要把它當成公開客服入口、跨 guild remote console，或 Discord permissions 的替代品。

## 產品語意

每個 share 都綁定單一 target：

```text
guild_id + channel_or_thread_id + opener_user_id
```

控制連結持有人可在 share 停止或撤銷前，於該 target 內代表 opener 操作。Action 仍會經過 bot 既有 channel/thread manager，因此 CWD validation、MCP policy、safe output handling、attachment checks、audit 與 usage attribution 仍然生效。

Relay 刻意保持 content-blind。它提供 TypeScript single-page web app 並轉送 opaque WebSocket frames。Bot 與 browser 會端到端加密 application payload；relay 看得到 room ID、peer ID、role、frame size、connection metadata 與 logs，但看不到 prompts、messages、attachments、command arguments 或 agent output。

## 委派身分

Discord 不允許 bot 以真人 user account 發訊息。WebShare 因此永遠不使用 user tokens、selfbot 行為，也不偽造 Discord gateway events。

當 browser 發出一般 Discord message 時，bot 會透過 bot 自己擁有的 Discord webhook 發送，webhook username 會是：

```text
Alice via WebShare
```

訊息內容保留使用者輸入的本文，不再在 content 裡額外加上 `via WebShare` prefix。此顯示模式需要 bot 在父層頻道具備 Discord **Manage Webhooks** 權限；缺少權限時 `/webshare start` 會拒絕建立 share。Browser UI 也會顯示 opener 與 target，讓 link holder 知道目前被委派的是誰的 Discord authority。

## 連結風險

控制連結是一個 capability。任何拿到連結的人，都可在 share 結束前於 shared target 內以 opener 權限操作。請把它視為短期 secret：

- 不要把完整 URL 貼到 Discord、issue tracker、logs、screenshots 或 support tickets。
- 優先用預期 operator 控制的 browser profile 開啟。
- Handoff 完成後立即停止 share。
- 如果懷疑連結外洩，立刻 revoke share。

Secret 放在 URL fragment，例如 `https://relay.example/#/join/<room>.<secret>`。Browser fragment 不會送進 HTTP request，因此 nginx 與 relay 不應記錄 room key 或 write token。Relay WebSocket query string 仍可能包含 `role` 等非 secret routing fields；不要把 token 放在 query string。

## 指令

| Command | 用途 |
| --- | --- |
| `/webshare start` | 為目前 channel 或 thread 建立／重連 WebShare，並以 private response 回傳 opener 的 control/view links。 |
| `/webshare status` | 私密顯示 active share state、relay connection、opener、target，以及 stop/revoke 指引。 |
| `/webshare stop` | 停止 opener 在目前 target 的 active share，並斷開 browser peers。 |
| `/webshare revoke` | 當 requester 具備 channel 管理權限時，對目前 target 執行 emergency revoke。 |

同一使用者在同一 target 只能有一個 active share。Share active 期間，opener 在 Discord app 內不能直接對該 target 發 prompt 或使用 bot command；必須使用 browser link 或 `/webshare stop`。其他具授權的 managers 仍可在 opener 不在場時 revoke share。

## Browser 可執行的動作

Write-capable WebShare 可執行：

- 對 target 既有 channel 或 thread session 傳送 agent prompt；
- 透過 WebShare command bridge 執行支援的 bot commands；
- 發送標示 `via WebShare` 的一般 channel/thread message；
- interrupt 或 cancel 目前 agent job；
- 從 parent channel 或 source message 建立 managed child thread；
- 切換到 shared target 底下既有 managed child thread；
- 上傳檔案供 agent 使用；
- 透過 bot-issued attachment refs 取得 scope 內 Discord attachments。

Browser 不會取得任意 Discord channels、raw local paths 或 direct ACP sessions 的權限。每個 action 都會重新檢查 opener 的 Discord access 與 channel management state。如果 opener 失去存取或管理權限，新的 actions 會被拒絕，share 會進入 degraded 或 revoked 狀態。

## Mentions

WebShare v1 只支援明確選取的 user mentions 與 bot mention。不支援 role mentions、`@everyone` 或 `@here`。

Browser composer 應使用 WebShare mention picker，而不是 raw mention text。Bot 發送 Discord message 時會設定 explicit allowed-mention lists：只有選取的 user IDs 可 ping，roles 為空，parse-all 停用，replied-user ping 也停用。若 member lookup 不可用，autocomplete 會降級為 cached/recent users，而不是廣泛解析所有 mentions。

## Attachments

Uploads 與 Discord attachment fetches 都維持 target-scoped：

- Browser uploads 會 chunked、size-limited、sanitized，並存到 validated project CWD 底下的 `.kiro-bot/attachments/webshare-<share>/<upload>/`。
- Discord attachments 只會透過 opaque bot-issued refs 暴露給 browser；ref 綁定 share、target、message、attachment、filename、size 與 expiry。
- Browser 不能用任意 channel/message/attachment ID 要求檔案。
- Local files 或 fetched content 顯示回 Discord 或 web UI 前，仍會套用 safe egress 與 redaction rules。
- Cleanup 沿用一般 attachment retention settings。

## Thread 行為

在 thread 內啟動 WebShare 時，share scope 就是該 thread。Agent prompts、bot commands、messages、uploads 與 attachment refs 會使用該 thread 既有 session/CWD 行為。

在 parent channel 啟動 WebShare 時，share scope 是該 parent channel。Browser 可建立 managed child threads，或選擇屬於 parent target 的既有 managed child thread。Thread operations 使用 Discord 正常 thread APIs 與 bot channel manager；WebShare 不建立另一套 session model。

## Relay 部署

Relay 是純 Go self-host binary。它可與 bot 同機、部署在另一台 VM，或以 container 放在 nginx、Caddy、Traefik 後方。Bot 會以 authenticated room host 主動 outbound 連到 relay，因此 bot process 不需要 inbound port。

Build 或安裝 relay binary：

```bash
go build -o webshare-relay ./cmd/webshare-relay
```

產生 host token，並把它放在 repository 之外：

```bash
install -d -m 0750 /etc/kdb-webshare
openssl rand -base64 32 > /etc/kdb-webshare/host-token
chmod 0640 /etc/kdb-webshare/host-token
```

Relay environment：

```env
RELAY_ADDR=:8080
RELAY_PUBLIC_BASE_URL=https://relay.example
RELAY_HOST_TOKEN_FILE=/etc/kdb-webshare/host-token
RELAY_TRUST_PROXY=true
RELAY_MAX_ROOMS=1000
RELAY_MAX_PEERS_PER_ROOM=32
RELAY_MAX_FRAME_BYTES=4194304
RELAY_HOST_IDLE_TIMEOUT=0
RELAY_GUEST_IDLE_TIMEOUT=0
RELAY_WRITE_TIMEOUT=30s
RELAY_LOG_LEVEL=info
RELAY_METRICS_ADDR=127.0.0.1:9090
```

Bot environment：

```env
WEBSHARE_ENABLED=true
WEBSHARE_RELAY_URL=wss://relay.example
WEBSHARE_PUBLIC_BASE_URL=https://relay.example
WEBSHARE_HOST_TOKEN_FILE=/etc/kdb-webshare/host-token
WEBSHARE_MAX_FRAME_BYTES=4194304
WEBSHARE_RECONNECT_INITIAL_MS=1000
WEBSHARE_RECONNECT_MAX_MS=30000
```

Production 建議使用 `WEBSHARE_HOST_TOKEN_FILE` 與 `RELAY_HOST_TOKEN_FILE`，避免 token 出現在 shell history、process listing 或 Compose files。`WEBSHARE_HOST_TOKEN` 只適合本機開發或由 secret manager 直接注入。

### nginx Reverse Proxy

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    '' close;
}

server {
    listen 443 ssl http2;
    server_name relay.example;

    client_max_body_size 32m;

    location /r/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Authorization $http_authorization;
        proxy_read_timeout 7d;
        proxy_send_timeout 7d;
        proxy_buffering off;
    }

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

請設定足夠長的 WebSocket read timeout。如果部署多個 relay instances，同一個 room 的 sockets 必須路由到同一個 instance；第一版假設每個 public hostname 只有一個 relay process。

### systemd 範例

```ini
[Unit]
Description=KDB WebShare Relay
After=network-online.target
Wants=network-online.target

[Service]
User=kdb-webshare
Group=kdb-webshare
EnvironmentFile=/etc/kdb-webshare/relay.env
ExecStart=/usr/local/bin/webshare-relay serve
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/kdb-webshare

[Install]
WantedBy=multi-user.target
```

### Docker Compose 範例

```yaml
services:
  webshare-relay:
    image: ghcr.io/<owner>/kdb-webshare-relay:<tag>
    restart: unless-stopped
    environment:
      RELAY_ADDR: ":8080"
      RELAY_PUBLIC_BASE_URL: "https://relay.example"
      RELAY_HOST_TOKEN_FILE: "/run/secrets/relay_host_token"
    secrets:
      - relay_host_token
    ports:
      - "127.0.0.1:8080:8080"

secrets:
  relay_host_token:
    file: ./relay_host_token.txt
```

## Stop 與 Revoke

正常 handoff 完成時使用 `/webshare stop`。它會關閉 share、斷開 browsers、解除 opener 在該 target 的 lockout，並阻止同一 link 後續 action。

當連結可能外洩、opener 不在場，或其他 manager 需要結束 share 時，使用 `/webshare revoke`。Revocation 會記錄到 audit metadata，但不會保存完整 link、secret、token、local raw paths 或 CDN signed URLs。
