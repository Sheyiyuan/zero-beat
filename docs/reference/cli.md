# CLI 子命令

`zero` 不带参数即进入终端界面。全部子命令：

```bash
zero                        # 终端界面（等价 zero tui）
zero login bilibili         # 只扫码登录 B 站，保存 cookie
zero login netease          # 只扫码登录网易云，保存 cookie
zero play BV1GJ411x7h7      # headless 播放（B 站/网易云），验证音频链路
zero demo [view]            # 打印一帧示例界面（library/search/queue/lyrics/help/login/clip/picker/downloads/menu/about/selection/sync/conflicts）
zero frame [选项]           # 读取运行中实例的当前帧（--ansi / --json / --delay / --stable）
zero +<按键> [--force]      # 向运行中实例注入按键（键名同 keymap.toml）
zero +type <文本>           # 向当前输入态灌入文本
zero clean-playlists        # 清空已导入的远端歌单，保留本地歌单
zero theme [auto|dark|light]  # 查看/设置深/浅模式
zero theme palette <zero-red|hanpai-blue>  # 选择配色（佐洛红 / 半拍蓝）
zero theme --init|--check     # 生成/校验自定义颜色 theme.toml
zero desktop install        # 注册 Linux 应用菜单入口（含图标；uninstall 移除）
zero download <目标...>     # 下载单曲（-o 目录/文件，-q 档位，-j 并发数）
zero keymap --init          # 生成默认快捷键配置
zero keymap --check         # 校验快捷键配置
zero sync init --remote <url>   # 初始化/加入同步仓库（zero sync 查看全部子命令）
zero sync status|pull|push|conflicts|resolve|key rotate
zero --version              # 打印版本
```

`zero` 不带子命令时可用 `--help` / `-h` 查看用法。

## 控制通道

`zero frame` / `zero +<按键>` / `zero +type` 通过本机 Unix socket（`~/.config/zero-beat/control.sock`，0600）驱动正在运行的 TUI，适合脚本或自动化测试驱动界面：

```bash
zero frame --stable 2 --json   # 等画面稳定后输出状态快照 + 纯文本帧
zero +down                     # 注入 down，并打印操作后的帧
zero +space                    # 播放 / 暂停
zero +type "周杰伦"            # 需要先打开搜索框、命令面板等输入态
```

- 只有同一数据目录下正在运行的 TUI 才能连接；没有实例时直接报错。
- `--force` 才允许删除、同步这类默认被拒的按键；`+type` 只接受输入态，避免字符落进全局快捷键。
- `--ansi` 保留颜色；和 `--json` 同用时帧里也保留 ANSI。
- 控制通道目前仅 macOS / Linux 可用。

## 环境变量

| 变量 | 作用 |
| --- | --- |
| `ZERO_THEME` | 临时指定深浅模式（`auto` / `dark` / `light`），优先级高于数据库里的 `theme` 设置 |
| `ZERO_THEME_PALETTE` | 临时指定配色（`zero-red` / `hanpai-blue`），优先级高于数据库里的 `theme_palette` 设置 |
| `ZERO_MOUSE` | 设为 `0` 关闭鼠标接管，终端自身的右键菜单与拖选恢复 |
| `ZERO_SYSMEDIA` | 设为 `0` 关闭 Linux MPRIS 系统媒体注册 |

## 数据位置

| 路径 | 内容 |
| --- | --- |
| `~/.config/zero-beat/zero-beat.db` | SQLite：歌曲、歌单、队列、设置、登录态 |
| `~/.config/zero-beat/control.sock` | TUI 控制通道（仅实例运行时存在，0600） |
| `~/.config/zero-beat/keymap.toml` | 自定义快捷键 |
| `~/.config/zero-beat/theme.toml` | 自定义主题颜色（可选，0600） |
| `~/.config/zero-beat/sync.toml` | 同步配置（0600） |
| `~/.config/zero-beat/sync-key` | 派生密钥（0600） |
| `~/.config/zero-beat/sync-token` | HTTPS 令牌兜底（0600，可选） |
| `~/.config/zero-beat/sync/archive/` | 冲突落选版本归档 |
| `~/.config/zero-beat/cache/covers` | 系统媒体面板封面缓存（64 MiB 上限） |
| `~/Music/zero-beat` | 下载目录（`-o` 可覆盖） |

## 实例锁

同一数据目录只允许一个 TUI 实例，锁文件记录 PID；`zero download` / `zero sync` 等 CLI 子命令不占实例锁。

## 相关页面

- 主题、配色与自定义颜色见[主题](../guide/theme.md)。
- 下载参数详解见[下载与本地优先](../guide/download.md)。
- 同步子命令见[跨设备同步](../guide/sync.md)。
