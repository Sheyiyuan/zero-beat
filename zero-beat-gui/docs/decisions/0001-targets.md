# ADR-0001：目标平台

- 状态：接受
- 日期：2026-11-16
- 修订：2026-11-16（Windows 改为 P7 候选；macOS 纳入 P1；iOS 仍不做）

## 背景

zero-beat 是 Go 写的终端播放器，官方支持 Linux 和 macOS，Windows 只有不支持的 zip。做图形客户端的主要动机是移动端，同时把桌面图形环境补上。

最初把 iOS、Windows 列为不做，macOS 列「需要 Mac 构建，暂缓」：

- iOS：需要 macOS + 完整 Xcode，且 AGPL 应用通过 App Store 分发存在条款冲突；
- Windows：终端适配困难（ANSI、字体、键盘、ConPTY、ffmpeg 依赖）；
- macOS：TUI 已支持，但 GUI 需要 Mac 构建环境。

现在情况变了：

- GUI 让 Windows 的终端问题消失，Windows 成为普通桌面目标（P7 候选）；
- macOS 构建环境已具备，且 TUI 官方支持 macOS，GUI 应当共享同一份数据；
- iOS 的构建与合规问题不受 GUI 影响，仍然不做。

## 决策

- **Android 为 P0**：唯一移动目标。
- **Linux 桌面为 P1**：与 TUI 共用数据、互斥运行，托盘/小窗/MPRIS。
- **macOS 为 P1**：与 Linux 并行纳入 v0.1；非沙盒共享 TUI 数据，Developer ID 公证 + DMG，不走 Mac App Store。细节见 [11-macos](../11-macos.md)。
- **Windows 为 P7 候选**：排在 v0.1 之后，需要 Windows 构建机；细节见 [10-windows](../10-windows.md)。
- **iOS 不做**：不编译、不适配、不测试。
- 本地仓库布局：GUI 文档位于 zero-beat 的 `zero-beat-gui/`，与 `zero-beat-core/`、`zero-beat-tui/` 并列；原 GUI 仓库只保留历史，导入来源见 [IMPORT](../../IMPORT.md)。

## 后果

- Android 与 Linux 在 Linux 上构建；macOS 需要 macOS + Xcode + CocoaPods；Windows 需要 Windows + Visual Studio 2022。
- macOS 需要 Apple Developer 账号做公证；不公证只能自用；不走 Mac App Store，避免 AGPL 与商店条款冲突。
- macOS **关闭 App Sandbox**，否则数据目录会被重定向到容器，无法与 TUI 共用；安全边界靠官方 DMG 分发与最小权限。
- Windows 的加入不改变 v0.1 的范围：它排在 v0.1 之后。
- 三套桌面（Linux/macOS/Windows）共享同一套 Flutter 视图与 Go 核心，差异集中在系统媒体、窗口/托盘、签名与打包。

## 备选

- iOS + Android 一起做：仍被否，成本与合规风险不成比例。
- macOS 继续延后：已有构建环境且 TUI 官方支持，放弃理由不足。
- Windows 继续不做：可行，但 GUI 已经把主要障碍去掉，放弃的理由变弱。
