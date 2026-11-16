# zero-beat-site

zero-beat 主仓库内的 VitePress 网站目录，统一发布到 <https://sheyiyuan.github.io/zero-beat/>。core、TUI、GUI 是 Git 子模块；site 由主仓库直接跟踪。

## 内容来源

- TUI 用户指南、参考、发布说明和截图：`../zero-beat-tui/docs/`。保留现有 `/guide/`、`/reference/`、`/changelog/` 和 `/images/` 地址。
- GUI 概览与设计文档：`../zero-beat-gui/README.md` 和 `../zero-beat-gui/docs/`，生成 `/gui/` 栏目，所有页面标注设计阶段。
- 网站首页、导航、主题：本目录；品牌素材：主仓库 `../assets/`。
- 集成与统一发布说明：主仓库 `../docs/`，需要时链接到 GitHub。

构建使用主仓库锁定的子模块提交，不拉取组件最新分支。生成页面和图片不提交，也不直接编辑；页面的“在 GitHub 编辑本页”会跳到源仓库。未生成到网站的源码/设计链接使用源仓库提交地址，避免跨子模块的 GitHub 链接失效。

## 开发与构建

需要 Node.js 22、pnpm 10。首次克隆主仓库时使用 `--recurse-submodules`；已有检出先在主仓库运行 `make submodules`。

从主仓库执行：

```sh
make site-install
make site-dev
make site-build
```

也可以在本目录执行 `pnpm dev` 或 `pnpm build`；二者都先运行 `scripts/sync.mjs`。仅验证内容同步可运行 `node scripts/sync.mjs`。

主仓库 `.github/workflows/pages.yml` 在本站源码、组件指针、主仓库文档或素材变更后部署。`SITE_BASE` 控制部署基础路径，GitHub 项目页使用 `/zero-beat/`。

迁移来源与历史备份见 [IMPORT.md](IMPORT.md)。
