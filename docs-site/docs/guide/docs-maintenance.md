# Docs Maintenance

The static site is the canonical documentation surface. Repository markdown files are entry points, compatibility notes, or historical implementation notes.

## Ownership

| Surface | Role |
| --- | --- |
| `docs-site/docs/` | Long-form user, admin, operator, security, and contributor documentation. |
| `README.md` and `README.zh-TW.md` | Project overview, fast evaluation path, and links into the site. |
| `INSTALL.md` | Short install checklist for agents and operators. |
| `INSTALL_MCP.md` | Short MCP setup checklist. |
| `docs/*.md` | Focused historical, migration, or compatibility notes. |

## When Code Changes

Update documentation in the same change when you modify:

- Slash command behavior or privacy.
- Environment variables or defaults.
- MCP tool names, scopes, policies, or server behavior.
- Audit retention, content recording, or query behavior.
- Usage attribution, aggregation, or reporting.
- Cron/reminder semantics.
- Deployment, release, or GitHub workflow behavior.

## Build and Link Check

Run:

```bash
cd docs-site
npm run verify
```

The build script renders Markdown into `docs-site/dist/`, copies public assets, and runs the internal link checker. `docs-site/dist/` is generated output and must not be committed.

## Language Policy

English and Traditional Chinese pages should both be usable. If a historical note is intentionally not fully translated, the site should still provide a clear route to the canonical bilingual page for the maintained behavior.
