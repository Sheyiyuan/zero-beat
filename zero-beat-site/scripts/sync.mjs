// 从主仓库锁定的 TUI/GUI 子模块生成页面；组件仓库维护原文，本站维护导航与主题。
import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'

const repo = path.resolve(import.meta.dirname, '..', '..')
const site = path.join(repo, 'zero-beat-site')
const tui = path.join(repo, 'zero-beat-tui')
const gui = path.join(repo, 'zero-beat-gui')
const pages = []
const push = (source, src, dest, route) => pages.push({ source, src, dest, route })

for (const source of [tui, gui]) {
  if (!fs.existsSync(path.join(source, 'docs'))) {
    throw new Error(`缺少 ${path.basename(source)}/docs；请先在主仓库运行 make submodules`)
  }
}

// 保留已发布的 TUI URL，旧书签和搜索结果继续有效。
for (const dir of ['guide', 'reference']) {
  for (const file of fs.readdirSync(path.join(tui, 'docs', dir)).sort()) {
    if (!file.endsWith('.md') || file === 'development.md') continue
    push(tui, `docs/${dir}/${file}`, `${dir}/${file}`, `/${dir}/${file.slice(0, -3)}`)
  }
}
for (const file of fs.readdirSync(path.join(tui, 'docs')).sort()) {
  const match = file.match(/^release-notes-(.+)\.md$/)
  if (match) push(tui, `docs/${file}`, `changelog/${match[1]}.md`, `/changelog/${match[1]}`)
}
push(gui, 'README.md', 'gui/index.md', '/gui/')
function addGUIDocs(dir = '') {
  for (const entry of fs.readdirSync(path.join(gui, 'docs', dir), { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    const rel = path.posix.join(dir, entry.name)
    if (entry.isDirectory()) addGUIDocs(rel)
    else if (entry.name.endsWith('.md')) {
      const dest = `gui/${rel.replace(/(^|\/)README\.md$/, '$1index.md')}`
      push(gui, `docs/${rel}`, dest, `/${dest.replace(/index\.md$/, '').replace(/\.md$/, '')}`)
    }
  }
}
addGUIDocs()

const routeOf = new Map(pages.map(page => [path.join(page.source, page.src), page.route]))
const sources = [tui, gui, path.join(repo, 'zero-beat-core'), repo].map(root => ({
  root,
  name: root === repo ? 'zero-beat' : path.basename(root),
  commit: execFileSync('git', ['-C', root, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim(),
}))
const manifest = pages.map(page => ({
  src: path.relative(repo, path.join(page.source, page.src)).split(path.sep).join('/'),
  dest: page.dest,
  route: page.route,
  title: titleOf(fs.readFileSync(path.join(page.source, page.src), 'utf8'), page.route),
}))

function rewrite(link, from) {
  if (/^(?:[a-z][a-z\d+.-]*:|\/|#)/i.test(link)) return link
  const match = link.match(/^([^?#]*)(.*)$/)
  const target = path.resolve(path.dirname(from), decodeURIComponent(match[1]))
  const suffix = match[2]
  const page = routeOf.get(target) || routeOf.get(path.join(target, 'README.md'))
  if (page) return page + suffix
  const images = path.relative(path.join(tui, 'docs/images'), target)
  if (!images.startsWith('..') && !path.isAbsolute(images)) return '/images/' + images.split(path.sep).join('/') + suffix
  for (const root of [tui, repo]) {
    const asset = path.relative(path.join(root, 'assets'), target)
    if (!asset.startsWith('..') && !path.isAbsolute(asset)) return '/assets/' + asset.split(path.sep).join('/') + suffix
  }
  // 未选入网站的设计/源码链接指向其真正所属仓库的固定提交。
  const source = sources.find(({ root }) => target.startsWith(root + path.sep))
  if (!source || !fs.existsSync(target)) throw new Error(`无效链接：${path.relative(repo, from)} -> ${link}`)
  const rel = path.relative(source.root, target).split(path.sep).map(encodeURIComponent).join('/')
  const kind = fs.statSync(target).isDirectory() ? 'tree' : 'blob'
  return `https://github.com/Sheyiyuan/${source.name}/${kind}/${source.commit}/${rel}${suffix}`
}

// 只清理本站生成的目录，避免已移除文档残留在搜索索引和部署产物中。
for (const dir of ['guide', 'reference', 'adr', 'architecture', 'design', 'changelog', 'gui', 'public/images', 'public/assets']) {
  fs.rmSync(path.join(site, dir), { recursive: true, force: true })
}
fs.cpSync(path.join(tui, 'docs/images'), path.join(site, 'public/images'), { recursive: true })
fs.cpSync(path.join(repo, 'assets'), path.join(site, 'public/assets'), { recursive: true })

const linkRe = /(!?\[[^\]]*\]\()([^)\s]+)(\))/g
const imgRe = /(<img[^>]*\bsrc=")([^"]+)(")/g
for (const [index, page] of pages.entries()) {
  const from = path.join(page.source, page.src)
  const body = fs.readFileSync(from, 'utf8')
    .replace(linkRe, (_, pre, target, post) => pre + rewrite(target, from) + post)
    .replace(imgRe, (_, pre, target, post) => pre + rewrite(target, from) + post)
  const title = manifest[index].title
  const frontmatter = `---\ntitle: ${JSON.stringify(title)}\n---\n`
  const notice = page.source === gui ? '\n::: info GUI 设计文档\nGUI 目前处于设计阶段，本栏目描述计划与接口约定，不代表已有可用的 GUI 应用。\n:::\n\n' : ''
  const content = body.startsWith('---\n')
    ? body.replace(/^(---\n[\s\S]*?\n---\n)/, '$1' + notice)
    : frontmatter + notice + body
  const output = path.join(site, page.dest)
  fs.mkdirSync(path.dirname(output), { recursive: true })
  fs.writeFileSync(output, content)
}
fs.writeFileSync(path.join(site, '.vitepress/manifest.json'), JSON.stringify(manifest, null, 2) + '\n')

function titleOf(body, fallback) {
  return (body.match(/^#\s+(.+)$/m)?.[1] || fallback).trim()
}
console.log(`已同步 ${pages.length} 个页面：TUI 用户文档与 GUI 设计文档`)
