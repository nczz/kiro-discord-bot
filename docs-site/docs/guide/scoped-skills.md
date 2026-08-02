# Scoped Skills

Scoped skills let a Discord server, channel, project, or channel/project pair keep reusable procedures in the bot instead of relying on one-off prompt text.

## What a Skill Is

A skill is a reviewed Markdown procedure with metadata:

- name and stable slug
- scope: `guild`, `channel`, `project`, or `channel_project`
- required MCP tools
- risk level
- lifecycle and usage audit records

SQLite is the source of truth. Project files under `.kiro-bot/skills/<slug>/SKILL.md` are materialized review copies, not the registry itself.

## Scope and Precedence

When multiple skills share a slug, the most specific active scope wins:

```text
channel_project > channel > project > guild
```

Thread targets inherit the parent channel scope. Project-scoped resolution uses the channel or thread working directory after the bot validates it against allowed CWD roots.

## Natural-language Workflow

The preferred workflow is conversational:

1. Ask the channel agent to turn a procedure, prior discussion, Markdown, URL, or GitHub repository into a skill draft.
2. The agent uses bot skill tools to create an inactive draft or a permitted channel/project lifecycle change.
3. A channel manager reviews the draft preview and confirms install when required.
4. The bot records mutation audit data before reporting success.

Slash commands remain fallback and admin shortcuts.

## Slash Commands

| Command | Purpose |
| --- | --- |
| `/skill list [query]` | List effective skills for the current channel/project. |
| `/skill get skill_id:<id-or-slug>` | Read one visible skill after scope and tool checks. |
| `/skill draft` | Create a draft from explicit name/content fields. |
| `/skill preview draft_id:<draft>` | Preview a draft with review buttons. |
| `/skill install draft_id:<draft>` | Install a reviewed draft when the caller can manage the target scope. |
| `/skill discard draft_id:<draft>` | Reject a draft. |
| `/skill disable skill_id:<id-or-slug> [scope]` | Disable an installed skill at the selected scope. |
| `/skill restore skill_id:<id-or-slug> [scope]` | Restore a disabled skill at the selected scope. |
| `/skill rollback skill_id:<id-or-slug> version:<version> [scope]` | Roll back to an existing version. |
| `/skill history skill_id:<id-or-slug> [scope]` | Show recent lifecycle audit history for an authorized scope. |

Text `!skill` commands are available as fallbacks. Sensitive or manager-only workflows should use slash commands where Discord can provide clearer UI.

## Bot Tools Contract

Default channel setup exposes read/use skill tools only:

- `bot_skills_search`
- `bot_skills_effective_list`
- `bot_skill_get`
- `bot_skills_server_search`
- `bot_skills_server_get`
- `bot_skills_server_inventory`
- `bot_skills_server_effective_for_channel`

Lifecycle tools are not trusted because a model asks for them. Channel lifecycle tools require the authenticated Discord actor context written by the bot for the current task and the caller's channel management permission. Server management tools are default-off and require server management context.

A skill can declare `required_tools`, but this only affects resolution and `missing_tools` reporting. Installing a skill never grants MCP permissions or changes channel policy.

## Audit and Recovery

Every mutation records actor, scope, source message or interaction, before/after status and version, content hashes, result status, and audit identifiers. Remove is a soft lifecycle state, so disabled or removed skills can be restored or rolled back when history is available.

Usage recording is separate from install history. Agents should record the skill ID and version they used for audit instead of claiming a skill was installed.

## Safety Rules

- Do not inspect raw bot `DATA_DIR/ch-*` state for normal skill work.
- Do not echo bot data paths in skill search, get, import, or lifecycle responses.
- Do not execute code from imported Markdown, URLs, or GitHub repositories.
- Do not use skill text to grant permissions.
- Do not mutate MCP policy as a side effect of skill install.
- User-facing Discord copy must go through locale keys and the shared Discord reply helpers.
