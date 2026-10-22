# 落地实施计划：评审收尾 → v0.5.0

> 对象：`refactor/decouple-tui`（相对 main 的 4 个提交，`e1229dc` / `5f8fd2f` / `037df1f` / `b926b99`）
> 依据：本轮评审的三份报告（`tmp/review/tech-arch.md`、`tmp/review/tech-tui.md`、`tmp/review/product.md`，未入库）
> 目标：把评审里的 1 个阻断回归 + 4 个应修项 + 下载闭环 6 个洞 + 交互回归收干净，统一列表/歌单行样式，作为 **v0.5.0** 一次发布。
> 产出方式：每个任务一个提交，合并到一个 PR 后让 CI 跑（CI 只在 push main / PR 触发），不做「一天 4 个提交、7000 行同批」。

---

## 0. 前置约束（开工前确认）

| 项 | 现状 | 处理 |
| --- | --- | --- |
| 分支 | `refactor/decouple-tui` 领先 main 4 个提交，未合、无 PR/CI 记录 | 保留分支，后续每个修复单独提交，最后一次开 PR 合 main |
| 构建/测试 | `go build ./...`、`go vet ./...`、`go test ./...`（17 包）全绿 | 每步完成后重跑；`make check` 作为闸门 |
| race | 默认 `GOCACHE` 只读，`-race` 会误报 `runtime/race: package testmain` | 固定用 `GOCACHE=$PWD/tmp/review/gocache go test -race ./...`，CI 里配置可写缓存 |
| 手测 | 本环境无音频/网络 | 发布前在真机走一遍：B 站/网易云各「搜索→播放→下载→本地优先→删文件→回落远程」 |
| 版本 | v0.4.1 已发布；有功能 + 破坏性按键变更 | 抬 **v0.5.0**，不单独为重构发版 |

---

## 1. 阶段总览

| 阶段 | 内容 | 预计 | 交付判定 |
| --- | --- | --- | --- |
| P0 阻断 | 5 个应修项（2 个用户可见回归 + 2 个交互错乱 + 1 个升级兼容） | 1.5 天 | 新增回归测试全绿，手测无跳歌/无重播 |
| P1 收尾 | 下载闭环 6 洞 + downloader 生命周期 + 数据竞争 + 持久化/游标 | 1.5 天 | 下载页可取消/可见失败/可补坏文件 |
| P2 打磨 | 交互回归回滚、文档与截图、demo 视图、demo 键提示 | 1 天 | README/ROADMAP/ARCHITECTURE 与实现一致 |
| 样式统一 | 歌曲行/歌单行共用一套列布局 | 0.5 天 | 跨页列起点一致，窄屏不溢出（本次已实现） |
| 发版 | release note、tag、AUR、截图 | 0.5 天 | `v0.5.0` Release 可下载安装 |

> 如果只有 1 天：只做 P0 全部 + 样式统一 + README 按键变更段落，发 v0.4.2（不带下载管理宣传），下载管理留到 v0.5.0。

> **本轮已完成**：样式统一（§5）、P0-5（方案 B）、R1（恢复 j/k/h/l + 歌词单键 `T`）、R2（Shift+Tab 反向轮转）。

---

## 2. P0：阻断修复（按依赖顺序，一项一提交）

> 状态：**P0-1 ~ P0-4 已落地**（提交 `d98c2b3` / `650360d` / `561f41d` / `2602dab`），P0-5 见上一轮。
> 每项都先写复现测试（确认在旧代码上失败），再改实现转绿。

### P0-1 `playSearchResult` 索引错位（回归，最高优先级）✅

- 文件：`internal/tui/model.go:1776-1789`
- 改法：闭包内不要读 Model 的 `m.queue` 长度，用 `Enqueue` 的返回值：
  ```go
  m.withController(func(c *player.Controller) { c.SetIndex(c.Enqueue(song)) })
  ```
- 测试：`internal/tui/model_test.go` 加 `TestPlaySearchResultAppendsAndSelects`：搜索源结果入队后 `len(queue)==L+1` 且 `queueIdx==L`；再断言 `advance()` 取到的是下一首而不是同一首。
- 验收：复现用例（审查时已确认失败：期望索引 2、实际 1）转绿。

### P0-2 切歌缓冲期的引擎事件守卫（回归，用户可见跳歌）✅

- 文件：`internal/player/playback.go:252-270`（`HandleEvent`）、`internal/tui/model.go:706-725`
- 改法：把 main 里 TUI 的守卫下沉到控制器（TUI 保持薄）：
  ```go
  func (c *Controller) HandleEvent(ev audio.Event) EventResult {
      if c.state.Awaiting &&
          (ev.Type == audio.EventFailed || ev.Type == audio.EventEnded || ev.State == audio.StateIdle) {
          return EventResult{} // 旧流事件，忽略
      }
      ...
  }
  ```
- 测试：`internal/player/controller_test.go` 加「Awaiting 期间 Failed/Ended 不改变状态、不返回 Retry/Ended」；`internal/tui` 加一条「BeginPlay 后立即喂 EventEnded，`queueIdx` 不变」。
- 验收：审查时的控制器级复现用例转绿；连续快速按 `n` 切歌不再跳过整首。

### P0-3 歌词滚动负起点跳变 ✅

- 文件：`internal/tui/lyrics_nav.go:58-74`
- 改法：两次 clamp 的下界都用 `min(0, base)`：
  ```go
  lo := min(0, base)
  start = clampInt(start, lo, maxStart)
  start = clampInt(start+delta, lo, maxStart)
  ```
- 测试：`internal/tui/lyrics_nav_test.go` 加 `lyricIdx=0` 的用例（`↓` 只移动 1 行、`↑` 能回到居中）与短歌词（`len(rows) < bodyHeight`）用例。
- 验收：歌曲开头按 `↓` 不再从居中跳到顶部，`↑` 能原路返回。

### P0-4 模态层级：搜索输入态 × 关于弹窗 ✅

- 文件：`internal/tui/mouse.go:36-56`、`internal/tui/model.go:823-831`
- 改法：① 页头三个分支（`hitAbout/hitPageMenu/hitAccount`）先 `m.searchEditing = false`；② `handleKey` 里把 `helpPopup/aboutPopup` 判断提到 `searchEditing` 之前。
- 测试：`internal/tui/mouse_test.go` / `view_test.go` 加「输入态点 zero → 弹窗打开且 `searchEditing==false`；按一次 esc 关闭弹窗」。
- 验收：弹窗提示与按键行为一致，不再需要连按 3 次 esc。

### P0-5 老 keymap 配置整份失效 ✅

- 文件：`internal/keymap/keymap.go`（`Load`/`build`）、`internal/tui/model.go:248-296`、`README.md` 自定义快捷键一节
- 现状：旧 `zero keymap --init` 生成的文件含 `right = ["right","l"]`，与新增默认 `lyrics_view = ["l"]` 冲突 → `Load` 失败 → **整份回退默认**（实测报 `按键 "l" 同时绑定到 right 和 lyrics_view`）。
- **已确认采用方案 B（已完成）**：`Load` 区分「用户覆盖项」与「默认项」，冲突时保留用户绑定、只禁用冲突的**默认**绑定，并提示「`download_selected` 的默认键 `W` 已被 `logout` 占用，已禁用」；整份回退只保留给同为用户配置的冲突。已同步 ADR-0012 与 `keymap_test.go`。
- 测试：用旧版 `--init` 产物做 fixture，断言自定义项仍生效、冲突的默认项被禁用且有提示；「用户 vs 默认」「用户 vs 用户」两类冲突分别覆盖。
- 验收：`zero keymap --check` 对旧文件加载成功并打印降级提示；TUI 启动用黄色提示说明让位项，不再静默丢失全部自定义。

---

## 3. P1：下载闭环与生命周期

| # | 任务 | 文件 | 改法要点 | 测试 | 预计 |
| --- | --- | --- | --- | --- | --- |
| D1 | 取消下载 | `internal/tui/downloads.go`、`download.go`、`downloader.Manager` | 下载页 `x` 取消选中任务（运行中取消 context，排队直接移除）；`X` 取消全部并走确认 prompt；被取消的任务转为 `Failed`（`Err=context.Canceled`，可重试） | 任务取消后不再发进度事件、并发槽位回收、下一个排队任务启动 | 0.3 天 |
| D2 | 失败态可见 | `internal/downloader/downloader.go`、`internal/tui/downloads.go`、`view.go` | `Task` 增加 `Failed/Err`；失败任务不自动删除，下载页用 `!` 标记 + 页脚「N 个失败，W 重试」；批量下载结束只弹一条汇总 | 失败任务保留、`W` 重试成功清除标记 | 0.8 天 |
| D3 | `W` 能补坏文件 | `internal/download/download.go:88-93`、`internal/app/download.go:48` | 0 字节目标不返回 `ErrExists`；`DownloadSong` 增加 `force bool`，下载页对 `missing` 行传 `true` | 0 字节文件 → W → 重新下载成功 | 0.3 天 |
| D4 | 跳过已下载 | `internal/tui/download.go:90-164` | 入队前批查 `downloads` 索引，已下载直接跳过并提示「已跳过 N 首」 | 重复下载整个歌单 0 次网络解析 | 0.3 天 |
| D5 | 列表标已下载 | `internal/tui/row.go`、`app/library.go`、各列表加载 cmd | 列表加载时批查下载索引，`songRowSpec.Downloaded=true` → marker 列显示 `↓`（`▶` 优先） | 曲库/队列/搜索/下载页同曲标记一致 | 0.5 天 |
| D6 | 下载页尊重本地优先开关 | `internal/tui/cmds.go:218-232` | `playLocalSongCmdAt` 先读 `local_first`，关闭时走远程解析；README 明说 | 关闭开关后 F 页 enter 走远程 | 0.1 天 |
| D7 | downloader 生命周期 | `internal/downloader/downloader.go` | 加 `Close()`；完成事件 `select` + ctx；`CancelAll` 给排队任务打标记；`runTask` defer recover | Close 后不再接收任务、事件通道关闭不阻塞 | 0.5 天 |
| D8 | 质量参数数据竞争 | `internal/tui/download.go:21-29`、`downloader.Manager` | 把 quality 作为任务参数（`Manager.Add(song, title, params)` 或 `Runner` 签名带 `Params{Quality}`），不在 goroutine 里读 `m.quality` | 静态消除；`-race` 下并发下载+切音质不报 | 0.3 天 |
| D9 | 播放/队列状态同步 | `internal/tui/model.go:1791-1806,1846-1872` | `playSong` 后 `persistQueue()`；`advance/rewind` 后同步 `queueCursor = queueIdx` | 自动切歌后队列页选中行跟随；解析中退进程恢复索引正确 | 0.2 天 |

---

## 4. P2：交互回归与文档

| # | 任务 | 文件 | 决策 |
| --- | --- | --- | --- |
| R1 | 恢复 `j`/`k`/`h`/`l`，歌词换键 | `internal/keymap/keymap.go` | **已完成**：`j`/`k` 回上下、`h`/`l` 回左右焦点；歌词改用单键 `T`（`ctrl+l` 备选）。裸 Ctrl 不可行：终端默认不发修饰键，kitty 协议下又会与 Ctrl+C/D/W 组合冲突 |
| R2 | `Shift+Tab` 反向轮转 | `internal/tui/model.go`、`nav.go` | **已完成**：`Shift+Tab`（终端 CSI Z → Tab+Shift）反向轮转，混合编辑器按曲库位置计算 |
| R3 | `esc` 在曲库首页不再打开关于 | `internal/tui/model.go:840-851` | `esc` 恢复「无操作」；「关于」进命令面板 `N` 新增一项，页头点击保留 |
| R4 | 鼠标开关 | `internal/tui/view.go:44`、`internal/app/settings.go`、README | `ZERO_MOUSE=0` 环境变量 + 设置项 `mouse`（默认开）；关闭时 `MouseModeNone`；README 补 tmux/ssh/mosh 说明 |
| R5 | 右键菜单「下载」键提示 | `internal/tui/context_menu.go:66` | 改用 `keymap.DownloadSelected`（`W`），与真实动作一致 |
| R6 | 帮助页按键列截断 | `internal/tui/keys.go:66` | `keyW` 按内容自适应（最长键串 + 1），窄终端允许换行/省略，不再吞 `PgDn` |
| R7 | 残留旧键提示 | `model.go:1281,1286`、`mix.go:184` | 全部改成 `m.firstKey(keymap.*)` 拼接，与 `hintLine` 一致 |
| R8 | `o` 打开二维码未接线 | `internal/tui/model.go:975` | 登录页 `m.match(keymap.LoginImage)` 与旧 `v` 同时生效 |
| R9 | `demo` 缺菜单/关于视图 | `internal/tui/demo.go` | 加 `menu`/`about` 两个视图，可出截图、可回归 |
| R10 | 文档漂移 | README / ROADMAP / ARCHITECTURE / ADR-0014 | ROADMAP 补 b926b99 能力；ARCHITECTURE:36 改掉「TUI 维护任务队列」，数据流补 `BeginPlay/CompletePlay`；ADR-0014 后果段加「见 0015」；README 补下载页截图、鼠标段 tmux/关闭方式、本地优先状态行显示实际码率 |
| R11 | 帮助/键位表与实现对齐 | README 键位表、`keys.go` 鼠标段 | 补「滚轮可滚列表」「导入页右键」；`▶` 语义变化写入说明 |
| R12 | 死代码/边角 | `player/playback.go:220`、`model.go:789`、`store.go:320` | 删 `_ = engineActive`/`_ = preview`；`UpsertSong` 的 `bvid = CASE WHEN excluded.bvid <> '' ...`；`Ext` 未知 codec 让适配器报错而不是猜 `.m4a` |

---

## 5. 样式统一（本次已实现，未提交）

**问题**：歌曲行有 5 套渲染（曲库/队列/源搜索/本地搜索/下载），序号 2 位 vs 3 位、有无播放标记、艺人列 14 vs 16、meta 宽度各异；歌单行有 4 套（侧栏 6 列前缀、导入页 2 位序号、picker 9 列 badge、混合编辑器 `[x]`+8 列来源）。切页时列会跳。

**方案**：新增 `internal/tui/row.go`，两个渲染器 + 一套语义：

- 歌曲行：`marker(2) + tag(4) + 序号(3) + 标题(弹性) + 艺人(14) + meta(固定宽右对齐) [+ extra(固定宽右对齐)]`，宽度 72/48/34 三级降级（先收 extra、再收艺人、最后只留 marker+tag+序号+标题）。
- 右列固定宽度：`MetaW`/`ExtraW` 由调用方按列表传入（时长 `rowDurW=7`、搜索额外列 `rowExtraW=18`、下载 `rowDownloadW=17`），**同一列表内艺人/时长/播放量列不再随内容浮动**。
- 歌单行：`marker(2) + tag(4) + [check(4)] + 标题(弹性) + count(右对齐 4)`；数量列自适应——标题不足 8 列时先丢数量、再丢 tag，不再用「宽度 <24 就丢数量」的固定阈值。
- 侧栏宽度 20→22（窄屏仍 14），容纳 tag + 标题 + 数量，歌单名不再因加 tag 被挤短。
- 语义统一：光标 = 蓝底；正在播放 = 灰底 + `▶`；`▶` **只表示正在播放**（对话框/编辑器光标只用蓝底）；缺失/错误 meta = 红字；只有光标行滚动标题。
- 列表补齐：队列序号 2→3 位、队列/搜索/下载补播放灰底与 `▶`、搜索补序号与独立艺人列、下载 meta = `大小 · 编码`。
- 新歌单 tag：remote=`[B]`/`[云]`、mixin=`[混]`、local=`[本]`、新建=`[新]`；替代 picker 原来的 `[B 云端]`。

**验收**：新增 `row_test.go`（跨视图列起点一致、同列表右列对齐、窄宽度 16–120 行宽精确、播放/光标样式、勾选行、侧栏数量自适应）；既有 `assertFrame` 与行断言零修改通过；`go test ./...`、`go test -race ./internal/tui`、`go vet`、`gofmt` 全绿；`zero demo library/search/queue/downloads/picker` 目测列对齐。

---

## 6. 发布清单（v0.5.0）

1. `make check` + `GOCACHE=... go test -race ./...` + `zero demo` 全视图。
2. 真机手测：B 站与网易云各走 搜索 → 播放 → 下载 → 本地优先 → 删除文件 → 回落远程；断网启动一次；批量下载后取消/重试一次。
3. 文档：README 按键表 + 鼠标段 + 已知限制（取消/失败重试/本地优先细节）、ROADMAP「v0.5 已完成」、`docs/images/downloads.webp`。
4. release note 结构：**「⚠️ 按键变了」置顶**（j/k/h/l 恢复方向与焦点、歌词改为 `T`、Tab 9 页 + `Shift+Tab` 反向、1-9 重编、esc 不再弹关于）→ 新增（下载管理、本地优先、鼠标、歌词滚动）→ 修复 → 内部重构一句带过。
5. `git tag v0.5.0 && git push origin v0.5.0`；AUR `zero-beat-git` 修版本注入（不要再用 `${pkgver%%.r*}`，否则关于弹窗显示已发布版本号）。
6. 合 main 并确认 CI 绿（分支上不再堆积无 CI 的 7000 行）。

---

## 7. 风险与回滚

- **P0-2 守卫下沉**可能让「解析失败但引擎仍应停」的路径漏处理：守卫只忽略 `Failed/Ended/Idle`，`CompletePlay` 的错误路径独立处理，测试覆盖两端。
- **P0-5 方案 B** 改动 keymap 语义（ADR-0012），需要单独评审；来不及时先走 A 并置顶 release note。
- **样式统一**改的是纯渲染，不改行数，命中区不受影响；若视觉有争议，`row.go` 是唯一改动点，回滚一个文件。
- **D2 失败态**会让下载任务列表变长，需要「移除」路径同时兼顾清理；上线前确认真实失败（VIP/无版权）与取消（`context.Canceled`）区分展示。
- 整批改动仍可能引入回归：每个任务一个提交，便于二分；合并后先在本地跑 3 天再用 v0.5.0 发版。

---

## 8. 明确不做（这批）

- 让 `player.Controller` 真正持有状态（ADR-0015 收尾）：单独迭代，别和发版混。
- 继续扩鼠标/菜单交互（hover、拖拽、二级菜单）；先只补开关与文档。
- MPRIS/媒体键/单实例锁、跨源兜底：v0.5.1 起的下一批，按产品评审的 ROI 顺序。
- Windows 适配、封面/sixel、凭证加密（最多做 0.5 天的数据目录 `chmod 600` + 文档）。

## 9. 实施进度（截至 2026-10-22 下午）

**已完成**（每项一个提交，见 `git log`）：

- P0 全部：搜索结果索引、切歌期事件守卫、歌词滚动负起点、模态层级、keymap 冲突方案 B
- 交付物：列表/歌单行样式统一、鼠标点击浮层外关闭、鼠标捕获开关（`ZERO_MOUSE=0`）
- P1：D1 取消下载（`x`/`X`）、D2 失败任务保留 + 常驻状态行、D3 0 字节文件重下、D4 已下载跳过、D6 下载页遵守本地优先、D7（panic 兜底 + CancelAll 语义）、D8 任务参数快照、D9 队列游标/起播持久化
- P2：R3 esc 回归取消语义（关于进命令面板）、R4 鼠标开关、R5 右键下载键提示、R6 帮助列宽、R8 登录页 `o`、R9 `demo menu/about`、R12 bvid 保留与死代码
- 额外修复：暂停时按 ←/→（及 `x`/`v`）调进度不再开始播放，按钮状态与引擎一致
- 歌词页交互重做：中央焦点行固定，「跟随 / 浏览」两态；↑↓/翻页/滚轮/单击移动焦点，Enter 跳转并恢复跟随，Esc 回播放行，双击跳转；正在播放行用蓝色渐变 + `▶`，焦点行用灰底，删除 5 秒自动回中

**剩余**：

- D5 列表「已下载」标记：需要在曲库/队列/搜索加载时批查下载索引（`↓` 放 marker 列，`▶` 优先）
- D2 下载页失败分区与「重试失败任务」入口：当前失败任务保留为常驻提示，进下载页清理；重试靠 `W`
- D7 `Manager.Close`：进程退出时主动收尾（目前依赖进程退出）
- R10 发布物料：下载页/菜单/关于截图、release note（按键变更置顶）、AUR -git 版本注入修正
- 发布：`make check` + `go test -race ./...` + 真机手测（下载→本地优先→删文件→回落、暂停调进度、取消批量下载）+ `v0.5.0` tag
