# Steering 檔案

Steering 檔案是提供 agent 穩定專案脈絡的 Markdown 文件。雙 engine 部署下，共用的跨 agent 入口是專案根目錄的 `AGENTS.md`。bot 預設只管理 `AGENTS.md`；`.kiro/steering/*.md` 保留給 legacy 或進階 Kiro-only guidance。

## 必要與可選檔案

bot 沒有強制要求任何固定檔案才能執行。`/steering create` 會建立：

- `AGENTS.md`：Kiro CLI 與 OMP 都會讀取的共用 agent guidance。

bot 預設不建立也不同步 `.kiro/steering/<project>.md`。如果專案已經有 Kiro-only steering files，`/steering status` 會顯示 legacy path 方便辨識。

如果團隊希望某些 steering 檔案必須存在，那應該視為團隊規範。例如 repo 可以規定正式作業前必須有 `AGENTS.md`，但這不是 bot runtime 的硬性要求。

## 建議命名

建議使用穩定、明確的檔名：

```text
AGENTS.md
```

進階 Kiro-only steering 可以額外使用 `.kiro/steering/*.md`，例如 `architecture.md`、`release-process.md` 或 `security-boundaries.md`。好的檔名應描述文件責任，而不是建立日期。

## 目錄結構

除非專案真的很大，否則優先維持扁平結構。扁平結構比較容易讓人掃描，也比較容易讓 agent 穩定引用。

建議基礎結構：

```text
AGENTS.md
```

大型且偏 Kiro-heavy 的 workspace 可以手動加入聚焦的 Kiro-only 檔案：

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

1. 跨 engine 的全域專案規則放在 `AGENTS.md`。
2. 專門主題放在 `release.md` 或 `security-boundaries.md` 這類 topic file。
3. 如果 topic file 要覆蓋一般規則，明確寫出來。
4. 移除過期規則，不要把歷史替代方案留在 active steering。
5. 修改高影響 steering 後，如果 active session 已經看過舊指引，執行 `/clear` 與 `/reset`。

範例：

```md
# Release Process

This file overrides the generic test command in AGENTS.md for release work.
Before tagging a release, run:

    scripts/release-preflight.sh
```

## Steering vs Memory

`/memory` 適合輕量、可以直接從 Discord list/remove 的使用者或頻道偏好。Steering 適合值得進 Git review 的專案知識。

只要規則還在 `/memory list` 看得到，就會影響未來 turns。只要規則在 steering，就應視為 source-controlled project guidance。
