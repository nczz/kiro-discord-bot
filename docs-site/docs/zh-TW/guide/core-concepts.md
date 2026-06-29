# 核心概念

## Channel Agent

一個 parent Discord channel 對應一個專案工作目錄與一個長生命週期 ACP agent session。Channel agent 擁有主要專案脈絡、memory block、steering 檔案、MCP policy 與對話延續性。

## Thread Agent

預設情況下，parent channel 的新任務會開 Discord thread。每個 thread 會有專屬 agent，帶入原始任務與受限的 thread transcript。Thread agent 和 parent channel agent 彼此獨立，因此 thread 裡的長任務不會阻塞其他 channel 工作。

在 thread 內使用 `/status`、`/reset`、`/cancel`、`/interrupt`、`/compact`、`/clear`、`/model` 時，會優先作用在目前 thread agent。

## Agent Engines

Kiro CLI 是預設 ACP engine。OMP 可以作為替代 engine 啟用，並共用同一套 Discord 指令、MCP policy、audit、usage、cron、memory 與 thread-agent 控制面。

Engine 選擇是目前 channel 或 thread 的 scope。把某個 channel 切到 OMP 不會改變其他 channel；切換某個 thread 也不會反寫 parent channel。完整 scope 與 runtime isolation model 見 [Agent Engines](agent-engines.md)。

## Memory、Flash Memory 與 Steering

三個 context layer 的用途不同：

| Layer | 範圍 | 適合用途 |
| --- | --- | --- |
| `/memory` | 持久 Discord-native 規則 | 個人偏好、回應語言、固定風格 |
| `/flashmemory` | 目前 session 重點 | 當前任務或 sprint 的臨時優先事項 |
| `AGENTS.md` | 版本化專案指引 | 架構、建置指令、安全規則、工作流程、領域背景 |

只要規則還在 `/memory list` 看得到，就會在每次 agent turn 前被注入。移除 memory rule 會停止未來注入，但目前 ACP agent session 可能已經在先前對話看過舊規則。若要完全淘汰過期或衝突規則，移除後再執行 `/clear` 與 `/reset`。

## MCP Policy

bot 會從 Kiro-format settings source 讀取 MCP server 定義作為 catalog，例如 `KIRO_MCP_CONFIG`、`KIRO_HOME/settings/mcp.json` 或 `~/.kiro/settings/mcp.json`，但不會預設暴露給 agent。

Runtime 時，每個 agent 只會透過 ACP `mcpServers` 收到目前 Discord 頻道 policy 允許的 MCP server。bot 會透過 policy proxy 啟動 server，過濾 `tools/list` 並阻擋未授權的 `tools/call`。

## Audit 與私密管理回應

`/mcp manage`、`/cwd`、`/status`、`/usage`、`/doctor`、`/audit`、`/models`、`/memory`、`/flashmemory` 等操作或敏感查詢，會在 Discord 支援時使用 ephemeral private response。Audit prompt 調查的最終報告也會私密回覆。
