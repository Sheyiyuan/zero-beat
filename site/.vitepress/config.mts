import fs from 'node:fs'
import path from 'node:path'
import { defineConfig } from 'vitepress'

// 侧栏由 pnpm sync 生成的 manifest 拼装，新增文档不用改这里
const manifest = JSON.parse(
  fs.readFileSync(path.resolve(import.meta.dirname, 'manifest.json'), 'utf8')
)

// GitHub Pages 项目页部署在 /<repo>/ 下；本地 dev 默认根路径。
const base = (process.env.SITE_BASE || '/').replace(/\/+$/, '/')

const order = {
  guide: [
    'install',
    'quickstart',
    'library',
    'playback',
    'download',
    'sync',
    'sysmedia',
    'theme',
    'limitations',
    'privacy',
  ],
  reference: ['keys', 'keymap', 'cli'],
}

function group(title, dir, ranked) {
  const items = manifest.filter((p) => p.route.startsWith(`/${dir}/`))
  const sorted = items.sort((a, b) => {
    const ka = ranked.indexOf(path.basename(a.route))
    const kb = ranked.indexOf(path.basename(b.route))
    return (ka < 0 ? 99 : ka) - (kb < 0 ? 99 : kb)
  })
  return { text: title, items: sorted.map((p) => ({ text: p.title, link: p.route })) }
}

const changelog = manifest
  .filter((p) => p.route.startsWith('/changelog/'))
  .sort((a, b) => b.route.localeCompare(a.route))
  .map((p) => ({ text: p.title, link: p.route }))

export default defineConfig({
  lang: 'zh-CN',
  title: 'zero-beat 零拍',
  description: 'B 站 + 网易云音乐终端播放器：双源搜索、混源队列、逐行歌词、音量均衡、下载与跨设备同步',
  // GitHub Pages 项目页部署在 /<repo>/ 下；本地 dev 默认根路径。
  base,
  // GitHub Pages 不做 clean URL 重写，统一输出 .html。
  cleanUrls: false,
  lastUpdated: true,
  head: [
    ['link', { rel: 'icon', type: 'image/png', href: `${base}assets/icons/appicon-256.png` }],
    ['meta', { property: 'og:title', content: 'zero-beat · 零拍' }],
    ['meta', {
      property: 'og:description',
      content: 'B 站 + 网易云音乐终端播放器，双源搜索、混源队列、逐行歌词、音量均衡、跨设备同步',
    }],
  ],

  themeConfig: {
    logo: '/assets/icons/appicon-round.svg',
    siteTitle: 'zero-beat',
    nav: [
      { text: '指南', activeMatch: '/guide/', items: [
        { text: '安装', link: '/guide/install' },
        { text: '快速上手', link: '/guide/quickstart' },
        { text: '跨设备同步', link: '/guide/sync' },
        { text: '已知限制', link: '/guide/limitations' },
      ] },
      { text: '参考', activeMatch: '/reference/', link: '/reference/keys' },
      { text: '更新', activeMatch: '/changelog/', items: changelog.slice(0, 3) },
    ],
    sidebar: [
      group('用户手册', 'guide', order.guide),
      group('参考', 'reference', order.reference),
      { text: '发布说明', items: changelog },
    ],
    socialLinks: [{ icon: 'github', link: 'https://github.com/Sheyiyuan/zero-beat' }],
    editLink: {
      pattern: 'https://github.com/Sheyiyuan/zero-beat/edit/main/:path',
      text: '在 GitHub 编辑本页',
    },
    footer: {
      message: 'AGPL-3.0 · 本项目不提供、不托管任何音源内容，音频与封面版权归 B 站、网易云及权利人所有。',
      copyright: 'zero-beat · 零拍',
    },
    search: { provider: 'local' },
    outline: { level: [2, 3], label: '本页目录' },
    docFooter: { prev: '上一页', next: '下一页' },
    darkModeTitle: '深色模式',
    returnToTopLabel: '回到顶部',
    sidebarMenuLabel: '目录',
    lastUpdatedText: '最后更新',
  },
})
