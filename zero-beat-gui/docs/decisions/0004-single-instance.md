# ADR-0004：单进程、单实例与窗口生命周期

- 状态：接受
- 日期：2026-11-16
- 修订：2026-11-16（统一数据目录、先锁后开库与重复初始化保护）

## 背景

需求：

- 桌面关掉主窗口后要能「小窗继续运行」；
- 与 TUI 共用数据目录，同一时间只能有一个播放实例；
- Android 上进程随时可能被系统回收，需要前台服务。

候选：

1. 独立 Go daemon + 多个前端；
2. 单进程（Flutter 内嵌 Go 核心）；
3. 只读 PID 文件做互斥。

## 决策

**单进程；桌面通过 Go 核心的 flock / LockFileEx 与 TUI 互斥；关窗默认进迷你窗口；退出必须显式触发。**

- Linux/Android/macOS 单进程：Go 核心编 `libzero.so`/`libzero.dylib` 在 App 内；
- Windows 例外：`zero-core.exe` sidecar（纯 Go，避免 MinGW DLL），由 Flutter 用 Job Object 绑定生命周期；
- Go 核心复用 TUI 的目录解析（Linux 尊重 `XDG_CONFIG_HOME`），先调用 `instance.Acquire`（Unix flock / Windows LockFileEx），再打开/迁移数据库和初始化音频；失败逆序清理，最后释放锁；
- 拿不到锁：尝试连接 `gui.sock`（Windows 为命名管道）让已有实例显示窗口，然后自己退出；连不上则显示「已在运行（PID x）」；
- 锁文件里的 PID 只用于提示，**不允许**用「读 PID 是否存在」替代锁；
- 桌面窗口：Linux normal ⇄ mini ⇄ tray；macOS normal ⇄ hidden（Dock/菜单栏）；关窗语义按平台，托盘/菜单栏「退出」才真正退出；
- 退出顺序：持久化队列 → 停引擎 → 注销系统媒体（MPRIS/Now Playing/SMTC）→ 关 socket / 结束 sidecar → 释放锁；
- Android 不使用桌面锁，Activity 与 audio_service 共用同一核心；`ZeroStart` 防止重复初始化，`ZeroStop` 可重复调用。播放前启动前台服务，系统仍可能回收进程。

## 后果

- 除 Windows 的纯 Go sidecar 外，没有 daemon 的生命周期与 IPC 复杂度；播放与 UI 同生共死，进程在播放就在。
- GUI 在托盘/小窗/隐藏期间持有锁，TUI 无法启动——这是有意的互斥语义。
- macOS 关闭窗口隐藏到 Dock/菜单栏，Cmd+Q 才释放锁；Linux 关窗进迷你窗口/托盘。
- 二次启动需要 `gui.sock` / 命名管道激活端点；它只做「显示窗口」，不做数据操作。
- `zero play` / `zero download` 等 CLI 目前不拿锁，是已知缺口；后续可扩展。
- 进程被杀时音乐停止，但队列每 5 秒持久化，重启可恢复。

## 备选

- **独立 daemon**：桌面可以关 UI 继续放，但 Android 需要独立前台服务，复杂且更易被杀；被否（Windows 的纯 Go sidecar 不属于这一类，它随 Flutter 生命周期）。
- **只读 PID 文件**：有 TOCTOU、崩溃残留、PID 复用问题；被否。
- **关窗即退出**：不满足「小窗继续运行」；被否。
