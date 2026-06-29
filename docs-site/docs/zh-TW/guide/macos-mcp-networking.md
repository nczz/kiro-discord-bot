# macOS MCP 網路

這份 runbook 處理 macOS LaunchAgent 部署中，URL-based MCP server 在互動 shell 可連，但 `/mcp manage` scan 從 bot 執行時出現 `no route to host` 的情境。

## 設計邊界

bot 應使用標準 MCP transports，不應在 MCP transport code 內加入隱藏 network workaround。請先修 host/service environment。只有在 macOS service context 無法直連 private endpoint 時，才把 relay 當成明確 deployment fallback。

## 快速診斷

在 macOS host 執行：

```bash
URL=http://192.168.169.21:8000/mcp
curl -v "$URL"
route -n get 192.168.169.21
scutil --proxy
```

再檢查 service environment，但不要印出 secrets：

```bash
launchctl print gui/$(id -u)/your.service.label
```

注意 proxy variables，以及 interactive shell 與 LaunchAgent 的 Local Network permission 差異。

## 測試 Bot MCP Path

用與 bot 相同的 binary 與 environment shape：

```bash
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}\n' >/tmp/kiro-mcp-smoke.jsonl

NO_PROXY='*' no_proxy='*' \
MCP_PROXY_URL="$URL" \
MCP_PROXY_HEADERS_JSON='{"Authorization":"Bearer <token>"}' \
MCP_PROXY_ALLOW_ALL_TOOLS=true \
/path/to/kiro-discord-bot mcp-proxy </tmp/kiro-mcp-smoke.jsonl
```

URL server 不需要驗證時可省略 `MCP_PROXY_HEADERS_JSON`。不要把真實 token 留在 shell history 或支援紀錄中。

如果只在 launchd 下失敗，問題在 service context，不是 Discord policy。

## 優先修法

1. 對 private networks 加上 `NO_PROXY` / `no_proxy`，或 service 不需要 HTTP proxy 時使用 `NO_PROXY=*`。
2. 確認 LaunchAgent 使用有 Local Network permission 的同一個 user。
3. 授權 launched binary 或 wrapper macOS Local Network permission。
4. 若 macOS 每次 rebuild 都視為新 app identity，使用穩定 signed wrapper。
5. 重啟 LaunchAgent 並重新執行 `/mcp manage` scan。

## Relay Fallback

如果 macOS service policy 仍阻止 private LAN 直連，請明確使用 relay，並在 MCP catalog URL 中留下可見線索。不要讓 relay route 變成隱形，否則未來 operator 會不知道流量不是直連。

Fallback 應保留 MCP protocol behavior，而不是修改 application logic。
