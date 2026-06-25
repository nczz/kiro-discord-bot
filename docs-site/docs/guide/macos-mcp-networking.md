# macOS MCP Networking

This runbook covers macOS LaunchAgent deployments where a URL-based MCP server works from an interactive shell but `/mcp manage` scan fails from the bot with errors such as `no route to host`.

## Design Boundary

The bot should use standard MCP transports. It should not add hidden network workarounds inside MCP transport code. Fix the host/service environment first. Use a relay only as an explicit deployment fallback when the macOS service context cannot directly reach the private endpoint.

## Quick Diagnosis

From the macOS host:

```bash
URL=http://192.168.169.21:8000/mcp
curl -v "$URL"
route -n get 192.168.169.21
scutil --proxy
```

Then check the service environment without printing secrets:

```bash
launchctl print gui/$(id -u)/your.service.label
```

Look for proxy variables and local network permission differences between the interactive shell and the LaunchAgent.

## Test the Bot MCP Path

Use the same binary and environment shape as the bot:

```bash
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}\n' >/tmp/kiro-mcp-smoke.jsonl

NO_PROXY='*' no_proxy='*' \
MCP_PROXY_URL="$URL" \
MCP_PROXY_ALLOW_ALL_TOOLS=true \
/path/to/kiro-discord-bot mcp-proxy </tmp/kiro-mcp-smoke.jsonl
```

If this fails only under launchd, the problem is service context, not Discord policy.

## Preferred Fixes

1. Add `NO_PROXY` / `no_proxy` for private networks or use `NO_PROXY=*` when the service does not need an HTTP proxy.
2. Ensure the LaunchAgent runs under the same user that has local network permission.
3. Grant macOS Local Network permission to the launched binary or wrapper.
4. Use a stable signed wrapper if macOS keeps treating each rebuilt binary as a new app identity.
5. Restart the LaunchAgent and re-run `/mcp manage` scan.

## Relay Fallback

If macOS service policy still prevents direct private LAN access, use a relay explicitly and document it in the MCP catalog URL. Avoid making relay use invisible, because future operators need to know traffic is not direct.

The fallback should preserve MCP protocol behavior rather than changing application logic.
