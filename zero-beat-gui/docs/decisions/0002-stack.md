# ADR-0002：技术栈

- 状态：接受
- 日期：2026-11-16
- 修订：2026-11-16（补充 Go 模块边界与 Windows 形态）

## 背景

GUI 的视图层和 TUI 没有复用可能，必须重写；但 zero-beat 有约 30k 行非 UI 代码（业务、数据、同步、播放状态机），重写代价极高且会漂移。需要一种方案，让新 UI 复用这些代码，同时保留 Android/Windows 的系统集成能力。

候选：

1. **Wails v3**：Web UI + Go 进程内，绑定自动生成；移动端 Android/iOS 存在但标注 Experimental；没有媒体通知/锁屏/音频焦点的现成能力。
2. **Flutter + Go 核心**：Dart UI，Go 编 `c-shared` 进 App（桌面可换 sidecar），本机 RPC；Android/macOS/Windows 生态成熟。
3. **纯 Flutter/Dart**：全部用 Dart 重写。

## 决策

采用 **Flutter UI + Go 核心 + 本机 NDJSON/JSON-RPC**。

- Flutter 负责所有视图与平台集成；
- Go 核心通过 zero-beat 新增的**公开 SDK**（`session`、`sync`）复用业务、数据、播放状态机与同步，**不 import `internal/`**（GUI 当前模块路径不在其可见范围内，见 [ADR-0006](0006-module-boundary.md)）；
- 进程形态：
  - Linux：`libzero.so`（c-shared），单进程；
  - Android：`libzero.so`（c-shared），单进程 + 前台服务；
  - macOS：`libzero.dylib`（c-shared），单进程；关闭 App Sandbox 以共享 TUI 数据；
  - Windows：`zero-core.exe` sidecar（纯 Go，`CGO_ENABLED=0`），Flutter 负责启停；
- 语言边界用回环 socket + JSON-RPC 2.0，双向方法 + 事件；四端共用同一套 RPC 与 DTO。

## 后果

- 复用 15k+ 行业务与数据代码，同步格式零漂移。
- 需要维护一层 RPC/DTO 与 `RemoteEngine`（约 3–4 周）；协议必须版本化。
- P0 先交付最小 `session` 和逻辑解耦（含 Session/DTO 与依赖检查，3–4 人周）；全量门面随后按功能补齐。工作项可能跨阶段，实施总估算以 [roadmap](../07-roadmap.md) 为准，不与这里的 RPC 工作量重复相加。
- Windows 走 sidecar，避免 MinGW 编 Go DLL 的 ABI 问题；macOS 用 dylib 单进程，需要签名/公证与非沙盒；代价在构建与分发，不在业务代码。
- 不依赖 Wails 的移动端实验状态；Android 系统组件走成熟插件。

## 备选

- **Wails v3**：被否。自动 bindings 省 3–4 周，但移动端媒体集成要手写原生层，且移动端 Experimental。
- **纯 Dart**：被否。要重写协议、同步加密、内容指纹、SQLite 迁移，4–6 个月且必然与 TUI 漂移。
- **GUI 核心放进 zero-beat 仓库**：保留为退路。公开 SDK 维护不动时，用 monorepo 换掉 `pkg/` 拆分。
