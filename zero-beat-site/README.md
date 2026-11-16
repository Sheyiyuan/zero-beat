# zero-beat-site

zero-beat 的 VitePress 网站包。用户文档以仓库根 `docs/` 为唯一来源，图片来自 `docs/images/` 与 `assets/`；同步生成的页面不提交。

在本目录运行（Node.js 22、pnpm 10）：

```sh
pnpm install --frozen-lockfile
pnpm dev
# 生产构建
pnpm build
```

`dev` 和 `build` 都先运行 `scripts/sync.mjs`。仅检查文档同步可运行 `node scripts/sync.mjs`。

根 `.github/workflows/pages.yml` 独立构建和发布站点；`SITE_BASE` 控制部署基础路径。目录迁移不改变线上地址：https://sheyiyuan.github.io/zero-beat/ 。
