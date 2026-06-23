# macOS MCP Networking

This note covers macOS deployments where the bot runs as a `launchd`
LaunchAgent and a URL-based MCP server is reachable from an interactive shell
but fails from `/mcp manage` or agent startup with errors such as:

```text
dial tcp 192.168.x.y:8000: connect: no route to host
```

## Design Boundary

The bot does not work around host networking policy inside MCP transport code.
URL-based MCP servers use standard MCP transports:

- Streamable HTTP POST with `Accept: application/json, text/event-stream`.
- `Mcp-Session-Id` persistence across Streamable HTTP requests.
- SSE endpoint support for URL paths ending in `/sse`.
- The same `mcp-proxy` path for agent runtime and `/mcp manage` tool discovery.

If private LAN access fails only under macOS `launchd`, fix the macOS service
environment first. Use a relay only as an explicit deployment fallback when the
host policy cannot be changed.

## Quick Diagnosis

Run these on the macOS host. Replace the URL with the affected MCP endpoint.

```bash
URL="http://192.168.x.y:8000/mcp"

# 1. Verify normal shell reachability.
curl -sS --max-time 5 \
  -H "Accept: application/json, text/event-stream" \
  -H "Content-Type: application/json" \
  --data-binary '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"curl-smoke","version":"0"}}}' \
  "$URL"

# 2. Check route and local interface selection.
route -n get 192.168.x.y
netstat -rn -f inet

# 3. Confirm the bot process proxy environment without printing secrets.
pid=$(pgrep -f '/kiro-discord-bot$' | head -1)
ps eww -p "$pid" | tr ' ' '\n' | grep -Ei '^(https?_proxy|all_proxy|no_proxy|NO_PROXY|HTTP_PROXY|HTTPS_PROXY|ALL_PROXY)=' || true
```

To test the exact MCP proxy path used by the bot:

```bash
tmp=/tmp/kiro-mcp-smoke.jsonl
printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"mcp-proxy-smoke","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' > "$tmp"

NO_PROXY='*' no_proxy='*' \
MCP_PROXY_URL="$URL" \
MCP_PROXY_ALLOW_ALL_TOOLS=true \
/path/to/kiro-discord-bot mcp-proxy < "$tmp"
```

If the shell smoke succeeds but `/mcp manage` fails, create a one-shot
LaunchAgent probe. This verifies whether the failure belongs to the launchd
context instead of Discord or MCP policy:

```bash
uid=$(id -u)
plist=/tmp/com.kiro.discord-bot.mcp-probe.plist
cat > "$plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.kiro.discord-bot.mcp-probe</string>
<key>ProgramArguments</key><array>
  <string>/bin/zsh</string>
  <string>-lc</string>
  <string>NO_PROXY='*' no_proxy='*' MCP_PROXY_URL='$URL' MCP_PROXY_ALLOW_ALL_TOOLS=true /path/to/kiro-discord-bot mcp-proxy &lt; /tmp/kiro-mcp-smoke.jsonl</string>
</array>
<key>StandardOutPath</key><string>/tmp/kiro-mcp-probe.out</string>
<key>StandardErrorPath</key><string>/tmp/kiro-mcp-probe.err</string>
<key>RunAtLoad</key><true/>
</dict></plist>
PLIST

launchctl bootout "gui/$uid/com.kiro.discord-bot.mcp-probe" >/dev/null 2>&1 || true
rm -f /tmp/kiro-mcp-probe.out /tmp/kiro-mcp-probe.err
launchctl bootstrap "gui/$uid" "$plist"
sleep 5
launchctl bootout "gui/$uid/com.kiro.discord-bot.mcp-probe" >/dev/null 2>&1 || true
cat /tmp/kiro-mcp-probe.out
cat /tmp/kiro-mcp-probe.err
```

## Preferred Fixes

Use the first option that fits the host policy.

### Stable App Wrapper

Grant the launched bot a stable macOS identity and Local Network access.
Package the bot in a signed app/helper wrapper with a stable bundle identifier
and an `Info.plist` containing `NSLocalNetworkUsageDescription`. Start that
wrapper from the LaunchAgent so macOS can show and persist Local Network
permission for the bot in System Settings.

Example app wrapper:

```bash
APP=/Applications/KiroDiscordBot.app
INSTALL_DIR=/Users/knockersadmin/kiro-discord-bot

mkdir -p "$APP/Contents/MacOS"
cp "$INSTALL_DIR/kiro-discord-bot" "$APP/Contents/MacOS/kiro-discord-bot"
cat > "$APP/Contents/MacOS/KiroDiscordBot" <<'SCRIPT'
#!/bin/zsh
set -euo pipefail
cd /Users/knockersadmin/kiro-discord-bot
set -a
[ -f .env ] && . ./.env
set +a
exec /Applications/KiroDiscordBot.app/Contents/MacOS/kiro-discord-bot "$@"
SCRIPT
chmod +x "$APP/Contents/MacOS/KiroDiscordBot" "$APP/Contents/MacOS/kiro-discord-bot"
```

Minimal `Info.plist`:

```bash
cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key>
  <string>tw.mxp.kiro-discord-bot.adam</string>
  <key>CFBundleName</key>
  <string>KiroDiscordBot</string>
  <key>CFBundleExecutable</key>
  <string>KiroDiscordBot</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>NSLocalNetworkUsageDescription</key>
  <string>Kiro Discord Bot needs local network access to reach private MCP servers.</string>
</dict>
</plist>
PLIST
```

Sign the wrapper. Ad-hoc signing is enough for local deployment; use a
Developer ID identity for managed distribution:

```bash
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"
```

Point the LaunchAgent at the wrapper executable:

```xml
<key>ProgramArguments</key>
<array>
  <string>/Applications/KiroDiscordBot.app/Contents/MacOS/KiroDiscordBot</string>
</array>
```

Restart the LaunchAgent, then trigger a private-LAN MCP scan from Discord:

```bash
launchctl kickstart -k "gui/$(id -u)/<launchd-label>"
```

macOS should show a Local Network prompt for `KiroDiscordBot`. Allow it, then
verify it is enabled in **System Settings > Privacy & Security > Local Network**
and rerun `/mcp manage` scan.

If the prompt does not appear, start the app once from the GUI session and
trigger the scan again:

```bash
open -n /Applications/KiroDiscordBot.app
```

If access was previously denied, reset only this app's Local Network TCC record
and trigger the prompt again:

```bash
tccutil reset LocalNetwork tw.mxp.kiro-discord-bot.adam
```

### Managed Service Context

Run the bot under a service context that is allowed to reach the LAN. On managed
hosts, a LaunchDaemon or MDM-approved service profile may be more appropriate
than a user LaunchAgent. This requires administrator control and should be
documented per host.

### Proxy Environment

Fix proxy environment only when the failure is actually proxy related. If logs
show requests going through a proxy, add both uppercase and lowercase no-proxy
variables to the LaunchAgent:

```xml
<key>EnvironmentVariables</key>
<dict>
  <key>NO_PROXY</key><string>localhost,127.0.0.1,::1,192.168.0.0/16,10.0.0.0/8,172.16.0.0/12</string>
  <key>no_proxy</key><string>localhost,127.0.0.1,::1,192.168.0.0/16,10.0.0.0/8,172.16.0.0/12</string>
</dict>
```

Restart the service after changing the plist:

```bash
launchctl kickstart -k "gui/$(id -u)/<launchd-label>"
```

## Relay Fallback

Use a relay only when the host's Local Network policy cannot be fixed. Keep it
explicit in the MCP catalog URL so operators know the deployment is routing
through a local bridge.

Example with `socat`:

```bash
socat TCP-LISTEN:18000,bind=127.0.0.1,fork,reuseaddr TCP:192.168.x.y:8000
```

Then configure the MCP server URL as:

```json
{
  "mcpServers": {
    "example-private-mcp": {
      "url": "http://127.0.0.1:18000/mcp"
    }
  }
}
```

Relay fallback should be tracked as operational debt and revisited when the
macOS service identity or network policy can be corrected.
