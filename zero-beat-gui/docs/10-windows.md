# Windows

- 状态：**P7 候选**，排在 Android + Linux + macOS v0.1 之后
- 结论：可以做。之前不做 Windows 的主要障碍是终端（ANSI、字体、键盘、ConPTY、ffmpeg 依赖），GUI 把这些绕开了；但 Windows 有自己的一套系统集成工作。

## 目标

- Windows 10/11 x64；
- 曲库、搜索、播放、歌词、登录、下载、同步与 Android/Linux/macOS 功能对齐；
- 迷你窗口、托盘、SMTC（系统媒体控制）、媒体键；
- 与现有 Windows TUI zip 共用 `%APPDATA%\zero-beat` 数据，互斥运行。

不做：Windows ARM64、Windows TUI 的正式支持（仍是 zip/不承诺）、Microsoft Store 强制上架。

## 构建

| 项 | 方案 |
| --- | --- |
| Flutter Windows | 需要 Windows + Visual Studio 2022「Desktop development with C++」；Linux 不能交叉构建（[Flutter 文档](https://docs.flutter.dev/platform-integration/windows/setup.md)） |
| Go 核心 | `zero-core.exe` sidecar，`CGO_ENABLED=0`，可在 Linux 交叉编译 |
| 为什么 sidecar | 避免 MinGW 编 Go c-shared DLL 的 ABI/运行库问题；RPC 协议与 Linux/Android 完全一致 |
| 进程管理 | Flutter 启动 `zero-core.exe`，用 Job Object 的 KILL_ON_JOB_CLOSE 绑定生命周期，退出时结束子进程 |
| 数据目录 | `%APPDATA%\zero-beat`（与 TUI zip 相同） |
| 锁 | 复用 `instance` 的 `LockFileEx`（zero-beat 已有 Windows 实现） |
| 安装包 | MSIX（`msix` 包）或 Inno Setup；可选代码签名 |

如果以后坚持单进程，可以再评估 `c-shared` DLL（MinGW-w64 + ABI 测试），v1 不走。

## 系统组件

- **SMTC**：用 [`smtc_windows`](https://pub.dev/documentation/smtc_windows/1.1.0/index.html)（MIT）替代 Linux 的 MPRIS 与 Android 的 audio_service，提供元数据、播放状态、时间轴、按钮；注意它**要求 rustup**，CI 需要 Rust 工具链。
- **媒体键/音量 OSD**：由 SMTC 接管，不需要额外抓键。
- **托盘/迷你窗口**：`window_manager` + `tray_manager` 支持 Windows；无边框、置顶、拖动、双击恢复；任务栏缩略图按钮后续再加。
- **音频焦点**：Windows 没有 Android 式 focus API；WASAPI 的 session ducking 由系统处理，不额外适配。
- **通知**：播放状态以 SMTC 为准；下载完成等可用 `local_notifier`（可选）。

## 下载与 ffmpeg

zero-beat 的下载依赖 `ffmpeg` 做封装与写标签（[`zero-beat-core/download`](https://github.com/Sheyiyuan/zero-beat/blob/main/zero-beat-core/download/download.go)）。Windows 有三种选择：

1. 安装包内附带 `ffmpeg.exe`（GPL 构建与 AGPL 应用兼容，需带许可证与源码指引）；
2. 检测系统 ffmpeg，没有就禁用下载并提示；
3. 后续实现纯 Go 重封装，去掉依赖。

v1 建议 2 或 1；不要静默失败。

## 同步

- 不依赖 Git for Windows：Windows 默认走 **go-git 后端**（与 Android 共用），检测到系统 git 时可切 CLI；
- 仓库格式、加密、合并逻辑仍由 zero-beat 的 sync 包统一提供（见 [04-sync-and-data](04-sync-and-data.md)）；
- 数据目录与锁与 TUI 共用，交替使用不会破坏仓库。

## 二次启动与激活

- GUI 的 sidecar 先拿 `instance.lock`，再开库/迁移；第二实例不触库；
- 第二次启动拿不到锁：通过命名管道 `\\.\pipe\zero-beat-gui`（或回环 RPC）让已有实例显示窗口，然后退出；
- 没有激活端点时显示「已在运行（PID x）」。

## CI

- 新增 `windows-latest` job：Flutter Windows 构建 + 打包；
- Go 核心在 Linux 上 `GOOS=windows` 交叉编译，产物随包；
- 本节只增加 Windows job；macOS 沿用 P1 job，iOS 不构建；
- 冒烟：启动、列歌单、播放 30 秒、SMTC 按钮、托盘/小窗、退出释放锁。

## 测试矩阵

| 项 | 覆盖 |
| --- | --- |
| 系统 | Windows 10 22H2、Windows 11 23H2+ |
| 缩放 | 100% / 125% / 150%，多显示器 |
| 媒体 | SMTC 按钮、媒体键、蓝牙耳机、音量 OSD |
| 窗口 | 小窗置顶、托盘恢复、任务栏、休眠/唤醒 |
| 数据 | 与 TUI zip 共用 DB、互斥、队列恢复 |
| 同步 | go-git fetch/push、冲突页、与 Linux TUI 双端同步 |
| 安装 | 未签名 SmartScreen、升级覆盖、卸载残留 |

## 估算

| 工作 | 人周 |
| --- | --- |
| Flutter Windows 目标 + CI | 1–2 |
| `zero-core.exe` sidecar + 路径/锁/激活 | 1–2 |
| SMTC + 托盘/小窗/任务栏 | 1–2 |
| 安装包/签名 | 1 |
| Windows 10/11、DPI、休眠、蓝牙 QA | 1–2 |
| **合计** | **5–8** |
| 随包 ffmpeg 下载 | +1–2 |

## 风险

- **Windows 构建机**：没有 Windows 机器就只能靠 CI，迭代慢；这是最实际的阻碍。
- **SMTC 插件依赖 Rust**：构建链多一环；备选是写一个小型 C++ 原生插件。
- **media_kit 维护节奏**：最新版发布已较久，Windows 上要锁定版本并做回归。
- **ffmpeg 分发**：体积、许可证、杀软误报。
- **未签名安装包**：SmartScreen 警告；正式分发前再买证书。
