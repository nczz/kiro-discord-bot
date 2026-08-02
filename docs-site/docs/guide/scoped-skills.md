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

1. Ask the channel agent to create a skill from a procedure, prior discussion, Markdown, URL, Gist, GitHub repository, or file.
2. The agent researches any sources itself, extracts the reusable procedure, and calls `bot_skill_create` with only clean curated Markdown plus source refs.
3. The bot creates the skill as installed but disabled. It is listed for channel managers, but agents cannot use it yet.
4. A channel manager clicks **Enable** or runs `/skill enable skill_id:<id-or-slug>` when the procedure is ready to use.
5. The bot records mutation audit data before reporting success.

Slash commands remain fallback and admin shortcuts.

## Slash Commands

| Command | Purpose |
| --- | --- |
| `/skill list [query]` | Managers see installed skills for the current channel/project, including disabled skills; other users see only effective skills. |
| `/skill get skill_id:<id-or-slug>` | Managers can read one installed skill after scope checks; other users can read only effective skills. |
| `/skill create` | Create a skill from explicit name/content fields; it is installed disabled by default. |
| `/skill disable skill_id:<id-or-slug> [scope]` | Disable an installed skill at the selected scope. |
| `/skill enable skill_id:<id-or-slug> [scope]` | Enable an installed disabled skill at the selected scope. |
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

A skill can declare `required_tools`, but this only affects resolution and `missing_tools` reporting. Creating, installing, or enabling a skill never grants MCP permissions or changes channel policy.

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
