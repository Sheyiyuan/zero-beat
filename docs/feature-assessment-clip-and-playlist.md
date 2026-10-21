# 片段播放与歌单混入：现状评估与落地方案

> 评估日期：2026-10-21
> 评估对象：zero-beat v0.3 工作区
> 对照实现：Half Beat Player 的「播放区间（只播放此段）」

> **落地状态（2026-10-21）**：本文是评估稿，已按确认后的口径实现。实际口径为：裁剪只做「播放区间」，不导出文件；歌单混入一律使用「引用」，目标必须是混合歌单；加歌目标支持本地、混合和同源云端歌单。文中「本地歌单复制」「远端歌单只读」等是当时的备选方案，未采用；以代码和 README 为准。

## 结论

| 需求 | 现状 | 判断 |
| --- | --- | --- |
| 裁剪歌曲片段 | 没有 | 需要新做。建议对齐 half-beat 的「播放区间」语义，而不是导出音频文件 |
| 把当前播放/选中歌曲加入歌单 | 有基础，但只完成了一半 | `a` 只在曲库歌曲列表和搜索结果里生效，且目标由程序隐式决定；队列、歌词里按 `a` 没反应，也不能挑歌单 |
| 把当前选中歌单混入另一歌单 | 只有间接路径 | 只能用 `N` 新建混合歌单时勾选多个歌单，或对已有的混合歌单按 `M` 改引用；没有「源歌单 → 目标歌单」的直接操作，也不能往本地歌单里合并 |

两件事都值得做。第二件风险低、日常收益直接；第一件要多碰播放引擎，边界更多。按下面的方案，整体大约 6–9 人日。

---

## 一、现状核对

### 1.1 裁剪片段：完全没有

- 播放引擎的输入 `audio.Stream` 只有 URL、请求头、编码、总时长、过期时间、音质，没有起止点：
  [`internal/audio/stream.go:8`](../internal/audio/stream.go#L8)。
- `startLocked` 只会因为「从中间起播」加 `-ss`，没有任何 `-t` / `-to` 输出时长限制：
  [`internal/audio/engine.go:211`](../internal/audio/engine.go#L211)。
- `PlayAt`、`Seek`、`Position` 都以整首歌的绝对时间为准：
  [`internal/audio/engine.go:190`](../internal/audio/engine.go#L190)、
  [`internal/audio/engine.go:428`](../internal/audio/engine.go#L428)、
  [`internal/audio/engine.go:447`](../internal/audio/engine.go#L447)。
- 数据层没有每首歌的区间字段；`songs` 表只有标题、歌手、时长等元数据：
  [`internal/store/store.go:64`](../internal/store/store.go#L64)。
- 唯一接近「每首歌的本地偏好」的先例是歌词偏移，存在 `settings` 表里：
  [`internal/tui/cmds.go:553`](../internal/tui/cmds.go#L553)。
- UI 的进度条只画全曲进度，没有区间标记：
  [`internal/tui/view.go:291`](../internal/tui/view.go#L291)。

**half-beat 的对应实现不是导出音频，而是「播放区间」：**

- 每首歌保存 `skipStartTime` / `skipEndTime`，单位为秒，`0` 表示「到结尾」；
- 编辑用 RangeSlider + 数字输入，精度 0.05 秒；
- 播放时 `currentTime < start` 会拉回起点，`> end` 会暂停并派发 `ended`，单曲循环回到 `start`；
- 音频元数据加载后，如果 `end == 0` 会自动补成实际时长。

所以这里要先确认语义：如果用户要的是「只播放这一段」，就是上面的行为；如果还要「把这一段导出成 mp3/m4a」，那是另一条 ffmpeg 落盘路径，工作量再加约 1 人日。

### 1.2 加歌到歌单：有，但入口和目标都受限

- `a` 的按键动作本身存在：[`internal/keymap/keymap.go:56`](../internal/keymap/keymap.go#L56)。
- 但它只在两个地方被处理：
  - 曲库歌曲焦点：[`internal/tui/model.go:1117`](../internal/tui/model.go#L1117)
  - 搜索结果：[`internal/tui/model.go:1233`](../internal/tui/model.go#L1233)
- 队列页没有处理 `a`：[`internal/tui/model.go:1480`](../internal/tui/model.go#L1480)。
- 歌词页走公共播放键，也没有处理 `a`。
- 更关键的是目标不可选。`addTarget()` 的规则是：
  1. 当前本地/混合歌单；
  2. 否则第一个本地/混合歌单；
  3. 都没有就自动建一个「本地歌单」。
  [`internal/tui/model.go:2160`](../internal/tui/model.go#L2160)
- 因此用户只能「加入程序替你选的那个歌单」，不能选目标；也不能在听歌时把正在播放的歌加进另一个歌单。README 里写的「`a` 一键加入」没有说清目标规则：[`README.md:167`](../README.md#L167)。

### 1.3 歌单混入：只有混合歌单的「引用」间接路径

- 混合歌单的模型是「引用」而不是「复制」：
  [`docs/adr/0011-mixin-playlists.md`](adr/0011-mixin-playlists.md)；
  数据层是 `playlist_refs`：
  [`internal/store/store.go:456`](../internal/store/store.go#L456)。
- 新建混合歌单时，`N` → 新建混合歌单 → 勾选多个歌单 → 命名：
  [`internal/tui/palette.go:53`](../internal/tui/palette.go#L53)、
  [`internal/tui/mix.go:20`](../internal/tui/mix.go#L20)。
- 已有的混合歌单可以按 `M` 编辑引用：
  [`internal/tui/model.go:1140`](../internal/tui/model.go#L1140)、
  [`internal/tui/mix.go:121`](../internal/tui/mix.go#L121)。
- 限制：
  - 混合歌单不能嵌套引用：[`internal/store/store.go:533`](../internal/store/store.go#L533)；
  - 普通本地歌单不能「引用」别的歌单，只能逐首加；
  - 远端同步歌单是只读镜像，目前不支持写入远端（ROADMAP 第 7 项，约 6 人日）：
    [`ROADMAP.md:70`](../ROADMAP.md#L70)。
- 所以「把当前选中歌单混入另一歌单」今天只能这样绕：
  - 如果目标是混合歌单：选中目标 → `M` → 勾选源歌单 → `enter`；
  - 如果目标是本地歌单：没有直接操作，只能把源歌单的歌逐首加进去；
  - 如果想把一个歌单「合并」进另一个本地歌单：没有入口。

---

## 二、方案 A：每首歌的播放区间（对齐 half-beat）

### 2.1 数据存哪里

推荐存在 `settings` 表，而不是给 `songs` 加列：

- key：`clip:<songID>`，value：`{"start_ms":12340,"end_ms":234560}`；
- 与 `lyric_offset:<songID>` 的生命周期一致，都是「本地偏好」；
- 远端歌单刷新、重新 Upsert 歌曲时不会碰它；
- 不用改 songs 的建表、迁移、Upsert、Replace、Search 里重复的 SELECT/INSERT。

代价是列表页想给所有歌显示剪刀标记时要逐首查；第一版只给当前歌曲显示，够用。以后真要在列表里标，可以加内存缓存或再迁到列。

需要补的 store 方法：

```go
type Clip struct {
    StartMS int64
    EndMS   int64 // 0 表示到结尾
}

func (s *Store) Clip(songID string) (Clip, error)
func (s *Store) SetClip(songID string, c Clip) error
func (s *Store) ClearClip(songID string) error
```

校验规则：

- `0 <= start < end`（end 为 0 时按总时长算）；
- 最小区间建议 0.5 秒，避免 ffmpeg 空转；
- `end` 超过总时长时 clamp 到总时长；
- 总时长未知（`duration == 0`）时允许只设 start，end 保持 0。

### 2.2 播放引擎怎么改

给 `audio.Stream` 增加两个字段（不要把区间塞进 `source.Stream`，音源不该知道本地剪辑）：

```go
type Stream struct {
    // ...现有字段
    ClipStartMS int64
    ClipEndMS   int64 // 0 = 不限制结尾
}
```

引擎行为：

1. `PlayAt`：
   - 如果 `at < clipStart`，改成 `clipStart`；
   - 如果 `at >= clipEnd`（clipEnd > 0），回到 `clipStart`；
   - `seekBase` 记录真正起播的绝对位置。
2. `startLocked`：
   - 保留现有 `-ss <start>`；
   - 在 `-i` 之后追加 `-t <clipEnd-start>`，让 ffmpeg 输出到区间末尾自然退出。
3. `Seek`：
   - clamp 到 `[clipStart, clipEnd]`；
   - 如果目标正好等于 `clipEnd`，可以当成「已到结尾」交给上层，或者 clamp 到 `clipEnd - 1ms` 让它自然结束。
4. `Position` / `Duration`：
   - **保持绝对时间**。`Position` 仍返回全曲位置，`Duration` 仍返回全曲时长。
   - 理由：歌词时间轴、断点恢复、队列保存的进度都是全曲绝对时间；改成区间相对时间会牵动三处，而且用户会看到进度条突然从 30 秒开始。
5. `EventEnded`：
   - ffmpeg 因 `-t` 正常退出，现有 `watch` 会发 `EventEnded`，上层 `advance()` 不用改逻辑。

### 2.3 TUI 怎么改

不要新增 `view`。当前 `viewNames` 和 `1-6` 直达是按下标绑定的，新增视图要动编号和轮转；片段编辑是模态操作，做成和命令面板/帮助弹窗一样的浮层更合适：

- 新状态 `clip *clipState`；
- `View()` 在 palette/helpPopup 之后叠加 `overlayBox`；
- `handleKey` 在 palette、prompt、搜索输入、helpPopup 之后优先处理 clip 浮层；
- 这样不影响 `viewNames`、`cycleViews` 和现有帧测试。

浮层内容建议：

```
┌ 片段 · 歌名 ───────────────────────────┐
│  起点  0:12.4   终点  3:45.0   长度 3:32.6 │
│  ├──────────[=============]──────────┤   │
│  当前位置 1:03.2                          │
│  space 播放/暂停 · x/X v/V 移动 · { 设起点 · } 设终点 │
│  ctrl+r 清除 · enter 保存并从此段开头播放 · esc 取消 │
└────────────────────────────────────────┘
```

键位：

- 新增动作 `edit_clip`，默认 `C`；
- 新增动作 `clip_set_start`，默认 `{`；
- 新增动作 `clip_set_end`，默认 `}`；
- 新增动作 `clip_clear`，默认 `ctrl+r`（`r` 已被刷新占用，不能重复绑定）；
- 保存用 `enter`，取消用 `esc`，和现有 prompt 一致；
- 浮层里继续用 `space`、`x/X`、`v/V`、左右键，不需要新造一套定位键。

交互流程（键盘比滑杆更适合 TUI）：

1. 播放到想要的起点，按 `{`；
2. 继续播放/快进到想要的终点，按 `}`；
3. `enter` 保存，从新起点重新起播；
4. 想恢复整首，`ctrl+r` 清除后 `enter`。

显示：

- 播放条在全曲进度上把区间外画暗、区间内画亮；
- 正在播放行加 `✂ 0:12–3:45` 标签；
- 进度条仍显示 `当前位置 / 全曲时长`，不改成区间时长。

### 2.4 需要一起处理的边界

- **起播**：`playSong` 把 `position` 设 0，但 `startStream` 要把 0 clamp 到 `clipStart`。
- **下一首/上一首**：`advance` / `rewind` 每首用自己的区间，预解析路径 `advance` 里 `startStream(..., 0)` 也要 clamp。
- **断点恢复**：保存的 `position` 可能在区间外，恢复时 clamp；大于等于终点就回到起点并提示。
- **单曲循环**：`advance()` 在 `modeLoopOne` 里会重新 `playSong`，`startStream` clamp 后自然从区间起点开始。
- **暂停后继续**：`engine.Resume()` 不重启 ffmpeg，不受影响。
- **已结束再按 space**：`togglePlayback` 的 `StateEnded` 分支从 0 起播，需要 clamp 到区间起点。
- **seek 越界**：`seekBy` 现在只按 `[0, duration]` clamp；要改成按 `[clipStart, clipEnd]`，并给一次「已限制在片段内」的提示，避免用户以为 seek 坏了。
- **地址过期**：seek/续播会重新解析，`startStream` 重新带上 clip 即可。
- **下载**：`w` / `W` 仍下载整首；如果要做「下载片段」，单独加 `download.RunClip`，不要偷偷改变 `w` 的语义。
- **精度**：`watch` 在 ffmpeg 退出后会 `Sleep(120ms)` 让 oto 把缓冲放完，区间末尾可能多出约 0.1 秒。要精确到采样点，应该在 clip 结束时立即关 player，或者走导出路径，不要指望 `-t` 绝对精确。

### 2.5 不推荐的做法

- **只在 TUI tick 里判断 `position >= end` 然后 stop+advance**：实现一天能完，但最多会多播一个 tick（250ms）加声卡缓冲，seek 到区间外也不好处理。只适合临时验证。
- **把播放位置改成区间相对时间**：歌词、断点、队列进度全部要改，收益为零。
- **把区间写进 queue**：队列是快照，同一首歌可能出现在多个队列/歌单；按 songID 存偏好更合理。
- **新增一个完整 view**：会牵动 `viewNames`、`1-6`、`cycleViews`、帧测试；模态浮层足够。

### 2.6 测试

- `store`：Clip 写入/读取/清除/非法值；歌曲重新 Upsert 后 clip 仍在。
- 引擎：`startLocked` 的 ffmpeg 参数（建议把参数拼装抽成纯函数测）；`PlayAt`/`Seek` 的 clamp；`Position` 仍是绝对时间。
- TUI：`C` 打开浮层、`{`/`}` 设点、`enter` 保存后从起点起播、`esc` 不改库、窄终端帧宽高。
- 真流验证：B 站 DASH 音频和网易云 mp3 各测一次 seek 到区间中间、区间末尾、区间外。

---

## 三、方案 B：目标选择器 + 歌单混入

### 3.1 共用一个「目标歌单」浮层

加歌和混入都需要「选一个目标歌单」，做一个可复用的浮层，复用命令面板的筛选/上下键/`enter` 逻辑：

- 列表只放**可写**歌单：`kind == local` 或 `kind == mixin`；
- 远端歌单不列出；如果用户只有远端歌单，底部提示「远端歌单只读，先按 U 解锁或按 N 新建」；
- 第一项可以是「＋ 新建本地歌单并加入」，解决一个可写歌单都没有的情况；
- 当前歌单/上次使用的目标排前面并标 `●`；
- 记住上次目标：`settings` 里存 `last_add_playlist`，目标被删了就回退。

### 3.2 加歌：把 `a` 变成「选目标」，并覆盖队列/歌词

现在 `a` 的问题是「有动作，但只在两个页面、目标隐式」。建议改成：

- `a` 在曲库歌曲焦点：加选中的歌；
- `a` 在搜索结果：加选中的结果；
- `a` 在队列页：加队列光标所在的歌（进入队列页时光标默认就是正在播放那首）；
- `a` 在歌词页：加正在播放那首。

实现上不要把 `a` 塞进 `handleCommonPlaybackKey`（会误伤帮助/登录/混合歌单页），而是在 `handleKey` 里加一个按视图取歌的 helper：

```go
func (m *Model) songForPlaylistAdd() (store.Song, bool) {
    switch m.view {
    case viewLibrary:
        if m.libraryFocus == focusSongs { ... }
    case viewSearch:
        ...
    case viewQueue:
        ...
    case viewLyrics:
        ...
    }
}
```

选中后打开目标浮层；`enter` 调现有的 `addSongToPlaylist(target, song)`（它已经会 Upsert、去重、取消 exclusion）：

- [`internal/tui/model.go:2140`](../internal/tui/model.go#L2140)
- [`internal/store/store.go:631`](../internal/store/store.go#L631)

另外在命令面板加一条「将正在播放加入歌单」，覆盖「人在曲库、歌在后台播放、想加当前歌」这种场景；否则在曲库里只能加选中的歌。

行为变化与兼容：

- 以前 `a` 是「一键加入当前/第一个歌单」，现在多一步 `enter`。这是为了可选择性，值得；
- 没有可写歌单时，浮层里的「新建本地歌单并加入」可以复用现有 prompt；现有测试 `TestAddSongCreatesLocalPlaylistWhenNone` 需要改成走这条路径；
- 远端歌单仍然不能写，别在 UI 上给假承诺。

### 3.3 混入：源歌单 → 目标歌单

入口：

- 新增动作 `merge_playlist`，默认 `S`（`S` 目前空闲；`M` 继续保留给混合歌单编辑引用）；
- 只在曲库、且当前有选中歌单时可用；歌单焦点或歌曲焦点都行，因为 `currentPlaylist()` 始终指向左栏选中项；
- 打开同一个目标浮层，排除源歌单自身。

语义建议（按目标类型自动选择，避免多一个模式开关）：

| 目标类型 | 行为 | 理由 |
| --- | --- | --- |
| `mixin`，且源不是 `mixin`，且尚未引用 | **增加引用** | 这是项目原生的「混合」语义，源更新自动同步，成本最低 |
| `mixin`，已引用源 | 不操作，提示「已引用」 | 避免重复 |
| `local` | **复制曲目** | 本地歌单没有引用模型，只能物化 |
| 目标是 `remote` | 拒绝 | 远端写操作未实现，见 ROADMAP 第 7 项 |
| 源是 `mixin` | **复制展开后的有效曲目** | 混合歌单不能嵌套引用 |

store 侧需要两个方法（二选一或都做）：

```go
// 目标为 mixin 时，追加一个引用
func (s *Store) AddMixinRef(playlistID, refID string) error

// 目标为 local（或源是 mixin）时，复制有效曲目
type MergeResult struct {
    Added   int
    Skipped int
}
func (s *Store) MergePlaylist(targetID, sourceID string) (MergeResult, error)
```

`MergePlaylist` 的规则：

- 只接受目标 `local` / `mixin`，拒绝 `remote`、拒绝自己；
- 源曲目用 `PlaylistSongs(sourceID)` 取（会自动展开 mixin、去重）；
- 目标现有可见曲目用 `PlaylistSongs(targetID)` 取，按 songID 去重；
- 只追加目标里还没有的歌，保持源顺序；
- 目标如果是 mixin，追加自己的 `playlist_items` 时同时删除对应的 `playlist_excludes`；
- 全程事务；位置从 `MAX(position)+1` 开始；
- 不修改源歌单，不移动源曲目（「混入」按非破坏性复制理解）；
- 返回 added/skipped 给 UI 提示。

确认与反馈：

- 选完目标后走现有 prompt 机制，底部显示：
  「将「源」的 N 首混入「目标」？混合歌单会引用源，本地歌单会复制 · enter 确认 · esc 取消」
- 完成后提示：「已引用「源」到「目标」」或「已复制 N 首到「目标」，跳过 M 首重复」；
- 刷新歌单列表计数（`loadLocalPlaylistsCmd`）。

边界：

- 跨源同曲不合并：B 站和网易云的同名版本是不同 songID，会同时存在（依赖未来的跨源匹配，ROADMAP 第 1 项）；
- 目标 mixin 已有 exclusion 的歌，如果源里也有，按「源曲目」加入并取消 exclusion，视为用户主动恢复；
- 大歌单（几百首）一次性复制是 SQLite 毫秒级，不需要后台任务，但要做确认；
- 删除被引用的源歌单仍会被 `IsReferenced` 拦住，这是现有语义，不是 bug；
- 如果用户把远端歌单混入本地歌单，之后退出登录/远端刷新不会影响已复制的本地条目。

### 3.4 测试

- store：`AddMixinRef` 的去重/嵌套/自引用；`MergePlaylist` 到 local、到 mixin、源是 mixin、重复曲目、exclusion、远端目标拒绝。
- TUI：四个视图里 `a` 取到正确的歌；目标浮层筛选/确认/新建；`S` 只在有选中歌单时打开；确认文案与计数。
- 帧测试：浮层在 60×18 和 100×30 下不超宽、不超高。

---

## 四、工作量与顺序

| 工作 | 内容 | 估时 |
| --- | --- | --- |
| B1 目标选择器 + 全视图加歌 | 浮层、按视图取歌、`a` 改造、命令面板补「正在播放」、测试 | 1.5–2 人日 |
| B2 歌单混入 | `AddMixinRef` / `MergePlaylist`、复用浮层、确认、测试 | 1.5–2 人日 |
| A 片段播放 | settings、引擎 `-t` + clamp、模态编辑器、进度标记、测试 | 3–4 人日 |
| 文档与回归 | help/README/demo、全量测试、真流手测 | 0.5–1 人日 |
| **合计** | | **6.5–9 人日** |

建议顺序：

1. **B1**：先解决「明明有 `a` 却找不到/不能选目标」的问题，风险最低，马上改善日常使用；
2. **B2**：复用 B1 的浮层，增量很小；
3. **A**：独立但边界最多，放在后面，先按「播放区间」做，确认不需要导出再加。

如果只想先做一个最小版本：

- 最小 B：只做「加歌目标选择器 + 队列/歌词可用」，约 1–1.5 人日；
- 最小 A：只做「当前歌曲设起止 + 播放限制」，不做列表标记和区间外视觉，约 2–2.5 人日；
- 不建议做 tick 版区间，精度和 seek 体验都会留坑。

---

## 五、主要风险

1. **ffmpeg 对网络流的 `-ss` / `-t` 行为**：B 站 DASH 和网易云 mp3 要分别真流验证；现有 `-ss` 已经在用，风险主要在末尾截断和缓冲。
2. **末尾多放约 120ms**：`watch` 的 drain 逻辑导致；要精确就改 engine 或走导出，不要用 UI 计时器补。
3. **远端歌单只读**：这是产品边界，不是本次能顺手解决的；ROADMAP 估 6 人日。
4. **跨源重复**：没有 match 层，混入后可能出现两个平台的同一首歌；文档要写清。
5. **`a` 行为变化**：从一键变成两步，需要更新 README、help 和现有测试；如果很在意速度，可以保留「目标浮层里 enter 默认当前歌单」。
6. **帧测试**：所有浮层都必须满足精确宽度/高度，`view_test.go` 会卡。

---

## 六、落地时建议补的 ADR

- **ADR：每首歌的播放区间**：存 settings 而不是 songs、绝对时间轴、引擎 `-t`、不做导出。
- **ADR：歌单混入**：目标 mixin 引用优先、local 复制、远端只读、非破坏性、跨源不去重。

这两份 ADR 可以把「为什么不是复制/为什么不是相对时间/为什么不写远端」固定下来，避免以后反复讨论。

---

## 七、需要确认的两件事

1. **「裁剪」是只播放这一段，还是要导出片段文件？**
   - 只播放：按方案 A，3–4 人日；
   - 还要导出：加 `download.RunClip`（ffmpeg `-ss` + `-t` + 标签/封装），再加约 1 人日，并且要想清楚 `w` 是下载整首还是片段。
2. **「混入」是复制曲目，还是把源歌单作为引用加进去？**
   - 推荐按目标自动：目标 mixin → 引用；目标 local → 复制；
   - 如果你希望「一律复制、行为完全可预测」，B2 可以简化成只做 `MergePlaylist`，少一个分支。
