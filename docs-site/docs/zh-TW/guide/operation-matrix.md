# 操作矩陣

當變更 agent engines、Discord 指令、用量統計、thread agents、MCP 或 audit 行為時，release 前使用這份矩陣檢查。

## Engine Scopes

| Scope | Kiro | OMP |
| --- | --- | --- |
| 新頻道 | 使用 `AGENT_ENGINE`，除非 `/engine` 儲存 override。 | 相同；需要 `AGENT_ENGINE=omp`，或在 OMP 已啟用時使用 `/engine omp`。 |
| 既有頻道 | 使用儲存在 `Session.Engine` 的值；空值代表 runtime default。 | 相同。切換 engine 會開新的 ACP session，並重放最近對話脈絡。 |
| 新 thread | 繼承 parent channel engine，除非 thread 儲存 override。 | 相同。 |
| 既有 thread | thread override 優先，其次是 parent channel engine。 | 相同。 |

## Command Behavior

| Command | Channel | Thread | Inactive Kiro | Inactive OMP |
| --- | --- | --- | --- | --- |
| `/engine` | 顯示或切換 channel engine。 | 顯示或切換 thread override。 | Kiro 啟用時可用。 | OMP 啟用時可用。 |
| `/models` | 列出 channel agent models。 | 列出 thread agent models。 | fallback 到 `kiro-cli chat --list-models`。 | 需要 active ACP session，因為 models 來自 `session/new`。 |
| `/model <id>` | 盡可能動態切換 channel model，否則 restart。 | 用指定 model respawn thread agent。 | 透過 Kiro CLI fallback 驗證。 | 透過 active ACP session 驗證。 |
| `/agent` | 列出 channel agent modes。 | 列出 thread agent modes。 | 需要 active session。 | 需要 active session。 |
| `/agent <id>` | 切換 channel agent mode。 | 切換 thread agent mode。 | 使用 ACP `session/set_mode`。 | 使用 ACP `session/set_mode`。 |
| `/status` | 顯示 engine、agent version、model、queue、context usage。 | thread agent 也相同。 | 顯示 active Kiro version/model。 | 顯示 active OMP version/model。 |
| `/usage` | 全伺服器私密報表：預設查自己；具備管理伺服器／系統管理員權限者可彙總所有使用者或指定其他成員。 | 採相同的全伺服器 scope 與權限規則。 | Credits 來自 Kiro metering metadata。 | USD cost 來自 OMP `usage_update`。 |
| `/usage-history` | 私密查詢全伺服器詳細 usage history，支援 user/period/status/source filters；只能查自己，除非 requester 具備管理伺服器／系統管理員權限。 | 採相同的全伺服器 scope 與權限規則。 | 讀取 SQLite usage history。 | 讀取 SQLite usage history。 |
| `/audit prompt` | 使用短生命週期 scoped agent，並把 usage 歸到 Discord caller。 | 相同，並帶 thread target metadata。 | 使用 channel engine。 | 使用 channel engine。 |
| `/webshare start` | WebShare 啟用且 requester 可管理 target 時，為 parent channel 開啟 delegated browser share。 | 為 thread target 開啟 delegated browser share。 | 透過正常 manager entrypoints 使用 channel/thread engine。 | 透過正常 manager entrypoints 使用 channel/thread engine。 |
| `/webshare stop` / `/webshare revoke` | 停止 opener share，或由 manager revoke active target share；會解除 opener lockout。 | Thread target 也相同。 | 不繞過 Kiro cancellation/session rules。 | 不繞過 OMP cancellation/session rules。 |

## Release Checklist

- 跑一般 tests、vet、build、docs verification 與 `git diff --check`。
- Kiro 行為有變更時跑 Kiro ACP smoke。
- OMP 行為有變更時跑 OMP ACP smoke。
- 在 Discord 的 parent channel 與 thread 內測 `/engine`、`/models`、`/model`、`/agent`、`/status`、`/usage`、`/usage-history` 與 `/audit prompt`。
- WebShare 變更需驗證 `/webshare start`、browser connect、可見 `via WebShare` post、selected-user mention、agent prompt、thread action、upload/fetch attachment、`/webshare stop` 與 manager `/webshare revoke`。
- 確認 `/doctor` 會回報每個 enabled engine、Usage SQLite schema/record health，且不要求 disabled engines。
- 確認失敗的 `/engine` switch 不會留下 partial channel 或 thread session。
