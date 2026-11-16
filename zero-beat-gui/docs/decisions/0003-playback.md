# ADR-0003：播放分工

- 状态：接受
- 日期：2026-11-16
- 修订：2026-11-16（解析请求、播放代次、重试预算与 RPC 接受语义）

## 背景

zero-beat 的播放是「ffmpeg 子进程解码 → PCM 增益链 → oto 输出」，队列、模式、重试、预取在 Go 侧。

Android 上这套不能照搬：

- iOS/Android 都不允许随意 `exec` 外部二进制；把 ffmpeg 塞进包再执行在 iOS 不成立，Android 10+ 也不可靠；
- 系统媒体通知、锁屏、音频焦点、后台播放需要 Android 原生组件，Go 侧没有生态；
- 用户要求「自动响度均衡最好有」，排除了只有增益的 ExoPlayer 方案（`LoudnessEnhancer` 不是 EBU R128）。

## 决策

**Go 保留播放决策，Dart 负责播放输出。**

- Go `player.Controller` 继续管队列、索引、模式、随机顺序、`HandleEvent`、`PlayResolved`、`Tick`、持久化；
- Go 新增 `session`（headless 编排）做 `BeginPlay` → 异步解析 → `CompletePlay` → 引擎事件 → 下一首；请求号筛掉旧解析结果，`playback_id` 筛掉旧引擎事件；
- 自动重试共享一次预算；不能用会清零计数的 `PlayResolved` 完成异步解析/重试，重试成功后再次失败也必须停止；
- Dart 实现 `player.Engine` 的远端版本（`RemoteEngine` 在 Go，`Engine` 在 Dart），用 **media_kit（libmpv）** 解码/输出；
- loudnorm 用 mpv 的 `af=loudnorm=I=-16:TP=-1.5:LRA=11`，与 TUI 的 ffmpeg 参数一致；
- Android 系统组件用 `audio_service` + `audio_session`；
- 桌面 MPRIS 继续用 Go 的 `sysmedia`，Dart 只提供状态。

## 后果

- 队列/重试/预取逻辑不重写，TUI 的测试仍有效。
- 需要维护 `RemoteEngine`：位置走 250ms 事件缓存，load 的 3s ack 只确认命令已接受，真实缓冲/播放通过事件报告；同一故障只能走一次失败路径。暂停/停止等控制超时不得触发自动起播。
- 响应分发与 session 队列分离；缓存独立短锁不跨 RPC 等待持有。命令带播放代次，事件另带序号并对终态去重；断线后停止旧播放，重连不自动续播。细则见 [02-rpc](../02-rpc.md)。
- loudnorm 与增益链路是「近似对齐」：TUI 的 tanh 限幅 vs mpv 的 `alimiter`，不保证 bit 级一致；用 A/B 响度计验收（≤ 1 LU）。
- media_kit 增加包体积与耗电；后台播放依赖 audio_service 前台服务，必须做 30 分钟稳定性测试。
- 片段播放用 mpv 的 start/end（或 seek + 定时停），精度约 100ms，与 TUI 的已知限制一致。
- Windows（P7）沿用同一分工：Dart 播放 + `smtc_windows` 提供系统媒体，Go 侧没有 MPRIS；`zero-core.exe` 只做数据与队列。

## 备选

- **just_audio（ExoPlayer）**：后台/焦点最稳、体积小，但没有 loudnorm；若最终放弃移动端 loudnorm，可切换。
- **Go 放音（oto）+ libav**：保留引擎实现，但 Android 上要链 libav 静态库，且媒体通知/焦点仍要写原生层，工作量更大。
- **WebView `<audio>`**：实现最省，但后台、锁屏、响度都弱，不适合音乐 App。
