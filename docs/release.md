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

2. Add ACP smoke checks when touching Kiro/ACP behavior:

   ```bash
   RUN_ACP_SMOKE=1 KIRO_CLI=$(which kiro-cli) scripts/release-preflight.sh
   ```

3. Review docs, tests, environment variables, and deployment notes.
4. Tag and push `vX.Y.Z`.
5. Wait for the release workflow and GitHub release artifacts.
6. Deploy binaries to target hosts.
7. Verify with `/doctor`, a simple agent reply, and any feature-specific smoke checks.

Do not delete `DATA_DIR`, Docker volumes, `.kiro/`, or `.env` during release or rollback.

[release]: https://nczz.github.io/kiro-discord-bot/guide/release.html
[deployment]: https://nczz.github.io/kiro-discord-bot/guide/deployment.html
[macos-networking]: https://nczz.github.io/kiro-discord-bot/guide/macos-mcp-networking.html
