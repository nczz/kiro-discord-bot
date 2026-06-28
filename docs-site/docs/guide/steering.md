# Steering Files

Steering files are Markdown documents that teach the agent stable project context. In dual-engine deployments, the shared cross-engine surface is `AGENTS.md` at the project root. The bot manages `AGENTS.md` by default; `.kiro/steering/*.md` is reserved for legacy or advanced Kiro-only guidance.

## Required vs Optional Files

No steering file is strictly required before a channel can run. `/steering create` creates:

- `AGENTS.md`: shared agent guidance read by Kiro CLI and OMP.

The bot does not create or synchronize `.kiro/steering/<project>.md` by default. If a project already has Kiro-only steering files, `/steering status` shows the legacy path for awareness.

Use required files only as a team convention. For example, a repository may decide that `AGENTS.md` must exist before production work starts, but that is a project policy, not a bot runtime requirement.

## Recommended Naming

Prefer stable, descriptive names:

```text
AGENTS.md
```

For advanced Kiro-only steering, additional `.kiro/steering/*.md` files can use lowercase kebab-case names such as `architecture.md`, `release-process.md`, or `security-boundaries.md`. Good names describe the responsibility of the document, not the date it was written.

## Directory Structure

Keep steering flat unless the project is large enough to justify deeper organization. A flat structure is easier for humans to scan and easier for agents to cite consistently.

Recommended baseline:

```text
AGENTS.md
```

For a larger Kiro-heavy workspace, add focused Kiro-only files manually:

```text
AGENTS.md
.kiro/
  steering/
    engineering/
      architecture.md
      coding-style.md
      testing.md
    operations/
      deployment.md
      incident-response.md
    product/
      domain.md
      terminology.md
```

Do not use steering as a dumping ground for raw chat logs or unreviewed notes. Curate the content into decisions, constraints, commands, and stable project facts.

## What to Put in Steering

Strong steering files usually include:

- Project purpose and important domain language.
- Architecture boundaries and ownership rules.
- Build, test, lint, and release commands.
- Security and data-handling constraints.
- Review standards and quality gates.
- Known operational procedures and rollback steps.
- Links to deeper docs when the file should stay short.

## Conflict Handling

The safest rule is: steering files should not conflict. The bot does not turn steering into a policy engine that decides which Markdown file wins. If two files disagree, the agent may receive confusing context.

Use these conventions:

1. Put cross-engine global project rules in `AGENTS.md`.
2. Put specialized rules in topic files such as `release.md` or `security-boundaries.md`.
3. When a topic file overrides a general rule, write that explicitly.
4. Remove outdated rules instead of leaving historical alternatives in active steering.
5. After changing a high-impact steering rule, run `/clear` and `/reset` if the active session has already seen the old guidance.

Example:

```md
# Release Process

This file overrides the generic test command in AGENTS.md for release work.
Before tagging a release, run:

    scripts/release-preflight.sh
```

## Steering vs Memory

Use `/memory` for lightweight user or channel preferences that should be easy to list and remove from Discord. Use steering for project knowledge that deserves review in Git.

If a rule is still visible in `/memory list`, it affects future turns. If a rule is in steering, it should be treated like source-controlled project guidance.
