# zero-beat GUI · 零拍图形客户端

面向 **Android、Linux 与 macOS 桌面**（Windows 为 P7 候选）的 [zero-beat](https://github.com/Sheyiyuan/zero-beat) 图形客户端。

界面用 Flutter 重写；曲库、搜索、登录、下载、同步和播放决策计划通过 zero-beat 待新增的公开 Go SDK（`session`、`sync`）复用；声音由 Dart 侧的 libmpv（media_kit）输出。

> **状态：设计阶段。** 本目录目前只有设计文档，没有可运行代码。技术选型与取舍见 [docs/09-evaluation.md](docs/09-evaluation.md) 和 [docs/decisions/](docs/decisions/)。

设计契约与现有实现须区分：SDK/逻辑解耦、RemoteEngine、Android 下载完成处理器、go-git 后端与写前格式校验均待实现。P0/P1 先验证最小闭环，验收结果未产生前不把平台能力视为已验证。

## 目标平台

| 平台 | 优先级 | 说明 |
| --- | --- | --- |
| Android | P0 | 触屏曲库与播放、后台播放、媒体通知/锁屏、跨设备同步 |
| Linux 桌面 | P1 | 与 TUI 共用数据、同一把实例锁、托盘/小窗继续播放、MPRIS |
| macOS | P1 | 与 TUI 共用 `~/Library/Application Support/zero-beat`、非沙盒、DMG 公证（[11-macos](docs/11-macos.md)） |
| Windows | P7 候选 | GUI 可适配；需要 Windows 构建环境，排在 v0.1 之后（[10-windows](docs/10-windows.md)） |
| iOS | **不做** | 与 macOS 无关：不做 iOS 目标、不构建 |

## 和 zero-beat 的关系

- 数据共享：桌面复用 TUI 的目录解析；Linux 默认 `~/.config/zero-beat/`，尊重 `XDG_CONFIG_HOME`；macOS 使用 `~/Library/Application Support/zero-beat`；Windows 使用 `%APPDATA%\zero-beat`；跨设备只共享同步仓库（`meta.v=2`）。
- 本地布局：文档位于 zero-beat 的 `zero-beat-gui/`，与 `zero-beat-core/`、`zero-beat-tui/` 并列；原 GUI 仓库只保留历史，导入来源见 [IMPORT.md](IMPORT.md)。
- 代码复用：zero-beat 新增公开 SDK（`session`、`sync`），GUI 只依赖公开包；**不 import `internal/`**，也不 fork/复制。公开门面之前先移除核心对 oto/Bubble Tea 的传递依赖。
- 同步不 fork：git CLI / go-git 两个后端都放在 zero-beat 的 sync 包里，GUI 只选后端；仓库格式与合并逻辑只有一份。
- 界面独立：TUI 继续存在，面向终端；GUI 的交互与信息架构完全重写，不复用 TUI 的视图逻辑。
- 互斥：桌面端 GUI 与 TUI 共用 `instance.lock`，同一数据目录只能有一个在运行；Android 没有 TUI，不需要这把锁。

## 关键决策

| 决策 | 内容 | 记录 |
| --- | --- | --- |
| 技术栈 | Flutter UI + Go 核心（Linux/Android/macOS c-shared，Windows sidecar）+ 本机 NDJSON RPC | [ADR-0002](docs/decisions/0002-stack.md) |
| 模块边界 | zero-beat 新增公开 `session` / `sync`；GUI 不 import `internal/` | [ADR-0006](docs/decisions/0006-module-boundary.md) |
| 播放 | Go `player.Controller` 保留队列/模式/重试；Dart 用 media_kit 做音频引擎，loudnorm 保留 | [ADR-0003](docs/decisions/0003-playback.md) |
| Android 下载 | Go 共享传输，保存经验证的完整媒体流；不依赖外部 ffmpeg，不承诺新增嵌入标签 | [ADR-0009](docs/decisions/0009-android-download.md) |
| 进程与生命周期 | Linux/Android/macOS 单进程；Windows 为 `zero-core.exe` sidecar；关窗退到小窗/托盘/Dock 继续播放 | [ADR-0004](docs/decisions/0004-single-instance.md) |
| 数据与同步 | SQLite 本机；sync v2 仓库是唯一跨端契约；git CLI / go-git 自动回退（TUI 也适用） | [ADR-0005](docs/decisions/0005-sync.md) |
| macOS | 非沙盒以共享 TUI 数据；Developer ID + 公证；不走 Mac App Store | [ADR-0008](docs/decisions/0008-macos.md) |
| 平台范围 | Android + Linux + macOS；Windows 为 P7 候选；iOS 不做 | [ADR-0001](docs/decisions/0001-targets.md) |

## 架构一览

```
┌────────────────── Flutter App（Linux/Android/macOS 单进程） ──────────────┐
│  UI（响应式：手机 / 桌面）        audio_service / 通知 / 锁屏 / 托盘      │
│        │                                   │                             │
│        └──────────────┬────────────────────┘                             │
│                       │ 引擎命令 / 播放状态                               │
│                 media_kit（libmpv）                                       │
│                       │                                                   │
│   Go 核心（Linux/Android: libzero.so；macOS: libzero.dylib；Windows: exe）│
│   session（session/DTO/Engine）· store · app · player · sync · instance  │
└───────────────────────────────┬──────────────────────────────────────────┘
                                │ NDJSON / JSON-RPC（127.0.0.1，随机 token）
                                │
             UI 与 Go 核心同进程（Windows 为子进程），RPC 只是语言边界
```

桌面端另有：

```
TUI（zero） ── 同一把 instance.lock ── GUI（Flutter）
```

> Windows 用 `zero-core.exe` sidecar，避免 MinGW 编 Go DLL；Linux/Android 用 `libzero.so`，macOS 用 `libzero.dylib`。RPC 协议四端一致。

## 文档索引

| 文档 | 内容 |
| --- | --- |
| [00-overview](docs/00-overview.md) | 目标、非目标、术语、兼容承诺、设计原则 |
| [01-architecture](docs/01-architecture.md) | 进程模型、层次、启动序列、播放数据流、失败处理 |
| [02-rpc](docs/02-rpc.md) | 传输、信封、方法/事件清单、DTO 规则、安全 |
| [03-playback](docs/03-playback.md) | 播放分工、loudnorm、片段、Android/macOS/Windows 系统组件 |
| [04-sync-and-data](docs/04-sync-and-data.md) | sync v2 字节契约、三方合并/提交历史、SQLite、Android 下载与存储 |
| [05-lock-and-lifecycle](docs/05-lock-and-lifecycle.md) | flock、关窗/小窗/托盘、二次启动、退出与崩溃 |
| [06-build-release](docs/06-build-release.md) | 工具链、构建、签名、CI、AGPL |
| [07-roadmap](docs/07-roadmap.md) | 阶段拆分、验收标准、估算 |
| [08-risks](docs/08-risks.md) | 风险清单与缓解、go/no-go 检查点 |
| [09-evaluation](docs/09-evaluation.md) | Wails / Flutter / 纯 Dart 的对比与结论 |
| [10-windows](docs/10-windows.md) | Windows P7：构建、SMTC、托盘/小窗、安装、CI |
| [11-macos](docs/11-macos.md) | macOS P1：非沙盒、audio_service、公证、DMG、CI |
| [decisions/](docs/decisions/) | ADR-0001 ~ ADR-0009 |
| [迁移指南](../docs/monorepo-and-sync.md) | 模块布局、版本标签、双 Git 后端 |
| [TUI 架构](../ARCHITECTURE.md) | 现有 TUI 的分层与模块清单 |
| [IMPORT.md](IMPORT.md) | 从原 GUI 仓库导入的来源记录 |

## 复用边界

| 通过 `session` 复用（Go） | 重写（Flutter/Dart） | 平台迁移 |
| --- | --- | --- |
| `store`（SQLite、迁移、队列持久化） | 全部 UI 与导航 | 数据目录（Android 沙盒 / macOS `~/Library/Application Support` / Windows `%APPDATA%`） |
| `app`（搜索、解析、歌词、歌单、登录、下载） | 播放引擎（media_kit） | `sync`：git CLI / go-git 双后端 |
| `source` / `source/bilibili` / `netease` | Android 通知/锁屏（audio_service） | 下载完成处理器：桌面 ffmpeg / Android 完整媒体流 |
| `player.Controller`（队列、模式、预取、重试） | 桌面托盘、小窗、窗口管理 | 本地文件夹歌单（SAF，后续） |
| `sync` / `library` / `lyrics` / `keymap` | 设置、主题、登录二维码展示 | MPRIS（Linux）/ SMTC（Windows）/ Now Playing（macOS） |
| `sysmedia`（Linux MPRIS）、`instance`（flock / LockFileEx） | macOS 非沙盒 entitlement 与公证 | macOS 关闭沙盒 |

## 下一步

1. P0（3–4 人周）：最小公开 SDK、Session、先锁后开库及目标平台构建；core/TUI 模块与同步后端已前移落地。
2. P1（3–4 人周）：Android 真机播放/切歌/重试、锁屏 30 分钟、无 ffmpeg 下载与离线播放；桌面互斥/窗口/系统媒体；独立临时仓库验证双后端分叉合并与 Android HTTPS。
3. 记录原型结果并重估，再按 [roadmap](docs/07-roadmap.md) 补齐 GUI 业务和同步接入。当前 v0.1 估算 21–29 人周，Windows 另加 5–8；这不是发布日期承诺，原型通过前不发布 release。

## 许可

与 zero-beat 一致：[AGPL-3.0](LICENSE)。Go 核心以 c-shared（Linux/Android/macOS）或 sidecar（Windows）形式随 App 分发，整个应用按 AGPL 发布；不分发闭源构建。

本项目仅供学习、研究与技术交流，不用于商业用途。B 站与网易云音乐的音频、封面、商标等内容版权归对应平台及权利人所有。
