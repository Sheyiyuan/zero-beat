# ADR-0007：统一 monorepo 与模块边界

- 状态：接受；核心/TUI 模块迁移已落地，GUI 实现另行推进
- 修订：2026-11-16

## 决策

以现有 zero-beat 仓库及历史为主，纳入 GUI 工作树。原 GUI 仓库保留历史，来源见 [IMPORT](../../IMPORT.md)。统一模块目录为 zero-beat-core、zero-beat-tui、zero-beat-gui；网站独立位于 zero-beat-site（JavaScript 包，不加入 Go 工作区）。

核心与 TUI 各有一个 Go module，路径为 `github.com/sheyiyuan/zero-beat/zero-beat-core` 和 `github.com/sheyiyuan/zero-beat/zero-beat-tui`。根 go.work 联调，固定模块版本用于发布。GUI 尚无 Go 入口，不创建空 module；未来 GUI Go module 放在 zero-beat-gui 根，Flutter 子目录 app 的包标识为 zero_beat_gui。

共享业务、同步、媒体类型、键位配置归 core；Bubble Tea、oto、系统媒体和终端控制归 TUI。同步和桌面音频暂不增加独立模块。core 公开当前 TUI 需要的包，禁止跨模块 internal 引用。完整 Session 与 GUI SDK 随 GUI P0 实现，当前 TUI 继续使用已有播放控制器。

## 发布与兼容

命令仍为 zero，配置路径、安装入口和安装资产保持兼容。core/TUI 配套模块标签采用 `zero-beat-core/vX.Y.Z`、`zero-beat-tui/vX.Y.Z`；根 `vX.Y.Z` 发布应用。GUI 以后独立打包，不能抢占 TUI 的 latest 安装入口。

模块迁移已从 GUI MVP 后的备选前移，相关实现与验证以[迁移说明](../../../docs/monorepo-and-sync.md)为准。公开 API、Session、跨平台运行验收仍需继续完成，物理迁移并不代替这些工作。
