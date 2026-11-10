# 架构

## 分层

```
┌────────────────────────────────────────────────────────────┐
│ cmd/zero   CLI：tui / play / download / login / sync / demo  │
├────────────────────────────────────────────────────────────┤
│ internal/tui        Bubble Tea v2：模型、渲染、按键、命令    │
├──────────────────────────────┼─────────────────────────────┤
│ internal/sync 跨设备同步     │                             │
│  ├─ 快照/确定性导出          │                             │
│  ├─ git 封装/合并/冲突       │                             │
│  └─ Argon2id + XChaCha20     │                             │
├──────────────────────────────┼─────────────────────────────┤
│ internal/app 业务服务         │ internal/player 播放控制器   │
│  ├─ 搜索 / 取流 / 歌词        │  ├─ 队列/播放状态/模式        │
│  ├─ 歌单同步 / 登录 / 队列恢复│  ├─ 引擎控制/重试/预解析      │
│  └─ 不依赖 Bubble Tea         │  └─ 不依赖 Bubble Tea        │
│ internal/downloader 下载任务  │ internal/audio  播放引擎     │
│  ├─ 并发/取消/进度事件        │ internal/store  SQLite      │
│  └─ 不依赖 Bubble Tea         │                             │
├──────────────────────────────┼─────────────────────────────┤
│ internal/source 契约 + 注册表 │                             │
│  ├─ source/bilibili 适配器    │                             │
│  └─ internal/netease 客户端   │                             │
├──────────────────────────────┴─────────────────────────────┤
│ internal/sysmedia  MPRIS：系统媒体面板/媒体键桥接            │
│ internal/cover     封面下载与缓存（只服务系统媒体面板）      │
│ internal/instance  TUI 单实例锁（flock/独占文件，记录 PID）  │
│ internal/bili   B 站 API 客户端（被适配器包装）              │
│ internal/lyrics LRC 解析、译文合并与时间轴                    │
│ internal/lrclib 外部歌词兜底（标题清洗 + 时长匹配）           │
│ internal/download 下载落盘（Range 续传 + ffmpeg 封装）       │
│ internal/config 数据目录                                     │
└────────────────────────────────────────────────────────────┘
```

依赖方向：`tui` 只依赖 `app.Service` 与 `source.Source` 接口，不按平台名写分支；
`app` 依赖 `source` / `store` / `lyrics` / `lrclib`，但不 import Bubble Tea；
`source/bilibili` 依赖 `bili`；`netease` 独立实现接口；`internal/player` 持有
播放状态机并通过 `Engine` / `QueueSaver` 接口调用引擎与存储；`internal/downloader`
持有下载任务队列；`internal/sync` 负责快照、git、合并与登录态加密；`internal/sysmedia` 把播放状态
投喂给系统媒体面板并把面板/媒体键命令转回播放器，`internal/cover` 只为它下载封面；
这些包都不依赖 Bubble Tea。`sync` 只依赖 `store` / `config` / `keymap`，不 import `app` 或 `tui`；
`app.LibraryRevision()` 以回调形式注入，用于导入前判断用户是否刚改过库。`audio` / `store` / `lyrics` /
`download` 互不认识。TUI 和 CLI 都通过 `download.Run` 落盘，TUI 还维护
任务队列、进度行和下载管理页。`cmd/zero` 启动 TUI 前先通过 `internal/instance`
在数据目录拿进程级锁；CLI 子命令（play/download/sync 等）不占这把锁。

平台展示信息（短标记、命令行别名、链接解析、歌词名、登录 App 名）属于适配器：
`Source.Mark` 是必选方法，其余是 `Aliased` / `TargetParser` / `LyricNamer` /
`LoginNamer` 可选接口。`source` 核心包不再穷举平台名，`Registry` 负责别名查找、
目标解析与展示兜底。

### 模块清单

```
cmd/zero                 CLI 入口：tui / play / download / login / frame / +key / desktop 等
internal/tui             Bubble Tea v2 界面：模型、渲染、按键、命令
internal/control         本机控制通道：Unix socket，读取当前帧与注入按键
internal/app             业务服务：搜索、取流、歌词、歌单同步、登录、队列恢复
internal/player          播放控制器：队列、播放状态、引擎、模式、预解析
internal/downloader      下载任务队列、并发、取消与进度事件
internal/sync            跨设备同步：快照、git、合并、冲突、凭据加密
internal/sysmedia        Linux MPRIS：系统媒体面板/媒体键与播放器之间的桥
internal/cover           封面下载与磁盘缓存（只服务系统媒体面板）
internal/instance        TUI 单实例锁（flock / 独占文件），锁文件记录 PID
internal/desktop         Linux 应用菜单入口（.desktop + hicolor 图标）安装与卸载
internal/source          多音源契约、能力位、注册表、平台元数据
internal/source/bilibili B 站适配器：搜索、多 P、收藏夹、字幕、播放
internal/source/local    本地音源：本机文件取流、本地旁挂/内嵌歌词（无登录、无搜索）
internal/library         本地目录扫描、音频标签、曲目身份与内容指纹
internal/netease         网易云客户端：基于 go-musicfox/netease-music 的 Source 适配
internal/bili            B 站 API 客户端
internal/audio           播放引擎：ffmpeg 解码 → 增益链 → oto 输出，事件通道
internal/lyrics          LRC 解析、时间轴定位、本地编码/翻译合并、lyrics.toml
internal/lrclib          LRCLIB 歌词查询与标题清洗
internal/download        音频落盘：Range 续传、ffmpeg 封装、标签
internal/store           SQLite：歌曲、歌单、队列、设置、登录态
internal/keymap          动作 ID 与 TOML 覆盖加载
internal/config          数据目录
```

关键契约包括：`source.Source`（TUI 只知道接口，不按平台名分支）、
`app.Service`（外部业务不依赖 Bubble Tea）、`player.Controller`（播放状态机与
引擎控制）、`downloader.Manager`（下载任务队列）、`sync.Manager` / `sync.Client`
（跨设备同步）和 `sysmedia.Service`（系统媒体面板桥接）；`audio.Stream` 仍是
音源到引擎的最终输入。

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
       ├─ （可选）ffmpeg `loudnorm` 做单遍自动响度均衡：-16 LUFS / -1.5 dBTP
       ├─ 把 stdout 包进 gainReader（应用音量 ×（全局补偿 + 单曲补偿），tanh 软限幅）
       └─ oto.Player 从 gainReader 拉 PCM
  → 并行发起 lyricsCmd：source.Lyrics（B 站字幕 / 网易云 LRC + 译文），没有再用 LRCLIB
  → 列表循环/顺序模式下再并行预解析下一首（prefetchMsg，只缓存一首，可跨源）
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
- 预解析只用于列表循环与顺序模式（顺序到队尾不预取）；随机/单曲不猜下一首。队列变化、切音质、切模式都会让缓存作废。
- 登录态过期：后台每 15 分钟用 `Account` 校验一次；cookie 还在但平台返回空账号或 301/401 时自动退出，
  清掉该源账号并把它的远端歌单置为 `frozen`。混合歌单对冻结歌单的引用与缓存曲目都保留，重新登录后自动解冻。
- 旧库在 `store.Open` 里自动迁移：`remote_id` 从 `bvid` 回填，`playlists.source` 从 ID 前缀回填，旧 B 站歌曲 ID 不变。

**进度与状态**

- 播放位置：`engine.Position()` 用「已交给声卡的 PCM 字节数 ÷ 字节率 + seek 基准」换算，250ms 的 `tickMsg` 拉一次。
- 播放事件：`engine.Events()` → `waitEngineCmd` 转成 `engineEventMsg`；`EventEnded` 按模式自动下一首，`EventFailed` 先重试一次再提示错误。

**系统媒体（Linux）**

```
tickMsg / 播放状态变化
  → publishNowPlaying()（内存快照，不阻塞）
  → sysmedia.Service.Publish → MPRIS 属性与 Seeked 信号
  → 系统面板/媒体键的命令走 Commands() channel
  → waitSysmediaCmd → sysCommandMsg → applySysCommand()（toggle/next/prev/seek/volume）

当前曲目变化
  → syncArtwork()（tea.Cmd）→ app.Service.CoverPath → cover.Fetch（带 Referer，磁盘缓存）
  → coverMsg 回填 artwork 路径 → 下一次 Publish 带上本地文件
```

**一次同步**

```
Manager（single-flight + debounce + 退避）
  → Client.Sync（进程内互斥 + 进程间 flock）
       ├─ git fetch / Recover（清理残留合并与脏文件）
       ├─ 校验 meta.json 与本地 sync-key（版本/盐/keycheck）
       ├─ 登录态 LWW：远端较新先回填本地库
       ├─ 导出快照：playlists/ 清空重写、keymap、annotations、accounts
       ├─ commit → git merge FETCH_HEAD
       │    ├─ 干净：导入最终树
       │    ├─ accounts 冲突：按密文内 updated_at 自动 LWW
       │    └─ 其他冲突：快照 ours/theirs/base → abort → 冲突页
       ├─ 导入（revision 检查；单实体事务，失败跳过并记录）
       └─ push（非快进则整轮重试，绝不 force push）
  → syncEventMsg → TUI 重载歌单、回填 session、必要时重载 keymap
```

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

已下载文件不走远程解析：`app.Resolve` 先查 `downloads` 索引，命中且文件有效时直接返回
本地路径（`Quality = "本地"`，不校验用户选择的音质）；记录缺失或文件失效则回落远程。
TUI 下载页强制首轮本地播放，引擎失败重试时跳过本地、改走远程，避免坏文件死循环。

云端歌单写入是可选能力 `source.PlaylistWriter`，由 `CapPlaylistWrite` 标记；
UI 只按能力显示云端目标，不按平台名分支。

封面只服务系统媒体面板：按需下载到 `~/.config/zero-beat/cache/covers`，不参与终端渲染，
不接 kitty/sixel；`songs.cover` 仍是平台元数据。系统媒体集成只在 Linux 可用，
macOS/Windows 是空实现（`sysmedia.New` 返回 nil）。

## 数据模型

```sql
songs(id, source, remote_id, bvid, page, cid, title, artist, album, duration, cover, added_at)
  UNIQUE(source, remote_id, page)     -- B 站 remote_id=BV 号，网易云=song id，本地=meta_key
playlists(id, source, title, kind, remote_id, frozen, sync_excluded, root_path,
          scan_status, scan_error, scanned_at, created_at)  -- kind: local / remote / mixin / folder
  -- frozen=1：该源登录失效或已退出，远端镜像暂停刷新；曲目、混合歌单引用与隐藏项保留
  -- sync_excluded=1：仅本机，不导出也不同步删除；root_path 仅 folder 歌单有值
playlist_items(playlist_id, song_id, position, added_at)    -- 本地曲目/混合歌单自己加的曲目
playlist_refs(playlist_id, ref_id, position)                -- 混合歌单引用哪些歌单
playlist_excludes(playlist_id, song_id)                     -- 混合歌单里隐藏的继承曲目
queue(id=1, data, position, queue_index, updated)   -- 单行：队列 ID JSON + 进度
settings(key, value)                                -- volume / comp_db / auto_gain / song_comp:<songID> / mode / quality /
                                                    -- lyric_offset:<songID>（自动 offset）/ clip:<songID> / last_add_playlist /
                                                    -- lyric_bind:<songID>（可同步绑定）/ lyric_bind_local:<songID>（本机绑定）
accounts(source, payload, updated_at)               -- 登录 cookie（JSON）
downloads(song_id PK, path, size, codec, duration_ms, created_at)  -- 已下载文件索引；path 建索引供本地扫描复用身份
library_files(playlist_id, path, song_id, size, mtime, content_hash, present)
  PRIMARY KEY(playlist_id, path)                    -- 文件夹歌单的路径索引；present=0 表示文件缺失但保留行
```

本地曲目复用 `songs`：`source=local`、`remote_id` 由音频标签生成的身份键，
`id=local:<meta_key>:1`。身份查找顺序是「同歌单路径 → 内容指纹 → 下载映射 →
标签身份键 → 新建」，所以改名/移动不换身份、改标签不换身份。

同步是可选的外挂数据，不写进 SQLite；封面缓存与实例锁同样只在文件系统：

```
~/.config/zero-beat/
├── instance.lock                            # TUI 单实例锁（0600，内容为 PID）
├── sync.toml / sync-key / sync-token        # 连接配置、派生密钥、可选 token
├── sync/{repo,state.json,lock,conflicts,archive}
└── cache/covers/                            # 系统媒体面板封面缓存（64 MiB 上限）
仓库布局：meta.json + playlists/<id>.json + annotations/<song>.json
        + keymap.toml + accounts/<source>.age + .gitattributes
注释文件包含歌词偏移、歌词绑定（仅 portable）、单曲音量补偿与播放片段。
仓库格式 meta.v=2：v1 仓库可读，首次成功同步后原地升级；旧客户端读到 v2 会要求升级。
```

歌单与注释是明文 JSON（一实体一文件，`merge=binary`）；登录态是 XChaCha20-Poly1305
密文，密钥由口令 + `meta.json` 的全局盐经 Argon2id 派生。冲突与归档只在本地
`sync/` 下，不进入仓库。

## 事件循环约束（改 UI 前必读）

1. **Update 里不许有网络或磁盘长任务**。网络、解析、歌词都放到 `tea.Cmd`；只有队列保存、设置项、本地歌单增删改这类 SQLite 短事务留在 Update（WAL，毫秒级）；网络与解析仍然全在 Cmd。
2. **引擎回调不许直接改 Model**。只往 channel 发事件，由 `waitEngineCmd` 转成消息。
3. **每处理一次引擎事件都要重新挂 `waitEngineCmd`**，否则事件流会断。
4. **渲染必须精确到行**：每行可见宽度等于终端宽度，总行数等于终端高度；`internal/tui/view_test.go` 会校验。帮助页内容比一屏长，由 `helpOffset` 做窗口裁剪，禁止直接把全部行塞给渲染器。
5. 所有宽度计算走 `ansi.StringWidth` / `ansi.Truncate`，不要用 `len()`，否则中文会错位。
6. **同步事件也要重新挂 `waitSyncCmd`**。`sync.Manager` 的网络/磁盘工作在后台
   goroutine，事件通过 channel 转成 `syncEventMsg`；`Update` 只更新状态、发起
   `tea.Cmd`，不直接跑同步。
7. **系统媒体命令也走事件**。MPRIS 命令从 `sysmedia.Service.Commands()` 经
   `waitSysmediaCmd` 变成 `sysCommandMsg`，`Update` 只做状态映射；封面下载在
   `tea.Cmd` 里完成，失败只影响封面，不影响播放。

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
| 快捷键配置 | `github.com/BurntSushi/toml` | 动作 ID 覆盖与冲突校验；同步配置也用它 |
| 同步加密 | `golang.org/x/crypto` | Argon2id、HKDF、XChaCha20-Poly1305 |
| 同步口令输入 | `golang.org/x/term` | CLI 无回显读取口令 |
| 系统媒体 | `github.com/godbus/dbus/v5` | Linux 会话总线 / MPRIS；非 Linux 编译为空实现 |
| 宽度计算 | `github.com/charmbracelet/x/ansi` | 中文/emoji 宽度 |

## 已知简化（与最初设计的差异）

第一版只有一个音源时故意没有抽接口（ADR-0009）。v0.3 接入网易云时，按约定先补
`source.Source`，再把 B 站适配器与网易云客户端塞进去，详见 [ADR-0010](docs/adr/0010-multi-source.md)。

仍然没有 `domain.Track` + `TrackSource` 两层模型：`store.Song` 直接带 `source + remote_id`，
足以支撑混源队列与本地歌单；跨源绑定要等 `match`/`resolve` 落地后再拆表。

云端歌单只实现了「加歌」；删除、改名、取消收藏和完整分页仍未做。播放片段只做播放区间，
不导出音频文件；区间末尾受 ffmpeg 输出缓冲影响，可能有约 100ms 的尾音。

边界：`internal/app` 承接搜索、取流、歌词、歌单同步、登录、队列恢复、歌单 CRUD、
偏好持久化、下载执行与本地优先解析等外部业务；`internal/player.Controller` 承接队列、当前曲目、
播放状态、模式、重试、预解析、引擎控制与落盘；`internal/downloader.Manager` 承接
下载任务、并发与取消。`Model` 只保留视图状态、按键路由和播放状态的只读镜像，
不再决定「下一首是谁」「什么时候重试」「队列怎么变」；片段编辑器的整首预览
也通过 `player.Controller` 完成。
