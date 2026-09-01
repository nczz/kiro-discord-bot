# WebShare

WebShare lets a channel or thread manager open a browser control surface for one Discord target. The browser connects to a self-hosted relay while the bot stays outbound-only, and every write action is treated as a delegated action from the Discord user who opened the share.

Use WebShare when an operator needs a temporary web UI for the same project-bound agent session that normally lives in Discord. Do not use it as a public support portal, cross-guild remote console, or replacement for Discord permissions.

## Product Semantics

A share is scoped to one target:

```text
guild_id + channel_or_thread_id + opener_user_id
```

The control link holder can act within that target until the share is stopped or revoked. Actions still flow through the bot's normal channel and thread manager, so CWD validation, MCP policy, safe output handling, attachment checks, audit, and usage attribution remain in force.

The relay is intentionally content-blind. It serves the TypeScript single-page web app and routes opaque WebSocket frames. The bot and browser encrypt application payloads end-to-end; the relay sees room IDs, peer IDs, roles, frame sizes, connection metadata, and logs, but not prompts, messages, attachments, command arguments, or agent output.

## Delegated Identity

Discord does not allow a bot to send messages as a real user account. WebShare therefore never uses user tokens, selfbot behavior, or fake Discord gateway events.

When the browser posts a normal Discord message, the bot sends it through a bot-owned Discord webhook whose username is:

```text
Alice via WebShare
```

The message body stays as the user's text, without an extra `via WebShare` prefix inside the content. This display mode requires the bot to have Discord **Manage Webhooks** permission in the parent channel; `/webshare start` rejects shares when that permission is missing. The browser UI also shows the opener and target so the link holder understands whose Discord authority is being delegated.

## Link Risk

The control link is a capability. Anyone who receives it can operate as the opener within the shared target until the share ends. Treat it like a temporary secret:

- Do not paste the full URL in Discord, issue trackers, logs, screenshots, or support tickets.
- Prefer opening it in a browser profile controlled by the intended operator.
- Stop the share when handoff is complete.
- Revoke a share immediately if the link may have leaked.

The secret is carried in the URL fragment, for example `https://relay.example/#/join/<room>.<secret>`. Browser fragments are not sent in HTTP requests, so nginx and the relay should not log the room key or write token. The relay WebSocket query string may still include non-secret routing fields such as `role`; never put tokens in a query string.

## Commands

| Command | Purpose |
| --- | --- |
| `/webshare start` | Create or reconnect a WebShare for the current channel or thread and return private control/view links to the opener. |
| `/webshare status` | Privately show the active share state, relay connection, opener, target, and stop/revoke guidance. |
| `/webshare stop` | Stop the opener's active share for the current target and disconnect browser peers. |
| `/webshare revoke` | Emergency-revoke a share for the current target when the requester has channel management authority. |

A user can have only one active share for the same target. While a share is active, the opener is locked out of direct Discord prompt and bot-command paths for that target. They must use the browser link or `/webshare stop`. Other authorized managers can still revoke a share if the opener is unavailable.

## Browser Actions

A write-capable WebShare can:

- send agent prompts against the target's existing channel or thread session;
- run supported bot commands through a WebShare command bridge;
- post normal channel or thread messages marked `via WebShare`;
- interrupt or cancel the current agent job;
- create a managed child thread from a parent channel or a source message;
- switch to an existing managed child thread within the shared target;
- upload files for the agent; and
- fetch in-scope Discord attachments through bot-issued attachment references.

The browser does not gain access to arbitrary Discord channels, raw local paths, or direct ACP sessions. Each action rechecks the opener's Discord access and channel management state. If the opener loses access or management authority, new actions are rejected and the share becomes degraded or revoked.

## Mentions

WebShare v1 supports only explicit selected user mentions plus the bot mention. It does not support role mentions, `@everyone`, or `@here`.

The browser composer should use the WebShare mention picker rather than raw mention text. The bot sends Discord messages with explicit allowed-mention lists: selected user IDs may ping, roles are empty, parse-all is disabled, and replied-user pings are disabled. If member lookup is unavailable, autocomplete degrades to cached/recent users instead of broad parsing.

## Attachments

Uploads and Discord attachment fetches stay target-scoped:

- Browser uploads are chunked, size-limited, sanitized, and stored under the validated project CWD in `.kiro-bot/attachments/webshare-<share>/<upload>/`.
- Discord attachments are exposed to the browser only through opaque bot-issued refs tied to the share, target, message, attachment, filename, size, and expiry.
- The browser cannot ask for a random channel/message/attachment by ID.
- Safe egress and redaction rules still apply before local files or fetched content are shown back to Discord or the web UI.
- Cleanup follows the normal attachment retention settings.

## Thread Behavior

Starting WebShare inside a thread scopes the share to that thread. Agent prompts, bot commands, messages, uploads, and attachment refs use the thread's existing session/CWD behavior.

Starting WebShare in a parent channel scopes the share to the parent channel. The browser may create managed child threads or select an existing managed child thread that belongs to the parent target. Thread operations use Discord's normal thread APIs and the bot's channel manager; WebShare does not create a separate session model.

## Relay Deployment

The relay is a pure Go self-hosted binary. It can run on the same host as the bot, on another VM, or in a container behind nginx, Caddy, or Traefik. The bot connects outbound as the authenticated room host, so the bot process does not need an inbound port.

Build or install the relay binary:

```bash
go build -o webshare-relay ./cmd/webshare-relay
```

Generate a host token and store it outside the repository:

```bash
install -d -m 0750 /etc/kdb-webshare
openssl rand -base64 32 > /etc/kdb-webshare/host-token
chmod 0640 /etc/kdb-webshare/host-token
```

Relay environment:

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

Bot environment:

```env
WEBSHARE_ENABLED=true
WEBSHARE_RELAY_URL=wss://relay.example
WEBSHARE_PUBLIC_BASE_URL=https://relay.example
WEBSHARE_HOST_TOKEN_FILE=/etc/kdb-webshare/host-token
WEBSHARE_MAX_FRAME_BYTES=4194304
WEBSHARE_RECONNECT_INITIAL_MS=1000
WEBSHARE_RECONNECT_MAX_MS=30000
```

Use `WEBSHARE_HOST_TOKEN` only for local development or secret-manager injection. Prefer `WEBSHARE_HOST_TOKEN_FILE` and `RELAY_HOST_TOKEN_FILE` in production so tokens do not appear in shell history, process listings, or Compose files.

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

Use a long WebSocket read timeout. If you run more than one relay instance, route all sockets for the same room to the same instance; the first version assumes one relay process per public hostname.

### systemd Example

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

### Docker Compose Example

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

## Stop and Revoke

Use `/webshare stop` for normal handoff completion. It closes the share, disconnects browsers, clears the opener lockout for the target, and prevents future actions with the same link.

Use `/webshare revoke` when a link may have leaked, the opener is unavailable, or another manager needs to end the share. Revocation is recorded in audit metadata without storing the full link, secret, token, local raw paths, or CDN signed URLs.
