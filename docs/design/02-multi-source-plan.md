<!-- 立项评估，保留作历史资料；实现状态见 docs/IMPLEMENTATION.md。 -->

# 多平台（B 站 + 网易云）TUI 重写方案评估

> 前提：独立新项目、全新 TUI、双平台登录、歌单可混装任意平台单曲、保留 v1 桌面版全部功能、语言在 Go / Rust 之间选。
> 结论依据：[TUI 重写评估](01-tui-feasibility.md) 的实测数据 + 本轮新增的 netease-music / symphonia / NCM 音质接口实测。

---

## 0. 结论先行

| 判断 | 评价 |
| --- | --- |
| 多平台统一 + 混源歌单 | **方向正确，而且这是唯一值得重写的理由**。单源 TUI 只是 v1 的降级版，混源才有新价值。 |
| "保留现有设计的全部功能" | **这条会翻车**。v1 的功能有一半是 B 站特有的（多 P、收藏夹双向同步、片头片尾区间），直接搬进混源模型会写出一堆 `if source == bilibili`，抽象就废了。必须先做"能力矩阵"重新定义功能，而不是逐条移植。 |
| 一步到位做双源 | **不建议**。先把 `Source` 接口和数据模型按多源设计好，但只实现 B 站，4–6 周就能有可用产品；网易云作为第二个插件再加，3–4 周。总工期几乎一样，风险分布完全不同。 |
| Go 还是 Rust | **Go**。省下来的不是"语法熟悉度"，是 v1 的 8,189 行领域逻辑 + 2,668 行测试 + 一个现成的网易云客户端（203 个文件、160+ API、含 weapi/eapi 加密和二维码登录）。选 Rust 等于把这些重写一遍，而换来的收益（音频库质量）并没有想象中大——见第 6 节。 |
| 独立项目 | 支持，但**必须是"搬迁"而不是"重写"**。新仓库、新模块名、v1 冻结只修 bug，领域层成建制搬过去，测试跟着搬。 |
| 工期 | 核心版（B 站接入 + 多源接口预留 + 混源歌单模型 + 播放 + 歌词）**60–75 人日**；补上网易云并对齐 v1 全部功能 **130–175 人日**（单人全职 6–9 个月，重度 AI 辅助压到 3.5–5 个月）。 |

---

## 1. 方案里哪部分是对的，哪部分要改

### 对的

1. **混源歌单是差异化**。musicfox 只做网易云，Azusa 只做 B 站。一个歌单里同时有"B 站 UP 主翻唱"和"网易云原版"，是目前没人做好的一件事。
2. **跨源兜底是被低估的杀手功能**。网易云灰歌（无版权/下架）在 B 站往往有搬运；B 站视频被删了，网易云可能还有。只要做了平台绑定层，"这首放不了就换另一个平台的版本"是顺带实现的——这才是混源真正的价值，不是把两个列表拼在一起。
3. **独立项目**。v1 是 Wails + React 的形态，硬塞一个 TUI 进去会让两套 UI 都难受；新仓库 + 冻结 v1 是干净的。

### 要改的

**（1）"保留全部功能"改成"能力矩阵 + 分层交付"。**

现有功能的平台归属：

| v1 功能 | B 站 | 网易云 | 混源模型下怎么定义 |
| --- | --- | --- | --- |
| 播放 / 队列 / 三种模式 | ✅ | ✅ | 通用，但要加"跨源解析与回退" |
| 音量补偿（全局 + 单曲） | ✅ | ✅ | 通用（纯 DSP，与平台无关） |
| 播放区间（片头片尾） | ✅ 刚需 | 弱需求 | 通用化成"每曲可设起止点"，NCM 侧当剪辑用 |
| 歌词（多源、偏移、锁定） | ✅ 字幕 | ✅ 官方 LRC | 通用，源数量 +1 |
| 多 P 分集 | ✅ 独有 | ❌ | 用 `Capabilities` 标记，UI 里按能力显隐 |
| 收藏夹双向同步 | ✅ 收藏夹 | ✅ 歌单 | 抽象成"远端歌单绑定 + 同步模式（只读/双向）" |
| BV 号解析 | ✅ 独有 | ❌ | 平台特有输入语法，注册到命令解析器 |
| 扫码登录 | ✅ | ✅ 还有手机号/邮箱 | 统一 Account 页，登录方式由 Source 声明 |
| 下载 / 缓存 | ✅ | ✅ | 通用，按源分目录，内容哈希去重 |
| 音质选择 | v1 其实没有（有 bug，见下） | 8 档（standard→jymaster） | 统一抽象成 `Quality` 枚举，各源自己映射 |
| 主题 / 背景图 / 滚动文字 | — | — | TUI 里重做成调色板，背景图丢弃 |
| 窗口控制 / 托盘 | — | — | 丢弃 |

这张表就是你的需求文档初稿。**凡是不能同时落在两个平台上的功能，都要先想清楚它在混源语义下的定义**，否则一定长出一堆平台分支。

**（2）必须修掉的 v1 遗留 bug：音质选轨。**

实测 `playurl?fnval=4048` 返回的 `dash.audio` 是按码率升序的，v1 的 `getAudioURL` 取 `audio[0]`，拿到的是 44kbps 的 HE-AAC 轨。新项目要按 `bandwidth` 降序 + `codecs` 可解码性来选轨，并且把"音质偏好"做成真实配置项（B 站映射到 30216/30232/30280/flac，网易云映射到 standard/higher/exhigh/lossless/hires）。

---

## 2. 真正的工作量在哪

三个地方，按风险排序：

1. **音频层**：现在要面对更大的编码矩阵（见第 5 节）。v1 把这些外包给 WebKit，新项目得自己做。
2. **平台抽象与跨源匹配**：不是写两个 client，而是定义"一首歌"在系统里到底是谁。这层设计错了，后面所有功能都要返工。
3. **终端 UI**：量大但不难。musicfox 的 `internal/ui` 有 24,830 行，这个量级要接受。

反过来说，业务逻辑本身（登录、搜索、歌单、同步、歌词解析）你已经写过一遍，这部分是搬迁 + 适配，不是从零想。

---

## 3. 数据模型（这是整个项目最关键的设计）

v1 的 `Song` 把"平台标识"和"曲目身份"混在一张表里（`bvid`、`pageNumber`、`streamUrl` 全是 B 站字段）。混源要求拆成两层：**规范曲目（用户看到的"一首歌"）+ 平台绑定（这首歌在各平台的版本）**。

```sql
-- 平台账号（每源一条，凭证加密存）
CREATE TABLE source_accounts (
  source        TEXT PRIMARY KEY,        -- 'bilibili' | 'netease'
  user_id       TEXT, nickname TEXT, avatar TEXT,
  credential    BLOB,                    -- SESSDATA / MUSIC_U，走 OS keyring 或本地密钥加密
  expires_at    INTEGER,
  updated_at    INTEGER
);

-- 规范曲目：用户视角的一首歌，与平台无关
CREATE TABLE tracks (
  id          TEXT PRIMARY KEY,
  title       TEXT NOT NULL,
  artist      TEXT,
  album       TEXT,
  duration_ms INTEGER,
  cover_url   TEXT, cover_local TEXT,
  norm_key    TEXT NOT NULL,             -- 归一化后的匹配键
  created_at  INTEGER, updated_at INTEGER
);
CREATE INDEX idx_tracks_norm ON tracks(norm_key);

-- 平台绑定：一个 track 可以挂 0..N 个平台版本
CREATE TABLE track_sources (
  id          TEXT PRIMARY KEY,
  track_id    TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  source      TEXT NOT NULL,
  external_id TEXT NOT NULL,             -- BV 号 / 网易云 song id
  page        INTEGER DEFAULT 0,         -- 多 P 分集
  quality     TEXT,                      -- 该平台实际可得的最高音质
  available   INTEGER DEFAULT 1,         -- 最近一次解析是否可播
  preferred   INTEGER DEFAULT 0,         -- 用户指定的优先平台
  extra       TEXT,                      -- 平台特有字段（pageTitle / videoTitle / 歌手数组…）
  checked_at  INTEGER
);
CREATE UNIQUE INDEX idx_track_sources ON track_sources(source, external_id, page);

-- 本地歌单：混源，引用 track_id
CREATE TABLE playlists (
  id TEXT PRIMARY KEY, title TEXT, kind TEXT, created_at INTEGER
);
CREATE TABLE playlist_items (
  id TEXT PRIMARY KEY,
  playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
  position INTEGER NOT NULL,
  track_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  added_at INTEGER
);

-- 远端歌单 / 收藏夹绑定与同步状态
CREATE TABLE remote_playlists (
  id TEXT PRIMARY KEY,
  source TEXT NOT NULL, external_id TEXT NOT NULL, title TEXT,
  mode TEXT NOT NULL,                    -- 'read' | 'two_way'
  last_sync_at INTEGER, cursor TEXT
);

-- 播放状态（队列 + 进度），单行表
CREATE TABLE player_state (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  queue TEXT, current_index INTEGER, position_ms INTEGER, source_pref TEXT
);
```

三个要点：

1. **不要把播放直链写进库**。v1 的 `Song.streamUrl` 会过期，是"缓存即负担"。新模型只存平台标识，播放时解析 `Stream`（含过期时间），过期前主动续签。
2. **`norm_key` 是跨源匹配的基础**：`normalize(title) + normalize(artist) + 时长分桶`。归一化要处理全半角、大小写、括号内容（`(Live)`、`(feat. xxx)`、`(Remastered 2011)`）、多余标点空格。
3. **`available` 要与播放联动**：解析失败就落库，队列里标灰，可配置"自动跳过"。

### 跨源匹配策略

```
评分 = 0.5 × titleSim + 0.3 × artistSim + 0.2 × durationScore(±5s 线性衰减)
≥ 0.85 自动建立绑定
0.6~0.85 进"待确认"列表，用户一键盘确认
< 0.6  不绑定
```

同时提供两个入口：`在另一平台找这首`（一键跨源搜索）、`手动关联`（搜到后绑定）。这个是纯逻辑，可单测，值得早写。

---

## 4. 平台接口（Source 抽象）

```go
type Capability uint32

const (
    CapSearch Capability = 1 << iota
    CapMultiPart            // 多 P 分集
    CapRemotePlaylistSync   // 收藏夹/歌单双向同步
    CapOfficialLyrics
    CapQualitySelect
)

type Quality int // QualityAuto / Standard / High / Lossless

type Codec int // AACLC / HEAAC / FLAC / MP3 / EC3

// Stream 是 Source 与音频引擎之间唯一的契约
type Stream struct {
    URL       string
    Headers   map[string]string // Referer / User-Agent / Cookie
    Codec     Codec
    Duration  time.Duration
    ExpiresAt time.Time
    Size      int64             // -1 未知（B 站不支持 HEAD）
}

type Ref struct { ExternalID string; Page int }

type Source interface {
    ID() string
    Caps() Capability
    Account(ctx context.Context) (*Account, error)
    Login(ctx context.Context, ui LoginUI) error   // 二维码/验证码由 UI 侧驱动
    Search(ctx context.Context, q string, limit int) ([]Candidate, error)
    Resolve(ctx context.Context, ref Ref, q Quality) (*Stream, error)
    Lyrics(ctx context.Context, ref Ref) ([]LyricDocument, error)
    RemotePlaylists(ctx context.Context) ([]RemotePlaylist, error)
    SyncRemote(ctx context.Context, id string) (SyncReport, error)
}
```

设计要点：

- **`Stream` 带 Headers 和 Codec 是刻意的**。B 站要 `Referer`，网易云要 cookie，音频引擎不该知道这些差异；Codec 决定用哪个解码后端。
- **`Caps()` 驱动 UI**，而不是让 UI 判断平台名。多 P 面板、音质选择、同步按钮都按能力显隐。
- **登录流程由 UI 驱动**（`LoginUI` 回调），因为二维码渲染在 TUI 里是 UI 的事，Source 只负责请求与轮询。这点很重要：v1 的登录把轮询写死在 service 里，TUI 里要重构成可观察的进度流。
- **加第三个平台（QQ 音乐 / YouTube Music）时只需实现这个接口**，前提是上面三条守住了。

---

## 5. 音频层：双平台后的真实编码矩阵

| 来源 | 编码 | 容器 | 纯 Go 可解 | 备注 |
| --- | --- | --- | --- | --- |
| 网易云 standard/higher/exhigh | MP3 | MP3 | ✅ `hajimehoshi/go-mp3` | 最省事 |
| 网易云 lossless/hires | FLAC | FLAC | ✅ `mewkiz/flac` | |
| 网易云 jyeffect/sky/jymaster | 多为 FLAC/Hi-Res 变体 | 视情况 | ⚠️ 需实测 | 杜比/沉浸声可能落到 EC-3 |
| B 站 30280 / 30232 | AAC-LC（`mp4a.40.2`） | fMP4 | ⚠️ `go-aac`（新库） | 192k / 132k，主力轨 |
| B 站 30216 | **HE-AAC**（`mp4a.40.5`） | fMP4 | ❌ | 必须外部解码器或避开 |
| B 站 大会员 | FLAC / EC-3 | fMP4 | FLAC ✅ / EC-3 ❌ | 需登录 + 会员 |

结论：**不要选"纯 Go"或"外部播放器"其中一条，做解码器抽象**。

```go
type Decoder interface {
    Supports(Codec) bool
    Open(ctx context.Context, s Stream) (pcm io.ReadCloser, format Format, err error)
}
// native:  mp3 / flac / aac-lc  → 纯 Go，覆盖绝大多数曲目
// mpv:     任何格式 → IPC 兜底，只在 native 不支持或解码失败时启用
```

这样 90% 的曲目走纯 Go（快、无依赖、可做淡入淡出），剩下 10% 走 mpv。选轨时优先挑可解的编码（B 站直接选 30280，天然避开 HE-AAC），双保险。

其余播放语义（与平台无关，但都要新写）：

- 预解析下一首：当前曲目播放到 70% 时解析下一首的 `Stream`，避免切歌等待。
- URL 续签：B 站签名按 2 小时兜底、网易云通常更短，过期前 60 秒后台重解析并无缝替换。
- 回退链：`preferred source` → 其他绑定 → 跳过并提示。失败要落 `available=0`。
- 音量补偿：`gain = 10^(dB/20)`，**必须配软限幅**，否则 +6 dB 直接削波。

---

## 6. Go vs Rust

先看事实，不看偏好：

| 维度 | Go | Rust |
| --- | --- | --- |
| 领域逻辑复用 | v1 的 8,189 行 + 2,668 行测试直接搬 | 0，全部重写 |
| 网易云客户端 | [`go-musicfox/netease-music`](https://github.com/go-musicfox/netease-music)：203 文件 / 160+ API / weapi+eapi 加密 / 二维码+手机号+邮箱登录 / `song_url_v1` 8 档音质 / 含 UNM | 需自己移植加密与端点（约 1.5–2k 行，且要持续跟接口变化） |
| B 站客户端 | v1 现成 | 重写（约 0.8–1.2k 行，不难） |
| AAC 解码 | `go-aac`（纯 Go，AAC-LC，2026-07 建仓，较新） | `symphonia-codec-aac`：AAC-LC 状态 *Great*，但 **HE-AAC / HE-AACv2 未实现** |
| 容器解封装 | 需 `go-m4a` / `mp4ff` 或自写 fMP4 | `symphonia-format-isomp4`（*Great*，fMP4 仍要实测） |
| 音频输出 | `oto/v3`：Linux/macOS/Windows **免 cgo** | `cpal` / `rodio`：成熟 |
| TUI | `bubbletea/v2`：Elm 架构 + list/textinput/viewport/help/paginator 全家桶 | `ratatui` + `crossterm`：**立即模式**渲染，控件要自己拼（`tui-textarea`/`tui-input` 补） |
| 并发模型 | goroutine + channel，天然贴合"解码线程 → UI 事件" | tokio + 借用检查，多任务共享播放器状态常要 `Arc<Mutex<_>>` |
| 分发 | 单文件，免 cgo 交叉编译 | 单文件，静态链接更彻底 |
| 学习/迁移成本 | 低 | 高（若 Rust 不是日常语言，+50% 时间） |

**关键事实：Rust 的音频优势没有想象中大。** symphonia 的 AAC 支持同样是"只有 AAC-LC，HE-AAC 没做"，和 Go 的 `go-aac` 面对同样的缺口，最终两边都要给 HE-AAC/EC-3 留外部解码兜底。真正成熟的差异只在"解码器实现质量"，而它被 mpv 兜底方案抹平了。

**建议：Go。** 除非满足下面任一条，否则不值得换：

- Rust 本身就是你的长期主力语言，你打算后续 GUI 版也一起用 Rust 重写；
- 你把"纯 Rust、零外部依赖、单二进制"当成项目目标本身；
- 你愿意把工期乘以 1.4–1.6。

---

## 7. 仓库与迁移

新仓库，模块名独立（如 `github.com/Sheyiyuan/half-beat-v2`），v1 冻结只修 bug。

```
half-beat-v2/
├── cmd/
│   └── zero/                 # 入口与子命令：tui / play <bvid|url> / search / login / daemon
├── internal/
│   ├── domain/              # Track / TrackSource / Playlist / Account / Quality
│   ├── store/               # SQLite（GORM 或 sqlc），迁移 + v1 导入器
│   ├── source/
│   │   ├── source.go        # 接口 + 注册表
│   │   ├── bilibili/        # 从 v1 的 services 搬迁
│   │   └── netease/         # 基于 netease-music 封装
│   ├── match/               # 跨源匹配（纯函数，可单测）
│   ├── resolve/             # 播放解析：选源、回退、预取、续签
│   ├── audio/               # engine + decoder(native/mpv) + dsp
│   ├── lyrics/              # 搬迁 v1 parser/matcher + 三源搜索
│   ├── tui/                 # app / pages / components / theme
│   └── config/              # TOML
└── docs/
```

搬迁顺序（每一步都能跑起来）：

1. 搬 `models`/`db`/`lyrics`/`netguard` + 测试 → 改名成 `domain`/`store`/`lyrics`。
2. 写 v1 数据导入器：`Song` → `Track` + `TrackSource(bilibili)`，`Favorite` → `Playlist` + `RemotePlaylist`。**老用户数据不能丢**，这是重写最容易得罪人的地方。
3. 把 v1 的 `services` 按 Source 接口切成 `source/bilibili`（登录、搜索、BV、多 P、播放、收藏夹）。
4. 音频引擎 + `zero play <bvid>` headless 子命令先跑通（无 UI 干扰，最容易调）。
5. TUI 骨架 + 页面。
6. 加 `source/netease`。
7. 匹配、回退、同步、下载、系统集成。

---

## 8. 里程碑与工时

按单人全职 + AI 辅助估（人日）：

| 阶段 | 内容 | 人日 |
| --- | --- | ---: |
| P0 地基 | 新仓库、领域模型、SQLite 迁移、v1 数据导入器、配置、日志、CI | 12–16 |
| P1 双源接入 | B 站 source 搬迁（含选轨修复）+ 网易云 source + 双登录 + 统一搜索合并 | 25–35 |
| P2 音频与播放语义 | 解码器抽象（native + mpv）、引擎、混源队列、回退、预取、续签、DSP | 25–35 |
| P3 TUI 应用层 | 框架（布局/键位/命令面板/主题）、全部页面（收藏夹/歌单/歌曲/队列/歌词/搜索/登录/设置/下载/任务/匹配确认） | 30–40 |
| P4 功能补齐 | 歌词统一层（官方 LRC / B 站字幕 / LRCLIB）、同步、下载缓存、MPRIS/通知、keyring 凭证、打包发布 | 25–35 |
| P5 稳定与打磨 | 长跑测试、终端兼容矩阵、文档、错误文案 | 12–18 |
| **合计** | | **129–179** |

分档看：

- **核心版**（P0 + B 站 source + 音频 + TUI 主干 + 歌词，接口按多源预留）：**60–75 人日**，单人 12–15 周，重度 AI 辅助 8–10 周。
- **完整双源版**（上面全量）：**130–175 人日**，单人 6–9 个月。
- 加封面（kitty/ANSI）、桌面歌词、i18n、多播放后端：再 +25–40 人日。

对照：v1 是 Go 8,189 行 + 前端 19,742 行；musicfox 是 46,856 行。你这个目标（双源 + TUI + 全功能）落在 **25k–35k 行**，也就是 musicfox 的 60% 左右，但因为你只做 TUI 不做 macOS 系统集成、桌面歌词、DLNA、多播放后端，这个体量是合理的。

---

## 9. 风险登记（在上一版基础上新增）

| # | 风险 | 概率 | 影响 | 缓解 |
| --- | --- | --- | --- | --- |
| M1 | 平台抽象做错，代码里长满 `if source == ...` | 高 | 高 | 先写能力矩阵；`Caps()` 驱动 UI；代码评审时禁止按平台名分支 |
| M2 | "保留全部功能"没有分层，前期铺太宽 | 高 | 高 | 核心版先落地；同步/下载/主题/i18n 明确排到 P4 之后 |
| M3 | 网易云风控/接口变更（weapi 参数、加密版本） | 中 | 高 | 用成熟库而不是自己实现加密；请求限速 + 结果缓存；错误分类可降级 |
| M4 | 网易云版权/会员限制：灰歌、试听 30 秒、高音质需会员 | 高 | 中 | 混源兜底正好解决；`available` 落库；UI 明示"会员曲目" |
| M5 | 双平台凭证泄露（v1 的 SESSDATA 是明文） | 中 | 高 | OS keyring（`zalando/go-keyring`）或本地密钥加密；日志脱敏 |
| M6 | 跨源匹配误绑，把翻唱当原版 | 中 | 中 | 阈值 + 人工确认；绑定可撤销；匹配结果标来源 |
| M7 | URL 过期导致播放中断 | 高 | 中 | 预取 + 过期前续签；失败自动重解析一次再报错 |
| M8 | 两个平台同时触发限流/封禁（尤其小号） | 中 | 高 | 串行 + 令牌桶；退避；错误提示区分"网络"与"风控" |
| M9 | 终端 UI 在大曲库（万首）下卡顿 | 中 | 中 | 虚拟列表（只渲染可视区）+ 分页查询，v1 已有分页经验 |
| M10 | 数据迁移失败丢歌单 | 中 | 高 | 导入器幂等 + 导入前自动备份 + `--dry-run` |

法务上提醒一句：双平台抓流 + 开源分发本身就是灰色地带，v1 的 AGPL 声明已经覆盖了免责，但**别把"跨源替换音源"做成默认开启的自动行为**，保持"用户登录自己的账号、只播自己能播的内容"这条边界，风险低得多。

---

## 10. 需要你拍板（附我的建议）

| # | 决策 | 建议 |
| --- | --- | --- |
| 1 | 一步到位双源，还是先 B 站单源但接口按多源设计？ | **后者**。4–6 周能有可用产品，NCM 作为第二个插件加，总工期不变，风险低一个量级 |
| 2 | Go 还是 Rust？ | **Go**。除非 Rust 是你的长期主力语言，或"纯 Rust 单二进制"本身是目标 |
| 3 | 新仓库怎么处理 v1？ | 新仓库搬迁领域层 + 测试，v1 冻结只修 bug，**写数据导入器** |
| 4 | "全部功能"如何分层？ | 核心版只做：双登录 + 混源歌单 + 播放 + 歌词 + 搜索；同步/下载/主题/i18n 排二期 |
| 5 | 音质策略？ | 统一 `Quality` 枚举；B 站选 30280（AAC-LC），避开 HE-AAC；网易云默认 exhigh，会员曲目提示 |
| 6 | 数据模型？ | **Track + TrackSource 两层**（第 3 节），不存播放直链 |

如果第 1、2 条按建议走，我建议的下一步是：**先做 P0 + B 站 source + `zero play` headless 播放**（约 2 周），把音频和选轨在真实曲库上验通，再决定 TUI 的页面顺序。需要的话我可以直接开始搭 P0 骨架。
