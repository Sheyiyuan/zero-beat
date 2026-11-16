# 开发与发布

## 本地开发

所有命令都在仓库根目录（含 `go.work` 的目录）执行，不要 `cd` 进 `zero-beat-core/` 或 `zero-beat-tui/` 单独构建。

```bash
make help          # 查看全部入口
make check         # gofmt + vet + 两模块测试 + 文档链接
make build         # 输出 bin/zero
make run           # 直接运行 TUI
./bin/zero demo    # 不联网看界面
```

| 目标 | 用途 |
| --- | --- |
| `make check` | fmt-check、vet、两模块 test、文档链接、`git diff --check` |
| `make check-modules` | 临时消费目录 + `GOWORK=off`，验证独立模块 |
| `make check-deps` | Linux/macOS/Windows/Android 的 core 依赖边界与编译 |
| `make check-docs` | 只检查 Markdown 本地链接与空白错误 |
| `make site-install` / `site-build` | 站点依赖与生产构建（不需要进子目录） |
| `make verify` | `check` + `check-modules` + `check-deps` + `check-site` |
| `make tidy` | 用临时模块代理整理 go.mod/go.sum |
| `make clean` / `distclean` | 清构建产物 / 连站点 node_modules 一起清 |

站点源文件仍在 `zero-beat-site`，但统一从根目录用 `make site-build` 触发；`pnpm -C zero-beat-site ...` 仍然可用。

UI 的单元测试会校验整帧尺寸（每行宽度、总行数）与关键字，改布局时先跑 `go -C zero-beat-tui test ./internal/tui`。

本地音乐库相关逻辑分布在 `zero-beat-core/library`（扫描、标签与身份）、`zero-beat-core/source/local`（取流与本地歌词）与 `zero-beat-core/lyrics`（LRC、编码与翻译）；改扫描或歌词匹配时先跑 `go -C zero-beat-core test ./library ./source/local ./lyrics`。

运行中的 TUI 也可以用 `zero frame` 读当前帧、`zero +<按键>` / `zero +type` 注入输入，适合脚本或 agent 验证界面；见 [CLI 子命令](../reference/cli.md#控制通道)。

CI（GitHub Actions `ci.yml`）在 push 到 main 与 PR 上执行：gofmt、`make vet`、`make test`、`CGO_ENABLED=0 make build`、`make check-modules`、core 平台依赖检查、强制 go-git 同步回归、文档链接检查、`sh -n scripts/install.sh`、`goreleaser check`。

## 发布

```bash
make check
go -C zero-beat-core test -race ./...
go -C zero-beat-tui test -race ./...
make check-modules
# 发布顺序：core 模块标签 → TUI 模块标签 → 应用 vX.Y.Z 标签
# 版本规则见 ../monorepo-and-sync.md
```

发布前手工过一遍：本地文件夹歌单的添加/重扫/更换目录、下载目录复用身份、本地歌词的编码与翻译、`B` 绑定与失效降级、offset 随来源、双设备同步 v2 的仅本机数据保留；以及登录态过期后的自动退出与歌单冻结、单曲补偿、自动响度均衡、双设备首次加入、SSH/HTTPS 切换、歌单冲突的本地/远程/保留两者、换口令、Linux 系统媒体面板与封面。发布说明样例见 [v0.7.0](../release-notes-v0.7.0.md)。

GitHub Actions 的 `Release` 工作流会用 GoReleaser 构建：

- Linux / macOS / Windows（Windows 为未验证的交叉编译产物）
- amd64 / arm64
- tar.gz / zip
- `checksums.txt`
- 带 `scripts/install.sh` 的压缩包

产物上传到 GitHub Release；Homebrew tap 暂未自动发布。当前检出没有历史文档所述 `.aur/`；外部 `zero-beat-git` 源码包需要适配模块布局，`zero-beat-bin` 继续使用原资产名。本次不代表外部 AUR 已更新。

## 分层与契约

模块分层、数据流与事件循环约束见 [ARCHITECTURE.md](../../ARCHITECTURE.md)；已定下来的决策与理由见 [ADR 索引](../adr/README.md)。

模块拆分、独立构建、配套标签与双后端配置见[模块迁移指南](../monorepo-and-sync.md)。
