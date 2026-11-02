# 跨设备同步

歌单、混合歌单、`keymap.toml`、单曲歌词偏移/片段与加密后的登录态可以同步到任意 git 仓库（GitHub 私有仓库、Gitea、自建裸仓库都行），仓库需要自己创建。

## CLI

```bash
zero sync init --remote git@github.com:me/zero-beat-sync.git --branch main
zero sync status
zero sync pull          # 拉取、合并、导入
zero sync push          # 导出、提交、合并、推送
zero sync conflicts     # 查看冲突实体
zero sync resolve <tx> --all local|remote
zero sync key rotate    # 换口令并重加密登录态
```

`zero sync` 可以查看全部子命令。

## 同步页

TUI 里按 `ctrl+s` 打开同步页，也可以点右下角的同步状态。页面包含状态、仓库与分支、连接方式（SSH/HTTPS）、密钥、自动同步、冲突入口和归档目录。

| 操作 | 说明 |
| --- | --- |
| `↑` / `↓` + `enter` | 选择操作；在仓库地址、分支、口令、令牌行上直接打字即可编辑（不必先按 enter） |
| `space` | 勾选「歌单会以明文托管」；未勾选时不能初始化 |
| `连接方式` | 在 SSH 与 HTTPS 之间切换；`sync-key` 按归一化仓库身份识别，切换后密钥与登录态不失效 |
| 同步口令 | 至少 12 个字符，输入不回显；确认后行内显示「已输入」 |
| 粘贴 | 编辑仓库地址、口令、令牌时使用终端粘贴（`ctrl+shift+v`、`cmd+v` 或中键） |

首次初始化必须勾选「歌单会以明文托管」；同步口令从终端无回显读取，只在本机内存里短暂停留，提交后立即从界面模型清除。

## 冲突页

| 操作 | 说明 |
| --- | --- |
| `l` / `r` / `b` | 逐项保留本地 / 远程 / 两者（两者仅歌单） |
| `L` / `R` | 全部本地 / 全部远程 |
| `enter` | 全部选择后应用、提交 merge commit 并推送 |
| `esc` | 返回同步页 |

落选版本归档到 `~/.config/zero-beat/sync/archive/`，不丢数据。

## 连接方式

- `git@host:...` / `ssh://...` 走 SSH（系统 ssh-agent 与 `~/.ssh/config`）。
- `https://...` 走 credential helper 或 0600 令牌。HTTPS 认证优先用系统 credential helper；也可以把 token 存成 `sync-token`（0600）作为兜底，经 `GIT_ASKPASS` 注入，不进入命令行与 git 配置。

## 自动同步

自动同步默认开启：启动拉取，歌单/键位/注释/登录态变化后 debounce 30 秒推送；网络失败按 5s → 30s → 2m → 10m 退避；出现冲突时冻结自动同步。

## 安全模型

- **歌单与注释是明文 JSON**：私有仓库只提供访问控制；删除不会抹掉 git 历史。
- **登录态是密文**：Argon2id（64 MiB、3 轮）+ XChaCha20-Poly1305，每个音源一个文件；口令不上云，新设备输入一次即可。本机只缓存派生密钥 `sync-key`（0600）。
- 同一实体两端都改才进冲突；逐项选择保留本地/远程/保留两者。
- 登录态冲突按 `updated_at` 后写者胜，不弹窗；登出不传播到其他设备。
- 队列、下载文件、音量/音质/模式/鼠标等设备态不同步。
- 同步需要系统 `git`；Windows 未适配。
- 同步登录态可能触发平台风控：失效时回落到扫码登录。

同步是最终一致，不是实时协作，依赖 git 的延迟与冲突模型。设计与威胁模型详见 [docs/sync-design.md](../sync-design.md) 与 [ADR 0016](../adr/0016-git-sync.md)。
