# 0015 播放状态机与下载任务从 TUI 抽出，Model 只保留视图状态

- 状态：已接受
- 日期：2026-10-22

## 背景

ADR-0014 把搜索、取流、歌词、歌单同步等外部业务抽到了 `internal/app`，但
`internal/tui` 的 `Model` 仍然直接持有：

- 队列、当前曲目、播放状态、进度、播放模式、随机基准、重试计数、预解析缓存；
- 引擎的 `PlayAt` / `Pause` / `Resume` / `Seek` / `Stop`；
- 下载任务、并发槽位、进度通道与完成后的清理；
- 会话恢复时的队列/进度/模式恢复。

这导致 TUI 的 `Update` 和 `Model` 同时承担视图状态机和播放器业务，难以脱离
Bubble Tea 测试，也让后续接入更多平台时继续被耦合。

## 决策

### `internal/player.Controller` 接管播放业务

- `player.State` 是完整播放状态：队列、索引、当前曲目、播放/缓冲/失败/结束态、
  位置、时长、模式、音量、补偿、静音、断点、重试计数、等待解析、请求号、
  预解析缓存、随机基准、睡眠定时、音质偏好、最近落盘时间。
- `player.Controller` 提供：
  - 队列：`LoadQueue` / `Enqueue` / `InsertNext` / `MoveItem` / `MoveToNext` /
    `RemoveItem` / `Clear` / `SetIndex` / `Next` / `Prev` / `SetMode`；
  - 播放：`BeginPlay` / `CompletePlay` / `PlayResolved` / `Toggle` / `Seek` /
    `HandleEvent` / `Tick` / `Persist` / `Stop` / `RestoreSession`；
  - 设置：`SetQuality` / `CycleQuality` / `SetVolume` / `ToggleMute` /
    `SetCompDB` / `CycleSleep`；
  - 预解析：`PrefetchCache`、`NextSongID`、`TakePrefetch`。
- `BeginPlay` 负责登记请求、切换当前曲目并进入缓冲态；异步解析结果由
  `CompletePlay` 处理，请求号不匹配的旧结果直接丢弃。
- `CompletePlay` 负责自动重试一次、失败停引擎、引擎 `PlayAt`、当前曲目/时长/
  进度更新与落盘。
- `HandleEvent` 负责引擎的失败/结束事件；`Tick` 负责 250ms 心跳的位置同步、
  睡眠定时与 5 秒落盘。
- 控制器不 import Bubble Tea，也不做渲染、按键或通知。

### `internal/downloader.Manager` 接管下载任务

- `Manager` 持有任务队列、并发上限、运行中任务、取消函数与事件通道；
- `Add` / `Start` / `Remove` / `CancelAll` / `Tasks` 是纯业务接口；
- 进度事件通过 `Events()` 交给 TUI，`Model` 只负责把事件转成 `tea.Msg` 和
  渲染底部下载行；
- 下载执行仍然走 `app.Service.DownloadSong`，控制器不关心文件路径细节。

### TUI 只保留视图状态

`Model` 保留：当前页面/焦点、列表与歌单镜像、搜索输入、弹层、提示、二维码
展示缓存、剪贴板/剪片编辑器草稿、按键映射、主题与尺寸。播放器字段退化为从
控制器拉取的视图镜像，所有播放/队列/下载操作都通过控制器或下载管理器完成。

## 理由

1. 播放状态机可以脱离 TUI 单测：控制器测试用 fake engine/fake saver，不渲染终端。
2. 异步请求的请求号、重试、预解析、落盘集中在一个状态机里，避免散落在
   `Update`、`cmds.go` 和 `Model` 三处。
3. 下载任务的并发、取消、事件流与渲染分离，TUI 不再持有通道和 goroutine 生命周期。
4. 后续接入新音源时，播放/队列/下载行为不随 UI 变化而改。

## 后果

- `Model` 仍保留播放状态的**镜像字段**，用于渲染和按键判断；每次控制器操作后
  通过 `player.State` 同步。这层镜像不是业务来源，允许测试直接构造。
- 片段编辑器的“整首预览/按新区间重播”也走 `player.Controller.PreviewAt` /
  `RestartWithClip`；`Model` 只保留编辑草稿和浮层状态。
- 控制器持有 `Engine` 与 `QueueSaver` 接口，生产实现分别是 `*audio.Engine` 与
  `app.Service`；测试可注入 fake。
- 事件流仍由 TUI 的 `waitEngineCmd` / `waitDownloadEventCmd` 桥接；控制器不依赖
  Bubble Tea。

## 替代方案

- 继续在 `Model` 里维护状态机：实现快，但业务无法脱离 Bubble Tea 测试，放弃。
- 把控制器做成 tea.Model / 发 tea.Msg：会把 Bubble Tea 依赖带进 player 包，
  违背分层目标，放弃。
- 只抽下载执行不抽任务队列：并发、取消和生命周期仍留在 TUI，问题只解决一半。
