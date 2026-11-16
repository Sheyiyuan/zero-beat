# 模块迁移与双 Git 后端

本次已运行的检查和环境阻塞见[本地验证记录](implementation-validation.md)；TUI 自动测试已跑通，手工音频、真机与发布验收尚未完成。

## 当前结构

现有 zero-beat Git 仓库是唯一代码主线，仓库根目录（含 `go.work`）就是 monorepo 根。仓库内包括：

| 目录 | 职责 |
| --- | --- |
| zero-beat-core | 公开业务包、媒体类型、数据和同步 |
| zero-beat-tui | zero-beat-tui/cmd/zero、终端界面、桌面音频和媒体控制 |
| zero-beat-gui | 导入的 GUI 设计文档，未来 Flutter 与 RPC 入口 |
| zero-beat-site | VitePress 网站包、站点配置和文档生成脚本 |

两个 Go module 都使用 `github.com/sheyiyuan/zero-beat/<目录名>`。根 `go.work` 用于本地联调。GUI 目前没有 Go module；实现入口后再加入工作区。原 GUI 仓库保留历史，导入来源见 [IMPORT](../zero-beat-gui/IMPORT.md)。

core 不依赖 TUI、Bubble Tea、oto 或 DBus。完整 Session 门面和 Android 下载适配尚未实现；本轮保持原业务接口和桌面下载行为。GUI 将独立打包，Dart 包标识使用 zero_beat_gui。

迁移清单：原 `internal/{app,bili,config,cover,download,downloader,instance,library,lrclib,lyrics,netease,player,source,store,sync}` 移到 core 的公开包；纯媒体类型归 core/media，键位解析与校验归 core/keymap。原 `cmd/zero`、`internal/{audio,control,desktop,sysmedia,tui}` 与嵌入图标包归 TUI；TUI/internal/keymap 只适配 Bubble Tea 按键。原 `site/` 整体迁到 zero-beat-site，根 docs/assets 仍是网站内容来源。

## 构建与发布

- 所有命令统一从仓库根目录执行：`make help` 查看入口，`make check` 检查两个模块与文档，`make build` 输出 bin/zero。
- `make verify` 汇总本地验收：`check`、独立模块消费、四平台 core 依赖检查、站点构建；真实设备与托管 Git 服务仍需单独验证。
- 站点统一用 `make site-install` / `make site-build` 构建（内部调用 `pnpm`），不再要求进入 `zero-beat-site` 手动执行；用户文档来源仍为根 `docs/`，网址保持不变。
- `make tidy` 使用临时模块代理整理依赖；不在 go.mod 提交兄弟模块相对路径 replace。core/v0.8.0 打 tag 前，go.work 用版本精确的 `replace` 指向本地 core，tag 后移除。
- `make check-modules` 在临时消费目录、GOWORK=off 下运行 test/vet/build。
- `python3 scripts/check-core-deps.py` 检查 Linux、macOS、Windows、Android 的核心依赖和编译；这不是 GUI 平台功能验收。
- 网易云依赖的第三方 replace 必须出现在每个消费模块，不能仅依赖 core 的 replace。
- 首次模块版本配套使用 v0.8.0。先发布 `zero-beat-core/v0.8.0`，再发布 `zero-beat-tui/v0.8.0`；`v0.8.0` 触发现有应用发布流水线。后续版本同样先 core 后 TUI，最后应用标签。
- 保持 zero 命令、zero_<version>_<os>_<arch> 安装资产和 releases/latest 入口。未来 GUI 发布须设为非 latest，不抢占 TUI 安装入口。
- 源码安装路径改为 `github.com/sheyiyuan/zero-beat-tui/cmd/zero@版本`；旧根模块路径仅对应历史版本。
- 当前检出没有 .aur 目录；外部 AUR 源码包维护者需更新构建工作目录，二进制包继续使用原资产名。

## 同步配置

```toml
remote = "https://github.com/example/music-sync.git"
branch = "main"
auto = true
backend = "auto"
# go-git SSH 可选项；加密私钥请通过 SSH agent 解锁。
# ssh_key = "/home/me/.ssh/id_ed25519"
# known_hosts = "/home/me/.ssh/known_hosts"
```

`zero sync --backend gogit pull` 或 `zero sync pull --backend gogit` 可覆盖本次调用。优先级：命令参数 > ZERO_SYNC_BACKEND > sync.toml > auto。Linux/macOS 的 auto 探测可用 Git 后优先 CLI，Windows/Android 优先 go-git。每次操作固定后端，网络、认证和冲突失败不会切换后端。

HTTPS token 继续使用 sync-token 文件。CLI 保留 credential helper 与 GIT_SSH_COMMAND；go-git 使用 token、SSH agent 或私钥，并验证 known_hosts，不执行这两种外部 Git 认证配置。首次 SSH 连接需先提供可信主机密钥。状态页显示请求的后端与上次实际后端。

## 数据与恢复

远端仍使用 v2 快照格式，字段和时间单位不变。合并输入是固定提交的 base/local/remote，而非 Git index stages。同一实体双方不同修改需要选择；不同实体自动合并，删除与空文件不同。账号沿用 LWW，分叉成功生成两个父提交；无共同祖先的非空历史和多个最佳共同祖先明确拒绝。

受管文件在导入、覆盖导出前严格校验；未知字段和版本会停止写同步。所有同步仓库修改先持进程间锁。冲突记录固定提交和本地数据摘要；恢复时重新 fetch，输入改变后归档旧事务并重新计算，旧选择不会应用到新冲突。旧版冲突仍可查看或归档，但不能直接提交。

transaction.json 仅在本地保存阶段和提交 ID。首次加入在检出前记录 joining 阶段，重启后先完成保留本地数据的导入。未完成导入的合并恢复到导出后的本地父提交，再结合当前数据库重新计算；已导入但未推送的提交留待重试。最多三轮非快进重试，不 force push。切换后端只在操作边界发生。

换密钥前写入仅本机的 0600 恢复日志；重启后先检查远端是否已接受新 meta，再决定保留新密钥或恢复旧密钥。网络失败时保留日志，避免把不确定的 push 结果当成失败回滚。
