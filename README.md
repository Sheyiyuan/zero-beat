# zero-beat · 零拍

zero-beat 的**集成与发布仓库**。共享业务、TUI、GUI 分别位于独立仓库，通过 git submodule 关联；网站源码直接放在本仓库的 `zero-beat-site/`。本仓库负责统一构建、检查、应用打包与网站发布。

TUI 的使用说明见 [zero-beat-tui](zero-beat-tui) 子模块。

> **v0.8.0 未发布，等待 GUI 完成后统一发布。** 已完成的评论、相关推荐、用户／歌手主页、播客电台、键盘交互及同步调整汇总在 [0.8.0 更新说明](zero-beat-tui/docs/release-notes-v0.8.0.md)。本地构建的 `0.8.0` 标识不代表正式发版；GUI 已有环境配置、双端设计预览与 Dart 类型绑定，尚无完整应用。

## 仓库关系

| 路径 | 仓库 | 职责 |
| --- | --- | --- |
| `zero-beat-core/` | [zero-beat-core](https://github.com/Sheyiyuan/zero-beat-core) | 共享 Go 模块：app/store/source/player/sync/media 等 |
| `zero-beat-tui/` | [zero-beat-tui](https://github.com/Sheyiyuan/zero-beat-tui) | TUI 应用；`go.mod` 依赖 `zero-beat-core v0.8.0` |
| `zero-beat-gui/` | [zero-beat-gui](https://github.com/Sheyiyuan/zero-beat-gui) | GUI 开发文档、双端 SVG 预览、Go/Dart 类型绑定与环境脚本 |
| `zero-beat-site/` | 本仓库普通目录 | VitePress 网站；读取 TUI/GUI 子模块文档 |
| `docs/`、`assets/` | 本仓库 | 集成、架构与发布文档，以及网站品牌素材 |
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
make check-gui-bindings # Go/Dart 数据契约与生成漂移检查（需 Dart）
make build        # bin/zero
make run          # 直接运行 TUI
make verify       # check + 独立模块 + 四平台依赖 + 站点构建
make site-build   # 构建 zero-beat-site
make version
```

发布相关（仅供 GUI 完成并通过联合验收后使用；当前不执行）：

```bash
make release-check   # goreleaser check
make release         # goreleaser release --clean
```

发布顺序：core 仓库的 `vX.Y.Z` → TUI 仓库的 `vX.Y.Z` → 更新本仓库子模块指针并发布应用 `vX.Y.Z`。Go module 位于各组件仓库根，标签不带目录前缀。

## 数据与文档

- TUI 用户文档、截图与发布说明只在 `zero-beat-tui/docs/` 维护；
- GUI 开发文档、桌面／移动端两套设计与数据契约在 `zero-beat-gui/` 维护，网站区分已验证准备项与待实现功能；
- 网站读取本仓库锁定的子模块版本，生成页面不提交；编辑链接返回对应源仓库；
- 网站文件或组件子模块指针更新时，主仓库 Pages 工作流重新构建；
- 主仓库 `docs/` 保留集成与发布说明，网站维护方式见 [zero-beat-site/README.md](zero-beat-site/README.md)；
- 同步默认不包含登录态；新建仓库使用支持无口令初始化的 v3，已有 v2 加密仓库仍可使用。TUI 与 GUI 共用 core 的数据/同步实现。

## 许可

[AGPL-3.0](LICENSE)。各子模块保留自己的 LICENSE；打包发布需同时提供对应源码版本。
