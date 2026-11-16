# 架构

## 进程模型

**Linux/Android/macOS：单进程**。Flutter App 启动时加载 Go 核心（Linux/Android `libzero.so`，macOS `libzero.dylib`），Go 与 Dart 在同一个进程里，通过本机回环的 NDJSON/JSON-RPC 通信。

**Windows：Flutter + `zero-core.exe` sidecar**。Go 核心编成纯 Go 可执行文件（`CGO_ENABLED=0`），由 Flutter 启动/结束，RPC 协议与单进程模式完全一致；这样避开 MinGW 编 Go DLL 的 ABI/运行库问题。

```
Flutter App 进程（Linux/Android/macOS）
├─ Dart UI isolate ── audio_service / 通知 / 托盘 / 窗口
├─ Dart 播放引擎 ── media_kit（libmpv）── 系统音频输出
├─ Go runtime（libzero.so / libzero.dylib）
│   ├─ session（session、DTO、Engine 接口）
│   ├─ store / app / source / library / lyrics / keymap
│   ├─ player.Controller（队列、模式、预取、重试）
│   ├─ sync（Linux/macOS git CLI；Android go-git；自动回退）
│   ├─ sysmedia（Linux MPRIS）、instance（flock）
│   └─ RPC server（127.0.0.1:0 + token）
└─ 同一进程内的 socket 连接（Dart ↔ Go）
```

Windows：

```
Flutter App（Windows） ── RPC ── zero-core.exe（session + sync(go-git) + LockFileEx）
```

没有独立 daemon。原因：

- Android 上后台进程受系统管理，额外的 Go 守护进程需要自己的前台服务，复杂且更容易被杀；
- 桌面（Linux/macOS）「关窗继续放」要求进程活着，而 Dart 的音频引擎本来就必须在这个进程里；
- 多进程只增加 IPC 和生命周期复杂度，不带来隔离收益。

Windows 的 sidecar 是例外，不是为了后台常驻，而是为了让 Go 核心以纯 Go 方式构建；Flutter 退出时结束它。

TUI 仍是独立二进制，与 GUI 在桌面互斥（见 [05-lock-and-lifecycle](05-lock-and-lifecycle.md)）。

## 层次与职责

| 层 | 位置 | 职责 | 不做什么 |
| --- | --- | --- | --- |
| UI | Flutter | 页面、导航、输入、状态展示 | 不碰 SQLite、不写 sync 仓库 |
| 系统集成 | Dart | audio_service、audio_session、SMTC（Windows）、Now Playing（macOS）、通知、锁屏、托盘、窗口 | 不决定「下一首是谁」 |
| 播放引擎 | Dart | media_kit：解码、输出、seek、loudnorm、片段边界 | 不决定队列与重试 |
| 会话编排 | zero-beat `session.Session` | 解析地址、起播、事件处理、预取、持久化、封面、系统媒体状态 | 不渲染 |
| 业务 | zero-beat `session` 门面 → `app` / `source` | 搜索、登录、歌单、歌词、下载、同步 | 不依赖 UI |
| 数据 | zero-beat `store` | SQLite、迁移、队列快照 | 不感知平台 |
| 桥 | GUI `core/rpc` + Dart client | 方法调用、事件推送、DTO | 不含业务规则 |

> 边界：业务已迁入 zero-beat-core 的公开包。禁止跨模块 internal 引用；GUI 后续通过公开 `session` / `sync` 接入，见 [ADR-0006](decisions/0006-module-boundary.md)。

### 为什么需要 `session`

zero-beat 的 `player.Controller` 已经承接了队列、模式、`HandleEvent`（重试/结束）、`PlayResolved`、`Tick`（睡眠定时/持久化）等逻辑；但「解析地址 → 交给控制器 → 处理事件 → 预取下一首 → 发布封面/MPRIS」这段编排目前写在 TUI 的 `playSongCmd` / `handleEngineEvent` / `publishNowPlaying` 里。

GUI 需要一份不依赖 Bubble Tea 的 headless 编排。它放在 **zero-beat 的 `session.Session`**（公开包），行为以 TUI 的对应函数和测试为规格：

- `Play(song, startAt, retry)`：先串行调用 `controller.BeginPlay` 取得请求号，再在 worker 中 `app.Resolve`，完成后回到会话队列调用 `controller.CompletePlay(req, ..., retry, err)`；过期请求直接丢弃。新点歌取消旧解析任务，取消后仍返回的结果也必须校验请求号；
- `onEngineEvent`：先校验播放代次，再交给 `controller.HandleEvent`；`Retry` 时沿用控制器返回的计数重新解析，`Ended` 时只触发一次下一首；
- `PlayResolved` 目前会把重试计数置零，只适用于显式开始的新播放（如新播放命中预取）；不能用作异步解析完成或自动重试的通用入口。首次尝试与自动重试合计最多两次，详见 [03-playback](03-playback.md)；
- `Tick`：`controller.Tick(now)`、每 5 秒 `Persist`、睡眠定时、位置事件；
- `Prefetch`：列表循环/顺序模式下预解析下一首（复用 `player.NextPlayableIndex`）；
- `Publish`：封面（`app.CoverPath`）与系统媒体状态（Linux MPRIS / macOS Now Playing）。

放在 `session` 而不是 GUI 仓库，是因为 GUI 无法 import `internal/player` / `internal/app`（[ADR-0006](decisions/0006-module-boundary.md)）；TUI 后续也可以迁到同一份 `Session`，避免两套编排。

## 启动序列（Linux/macOS）

1. Flutter 准备启动配置，桌面默认不传 `data_dir`，由 `ZeroStart` 内复用 TUI 的 `config.DataDir()` / `os.UserConfigDir()`；Linux 尊重 `XDG_CONFIG_HOME`，未设置时为 `~/.config/zero-beat`，macOS 为 `~/Library/Application Support/zero-beat`。当前 TUI 不支持 `ZERO_BEAT_DATA_DIR`，GUI 不单独引入该覆盖项。
2. `DynamicLibrary.open("libzero.so")`（macOS 为 `libzero.dylib`），调用 `ZeroStart(configJSON)`。
3. Go 核心先确定并规范化目录、`instance.Acquire`，再打开 SQLite（WAL、迁移）、构建 source registry 和 session。拿不到锁则返回「已在运行」，第二实例只尝试激活已有 GUI 后退出，不能打开数据库或初始化音频。
4. 核心读取队列并 `controller.Load`，恢复状态但不自动起播；随后启动 RPC/激活端点并返回 `{port, token, api, version}`。任何启动失败都逆序关闭已建资源，最后释放锁。
5. Flutter 初始化唯一的引擎适配器，连接 RPC 并发送 `core.hello`（含 token、引擎能力）；握手完成后启动 sync manager、系统媒体命令消费，核心返回恢复后的播放快照。
6. Flutter 拉歌单/曲目、订阅事件并渲染首屏。
7. 用户点歌：`player.play` → `BeginPlay` → 异步解析 → `CompletePlay` → `engine.load` 到 Dart → media_kit 播放。

macOS 差异：

- App **关闭 App Sandbox**，直接读写 `~/Library/Application Support/zero-beat`，与 TUI 共用；
- 系统媒体走 Dart 的 `audio_service`（Now Playing/控制中心），Go 侧没有 MPRIS；
- 关闭窗口 = 隐藏，Cmd+Q 才退出；Dock 点击恢复；
- dylib 需签名并放入 `Contents/Frameworks`，沿用 `@rpath`。

## 启动序列（Windows）

1. Flutter 准备启动配置，默认目录由 sidecar 复用 TUI 规则解析为 `%APPDATA%\zero-beat`；
2. 启动 `zero-core.exe --port 0` 并绑定 Job Object；
3. 核心先拿 `instance.lock`（`LockFileEx`），再打开 SQLite、迁移、恢复队列并启动 RPC；成功后才向 stdout 写端口/token 握手行，失败返回结构化错误；
4. 后续与 Linux 相同；不同点是：
   - 系统媒体走 SMTC（Dart 侧 `smtc_windows`），没有 MPRIS；
   - 退出时 Flutter 先持久化，再结束子进程；
   - 第二次启动通过命名管道激活已有实例。

## 启动序列（Android）

与 Linux 相同，差异：

- 数据目录由 Flutter 通过 `path_provider` 取应用沙盒路径传入，核心不调用 `os.UserConfigDir`；
- 不获取桌面 `instance.lock`；Activity 与 audio_service 使用同一进程、同一核心，`ZeroStart` 必须防止重复初始化；
- 首次播放前由 audio_service 启动前台服务；这降低回收概率，不构成进程永久存活保证，需真机锁屏验收；
- 没有托盘/小窗，系统集成走通知与锁屏。

## 播放数据流

```
UI 点歌
  → RPC: player.play(song_id)
  → session.Play
      → controller.BeginPlay → req，旧播放事件失效
      → worker: app.Resolve（本地优先/远程，B 站或网易云）
      → session 队列: controller.CompletePlay(req, ..., retry, err)
          → RemoteEngine.PlayAt（新 playback_id）
              → RPC: engine.load {playback_id, url, headers, start_at_ms, clip, ...}
                  → Dart 确认接受命令；media_kit 异步打开并从指定位置播放
  ← 事件流：
      engine.state / position / ended / failed（均带 playback_id、seq）
  → 丢弃旧代次/重复事件
  → controller.HandleEvent
      → Retry：重新解析并重试一次
      → Ended：controller.Next → 解析下一首 → 循环
```

位置与持久化：

- Dart 每 250ms 推 `engine.position`；Go 缓存，`controller.Position()` 直接读缓存，不做同步 RPC。
- 核心每 250ms 导出 `controller.Export()`，有变化时推 `player.state`；UI 与 audio_service 都消费这一份。
- 每 5 秒或状态变化时 `controller.Persist()` 写 `queue` 表，退出/暂停/切歌时也写一次。

## 线程与事件

- Go 侧：`session` 用单 goroutine 队列串行执行控制器调用；网络解析在可取消 worker 中执行，完成结果携带请求号回队列。ticker 同样投递消息。
- RPC 读循环、写循环和请求处理分离；读循环直接把响应交给 pending-request 表，不能等 session 执行完当前调用。`RemoteEngine` 的缓存使用独立短锁，等待 ack 时不持缓存锁；响应分发不得获取 session 的锁或等待其队列消费。
- Dart 侧：UI isolate 持有 media_kit Player 与 audio_service handler；不要在多 isolate 里重复创建 Player。
- 队列有界：位置通知可合并为最新值；响应、结束和失败事件不可静默丢弃。可靠消息队列溢出则关闭连接并按断线处理，禁止无限增长或阻塞 RPC 读循环。
- 禁止：在 RPC 读循环或 session 队列里做长网络请求，在 Dart UI isolate 做同步 FFI 大调用。`ZeroStart` 在工作 isolate 执行，Player 仍只由 UI isolate 持有。

## 失败处理

| 场景 | 行为 |
| --- | --- |
| RPC 超时（3s） | 返回错误；UI 提示，不崩溃 |
| 核心 panic | RPC handler `recover`，记录日志，返回 internal error；进程不退出 |
| 引擎启动失败 | 同步错误或 `engine.failed` 归一为一次失败，保留 retry 计数；至多自动重试一次 |
| 链接过期 | 播放/seek 失败 → `Resolve` 重新取流（`Stream.ExpiresAt` 逻辑复用） |
| Dart 引擎无响应 | load 超时按当前代次失败处理；暂停/停止/音量等控制超时不触发重新起播，按 [02-rpc](02-rpc.md) 恢复 |
| RPC 断线 | Dart 停止旧播放并作废命令；Go 保存最后确认的位置、失效旧代次，重连后校准为未播放状态，用户显式续播 |
| 同步失败 | 不阻塞播放；状态事件推给 UI，可手动重试 |
| 数据库锁 | WAL + 5s busy_timeout；写冲突交给 SQLite |

## 测试策略

- Go：`session` 用 fake engine + 临时 SQLite 验证解析乱序、取消后返回、失败两次停止、重试成功后再次失败仍停止；复用现有 `player` / `app` 测试。
- RPC：固定请求/响应 JSON；验证双向调用等待期间仍可分发响应、旧代次事件无效、重复终态去重、断线和有界队列溢出。
- 启动：已有 TUI/GUI 持锁时第二实例不执行 `store.Open`，数据库内容/schema 不变；覆盖自定义 `XDG_CONFIG_HOME`、启动失败释放锁、重复 `ZeroStart`。
- Dart：引擎封装用 mock 平台通道测状态机；UI 用 golden 测试关键页面。
- 端到端：Android 模拟器 + Linux 桌面 + macOS 各跑一条「登录→搜索→播放→切歌→同步」脚本；Windows（P7）随后补。
- 性能：2000 首列表滚动、歌词 60fps、冷启动 < 2s、内存峰值记录。
