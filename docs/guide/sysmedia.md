# 系统媒体与封面（Linux）

Linux 下启动后会自动向会话 D-Bus 注册 MPRIS 播放器（`org.mpris.MediaPlayer2.zero`），系统媒体面板、锁屏和媒体键可以控制播放/暂停、上一首/下一首、停止、进度与音量，并显示当前曲目、时长、播放状态与封面。

## 能做什么

- 播放/暂停、上一首、下一首、停止、进度拖动、音量调节由系统媒体面板或媒体键下发。
- 媒体键的具体按键取决于桌面环境；MPRIS 只写本机会话总线，不额外上传任何数据。

## 封面

封面按需从平台 CDN 下载，缓存到 `~/.config/zero-beat/cache/covers`，上限 64 MiB；失效 URL 会重新下载。

封面**只给系统媒体面板读取**，终端界面不显示封面，也不接 kitty/sixel 图形协议。

## 关闭与限制

- 不希望被接管时用 `ZERO_SYSMEDIA=0 zero` 启动。
- macOS / Windows 暂无系统媒体集成，也没有桌面通知。
- MPRIS 名称被占用时会明确报错，而不是注册成第二个播放器。

## 单实例

同一数据目录只允许一个 TUI 实例；重复启动会提示旧实例 PID，避免两个 MPRIS 播放器抢控制权。`zero download` / `zero sync` 等 CLI 子命令不占实例锁。

设计与取舍见 [ADR 0017](../adr/0017-system-media.md)。
