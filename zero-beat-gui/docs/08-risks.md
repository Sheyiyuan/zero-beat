# 风险

编号保持稳定，不代表实时排序。P0/P1 优先验证播放契约、核心平台依赖、Android 离线下载和同步合并；生产级恢复与全量兼容用例在对应阶段验收。

| # | 风险 | 影响 | 概率 | 缓解 | 检查点 |
| --- | --- | --- | --- | --- | --- |
| R1 | media_kit（libmpv）在 Android 后台被暂停/被杀 | 高 | 中 | audio_service 前台服务 + wake lock；后台 30 分钟实测；必要时改用 ExoPlayer（放弃 loudnorm 或用 mpv 预处理） | P1 |
| R2 | `af=loudnorm` 在移动端 libmpv 不可用或听感差异大 | 中 | 中 | 换 media_kit 构建/自带 libmpv；保留开关降级提示；A/B 响度计 | P1 |
| R3 | Flutter Linux 中文输入法（fcitx5/Wayland）不可用 | 中 | 中 | 实测 GTK IM；必要时用系统输入框桥接；最坏回退 Wails/Web UI | P1 |
| R4 | go-git 的 fetch/push 或合并语义与 git CLI 不一致 | 高 | 中 | P1 隔离仓库验证真实共同祖先、双父提交和再次同步；P5 交付共享合并器及完整恢复测试 | P1 / P5 |
| R5 | Go c-shared 在 Android/Linux/macOS 的加载、符号、16KB/架构对齐问题 | 高 | 低 | spike 先跑通最小 `ZeroStart`；CI 校验 ELF/Mach-O；保留 `gomobile bind`/sidecar 备选 | P1 |
| R6 | 会话编排（session）与 TUI 行为漂移 | 高 | 高 | BeginPlay/CompletePlay 保留请求号和 retry；验证乱序解析、失败最多两次、重试成功后再失败停止 | P0 / P1 |
| R7 | TUI 侧代码演进导致 GUI 依赖的包 API 变化 | 中 | 中 | 固定 zero-beat tag；升级时跑双端同步/播放回归；必要时给上游提 issue | 持续 |
| R8 | Android 下载依赖外部 ffmpeg，离线闭环不可用 | 高 | 高 | 注入完整媒体流落盘处理器；P1 真机验证两音源、续传/取消和飞行模式；不支持格式明确拒绝 | P1 / P3 |
| R9 | 托盘/小窗在 Wayland 下受限 | 低 | 中 | 降级为最小化；不静默丢窗口；文档说明 | P6 |
| R10 | 双 UI 维护成本（TUI + GUI） | 中 | 高 | 数据层共享；session 放 `session`；不接受「GUI 复刻所有 TUI 交互」 | 持续 |
| R11 | AGPL 与分发渠道 | 低 | 低 | Android/Linux 商店合规；macOS 用 Developer ID + DMG；Windows 用 MSIX/Inno；源码公开 | P6 |
| R12 | 音源接口变化/风控 | 高 | 中 | 复用 zero-beat 的适配层，跟随上游修复；错误可读 | 持续 |
| R13 | 同步口令在手机上的输入与保存体验 | 中 | 中 | 安全存储（Keystore/Keychain 思路）；记住派生密钥；不存明文口令 | P5 |
| R14 | 性能：长列表、歌词滚动、冷启动 | 中 | 中 | 虚拟列表；避免每帧重建；启动只拉首屏 | P2 |
| R15 | 范围蔓延（iOS/视频/播客） | 中 | 中 | 本文档明确非目标；Windows 也只到 P7；需求先过 ADR | 持续 |
| R16 | 公开门面仍传递引入 oto/Bubble Tea，目标平台无法构建 | 高 | 中 | P0 必做纯类型/keymap 解耦、依赖树检查与各平台最小核心构建；物理拆仓不能代替依赖解耦 | P0 |
| R17 | 没有 Windows 构建机，Windows 迭代靠 CI | 中 | 高 | 先确认 Windows VM/CI 可用再排 P7；否则 Windows 保持「候选」 | P7 |
| R18 | `smtc_windows` 依赖 Rust 工具链、维护节奏不明 | 中 | 中 | CI 装 rustup；备选写小型 C++ 原生插件；锁版本 | P7 |
| R19 | media_kit 发布节奏变慢，Windows/Android 回归成本 | 中 | 中 | 锁版本；保留 just_audio/ExoPlayer 的退路；记录已知问题 | 持续 |
| R20 | git CLI 与 go-git 两个后端的提交/合并行为漂移 | 高 | 中 | 两端只做传输/对象/引用操作，共用实体合并器；校验树、两个父节点、删除冲突和事务恢复 | P1 / P5 |
| R21 | TUI 默认回退 go-git 后，go-git 的缺陷影响所有无 git 环境 | 高 | 中 | 双后端一致性测试；`ZERO_SYNC_BACKEND=cli` 可强制回退；发布前用真实远端跑一轮 | P5 |
| R22 | macOS 公证依赖 Apple Developer 账号与证书 | 中 | 中 | 先做本机签名；确认账号/证书可用后再排公证；未公证只做自用 | P6 |
| R23 | macOS 关闭 App Sandbox 降低隔离性 | 中 | 中 | 只从官方 DMG 分发；最小权限；文档说明；不引入动态下载代码 | P6 |
| R24 | Flutter macOS 关窗默认退出、通用二进制复杂 | 中 | 中 | 显式改 `applicationShouldTerminateAfterLastWindowClosed`；先 arm64，Intel 后置 | P6 |
| R25 | `audio_service` 的 macOS 后端维护/媒体键行为不如 Android 稳定 | 中 | 中 | 实测 Now Playing/媒体键；备选写小型 Swift 插件；锁版本 | P6 |
| R26 | 双向 RPC 等 ack 死锁、旧事件污染新播放 | 高 | 中 | 响应分发独立于 session，缓存锁不跨等待；播放代次/序号、终态去重和有界队列测试 | P0 / P1 |
| R27 | 新实例在拿锁前迁移数据库，或两端目录不一致 | 高 | 中 | 复用 TUI 路径解析，先锁后开库；二次启动与自定义 XDG 路径验收 | P0 / P1 |
| R28 | v2 格式误改或未知字段在重新导出时丢失 | 高 | 中 | 固定现有字节样本，区分 RPC 时间规则；写前严格校验，未知字段阻止写同步 | P1 / P5 |
| R29 | 把回环误当 Android 应用隔离，未认证连接耗尽资源 | 中 | 中 | token 鉴权；hello 时间/帧大小/并发连接上限；只允许一个引擎所有者 | P1 |

## Go / No-Go 门槛

P1 结束必须回答下列原型问题；P5 的生产验收不能用原型通过替代。Windows 构建机和发布证书是后续阶段的准备项。

1. Android 锁屏 30 分钟是否稳定？失败则改音频路线（ExoPlayer 或原生 Media3）。
2. `loudnorm` 是否可用且与 TUI 差异 ≤ 1 LU？失败则移动端默认关闭，桌面保留。
3. 去掉 oto/Bubble Tea 传递依赖后，Go 核心在 Android 是否能稳定启动、读写 SQLite、连接 RPC？失败则评估 gomobile bind；桌面才考虑 sidecar，不能用桌面 daemon 替代 Android 验收。
4. Flutter Linux 中文输入是否可用？失败则桌面端考虑 Wails 或 Web 前端。
5. 桌面关窗小窗 + 托盘 + MPRIS 是否可用？失败则降级为「最小化到任务栏」。
6. macOS 非沙盒数据目录、关窗隐藏、Now Playing 是否可用？失败则 macOS 降级或延后。
7. macOS 公证证书/账号是否可用？不可用则只做本机签名，暂不对外分发 DMG。
8. 最小 `session` 能否保留 TUI 行为并通过目标构建？必须先解决依赖边界，再决定公开 API 或 monorepo，不以拆仓代替解耦。
9. Windows 构建机是否可得？没有就先不排 P7。
10. CLI/go-git 原型能否完成双方离线修改、双父合并、push 后再次同步无变化，以及 Android HTTPS 传输？失败则修改后端方案；仅快进/只读模式不能视为完整同步已达标。
11. Android 无 ffmpeg 时能否对两音源支持样本下载、取消/续传并离线播放？失败则重估原生封装后端或更新离线目标，不能推迟到 P3 才发现。
12. 快速切歌、解析乱序、重复结束、load 超时与重试成功后再次失败是否满足协议？任何一项失败先修 session/RPC，不进入全量播放 UI。
13. 持锁二次启动是否完全不触库，未知字段样本是否被只读校验识别？写前阻断和全量同步恢复在 P5 再验收。

任何一项失败都不直接否掉项目，但必须改方案并更新 ADR，不能带着不确定性进入下一阶段。
