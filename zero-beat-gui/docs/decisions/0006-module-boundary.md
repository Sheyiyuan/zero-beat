# ADR-0006：公开 Go 模块与 GUI SDK

- 状态：接受
- 修订：2026-11-16（与 monorepo 迁移对齐）

## 当前边界

共享业务已经位于 `github.com/sheyiyuan/zero-beat-core`，公开 app、store、source、player、sync、media、keymap 等包。TUI 依赖这些公开接口；不跨 module 引用 internal，不复制业务实现到 GUI。

媒体类型不依赖 oto，键位校验不依赖 Bubble Tea。GUI 文档、未来 Flutter 和平台入口位于 zero-beat-gui，未来 Go module 同名。core 中只保留通用业务，RPC、RemoteEngine 和 c-shared/sidecar 组装属于 GUI。

## GUI P0 待实现

在 core 内新增 `session` 门面与公开 DTO、Engine 接口；实现 Open/Close、列歌单/曲目、播放，根路径可注入，实例锁先于数据库打开。异步播放使用 BeginPlay/CompletePlay 保留请求代次和重试预算。搜索、下载等门面随对应功能补齐。

TUI 本轮继续使用现有公开业务包和播放控制器，后续迁移 Session。sync 包直接公开配置、Client、状态与冲突接口；双后端及共享合并已在 TUI 阶段实现，GUI 只接入同一模块。

核心目标平台编译检查不能代替 Linux/Android/macOS c-shared 与 Windows sidecar 的实际运行验证。GUI 入口尚未实现，P0 仍须完成最小端到端验证。
