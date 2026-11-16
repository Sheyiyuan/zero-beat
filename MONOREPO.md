# zero-beat 仓库关系

本仓库是 **zero-beat 的 monorepo 主仓**，负责全部源码、Go 工作区、站点和发布打包。

| 仓库 | 关系 |
| --- | --- |
| `Sheyiyuan/zero-beat` | 本仓库；monorepo 主仓 + 打包入口 |
| `Sheyiyuan/zero-beat-core` | 由本仓库 `zero-beat-core/` 通过 `git subtree` 推送的镜像 |
| `Sheyiyuan/zero-beat-gui` | 由本仓库 `zero-beat-gui/` 通过 `git subtree` 推送的镜像 |
| `Sheyiyuan/zero-beat-site` | 由本仓库 `zero-beat-site/` 通过 `git subtree` 推送的镜像 |
| `Sheyiyuan/zero-beat-tui` | 原 zero-beat 仓库改名保留；保留 TUI 的 README 与历史 |

`zero-beat-tui` 保留原仓库的历史与 README，不在本次迁移中强制改写；TUI 代码的当前主线在 monorepo 的 `zero-beat-tui/` 下。

## 关联方式

本机添加组件远程（一次性）：

```bash
git remote add core git@github.com:Sheyiyuan/zero-beat-core.git
git remote add gui git@github.com:Sheyiyuan/zero-beat-gui.git
git remote add site git@github.com:Sheyiyuan/zero-beat-site.git
```

把 monorepo 的目录同步到组件仓库：

```bash
make publish-mirrors
# 等价于：
# git subtree push --prefix=zero-beat-core core main
# git subtree push --prefix=zero-beat-gui gui main
# git subtree push --prefix=zero-beat-site site main
```

`git subtree` 只把对应目录及其历史推送出去，不复制 `.git`，也不改变 monorepo 的工作树。组件仓库默认只作为镜像/独立发布入口；日常开发仍在本 monorepo 进行。

## 打包

- `make check` / `make build`：本仓库根目录构建 `bin/zero`；
- `make verify`：包含两模块测试、独立模块消费、四平台 core 依赖检查和站点构建；
- 发布标签：先 `zero-beat-core/vX.Y.Z`，再 `zero-beat-tui/vX.Y.Z`，最后应用 `vX.Y.Z`；
- 站点由 `zero-beat-site` 独立构建，但仍从本仓库的 `docs/` 同步生成。

## 注意

- 组件仓库的首次推送会创建 `main` 分支；推之前确认远程为空或允许覆盖；
- 不要在两个地方同时手改同一目录；镜像只接受从 monorepo 的 subtree 推送；
- `zero-beat-tui` 的旧 README 保持原样，新的 monorepo 说明以本文件为准。
