# 概览

## 背景

zero-beat 是一个 Go 写的 B 站 + 网易云终端播放器，已经有完整的曲库、歌单、搜索、登录、下载、歌词和跨设备同步。TUI 覆盖终端用户，但四类场景覆盖不到：

1. **手机**：没有终端，更需要触屏、后台播放和系统媒体控制；
2. **Linux 桌面图形环境**：想要托盘、小窗、系统媒体面板，而不是常驻一个终端窗口；
3. **macOS 桌面**：TUI 虽然支持 macOS，但图形环境需要原生窗口、菜单栏、Now Playing；GUI 直接复用 TUI 的数据。
4. **Windows 桌面**：TUI 在 Windows 上难以适配（终端、字体、键盘、ffmpeg），GUI 没有这个问题。

GUI 的目标不是替代 TUI，而是让同一份数据在手机和桌面图形环境里可用。Android + Linux + macOS 是 v0.1；Windows 作为 P7 候选（见 [10-windows](10-windows.md)）。

## 目标用户与场景

- 在 Android 手机上听 B 站/网易云，锁屏/通知里能控制，通勤时离线听已下载的歌；
- 在 Linux 桌面用图形界面管理歌单、搜歌，关掉主窗口后小窗或托盘继续放；
- 在 macOS 上用同一份数据，菜单栏/Now Playing 可控，关窗隐藏而不是退出；
- 同时是 TUI 用户：桌面端和 TUI 交替使用，歌单/注释/登录态通过同步仓库保持一致。

## 目标（v1）

- Android：曲库、歌单、搜索、队列、播放、歌词、扫码登录、下载、同步、后台播放、媒体通知/锁屏。
- Android 下载保存经过验证的完整媒体流，支持容器由 P1 真机样本锁定；保留原容器与标签，应用内元数据由曲库提供（[ADR-0009](decisions/0009-android-download.md)）。
- Linux：以上能力 + 托盘、迷你窗口、MPRIS、与 TUI 互斥。
- macOS：以上能力 + 菜单栏、迷你窗口、Now Playing、非沙盒共享 TUI 数据、DMG 公证（见 [11-macos](11-macos.md)）。
- 数据：与 TUI 共用桌面数据目录（Linux `~/.config/zero-beat/`、macOS `~/Library/Application Support/zero-beat`）；与其它设备共用 sync v2 仓库；写入后 TUI 能正常读取。
- 播放：保留自动响度均衡（loudnorm）、全局/单曲增益、片段播放、断点续播、预取与失败重试。
- Windows：**不在 v1**，作为 P7 候选；能力对齐后再纳入发布矩阵。

## 非目标（v1）

- iOS：不编译、不适配、不测试（与 macOS 无关；GUI 不做 iOS 目标）。
- Windows：v1 不做；P7 再评估（见 [10-windows](10-windows.md)）。
- 替换 TUI，或让 TUI 变成 GUI 的客户端。
- Android 上的本地文件夹歌单（SAF 目录树）、外部播放器投送、Android Auto。
- Android 下载后的转码、重封装和新增嵌入标签；HLS/DASH 清单及不完整媒体碎片的下载。
- 插件系统、主题商店、在线歌词编辑。

## 兼容承诺

| 契约 | 承诺 |
| --- | --- |
| sync 仓库 | 保留 `meta.v=2` 现有字节格式、字段类型和单位；已支持 v2 的 TUI 可同步同一仓库；不承诺未知字段透传，写前严格校验 |
| SQLite | 桌面先取得实例锁，再由 Go 核心打开和迁移；GUI 不直接读写 |
| 数据目录 | 桌面复用 TUI 目录规则，Linux 尊重 `XDG_CONFIG_HOME`；macOS 为 `~/Library/Application Support/zero-beat`（非沙盒）；Windows 为 `%APPDATA%\zero-beat`；Android 在应用沙盒内 |
| 键位 | 读取并尊重 `keymap.toml`（桌面键盘快捷键）；GUI 另有自己的快捷键层 |
| 实例锁 | 桌面 GUI 与 TUI 主界面互斥；CLI play/download 尚未纳入，限制见生命周期文档 |

## 术语

| 术语 | 含义 |
| --- | --- |
| **core** | Go 核心；Linux/Android 为 `libzero.so`、macOS 为 `libzero.dylib`，在 App 进程内运行；Windows 为 `zero-core.exe` sidecar |
| **engine** | Dart 侧音频引擎，封装 media_kit/libmpv，实现 Go `player.Engine` 的远端版本 |
| **controller** | Go `player.Controller`，负责队列、模式、预取、重试、持久化 |
| **repo** | 同步用的 git 仓库（`sync/repo`），跨设备唯一契约 |
| **device state** | 队列、下载、音量、模式、鼠标偏好等只跟设备走的数据 |
| **TUI** | 现有 zero-beat 终端界面 |

## 设计原则

1. **单一数据源**：SQLite 和 sync 仓库只由 Go 核心写；Flutter 只通过 RPC 读写业务数据。
2. **播放决策留在 Go**：复用 `player.Controller`；session 保留异步请求号和重试预算，RPC 事件带播放代次，补跨语言乱序/超时测试。
3. **播放输出留在 Dart**：解码、输出设备、音频焦点、通知、锁屏、后台，全部交给平台生态。
4. **模块边界**：GUI 只依赖公开 `session` / `sync`，核心依赖树不含 oto/Bubble Tea；core/TUI 模块已拆分，GUI Session 后续补齐，不复制同步代码（见 [ADR-0006](decisions/0006-module-boundary.md)）。
5. **平台差异用 build tag 和依赖注入解决**，不在业务代码里写 `if android`。
6. **先 spike 再承诺**：先验证播放协议、后台/loudnorm、Android 离线下载、go-git 分叉合并、Linux 输入法与 macOS 系统集成；生产恢复和完整兼容性另设阶段验收，不将原型通过视为产品完成。

## 成功标准

- 手机锁屏 30 分钟播放不中断，通知/耳机键可控；
- 手机在无 ffmpeg 的环境完成两音源支持样本的下载、取消/续传与飞行模式播放；
- 桌面关窗后小窗继续放，MPRIS/Now Playing 可控，TUI 启动时得到明确「已在运行」提示；
- macOS 关窗隐藏、Cmd+Q 退出、Dock 恢复，公证后的 DMG 能直接打开；
- 手机与桌面同步后，可同步歌单、歌词偏移、单曲补偿、登录态一致；分叉合并保留双方祖先，再次同步无内容变化；
- 同一首歌在 GUI 与 TUI 上的响度听感接近（A/B 主观 + 响度计）；
- 断网/换网/链接过期时至多自动重试一次；仍失败则停止并提示。快速切歌不会被旧解析/事件覆盖；RPC 断线后停止旧播放，重连不自动起播。

Windows（P7）另有一套验收标准：SMTC 可控、与 TUI zip 共用数据、go-git 同步可用，见 [10-windows](10-windows.md)。
