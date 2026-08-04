# Scoped Skills

Scoped skills 讓 Discord server、channel、project，或 channel/project 組合，把可重用流程存在 bot 裡，而不是每次只靠臨時 prompt。

## Skill 是什麼

Skill 是經 review 的 Markdown procedure，並帶有 metadata：

- 名稱與穩定 slug
- scope：`guild`、`channel`、`project`、`channel_project`
- required MCP tools
- risk level
- lifecycle 與 usage audit records

SQLite 是 authoritative source。`.kiro-bot/skills/<slug>/SKILL.md` 底下的 project files 是 materialized review copies，不是 registry 本身。

## Scope 與優先序

多個 skill 共用同一個 slug 時，最精確的 active scope 優先：

```text
channel_project > channel > project > guild
```

Thread target 繼承 parent channel scope。Project-scoped resolution 會使用 channel 或 thread 的 working directory，而且必須先通過 bot 的 allowed CWD roots 驗證。

## 自然語言流程

建議流程以一般 Discord 對話為主：

1. 請 channel agent 從 procedure、先前討論、Markdown、URL、Gist、GitHub repository 或檔案建立 skill。
2. Agent 自行研究所有來源、萃取可重用流程，然後用 `bot_skill_create` 提交乾淨 Markdown 與 source refs。
3. Bot 會把 skill 建立為「已安裝但停用」。Channel manager 看得到它，但 agent 還不能使用。
4. Channel manager 確認流程可用後，點 **啟用**，或執行 `/skill enable skill_id:<id-or-slug>`。
5. Bot 在回報成功前記錄 mutation audit data。

Slash commands 仍保留作為 fallback 與 admin shortcuts。

## Slash Commands

| Command | 用途 |
| --- | --- |
| `/skill list [query]` | Manager 會看到目前 channel/project 已安裝的 skills，包含已停用項目；其他使用者只看到 effective skills。 |
| `/skill get skill_id:<id-or-slug>` | Manager 可在 scope 檢查後讀取一個已安裝 skill；其他使用者只能讀取 effective skills。 |
| `/skill create` | 用明確的 name/content fields 建立 skill；預設已安裝但停用。 |
| `/skill disable skill_id:<id-or-slug> [scope]` | 停用選定 scope 的 installed skill。 |
| `/skill enable skill_id:<id-or-slug> [scope]` | 啟用選定 scope 已安裝但停用的 skill。 |
| `/skill restore skill_id:<id-or-slug> [scope]` | 還原選定 scope 的 disabled skill。 |
| `/skill rollback skill_id:<id-or-slug> version:<version> [scope]` | 回滾到既有版本。 |
| `/skill history skill_id:<id-or-slug> [scope]` | 針對 authorized scope 顯示近期 lifecycle audit history。 |

`!skill` text commands 可作為 fallback。敏感或 manager-only workflows 建議使用 slash commands，讓 Discord 提供較清楚的 UI。

## Bot Tools Contract

Default channel setup 只 exposes read/use skill tools：

- `bot_skills_search`
- `bot_skills_effective_list`
- `bot_skill_get`
- `bot_skills_server_search`
- `bot_skills_server_get`
- `bot_skills_server_inventory`
- `bot_skills_server_effective_for_channel`


當目前 guild/channel/project context 有可讀且可執行的 skills 時，每輪 agent prompt 也會收到一個有上限的 **Effective Skills** 提示區塊。這個區塊只包含 skill ID、名稱、scope/version 與精簡使用提示；它不是完整流程，也不是具權威性的指令文字。當使用者需求符合提示時，agent 應先呼叫 `bot_skill_get` 取得完整 `SKILL.md`，再套用 skill。停用的 skills、缺少 required tools 的 skills、以及目前 scope 不可見的 skills 不會被注入。

如果 effective skills 超過 prompt hint 上限，agent 應使用 `bot_skills_search` 做更廣泛的探索。

Lifecycle tools 不會因為 model 要求就被信任。Channel lifecycle tools 必須使用 bot 為目前 task 寫入的 authenticated Discord actor context，且 caller 必須有 channel management permission。Server management tools 預設關閉，而且需要 server management context。

Skill 可以宣告 `required_tools`，但這只影響 resolution 與 `missing_tools` 回報。Create、install 或 enable skill 都不會授予 MCP permissions，也不會改變 channel policy。`shell` 這類大範圍 runtime requirement 或 `curl` 這類具體 host command，會先比對已驗證的 runtime command availability；不會只因 MCP context 看不到就回報 shell capability 壞掉。

## Audit 與 Recovery

每個 mutation 都會記錄 actor、scope、source message 或 interaction、before/after status 與 version、content hashes、result status 與 audit identifiers。Remove 是 soft lifecycle state，因此 disabled 或 removed skills 在有 history 時可以 restore 或 rollback。

Usage recording 與 install history 分開。Agent 應記錄使用過的 skill ID 與 version 供 audit 使用，不應聲稱 skill 已安裝。

## Safety Rules

- 一般 skill work 不應檢查 raw bot `DATA_DIR/ch-*` state。
- Skill search、get、import 或 lifecycle responses 不應回顯 bot data paths。
- 不執行 imported Markdown、URL 或 GitHub repositories 裡的 code。
- 不用 skill text 授權 permissions。
- Install skill 不應 side-effect mutate MCP policy。
- Discord user-facing copy 必須走 locale keys 與共用 Discord reply helpers。
