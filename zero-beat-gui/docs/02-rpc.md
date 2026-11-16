# 本机 RPC

Dart 与 Go 在同一个进程里（Windows 为 `zero-core.exe` sidecar），但仍然通过回环 socket 通信。理由：

- 事件推送天然异步，不需要在 FFI 里做回调注册与生命周期管理；
- 同一套协议可以复用到桌面调试、`zero` CLI 验证和 Windows sidecar；
- 序列化边界强制 DTO 稳定，避免把 Go 结构体（无 json tag、含 `time.Time`）直接漏给 UI。

## 传输

- TCP，绑定 `127.0.0.1:0`，由核心返回实际端口；Windows sidecar 通过 stdout 握手行返回 `{port, token}`；
- 每行一个 JSON 对象（NDJSON，UTF-8，`\n` 结尾），单行上限 8 MiB；
- 连接建立后第一条必须是 `core.hello`，携带 `token`；校验失败立即断开；
- token 由 `ZeroStart` 返回，只存在内存里，不落盘；端口随机；
- 只监听回环，不接受局域网连接；Android/Windows 上同样只绑 `127.0.0.1`。

## 信封

采用 JSON-RPC 2.0 子集，双向都可用：

```json
{"jsonrpc":"2.0","id":17,"method":"library.playlists","params":{}}
{"jsonrpc":"2.0","id":17,"result":{"playlists":[]}}
{"jsonrpc":"2.0","id":17,"error":{"code":-32010,"message":"曲库暂不可用","data":{}}}
{"jsonrpc":"2.0","method":"player.state","params":{"state":"paused","playback_id":null,"position_ms":0}}
```

- 请求必须有 `id`（正整数，连接内唯一，限制在 JSON 安全整数范围）；Dart 发奇数、Go 发偶数，重连清空 pending 表；通知（无 `id`）只用于事件；
- 结果必须是对象，避免顶层数组/标量；
- 错误码：`-32700` 解析失败、`-32600` 非法请求、`-32601` 未知方法、`-32602` 参数错误、`-32603` 内部错误；业务错误从 `-32000` 起；
- 所有方法带 3 秒超时（解析/搜索类可到 20 秒，单独声明）。
- `player.play/next/prev` 在会话接受请求后返回 `{request_id}`，不等解析或出声；播放结果走 `player.state`。`download.start` / `sync.start` 返回任务标识，长任务通过事件报告完成。
- 请求处理必须与读循环分离；响应直接完成 pending future。写操作由单一队列发送，不允许两个 writer 拼接帧。

## 命名空间与方向

| 前缀 | 方向 | 用途 |
| --- | --- | --- |
| `core.*` | Dart → Go | 握手与协议控制 |
| `library.*` / `search.*` / `source.*` / `lyrics.*` / `auth.*` / `player.*` / `download.*` / `settings.*` / `sync.*` / `app.*` | Dart → Go | 业务调用；方法名以以下清单为准 |
| `engine.*` | Go → Dart | 音频引擎命令（load/pause/seek/volume/…） |
| 事件（无 id） | 双向 | `player.state`、`engine.position`、`sync.state`、`download.progress` 等 |

## 握手

`core.hello`：

```json
{"token":"<ZeroStart 返回的随机 token>","api":1,"repo":2,"client_version":"0.1.0","platform":"linux","engine_capabilities":["clip","loudnorm"]}
```

响应：

```json
{
  "api": 1,
  "repo": 2,
  "core_version": "0.1.0",
  "data_dir": "/home/me/.config/zero-beat",
  "capabilities": ["loudnorm", "clip", "downloads", "sync", "mpris", "tray"],
  "account": {"bilibili": {"logged_in": true, "name": "..."}, "netease": {"logged_in": false}},
  "player": {"queue_len": 42, "index": 3, "state": "paused", "position_ms": 0, "playback_id": null}
}
```

`api` 不匹配时客户端必须拒绝连接并提示升级；`repo` 用于提示同步格式兼容性。

`data_dir` 由启动入口确定，握手不能重定向数据库。引擎能力以本机探测结果为准，下载能力另含经验证的容器范围（见 [04-sync-and-data](04-sync-and-data.md)）。

## 方法清单（v1）

### 协议控制

| 方法 | 说明 |
| --- | --- |
| `core.hello` | 首帧鉴权、版本与能力协商 |
| `core.cancel` | `{rpc_id}`，请求取消本连接中由调用方发起的未完成业务 RPC；不得取消另一方向的请求 |

取消是尽力而为：服务端取消对应 context，已经提交的写操作不回滚；客户端需重新查询结果，不盲目重试写请求。已接受的下载任务使用 `download.cancel`，新点歌由 session 取消前一次解析；连接断开取消未完成请求。方法超时同样取消服务端任务，不能只让客户端停止等待。

### 曲库与歌单

| 方法 | 说明 |
| --- | --- |
| `library.playlists` | 歌单树（本地/远端/混合/文件夹），含仅本机标记 |
| `library.songs` | 分页取歌单曲目（含混合歌单展开、excludes） |
| `library.local_songs` | 本机曲目（桌面文件夹歌单） |
| `library.create` / `library.rename` / `library.delete` | 本地歌单 CRUD |
| `library.add_songs` / `library.remove_song` | 曲目增删 |
| `library.import_remote` | 导入远端歌单 |
| `library.add_folder` / `library.rescan` | 文件夹歌单（桌面；Android 后续） |
| `library.refresh` | 强制刷新远端镜像 |

### 搜索与解析

| 方法 | 说明 |
| --- | --- |
| `search.query` | 合并/单源搜索，分页，返回 `pages` 与告警 |
| `search.local` | 本地曲库搜索 |
| `source.resolve` | 按 song_id 解析播放地址（不直接给 UI 用，供调试/预取） |
| `source.lyrics` | 歌词（平台 → 本地 → LRCLIB），含来源与偏移 |
| `lyrics.bind` / `lyrics.unbind` / `lyrics.offset` | 歌词来源绑定与偏移 |

### 登录

| 方法 | 说明 |
| --- | --- |
| `auth.qr.start` | 生成二维码（PNG/ASCII 由 UI 选择） |
| `auth.qr.poll` | 轮询状态（waiting/scanned/expired/success） |
| `auth.logout` | 清除登录态并冻结该源镜像，保留曲目与混合歌单引用 |

### 播放（命令进 Go，声音出 Dart）

| 方法 | 说明 |
| --- | --- |
| `player.play` | 指定 song_id/队列位置起播 |
| `player.toggle` / `player.pause` / `player.resume` | 播放/暂停 |
| `player.next` / `player.prev` | 切歌（受模式影响） |
| `player.seek` | 绝对位置（ms） |
| `player.mode` | 顺序/列表循环/单曲/随机 |
| `player.volume` / `player.mute` | 音量与静音 |
| `player.comp` | 全局/单曲补偿（dB） |
| `player.auto_gain` | 自动响度均衡开关 |
| `player.clip` | 设置/清除片段（起止 ms） |
| `player.sleep` | 睡眠定时（分钟，0 取消） |
| `player.queue` | 队列读取、移动、删除、清空、下一首播放 |
| `player.state` | 拉取当前状态（订阅用事件） |

### 下载与设置

| 方法 | 说明 |
| --- | --- |
| `download.start` / `download.cancel` / `download.list` | 下载任务与进度事件 |
| `settings.get` / `settings.set` | 音量、音质、模式、本地优先等 |
| `app.quit` | 持久化并关闭核心；完成响应后再关闭传输，超时由宿主执行退出流程 |

### 同步

| 方法 | 说明 |
| --- | --- |
| `sync.status` | 仓库配置、上次同步、仅本机计数 |
| `sync.configure` | 仓库地址、分支、认证方式 |
| `sync.start` | 手动同步（单飞） |
| `sync.conflicts` | 冲突列表 |
| `sync.choose` | 逐项选择 ours/theirs |
| `sync.unlock` | 口令派生密钥（只在本机内存） |

### 引擎（Go → Dart）

| 方法 | 说明 |
| --- | --- |
| `engine.load` | `{playback_id, url, headers, codec, duration_ms, quality, clip_start_ms, clip_end_ms, start_at_ms, play_when_ready, volume, comp_db, auto_gain}` |
| `engine.pause` / `engine.resume` / `engine.stop` | 传输控制 |
| `engine.seek` | 绝对位置（ms） |
| `engine.volume` | 0.0–1.0 |
| `engine.comp` | 总增益（dB，含单曲补偿） |
| `engine.auto_gain` | 开关 loudnorm |
| `engine.state` | Go 主动查询；返回 `{playback_id, seq, state, position_ms, duration_ms}`，无已加载流时 playback_id 为 null |

除查询 `engine.state` 外，引擎命令都携带目标 `playback_id`；过期目标返回 `stale_playback`，不得作用于新曲目。尚未加载时音量/增益等只写 Go 缓存，在 load 中一次性传入；`play_when_ready` 来自 session 的播放意图，暂停中的 seek/恢复不能先出声再补 pause。

`engine.load` 在 3 秒内返回 `{accepted:true, playback_id}`，表示已验证参数、安装代次并进入串行执行队列；不代表已缓冲或已出声。打开流的成功/失败通过事件报告，避免把慢网络误判为 RPC 超时。拒绝接受使用 JSON-RPC `error`，不另设 `{ok:false}` 信封。Go 的同步 `PlayAt` 在收到接受响应后返回；真实缓冲状态由后续引擎事件校准。

接受后仍有独立的起播期限：默认 20 秒内必须就绪（playing，或按意图停在已就绪的 paused），否则取消该加载并发送一次 `engine.failed`。5 秒尚未就绪只提示网络较慢；不得因已返回 ack 而无限等待缓冲。期限和时钟在测试中可注入。

### 播放代次、错误与重连

- `playback_id` 是 Go 为每次实际加载分配的不透明字符串，跨核心重启也不复用；同曲重试/重新打开流也要换代次。解析请求号、JSON-RPC `id`、播放代次是三个不同的标识。
- 新点歌在 `BeginPlay` 时立即使旧代次在 Go 侧失效；解析结果用请求号筛选，引擎通知用播放代次筛选。所有引擎事件再带该代次递增的 `seq`，接收方丢弃旧序号；每代只接受一次 `ended` 或 `failed` 终态，之后该代次的普通状态/位置也无效。
- Dart 安装新代次时取消旧加载操作，串行执行 load/seek/stop 等命令；执行前及异步返回后均校验代次，回调必须绑定产生它的操作，不能把迟到回调贴上当前代次。适配器需验证 media_kit 的事件归属；不能可靠区分时，采用停止并隔离旧订阅/重建底层播放器等方案，通过切歌乱序测试后才启用。
- load 拒绝/超时由同步错误路径交给 session；已接受的 load 通过 `engine.failed` 报告失败。同一故障只进入控制器一次；超时后迟到的响应/事件无效。自动重试计数由 session 保留，不因 load 成功清零。
- 暂停、停止、音量等控制命令超时不触发自动重新播放。连接仍可用时查询 `engine.state` 校准；不能确认状态时转入连接恢复。seek 失败需要重新解析时，也必须沿用本次播放的重试预算。
- 断线后 Dart 停止播放、清空旧命令并隔离旧异步回调；Go 失效旧代次、保存最后确认的位置，断线期间不发起自动重试。重连只允许一个引擎所有者，先查询 `engine.state` 确认旧播放已停止，再重拉业务快照；不重放未确认的 next/play 等命令，不自动起播。用户续播时创建新代次。

## 事件

| 事件 | 方向 | 负载 |
| --- | --- | --- |
| `player.state` | Go → Dart | request_id、playback_id（未加载时为 null）、队列长度、索引、当前曲目、状态、位置、时长、模式、音量、补偿、片段、睡眠定时 |
| `player.changed` | Go → Dart | 当前曲目变化（用于通知/封面） |
| `engine.position` | Dart → Go | `{playback_id, seq, position_ms, duration_ms}`，250ms |
| `engine.state` | Dart → Go | `{playback_id, seq, state: buffering/playing/paused/idle}`；终态只用以下事件 |
| `engine.ended` / `engine.failed` | Dart → Go | `{playback_id, seq, position_ms, error?}`；自然结束/失败，主动 stop 不发送 ended |
| `library.changed` | Go → Dart | 本地/远端歌单变化，UI 重拉 |
| `download.progress` | Go → Dart | `{song_id, done, total, state}` |
| `sync.state` | Go → Dart | `{phase, message, error?}` |
| `notice` | Go → Dart | `{text, level}` |
| `app.activate` | Go → Dart | 第二次启动请求显示主窗口 |

事件不重放；断线后按上述流程恢复，再拉 `player.state` 与当前列表。传输与 session 消息队列必须有界；只有位置通知可以合并，响应和终态不可静默丢弃，可靠消息无法入队时关闭连接。合并位置时不得越过后续终态的序号顺序。

## DTO 规则

- 独立定义 DTO，**不要直接序列化 `store.Song`**：它没有 json tag，`AddedAt` 是 `time.Time`；
- 时间：仅 RPC DTO 的绝对时间用 RFC3339（UTC），时长/位置用毫秒整数；同步仓库保持现有 v2 字段类型与单位，不套用 RPC 规则；
- ID 一律字符串；`source` 用 `"bilibili"` / `"netease"` / `"local"`；
- 金额、浮点音量用 `double`，范围在文档里写明；
- 字段只增不改；破坏性变更提升 `api`；
- 错误用 `{code, message, data}`，`message` 给用户看，`data` 给日志。

歌曲 DTO：

```json
{
  "id": "bilibili:BV1xx:1",
  "source": "bilibili",
  "remote_id": "BV1xx",
  "title": "…",
  "artist": "…",
  "album": "…",
  "duration_ms": 245000,
  "cover": "https://…",
  "added_at": "2026-11-01T12:00:00Z"
}
```

播放流 DTO：

```json
{
  "url": "https://…",
  "headers": {"Referer": "https://www.bilibili.com/"},
  "codec": "aac",
  "duration_ms": 245000,
  "quality": "192k",
  "expires_at": "2026-11-01T14:00:00Z",
  "clip_start_ms": 0,
  "clip_end_ms": 0
}
```

## 安全

- 只绑回环；token 随机 128-bit；hello 失败即断开；
- 不把 cookie/Authorization 写进 URL；headers 只在内存与 RPC 帧里；
- 日志脱敏：URL 去掉 query，headers 只留键名；
- 回环地址不是应用隔离边界；Android 上具备网络权限的其他 App 也可能连接。随机端口只用于分配，鉴权依赖 token；未认证连接不能触发业务、订阅事件或取得引擎所有权。
- hello 必须在连接后 3 秒内完整到达，认证前帧上限 16 KiB、并发未认证连接上限 8；失败即关闭并释放资源。认证后才启用 8 MiB 帧上限与方法分发。
- 每个核心只允许一个已认证的 Dart 控制连接；第二连接被拒绝，重连必须等旧连接注销。token 在核心重启时重新生成，比较采用常数时间实现，不记录到日志。

## 契约测试

- `testdata/rpc/*.json`：每个方法的请求/响应样例，Go 与 Dart 各跑一遍解析；
- 事件顺序测试：起播 → buffering → playing → ended → 下一首；
- 代次测试：快速 A→B、A 的迟到 position/ended/failed、同曲重试、重复终态均不污染 B 或重复切歌；
- 双向调用测试：Go 等待 load ack 时读循环仍可接收响应与事件；队列满不会死锁，可靠消息不静默丢失；
- 超时/断线测试：慢缓冲不误判 load 超时、暂停超时不重播、断线双方停止旧代次、重连不重放命令；
- 取消测试：搜索超时/主动取消释放服务端任务；取消后到达的旧解析结果无效，已提交写操作通过重拉校准；
- 鉴权测试：缺失/错误 token、超时 hello、超大首帧、并发连接超限均被拒绝且无业务副作用；
- 版本测试：`api` 不匹配必须拒绝。
