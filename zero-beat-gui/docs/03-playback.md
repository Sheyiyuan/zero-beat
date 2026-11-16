# 播放

## 分工

| 职责 | 位置 | 说明 |
| --- | --- | --- |
| 队列、索引、模式、随机顺序 | Go `player.Controller` | 复用现有实现与测试 |
| 解析播放地址、过期重取、重试一次 | Go `session` + `app.Resolve` | 复用 `audio.Stream.ExpiresAt`、本地优先 |
| 预取下一首 | Go `session` | 列表循环/顺序模式，缓存一条 |
| 持久化队列/进度 | Go `player.Controller` | 写 `queue` 表，TUI 可恢复 |
| 解码、输出、seek、loudnorm、片段边界 | Dart `Engine` | media_kit + libmpv |
| 通知、锁屏、音频焦点、后台 | Dart | audio_service + audio_session |
| 桌面 MPRIS、媒体键、封面 | Go `sysmedia` + `cover` | 复用现有实现；Dart 把状态推给 Go |

`player.Controller` 已经包含 `HandleEvent`、`PlayResolved`、`CompletePlay`、`Tick` 等关键逻辑；GUI 的 `session` 只做「异步解析 + 把事件喂给控制器 + 把控制器状态发出去」。

### 解析与重试预算

先调用 `BeginPlay(song, startAt, retry)`，再异步解析，最终调用 `CompletePlay(req, ..., retry, err)`。`CompletePlay` 丢弃请求号不匹配的旧结果；`PlayResolved` 会把 retry 置零，不能用来完成异步解析或自动重试。

一次用户起播最多自动重试一次，解析失败、load 失败与播放中失败共用这一个预算。解析失败消费 `CompletePlay` 的 `Retry` 结果；load 同步错误通过 session 归一为一次 `HandleEvent(Failed)`，不能同时再合成一个失败事件；异步引擎失败先按 `playback_id` 去重再处理。重试成功后计数仍为 1，再失败就停止。用户显式重试/切换曲目才开启新预算。seek 重新取流沿用当前预算，并保持原来的暂停/播放意图。

这是 SDK 抽取时必须补齐的行为测试，不能仅凭复用控制器就认定自动成立；`CompletePlay` 对引擎同步错误目前直接返回 `Err`，需要 session 接续失败处理。

## 引擎契约

Go 侧通过 zero-beat 公开的 `session.Engine` 抽象（内部适配 `player.Engine`，接口见 [zero-beat `zero-beat-core/player/controller.go`](https://github.com/Sheyiyuan/zero-beat/blob/main/zero-beat-core/player/controller.go)）。GUI 用 `RemoteEngine` 实现它：

表中沿用现有控制器方法名。公开 SDK 还需定义本次加载的播放意图选项，由 session 在调用 `CompletePlay` 前提供给引擎适配器，并映射到 RPC 的 `play_when_ready`；这是待新增的适配契约，不能假定现有 `PlayAt(ctx, stream, at)` 已表达暂停恢复语义。

| 方法 | RemoteEngine 行为 | Dart 侧 |
| --- | --- | --- |
| `PlayAt(ctx, stream, at)` | 新建播放代次，发 `engine.load`，等接受 ack（3s） | 串行打开流，起始位置/片段就绪后才允许出声；缓冲完成走事件 |
| `Pause()` / `Resume()` / `Stop()` | 发对应命令 | `Player.pause()` / `play()` / `stop()` |
| `Seek(ctx, pos)` | 发 `engine.seek` | `Player.seek(pos)` |
| `SetVolume(v)` | 发 `engine.volume`（0.0–1.0） | 转为 media_kit 的百分比：`Player.setVolume(v * 100)` |
| `SetCompensationDB(db)` | 发 `engine.comp` | 计算线性增益，应用到音量/滤镜 |
| `SetAutoGain(bool)` | 发 `engine.auto_gain` | 切换 `af=loudnorm` |
| `AutoGainSupported()` | 启动时由 Dart 声明 | libmpv 带 libavfilter → true |
| `Position()` | 读缓存的 250ms 位置 | `engine.position` 事件 |
| `Duration()` | 读缓存时长 | 同一代次的 position/duration 更新 |
| `State()` | 读缓存状态 | `engine.state` 事件 |
| `Events()` | 内部 channel | `ended`/`failed`/`state` 转成 `audio.Event` |
| `CurrentStream()` | 记录最后一次 `load` 的 stream | — |

约束：

- `controller` 的调用在 session 队列串行化；`RemoteEngine` 缓存用独立短锁，等待 ack 前释放。RPC 响应分发独立于 session 队列，不能等正在等待响应的调用完成；
- `engine.position` 只做缓存，不触发同步 RPC，避免每 250ms 一次往返；
- 命令/事件携带播放代次，事件另带序号；旧代次和重复终态在进入控制器前丢弃。错误与断线处理遵守 [02-rpc](02-rpc.md)，暂停/停止等控制超时不能导致重新起播；
- 接受 load 不等于已经播放。适配层在接受时置缓存为 buffering，后续 engine.state 校准控制器及导出状态；需覆盖现有 `CompletePlay` 乐观写 playing 的行为；
- 尚未建立播放代次时，音量/补偿/自动增益只更新缓存，下一次 load 携带完整设置；暂停中的 seek 或重新解析使用 `play_when_ready=false`，位置准备好后仍保持暂停；
- Dart 侧必须是 Player 的唯一所有者，不允许在多个 isolate 重复初始化。

## media_kit 配置

- 包：`media_kit` + 平台库（`media_kit_libs_android_*`、`media_kit_libs_linux`）；
- 打开：`Media(uri, httpHeaders: headers)`；B 站/网易云的 `Referer`、`Cookie` 通过 headers 传入，不拼进 URL；
- 位置/时长/缓冲：订阅 `player.stream.position`、`duration`、`buffering`；
- 自然结束/片段结束：`player.stream.completed` → 当前代次的 `engine.ended`；主动 stop、替换流和旧回调不触发下一首；
- 错误：`player.stream.error` → `engine.failed`；
- 音质选择仍由 Go 的 `source.Resolve` 决定，Dart 不选流。

## 自动响度均衡（loudnorm）

TUI 当前用 ffmpeg 单遍：

```
ffmpeg -af loudnorm=I=-16:TP=-1.5:LRA=11
```

GUI 用 libmpv 的 libavfilter：

```
af = loudnorm=I=-16:TP=-1.5:LRA=11
```

实现方式：通过 media_kit 的 `NativePlayer` 设置 mpv property（`Player.platform`，落地时确认 `setProperty`/`command` 可用；不可用则退回 `command(['af', 'set', ...])` 或在 `loadfile` 参数里传）。

对齐点：

- 目标响度、真峰值、LRA 与 TUI 保持一致（-16 / -1.5 / 11）；
- 单遍模式同样是动态增益，不预分析整首；
- 增益补偿：TUI 是 `音量 × (全局补偿 + 单曲补偿)` 后接 tanh 软限幅；GUI 用 mpv `volume` + `alimiter`（或 `af=volume=...`）近似，**不追求 bit 级一致**；
- 提供 A/B 测试：同一首歌在 TUI 与 GUI 各播 30 秒，用响度计（`ffmpeg -af ebur128` 录回环或外部测量）比较 integrated LUFS，差异目标 ≤ 1 LU；
- 若 `af=loudnorm` 在某些 Android 构建不可用，开关置灰并明确提示，不静默降级。

## 片段播放

- 语义与 TUI 一致：位置仍是全曲绝对时间，片段只限制解码区间；
- 优先使用 mpv 的 `start`/`end` 选项（若 media_kit 的 `Media` 支持就直接传；否则用 `NativePlayer` 设置 `start`/`end` 后再 `loadfile`）；
- 兜底：先以暂停状态打开、seek 到起点再播放，达到终点后停止并发送一次该代次的 `engine.ended`；不得把主动停止误判为片段结束。精度以真机验收为准；
- 片段编辑器：预览整首，确认后写 `clip:<songID>` 设置（复用 Go `SaveClip`）。

## 音频焦点与系统组件（Android）

- `audio_session`：配置 `AudioSessionConfiguration.music()`；
- 来电/其他 App 播放：`becomingNoisy`、`audioFocus` 中断 → 暂停；恢复时按策略续播；
- 耳机拔出：暂停（与系统播放器一致）；
- `audio_service`：
  - `AudioHandler` 的 play/pause/skip/seek 都转为 Go `player.*` 命令，不绕过会话直接更改队列或重试；
  - `MediaItem` 来自 Go 的 `player.state`（标题、艺人、时长、封面本地路径）；
  - `PlaybackState` 同步播放状态与位置；
  - 前台服务 + 常驻通知降低后台回收概率；进程仍可能被系统终止，恢复规则见生命周期文档；
  - 通知按钮：上一首/播放暂停/下一首；点击回到 App；
- 蓝牙/车机按键走 MediaSession，不额外适配；
- 锁屏进度条依赖 `PlaybackState.position`，每 250ms 更新一次即可。

## 桌面系统媒体（Linux）

- zero-beat 内部复用 `sysmedia`：它已经实现 MPRIS（`org.mpris.MediaPlayer2`），状态来自 `player.Controller`，封面来自 `cover` 缓存；GUI 通过 `session` 拿到 MPRIS 所需状态；
- `session` 需要像 TUI 一样消费 `sysmedia.Service.Commands()`，把 toggle/next/prev/seek/volume 映射到 controller；
- 媒体键（XF86）由桌面环境经 MPRIS 转发，无需单独抓键；
- 托盘/小窗的播放按钮直接调 controller，不经过 MPRIS。

## macOS 系统媒体（P1）

- 用 `audio_service` 的 macOS 后端：Control Center / Now Playing、耳机键、封面、进度、上一首/下一首；
- 没有 MPRIS（Linux）也没有 SMTC（Windows）；Go 侧的 `sysmedia` 在 macOS 是空实现，不需要改；
- 媒体键由 Now Playing 会话接管，不需要 `MPRemoteCommandCenter` 手写；
- 关闭主窗口只是隐藏，播放继续；Cmd+Q 才停止并退出；
- 音频输出走 CoreAudio（media_kit），设备切换跟随系统；
- **App Sandbox 必须关闭**才能读写 `~/Library/Application Support/zero-beat`，与 TUI 共用数据；签名/公证见 [11-macos](11-macos.md)。

## Windows 系统媒体（P7）

- 用 [`smtc_windows`](https://pub.dev/documentation/smtc_windows/1.1.0/index.html) 接 SystemMediaTransportControls：元数据、播放状态、时间轴、按钮；
- 位置/状态由 `player.state` 驱动，按钮回调转成 `player.*` 命令；
- 媒体键、音量 OSD、锁屏信息由 SMTC 接管；
- 该插件要求构建机安装 rustup（CI 需要 Rust 工具链）；备选是写小型 C++ 原生插件；
- 没有 audio_service，也没有 MPRIS；音频焦点交给 WASAPI session。

## 睡眠定时、音量、静音

- 睡眠定时：`controller.Tick(now)` 到点后暂停，`player.state` 推给 UI；
- 音量：0.0–1.0，持久化到 `settings`（与 TUI 共用）；静音是 UI 层记忆，恢复时用原音量；
- 单曲/全局补偿：dB 值，写 `settings` / 注释表；`engine.comp` 只负责实际增益。

## 状态机与 UI

```
idle → buffering → playing ⇄ paused
                 ↘ failed → (retry once) → buffering
                 ↘ ended → next → buffering
```

- UI 只消费 `player.state`，不自行推断；
- buffering 超过 5 秒显示「网络较慢」提示，不弹模态；
- 失败重试一次后停在当前曲目，显示可点的「重试」；
- 队列为空/到队尾：显示空状态，不自动循环（除非模式是列表循环）。

## 测试矩阵

| 场景 | 期望 |
| --- | --- |
| 锁屏 30 分钟 | 播放不中断，通知进度正常 |
| 耳机拔出/蓝牙断开 | 暂停 |
| 来电/导航播报 | 暂停或 duck（按 audio_session 配置），结束后恢复 |
| 链接过期（改系统时间/等 20 分钟） | 自动重解析并续播 |
| 断网 → 恢复 | 失败重试一次，提示可手动重试 |
| 解析 A 慢于后来点选的 B | A 的完成结果被丢弃，B 不被覆盖 |
| 失败 → 自动重试成功 → 再失败 | 停止并提示，不开启第三次尝试 |
| 快速切歌/重复 ended/旧 position | 每代终态只处理一次，新曲目与进度不受旧事件影响 |
| load 慢缓冲/暂停超时/连接断开 | 接受 ack 与出声分离，超过起播期限只失败一次；暂停超时不重播；重连保持未播放 |
| 片段起止 | 起点误差 < 300ms，尾音 < 150ms |
| loudnorm A/B | 与 TUI 差异 ≤ 1 LU |
| 2000 首队列随机切换 | 不卡顿，内存稳定 |
| 桌面关窗到小窗 | 播放不断，MPRIS 状态正确 |
| macOS 关窗/隐藏 | 播放不断，Dock 恢复窗口，Cmd+Q 才退出 |
| macOS Now Playing/媒体键 | 控制中心显示元数据，播放/暂停/切歌可用 |
| TUI 启动（GUI 在跑） | 明确提示已有一个实例 |
