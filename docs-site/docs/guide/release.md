# Release Runbook

Use this runbook before tagging, publishing, or deploying a new release.

## 1. Preflight

Run the standard preflight:

```bash
scripts/release-preflight.sh
```

When changing ACP behavior, engine integration, MCP policy, `bot-tools`, or cron pending ingestion, also run the relevant smoke checks:

```bash
RUN_ACP_SMOKE=1 KIRO_CLI=$(which kiro-cli) scripts/release-preflight.sh
RUN_OMP_SMOKE=1 OMP_PATH=$(which omp) scripts/release-preflight.sh
```

## 2. Review the Diff

Before tagging:

- Confirm docs match behavior changes.
- Confirm `.env` variables are documented.
- Confirm tests cover changed contracts.
- Confirm deployment notes mention any manual migration.
- Confirm generated artifacts are not staged.

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
3. `launchctl kickstart -k` the service.
4. Confirm `Bot running as ...` and `/doctor`.

## 6. Post-deploy Checks

- Run `/doctor` in a normal parent channel.
- Test a simple agent reply.
- Test `/status`.
- If MCP changed, open `/mcp manage` and scan a configured server.
- If cron changed, run one safe `/cron-run`.
- If thread behavior changed, start a task and continue inside its thread.
- If engine behavior changed, test `/engine`, `/models`, `/model`, `/agent`, `/status`, and `/usage` in both channel and thread scopes for each enabled engine.

Use the [Operation Matrix](operation-matrix.md) for the full channel/thread and Kiro/OMP checklist.

## 7. Rollback

Keep previous binaries until the new release has passed live checks. A rollback should restore binaries only; do not delete `DATA_DIR`, Docker volumes, `.kiro/`, or `.env`.

After rollback, restart the service and run `/doctor`.

## 8. Kiro CLI Upgrades

Use the Kiro CLI's own update command where available:

```bash
kiro-cli update -y
kiro-cli --version
```

Restart the bot after a CLI upgrade so preflight and agent sessions use the new binary.
