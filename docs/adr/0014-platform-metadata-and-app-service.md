# 0014 平台元数据下沉到适配器，业务从 Bubble Tea 模型抽到 app 服务

- 状态：已接受
- 日期：2026-10-22

## 背景

v0.3 接第二个音源时补了 `source.Source`，但平台信息仍然散落在核心包里：

- `source.ID` 用 switch 穷举 `Bilibili` / `NetEase` 的名称与短标记；
- CLI 的登录别名、下载目标解析、TUI 的命令面板、歌词空状态、登录 App 名
  各有一份平台分支；
- 每日推荐只给网易云建入口，`Candidate.Fee` 是网易云专用字段，
  `download.Ext` 用平台名猜扩展名；
- `internal/tui/cmds.go` 直接做多源搜索、取流、歌词兜底、歌单同步与登录，
  `Model` 同时承担 UI 状态和业务编排。

继续加平台会把这些问题放大：每加一个源都要改核心包、CLI、TUI 和下载层。

## 决策

### 平台元数据属于适配器

- `source.Source` 增加必选方法 `Mark() string`，短标记由各适配器返回。
- 其余展示/解析能力用可选接口表达：
  - `Aliased`：命令行别名；
  - `TargetParser`：把链接/编号解析成 `Ref`；
  - `LyricNamer`：歌词来源展示名；
  - `LoginNamer`：扫码登录页的 App 名。
- `source.Registry` 负责别名查找（`GetByName`）、展示兜底（`Name` / `Mark` /
  `LyricName` / `LoginAppName`）和按注册顺序尝试 `ParseTarget`。
- 删除 `source.ID` 上的 `Label` / `Mark` switch；B 站与网易云分别在适配器里
  实现 `Mark`、`Aliases`、`ParseTarget` 等。

### 通用字段不再带平台语义

- `Candidate.Fee int` 改为 `Candidate.VIP bool`；网易云在搜索映射里把
  `fee == 1 || fee == 4` 转成 `VIP`。
- `download.Ext(codec, src)` 改为 `Ext(codec)`；未知编码统一按 `.m4a` 兜底，
  适配器负责在 `Stream.Codec` 里给出真实编码。
- `store.NormalizeSong` 接管歌曲派生字段，但**不猜音源**，也不再自动写
  B 站专用的 `BVID`；`BVID` 只作为旧库兼容字段读取。
- 每日推荐入口的 ID 由 `app.DailyPlaylistID(sourceID)` 生成，`app.IsDailyPlaylist`
  统一判断；任何 `CapDailyRecommendations` + `DailyRecommender` 的音源都会获得入口。

### 新增 `internal/app`

`internal/app` 是界面的业务服务，持有 `source.Registry` / `store.Store` /
`lrclib.Client`，提供：

- `Search`：多源并发搜索、部分失败告警；
- `Stream` / `Resolve`：取流、元数据合并与回写；
- `Lyrics`：平台歌词优先、LRCLIB 兜底；
- `Playlists` / `LocalPlaylists` / `RemoteFolders` / `PlaylistSongs`：
  默认歌单、每日推荐、远端镜像与混合歌单刷新；
- `Users` / `StartLogin` / `PollLogin`：账号与扫码登录；
- `RestoreQueue` / `SaveQueue`：队列恢复与持久化；
- `Preferences` 与各 `Save*`：音量、补偿、音质、模式、播放歌单；
- 歌单 CRUD、混合歌单引用、片段设置、本地搜索、账号持久化；
- `AddToCloudPlaylist`：同源云端写入。

`internal/tui/cmds.go` 只保留 `tea.Cmd` 包装和消息类型：从 `Model` 取依赖、
构造 `app.Service`、把返回值转成 `tea.Msg`。`app` 不 import Bubble Tea。

## 理由

1. 平台差异应该收敛在适配器里；核心包和 UI 只认能力位与少量通用字段。
2. 业务服务可以脱离 TUI 单测：`internal/app` 的测试直接构造 fake source 和临时
   SQLite，不需要渲染一帧终端。
3. `tea.Cmd` 只负责「在 goroutine 里跑、把结果送回 Update」，不再承载搜索/同步
   的具体流程，后续换 UI 或加 CLI 复用时不需要复制业务。
4. 用可选接口而不是继续膨胀 `Source` 主接口，第三方实现可以只实现必要能力。

## 后果

- 所有 `source.Source` 实现（含测试 fake）都要补 `Mark()`。
- 平台链接解析从 `cmd/zero` 移到适配器；`zero download` 的目标识别由注册表驱动。
- `source` 核心包不再知道具体平台，新增平台只需新适配器 + 组合根注册。
- 队列索引、洗牌/恢复、预解析缓存已抽到 `internal/player`；当前曲目、播放状态与
  引擎控制仍在 `internal/tui`，定位为播放器的视图控制器，只调用 `app.Service` 与
  `audio.Engine`。下载任务队列/进度也属于界面状态。

## 替代方案

- 继续在 `source.ID` 上 switch：每加平台改核心包，TUI 仍会出现平台分支，放弃。
- 把所有方法都塞进 `Source` 主接口：第三方实现成本高，且大部分平台并不支持
  云端写歌/每日推荐，放弃。
- 直接在 TUI 里调用 store/source：实现快，但业务无法脱离 Bubble Tea 测试，放弃。
