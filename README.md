# zero-beat · 零拍

zero-beat 的 **monorepo / 打包仓库**。共享业务、TUI、GUI 和网站分别位于独立仓库，通过 git submodule 关联；本仓库负责统一构建、检查与发布打包。

TUI 的 README 仍保留在 [zero-beat-tui](zero-beat-tui) 子模块中，内容保持原样。

## 仓库关系

| 路径 | 仓库 | 职责 |
| --- | --- | --- |
| `zero-beat-core/` | [zero-beat-core](https://github.com/Sheyiyuan/zero-beat-core) | 共享 Go 模块：app/store/source/player/sync/media 等 |
| `zero-beat-tui/` | [zero-beat-tui](https://github.com/Sheyiyuan/zero-beat-tui) | TUI 应用；`go.mod` 依赖 `zero-beat-core v0.8.0` |
| `zero-beat-gui/` | [zero-beat-gui](https://github.com/Sheyiyuan/zero-beat-gui) | GUI 设计文档与未来 Flutter 客户端 |
| `zero-beat-site/` | [zero-beat-site](https://github.com/Sheyiyuan/zero-beat-site) | VitePress 网站包 |
| `docs/`、`assets/` | 本仓库 | 用户文档与站点素材；`:site` 从根目录同步生成 |
| `scripts/`、`Makefile` | 本仓库 | 统一检查、构建、打包与发布 |

TUI 依赖 core 的方式是 **Go module**，不是把 core 源码放进 TUI；本仓库的 `go.work` 只在本地联调时把 `zero-beat-core` 指到子模块。

## 克隆

```bash
git clone --recurse-submodules git@github.com:Sheyiyuan/zero-beat.git
cd zero-beat
make submodules   # 已递归克隆时可跳过
```

## 构建与检查

```bash
make help
make check        # fmt/vet/两模块测试/文档链接
make build        # bin/zero
make run          # 直接运行 TUI
make verify       # check + 独立模块 + 四平台依赖 + 站点构建
make site-build   # 构建 zero-beat-site
make version
```

发布相关：

```bash
make release-check   # goreleaser check
make release         # goreleaser release --clean
```

发布顺序：`zero-beat-core/vX.Y.Z` → `zero-beat-tui/vX.Y.Z` → 本仓库应用 `vX.Y.Z`。组件仓库的模块标签由各自仓库维护。

## 数据与文档

- 用户文档以本仓库 `docs/` 为唯一来源；
- 站点构建时把 `docs/`、`assets/` 同步进 `zero-beat-site` 子模块；
- GUI 设计文档在 `zero-beat-gui/` 子模块；
- 同步仓库仍为 v2；TUI 与 GUI 共用 core 的数据/同步实现。

## 许可

[AGPL-3.0](LICENSE)。各子模块保留自己的 LICENSE；打包发布需同时提供对应源码版本。
