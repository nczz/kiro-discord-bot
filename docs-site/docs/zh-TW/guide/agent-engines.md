# Agent Engines

`kiro-discord-bot` 是給專案型 ACP agents 使用的 Discord control plane。bot 負責 Discord 體驗、channel policy、專案綁定、audit、usage、cron、memory 與 thread lifecycle；agent engine 則負責真正對專案進行推理與工作的 coding-agent session。

Kiro CLI 仍是預設且最完整驗證的 engine。當團隊希望在不改變 Discord 工作流的前提下試用另一個 ACP engine 時，可以啟用 OMP。

## 為什麼 Engine 很重要

多數團隊不需要先理解 protocol 細節。實務上，engine 選擇回答的是比較產品化的問題：

- 這個 Discord channel 要交給哪一個 coding agent？
- 要使用哪一套認證、model catalog 與計價/用量資料？
- 團隊能不能在不重做 Discord 維運流程的前提下試用另一個 agent？

bot 讓這些決策維持在明確 scope，而且可以回復。某個 channel 可以留在 Kiro，另一個 channel 可以使用 OMP；某個 thread 也可以為了單次調查暫時切換 engine。外層控制面維持一致：`/cwd`、`/mcp`、`/status`、`/usage`、`/audit`、cron、memory、steering 與 thread agents。

## Kiro CLI

Kiro CLI 是預設 engine，也是新部署最建議從這裡開始的路徑。它在這個專案中有最完整的實作歷史與 fallback behavior。

適合使用 Kiro 的情境：

- 需要預設、最完整驗證的 production path。
- 希望 `/models` 與 `/model` 有 Kiro model fallback behavior。
- 希望 Kiro runtime settings 隔離在 `DATA_DIR/kiro-agent-runtime`。
- 需要 Kiro metering metadata；engine 有提供時會以 credits 顯示。
- 既有 Kiro workflows 與 MCP catalog sources 需要延續。

Kiro-only 部署請讓 OMP 保持停用。既有安裝升級時不需要新增環境變數。

## OMP

OMP 是可選的替代 ACP engine。當團隊已經在使用 OMP、想評估另一個 ACP-compatible agent，或希望部分 channel 使用另一套 engine profile 時，才適合啟用。

適合使用 OMP 的情境：

- 希望在同一套 Discord 指令與 MCP policy 後方加入第二個 engine。
- 需要由 active ACP session 回報 model 與 mode catalog。
- 希望在 OMP emit `usage_update` 時記錄 USD cost metadata。
- 需要用 `OMP_PROFILE` 隔離 OMP auth/settings/cache。
- 需要用 `OMP_SESSION_DIR` 管理 bot 啟動的 OMP session files。

OMP 是 opt-in。請先安裝並認證 `omp`，再啟用 OMP；重啟後用 `/doctor` 確認 readiness。

## 選擇部署型態

| 型態 | 適合情境 | 設定 |
| --- | --- | --- |
| Kiro-only | 第一次安裝、保守 production rollout、既有 Kiro 團隊。 | `AGENT_ENGINE=kiro`，`AGENT_ENGINES_ENABLED` 留空或只包含 `kiro`。 |
| Dual-engine bot | 想讓部分 channel/thread 試用 OMP，但不想增加另一個 Discord bot。 | `AGENT_ENGINE=kiro`，`AGENT_ENGINES_ENABLED=kiro,omp`。 |
| OMP-only | 明確不希望這個 bot process 使用 Kiro。 | `AGENT_ENGINE=omp`，`AGENT_ENGINES_ENABLED=omp`。 |
| 多 bot identities | 部門需要不同 Discord bot persona、data directory 與 ownership。 | 每個 bot token 跑一個 process，且使用不同 `DATA_DIR`。 |

除非你已經知道為什麼需要 OMP，否則建議先從 Kiro-only 開始。等 Discord、專案綁定、MCP 與 audit workflow 穩定後，再啟用 OMP。

## Bot 負責什麼

Engine 是 bot 針對 channel、thread、cron job 或 private audit prompt 啟動的 ACP command。目前支援：

| Engine | Binary | Dialect | 主要用途 |
| --- | --- | --- | --- |
| `kiro` | `kiro-cli` | Kiro ACP | 預設 engine、Kiro model fallback、Kiro runtime settings、既有 Kiro 工作流。 |
| `omp` | `omp` | OMP ACP | 替代 ACP engine，model/mode catalog 由 `session/new` 回報，可在有 metadata 時記錄 USD cost，並支援 OMP profile。 |

Engine 不會繞過 bot 的 policy model。Kiro 與 OMP 都透過同一套 MCP policy injection path 收到 tools，都透過 bot usage ledger 記錄用量，也都會在 audit events 中標記處理該工作的 engine。

## Scope 規則

Engine state 是 Discord target scope，不是全域 process state。

| Scope | 行為 |
| --- | --- |
| 新 channel | 使用 `AGENT_ENGINE`，除非已有儲存的 channel override。 |
| 既有 channel | 使用該 channel 儲存的 `Session.Engine`；空值退回 runtime default。 |
| 新 thread | 繼承 parent channel engine。 |
| 既有 thread | 優先使用 thread override，再退回 parent channel engine。 |
| 在 channel 使用 `/engine <engine>` | 只切換該 channel，並用新 engine 開全新 ACP session，重放近期 context。 |
| 在 thread 使用 `/engine <engine>` | 只切換該 thread；不會改 parent channel 或 sibling threads。 |

把某一個 channel 從 Kiro 切到 OMP 不會影響其他 channel。bot 會在每個 spawn point 解析 engine、binary、dialect、runtime env、MCP policy、audit attribution 與 usage attribution。

## 設定

| 變數 | 用途 |
| --- | --- |
| `AGENT_ENGINE` | 新 scope 的預設 engine。預設：`kiro`。 |
| `AGENT_ENGINES_ENABLED` | `/engine` 可切換的逗號分隔 engine。空值代表只允許 `AGENT_ENGINE`。 |
| `KIRO_CLI_PATH` | Kiro binary path。 |
| `OMP_PATH` | OMP binary path。 |
| `OMP_SESSION_DIR` | 可選 OMP session 目錄。空值表示 `DATA_DIR/omp-agent-runtime/sessions`。 |
| `OMP_PROFILE` | 可選 OMP profile，用來隔離 auth/settings/cache。空值沿用 OMP default profile，方便既有部署升級。 |

從舊的 Kiro-only 部署升級時，不需要新增任何環境變數。只有在安裝並認證 `omp` 後才啟用 OMP。

完整變數清單、常見設定型態與升級注意事項請看 [環境變數參考](environment.md)。

## Runtime Isolation

Kiro sessions 會收到 bot-managed Kiro runtime settings：

- `KIRO_HOME=DATA_DIR/kiro-agent-runtime`
- `KIRO_MCP_CONFIG=DATA_DIR/kiro-agent-runtime/settings/mcp.json`

bot 會把使用者 Kiro MCP settings 視為 catalog source，而不是直接 runtime inheritance。

OMP sessions 不會收到 Kiro runtime env。bot 會傳入 `--session-dir DATA_DIR/omp-agent-runtime/sessions`，讓 ACP session files 由 bot 管理，但不搬移既有 OMP auth/model database。

`OMP_PROFILE` 是可選設定。若設定，OMP 會使用該 named profile 管理 auth、settings、sessions、caches。啟用前請先認證：

```bash
OMP_PROFILE=kiro-discord-bot omp setup
```

若未設定 `OMP_PROFILE`，OMP 會使用 default profile。這是刻意保留的升級相容性，讓既有 OMP 安裝不會因升級失效。

## 操作差異

| 項目 | Kiro CLI | OMP |
| --- | --- | --- |
| 建議預設 | 是。 | 否，完成設定後 opt-in。 |
| Model listing | 可使用 Kiro fallback paths。 | 需要 active ACP session，因為 models 來自 `session/new`。 |
| Model switching | 使用 Kiro ACP model APIs。 | 使用 `session/set_config_option` 與 `configId=model`。 |
| Usage metadata | 有 Kiro metering metadata 時記錄 credits。 | 有 OMP `usage_update` metadata 時記錄 USD cost。 |
| Runtime settings | 隔離 `KIRO_HOME` 與 `KIRO_MCP_CONFIG`。 | 隔離 `--session-dir`，可選 `OMP_PROFILE`。 |
| MCP injection | 同一套 bot policy 與 proxy layer。 | 同一套 bot policy 與 proxy layer。 |
| 建議 rollout | 從這裡開始。 | Kiro-only 操作穩定後，或 OMP 已是團隊既有 workflow 時再啟用。 |

使用 `/doctor` 檢查 enabled engines 與有效 runtime values。切換 engine 後，用 `/status`、`/model`、`/usage` 確認 active engine、實際 model 與 usage attribution。
