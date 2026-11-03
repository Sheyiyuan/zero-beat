// 把仓库根目录的 markdown 同步进站点，站点不保存文档副本。
// 站点只保留面向用户的文档：guide/ 下排除 development.md，不同步 ADR、
// 架构与同步设计；这些内容仍留在仓库里，链接会改指 GitHub。
// 生成物（guide/ reference/ adr/ architecture/ design/ changelog/ public/images/ public/assets/）
// 已在 .gitignore 里，唯一来源始终是 docs/。
import fs from 'node:fs'
import path from 'node:path'

const repo = path.resolve(import.meta.dirname, '..', '..')
const site = path.join(repo, 'site')

// 源文件 → 站点路由；route 以 / 结尾表示 index 页
const pages = []
const push = (src, dest, route) => pages.push({ src, dest, route })

const guideSkip = new Set(['development.md'])

for (const dir of ['guide', 'reference']) {
  for (const f of fs.readdirSync(path.join(repo, 'docs', dir))) {
    if (!f.endsWith('.md')) continue
    if (dir === 'guide' && guideSkip.has(f)) continue
    push(`docs/${dir}/${f}`, `site/${dir}/${f}`, `/${dir}/${f.slice(0, -3)}`)
  }
}

for (const f of fs.readdirSync(path.join(repo, 'docs'))) {
  const m = f.match(/^release-notes-(.+)\.md$/)
  if (m) push(`docs/${f}`, `site/changelog/${m[1]}.md`, `/changelog/${m[1]}`)
}

const routeOf = new Map(pages.map((p) => [p.src, p.route]))
const blobs = ['README.md', 'ROADMAP.md', 'THIRD_PARTY_NOTICES.md', 'LICENSE']
const blobBase = 'https://github.com/Sheyiyuan/zero-beat/blob/main/'
// 站点不展示的开发者文档：用户手册里还引用着它们，改指 GitHub。
const devDocs = new Set(['ARCHITECTURE.md', 'docs/sync-design.md', 'docs/guide/development.md'])

function rewrite(link, fromSrc) {
  if (/^(https?:|mailto:|#)/.test(link)) return link
  const [target, hash] = link.split('#')
  if (!target) return link
  const abs = path.resolve(path.dirname(path.join(repo, fromSrc)), target)
  const rel = path.relative(repo, abs).split(path.sep).join('/')
  const withHash = (url) => (hash ? `${url}#${hash}` : url)
  if (routeOf.has(rel)) return withHash(routeOf.get(rel))
  if (devDocs.has(rel) || rel.startsWith('docs/adr/')) return withHash(blobBase + rel)
  if (rel.startsWith('docs/images/')) return withHash(`/images/${path.basename(rel)}`)
  if (rel.startsWith('assets/')) return withHash(`/assets/${rel.slice('assets/'.length)}`)
  if (blobs.includes(rel)) return withHash(blobBase + rel)
  // 文档里指向仓库源码的行号链接（internal/**、cmd/**）在站点里没有对应页面，改指 GitHub
  if (/^(internal|cmd|scripts|assets)\//.test(rel) || /(\.go|\.sh|\.yaml|\.mod)$/.test(rel)) {
    return withHash(blobBase + rel)
  }
  console.warn(`  ! 未映射的链接: ${fromSrc} -> ${link}`)
  return link
}

const linkRe = /(!?\[[^\]]*\]\()([^)\s]+)(\))/g
const imgRe = /(<img[^>]*\bsrc=")([^"]+)(")/g

// 页面是生成物：先清掉旧目录，避免已经从站点移除的文档（ADR、架构、开发）
// 继续出现在搜索索引和静态文件里。
for (const dir of ['guide', 'reference', 'adr', 'architecture', 'design', 'changelog']) {
  fs.rmSync(path.join(site, dir), { recursive: true, force: true })
}

fs.mkdirSync(path.join(site, 'public/images'), { recursive: true })
fs.cpSync(path.join(repo, 'docs/images'), path.join(site, 'public/images'), { recursive: true })
fs.cpSync(path.join(repo, 'assets'), path.join(site, 'public/assets'), { recursive: true })

const manifest = []
let count = 0
for (const { src, dest, route } of pages) {
  const text = fs.readFileSync(path.join(repo, src), 'utf8')
  const out = text
    .replace(linkRe, (_, pre, target, post) => pre + rewrite(target, src) + post)
    .replace(imgRe, (_, pre, target, post) => pre + rewrite(target, src) + post)
  const title = titleOf(out, route)
  // 已有 frontmatter 的文档不重复注入，避免覆盖站点里手工设定的元信息
  const body = out.startsWith('---')
    ? out
    : `---\ntitle: ${title}\n---\n` + out
  const outFile = path.join(site, dest.replace(/^site\//, ''))
  fs.mkdirSync(path.dirname(outFile), { recursive: true })
  fs.writeFileSync(outFile, body)
  manifest.push({ src, route, title })
  count++
}

// config.mts 用它生成侧栏，新增文档不用改配置
fs.writeFileSync(
  path.join(site, '.vitepress/manifest.json'),
  JSON.stringify(manifest, null, 2) + '\n'
)

function titleOf(body, route) {
  const m = body.match(/^#\s+(.+)$/m)
  return (m ? m[1] : route).trim()
}

console.log(`已同步 ${count} 个面向用户的页面，来源为 docs/`)
