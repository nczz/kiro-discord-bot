import { mkdir, readdir, readFile, rm, stat, writeFile, copyFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const srcDir = path.join(root, 'docs')
const outDir = path.join(root, 'dist')
const base = '/kiro-discord-bot/'

const pages = [
  ['Home', 'index.html'],
  ['Getting Started', 'guide/getting-started.html'],
  ['Installation', 'guide/installation.html'],
  ['Environment Reference', 'guide/environment.html'],
  ['Core Concepts', 'guide/core-concepts.html'],
  ['Command Reference', 'guide/commands.html'],
  ['Daily Workflows', 'guide/daily-workflows.html'],
  ['Listen Modes', 'guide/listen-modes.html'],
  ['Steering Files', 'guide/steering.html'],
  ['MCP Policy', 'guide/mcp.html'],
  ['Bot Tools MCP', 'guide/bot-tools.html'],
  ['Discord MCP', 'guide/mcp-discord.html'],
  ['Media MCP', 'guide/media-mcp.html'],
  ['Audit, Usage, and Privacy', 'guide/audit-usage-privacy.html'],
  ['Cron and Reminders', 'guide/cron-reminders.html'],
  ['Security Model', 'guide/security-model.html'],
  ['Admin & Security', 'guide/admin-security.html'],
  ['Deployment', 'guide/deployment.html'],
  ['Release Runbook', 'guide/release.html'],
  ['macOS MCP Networking', 'guide/macos-mcp-networking.html'],
  ['Troubleshooting', 'guide/troubleshooting.html'],
  ['Architecture', 'guide/architecture.html'],
  ['Contributor Guide', 'guide/contributing.html'],
  ['Docs Maintenance', 'guide/docs-maintenance.html'],
]

const zhPages = [
  ['首頁', 'zh-TW/index.html'],
  ['快速開始', 'zh-TW/guide/getting-started.html'],
  ['安裝', 'zh-TW/guide/installation.html'],
  ['環境變數參考', 'zh-TW/guide/environment.html'],
  ['核心概念', 'zh-TW/guide/core-concepts.html'],
  ['指令參考', 'zh-TW/guide/commands.html'],
  ['日常工作流', 'zh-TW/guide/daily-workflows.html'],
  ['監聽模式', 'zh-TW/guide/listen-modes.html'],
  ['Steering 檔案', 'zh-TW/guide/steering.html'],
  ['MCP 權限', 'zh-TW/guide/mcp.html'],
  ['Bot Tools MCP', 'zh-TW/guide/bot-tools.html'],
  ['Discord MCP', 'zh-TW/guide/mcp-discord.html'],
  ['Media MCP', 'zh-TW/guide/media-mcp.html'],
  ['Audit、用量與隱私', 'zh-TW/guide/audit-usage-privacy.html'],
  ['Cron 與提醒', 'zh-TW/guide/cron-reminders.html'],
  ['安全模型', 'zh-TW/guide/security-model.html'],
  ['管理與安全', 'zh-TW/guide/admin-security.html'],
  ['部署', 'zh-TW/guide/deployment.html'],
  ['Release Runbook', 'zh-TW/guide/release.html'],
  ['macOS MCP 網路', 'zh-TW/guide/macos-mcp-networking.html'],
  ['疑難排解', 'zh-TW/guide/troubleshooting.html'],
  ['架構', 'zh-TW/guide/architecture.html'],
  ['貢獻者指南', 'zh-TW/guide/contributing.html'],
  ['文件維護', 'zh-TW/guide/docs-maintenance.html'],
]

await rm(outDir, { recursive: true, force: true })
await mkdir(outDir, { recursive: true })

for await (const file of walk(srcDir)) {
  const rel = path.relative(srcDir, file)
  if (rel.startsWith(`public${path.sep}`)) {
    const target = path.join(outDir, rel.slice(`public${path.sep}`.length))
    await mkdir(path.dirname(target), { recursive: true })
    await copyFile(file, target)
    continue
  }
  if (!rel.endsWith('.md')) continue
  const markdown = await readFile(file, 'utf8')
  const html = renderPage(rel, markdown)
  const target = path.join(outDir, rel.replace(/\.md$/, '.html'))
  await mkdir(path.dirname(target), { recursive: true })
  await writeFile(target, html)
}

await writeFile(path.join(outDir, 'style.css'), stylesheet())
await writeFile(path.join(outDir, '404.html'), renderNotFound())

async function* walk(dir) {
  for (const name of await readdir(dir)) {
    const full = path.join(dir, name)
    const info = await stat(full)
    if (info.isDirectory()) {
      yield* walk(full)
    } else {
      yield full
    }
  }
}

function renderPage(rel, markdown) {
  const isZh = rel.startsWith(`zh-TW${path.sep}`)
  const pagePath = rel.replace(/\.md$/, '.html').replaceAll(path.sep, '/')
  const title = firstHeading(markdown) ?? 'kiro-discord-bot'
  const nav = renderNav(isZh, pagePath)
  const sidebar = renderSidebar(isZh, pagePath)
  const content = markdownToHtml(markdown, pagePath)
  const lang = isZh ? 'zh-TW' : 'en'
  const description = isZh
    ? 'kiro-discord-bot 文件：安裝、steering、MCP、部署與疑難排解。'
    : 'kiro-discord-bot documentation: setup, steering, MCP, deployment, and troubleshooting.'
  return `<!doctype html>
<html lang="${lang}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="description" content="${escapeAttr(description)}">
  <title>${escapeHtml(title)} | kiro-discord-bot</title>
  <link rel="icon" href="${base}logo.svg" type="image/svg+xml">
  <link rel="stylesheet" href="${base}style.css">
</head>
<body>
  <header class="topbar">
    <a class="brand" href="${base}">
      <img src="${base}logo.svg" alt="" width="32" height="32">
      <span>kiro-discord-bot</span>
    </a>
    <nav aria-label="Primary">${nav}</nav>
  </header>
  <div class="shell">
    <aside class="sidebar">${sidebar}</aside>
    <main class="content">
      ${content}
    </main>
  </div>
</body>
</html>
`
}

function renderNav(isZh, pagePath) {
  const links = isZh
    ? [
        ['指南', 'zh-TW/guide/getting-started.html'],
        ['Steering', 'zh-TW/guide/steering.html'],
        ['MCP', 'zh-TW/guide/mcp.html'],
        ['English', 'index.html'],
        ['GitHub', 'https://github.com/nczz/kiro-discord-bot'],
      ]
    : [
        ['Guide', 'guide/getting-started.html'],
        ['Steering', 'guide/steering.html'],
        ['MCP', 'guide/mcp.html'],
        ['zh-TW', 'zh-TW/index.html'],
        ['GitHub', 'https://github.com/nczz/kiro-discord-bot'],
      ]
  return links.map(([label, href]) => {
    const url = href.startsWith('http') ? href : base + href
    const active = href === pagePath ? ' class="active"' : ''
    return `<a${active} href="${escapeAttr(url)}">${escapeHtml(label)}</a>`
  }).join('')
}

function renderSidebar(isZh, pagePath) {
  const items = isZh ? zhPages : pages
  return `<nav aria-label="${isZh ? '文件導覽' : 'Documentation'}">` + items.map(([label, href]) => {
    const active = href === pagePath ? ' class="active"' : ''
    return `<a${active} href="${escapeAttr(base + href)}">${escapeHtml(label)}</a>`
  }).join('') + '</nav>'
}

function renderNotFound() {
  return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Not Found | kiro-discord-bot</title>
  <link rel="stylesheet" href="${base}style.css">
</head>
<body>
  <main class="content standalone">
    <h1>Page Not Found</h1>
    <p>The requested documentation page does not exist.</p>
    <p><a href="${base}">Back to documentation home</a></p>
  </main>
</body>
</html>`
}

function firstHeading(markdown) {
  const match = markdown.match(/^#\s+(.+)$/m)
  return match?.[1]?.trim()
}

function markdownToHtml(markdown, pagePath) {
  const lines = markdown.replace(/\r\n/g, '\n').split('\n')
  const out = []
  let paragraph = []
  let code = null
  let list = null
  let table = []
  const currentDir = path.posix.dirname(pagePath)

  const flushParagraph = () => {
    if (paragraph.length) {
      out.push(`<p>${inline(paragraph.join(' '), currentDir)}</p>`)
      paragraph = []
    }
  }
  const flushList = () => {
    if (list) {
      out.push(`</${list}>`)
      list = null
    }
  }
  const flushTable = () => {
    if (!table.length) return
    const [header, , ...rows] = table
    out.push('<table><thead><tr>' + splitTable(header).map(c => `<th>${inline(c, currentDir)}</th>`).join('') + '</tr></thead><tbody>')
    for (const row of rows) {
      out.push('<tr>' + splitTable(row).map(c => `<td>${inline(c, currentDir)}</td>`).join('') + '</tr>')
    }
    out.push('</tbody></table>')
    table = []
  }

  for (const line of lines) {
    if (code !== null) {
      if (line.startsWith('```')) {
        out.push(`<pre><code>${escapeHtml(code.replace(/\n$/, ''))}</code></pre>`)
        code = null
      } else {
        code += line + '\n'
      }
      continue
    }
    if (line.startsWith('```')) {
      flushParagraph(); flushList(); flushTable()
      code = ''
      continue
    }
    if (/^\s*$/.test(line)) {
      flushParagraph(); flushList(); flushTable()
      continue
    }
    if (/^\|.+\|$/.test(line)) {
      flushParagraph(); flushList()
      table.push(line)
      continue
    }
    flushTable()
    const heading = line.match(/^(#{1,4})\s+(.+)$/)
    if (heading) {
      flushParagraph(); flushList()
      const level = heading[1].length
      const text = heading[2].trim()
      const id = slug(text)
      out.push(`<h${level} id="${id}">${inline(text, currentDir)} <a class="anchor" href="#${id}" aria-label="Permalink">#</a></h${level}>`)
      continue
    }
    const unordered = line.match(/^\s*-\s+(.+)$/)
    if (unordered) {
      flushParagraph()
      if (list !== 'ul') {
        flushList()
        out.push('<ul>')
        list = 'ul'
      }
      out.push(`<li>${inline(unordered[1], currentDir)}</li>`)
      continue
    }
    const ordered = line.match(/^\s*\d+\.\s+(.+)$/)
    if (ordered) {
      flushParagraph()
      if (list !== 'ol') {
        flushList()
        out.push('<ol>')
        list = 'ol'
      }
      out.push(`<li>${inline(ordered[1], currentDir)}</li>`)
      continue
    }
    paragraph.push(line.trim())
  }
  flushParagraph(); flushList(); flushTable()
  return out.join('\n')
}

function splitTable(row) {
  return row.replace(/^\||\|$/g, '').split('|').map(c => c.trim()).filter(c => !/^:?-{3,}:?$/.test(c))
}

function inline(text, currentDir = '') {
  let s = escapeHtml(text)
  s = s.replace(/`([^`]+)`/g, '<code>$1</code>')
  s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
  s = s.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, label, href) => {
    const safeHref = normalizeHref(href, currentDir)
    return `<a href="${escapeAttr(safeHref)}">${label}</a>`
  })
  return s
}

function normalizeHref(href, currentDir = '') {
  if (href.startsWith('http') || href.startsWith('#')) return href
  if (href === '/') return base
  if (href.startsWith('/')) return base + href.slice(1)
  const [rawPath, fragment = ''] = href.split('#', 2)
  const outputPath = rawPath.replace(/\.md$/, '.html')
  const resolved = path.posix.normalize(path.posix.join(currentDir === '.' ? '' : currentDir, outputPath))
  const suffix = fragment ? `#${fragment}` : ''
  return base + resolved.replace(/^\.\//, '') + suffix
}

function slug(text) {
  return text.toLowerCase().replace(/<[^>]+>/g, '').replace(/[^a-z0-9\u4e00-\u9fff]+/gi, '-').replace(/^-|-$/g, '')
}

function escapeHtml(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
}

function escapeAttr(value) {
  return escapeHtml(value).replaceAll("'", '&#39;')
}

function stylesheet() {
  return `:root {
  color-scheme: light;
  --bg: #ffffff;
  --panel: #f8fafc;
  --text: #111827;
  --muted: #4b5563;
  --line: #e5e7eb;
  --brand: #2563eb;
  --brand-strong: #1d4ed8;
  --code-bg: #f3f4f6;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  line-height: 1.65;
  color: var(--text);
  background: var(--bg);
}
a { color: var(--brand); text-decoration: none; }
a:hover { text-decoration: underline; }
.topbar {
  position: sticky;
  top: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  min-height: 64px;
  padding: 0 32px;
  border-bottom: 1px solid var(--line);
  background: rgba(255, 255, 255, 0.96);
  backdrop-filter: blur(8px);
}
.brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  color: var(--text);
  font-weight: 700;
}
.topbar nav {
  display: flex;
  align-items: center;
  gap: 18px;
  font-size: 14px;
}
.topbar a.active,
.sidebar a.active { color: var(--brand-strong); font-weight: 700; }
.shell {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  max-width: 1180px;
  margin: 0 auto;
}
.sidebar {
  position: sticky;
  top: 64px;
  height: calc(100vh - 64px);
  padding: 32px 24px;
  border-right: 1px solid var(--line);
}
.sidebar nav {
  display: grid;
  gap: 8px;
}
.sidebar a {
  display: block;
  padding: 6px 8px;
  color: var(--muted);
  border-radius: 6px;
}
.sidebar a:hover {
  background: var(--panel);
  text-decoration: none;
}
.content {
  width: 100%;
  max-width: 840px;
  padding: 48px 40px 80px;
}
.content.standalone {
  margin: 0 auto;
}
h1, h2, h3, h4 {
  line-height: 1.25;
  letter-spacing: 0;
}
h1 {
  margin: 0 0 16px;
  font-size: 44px;
}
h2 {
  margin-top: 44px;
  padding-top: 8px;
  font-size: 28px;
}
h3 { margin-top: 32px; font-size: 21px; }
p, ul, ol, table, pre { margin: 16px 0; }
ul, ol { padding-left: 24px; }
code {
  padding: 2px 5px;
  border-radius: 5px;
  background: var(--code-bg);
  font-size: 0.92em;
}
pre {
  overflow-x: auto;
  padding: 16px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: #0f172a;
}
pre code {
  padding: 0;
  color: #e5e7eb;
  background: transparent;
}
table {
  display: block;
  overflow-x: auto;
  border-collapse: collapse;
  width: 100%;
}
th, td {
  padding: 10px 12px;
  border: 1px solid var(--line);
  text-align: left;
  vertical-align: top;
}
th { background: var(--panel); }
.anchor {
  color: #9ca3af;
  font-size: 0.75em;
  opacity: 0;
}
h1:hover .anchor,
h2:hover .anchor,
h3:hover .anchor,
h4:hover .anchor { opacity: 1; }
@media (max-width: 800px) {
  .topbar {
    position: static;
    align-items: flex-start;
    flex-direction: column;
    padding: 16px 20px;
  }
  .topbar nav {
    flex-wrap: wrap;
    gap: 12px;
  }
  .shell {
    display: block;
  }
  .sidebar {
    position: static;
    height: auto;
    padding: 18px 20px;
    border-right: 0;
    border-bottom: 1px solid var(--line);
  }
  .sidebar nav {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .content {
    padding: 32px 20px 64px;
  }
  h1 { font-size: 34px; }
}
`
}
