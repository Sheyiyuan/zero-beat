# 架构

## 分层

```
┌────────────────────────────────────────────────────────────┐
│ cmd/zero            CLI：tui / play / download / login / demo │
├────────────────────────────────────────────────────────────┤
│ internal/tui        Bubble Tea v2：模型、渲染、按键、命令    │
├──────────────────────────────┬─────────────────────────────┤
│ internal/app 业务服务         │ internal/audio  播放引擎     │
│  ├─ 搜索 / 取流 / 歌词        │ internal/player 队列算法     │
│  ├─ 歌单同步 / 登录 / 队列恢复│ internal/store  SQLite      │
│  └─ 不依赖 Bubble Tea         │                             │
├──────────────────────────────┼─────────────────────────────┤
│ internal/source 契约 + 注册表 │                             │
│  ├─ source/bilibili 适配器    │                             │
│  └─ internal/netease 客户端   │                             │
├──────────────────────────────┴─────────────────────────────┤
│ internal/bili   B 站 API 客户端（被适配器包装）              │
│ internal/lyrics LRC 解析、译文合并与时间轴                    │
│ internal/lrclib 外部歌词兜底（标题清洗 + 时长匹配）           │
│ internal/download 下载落盘（Range 续传 + ffmpeg 封装）       │
│ internal/config 数据目录                                     │
└────────────────────────────────────────────────────────────┘
```

依赖方向：`tui` 只依赖 `app.Service` 与 `source.Source` 接口，不按平台名写分支；
`app` 依赖 `source` / `store` / `lyrics` / `lrclib`，但不 import Bubble Tea；
`source/bilibili` 依赖 `bili`；`netease` 独立实现接口；`internal/player` 只
处理队列索引的纯算法，不依赖 Bubble Tea；`audio` / `store` / `lyrics` /
`download` 互不认识。TUI 和 CLI 都通过 `download.Run` 落盘，TUI 只
额外维护任务队列和进度行。

平台展示信息（短标记、命令行别名、链接解析、歌词名、登录 App 名）属于适配器：
`Source.Mark` 是必选方法，其余是 `Aliased` / `TargetParser` / `LyricNamer` /
`LoginNamer` 可选接口。`source` 核心包不再穷举平台名，`Registry` 负责别名查找、
目标解析与展示兜底。

## 数据流

**一次播放的完整路径**

```
用户在列表里按 enter
  → Update 拿到 KeyPressMsg
  → playSong()：置 buffering、记住当前曲目
  → playSongCmd（tea.Cmd，在 goroutine 里跑）
       └─ app.Service.Resolve
            └─ registry.GetString(song.Source)
            ├─ B 站：缺 CID 时 GET /x/web-interface/view，再 GET /x/player/playurl
            └─ 网易云：go-musicfox/netease-music 的 song_url_v1（weapi）
       └─ source.Resolve 返回 Resolved{Track, Stream}，元数据回写 songs 表
  → streamMsg 回到 Update
  → engine.PlayAt(audio.Stream{URL, Headers, Codec, ExpiresAt...}, startAt)
       ├─ 起回环 header 代理注入 Referer/Cookie；exec ffmpeg -ss startAt -i http://127.0.0.1:<port>/<token> -f s16le ... pipe:1
       ├─ 把 stdout 包进 gainReader（应用音量 × 补偿，tanh 软限幅）
       └─ oto.Player 从 gainReader 拉 PCM
  → 并行发起 lyricsCmd：source.Lyrics（B 站字幕 / 网易云 LRC + 译文），没有再用 LRCLIB
  → 顺序模式下再并行预解析下一首（prefetchMsg，只缓存一首，可跨源）
```

**搜索与歌单**

```
搜索页 s 切换：全部 / 各音源
  → app.Service.Search 对每个带 CapSearch 的 Source 并发 Search
  → 结果带 [B] / [云] 标记合并，页码取各源最大值
  → 进入队列时只保留 source + external_id，播放时再按源解析

登录后 app.Service.Playlists：
  → 首次启动补一个本地默认歌单（删掉后不再重建）
  → 任何 CapDailyRecommendations + DailyRecommender 的音源都补「每日推荐」入口
  → 不调用 RemotePlaylists，远端歌单都不自动导入

按需导入：
  → 曲库按 f 进入选择页，s 在支持远端歌单的音源之间切换
  → app.Service.RemoteFolders 只拉候选，过滤已添加
  → enter 才 EnsurePlaylist，随后正常按需刷新曲目

登录二维码：
  → 按 source 缓存 QR/ASCII/PNG，本地 150 秒有效
  → L 进入登录管理时才 ensure；切源复用缓存
  → 轮询 Expired 或缓存到期后自动重新申请

混合歌单：
  → 打开时刷新其中的远端引用
  → 有效曲目 = 按引用顺序去重 + 自己的 playlist_items - playlist_excludes
  → 删除继承曲目只写 exclusion，不修改源歌单
  → 解锁同步歌单 = kind 改为 local，保留全部曲目并停止远端同步
  → `S` 把当前歌单追加为另一个混合歌单的引用（只能引用非混合歌单）
```

**片段播放与云端写歌**

```
C → 读取 settings 里 clip:<songID> → 浮层里 { / } 设起止
  → enter 写入 settings → 重新解析并用 audio.Stream.ClipStartMS/ClipEndMS 起播
  → engine 用 -ss + -t 把解码限制在片段内；位置仍是全曲绝对时间

a → 目标选择器（本地/混合/同源云端）
  → 本地/混合：Update 内同步 SQLite
  → 云端：tea.Cmd 里调 source.PlaylistWriter.AddToPlaylist，成功后刷新已导入的镜像
```

**会话恢复与容错**

- 队列、当前曲目、进度每 5 秒写入 `queue` 表；`queueRestoredMsg` 只恢复状态不起播，第一次 `space` 用 `PlayAt` 从断点开始。
- `Stream.ExpiresAt`：B 站从 CDN 的 `deadline` / `expires` 解析，网易云从 `expi` 换算；临近过期时 seek、续播会先重新解析。
- `EventFailed` 会带着最后进度重新解析并重试一次；成功重试后计数保留，防止解码错误无限循环。
- 预解析只用于顺序模式；随机/单曲不猜下一首。队列变化、切音质、切模式都会让缓存作废。
- 旧库在 `store.Open` 里自动迁移：`remote_id` 从 `bvid` 回填，`playlists.source` 从 ID 前缀回填，旧 B 站歌曲 ID 不变。

**进度与状态**

- 播放位置：`engine.Position()` 用「已交给声卡的 PCM 字节数 ÷ 字节率 + seek 基准」换算，250ms 的 `tickMsg` 拉一次。
- 播放事件：`engine.Events()` → `waitEngineCmd` 转成 `engineEventMsg`；`EventEnded` 按模式自动下一首，`EventFailed` 先重试一次再提示错误。

## 关键契约

跨包的核心契约有两个：`source.Source`（TUI → 音源）与 `audio.Stream`（音源 → 引擎）；
云端写歌是可选的 `source.PlaylistWriter` 能力。

```go
// TUI → 音源（实际定义在 internal/source）
type Source interface {
    ID() ID
    Name() string
    Mark() string
    Search(ctx, keyword string, page int) (SearchPage, error)
    Resolve(ctx, ref Ref, quality Quality) (Resolved, error)
    Lyrics(ctx, ref Ref) ([]lyrics.Line, string, error)
    RemotePlaylists(ctx) ([]Playlist, error)
    PlaylistTracks(ctx, remoteID string) ([]Track, error)
    StartQRLogin(ctx) (QRCode, error)
    PollQRLogin(ctx, key string) (LoginStatus, error)
    // Account / User / Session / Logout ...
}

// 音源 → 引擎
type audio.Stream struct {
    URL        string
    Headers    map[string]string // B 站要 Referer
    Codec      string            // 记录实际编码，便于排障
    DurationMS int64
    ExpiresAt  time.Time
    Quality    string            // 展示用：192k / FLAC
    ClipStartMS int64            // 本地片段起点
    ClipEndMS   int64            // 本地片段终点；0 = 到结尾
}

// 引擎 → UI
type audio.Event struct {
    Type  audio.EventType // StateChanged / Ended / Failed
    State audio.State
    Err   error
}
```

音源负责把平台差异（Referer、cookie、编码选择、过期时间）压进 `Stream`，引擎只认它。
播放直链**不落库**：B 站约 2 小时、网易云约 20 分钟过期，落库只会产出脏数据。

云端歌单写入是可选能力 `source.PlaylistWriter`，由 `CapPlaylistWrite` 标记；
UI 只按能力显示云端目标，不按平台名分支。

不做封面：不抓取、不显示、不嵌入封面图，也不接 kitty/sixel；`songs.cover` 仅保留平台元数据，不参与 UI 或下载。

## 数据模型

```sql
songs(id, source, remote_id, bvid, page, cid, title, artist, album, duration, cover, added_at)
  UNIQUE(source, remote_id, page)     -- B 站 remote_id=BV 号，网易云=song id
playlists(id, source, title, kind, remote_id, created_at)  -- kind: local / remote / mixin
playlist_items(playlist_id, song_id, position, added_at)    -- 本地曲目/混合歌单自己加的曲目
playlist_refs(playlist_id, ref_id, position)                -- 混合歌单引用哪些歌单
playlist_excludes(playlist_id, song_id)                     -- 混合歌单里隐藏的继承曲目
queue(id=1, data, position, queue_index, updated)   -- 单行：队列 ID JSON + 进度
settings(key, value)                                -- volume / comp_db / mode / quality / lyric_offset:<songID> / clip:<songID> / last_add_playlist
accounts(source, payload, updated_at)               -- 登录 cookie（JSON）
```

## 事件循环约束（改 UI 前必读）

1. **Update 里不许有网络或磁盘长任务**。网络、解析、歌词都放到 `tea.Cmd`；只有队列保存、设置项、本地歌单增删改这类 SQLite 短事务留在 Update（WAL，毫秒级）；网络与解析仍然全在 Cmd。
2. **引擎回调不许直接改 Model**。只往 channel 发事件，由 `waitEngineCmd` 转成消息。
3. **每处理一次引擎事件都要重新挂 `waitEngineCmd`**，否则事件流会断。
4. **渲染必须精确到行**：每行可见宽度等于终端宽度，总行数等于终端高度；`internal/tui/view_test.go` 会校验。帮助页内容比一屏长，由 `helpOffset` 做窗口裁剪，禁止直接把全部行塞给渲染器。
5. 所有宽度计算走 `ansi.StringWidth` / `ansi.Truncate`，不要用 `len()`，否则中文会错位。

## 依赖

| 用途 | 依赖 | 备注 |
| --- | --- | --- |
| TUI | `charm.land/bubbletea/v2` `lipgloss/v2` | 需要 Go 1.26 |
| 音频输出 | `github.com/ebitengine/oto/v3` | 免 cgo，PulseAudio / CoreAudio；Windows 仅交叉编译，未适配 |
| 解码 | 系统 `ffmpeg` | 覆盖 AAC-LC / HE-AAC / FLAC / EC-3 |
| SQLite | `modernc.org/sqlite` | 纯 Go，免 cgo |
| 二维码 | `github.com/skip2/go-qrcode` | 半块字符 + PNG 兜底；与 musicfox 相同的白块/黑空映射 |
| 网易云 | `go-musicfox/netease-music` | weapi/cloudsearch + 二维码 chainId；UNM 关闭 |
| 歌词兜底 | LRCLIB HTTP API | 无额外依赖，8–20 秒超时 |
| 快捷键配置 | `github.com/BurntSushi/toml` | 动作 ID 覆盖与冲突校验 |
| 宽度计算 | `github.com/charmbracelet/x/ansi` | 中文/emoji 宽度 |

## 已知简化（与最初设计的差异）

第一版只有一个音源时故意没有抽接口（ADR-0009）。v0.3 接入网易云时，按约定先补
`source.Source`，再把 B 站适配器与网易云客户端塞进去，详见 [ADR-0010](docs/adr/0010-multi-source.md)。

仍然没有 `domain.Track` + `TrackSource` 两层模型：`store.Song` 直接带 `source + remote_id`，
足以支撑混源队列与本地歌单；跨源绑定要等 `match`/`resolve` 落地后再拆表。

云端歌单只实现了「加歌」；删除、改名、取消收藏和完整分页仍未做。播放片段只做播放区间，
不导出音频文件；区间末尾受 ffmpeg 输出缓冲影响，可能有约 100ms 的尾音。

边界：`internal/app` 承接搜索、取流、歌词、歌单同步、登录、队列恢复、歌单 CRUD、
偏好持久化、下载执行等外部业务；`internal/player` 承接队列索引、洗牌/恢复、预解析缓存
等纯算法。当前曲目、播放状态、引擎控制仍留在 `Model`，它在这里是**播放器的视图控制器**
（只调 `app.Service` 与 `audio.Engine`），不再直接承载平台、网络或存储业务。
