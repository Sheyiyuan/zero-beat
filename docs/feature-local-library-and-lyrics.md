# 本地音乐库、本地歌词与歌词绑定

- 状态：已实施（v0.7.0）
- 定稿日期：2026-11-10
- 目标版本：v0.7.0
- 关联：[ADR-0010](adr/0010-multi-source.md)、[ADR-0011](adr/0011-mixin-playlists.md)、[ADR-0013](adr/0013-clip-and-cloud-write.md)、[ADR-0016](adr/0016-git-sync.md)、[同步设计](sync-design.md)、[ARCHITECTURE.md](../ARCHITECTURE.md)

## 0. 摘要

**先看这五个点：**

1. 本地曲目复用 `songs` 表，身份来自音频 meta；路径只是索引。
2. 同步过滤是双向契约：只做导出过滤会在下一轮导入删掉本机数据。
3. 只有“portable 绑定 + 歌曲非 local”进同步；local 绑定和自动结果都不进。
4. offset 属于当前歌词来源，不属于歌曲；换源就换 offset。
5. 同步格式升到 `meta.v=2`，会切断旧客户端；多设备必须先升级。

这一版加三件互相咬合的事：

1. **本地音乐库**：手动添加一个文件夹，它就是一个只读歌单；不递归、不监听文件变化、不参与同步。
2. **本地歌词识别**：为本地曲目自动找同目录/`lyrics/` 下的 `.lrc`、内嵌歌词和翻译，失败再走 LRCLIB。
3. **歌词手动绑定**：允许把任意一首歌的歌词来源指定为其他平台（网易云/LRCLIB）或本地文件/本地曲目，优先级高于自动匹配；只有“用户指定 + 平台引用”的绑定参与同步，本地绑定不进同步。

三件事共用一个原则：**自动结果和设备相关数据都当派生数据，不落库不同步；只有用户显式选择、且跨设备可复现的引用才持久化并同步。**

## 1. 已定决策

| 编号 | 决策 |
| --- | --- |
| D1 | 本地库只支持手动添加文件夹，不递归扫描；一个文件夹对应一个只读歌单。 |
| D2 | 文件夹歌单 `kind=folder`，扫描结果物化到 `playlist_items`；扫描失败保留上次内容。 |
| D3 | 本地曲目 `source=local`，`remote_id` 由音频 meta 归一化后生成；无标题时回退内容指纹。 |
| D4 | 本地源是独立 `Source`，可被混合歌单引用；平台源新增 `CapAccount`，登录页只遍历有该能力的源。 |
| D5 | 扫描时路径命中 `downloads.path` 的文件复用平台 `song_id`，不再生成一份本地曲目。 |
| D6 | 文件夹歌单、引用文件夹歌单的混合歌单、显式“仅本机”歌单都标记 `sync_excluded=1`。 |
| D7 | 同步导出不写 `sync_excluded` 实体，导入删除也必须跳过它们；单侧过滤一律视为 bug。 |
| D8 | 本地曲目只能加入 `sync_excluded=1` 的歌单；引用文件夹的混合歌单必须从创建时就是仅本机。 |
| D9 | 本地歌词顺序：同名 `.lrc` → `lyrics/` 子目录 → 归一化文件名 → 内嵌歌词 → LRCLIB。 |
| D10 | 纯文本歌词不做；绑定只接受带时间轴的歌词。 |
| D11 | 本地歌词候选默认自动选第一个，顺序由 `lyrics.toml` 配置；手动绑定永远优先于配置。 |
| D12 | 歌词解析顺序：用户绑定 > 自动（平台官方 / 本地旁挂+内嵌） > LRCLIB；任一级失败才降级。 |
| D13 | 绑定类型：`source`（平台引用）、`lrclib`、`local_file`、`local_song`；前两种在歌曲不是 local 源时可同步，后两种不同步。 |
| D14 | 自动匹配结果不落库；只有用户确认的绑定才写 `settings`。 |
| D15 | offset 属于当前歌词来源：portable/local 绑定各自带 `offset_ms`；无绑定用 `lyric_offset:<songID>`；新绑定从 0 开始。 |
| D16 | 绑定前必须成功取到带时间轴的歌词并预览；失败不落库。 |
| D17 | 同步格式升到 `meta.v=2`；新客户端可读 v1 并在首次成功同步时升级，升级后旧客户端拒绝同步。 |
| D18 | `lyrics.toml` 只在本机生效，v1 不参与同步。 |

## 2. 概念与边界

### 2.1 术语

- **文件夹歌单**：用户手动添加的目录，只读；内容随磁盘变化，重新扫描后更新。
- **本地曲目**：`source=local` 的歌曲行，身份来自音频 meta，不代表某个平台。
- **本地源**：`source.Source` 的 `local` 实现，负责本地文件取流和本地歌词。
- **自动歌词**：不经过用户绑定，按来源类型即时解析出的歌词。
- **歌词绑定**：用户为某首歌显式指定的歌词来源。
- **portable 绑定**：指向平台歌曲或 LRCLIB 记录，跨设备可复现；仅在歌曲本身不是 local 源时参与同步。
- **local 绑定**：指向本机文件或本机曲目，设备相关，不参与同步。
- **仅本机**：`sync_excluded=1` 的歌单/数据，只在当前设备存在。

### 2.2 三张边界

| 边界 | 规则 |
| --- | --- |
| 歌曲身份 | 本地曲目用 `local:<meta-key>:1`；平台曲目保持 `source:<external-id>:<page>`。 |
| 歌词来源 | 绑定 > 自动来源 > LRCLIB；自动结果不落库。 |
| 同步 | 只同步“用户显式选择 + 可跨设备复现”的绑定与注释；本地路径、自动结果、本机状态都不同步。 |

## 3. 本地音乐库

### 3.1 添加与扫描

入口：

- TUI：命令面板/歌单右键“添加本地文件夹”，输入路径；支持 `~` 展开，校验必须是目录。
- CLI：`zero library add <path>`，便于脚本和排障。

扫描规则：

- **不递归**：只读当前目录下的普通文件；子目录不进入，扫描结果里带 `subdirs` 数量，UI 提示“发现 N 个子文件夹，未扫描”。`lyrics/` 不计入这个提示，它是歌词定点目录。
- 扩展名白名单，大小写不敏感：`.mp3`、`.flac`、`.m4a`、`.aac`、`.ogg`、`.opus`、`.wav`、`.ape`、`.wv`、`.aiff`、`.aif`、`.wma`。
- 跳过隐藏文件（`.` 开头）、符号链接、大小为 0 的文件、权限不足的文件；后者记 warning，不中断整次扫描。
- 扫描在 `tea.Cmd` 里执行，不阻塞 `Update`。
- 目录不存在/无权限：**不覆盖已有条目**，只更新 `scan_status`/`scan_error`，歌单显示“文件夹不可用”。
- 单个文件读取失败：跳过并记 warning；已存在条目保留为“缺失”，不删除歌曲行和注释。
- 扫描成功后在一个事务里替换 `playlist_items`，并更新 `library_files` 的 `present`/`size`/`mtime`/`content_hash`。
- 排序：disc number → track number → 文件名自然排序；缺失的字段按 0/空处理。
- 不做文件监听；打开歌单时若距上次扫描超过 60 秒，或用户按 `r`，才重扫。v1 的“打开时刷新”只做 mtime 级增量，不做全量重算。
- 打开文件夹歌单/按 `r` 走 `RescanLibrary`，不复用远端歌单的 `PlaylistSongs(force)` 刷新路径。

### 3.2 曲目身份

本地曲目的身份**从音频 meta 生成**，不使用路径作为主键：

```text
normalize(s) = NFKC → trim → casefold → 连续空白折叠

meta_key:
  若 title 为空  → "file:" + quick_hash
  否则          → "m:" + sha256(artist \n title \n album \n disc \n track)[:32]

quick_hash:
  文件 ≤ 128 KiB → sha256(全部内容)
  文件 > 128 KiB → sha256(size \n 前 64 KiB \n 后 64 KiB)
```

- `songs.source = "local"`，`songs.remote_id = meta_key`，`songs.page = 1`，`songs.id = local:<meta_key>:1`。
- identity **不包含 duration**：duration 经常缺失或取整，包含它会让同一文件的重编码副本分成两条。代价是标题/艺术家/专辑/碟号/轨号完全相同的不同录音会被合并；这是“从 meta 生成身份”的已知取舍，文档和扫描 warning 里写明。
- title/artist 缺失时，标题回退文件名 stem；artist/album 留空。
- 查找顺序（必须按这个顺序）：
  1. `library_files` 里同一 `playlist_id + path` 的旧行 → 复用 `song_id`；
  2. `library_files` 里相同 `content_hash`（可跨歌单）→ 复用 `song_id`；
  3. `songs` 里 `source=local AND remote_id=meta_key` → 复用；
  4. 新建。
- 路径优先保证了：用户改标签（path 不变）不会换 ID；文件移动/改名（hash 不变）不会换 ID；两者都变才算新歌。
- 同一 meta 的多份文件共享一个 `song_id`，`library_files` 多行；`playlist_items` 在同一歌单里只出现一次。这是去重特性，不是 bug。
- 扫描时若发现“同一歌单内 meta 相同但 content_hash 不同”，记一条 warning，提示可能是不同版本；v1 不自动拆分。

### 3.3 播放与时长

- `app.ResolveWithLocal` 对 `source=local` 走注册表里的本地源；不需要在 `app` 里写平台特判。
- 本地源 `Resolve`：
  - 按 `song_id` 找 `library_files` 里 `present=1` 的路径，逐个 `os.Stat`，取第一个有效的；
  - 返回 `source.Stream{URL: path, Codec: 扩展名, Duration: song.Duration, Quality: "本地文件"}`；
  - `Headers` 为空，`ExpiresAt` 为零；不经过 header 代理。
- 播放引擎已经把本地路径直接交给 ffmpeg（已下载歌曲就是这么播的），本地文件不需要新增解码路径。
- duration 优先用标签；标签缺失时首次播放/seek 用一次 `ffprobe` 取时长并回写 `songs.duration`，不在扫描阶段逐文件 spawn。
- 本地源不参与“本地优先”开关的判断：它本来就是本地；平台歌曲的 `downloads` 优先逻辑不变。
- 本地源 `Caps()` 不置 `CapSearch`、`CapRemotePlaylists`、`CapPlaylistWrite`、`CapAccount`；`/` 的“本地全部”搜索本来就会查到 `songs` 表里的本地曲目。
- 封面：v1 可选。内嵌/同目录 `cover.jpg` 可以抽到 `cache/covers`，`CoverPath` 对 local 分支直接返回缓存文件；`cover.Fetch` 只接受 http(s)，不能把本地路径塞进 `songs.cover` 走原逻辑。

### 3.4 下载映射

“下载视作已下载”落成一条明确规则：

- 扫描每个文件前先查 `downloads.path`（绝对路径，`filepath.Clean` 后比较，新增索引）；
- 命中且对应 `songs` 行存在 → 文件夹歌单里使用原来的平台 `song_id`，显示 `[B]`/`[云]`，不创建 local 曲目；
- 命中但歌曲行缺失 → 记 warning，回退 local 身份；
- 未命中 → 走本地身份。

结果：

- 同一文件不会出现“平台下载 + 本地曲目”两个身份；
- 下载目录可以直接作为一个文件夹歌单使用；
- `w`/`W` 对 `source=local` 禁用（本地已经在本地），下载页不新增 local 记录；
- 删除下载记录但保留文件时，下次扫描会把它变成 local 曲目，这是预期行为。

### 3.5 本地源接入

新增 `internal/source/local`：

- `ID() = "local"`，`Name() = "本地音乐库"`，`Mark() = "本"`，`Caps() = 0`。
- `LoggedIn() = true`，`User()` 返回“本机”；`Session` 为空，`SetSession`/`Logout` 空实现。
- `StartQRLogin`/`PollQRLogin` 返回“本地源不需要登录”；因为不置 `CapAccount`，登录页不会调用。
- `Search` 返回空（不置 `CapSearch`）；`RemotePlaylists` 返回空；`PlaylistTracks` 返回空。
- `Resolve` 按 3.3 实现；`Lyrics` 按第 4 节实现。
- 组合根 `cmd/zero/main.go` 的 `newSources` 把 `local.New(st)` 加进注册表；本地源需要 store，平台源不需要。

登录页与账号摘要：

- `source.Capability` 追加 `CapAccount`，B 站/网易云置位，local 不置位。
- `Registry` 增加 `AccountIDs()`，只返回有 `CapAccount` 的源。
- 登录源初始化、`switchLoginSource`、`accountSummary`、`m.users` 初始化全部改用 `AccountIDs()`；`app.Users` 也只遍历 `CapAccount` 源。`Registry.IDs()` 仍保留给需要全部源的场合。
- 这样 local 不会出现在登录页、二维码轮换或账号摘要里，但列表行的 `[本]` 标记正常。

### 3.6 歌单语义与操作

- 文件夹歌单只读：
  - 不能加歌、删歌、拖拽排序；
  - 可以改显示名、重新扫描、更换文件夹、移除整个歌单；
  - 移除只删本地记录和 `library_files`，不删音频文件；v1 保留 songs 行与注释，避免重加文件夹丢数据，孤儿清理不在本期。
- 删除被混合歌单引用的文件夹歌单：沿用 `IsReferenced`，提示先移除引用。
- 文件夹歌单 ID 用 `folder-<unixnano>`，与 `local-`/`mix-` 区分；`validatePlaylistID` 放开这个前缀，但快照导入仍拒绝 `kind=folder`。
- 文件夹歌单计数直接来自物化后的 `playlist_items`，`ListPlaylists` 不需要特判。
- “仅本机”普通歌单：
  - 新增创建入口（命令面板/新建菜单），`sync_excluded=1`；
  - 本地曲目只能加入这种歌单或文件夹歌单；
  - 新建混合歌单时，只要任一引用是 `sync_excluded=1`，新 mixin 自动 `sync_excluded=1`。
- 已同步的混合歌单**不允许**追加文件夹引用，也不允许转成“仅本机”；UI 引导“新建仅本机混合歌单”。原因见 6.4。

## 4. 本地歌词识别

### 4.1 识别链

对 `source=local` 的自动歌词，按顺序：

1. 音频同目录、同名 stem 的 `.lrc`（大小写不敏感；也试 `song.flac.lrc`）；
2. 同目录 `lyrics/`（或 `Lyrics/`）子目录下的同名 `.lrc`；
3. 归一化文件名匹配：去掉音轨号、扩展名、空白/标点差异，比较 `title`、`artist - title`、`title - artist`；
4. LRC 内 `[ti:]`/`[ar:]` 与歌曲 meta 匹配（候选打分）；
5. 内嵌歌词：ID3v2 USLT、FLAC Vorbis `LYRICS`/`UNSYNCEDLYRICS`、MP4 `©lyr`、OGG；
6. LRCLIB 兜底（用本地 meta 的 title/artist/duration，沿用 `lrclib.Lookup` 的时长匹配）；
7. 都没有：返回空，UI 显示“未找到歌词”。

`lyrics/` 子目录是**定点查找**，不是递归扫描；文件夹歌单本身仍然不递归。

### 4.2 匹配与自动选择

- 大小写不敏感，Unicode NFC/NFD 归一（macOS 文件名常见 NFD）。
- 多候选按 `lyrics.toml` 的 `candidate_order` 打分；默认：
  1. `stem`（同名 stem）
  2. `artist-title`
  3. `title-artist`
  4. `title`
  5. `tag`（LRC 内 `[ti:]`/`[ar:]`）
- 同分选第一个，但在歌词页标记“候选 N”，后续可加“选择歌词”入口。
- 不做模糊编辑距离；本地目录里绑错歌词比没有歌词更糟。
- 目录列表按“目录 + mtime”做短缓存，避免每首歌都 `ReadDir`。

### 4.3 解析与编码

本地 `.lrc` 用独立的解析入口（不直接改平台歌词解析，避免网易云 tlyric 行为回归）：

- 编码：UTF-8 BOM、UTF-16 BOM、UTF-8；失败按 GB18030 解码；再失败报“无法解码”。文件上限 2 MiB。
- CRLF/CR 归一，去 NUL。
- 标准 LRC：`[mm:ss.xx]`、`[mm:ss.xxx]`、`[mm:ss]`，一行多时间戳。
- `[offset:+/-N]`：解析时整体加到时间轴；正负方向写测试固定（本文定义为正值让歌词延后）。用户 `<`/`>` 的 offset 是另一层，不混。
- 增强 LRC 的 `<mm:ss.xx>` 词级标签：剥掉，保留行时间戳；词级高亮不在本期。
- 元数据标签 `[ti:]`/`[ar:]`/`[al:]`/`[by:]`：正文忽略，`ti`/`ar` 可用于候选匹配。
- 没有时间戳的纯文本：不进入歌词链；`.txt` 不识别。

### 4.4 翻译

两种都支持，按“都能用就合并”处理：

1. **同时间戳双语行**：同一时间戳出现两行、正文不同，第二行挂到第一行的 `Translation`；TUI 已有译文渲染。
2. **独立翻译文件**：`<stem>.trans.lrc`、`<stem>.zh.lrc`（同目录或 `lyrics/`）。按时间戳精确匹配；缺失的行用双语行补，冲突时 sidecar 优先。

`lyrics.toml` 的 `translation = "both" | "same-timestamp" | "sidecar"` 控制；默认 `both`。

### 4.5 配置

`~/.config/zero-beat/lyrics.toml`（新增 `config.LyricsPath()`），v1 不参与同步：

```toml
[local]
candidate_order = ["stem", "artist-title", "title-artist", "title", "tag"]
translation = "both" # same-timestamp | sidecar | both
max_size_kb = 2048
```

- 文件不存在 → 默认值；无效 → 默认值 + warning，不影响播放。
- 手动绑定永远优先于这个配置。
- 候选顺序只影响“自动选谁”，不影响识别链的前四步是否执行。

### 4.6 运行时

- `local.Source.Lyrics` 返回 `(lines, label, err)`；`app.Lyrics` 现有“源歌词 → 空了再 LRCLIB”的流程不用改。
- label：`本地 LRC` / `内嵌歌词` / `LRCLIB`；歌词页 footer 已显示来源和 offset。
- 歌词文件不参与文件夹扫描，按需读取；播放中改 `.lrc`，下次进歌词页/切歌才生效，v1 不做监听。
- 歌词正文不落库、不同步；只有“歌词来源绑定”和“歌词 offset”会进设置/同步。
- 自动本地歌词的 offset 存在 `lyric_offset:<songID>`（auto）；local 源歌曲的注释按 6.2 的规则不导出。若这首歌绑定了歌词，offset 属于绑定（5.3）。

## 5. 歌词手动绑定

### 5.1 绑定类型与存储

```go
type LyricBinding struct {
    Kind       string `json:"kind"` // source | lrclib | local_file | local_song
    Source     string `json:"source,omitempty"`
    ExternalID string `json:"external_id,omitempty"`
    Page       int    `json:"page,omitempty"`
    CID        int64  `json:"cid,omitempty"`
    LRCLIBID   int64  `json:"lrclib_id,omitempty"`
    Path       string `json:"path,omitempty"`
    SongID     string `json:"song_id,omitempty"`
    Title      string `json:"title,omitempty"`
    Artist     string `json:"artist,omitempty"`
    Duration   int    `json:"duration,omitempty"`
    OffsetMS   int    `json:"offset_ms,omitempty"`
    BoundAt    int64  `json:"bound_at,omitempty"`
}
```

| kind | 含义 | 存储 key | 同步 |
| --- | --- | --- | --- |
| `source` | 平台歌曲（v1 仅开放网易云；B 站字幕 donor 见第 12 节） | `lyric_bind:<songID>` | 是（歌曲非 local） |
| `lrclib` | LRCLIB 记录 | `lyric_bind:<songID>` | 是（歌曲非 local） |
| `local_file` | 本机 `.lrc` | `lyric_bind_local:<songID>` | 否 |
| `local_song` | 本机曲目（用它的旁挂/内嵌歌词） | `lyric_bind_local:<songID>` | 否 |

规则：

- 一首歌只有一个生效绑定；新绑定覆盖旧绑定。
- portable 与 local 分开两个 key，永不混写；本机绑定优先，UI 显示“本机绑定覆盖同步绑定”。
- `local_song` 只读取目标曲目的旁挂/内嵌歌词，**不跟随目标曲目自己的绑定**，避免循环。
- 绑定只存引用和展示信息，不存歌词正文；正文每次按引用重新取。
- `BoundAt` 用于展示/排障，不参与冲突决策。
- local 源歌曲上的绑定一律视为本机，见 6.2。

### 5.2 优先级与降级

`app.Lyrics` 的解析顺序：

1. 手动绑定（portable 或 local）→ 按绑定取歌词；
2. 自动来源：
   - 平台歌曲：平台官方歌词（B 站字幕 / 网易云）；
   - 本地歌曲：4.1 的本地链；
3. LRCLIB；
4. 都没有：空歌词 + 明确提示。

- 任一级取到带时间轴的歌词即停止。
- 绑定存在但取不到（源未登录、记录被删、文件丢失、没有歌词）：**降级到自动**，但 `LyricsResult` 必须带 `Degraded=true`/`Warning`，UI 显示“绑定失效，已回退”。
- 自动结果不写 settings；只有用户确认的绑定才落库。
- 自动匹配失败后再走 LRCLIB，保持现状。

### 5.3 offset 随源

offset 是“当前这版歌词”的属性，不是歌曲的属性：

| 当前实际来源 | offset 存哪 | 同步 |
| --- | --- | --- |
| portable 绑定 | 绑定 JSON 的 `offset_ms` | 跟着绑定同步 |
| local 绑定 | local 绑定 JSON 的 `offset_ms` | 不同步 |
| 自动（无绑定或降级到自动） | `lyric_offset:<songID>` | 按 6.3 |

- `LyricsResult` 增加 `OffsetKey`：`auto` / `portable` / `local`，表示**实际生效**的来源，而不是“是否存在绑定”。
- TUI `shiftLyrics` 保存时把 `OffsetKey` 传给 `SaveLyricOffset(songID, key, offset)`：
  - `portable` → 更新 `lyric_bind:` 的 `offset_ms`；
  - `local` → 更新 `lyric_bind_local:` 的 `offset_ms`；
  - `auto` → 更新 `lyric_offset:<songID>`。
- 绑定失败降级到自动时，`OffsetKey=auto`；此时调 offset 写 auto，**不写失效绑定**。绑定恢复后使用它自己的 offset。
- 新绑定从 0 开始；切回旧绑定恢复它自己的 offset；解绑回到 auto offset；`0` 只清零当前来源。
- `lyricMsg` 带 `offsetKey` 和请求 generation；绑定切换后丢弃旧响应，避免慢请求覆盖新结果。

### 5.4 绑定界面

新增动作 `bind_lyrics`，默认键 `B`（`b` 已是返回）。作用于正在播放的歌，任意页面可用。

浮层结构：

```text
┌ 歌词来源 · B站 · 【翻唱】晴天 ──────────────────────────┐
│ 当前：自动（B站字幕） · 偏移 +0.0s                       │
│ 搜索 周杰伦 晴天                                        │
│ ────────────────────────────────────────────────────── │
│ ● 网易云  晴天 · 周杰伦            4:29  [网易云]        │
│ ○ LRCLIB  晴天 · 周杰伦            4:29  [同步歌词]      │
│ ────────────────────────────────────────────────────── │
│ enter 绑定并预览 · tab 切源 · l 本地 · u 链接/ID · x 解除 │
└────────────────────────────────────────────────────────┘
```

- 搜索框预填 `lrclib.CleanTitle(song.Title)`，可编辑。
- `tab` 在 全部 / 网易云 / LRCLIB 之间切换；v1 donor 不列 B 站（B 站字幕 donor 需要 CID 解析，见第 12 节）。
- 候选显示标题、歌手、时长、来源；LRCLIB 预知是否有时间轴，只有 `plainLyrics` 的候选标“无时间轴”、不可选。网易云搜索不返回歌词内容，候选不预校验；选中后取一次，没歌词就提示并不落库。
- `enter`：先取歌词 → 校验有带时间轴的行 → 预览前 3 行 + 总行数 → 再确认落库。
- `l`：本地文件路径输入，或从 `source=local` 的曲目里选（`local_song`）。
- `u`：粘贴链接/ID。网易云链接走 `TargetParser`；纯数字按网易云 song id；`lrclib:<id>` 按 LRCLIB；B 站链接 v1 直接提示“暂不支持 B 站字幕 donor”；以 `/`、`~`、`.` 开头的按本地文件。
- `x`：解除当前绑定（portable 和 local 都清）；`esc`：取消。
- 绑定成功后回到歌词页，来源标签显示 `绑定 · 网易云歌词`；失败留在浮层显示错误。

### 5.5 搜索后端

- 网易云：复用 `Source.Search`，取第 1 页，转成 `LyricCandidate`；不要求先解析音频。
- LRCLIB：新增
  - `Search(ctx, q string) ([]Result, error)` → `GET /api/search?q=...`；
  - `Get(ctx, id int64) (Result, bool, error)` → `GET /api/get/{id}`；
  - `Result` 增加 `ID int64`；`apiItem` 增加 `id`。
- 候选合并：按来源顺序（网易云 → LRCLIB）和标题/歌手/时长接近度排序；不自动替用户选。
- B 站作为 **目标** 没问题；B 站作为 **donor** v1 不做。`bilibili.Lyrics` 在 `CID == 0` 时直接返回空，搜索候选又没有 CID，硬做会多一次 resolve，且字幕质量不稳定。

### 5.6 校验、清除、失败

- 绑定前必须取到至少一行带时间轴的歌词；纯文本/无时间轴一律拒绝。
- 绑定后每次播放重新取；源不可用、记录被删、本地文件丢失 → 降级自动 + 提示，不自动清除绑定。
- 用户手动解除绑定才删除 `lyric_bind:`/`lyric_bind_local:`。
- 绑定指向同一首歌（source/external_id/page 相同）视为解除绑定。
- 本地文件绑定提供“重新定位”；只换路径，不换绑定 ID。

## 6. 同步设计

### 6.1 歌单同步边界

现状：`BuildSnapshot` 只导出 `kind in (local, mixin)`（[snapshot.go](../internal/sync/snapshot.go)）；导入在 `KeepLocal=false` 时会把远端没有的 local/mixin 删掉。**所以“过滤”必须是双向契约：导出不写 + 导入不删。**

新增 `playlists.sync_excluded`：

- 文件夹歌单：`kind=folder` 且 `sync_excluded=1`；
- 引用任何 `sync_excluded=1` 歌单的 mixin：`sync_excluded=1`；
- 用户显式创建的“仅本机”普通歌单：`sync_excluded=1`；
- 现有普通 local/mixin 默认 0，行为不变。

实现要点：

- `ListPlaylistSnapshots(kinds...)` 加 `WHERE sync_excluded = 0`；导出和 `ImportSnapshot` 的删除遍历**共用这个查询**，一处过滤两侧生效。
- `BuildSnapshot` 加防御：遇到 `sync_excluded=0` 但引用 folder 的 mixin，直接报错，不静默导出。
- `BuildSnapshot` 再检查会同步的歌单里是否混入 `source=local` 曲目；有则直接报错（正常路径应被 store guard 拦住），不静默过滤——静默过滤会在导入时删掉本机条目。
- `ImportPlaylistSnapshot` 拒绝 `kind=folder`（仓库里不应该出现）。
- `EnsurePlaylist` 的 `ON CONFLICT` 不能重置 `sync_excluded`/`root_path`。
- `DeletePlaylistForSync` 只处理 local/mixin；`sync_excluded` 实体不会进入这条路径。
- 仓库里出现 `kind=folder` 的歌单或引用时，按无效处理：导入跳过并 warning，不创建没有 `root_path` 的 folder 占位。
- 同步状态/同步页显示“仅本机歌单 N / 本机注释 M”，避免用户以为换设备丢数据。

### 6.2 注释与本地曲目

- 新增 `ListSyncAnnotations`：读取 `clip:`/`lyric_offset:`/`song_comp:`/`lyric_bind:`，跳过 `source=local` 的歌曲，以及 ID 以 `local:` 开头的孤儿注释；其他未知歌曲的注释保持现状（不因本次改动被误删）。
- 因此 local 源歌曲上的 portable 绑定也不会导出；绑定是否跨设备必须同时满足“目标 portable”和“歌曲非 local”。
- `BuildSnapshot` 用 `ListSyncAnnotations`；`deleteMissingAnnotations` 也用它。这样本地曲目的注释不会进仓库，也不会因为“远端没有”被本机删掉。
- `SongAnnotation` 增加 `LyricBind *LyricBinding`（仅 portable）；`Active()` 把 `LyricBind != nil` 算进去。
- `SaveAnnotation` 写 `lyric_bind:<songID>`（`json.Marshal`）或删除；`DeleteAnnotation` 清 `lyric_bind:`，**不清** `lyric_bind_local:`。
- `AnnotationFile` 增加 `lyric_bind` 字段；`ListAnnotations` 读取并解析；无效 JSON 跳过并 warning。
- `lyric_bind_local:` 不进入上述任何查询；同步永远看不到本地绑定。

### 6.3 offset 同步

- portable 绑定：offset 在 `lyric_bind.offset_ms` 里，随注释同步（歌曲非 local 时）；`SongAnnotation.LyricOffsetMS` 可以同时存在，只服务自动降级。
- local 绑定：offset 在 `lyric_bind_local.offset_ms` 里，不同步。
- 无绑定：`lyric_offset:<songID>` 按现有规则同步；local 源歌曲的注释按 6.2 跳过。
- 平台歌曲绑定了本地 LRC 时，`lyric_offset` 仍可能同步，但它只服务自动降级，不会被当作本地绑定的 offset；两边已经隔离，不需要额外“抑制 offset”逻辑。
- 绑定是否跨设备 = 目标 portable **且** 歌曲不是 local 源。local 源歌曲上的 portable 绑定也按本机处理，因为本地曲目本身不在同步仓库里。
- portable/auto offset 保存后照常 `bumpLibraryRevision`；local offset 保存不触发同步。

### 6.4 为什么不能给已同步的 mixin 加 folder 引用

- 如果允许，mixin 必须从“同步”变成“仅本机”。但仓库里已有的 `playlists/<id>.json` 不会因为本地 `sync_excluded=1` 自动消失；下一轮 git 合并/导入可能把它又导回来，或者把其他设备上的副本删掉。
- 要做“同步转仅本机”，需要显式的仓库删除 + 跨设备删除传播（其他设备是删副本还是归档），这是独立功能，v1 不做。
- 因此：**folder 引用只能出现在从创建时就是 `sync_excluded=1` 的 mixin 里**；已同步 mixin 想引用 folder，UI 引导“新建仅本机混合歌单”。

### 6.5 同步格式 v2

新增 `sync_excluded` 与 `lyric_bind` 后，旧客户端无法安全参与同一仓库：

- 旧客户端不认识 `sync_excluded`，可能把仅本机歌单重新导出，或在导入时删掉它们；
- 旧客户端 strict JSON 解码会跳过带 `lyric_bind` 的注释，重新导出时可能丢字段。

因此：

- `MetaVersion` 从 1 升到 2；`meta.json` 不新增字段，保证旧客户端读到的是“版本不匹配”而不是“JSON 损坏”。
- 新客户端 `DecodeMeta` 接受 v1/v2；v2 正常同步。
- 首次在 v1 仓库上成功同步后，写回 v2 `meta.json`，并在同步状态里提示“仓库已升级到 v2，旧版本客户端将无法同步”。
- 旧客户端读到 v2 直接报 `ErrSchema`，要求升级；不允许降级写回 v1。
- 多设备用户必须先升级所有设备，再让任意一台执行同步。

这是有意的兼容性切断：同步边界涉及数据删除，不能靠“旧客户端忽略未知字段”赌安全。

## 7. 数据模型与迁移

### 7.1 表结构

```sql
-- playlists 新增列（migrate 里用 hasColumn 判断后 ALTER TABLE）
ALTER TABLE playlists ADD COLUMN sync_excluded INTEGER NOT NULL DEFAULT 0;
ALTER TABLE playlists ADD COLUMN root_path     TEXT    NOT NULL DEFAULT '';
ALTER TABLE playlists ADD COLUMN scan_status   TEXT    NOT NULL DEFAULT '';
ALTER TABLE playlists ADD COLUMN scan_error    TEXT    NOT NULL DEFAULT '';
ALTER TABLE playlists ADD COLUMN scanned_at    INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS library_files (
    playlist_id  TEXT    NOT NULL,
    path         TEXT    NOT NULL,
    song_id      TEXT    NOT NULL,
    size         INTEGER NOT NULL DEFAULT 0,
    mtime        INTEGER NOT NULL DEFAULT 0,
    content_hash TEXT    NOT NULL DEFAULT '',
    present      INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (playlist_id, path)
);
CREATE INDEX IF NOT EXISTS idx_library_files_song ON library_files(song_id);
CREATE INDEX IF NOT EXISTS idx_library_files_hash ON library_files(content_hash);
CREATE INDEX IF NOT EXISTS idx_downloads_path      ON downloads(path);
```

- `scan_status`：`ok` / `missing` / `error`；`scan_error` 存最近一次错误文案。
- `root_path` 为绝对路径；非 folder 歌单为空。
- `library_files` 是路径索引；`playlist_items` 是展示/播放用的物化结果。两者在同一事务里更新。
- `songs` 不加列；本地曲目复用现有字段。

### 7.2 settings key

| key | 内容 | 同步 |
| --- | --- | --- |
| `lyric_offset:<songID>` | 自动歌词 offset（毫秒） | 平台歌曲是；local 源否 |
| `lyric_bind:<songID>` | portable `LyricBinding` JSON | 是（随注释） |
| `lyric_bind_local:<songID>` | local `LyricBinding` JSON | 否 |

### 7.3 注释文件

```json
{
  "song_id": "bilibili:BV1xx411c7mD:1",
  "lyric_offset_ms": 500,
  "lyric_bind": {
    "kind": "source",
    "source": "netease",
    "external_id": "123456",
    "page": 1,
    "title": "晴天",
    "artist": "周杰伦",
    "duration": 269,
    "offset_ms": 300,
    "bound_at": 1760000000
  }
}
```

- `lyric_bind` 只出现 portable；local 不写。
- 字段顺序由 struct 固定，导出确定性不变。
- 旧注释没有 `lyric_bind`，导入按“无绑定”处理。

### 7.4 迁移顺序

1. `schema` 里加 `library_files` 表/索引；`migrate()` 用 `hasColumn` 加 `playlists` 新列。
2. 现有 playlists 的 `sync_excluded=0`、`root_path=''`，行为不变。
3. `MetaVersion` 升 2；v1 仓库升级逻辑放在 sync client，不在 store。
4. 旧库不需要数据回填；本地库是全新数据。

## 8. 代码改动清单

| 位置 | 改动 |
| --- | --- |
| `internal/store` | 新列/新表/索引；`Playlist` 字段；`CreateFolderPlaylist`/`CreatePlaylistEx`、`LocalSongPath`、`DownloadByPath`、`LibraryFiles`、`ReplaceLibraryItems`；`sync_excluded` 过滤；`SetLyricOffset(songID, key, offset)`；`LyricBinding` 读写；`ListSyncAnnotations`；`Active/Save/Delete` 改造；`DeletePlaylist`/`RenamePlaylist` 放开 folder；`AddSongToPlaylist`/`RemoveSongFromPlaylist` 拒绝 folder；`MergePlaylist` 目标为 folder 时拒绝（源可以是 folder）；`AddMixinRef`/`SetMixinRefs` 的同步 guard。 |
| `internal/library` | 新包：目录扫描、扩展名过滤、meta 读取、identity/quick hash、排序、扫描结果。 |
| 依赖 | 标签读取引入纯 Go tag 库（候选 `github.com/dhowden/tag`），确认许可证后补 `THIRD_PARTY_NOTICES.md`；GB18030 使用已在依赖里的 `golang.org/x/text`，提升为直接依赖。 |
| `internal/source/local` | 新包：`Source` 实现、`Resolve`、`Lyrics`、`Mark`。 |
| `internal/source` | `CapAccount`；`Registry.AccountIDs()`；`source.Local` 常量。 |
| `internal/source/bilibili`、`internal/netease` | `Caps()` 加 `CapAccount`。 |
| `internal/app` | `Add/Remove/Relocate/RescanLibrary`；`Lyrics` resolver；`SearchLyricCandidates`、`BindLyrics`、`ClearLyricBinding`、`PreviewLyricBinding`；`SaveLyricOffset` 带 `OffsetKey`。 |
| `internal/lyrics` | 本地 LRC 解析入口、编码、offset、双语、增强标签剥离。 |
| `internal/lrclib` | `ID`、`Search`、`Get`。 |
| `internal/sync` | `MetaVersion=2`、v1→v2 升级；`PlaylistFile` 不需要新字段；`AnnotationFile.LyricBind`；`ListPlaylistSnapshots` 过滤；`ListSyncAnnotations`；导入删除过滤。 |
| `internal/config` | `LyricsPath()`。 |
| `internal/tui` | 侧栏 folder/仅本机徽标；添加文件夹/重扫/移除；只读守卫；歌词来源浮层；`lyricOffsetKey`；帮助/演示页；帧测试。 |
| `internal/keymap` | `bind_lyrics` 默认 `B`。 |
| `cmd/zero` | `library`/`lyrics` 子命令；`newSources` 注册 local；usage/文档。 |
| `docs` | README 文档地图、guide/library、guide/playback、reference/cli、reference/keys、ARCHITECTURE 数据模型、THIRD_PARTY_NOTICES（标签库）。 |

## 9. 测试计划

### 9.1 store

- 迁移：旧库加列/建表可重复执行；现有 playlists 默认 `sync_excluded=0`。
- 文件夹 CRUD：创建、改名、删除、被引用时拒绝删除。
- `library_files`：多路径映射同一 song、present 切换、删除歌单清理。
- `downloads.path` 索引与 `DownloadByPath`。
- 本地曲目 guard：加入 synced 歌单被拒；加入 `sync_excluded=1` 允许。
- mixin：folder 引用 → 新 mixin `sync_excluded=1`；已同步 mixin 加 folder 引用被拒。
- annotation：只用 binding 的注释 `Active()=true`；`Save/Delete` 不碰 local key。
- `ListSyncAnnotations` 跳过 local 歌曲，保留非 local 的孤儿注释。

### 9.2 扫描与身份

- 非递归：子目录不进入；`subdirs` 计数正确。
- 扩展名大小写、隐藏文件、符号链接、0 字节、权限错误。
- 无标签/部分标签/全标签；文件名回退。
- 同一 meta 两个文件 → 一个 song、两个 path；content hash 不同 → warning。
- 改名（path 变、hash 不变）→ ID 不变；改标签（path 不变）→ ID 不变；两者都变 → 新 ID。
- 删文件 → `present=0`、条目消失、注释保留；文件回来 → 恢复。
- 根目录不存在 → 不清空条目、`scan_status=missing`。
- 下载路径命中 → 平台 song_id；下载记录删除 → 下次扫描变 local。

### 9.3 本地歌词

- 编码：UTF-8/BOM/UTF-16/GB18030；CRLF/NUL；超大文件拒绝。
- 解析：多时间戳、`[offset:]`、增强标签、元数据标签、空行、坏时间戳。
- 双语：同时间戳合并；sidecar 翻译覆盖/补齐；两者同时存在。
- 匹配：同名、`lyrics/`、`artist - title`、`title - artist`、tag 匹配、多候选、无候选。
- 内嵌：ID3/FLAC/MP4/OGG 各一个 fixture；不支持格式回退。
- LRCLIB 兜底：有本地歌词时不发请求；空时发请求。

### 9.4 绑定与 offset

- portable/local 存储 key 正确；互不覆盖；本机优先。
- 优先级：绑定 > 平台官方 > 本地旁挂/内嵌 > LRCLIB。
- 绑定失败降级：`Degraded=true`、label 正确；offset 写 auto 而不是失效绑定。
- 切换绑定：新绑定 offset=0；切回恢复；解绑回 auto；`0` 只清当前。
- 校验：无时间轴/空歌词拒绝落库；预览行数正确。
- LRCLIB `Search`/`Get`：httptest；无 synced 的候选不可绑。
- 本地文件绑定：路径 `~` 展开、缺失回退、重新定位。
- 请求 generation：旧歌词响应不覆盖新绑定。

### 9.5 同步

- 导出：folder、`sync_excluded` mixin、local 注释、local binding 都不在仓库。
- 导入 `KeepLocal=false`：不删除 folder/`sync_excluded` mixin/local 注释/local binding；普通 local/mixin 仍按现状删除。
- portable binding + offset 往返；local binding 不往返；auto offset 保持现状。
- 平台歌曲上的 portable 绑定导出；local 源歌曲上的 portable 绑定不导出；平台歌曲上的 local 绑定不导出。
- `AnnotationFile` 无 `lyric_bind` 的旧文件可导入。
- v1→v2 升级：成功同步后 meta.v=2；旧客户端模拟读到 v2 报错；v2 不降级。
- 冲突：同一注释两端改 binding/offset 整版冲突；选择后行为正确。

### 9.6 TUI / CLI / 手动

- 帧测试：60×18 / 100×30 下 folder 列表、绑定浮层、预览不超宽高。
- 键位：`B` 打开/关闭、`tab` 切源、`enter` 绑定、`l`/`u`/`x` 生效、`esc` 不改库。
- folder 只读：`a`/`d`/拖拽被拒并有提示。
- 同步状态显示“仅本机 N”。
- CLI：`library add/list/scan/remove`、`lyrics show/bind/unbind` 的退出码与输出。
- 手动：真实 MP3/FLAC（标签/内嵌歌词/GBK LRC）、B 站歌曲绑定网易云、两台设备同步。

## 10. 实施顺序与估时

| 阶段 | 内容 | 估时 |
| --- | --- | --- |
| P1 | schema/迁移、`sync_excluded`、`LibraryFiles`、注释过滤、meta v2 | 2.5–3.5 人日 |
| P2 | 扫描/身份/本地源/Resolve/下载映射/CLI add+scan | 3–4 人日 |
| P3 | TUI 本地库：侧栏、添加/重扫/移除、只读、徽标 | 2–3 人日 |
| P4 | 本地歌词：解析/编码/匹配/内嵌/翻译/lyrics.toml | 3–4 人日 |
| P5 | 歌词绑定：数据/resolver/offset/LRCLIB/浮层/同步 | 5–7 人日 |
| P6 | 联调、测试、文档、演示页 | 2 人日 |

合计约 **17–23 人日**；P1–P3 可并行一部分，紧凑排期约 15–19 人日。

可独立上线的最小版本：

- **本地库 + 自动本地歌词**（P1–P4）：10–14 人日；
- 歌词绑定（P5）：再 +5–7 人日。

## 11. 风险与替代方案

### 11.1 风险

| 风险 | 缓解 |
| --- | --- |
| 同步过滤只做一侧，导入删数据 | 导出/删除共用 `ListPlaylistSnapshots`；专项测试 `KeepLocal=false`；meta v2 切断旧客户端。 |
| meta 身份碰撞，把不同录音合并 | 文档写明取舍；扫描 warning；后续可加“拆分曲目”。 |
| 标签库格式/编码覆盖不足 | 文件名/sidecar 兜底；编码先 UTF-8 再 GB18030；fixture 覆盖常见格式。 |
| 大目录扫描慢 | 不递归、增量 mtime、`tea.Cmd`、不做监听；必要时后续加进度。 |
| offset/来源切换竞态 | `OffsetKey` + 请求 generation；旧响应丢弃。 |
| 旧客户端/旧仓库不兼容 | meta v2；升级提示；拒绝降级写回。 |
| B 站字幕 donor 的 CID/质量 | v1 不做；后续加 `TrackInfo` 或绑定时 resolve。 |
| 本地封面/MPRIS 复杂度 | 标为可选，不阻塞主链路；`CoverPath` 单独分支。 |
| 版权 | 只存引用，不存歌词正文；本地 `.lrc` 由用户提供。 |

### 11.2 替代方案

| 方案 | 不采用的原因 |
| --- | --- |
| 本地曲目单独建表 | 队列、注释、下载、混合歌单全按 `song_id` 引用，读路径要全改。 |
| 递归扫描/自动发现 | 层级、空目录、重名、移动目录语义复杂；v1 先做手动单层。 |
| 本地库参与同步 | 路径/文件设备相关，会泄漏路径且无法复现；只同步元数据也不值得。 |
| 保存歌词正文 | 体积、版权、平台更新；只存引用，按需重取。 |
| 每首歌一个全局 offset | 换歌词来源后偏移错位；offset 属于来源。 |
| 导出时按条目过滤本地曲目 | 导入是整版覆盖，过滤会在本机删数据；必须整歌单取舍。 |
| 把 local 做成完整 Source（含登录/远端歌单） | 登录页/二维码/远端歌单会被污染；只保留 Resolve/Lyrics，用 `CapAccount` 隔离。 |
| 落库自动匹配结果 | 设备/网络相关，会以“用户指定”身份进入同步。 |

## 12. 不在本期

- 递归扫描、自动发现子目录、按专辑/艺术家自动分组。
- 文件监听（inotify/fsnotify），自动刷新。
- 把已同步歌单转换为“仅本机”（需要仓库删除与跨设备传播）。
- B 站字幕作为 donor（需要 CID/TrackInfo；`bilibili.Lyrics` 在 CID=0 时返回空）。
- 每个自动来源各自记 offset（平台官方 vs LRCLIB 目前共用一个 auto offset）。
- 纯文本歌词、原始 LRC 文本粘贴绑定。
- CUE/APE 单文件多轨拆分。
- 本地封面/MPRIS 完整支持（v1 可选）。
- `zero play <本地路径>` 直接播放（走 TUI 文件夹歌单即可）。
- Windows 专门适配。

## 13. 验收标准

- [ ] 手动添加文件夹后出现只读歌单，只收录当前层音频；有子目录时提示未扫描。
- [ ] 本地文件可播放、可 seek、进度/时长正确；本地优先开关不影响本地源。
- [ ] 改名/移动文件不换 `song_id`；改标签不换 ID；删除文件条目不残留、注释不丢。
- [ ] 下载目录中的文件以平台歌曲出现，不产生重复 local 身份。
- [ ] 文件夹歌单、仅本机 mixin、本地曲目注释、本地绑定都不出现在同步仓库。
- [ ] `KeepLocal=false` 导入不会删除上述本机数据；普通 local/mixin 行为不变。
- [ ] 本地 LRC：同名/`lyrics/`/归一化匹配、内嵌、GB18030、`[offset:]`、双语、`.trans.lrc` 全部可用。
- [ ] 纯文本/无时间轴歌词不被使用；自动候选按 `lyrics.toml` 顺序选择。
- [ ] 绑定优先级正确；绑定失败降级并有可见提示。
- [ ] offset 随当前来源：切换/解绑/归零行为符合 D15；本地绑定 offset 不同步。
- [ ] portable 绑定在平台歌曲上跨设备可用；local 绑定、以及 local 源歌曲上的绑定都不跨设备。
- [ ] 现有同步、队列、播放、TUI 帧测试全绿；旧库/旧同步仓库升级路径有测试覆盖。

## 14. ADR 拆分建议

本文是实施级设计。定稿后建议拆出三份 ADR，避免以后重新讨论：

- **ADR-0018：本地音乐库**——本地源 + 文件夹歌单 + 下载映射 + 同步边界。
- **ADR-0019：歌词来源绑定**——用户指定优先、offset 随源、只同步 portable 绑定。
- **ADR-0020：同步格式 v2**——`sync_excluded`、注释新字段、旧客户端切断。
