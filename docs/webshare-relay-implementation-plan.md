# WebShare Relay Implementation Plan

> 版本: 0.1 | 日期: 2026-08-30 | 狀態: design approved by requested product direction | 目的: 以純 Go/self-host relay + TypeScript web client，讓 Discord channel/thread 的管理員產生 capability link，將該 target 的 agent/session 與頻道操作委派到 web 介面，同時保留現有 Discord 權限、安全輸出、附件與 audit 邊界。

## 0. 決策摘要

本功能要做的是 **Discord target-scoped WebShare delegated session**。

核心決策：

1. **Relay 是純 Go/self-host**：一個可部署在 VM/container/systemd 後方、可被 nginx/Caddy/Traefik 反向代理的 Go binary。它只負責 static web assets、WebSocket room routing、limits、observability，不理解 agent/Discord 明文。
2. **Web UI 是 TypeScript SPA**：relay binary 可 embed 打包後的 web assets；也可由前級靜態站提供。browser 端使用 WebCrypto，key/token 放 URL fragment，不進 HTTP path/query。
3. **Bot 主動連 relay**：bot 不開 inbound port；`/webshare start` 後 bot 以 host 身份 outbound WebSocket 連到 relay room。
4. **Relay content-blind**：host/browser frame payload 端到端加密。relay 只看 room id、peer id、role、frame size、connection metadata。
5. **Link 是 opener 的 delegated capability**：link 持有人可在指定 guild/channel/thread 內代表 opener 操作。這不是 Discord user token，也不是 selfbot。
6. **Discord 顯示必須明確標示 delegation**：Discord API 不允許 bot 真正以 user account 發言。第一版可發一般 channel message，但必須顯示 `Alice via WebShare` 或等價標記；不可假裝成 Alice 本人。
7. **第一版即完整包含 channel parity**：web 介面可看互動、傳 prompt、執行 bot 指令、發一般頻道訊息、tag 目前可解析用戶與 bot、上傳檔案、取得 scope 內 Discord attachment、開/用討論串、中斷/控制目前 agent job。
8. **Opener Discord 端鎖定**：active share 期間，opener 在 Discord app 內不能再直接呼叫 agent 或 bot 指令；必須用 web link 或 `/webshare stop`。其他具管理權限者可 emergency revoke。
9. **權限每次 action 重新判定**：web action 使用 opener 的 Discord actor context，但只限該 target；若 opener 失去 channel/thread 存取或管理權，share 立即拒絕 action 並進入 revoked/degraded 狀態。
10. **現有安全邊界不降級**：redaction、safe egress、CWD validation、MCP policy、AllowedMentions、audit、attachment sanitizer 全部沿用或抽成共用 helper；不得因 web parity 繞過。

## 1. 產品語意

### 1.1 使用者流程

管理員在 channel 或 thread 執行：

```text
/webshare start
```

bot 回 ephemeral：

```text
WebShare 已啟用。
控制連結: https://relay.example/#<room>.<secret>
檢視連結: https://relay.example/#<room>.<view-secret>
此控制連結持有人可代表你在此頻道操作 agent、發言、上傳/取得附件與建立討論串。
停止: /webshare stop
```

browser 開啟控制連結後：

- 看到 target channel/thread 名稱與 delegation banner。
- 看到目前 session 狀態、agent 狀態、recent channel events、thread events。
- 可輸入一般 message 或 agent prompt。
- 可 tag 可解析的 target users 與 bot；role mention、`@everyone`、`@here` 不在 v1。
- 可執行 bot 指令，包含原本 Discord channel 內可用的 bang/slash-equivalent 指令。
- 可上傳檔案供 agent 使用。
- 可選取 scope 內 Discord attachment 下載/加入 prompt。
- 可建立討論串或切換到 target thread。
- 可 interrupt/cancel 目前 job。

### 1.2 身份顯示

Discord 不支援 bot 以真人 user account 發言；禁止 user token/selfbot。

第一版訊息顯示策略：

```text
**Alice via WebShare**: message text
```

若後續加入 webhook display，也必須使用：

```text
username = "Alice via WebShare"
```

不可只用 `Alice`，避免 spoofing 與 audit ambiguity。

### 1.3 Target scope

每個 share 綁定單一 target：

```text
guild_id + target_type(channel|thread) + target_id + opener_user_id
```

行為完整對標「在該 target 中操作」，不是跨 guild/channel 的 remote console。

若在 parent channel 開啟：

- 預設 target 是 parent channel。
- 可由 web 建立新 thread，之後 prompt/message 可選該 managed thread。
- thread 行為必須用現有 channel/thread model，不可新建平行 session model。

若在 thread 開啟：

- target 是該 thread。
- CWD 與 session 繼承/override 沿用 `channel.Manager.TargetCWDPath`、session key 既有規則。

## 2. 現有架構接點

| 需求 | 既有接點 | 使用方式 |
|---|---|---|
| Discord command routing | `bot/handler.go`, `bot/commands.go`, interaction handlers | 新增 `/webshare`，web command bridge reuse existing command functions where response shape can be abstracted through `cmdCtx`。 |
| 管理權限 | `bot.userCanManageAuditTarget`, `bot.userCanManageTarget` | `/webshare start/revoke` 與 web delegated action 每次使用 opener context 重新檢查。 |
| Channel/session lifecycle | `channel.Manager`, `channel.SessionStore`, `channel.Worker` | web prompt/post/thread operations 必須通過 Manager/Worker；不得直接操作 ACP。 |
| CWD safety | `channel.Manager.ValidateCWD`, `TargetCWDPath` | web upload/attachment manifest 只落在 validated project CWD 下。 |
| Discord attachments | `downloadAttachments`, `safeAttachmentFilename`, `attachmentManifest` | 抽共用 helper，支援 web upload 與 Discord attachment proxy。 |
| Safe egress | `bot/safe_egress.go`, `internal/botegress` | web-visible file/result 也走 redaction/sanitization；不得直接 expose local files。 |
| Dynamic bot tools target | `channel/bot_tools_target.go` | 新增 delegated actor fields：`WebShareID`, `DelegatedByUserID`, `RemoteActorName`, `DelegatedSession`。 |
| Audit | `bot/audit.go`, channel audit hooks | 新增 webshare event type + metadata；禁止記錄 secret/link/raw local path。 |
| Existing webhook input | `/webhook mode` decision | webshare 是獨立 ingress；不可弱化 webhook leading-mention gate 或 bot-authored loop guard。 |
| Member lookup | existing Discord state/API helpers | tag autocomplete 優先 recent/cache，必要時用 Discord member search；缺少 intent/API 權限時降級。 |

## 3. Package layout

### 3.1 Bot repo additions

```text
webshare/
  model.go              // Share, Peer, Capability, Frame metadata
  token.go              // crypto-random IDs, key wrapping, token fingerprints
  store.go              // SQLite authoritative store
  migrations.go
  relay_client.go       // bot outbound host WebSocket client
  crypto.go             // bot-side E2E frame seal/open
  protocol.go           // typed payloads after decrypt
  dispatcher.go         // validated browser actions -> manager/bot operations
  attachments.go        // web upload + Discord attachment bridge
  audit.go              // audit event helpers
  limits.go

bot/
  webshare_commands.go
  webshare_components.go
  webshare_bridge.go    // web command reply adapter and Discord post adapter
  webshare_test.go

channel/
  webshare.go           // Manager WebShare entrypoints and source-aware enqueue
  webshare_test.go

cmd/webshare-relay/
  main.go               // pure Go self-host relay binary

internal/websharerelay/
  room.go
  hub.go
  protocol.go           // relay-level opaque routing frame
  config.go
  limits.go
  metrics.go
  static.go
```

### 3.2 Web client additions

```text
webshare-web/
  package.json
  src/
    App.tsx
    crypto.ts
    protocol.ts
    transport.ts
    composer.tsx
    mentions.tsx
    attachments.tsx
    commands.tsx
    threads.tsx
  dist/                 // generated, embedded by Go relay; not hand-edited
```

`webshare-web/src/protocol.ts` and Go `webshare/protocol.go` need golden JSON compatibility tests. Do not rely on manual duplicated shapes without tests.

## 4. Relay architecture: pure Go/self-host

### 4.1 Responsibilities

Relay owns:

- HTTPS/WebSocket endpoint compatibility behind reverse proxy.
- Static web client delivery.
- Room lifecycle.
- Host/guest socket registration.
- Peer id assignment.
- Binary frame routing.
- Rate/size/connection limits.
- Structured logs and metrics.

Relay does not own:

- Discord authz.
- Agent state.
- Plaintext command parsing.
- Attachment content inspection.
- Link secret validation beyond optional host token.
- Durable long-term session data.

### 4.2 Binary/runtime choices

Recommended Go dependencies:

- `nhooyr.io/websocket` for context-aware WebSocket handling.
- `net/http` stdlib server.
- `embed` for static SPA assets.
- no per-message compression by default because frames are encrypted and compression can add side-channel/CPU cost.

Binary commands:

```text
webshare-relay serve
webshare-relay version
webshare-relay healthcheck
```

### 4.3 Config

Environment/config file keys:

```text
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

Default no TTL means no application-level room expiry while host is connected. Transport still needs ping/pong keepalive and reconnect.

Relay 只能 enforce opaque frame size，不能有 upload-specific plaintext limit。Upload total size、chunk count、MIME policy、per-share quota 全部在 bot decrypt typed payload 後執行。

### 4.4 Host authentication

Relay should not let arbitrary internet clients create host rooms.

Host connection:

```http
GET /r/<room_id>?role=host HTTP/1.1
Authorization: Bearer <relay-host-token>
Upgrade: websocket
```

Guest connection:

```http
GET /r/<room_id>?role=guest HTTP/1.1
Upgrade: websocket
```

Do not put host token in query string. nginx access logs commonly record query strings.

### 4.5 Room routing

Rules:

- First authenticated host creates room.
- Second host for same room is rejected with close code `4009`.
- Guest for missing room is rejected with close code `4004`.
- Host disconnect closes room by default and notifies guests with `room-closed`.
- Bot reconnect creates a new host connection for the same persisted room only if relay still has no host; clients reconnect using same URL.
- If relay process restarts, rooms disappear; bot host reconnect recreates room; browser clients reconnect.

Relay frame shape is opaque:

```text
uint32 peer_id_be + encrypted_payload_bytes
```

Routing:

- browser -> relay -> host: relay overwrites/sets sender peer id.
- host -> relay -> peer id N: targeted.
- host -> relay -> peer id 0: broadcast.

### 4.6 Reverse proxy requirements

nginx example:

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

Operational notes:

- URL fragment is never sent to nginx/relay; this protects room key/write token from access logs.
- Query string still contains `role`; avoid secrets in query.
- Long `proxy_read_timeout` is required for long-lived sessions.
- If multiple relay instances are deployed, rooms need sticky routing by room id or a shared coordinator. First version assumes one relay process per public hostname.
- TLS terminates at reverse proxy or relay. If proxy terminates TLS, set `RELAY_TRUST_PROXY=true` and validate `X-Forwarded-Proto` only from trusted proxy addresses.

### 4.7 Deployment options

#### systemd

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

#### Docker Compose

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

## 5. Bot-side WebShare store

SQLite authoritative store under bot `DATA_DIR`:

```text
DATA_DIR/webshare/webshare.sqlite
```

### 5.1 `webshare_sessions`

```sql
CREATE TABLE webshare_sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  share_id TEXT NOT NULL UNIQUE,
  guild_id TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  parent_channel_id TEXT NOT NULL DEFAULT '',
  opener_user_id TEXT NOT NULL,
  opener_username TEXT NOT NULL DEFAULT '',
  relay_url TEXT NOT NULL,
  public_base_url TEXT NOT NULL,
  room_id TEXT NOT NULL UNIQUE,
  room_key_ciphertext BLOB NOT NULL,
  write_token_hash BLOB NOT NULL,
  view_secret_fingerprint TEXT NOT NULL DEFAULT '',
  capabilities_json TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_connected_at TEXT NOT NULL DEFAULT '',
  last_peer_seen_at TEXT NOT NULL DEFAULT '',
  revoked_at TEXT NOT NULL DEFAULT '',
  revoked_by_user_id TEXT NOT NULL DEFAULT '',
  revoke_reason TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX webshare_active_opener_target
ON webshare_sessions(guild_id, target_type, target_id, opener_user_id)
WHERE status IN ('created','connecting','active','disconnected');
```

### 5.2 `webshare_events`

```sql
CREATE TABLE webshare_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  share_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  actor_user_id TEXT NOT NULL DEFAULT '',
  remote_actor_name TEXT NOT NULL DEFAULT '',
  target_id TEXT NOT NULL DEFAULT '',
  allowed INTEGER NOT NULL,
  reason_code TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);
```

No full URL, key, write token, CDN signed URL, or local raw path in either table.

## 6. Link crypto/protocol

### 6.1 Link format

```text
https://relay.example/#/join/<room_id>.<base64url(secret)>
```

Secret payload variants:

```text
view:  version || room_key
write: version || room_key || write_token
```

The bot returns both links to opener. The relay only receives `/` path and later WebSocket `/r/<room_id>?role=guest`; fragment stays browser-local.

### 6.2 E2E frame encryption

Browser and bot derive frame keys from `room_key`:

```text
HKDF-SHA256(room_key, salt=room_id, info="kdb-webshare-v1")
```

Frame encryption:

- AES-256-GCM or XChaCha20-Poly1305.
- 96-bit nonce for AES-GCM, never reused per direction.
- Direction-separated keys or nonce prefixes for host->guest and guest->host.
- Associated data includes protocol version, room id, peer id, sequence number, frame type.
- Replay protection via monotonically increasing sequence per peer.

Browser write permission:

- `hello` includes proof of write token possession inside encrypted payload.
- Bot compares token hash timing-safely.
- View clients receive events but prompt/post/upload/command/thread frames are rejected.

## 7. Web action model

### 7.1 Actions

```ts
type ClientAction =
  | { type: "hello"; proto: 1; displayName: string; writeToken?: string }
  | { type: "send_agent_prompt"; text: string; attachments?: AttachmentRef[]; targetThreadID?: string }
  | { type: "post_channel_message"; text: string; attachments?: AttachmentRef[]; targetThreadID?: string; allowedMentions: AllowedMentionSelection }
  | { type: "run_bot_command"; command: string; args: Record<string, unknown>; targetThreadID?: string }
  | { type: "create_thread"; sourceMessageID?: string; name: string; autoArchiveDuration?: number }
  | { type: "select_thread"; threadID: string }
  | { type: "interrupt_agent"; jobID?: string }
  | { type: "upload_init"; name: string; mime: string; size: number; sha256?: string }
  | { type: "upload_chunk"; uploadID: string; seq: number; bytes: string }
  | { type: "upload_finish"; uploadID: string }
  | { type: "fetch_discord_attachment"; attachmentRef: string }
  | { type: "ack"; eventID: string };
```

### 7.2 Server events

```ts
type ServerEvent =
  | { type: "welcome"; share: ShareView; target: TargetView; opener: ActorView; capabilities: Capabilities }
  | { type: "channel_event"; event: SanitizedDiscordEvent }
  | { type: "thread_event"; event: SanitizedThreadEvent }
  | { type: "agent_event"; event: SanitizedAgentEvent }
  | { type: "command_result"; requestID: string; status: "ok" | "error"; content: string; visibility: "web" | "discord" | "both" }
  | { type: "upload_state"; uploadID: string; status: "accepted" | "complete" | "rejected"; reason?: string }
  | { type: "attachment_stream"; streamID: string; metadata: AttachmentMetadata; chunk?: string; done?: boolean }
  | { type: "notice"; level: "info" | "warn"; messageKey: string; args?: string[] }
  | { type: "error"; code: string; messageKey: string; args?: string[] }
  | { type: "bye"; reasonCode: string };
```

Sanitized `channel_event` / `thread_event` payloads expose Discord attachments only as bot-issued opaque `attachmentRef` values. A ref embeds or indexes share id, target id, message id, attachment id, filename, size, and expiry in bot-owned state. Browser never supplies channel/thread/message authority beyond this ref.

## 8. Command parity

### 8.1 Do not simulate Discord events blindly

Web action must not be converted into fake `discordgo.MessageCreate` or fake `InteractionCreate`. That would confuse event ids, Discord response semantics, and audit.

Instead add a command bridge:

```go
type DelegatedCommandRequest struct {
    ShareID         string
    GuildID         string
    ChannelID       string
    TargetID        string
    InThread        bool
    OpenerUserID    string
    OpenerUsername  string
    RemoteActorName string
    Command         string
    Args            map[string]any
}
```

The bridge builds a `cmdCtx` with:

- `source = "webshare"`
- `userID = opener_user_id`
- `username = opener_username + " via WebShare/" + remote_actor_name`
- reply function that returns `command_result` to web and optionally posts visible output to Discord depending on command visibility.

`remote_actor_name` is browser-supplied display text, not authority. Normalize before storage, audit, or display: trim, length-cap, strip control characters, neutralize Discord mention syntax, and pass through existing secret redaction. Empty result falls back to `web`.

### 8.2 Permission checks stay real

Every command path must still call existing permission helpers, using opener user id and target id. No command may accept remote actor self-asserted guild/channel/permission.

### 8.3 Slash-equivalent mapping

The web UI should present command forms for known slash commands instead of requiring raw slash syntax. Internally map form input to existing command handlers or shared command service.

Bang text input can be supported for parity, but parsing result must route through the same command bridge and audit as `run_bot_command`.

### 8.4 Commands that need Discord interaction-only UI

Some commands currently depend on ephemeral interaction responses, modals, or components. First version must either:

- provide an equivalent web form/result renderer, or
- return an explicit unsupported-in-web error and list that command in this plan's unsupported table.

Because the requested first version is complete channel parity, unsupported commands should be treated as blockers during implementation, not silently skipped.

## 9. Mentions and bot tagging

### 9.1 Mention composer

Web UI should not allow arbitrary raw `<@id>` spam by default. It should build mentions from a bot-provided target-scoped roster:

```ts
type Mentionable = {
  kind: "user" | "bot";
  id: string;
  displayName: string;
  mention: string;
  source: "recent" | "discord_state" | "discord_search";
};
```

Source order:

1. Current target recent messages/audit snippets.
2. Discord session state cache.
3. Discord member search API.
4. Explicit raw ID only if opener has manage permission and UI marks it as advanced.

Current bot gateway intents include guild messages, direct messages, reactions, message content, and guilds; not guild members. Therefore full member autocomplete may require Discord REST member search and may degrade if guild/member permissions/intents are unavailable.

### 9.2 AllowedMentions policy

Unlike normal safe egress, this feature intentionally allows selected mentions. Therefore web-originated channel messages must build explicit `AllowedMentions` from UI-selected ids only:

```go
AllowedMentions.Parse = nil
AllowedMentions.Users = selectedUserIDs
AllowedMentions.Roles = nil
AllowedMentions.RepliedUser = false
```

Never use parse-all mentions. `@everyone` and `@here` disabled unless a separate explicit capability is added; not in v1.

### 9.3 Tagging the bot

If web message mentions the bot or uses a bot command, do not rely on Discord seeing its own bot-authored message. Route directly through `run_bot_command` / `send_agent_prompt` path, and optionally mirror a visible `Alice via WebShare` message to Discord.

## 10. Message posting parity

### 10.1 `post_channel_message`

Flow:

```text
browser -> relay -> bot decrypt -> permission check -> sanitize -> Discord send -> audit -> echo event to web
```

Discord output:

```text
**Alice via WebShare**: <message>
```

Rules:

- target must be bound channel/thread or a web-created/selected thread under bound parent.
- content goes through secret redaction before Discord send.
- message length uses existing split helpers.
- mentions are explicit selected ids only.
- attachments are uploaded via Discord file send only after sanitizer/scope checks.

### 10.2 Webhook display optional later

Webhook mode is not first implementation default. If added:

- create/manage webhook only after opener/manage permission check.
- name always includes `via WebShare`.
- outbound webhook messages must be marked in memory/audit so `handleMessage` ignores them as self-generated webshare display.
- do not weaken existing `/webhook mode` inbound policy.

## 11. Thread parity

Supported actions:

- create new thread from channel.
- create thread from a specific source message if provided and accessible.
- select existing managed thread under parent channel.
- post/prompt inside selected thread.
- observe thread events.

Implementation constraints:

- Do not fake a `MessageID` for web prompt.
- For channel target with no source message, use Discord thread start API that does not require a message id.
- For source-message thread creation, validate message belongs to bound target channel and is visible to bot.
- Manager/session target mapping remains authoritative.

## 12. Attachments

### 12.1 Web upload

Storage path under validated project CWD:

```text
<projectCWD>/.kiro-bot/attachments/webshare-<shareID>/<uploadID>/<safeName>
```

Rules:

- validate CWD through Manager path helpers.
- sanitize filename using shared helper.
- size and count limits enforced before allocation where possible.
- chunk streaming; do not hold whole large file in memory.
- compute SHA-256 while writing.
- attach manifest to prompt with origin `webshare`.
- heartbeat cleanup must prune webshare upload roots.

### 12.2 Discord attachment fetch

Flow:

```text
browser requests bot-issued attachmentRef
  -> bot resolves ref to share/target/message/attachment metadata
  -> bot checks opener still has permission
  -> bot fetches/downloads via Discord URL/API if available
  -> bot streams encrypted chunks to browser or saves as prompt attachment
```

Rules:

- browser may request only opaque `attachmentRef` values previously emitted by sanitized target events.
- message must be in bound channel/thread or selected child thread.
- never expose Discord CDN URL if it includes sensitive query/signature.
- never expose local storage path.
- audit every fetch with message id, attachment id, filename, byte count, result.
- respect Discord CDN expiry/failure; return localized error.

## 13. Opener lockout

### 13.1 Lock key

```text
primary lock: guild_id + target_type + target_id + opener_user_id
coverage: primary target plus every WebShare-created/selected child thread under that target
```

Discord-side lockout checks must normalize incoming message/command targets through parent/child relationships. A parent-channel share covers the parent channel and any child thread selected or created through that share; otherwise the opener could keep controlling the same delegated session from Discord inside a child thread.

### 13.2 Denied on Discord side while active

When active/connecting/disconnected share exists, opener cannot from Discord invoke:

- normal mention prompt.
- bang prompt.
- slash-equivalent agent commands.
- lifecycle commands that affect current agent/session/runtime: interrupt, cancel, clear, restart, model, engine, cwd, mcp, memory, skill mutation, steering mutation, webhook mode changes, cron agent prompt.

Allowed:

- `/webshare status`
- `/webshare stop`
- emergency admin revoke by other managers
- non-agent informational commands only if explicitly whitelisted

### 13.3 Web side remains active

The same actions are allowed from web because web is the delegated control channel. This intentionally makes web the single control plane for opener until stop.

## 14. Bot tools target state

Extend dynamic state:

```go
type TargetState struct {
    // existing fields
    RequesterID       string
    RequesterName     string
    RequestSource     string

    // new webshare fields
    WebShareID        string
    DelegatedSession  bool
    DelegatedByUserID string
    DelegatedByName   string
    RemoteActorName   string
}
```

Permission booleans must be computed from opener's current Discord permissions at action time, not from link payload.

Because user requested full channel parity, web command bridge may expose manager-level command capability when opener still has it. This is high risk; audit and lockout are mandatory safeguards.

## 15. Audit events

Required event types:

```text
webshare_started
webshare_host_connected
webshare_peer_joined
webshare_peer_left
webshare_action_allowed
webshare_action_rejected
webshare_prompt_enqueued
webshare_command_invoked
webshare_command_completed
webshare_message_posted
webshare_thread_created
webshare_upload_started
webshare_upload_completed
webshare_attachment_fetched
webshare_interrupted
webshare_disconnected
webshare_revoked
```

Required metadata:

```json
{
  "share_id": "ws_...",
  "guild_id": "...",
  "target_type": "channel",
  "target_id": "...",
  "parent_channel_id": "...",
  "opener_user_id": "...",
  "opener_username": "...",
  "remote_actor_name": "browser-name",
  "action": "post_channel_message",
  "capability": "post_message",
  "allowed": true,
  "reason_code": "",
  "relay_origin": "https://relay.example",
  "token_fingerprint": "sha256:abcd1234",
  "attachment_count": 1,
  "byte_count": 12345
}
```

Never log:

- full share URL.
- URL fragment.
- room key.
- write token.
- raw local path.
- Discord CDN signed URL.
- unredacted file content.

## 16. Security review built into design

### 16.1 Threats

| Threat | Control |
|---|---|
| Link leak gives attacker opener capability | explicit warning, audit, opener lockout, revoke, token fingerprint, no TTL but visible status |
| Relay operator reads content | E2E encrypted frames; relay sees only opaque bytes |
| nginx/access log leaks secret | key/token in fragment only; host token in Authorization header |
| Browser arbitrary mentions spam channel | explicit AllowedMentions allowlist from selected ids; no parse-all; no everyone/here |
| Webhook/display identity spoofing | default bot attribution; webhook later must include `via WebShare` |
| Web command bypasses permissions | command bridge uses opener id + existing permission helpers every action |
| Attachment exfiltration | target scope validation, permission recheck, no CDN/local path exposure, audit |
| Local path traversal | safe filename + validated CWD + uploadID directory |
| MCP/CWD/tool policy bypass | web commands call same Manager/helper paths; no direct ACP/control |
| Orphan lock after relay outage | `/webshare stop` always available; manager revoke; status shows disconnected |
| Replay or frame injection | AEAD associated data + sequence numbers + write token proof |
| DoS via frames/uploads | relay and bot size/rate/peer limits; streaming writes; no unbounded buffering |

### 16.2 Strict invariants

1. No code path may accept guild/channel/user id from browser as authority.
2. No web action may skip opener permission recheck.
3. No raw secret/link/local path may enter audit/log/Discord output.
4. No web-originated Discord message may use parse-all mentions.
5. No fake Discord event may be injected into `handleMessage` as if Discord emitted it.
6. No direct ACP call from relay/web layer.
7. No attachment fetch outside bound target or selected child thread.
8. No webhook integration may weaken existing `/webhook mode` inbound guard.

## 17. Implementation milestones

### Milestone 1 — Relay skeleton and deployment

Deliverables:

- `cmd/webshare-relay` Go binary.
- static asset serving.
- `/healthz`.
- `/r/<room>?role=host|guest` WebSocket endpoint.
- host bearer auth.
- room host/guest lifecycle.
- nginx/systemd/docker docs.

Verification:

- relay unit tests for first host, second host rejection, guest missing room, host disconnect closing guests.
- integration test using in-process `httptest` WebSocket clients.

### Milestone 2 — Bot store, commands, lockout

Deliverables:

- SQLite store/migrations.
- `/webshare start|stop|status|revoke`.
- locale keys en/zh-TW.
- opener lockout in message and slash paths.
- relay host client config.

Verification:

- command permission tests.
- active share unique index test.
- lockout denial tests for prompt and command paths.
- stop/revoke releases lock.

### Milestone 3 — Encrypted protocol

Deliverables:

- room key/write token generation.
- link parser/formatter.
- browser WebCrypto implementation.
- Go seal/open implementation.
- sequence replay protection.
- view/write role enforcement.

Verification:

- golden vectors shared by Go and TS.
- wrong token rejected.
- view link cannot mutate.
- replayed frame rejected.
- logs contain no key/token/link fragment.

### Milestone 4 — Prompt and live agent output

Deliverables:

- `send_agent_prompt` action.
- `Source="webshare"` jobs.
- web event stream for queue/running/final/error.
- thread-safe no-MessageID path.

Verification:

- focused `channel` tests for webshare enqueue.
- worker path test proving no fake Discord message id.
- audit event test.

### Milestone 5 — Command parity

Deliverables:

- web command bridge.
- slash-equivalent web forms.
- bang text parser support where safe.
- command result renderer in web UI.

Verification:

- each command class test: info, agent-affecting, admin-affecting, denied/allowed.
- permission recheck test with opener losing permission.
- unsupported interaction-only command inventory must be empty or explicitly implemented in web form.

### Milestone 6 — Channel message posting and mentions

Deliverables:

- `post_channel_message`.
- explicit mention autocomplete.
- allowed mentions builder.
- bot-tag command routing.

Verification:

- selected user mention allowed.
- unselected raw mention not pinged.
- everyone/here not pinged.
- bot mention routes internally, not through self-authored Discord event.
- split/redaction tests.

### Milestone 7 — Thread parity

Deliverables:

- create thread from channel.
- create thread from source message.
- select existing child thread.
- post/prompt within thread.

Verification:

- source message scope validation.
- channel thread create uses correct Discord API path.
- selected thread belongs to parent target.
- session/CWD inheritance test.

### Milestone 8 — Upload and Discord attachment fetch

Deliverables:

- chunked upload.
- sanitized storage.
- prompt attachment manifest.
- scoped Discord attachment fetch.
- cleanup integration.

Verification:

- traversal filename rejected/sanitized.
- oversize upload rejected before full buffering.
- wrong-channel attachment fetch rejected.
- cleanup prunes webshare upload dirs.
- no local path in web/Discord/audit output.

### Milestone 9 — Full system smoke and docs

Deliverables:

- docs-site user/admin guide.
- deployment guide.
- steering decision record.
- release/preflight update if needed.

Verification:

- self-host relay behind local nginx or equivalent reverse proxy smoke.
- bot start share -> browser connect -> post message with mention -> prompt agent -> create thread -> upload file -> fetch attachment -> stop share.
- `go test ./webshare ./internal/websharerelay ./bot ./channel`.
- web client typecheck/build.
- docs-site verify if docs-site changed.

## 18. Pre-implementation checks resolved

These checks were completed before implementation planning handoff. They are now implementation facts, not product blockers.

1. **Discord thread APIs are available in current dependency**: `go.mod` pins `github.com/bwmarrin/discordgo v0.29.0`; `Session.ThreadStart`, `Session.ThreadStartComplex`, `Session.MessageThreadStart`, and `ThreadStart` are present. WebShare can create a thread without a source message via `ThreadStart`/`ThreadStartComplex`, and can create source-message threads via `MessageThreadStart`.
2. **Discord message posting with explicit mentions is available**: `Session.ChannelMessageSendComplex` accepts `MessageSend`, and `MessageSend.AllowedMentions` supports explicit `Users`, explicit `Roles`, and zero-value no-parse behavior. V1 must set `Parse=nil`, selected `Users`, `Roles=nil`, and `RepliedUser=false`.
3. **Member lookup can support mention autocomplete with graceful degradation**: the bot gateway intents currently include guild messages, direct messages, reactions, message content, and guilds, but not guild members. `discordgo.Session.GuildMembersSearch` and `GuildMembers` exist; existing MCP mention resolution already uses fresh search, bounded member scan, then state cache. WebShare should extract a shared mention lookup service from that logic and degrade to recent/cache-only if Discord denies member search.
4. **Command parity requires a bridge, not fake interactions**: slash commands are registered in `buildSlashCommandsWithA2A`; dispatch splits between immediate interaction-only paths (`cron`, `cron-list`, `cron-run`, `cron-prompt`, `remind`, `usage-history`, `cwd`, `steering`) and deferred handler paths (`start`, `reset`, `restart`, `help`, `status`, `usage`, `doctor`, `audit`, `mcp`, `skill`, `a2a`, `cancel`, `interrupt`, `compact`, `clear`, `pause`, `back`, `silent`, `thread`, `webhook`, `model`, `models`, `agent`, `engine`, `memory`, `flashmemory`, `close`, `close-thread`, `resume`, `session`). WebShare must implement web-native equivalents for modal/component commands instead of constructing fake `InteractionCreate`.
5. **Config loader pattern is straightforward**: top-level `Config` in `config.go` reads env through `envOr`, `envInt`, `envBool`; `main.go` maps those fields into `bot.BotConfig` and `channel.ManagerConfig`; `bot.NewFromConfig` normalizes `DataDir`, opens stores, registers heartbeat tasks, and installs Discord handlers. WebShare config should follow this same path and update `.env.example` plus docs-site environment docs.
6. **Audit schema can carry WebShare metadata**: `audit.BotEvent` already has `Metadata map[string]any`; `Recorder.RecordBotEvent` queues bot events; audit store records projected bot events and raw JSON with content redaction behavior. WebShare can record new bot event types without a separate audit database, but must keep full links, key material, CDN signed URLs, and raw local paths out of metadata.
7. **Attachment/safe-egress extension points exist**: current Discord attachment download uses project CWD `.kiro-bot/attachments/<messageID>/...`, `safeAttachmentFilename`, size checks, and manifest injection. Cleanup already scans project `.kiro-bot/attachments` roots from sessions. Safe file egress uses `internal/botegress.PrepareSanitizedFile`. WebShare should extract shared attachment helpers rather than duplicate them in `bot/handler.go`.
8. **Docs-site must be updated with implementation**: `docs-site/docs/guide/docs-maintenance.md` makes `docs-site/docs/` the canonical long-form user/admin/operator surface. A WebShare implementation changes slash commands, privacy/security behavior, environment variables, and deployment, so the implementation PR must update English and Traditional Chinese docs-site pages and run `cd docs-site && npm run verify`.

## 19. Documentation surface

Canonical end-user and operator docs now live in:

- `docs-site/docs/guide/webshare.md`
- `docs-site/docs/zh-TW/guide/webshare.md`
- `docs-site/docs/guide/commands.md`
- `docs-site/docs/zh-TW/guide/commands.md`
- `docs-site/docs/guide/environment.md`
- `docs-site/docs/zh-TW/guide/environment.md`
- `docs-site/docs/guide/deployment.md`
- `docs-site/docs/zh-TW/guide/deployment.md`
- `docs-site/docs/guide/security-model.md`
- `docs-site/docs/zh-TW/guide/security-model.md`
- `docs-site/docs/guide/audit-usage-privacy.md`
- `docs-site/docs/zh-TW/guide/audit-usage-privacy.md`

They are linked from the English/Traditional Chinese docs home pages, `docs-site/scripts/build.mjs` navigation arrays, and both READMEs.

Keep these pages synchronized with the implementation facts in this plan: pure Go/self-host relay, TypeScript SPA, bot outbound host connection, URL-fragment link secrets, explicit `via WebShare` display, opener lockout, selected-user/bot mentions only, no roles/everyone/here, scoped attachments, managed thread behavior, and stop/revoke semantics.

## 20. Strict review checklist

Before implementation PR can merge:

- [ ] Relay is pure Go/self-host; no Cloudflare runtime dependency.
- [ ] nginx reverse proxy WebSocket settings documented and smoke-tested.
- [ ] Bot connects outbound as host; no inbound bot port.
- [ ] Relay cannot read plaintext frame payloads.
- [ ] Link secrets are only in URL fragment.
- [ ] Host token is not in query string.
- [ ] Opener Discord lockout covers prompt and command paths.
- [ ] Web command bridge does not fake Discord gateway events.
- [ ] Web actions recheck opener permissions every time.
- [ ] Full channel parity actions have tests: prompt, command, post, mention, thread, upload, fetch attachment.
- [ ] Selected mentions use explicit `AllowedMentions`, never parse-all.
- [ ] `@everyone`/`@here` disabled in v1.
- [ ] Web-originated output is visibly marked `via WebShare`.
- [ ] Audit has opener + remote actor + share id for every mutation.
- [ ] Audit/log never includes full link/key/token/local path/CDN signed URL.
- [ ] Attachment fetch is target-scoped.
- [ ] Upload path is under validated project CWD and cleanup-managed.
- [ ] MCP policy and CWD validation are not bypassed.
- [ ] Existing `/webhook mode` behavior is unchanged.
- [ ] Locale keys are parity-checked.
- [ ] Browser UI warns that control link delegates opener capability until stopped.
