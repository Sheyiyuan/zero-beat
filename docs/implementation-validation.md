# 本次迁移的本地验证记录

日期：2026-11-16。代码位于现有 zero-beat 仓库工作树；尚未创建发布标签或发布资产。此记录区分已经运行的检查与仍待完成的验收，不代表整个发布验收已通过。

## 已通过

- Go 1.26.10，`GOWORK=off go -C zero-beat-core vet ./...`。
- `GOWORK=off go -C zero-beat-core test -race ./sync ./keymap ./player -count=1`。
- 同步包在默认 CLI 与强制 go-git 下运行既有集成场景；新增双后端测试覆盖三方合并树、父节点、未知字段/版本、旧冲突档案、输入变化、中断恢复、换密钥、目标分支切换、引用 CAS、后端切换和再次同步无额外提交。
- 远端在 fetch 与 push 之间抢先推进：两个后端均验证一次竞争后收敛、连续竞争最多三轮后返回拒绝、下一轮保留双方编辑。go-git 场景清空 PATH，准备裸仓库也不调用系统 Git。
- 无 Git 双设备初始化、冲突选择与合并祖先测试。SSH 私钥/known_hosts 回调验证可信主机通过、未知主机和主机密钥变化被拒；HTTPS token 交给内置传输。未用真实托管服务做认证验收。
- `scripts/check-core-deps.py` 在 Linux、macOS、Windows、Android arm64、CGO 关闭时编译 core，并检查依赖树不包含 TUI、Bubble Tea、oto 或 DBus。
- CLI 后端参数解析测试单独运行，通过前置/后置参数、等号形式及无效值；这不代表完整 TUI 应用已编译通过。
- 格式检查、`git diff --check`、安装脚本 shell 语法检查、Python 检查脚本解析、仓库文档链接检查。
- 网站迁移后的同步脚本生成 16 个用户页面，JavaScript 语法检查通过；Pages 配置已指向 zero-beat-site。
- GUI 原工作树已导入并记录来源提交，未复制嵌套 `.git`。迁移后源码的本地包引用静态检查没有缺失的导出名称。

## 尚未完成（迁移时记录；复审见文末）

> 下表为迁移当时的限制；2026-11-16 复审已补跑 TUI 构建/测试、`make check`、`check-modules` 与 core 全量测试，见文末。

| 验收 | 当前限制与后续命令 |
| --- | --- |
| TUI 依赖整理、test/vet/build、根 zero 构建 | 本机缺少 Bubble Tea、oto 等依赖归档；需运行 `make tidy`、`make check`、`CGO_ENABLED=0 make build`。TUI go.mod/go.sum 当前按原 TUI 与 core 依赖合并，尚未经过真实 tidy 验证。 |
| GOWORK=off 独立模块消费 | 临时模块代理脚本和 CI 步骤已加入；`make check-modules` 尚未端到端通过。它会在临时消费目录 test/vet/build，并拒绝构建期间改动 go.mod。 |
| core 全包测试 | 已尝试；app、cover、download、lrclib 等使用 httptest 本地监听的测试受沙箱禁止监听 TCP 影响，需在允许本地监听的环境运行 `GOWORK=off go -C zero-beat-core test ./...`。 |
| 完整 TUI 回归 | 播放、Bubble Tea 键位适配、同步页帧尺寸和冲突选择须随 TUI 依赖补齐后验证，手工音频回归尚未执行。 |
| 发布配置 | GoReleaser 配置已迁移，但本机无 goreleaser，`goreleaser check` 尚未运行。 |
| 网站生产构建 | 本机无站点 node_modules；需在 zero-beat-site 执行 `pnpm install --frozen-lockfile`、`pnpm build`。 |
| 外部 AUR | 当前检出没有 `.aur`，本次没有更新外部包。 |

依赖下载的提权命令多次因自动审批服务 HTTP 429 而未执行。返回说明是审批服务故障，并非判定命令不安全；未绕过审批或网络限制。上述检查已放入 CI 或开发命令，环境恢复后需补齐再发布。

## 2026-11-16 复审（自动验收）

在允许写入工作区 GOPATH、下载 Go 1.26.10 与依赖后，补跑并通过：

- `make check`（fmt-check、vet、两模块全量测试）通过；`make build` 通过，`bin/zero version` 输出 `zero 0.8.0`，`zero demo` 正常渲染。
- `GOWORK=off go -C zero-beat-core test ./...` 全量通过（含 sync 11.7s）；`GOWORK=off go -C zero-beat-tui test ./...` 全量通过。
- `go -C zero-beat-core test -race ./sync ./keymap ./player -count=1` 通过。
- `scripts/check-core-deps.py` 在 Linux/macOS/Windows/Android arm64、CGO 关闭下通过。
- `scripts/check-modules.py` 在临时模块代理、GOWORK=off 的独立消费目录 test/vet/build 通过。
- `scripts/check-doc-links.py` 检查 217 个本地链接目标通过；`git diff --check` 通过。
- `pnpm install --frozen-lockfile` + `pnpm build` 在 zero-beat-site 通过；修复了 editLink 闭包引用 `manifest` 导致的 SSR `ReferenceError`，构建无错误输出。
- 根目录 Makefile 统一入口验证：`make help`、`make check`、`make build`、`make version`、`make site-build`、`make verify` 全部通过；站点不再需要进入子目录构建。

复审发现并处理：

1. **默认 `make check` / `make build` 在打 tag 前不可用**：`go.work` 只有 `use`，TUI 的 `require zero-beat-core v0.8.0` 会尝试解析远端不存在的版本。已在 `go.work` 加入版本精确的本地 replace，并在文档中注明 tag 后移除；这不改变发布模块的 go.mod。
2. **GUI 文档存在两份工作副本**：`gui/` 独立仓库（工作树 28–38 人周、P5 8–11）与已导入的 `tui/zero-beat-gui`（21–29 人周、P5 1–2）不一致。`IMPORT.md` 声明后者为后续入口；本次导入版本与已实现的双 Git 后端一致。`gui/` 应冻结或归档，避免继续双写。
3. **文档内的旧路径**：`internal/player`、`internal/download`、`internal/instance`、`internal/desktop` 已改为 `zero-beat-core` / `zero-beat-tui` 下的实际路径。
4. **验证记录已过期**：原先“TUI 未编译、core 测试受沙箱阻塞、check-modules 未通过”等限制，在本次环境已解除。

仍未完成：真实 Android/macOS/Windows 设备或托管 Git 服务认证验收、`goreleaser check`、外部 AUR；迁移整体仍未提交。
