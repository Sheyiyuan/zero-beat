# ADR-0008：macOS 支持、非沙盒与分发

- 状态：接受
- 日期：2026-11-16

## 背景

TUI 官方支持 macOS；GUI 之前把 macOS 列为「需要 Mac 构建，暂缓」。现在已有 macOS + Xcode 构建环境，macOS 纳入 v0.1，iOS 仍然不做。

macOS 有两个与其它平台不同的约束：

1. **数据共享**：TUI 的数据在 `~/Library/Application Support/zero-beat`；Flutter macOS 默认开启 App Sandbox，沙盒应用的路径会变成 `~/Library/Containers/<bundle-id>/...`，与 TUI 不互通。
2. **分发**：AGPL 应用不走 Mac App Store（条款冲突），只能 Developer ID 签名 + 公证 + DMG。

## 决策

- **macOS 为 P1**，与 Linux 一起纳入 v0.1；iOS 不做；
- **关闭 App Sandbox**，保留 Hardened Runtime，用 Developer ID 签名 + notarization 分发 DMG；
- Go 核心用 `c-shared`（`libzero.dylib`）嵌入 Flutter App，保持单进程；Windows 仍用 sidecar；
- 系统媒体用 `audio_service` 的 macOS 后端（Now Playing/控制中心/媒体键），不写 MPRIS；
- 先 arm64，Intel/通用包后置；
- 关闭窗口 = 隐藏并继续播放，Cmd+Q 才退出。

## 后果

- 与 TUI 共用 SQLite、`instance.lock` 和 sync 仓库，交替使用不破坏数据；
- 需要 Apple Developer 账号做公证；没有账号的构建只能自用；
- 不能上架 Mac App Store；DMG 需要公证 + 装订，首次打开才不被 Gatekeeper 拦；
- 沙盒关闭意味着更大的文件访问权限，安全边界靠「只从官方渠道下载」和最小权限代码；
- 通用二进制、公证、签名会增加 CI 与发布成本（约 1–1.5 人周）。

## 备选

- **保持沙盒 + App Group**：TUI 不在同一 App Group，数据无法共享；被否。
- **Mac App Store**：AGPL 与商店条款冲突，且沙盒会破坏数据共享；被否。
- **macOS 用 sidecar**：可行，但会引入多进程生命周期；macOS 上 c-shared 更简单，沿用 Linux/Android 形态。
- **macOS 继续不做**：已有构建环境且 TUI 官方支持 macOS，放弃理由不足。
