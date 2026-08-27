# Release and Deployment

The canonical release and deployment runbooks now live on the static documentation site:

- [Release Runbook][release]
- [Deployment][deployment]
- [macOS MCP Networking][macos-networking]

## Short Checklist

1. Run preflight:

   ```bash
   scripts/release-preflight.sh
   ```

   On macOS, if preflight reports `no required module provides package` for dependencies already listed in `go.mod`, rerun with a stable cache base:

   ```bash
   TMP_BASE=/tmp scripts/release-preflight.sh
   ```

2. Add ACP smoke checks when touching engine/ACP behavior:

   ```bash
   RUN_ACP_SMOKE=1 KIRO_CLI=$(which kiro-cli) scripts/release-preflight.sh
   RUN_OMP_SMOKE=1 OMP_PATH=$(which omp) scripts/release-preflight.sh
   ```

3. Review docs, tests, environment variables, and deployment notes.
4. For agent-engine changes, confirm Kiro-only upgrades need no new env, OMP is opt-in, runtime isolation is documented, and `/status`, `/models`, `/model`, `/agent`, `/usage`, `/audit prompt`, MCP policy, cron, and thread-agent paths were covered.
5. Tag and push `vX.Y.Z`.
6. Wait for the release workflow and GitHub release artifacts.
7. Deploy binaries to target hosts.
8. Verify with `/doctor`, a simple agent reply, and any feature-specific smoke checks. For engine changes, use the operation matrix linked from the release runbook.
9. For A2A NATS changes or first enablement, follow `docs/a2a-nats-rollout.md` and complete the local two-bot, same-channel co-present, cross-server proxy, NATS restart, credential revocation, and rollback smokes.

Do not delete `DATA_DIR`, Docker volumes, `.kiro/`, or `.env` during release or rollback.

[release]: https://nczz.github.io/kiro-discord-bot/guide/release.html
[deployment]: https://nczz.github.io/kiro-discord-bot/guide/deployment.html
[macos-networking]: https://nczz.github.io/kiro-discord-bot/guide/macos-mcp-networking.html
