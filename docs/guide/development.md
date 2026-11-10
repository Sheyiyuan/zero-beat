# 开发与发布

## 本地开发

```bash
make check        # gofmt 检查 + go vet + go test
make build
./bin/zero demo   # 不联网看界面
```

UI 的单元测试会校验整帧尺寸（每行宽度、总行数）与关键字，改布局时先跑 `go test ./internal/tui`。

本地音乐库相关逻辑分布在 `internal/library`（扫描、标签与身份）、`internal/source/local`（取流与本地歌词）与 `internal/lyrics`（LRC、编码与翻译）；改扫描或歌词匹配时先跑 `go test ./internal/library ./internal/source/local ./internal/lyrics`。

运行中的 TUI 也可以用 `zero frame` 读当前帧、`zero +<按键>` / `zero +type` 注入输入，适合脚本或 agent 验证界面；见 [CLI 子命令](../reference/cli.md#控制通道)。

CI（GitHub Actions `ci.yml`）在 push 到 main 与 PR 上执行：gofmt、`go vet`、`go test`、`CGO_ENABLED=0 go build ./...`、`sh -n scripts/install.sh`、`goreleaser check`。

## 发布

```bash
make check
go test -race ./...
git tag v0.7.0
git push origin main v0.7.0
```

发布前手工过一遍：本地文件夹歌单的添加/重扫/更换目录、下载目录复用身份、本地歌词的编码与翻译、`B` 绑定与失效降级、offset 随来源、双设备同步 v2 的仅本机数据保留；以及登录态过期后的自动退出与歌单冻结、单曲补偿、自动响度均衡、双设备首次加入、SSH/HTTPS 切换、歌单冲突的本地/远程/保留两者、换口令、Linux 系统媒体面板与封面。发布说明样例见 [v0.7.0](../release-notes-v0.7.0.md)。

GitHub Actions 的 `Release` 工作流会用 GoReleaser 构建：

- Linux / macOS / Windows（Windows 为未验证的交叉编译产物）
- amd64 / arm64
- tar.gz / zip
- `checksums.txt`
- 带 `scripts/install.sh` 的压缩包

产物上传到 GitHub Release；Homebrew tap 暂未自动发布。AUR 打包在 `.aur/aur-repo`（包名 `zero-beat-git`），由 `update-srcinfo.sh` 维护。

## 分层与契约

模块分层、数据流与事件循环约束见 [ARCHITECTURE.md](../../ARCHITECTURE.md)；已定下来的决策与理由见 [ADR 索引](../adr/README.md)。
