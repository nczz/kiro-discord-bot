# Agent Engines

`kiro-discord-bot` 現在是 Discord 上的 ACP agent control plane。Kiro CLI 仍是預設且最完整驗證的 engine，但 engine 選擇已經是一個正式 runtime 概念，不再是寫死的專案身份。

Engine 是 bot 針對 channel、thread、cron job 或 private audit prompt 啟動的 ACP command。目前支援：

| Engine | Binary | Dialect | 主要用途 |
| --- | --- | --- | --- |
| `kiro` | `kiro-cli` | Kiro ACP | 預設 engine、Kiro model fallback、Kiro runtime settings、既有 Kiro 工作流。 |
| `omp` | `omp` | OMP ACP | 替代 ACP engine，model/mode catalog 由 `session/new` 回報，可在有 metadata 時記錄 USD cost，並支援 OMP profile。 |

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

| 項目 | Kiro | OMP |
| --- | --- | --- |
| Model listing | 可使用 Kiro fallback paths。 | 需要 active ACP session，因為 models 來自 `session/new`。 |
| Model switching | 使用 Kiro ACP model APIs。 | 使用 `session/set_config_option` 與 `configId=model`。 |
| Usage metadata | 有 Kiro metering metadata 時記錄 credits。 | 有 OMP `usage_update` metadata 時記錄 USD cost。 |
| Runtime settings | 隔離 `KIRO_HOME` 與 `KIRO_MCP_CONFIG`。 | 隔離 `--session-dir`，可選 `OMP_PROFILE`。 |
| MCP injection | 同一套 bot policy 與 proxy layer。 | 同一套 bot policy 與 proxy layer。 |

使用 `/doctor` 檢查 enabled engines 與有效 runtime values。切換 engine 後，用 `/status`、`/model`、`/usage` 確認 active engine、實際 model 與 usage attribution。
