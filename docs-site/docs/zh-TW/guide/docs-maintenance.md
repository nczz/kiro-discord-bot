# 文件維護

靜態站是 canonical documentation surface。Repository markdown files 是入口、compatibility notes 或歷史 implementation notes。

## Ownership

| Surface | 角色 |
| --- | --- |
| `docs-site/docs/` | 長篇 user、admin、operator、security、contributor documentation。 |
| `README.md` 與 `README.zh-TW.md` | 專案概覽、快速評估路徑、導向靜態站。 |
| `INSTALL.md` | 給 agent 與 operator 的精簡安裝 checklist。 |
| `INSTALL_MCP.md` | 精簡 MCP setup checklist。 |
| `docs/*.md` | 聚焦的歷史、migration 或 compatibility notes。 |

## Code 變更時

修改以下行為時，必須在同一個 change 更新文件：

- Slash command behavior 或 privacy。
- Environment variables 或 defaults。
- MCP tool names、scopes、policies 或 server behavior。
- Audit retention、content recording 或 query behavior。
- Usage attribution、aggregation 或 reporting。
- Cron/reminder semantics。
- Deployment、release 或 GitHub workflow behavior。

## Build 與 Link Check

執行：

```bash
cd docs-site
npm run verify
```

Build script 會把 Markdown render 到 `docs-site/dist/`、複製 public assets，並執行 internal link checker。`docs-site/dist/` 是 generated output，不應 commit。

## 語言政策

英文與繁體中文頁面都應維持可用。如果某個歷史 note 有意不完整雙語，也要讓使用者能清楚導向維護中行為的 canonical bilingual page。
