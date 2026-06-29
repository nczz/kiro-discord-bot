# Release Runbook

Tag、publish 或部署新 release 前使用這份 runbook。

## 1. Preflight

執行標準 preflight：

```bash
scripts/release-preflight.sh
```

若修改 ACP、engine 整合、MCP policy、`bot-tools` 或 cron pending ingestion，也執行對應 smoke checks：

```bash
RUN_ACP_SMOKE=1 KIRO_CLI=$(which kiro-cli) scripts/release-preflight.sh
RUN_OMP_SMOKE=1 OMP_PATH=$(which omp) scripts/release-preflight.sh
```

## 2. Review Diff

Tag 前確認：

- 文件與行為變更一致。
- 新增 `.env` 變數已文件化。
- 測試覆蓋改動的 contract。
- 部署注意事項包含必要 migration。
- 產物檔沒有被 staged。

若這次包含 agent-engine 架構變更，也要確認：

- Kiro-only 升級不需要新增環境變數。
- OMP 仍是 opt-in，並明確記載需要先安裝且認證 `omp` binary。
- `AGENT_ENGINE` 預設為 `kiro`，`AGENT_ENGINES_ENABLED` 只控制 `/engine` 可切換的清單。
- Kiro 與 OMP runtime isolation 都已文件化：`DATA_DIR/kiro-agent-runtime` 與 `DATA_DIR/omp-agent-runtime/sessions`。
- `OMP_PROFILE` 沒有被描述成必填；若使用，必須由執行 bot 的同一個 OS service user 先完成認證。
- `/status`、`/models`、`/model`、`/agent`、`/usage`、`/audit prompt`、MCP policy、cron 與 thread agents 都有針對變更的 engine path 做測試或 release smoke check。

## 3. Tag and Push

```bash
git tag vX.Y.Z
git push origin main vX.Y.Z
```

Release workflow 會為 Linux/macOS、amd64/arm64 建置 archives。每個 archive 應包含：

- `kiro-discord-bot`
- `mcp-discord` 或 `mcp-discord-server`
- `mcp-media` 或 `mcp-media-server`

## 4. 驗證 GitHub Actions

```bash
gh run list --workflow release --limit 1
gh run view <run-id>
gh release view vX.Y.Z --json tagName,name,isDraft,isPrerelease,url
```

Release 尚未存在、仍是 draft，或 artifacts 尚未可用前，不要部署新 tag。

## 5. 部署

Systemd hosts：

1. 下載 release archive。
2. 備份目前 binaries。
3. 停止 service。
4. 替換 binaries。
5. 啟動 service。
6. 檢查 logs 與 `/doctor`。

macOS launchd hosts：

1. 替換 local install directory 下的 binaries。
2. 保留 `.env`、data 與 launchd plist。
3. `launchctl kickstart -k` service。
4. 確認 `Bot running as ...` 與 `/doctor`。

## 6. Post-deploy Checks

- 在一般 parent channel 執行 `/doctor`。
- 測試簡單 agent reply。
- 測試 `/status`。
- 如果 MCP 有變更，開 `/mcp manage` 掃描 configured server。
- 如果 cron 有變更，跑一個安全的 `/cron-run`。
- 如果 thread 行為有變更，開新任務並在 thread 內延續。
- 如果 engine 行為有變更，在每個 enabled engine 的 channel/thread scope 測 `/engine`、`/models`、`/model`、`/agent`、`/status` 與 `/usage`。

完整 channel/thread 與 Kiro/OMP checklist 請使用 [操作矩陣](operation-matrix.md)。

## 7. Rollback

新 release 通過 live checks 前，保留上一版 binaries。Rollback 只應還原 binaries；不要刪除 `DATA_DIR`、Docker volumes、`.kiro/` 或 `.env`。

Rollback 後重啟 service 並跑 `/doctor`。

## 8. Kiro CLI 升級

可用時優先使用 Kiro CLI 自己的 update command：

```bash
kiro-cli update -y
kiro-cli --version
```

升級 CLI 後重啟 bot，讓 preflight 與 agent sessions 使用新 binary。
