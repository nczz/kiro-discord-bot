# Steering Files

Steering files are Markdown documents under `.kiro/steering/` that teach the agent stable project context. They are best for information that should be reviewed, versioned, and shared with every future agent session for that project.

## Required vs Optional Files

No steering file name is strictly required by the bot. A new channel setup creates the `.kiro/steering/` directory when needed, and `/steering create` can create a project context file for the current channel.

Use required files only as a team convention. For example, a repository may decide that `.kiro/steering/project.md` must exist before production work starts, but that is a project policy, not a bot runtime requirement.

## Recommended Naming

Prefer stable, descriptive, lowercase kebab-case names:

```text
.kiro/steering/
  project.md
  architecture.md
  coding-style.md
  release-process.md
  security-boundaries.md
  operations.md
```

Good names describe the responsibility of the document, not the date it was written. Avoid vague names such as `notes.md`, `misc.md`, or `important.md`.

## Directory Structure

Keep steering flat unless the project is large enough to justify deeper organization. A flat structure is easier for humans to scan and easier for agents to cite consistently.

Recommended baseline:

```text
.kiro/
  steering/
    project.md
    architecture.md
    workflow.md
    release.md
    safety.md
```

For a larger workspace, use focused subdirectories:

```text
.kiro/
  steering/
    project.md
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

1. Put global project rules in `project.md`.
2. Put specialized rules in topic files such as `release.md` or `security-boundaries.md`.
3. When a topic file overrides a general rule, write that explicitly.
4. Remove outdated rules instead of leaving historical alternatives in active steering.
5. After changing a high-impact steering rule, run `/clear` and `/reset` if the active session has already seen the old guidance.

Example:

```md
# Release Process

This file overrides the generic test command in project.md for release work.
Before tagging a release, run:

    scripts/release-preflight.sh
```

## Steering vs Memory

Use `/memory` for lightweight user or channel preferences that should be easy to list and remove from Discord. Use steering for project knowledge that deserves review in Git.

If a rule is still visible in `/memory list`, it affects future turns. If a rule is in steering, it should be treated like source-controlled project guidance.
