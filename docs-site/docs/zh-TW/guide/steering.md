# Steering 檔案

Steering 檔案是 `.kiro/steering/` 底下的 Markdown 文件，用來提供 agent 穩定的專案脈絡。它最適合放需要被 review、版本控制，並且每個未來 agent session 都應該知道的專案知識。

## 必要與可選檔案

bot 沒有強制要求任何固定檔名。新頻道 setup 需要時會建立 `.kiro/steering/` 目錄，`/steering create` 可以替目前頻道建立專案 context 檔。

如果團隊希望某些 steering 檔案必須存在，那應該視為團隊規範。例如 repo 可以規定正式作業前必須有 `.kiro/steering/project.md`，但這不是 bot runtime 的硬性要求。

## 建議命名

建議使用穩定、明確、小寫 kebab-case 檔名：

```text
.kiro/steering/
  project.md
  architecture.md
  coding-style.md
  release-process.md
  security-boundaries.md
  operations.md
```

好的檔名應描述文件責任，而不是建立日期。避免 `notes.md`、`misc.md`、`important.md` 這類模糊名稱。

## 目錄結構

除非專案真的很大，否則優先維持扁平結構。扁平結構比較容易讓人掃描，也比較容易讓 agent 穩定引用。

建議基礎結構：

```text
.kiro/
  steering/
    project.md
    architecture.md
    workflow.md
    release.md
    safety.md
```

大型 workspace 可以使用聚焦的子目錄：

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

不要把 steering 當成原始聊天紀錄或未整理筆記的堆放區。請把內容整理成決策、限制、指令與穩定專案事實。

## Steering 應該放什麼

高品質 steering 通常包含：

- 專案目的與重要領域語彙。
- 架構邊界與 ownership 規則。
- build、test、lint、release 指令。
- 安全與資料處理限制。
- review 標準與品質門檻。
- 維運流程與 rollback 步驟。
- 需要保持精簡時，連到更深入文件的連結。

## 衝突處理

最安全的規則是：steering 檔案不應互相衝突。bot 不會把 steering 變成自動判斷哪份 Markdown 優先的 policy engine。如果兩份文件互相矛盾，agent 可能會收到混亂脈絡。

建議慣例：

1. 全域專案規則放在 `project.md`。
2. 專門主題放在 `release.md` 或 `security-boundaries.md` 這類 topic file。
3. 如果 topic file 要覆蓋一般規則，明確寫出來。
4. 移除過期規則，不要把歷史替代方案留在 active steering。
5. 修改高影響 steering 後，如果 active session 已經看過舊指引，執行 `/clear` 與 `/reset`。

範例：

```md
# Release Process

This file overrides the generic test command in project.md for release work.
Before tagging a release, run:

    scripts/release-preflight.sh
```

## Steering vs Memory

`/memory` 適合輕量、可以直接從 Discord list/remove 的使用者或頻道偏好。Steering 適合值得進 Git review 的專案知識。

只要規則還在 `/memory list` 看得到，就會影響未來 turns。只要規則在 steering，就應視為 source-controlled project guidance。
