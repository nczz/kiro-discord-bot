# macOS MCP Networking

The canonical macOS MCP networking runbook now lives on the static documentation site: [macOS MCP Networking][macos-networking].

## Short Checklist

Use this when a private LAN MCP endpoint works from an interactive shell but fails from a macOS LaunchAgent with errors such as `no route to host`.

1. Verify shell reachability with `curl`.
2. Check route selection with `route -n get <ip>`.
3. Check system proxy settings with `scutil --proxy`.
4. Inspect the LaunchAgent environment without printing secrets.
5. Prefer fixing `NO_PROXY`, Local Network permission, service identity, or a stable wrapper.
6. Use a relay only as an explicit deployment fallback.

The bot should use standard MCP transports; host networking issues should be solved at the deployment layer first.

[macos-networking]: https://nczz.github.io/kiro-discord-bot/guide/macos-mcp-networking.html
