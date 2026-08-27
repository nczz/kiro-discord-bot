# Release Runbook

Use this runbook before tagging, publishing, or deploying a new release.

## 1. Preflight

Run the standard preflight:

```bash
scripts/release-preflight.sh
```

On macOS, avoid the default per-app `/var/folders/...` `TMPDIR` for the Go caches used by preflight. The script normalizes that default to `/tmp`; if module resolution reports `no required module provides package` for dependencies already present in `go.mod`, rerun with an explicit stable cache base:

```bash
TMP_BASE=/tmp RUN_ACP_SMOKE=1 KIRO_CLI=$(which kiro-cli) scripts/release-preflight.sh
```

When changing ACP behavior, engine integration, MCP policy, `bot-tools`, or cron pending ingestion, also run the relevant smoke checks:

```bash
RUN_ACP_SMOKE=1 KIRO_CLI=$(which kiro-cli) scripts/release-preflight.sh
RUN_OMP_SMOKE=1 OMP_PATH=$(which omp) scripts/release-preflight.sh
```

When changing A2A NATS behavior or rollout configuration, also run:

```bash
go test ./a2a ./channel ./internal/botmcp ./bot ./audit ./locale -run 'Test.*A2A|TestDoctor.*A2A'
python3 - <<'PY'
from pathlib import Path
rollout = Path('docs/a2a-nats-rollout.md').read_text()
for required in ['local two-bot smoke', 'same-channel co-present smoke', 'cross-server proxy smoke', 'NATS restart smoke', 'credential revocation smoke']:
    assert required in rollout
print('a2a-rollout-guide-ok')
PY
```

## 2. Review the Diff

Before tagging:

- Confirm docs match behavior changes.
- Confirm `.env` variables are documented.
- Confirm tests cover changed contracts.
- Confirm deployment notes mention any manual migration.
- Confirm generated artifacts are not staged.

For agent-engine architecture changes, also confirm:

- Kiro-only upgrades require no new environment variables.
- OMP remains opt-in and is documented as requiring an installed and authenticated `omp` binary.
- `AGENT_ENGINE` defaults to `kiro`, and `AGENT_ENGINES_ENABLED` controls only allowed `/engine` switches.
- Runtime isolation is documented for both `DATA_DIR/kiro-agent-runtime` and `DATA_DIR/omp-agent-runtime/sessions`.
- `OMP_PROFILE` is not presented as mandatory; when used, it must be authenticated by the same OS service user that runs the bot.
- `/status`, `/models`, `/model`, `/agent`, `/usage`, `/audit prompt`, MCP policy, cron, and thread agents are covered by tests or release smoke checks for the changed engine paths.

## 3. Tag and Push

```bash
git tag vX.Y.Z
git push origin main vX.Y.Z
```

The release workflow builds archives for Linux and macOS on amd64 and arm64. Each archive should include:

- `kiro-discord-bot`
- `mcp-discord` or `mcp-discord-server`
- `mcp-media` or `mcp-media-server`

## 4. Verify GitHub Actions

```bash
gh run list --workflow release --limit 1
gh run view <run-id>
gh release view vX.Y.Z --json tagName,name,isDraft,isPrerelease,url
```

Do not deploy a new tag until the release exists, is not a draft, and the artifacts are available.

## 5. Deploy

For systemd hosts:

1. Download the release archive.
2. Backup current binaries.
3. Stop the service.
4. Replace binaries.
5. Start the service.
6. Verify logs and `/doctor`.

For macOS launchd hosts:

1. Replace binaries under the local install directory.
2. Keep `.env`, data, and launchd plist intact.
3. Re-sign the replaced Darwin binaries before restart so macOS AppleSystemPolicy does not kill MCP child processes with `Transport closed`:

   ```bash
   for bin in kiro-discord-bot mcp-discord mcp-media mcp-discord-server mcp-media-server; do
     [ -e "$bin" ] && codesign --force --sign - "$bin"
   done
   ```

   Run this from the install directory after copying the release binaries; signing changes the installed file hash, so compare hashes before signing when you need release-asset parity.
4. `launchctl kickstart -k` the service.
5. Confirm `Bot running as ...`, `/doctor`, and an MCP smoke such as `/mcp manage` or a simple agent reply when `mcp-discord` is enabled.

## 6. Post-deploy Checks

- Run `/doctor` in a normal parent channel.
- Test a simple agent reply.
- Test `/status`.
- If MCP changed, open `/mcp manage` and scan a configured server.
- If cron changed, run one safe `/cron-run`.
- If thread behavior changed, start a task and continue inside its thread.
- If engine behavior changed, test `/engine`, `/models`, `/model`, `/agent`, `/status`, and `/usage` in both channel and thread scopes for each enabled engine.
- If A2A NATS changed or is being enabled, complete the [A2A NATS rollout gates](a2a-nats-rollout.md): local two-bot smoke, same-channel co-present smoke, cross-server proxy smoke, NATS restart smoke, credential revocation smoke, and rollback smoke.

Use the [Operation Matrix](operation-matrix.md) for the full channel/thread and Kiro/OMP checklist.

## 7. Rollback

Keep previous binaries until the new release has passed live checks. A rollback should restore binaries only; do not delete `DATA_DIR`, Docker volumes, `.kiro/`, or `.env`.

For A2A rollback, set `NATS_URL=""`, restart or drain the bot, keep A2A stores/audit rows under `DATA_DIR` for postmortem, run `/doctor`, and verify a simple non-A2A Discord reply.

After any rollback, restart the service and run `/doctor`.

## 8. Agent CLI Upgrades

Kiro CLI and OMP are external agent CLIs. This repository does not publish or update those CLIs; update them with their own tools and restart the bot afterward.

```bash
kiro-cli update -y
kiro-cli --version

omp update --check
omp update
omp --version
```

Restart the bot after any agent CLI upgrade so preflight and future agent sessions use the new binary. Run `/doctor` after restart to verify the enabled engines.
