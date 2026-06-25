import { readdir, readFile, stat } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const outDir = path.join(root, 'dist')
const base = '/kiro-discord-bot/'
const files = []

for await (const file of walk(outDir)) {
  if (file.endsWith('.html')) files.push(file)
}

const generated = new Set()
for (const file of files) {
  const rel = path.relative(outDir, file).replaceAll(path.sep, '/')
  generated.add(base + rel)
  if (rel.endsWith('/index.html')) {
    generated.add(base + rel.slice(0, -'index.html'.length))
  }
  if (rel === 'index.html') {
    generated.add(base)
  }
}
generated.add(base + 'style.css')
generated.add(base + 'logo.svg')

const failures = []
for (const file of files) {
  const html = await readFile(file, 'utf8')
  const refs = [...html.matchAll(/\s(?:href|src)="([^"]+)"/g)].map(m => m[1])
  for (const ref of refs) {
    if (ref.startsWith('http') || ref.startsWith('mailto:') || ref.startsWith('#')) continue
    const [withoutHash] = ref.split('#')
    if (!generated.has(withoutHash)) {
      failures.push(`${path.relative(outDir, file)} -> ${ref}`)
    }
  }
}

if (failures.length) {
  console.error('Broken generated links:')
  for (const failure of failures) console.error(`- ${failure}`)
  process.exit(1)
}

console.log(`Checked ${files.length} HTML files; no broken generated links.`)

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
