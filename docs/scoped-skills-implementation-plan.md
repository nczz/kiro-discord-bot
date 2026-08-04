# Scoped Skills Implementation Plan

> 版本: 0.3 | 日期: 2026-08-02 | 狀態: implemented / release validation | 目的: 將 Discord server/channel/project scoped skills 收斂成自然語言優先、頻道可自理、server 管理面受控、可稽核可回復的工程規格。

## 0. 決策摘要

本功能要做的是 **Scoped Skill Registry + Natural-language Skill Management MCP + Audit-backed Lifecycle + Tool Requirement Resolver**。

它不是單純的 Markdown 管理器，也不是讓 agent 自行安裝 plugin 的 marketplace。

核心決策：

1. **SQLite 是 authoritative source**：skill catalog、版本、scope install、draft、mutation audit、usage audit 都存在 bot 的 SQLite store。
2. **project-local `SKILL.md` 是 materialized copy**：方便 agent 和使用者 review，但不是主資料源。
3. **自然語言優先，slash 是 fallback/admin shortcut**：使用者可直接請 agent 新增、啟用、更新、移除頻道 skills；`/skill` 指令保留但不作為主要體感。
4. **channel skills MCP 預設 read/use-only，lifecycle 需明確授權**：預設 safe bot tools 只暴露搜尋、effective list、get 與 server read tools；建立、啟用、更新、停用、移除、回復等 lifecycle tools 需 channel policy 明確允許，且 mutation 仍必須通過 authenticated Discord actor context 與頻道管理權限。
5. **server skills MCP 預設 read/use-only**：`bot-skills-server` 可讀/用 guild skills；建立、更新、移除、跨頻道管理需 server admin 明確開啟 management tools。
6. **skill 可宣告工具需求，但不能自己擴權**：`required_tools` 只能被 resolver 檢查；是否允許工具仍由既有 MCP policy 決定。
7. **所有 mutation 都可稽核、可回復**：mutation 需要 Discord actor、source message/session、before/after version/content SHA、result/error、hash chain；remove 是 soft-delete。
8. **tool package execution 不進 MVP**：外部 repo 若包含工具實作，第一版只記錄為未啟用的 detected tool package，不執行、不安裝。
9. **A2A `AgentSkill` 是輸出/映射層，不是 skill registry 主模型**：skill registry 應獨立存在，後續可映射到 A2A expose/delegate metadata。
10. **effective skill hints 是提示索引，不是完整技能注入**：每輪 prompt 可注入 bounded metadata/usage hints，幫助 agent 判斷何時呼叫 `bot_skill_get`；提示不得包含完整 procedure、不得取代 MCP scope visibility，也不得讓 skill text 變成權限或 system-level instruction。
## 1. 背景與既有架構接點

本規格必須沿用現有架構，不重複造輪。

已確認的接點：

| 需求 | 既有接點 | 使用方式 |
|---|---|---|
| bot MCP tools 註冊 | `internal/botmcp/server.go` `NewServer` | 新增 skill read/draft/install tools，沿用現有 `readOnlyTool` / `writeTool` annotation pattern。 |
| MCP tool visibility | `channel/mcp_policy.go` `MCPChannelPolicy.EffectiveTools` | skill resolver 用它判定 `required_tools` 是否可用。 |
| MCP preset / policy update | `channel/mcp_policy.go` `ApplyPreset` / policy tests | skill install tools 不能自動改 policy；需要管理者明確更新。 |
| CWD 安全邊界 | `channel.Manager.ValidateCWD` | 所有 project materialization 必須先 validate CWD。 |
| thread CWD 行為 | `channel.Manager.TargetCWDPath` | thread 預設繼承 parent channel CWD，除非 thread session 有 override。 |
| Discord 管理權限 | `bot.userCanManageAuditTarget` | server/channel scope install confirmation 應沿用相同 permission style。 |
| A2A skill 顯示 | `bot.agentSkillsFromPolicy` | 後續可把 installed/exposed skills 映射到 A2A runtime card，不作為第一版 source of truth。 |

### 1.1 不能繞過的現有邊界

1. 不直接讀寫 raw `DATA_DIR/ch-*` 作為 agent-visible state。
2. 不讓 generated skill 改寫 MCP policy。
3. 不讓 external source 直接進入 runtime prompt 成為永久規則。
4. 不繞過 `ValidateCWD` 寫 project files。
5. 不把 tool install 當成 skill install 的副作用。

## 2. 目標與非目標

### 2.1 目標

第一版完成後，bot 應支援：

1. server/guild scoped skills。
2. channel scoped skills。
3. project scoped skills。
4. channel + project scoped skills。
5. thread 透過 parent channel 繼承 effective skills，並使用 thread CWD override。
6. 使用者可用自然語言要求 agent 將目前討論收斂成 skill。
7. 使用者可提供 Markdown / URL / GitHub repo 讓 agent 轉成 skill。
8. channel 有權者可透過自然語言新增、啟用、更新、停用、移除、回復頻道/project scoped skills。
9. server/guild skills 預設可讀/用；server 管理面需明確開啟。
10. agent 可透過 MCP 搜尋 effective skills。
11. agent 可按需讀單一 skill 內容。
12. skill 可宣告 `required_tools`。
13. resolver 可回報 tool availability / missing tools。
14. 每次 skill 使用與 mutation 都可被 audit。
15. project skill 可 materialize 到 `<projectCWD>/.kiro-bot/skills/<slug>/SKILL.md`。

### 2.2 非目標

第一版不做：

1. marketplace / publish / subscription。
2. arbitrary plugin runtime。
3. 從 GitHub repo 自動執行 install script。
4. npm/pip/go dependency installation。
5. skill 自動開啟 MCP tools。
6. channel MCP 管理 server/guild scope。
7. server MCP 管理面預設開啟。
8. 將所有 skills 預載進 agent prompt。
9. 用 project-local markdown 取代 SQLite registry。
10. 直接把 existing A2A `AgentSkill` 當成 registry model。

## 3. 核心模型

### 3.1 Skill lifecycle

```text
external/conversation source
  -> draft or direct create request
  -> validation/risk report/tool requirement resolution
  -> channel actor authorization or server management gate
  -> installed/enabled scope
  -> effective resolution
  -> usage audit + mutation audit
  -> update/disable/remove/restore/rollback as new audited lifecycle events
```

Conversation/import sources may create active channel skills when the MCP call has authenticated actor context and passes channel permission checks. Otherwise they remain inactive drafts or return an explicit authorization error.
### 3.2 Scope model

支援四種 install scope：

```text
guild
channel
project
channel_project
```

解析順序：

```text
channel_project > channel > project > guild
```

同一 `canonical_slug` 在多個 scope 出現時，較窄 scope 覆蓋較寬 scope。Narrow scope 也可明確 disable inherited skill。

Thread 行為：

```text
thread target -> parent channel scope
thread CWD -> TargetCWDPath(targetID, parentChannelID)
```

### 3.3 Source model

`source_type`：

```text
conversation
markdown
url
github_repo
manual
builtin
```

來源只描述 skill 怎麼產生，不決定 skill scope 或權限。

### 3.4 Tool model

MVP 中，skill 只支援 tool requirement：

```yaml
required_tools:
  - read
  - python
  - discord_download_attachment
```

Resolver 只判定：

```text
required_tools - current_effective_mcp_tools = missing_tools
```

若有 missing tools，skill 仍可被讀，但 `executable=false`。

## 4. Package layout

新增 package：

```text
internal/skills/
  model.go
  store.go
  migrations.go
  scope.go
  resolver.go
  materialize.go
  draft.go
  import_markdown.go
  import_github.go
  sanitize.go
```

新增 botmcp glue：

```text
internal/botmcp/skills_tools.go
internal/botmcp/skills_tools_test.go
```

必要時新增 bot UI/command glue：

```text
bot/skills_commands.go
bot/skills_components.go
bot/skills_test.go
```

不應把 skill store 塞進 `channel.Manager`。`channel.Manager` 只提供 CWD、policy、session context；skill registry 是獨立 domain。

## 5. SQLite schema

資料庫位置：

```text
DATA_DIR/skills/skills.sqlite
```

### 5.1 `skills`

```sql
CREATE TABLE skills (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  skill_id TEXT NOT NULL UNIQUE,
  canonical_slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  current_version TEXT NOT NULL DEFAULT '',
  source_type TEXT NOT NULL,
  source_ref TEXT NOT NULL DEFAULT '',
  risk_level TEXT NOT NULL DEFAULT 'low',
  status TEXT NOT NULL DEFAULT 'active',
  created_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

### 5.2 `skill_versions`

```sql
CREATE TABLE skill_versions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  skill_id TEXT NOT NULL,
  version TEXT NOT NULL,
  content_markdown TEXT NOT NULL,
  content_sha256 TEXT NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(skill_id, version)
);
```

### 5.3 `skill_installs`

```sql
CREATE TABLE skill_installs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  install_id TEXT NOT NULL UNIQUE,
  skill_id TEXT NOT NULL,
  version TEXT NOT NULL,
  scope_type TEXT NOT NULL,
  guild_id TEXT NOT NULL DEFAULT '',
  channel_id TEXT NOT NULL DEFAULT '',
  project_cwd_hash TEXT NOT NULL DEFAULT '',
  project_cwd TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  override_policy TEXT NOT NULL DEFAULT 'inherit',
  materialized_path TEXT NOT NULL DEFAULT '',
  materialized_sha256 TEXT NOT NULL DEFAULT '',
  installed_by TEXT NOT NULL DEFAULT '',
  installed_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

Rules:

```text
scope_type=guild           -> guild_id required
scope_type=channel         -> guild_id + channel_id required
scope_type=project         -> project_cwd_hash + project_cwd required
scope_type=channel_project -> guild_id + channel_id + project_cwd_hash + project_cwd required
```

### 5.4 `skill_drafts`

```sql
CREATE TABLE skill_drafts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  draft_id TEXT NOT NULL UNIQUE,
  proposed_skill_id TEXT NOT NULL DEFAULT '',
  proposed_slug TEXT NOT NULL,
  proposed_name TEXT NOT NULL,
  proposed_description TEXT NOT NULL DEFAULT '',
  proposed_version TEXT NOT NULL DEFAULT '1.0.0',
  proposed_scope_type TEXT NOT NULL,
  guild_id TEXT NOT NULL DEFAULT '',
  channel_id TEXT NOT NULL DEFAULT '',
  project_cwd_hash TEXT NOT NULL DEFAULT '',
  project_cwd TEXT NOT NULL DEFAULT '',
  source_type TEXT NOT NULL,
  source_ref TEXT NOT NULL DEFAULT '',
  source_message_refs_json TEXT NOT NULL DEFAULT '[]',
  proposed_content_markdown TEXT NOT NULL,
  required_tools_json TEXT NOT NULL DEFAULT '[]',
  risk_report_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'draft',
  created_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL DEFAULT ''
);
```

### 5.5 `skill_tool_requirements`

```sql
CREATE TABLE skill_tool_requirements (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  skill_id TEXT NOT NULL,
  version TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  required INTEGER NOT NULL DEFAULT 1,
  min_version TEXT NOT NULL DEFAULT '',
  permission_level TEXT NOT NULL DEFAULT 'read',
  UNIQUE(skill_id, version, tool_name)
);
```

### 5.6 `skill_usage_events`

```sql
CREATE TABLE skill_usage_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  usage_id TEXT NOT NULL UNIQUE,
  skill_id TEXT NOT NULL,
  version TEXT NOT NULL,
  guild_id TEXT NOT NULL DEFAULT '',
  channel_id TEXT NOT NULL DEFAULT '',
  thread_id TEXT NOT NULL DEFAULT '',
  project_cwd_hash TEXT NOT NULL DEFAULT '',
  message_id TEXT NOT NULL DEFAULT '',
  agent_session_id TEXT NOT NULL DEFAULT '',
  selected_by TEXT NOT NULL DEFAULT 'agent',
  used_at TEXT NOT NULL
);
```

### 5.7 `skill_mutation_events`

```sql
CREATE TABLE skill_mutation_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT NOT NULL UNIQUE,
  event_type TEXT NOT NULL,
  action TEXT NOT NULL,
  skill_id TEXT NOT NULL DEFAULT '',
  install_id TEXT NOT NULL DEFAULT '',
  draft_id TEXT NOT NULL DEFAULT '',
  scope_type TEXT NOT NULL DEFAULT '',
  guild_id TEXT NOT NULL DEFAULT '',
  channel_id TEXT NOT NULL DEFAULT '',
  target_channel_id TEXT NOT NULL DEFAULT '',
  project_cwd_hash TEXT NOT NULL DEFAULT '',
  actor_user_id TEXT NOT NULL DEFAULT '',
  actor_username TEXT NOT NULL DEFAULT '',
  source_message_id TEXT NOT NULL DEFAULT '',
  source_interaction_id TEXT NOT NULL DEFAULT '',
  agent_session_id TEXT NOT NULL DEFAULT '',
  mcp_server_name TEXT NOT NULL DEFAULT '',
  mcp_tool_name TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  status_before TEXT NOT NULL DEFAULT '',
  status_after TEXT NOT NULL DEFAULT '',
  version_before TEXT NOT NULL DEFAULT '',
  version_after TEXT NOT NULL DEFAULT '',
  content_sha_before TEXT NOT NULL DEFAULT '',
  content_sha_after TEXT NOT NULL DEFAULT '',
  materialized_path TEXT NOT NULL DEFAULT '',
  materialized_sha256 TEXT NOT NULL DEFAULT '',
  result_status TEXT NOT NULL,
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  previous_event_hash TEXT NOT NULL DEFAULT '',
  event_hash TEXT NOT NULL,
  occurred_at TEXT NOT NULL
);

CREATE INDEX idx_skill_mutation_skill_time ON skill_mutation_events(skill_id, occurred_at);
CREATE INDEX idx_skill_mutation_scope_time ON skill_mutation_events(guild_id, channel_id, target_channel_id, occurred_at);
```

Every lifecycle transaction that changes `skills`, `skill_versions`, or `skill_installs` must insert a corresponding `skill_mutation_events` row in the same transaction before reporting success.

## 6. Skill file format

Canonical file format：

```md
---
id: erp-excel-reconcile
name: ERP Excel Reconcile
version: 1.0.0
description: Reconcile ERP Excel exports against user-provided worksheets.
tags: [erp, excel, reconciliation]
required_tools: [read, python, discord_download_attachment]
risk_level: medium
source_type: conversation
---

# When to use

Use this when the user asks to reconcile ERP inventory, invoice, or worksheet data.

# Preconditions

- Input files must be user-provided or already under the validated project CWD.
- Never mutate original files.

# Procedure

1. Locate relevant inputs.
2. Copy inputs into a working/output directory.
3. Normalize fields.
4. Compare by stable keys.
5. Produce a mismatch report.

# Safety

- Do not inspect bot DATA_DIR state files.
- Do not assume filenames reveal content.
- Do not enable missing tools yourself.

# Output contract

Return:
- input files used
- matched count
- mismatch count
- generated report path
```

Required sections:

```text
When to use
Preconditions
Procedure
Safety
Output contract
```

Markdown import must normalize into this format before draft preview.

## 7. MCP tool design

### 7.1 Split MCP services

Skills are exposed as two explicit MCP catalog entries:

```text
bot-skills-channel
bot-skills-server
```

`bot-skills-channel` is scoped to the current Discord channel/thread/project target and defaults to full lifecycle management for users with authenticated channel management permission.

`bot-skills-server` is scoped to the Discord guild. It defaults to read/use-only; management tools require explicit server admin enablement.

Legacy generic `bot_*` skill tools may remain for compatibility, but must not become the primary natural-language surface.

### 7.2 Channel tools: default full lifecycle

Default channel allowlist:

```text
skills_channel_search
skills_channel_effective_list
skills_channel_get
skills_channel_history
skills_channel_create
skills_channel_draft_from_conversation
skills_channel_import_markdown
skills_channel_import_url
skills_channel_import_github_repo
skills_channel_preview_draft
skills_channel_enable
skills_channel_update
skills_channel_disable
skills_channel_remove
skills_channel_restore
skills_channel_rollback
skills_channel_usage_record
```

Mutation tools must validate server-side actor context and write audit records. They must not trust actor, guild, channel, or project values supplied as normal MCP arguments.

### 7.3 Server tools: default read/use-only

Default server allowlist:

```text
skills_server_search
skills_server_get
skills_server_inventory
skills_server_effective_for_channel
skills_server_usage_report
```

Server management tools are registered but not included in default policy:

```text
skills_server_create
skills_server_update
skills_server_disable
skills_server_remove
skills_server_copy_to_channel
skills_server_promote_channel_skill
skills_server_restore
skills_server_rollback
```
### 7.4 Tool input contract

Channel tools do not accept authoritative `guild_id`, `channel_id`, `target_channel_id`, `actor_user_id`, or `project_cwd` arguments. They load those values from the server-side target state written by the Discord bot for the current agent job.

Mutation tools require:

```json
{
  "skill_id": "erp-excel-reconcile",
  "name": "ERP Excel Reconcile",
  "content_markdown": "...",
  "scope_type": "channel_project",
  "required_tools": ["read", "python"],
  "risk_level": "medium",
  "reason": "User asked to save the ERP reconciliation procedure for this channel."
}
```

Tool descriptions must be beginner-safe:

1. Say when to use the tool in plain language.
2. Say what user intent is required.
3. Say that channel tools are only for the current channel/thread/project.
4. Say that server management needs `bot-skills-server` management tools.
5. Say missing MCP tools are reported, not granted.

### 7.5 Channel lifecycle behavior

`skills_channel_create` and `skills_channel_update` may install/enable directly when:

1. `bot-skills-channel` loaded authenticated actor context from target state.
2. The actor has permission to manage the current channel/thread target.
3. Requested scope is `channel`, `project`, or `channel_project`.
4. Project scopes pass CWD validation and materialization safety.
5. Mutation audit write succeeds in the same operation.

If any condition fails, the tool returns a user-actionable error; it must not silently downgrade into an untracked draft.

### 7.6 Server lifecycle behavior

`bot-skills-server` read/use tools can resolve guild skills by default.

Server management tools may mutate only when:

1. The persistent MCP policy explicitly enables the server management tool.
2. The actor context proves server-level management permission.
3. Cross-channel targets are resolved by Discord channel mention/name lookup.
4. Mutation audit write succeeds.

Channel MCP can never create/update/remove `guild` scope skills.

## 8. Permission model

### 8.1 Authenticated actor context

Mutating MCP calls require bot-authored target state, not user-supplied tool arguments:

```json
{
  "guild_id": "guild-1",
  "channel_id": "parent-channel",
  "target_channel_id": "thread-or-channel",
  "project_cwd_hash": "sha256...",
  "actor_user_id": "123",
  "actor_username": "alice",
  "source_message_id": "msg-456",
  "agent_session_id": "session-789",
  "permission_snapshot": {
    "can_manage_channel": true,
    "can_manage_guild": false
  },
  "expires_at": "2026-07-30T12:34:56Z",
  "nonce": "..."
}
```

Rules:

1. State expires quickly and is bound to guild/channel/target/session.
2. MCP tools ignore actor/guild/channel/project values supplied in normal args.
3. Missing or expired actor state allows read tools only.
4. Permission snapshots are checked again when Discord APIs are available.

### 8.2 Guild/server scope

Installing, updating, disabling, removing, or rolling back guild skills requires server-level management permission and enabled `bot-skills-server` management tools.

### 8.3 Channel scope

Channel, project, and channel_project skill lifecycle operations require permission to manage that channel. Thread requests resolve parent channel for permission while using `TargetCWDPath(targetID, parentChannelID)` for project CWD.

### 8.4 Project scope

Project materialization requires:

1. CWD exists.
2. CWD passes `channel.Manager.ValidateCWD`.
3. path stays under `<projectCWD>/.kiro-bot/skills/`.
4. slug sanitization blocks path traversal and hidden/control names.

### 8.5 Tool permission

Skill install does not grant tool permission.

A skill with missing tools is installed as non-executable for that scope until MCP policy is updated.

## 9. Tool requirement resolver

Resolver input:

```go
type ResolveContext struct {
    GuildID         string
    ChannelID       string
    ParentChannelID string
    TargetID        string
    ProjectCWD      string
    EffectiveTools  []string
    AllowAllTools   bool
    ReadOnlyPolicy  bool
}
```

Resolver output:

```go
type ResolvedSkill struct {
    SkillID      string
    Slug         string
    Version      string
    ScopeType    string
    Content      string
    RequiredTools []string
    MissingTools []string
    Executable   bool
}
```

Rules:

1. If policy `AllowAllTools=true`, resolver treats required tools as available but still reports requirement list.
2. If policy `ReadOnly=true`, write/destructive tool requirements are missing unless explicitly safe/read-only.
3. Empty `required_tools` means executable from skill perspective.
4. Missing optional tools should not block execution but must be reported.
5. Unknown tool names are missing, not auto-created.

## 10. Import pipeline

### 10.1 Conversation import

Source is Discord conversation or agent-provided transcript refs.

1. Require explicit user intent, e.g. "做成技能", "存成日後可用的技能"。
2. Extract durable procedure, not ephemeral user data.
3. Reject secrets and credentials.
4. Convert into canonical sections.
5. For channel MCP with authenticated actor context, create/enable directly and audit.
6. Otherwise produce an inactive draft or an explicit authorization error.

### 10.2 Markdown import

Input can be raw markdown text or a downloaded markdown attachment.

Rules:

1. Parse frontmatter if present.
2. Normalize missing sections.
3. Preserve source attribution.
4. Reject path/secrets/policy-bypass content where detectable.
5. Imported content always creates an inactive review draft first. A separate explicit human confirmation may install or enable it; installation must audit the actor and must not grant missing tools automatically.

### 10.3 URL/GitHub import

Rules:

1. Fetch to temp only.
2. Enforce size limit and file count limit.
3. Only inspect markdown/text allowlist in MVP.
4. Do not execute source code.
5. Do not run package manager commands.
6. Do not write project files until actor authorization and materialization checks pass.
7. If tool code is detected, include `detected_tool_package=true` in risk report and keep it disabled.

Suggested candidate files:

```text
README.md
docs/**/*.md
examples/**/*.md
*.md
```

## 11. Materialization

Path:

```text
<projectCWD>/.kiro-bot/skills/<slug>/SKILL.md
```

Rules:

1. `<projectCWD>` must pass `ValidateCWD`.
2. `<slug>` must pass strict sanitizer:
   - lowercase letters, digits, `-` only
   - max length 80
   - no leading dot
   - no slash/backslash/colon
   - no `..`
3. Write is atomic: temp file + rename.
4. Store `materialized_sha256`.
5. If existing file checksum mismatches, report drift instead of overwriting unless install/update explicitly confirms replacement.

## 12. Security invariants

1. External content is untrusted until validated and auditable.
2. Skill text cannot grant permissions.
3. Skill install/update/remove cannot mutate MCP policy.
4. Tool package code is never executed in MVP.
5. Channel MCP may mutate channel/project scopes only with authenticated Discord actor context and channel management permission.
6. Server MCP management tools are default-off; server read/use tools remain available by default.
7. Project file materialization never writes outside validated CWD.
8. Search/list tools never expose invisible scope skills.
9. `*_get` returns one skill only after scope visibility check.
10. Usage audit records skill ID/version/scope/message/session.
11. Mutation audit records actor, source message/session, before/after status/version/content SHA, result/error, and hash chain.
12. Remove/uninstall is soft-delete or disabled status; hard delete is not part of natural-language lifecycle.
13. No tool output should include raw bot DATA_DIR paths unless the tool is explicitly admin/audit-scoped and redacted.

### 12.1 Runtime configuration

New configuration should be optional and fail closed:

```text
SKILLS_ENABLED=true
SKILLS_DB_PATH=
SKILL_IMPORT_MAX_BYTES=1048576
SKILL_IMPORT_MAX_FILES=50
SKILL_DRAFT_TTL_HOURS=72
SKILL_MATERIALIZE=true
```

Rules:

1. Empty `SKILLS_DB_PATH` means `DATA_DIR/skills/skills.sqlite`.
2. If `SKILLS_ENABLED=false`, MCP skill tools return explicit disabled errors.
3. Import limits apply before any LLM summarization or draft conversion.
4. Materialization can be disabled without disabling SQLite registry.
5. No config may default to granting new MCP tool permissions.

### 12.2 Privacy and path exposure

Skill tools must avoid repeating the historical attachment/data path leak pattern:

1. Public/default skill tools should return skill IDs, scope labels, and project-relative materialized paths only.
2. Full `project_cwd` may be accepted as input for resolution but should not be echoed unless the caller is an admin/audit flow.
3. Bot `DATA_DIR`, `ch-*`, `sessions.json`, `policy.sqlite`, and audit database paths must never appear in skill search/get/import responses.
4. External source import reports should list source URLs and sanitized filenames, not temp paths.
5. Materialization responses should prefer `.kiro-bot/skills/<slug>/SKILL.md` over absolute host paths.

## 13. Implementation phases

Each phase must receive strict review before proceeding. Reviews must include tool instruction quality and at least one realistic user scenario where the user does not know slash/MCP terminology.

### Phase 1: Planning and architecture update

Acceptance:

1. This plan reflects channel default-full lifecycle and server read/use default.
2. Tool descriptions are specified as natural-language-first UX contracts.
3. Audit and actor-context requirements are explicit.

### Phase 2: Store, audit, and lifecycle

Files:

```text
internal/skills/model.go
internal/skills/store.go
internal/skills/migrations.go
internal/skills/store_test.go
```

Acceptance:

1. Can create/update/enable/disable/remove/restore/rollback skill versions transactionally.
2. Remove is soft-delete/disabled, not hard delete.
3. Mutation audit records before/after status/version/content SHA, actor, scope, source message/session, result/error.
4. Audit events are append-only and tamper-evident through event hash chaining.
5. Effective resolver returns correct precedence and ignores disabled installs.

### Phase 3: Authenticated channel actor context

Files:

```text
channel/bot_tools_target.go
channel/manager.go
internal/botmcp/server.go
internal/botmcp/skills_tools.go
```

Acceptance:

1. Target state includes actor user, source message/session, permission snapshot, expiry, and nonce.
2. Channel mutating tools ignore spoofed actor/guild/channel/project args.
3. Missing/expired actor context permits read tools but rejects mutations.
4. Thread target uses parent channel for permission and target CWD for project hash.

### Phase 4: Channel skills MCP full lifecycle

Files:

```text
internal/botmcp/skills_channel_tools.go
internal/botmcp/skills_tools_test.go
channel/mcp_policy.go
channel/mcp_policy_test.go
```

Acceptance:

1. `bot-skills-channel` is registered as a builtin MCP server.
2. Default channel policy exposes channel read and lifecycle skill tools.
3. Natural-language create/update/disable/remove/restore flows work with authenticated actor context.
4. Channel tools cannot manage `guild` scope.
5. Tool responses are beginner-safe and action-oriented.

### Phase 5: Server skills MCP read/use and gated management

Files:

```text
internal/botmcp/skills_server_tools.go
internal/botmcp/skills_tools_test.go
channel/mcp_policy.go
channel/mcp_policy_test.go
```

Acceptance:

1. `bot-skills-server` is registered as a builtin MCP server.
2. Default policy exposes server read/use tools only.
3. Server management tools are available only after explicit policy enablement.
4. Server management rejects non-guild-manager actor context.
5. Cross-channel operations audit source and target channel IDs.

### Phase 6: Discord fallback UX

Files:

```text
bot/skill_commands.go
bot/skill_commands_test.go
bot/handler.go
```

Acceptance:

1. `/skill` fallback still works.
2. Add `/skill history`, `/skill restore`, `/skill rollback` or equivalent subcommands.
3. Discord responses include audit IDs for lifecycle mutations.
4. Natural-language MCP flows do not require users to know slash command names.

### Phase 7: Import and materialization hardening

Files:

```text
internal/skills/materialize.go
internal/skills/materialize_test.go
internal/botmcp/skills_tools.go
internal/botmcp/skills_tools_test.go
```

Acceptance:

1. Project skills write `SKILL.md` under `.kiro-bot/skills/<slug>/` only.
2. Blocks invalid CWD, unsafe slug, symlink escape, and private URL import.
3. Imported content can create/enable channel skill only with actor context; otherwise draft only.
4. Responses expose project-relative paths only.

### Phase 8: Final review and regression

Acceptance:

1. Targeted package tests pass.
2. Full `go test ./...` passes.
3. Strict reviewer returns no blocking findings.
4. Reviewer explicitly checks natural-language interaction quality, tool descriptions, default policy exposure, actor-context anti-spoofing, audit completeness, and rollback behavior.

## 14. Test plan

Run targeted tests per phase:

```text
go test ./internal/skills
go test ./internal/botmcp
go test ./channel
go test ./bot
```

Full regression after integration:

```text
go test ./...
```

Required behavioral test cases:

1. Guild skill visible in channel.
2. Channel skill overrides guild skill with same slug.
3. Channel disables inherited guild skill.
4. Project skill visible only when project CWD hash matches.
5. Channel_project skill wins over channel/project/guild.
6. Thread inherits parent channel skill and uses thread CWD override.
7. Missing required tool blocks executable status.
8. Channel MCP create/update/disable/remove works with authenticated channel actor context.
9. Channel MCP mutation rejects spoofed actor/channel args.
10. Channel MCP rejects guild-scope mutation.
11. Server MCP read tools work by default.
12. Server MCP management tools are default-blocked.
13. Server MCP management requires guild management permission when enabled.
14. Mutation audit records actor, source message/session, before/after version/content SHA, result, and event hash.
15. Restore/rollback can recover a disabled/removed skill.
16. Install/update rejects invalid CWD.
17. Materialization rejects unsafe slug and symlink escape.
18. URL/GitHub import does not execute code and rejects private-network sources.
19. Usage audit records version and scope.
20. Tool descriptions guide a non-expert user toward natural-language management without mentioning raw IDs unless needed.

## 15. Migration and compatibility

Initial deployment does not need to migrate existing memory or A2A skills.

Rules:

1. Existing channel memory remains separate.
2. Existing A2A expose/delegate policy remains separate.
3. Existing MCP policy remains source of non-skill tool access.
4. New skill store can be absent; bot should lazily create it.
5. Existing generic `bot_skill_*` tools remain compatibility aliases or fail closed for management until replaced by mode-specific channel/server tools.
6. If `DATA_DIR/skills/skills.sqlite` cannot open, skill tools return explicit error and do not affect other bot tools.

## 16. Operational behavior

### 16.1 Agent selection flow

```text
User asks task
  -> agent calls skills_channel_search / skills_server_search
  -> agent selects relevant skill metadata
  -> agent calls skills_channel_get / skills_server_get
  -> resolver returns content + executable/missing_tools
  -> agent follows skill or reports missing tools in plain language
  -> agent records usage when skill is used
```

### 16.2 Natural-language channel creation flow

```text
User asks to save current method as skill
  -> agent asks only missing beginner-friendly details
  -> skills_channel_create validates actor context and channel permission
  -> registry stores version + install + audit
  -> materialize if project scope exists
  -> bot/agent replies with skill ID, scope, missing tools, audit ID, and rollback hint
```

### 16.3 Server skill management flow

```text
User asks for server-wide skill management
  -> server read tools may inspect existing server skills
  -> management request checks whether bot-skills-server management tools are enabled
  -> if disabled, agent explains how a server admin enables them
  -> if enabled, management still requires guild-manager actor context and audit
```

### 16.4 External import flow

```text
User provides markdown/GitHub URL
  -> fetch/extract docs only
  -> normalize to SKILL.md
  -> risk report
  -> if channel actor context is authorized, create/update active skill
  -> otherwise produce draft only or explicit authorization error
```

## 17. Failure modes and required responses

| Failure | Required behavior |
|---|---|
| Skill store unavailable | Return explicit MCP error; do not panic; do not disable other bot tools. |
| Missing required tools | Return `executable=false` and `missing_tools`; do not auto-enable policy. |
| Missing actor context on mutation | Reject mutation; read tools continue to work. |
| Spoofed actor/channel args | Ignore args; use server-side target state; audit rejected attempts when detectable. |
| Server management tool disabled | Explain that server skills can be used/read but not managed until admin enables management tools. |
| Invalid CWD | Reject materialization/install/update for project scopes. |
| Unsafe slug | Reject create/update until sanitized. |
| External source too large | Reject import with limit details. |
| Tool code detected in repo | Mark as disabled candidate; do not execute. |
| Scope conflict | Apply precedence deterministically and include selected scope in result. |
| Drifted materialized file | Report drift; do not overwrite unless explicit update flow confirms. |

## 18. Open design hooks for future tool packages

Do not implement in MVP, but reserve concepts:

```text
tool_packages
tool_package_versions
tool_installs
tool_usage_events
```

Future tool package rules:

1. Tool package install is separate from skill install.
2. Admin approval required.
3. Runtime sandbox required.
4. No bot DATA_DIR access.
5. Explicit filesystem/network/secrets permissions.
6. Version and checksum pinned.
7. Tool call audit required.

## 19. Definition of done

The implementation is complete only when:

1. A channel actor can create, update, disable, remove, restore, and rollback a skill through natural-language MCP flow.
2. A server/guild skill is visible in channels by default.
3. Server management tools are default-off while server read/use tools work.
4. A channel skill can override or disable inherited guild skill.
5. A project skill materializes under validated project CWD.
6. Agent can search/get effective skills through channel and server MCP services.
7. Missing tool requirements are reported and block executable status.
8. No skill lifecycle path mutates MCP policy.
9. External markdown/GitHub source never executes code and can only active-install with authenticated actor context.
10. Mutation audit and rollback are complete enough to explain and recover every lifecycle change.
11. Tests cover store, resolver, MCP tools, permission edge cases, audit, rollback, and materialization safety.
12. Existing bot behavior remains unchanged when no skill store exists or no skill is installed.

## 20. Anti-drift checklist for implementers

Before adding code, confirm the change obeys these rules:

- [ ] Is this using `internal/skills` instead of hiding persistence in `channel.Manager`?
- [ ] Is SQLite the source of truth?
- [ ] Is project file output only a materialized copy?
- [ ] Does every project path pass `ValidateCWD`?
- [ ] Does every Discord target resolve parent thread behavior?
- [ ] Are mode-specific MCP tools named for channel/server scope?
- [ ] Are channel lifecycle tools default-exposed only through authenticated actor context?
- [ ] Are server management tools excluded from default policy?
- [ ] Does skill lifecycle avoid changing MCP policy?
- [ ] Are required tools resolved, not granted?
- [ ] Does every mutation write audit before reporting success?
- [ ] Can remove/disable be restored or rolled back?
- [ ] Is tool package code detected but not executed?
- [ ] Are secrets/private paths rejected or redacted in draft/import output?
- [ ] Are tool descriptions understandable to non-expert Discord users?
- [ ] Are tests added for the behavior, not implementation details?

