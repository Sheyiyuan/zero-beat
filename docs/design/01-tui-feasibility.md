<!-- 本文件由 v1 仓库 docs/ 迁入，作为本项目的设计依据；实现细节以 docs/adr/ 为准。 -->

# Half Beat Player TUI 版：难度与技术栈评估

> 评估对象：以现有 Wails 桌面版的功能设计为蓝本，参考 [go-musicfox](https://github.com/go-musicfox/go-musicfox) 的完成度，重写一个终端版本。
> 评估依据：本仓库源码实测（Go 8,189 行非测试代码 / 前端 19,742 行）、B 站 playurl 接口实测、musicfox 源码实测（46,856 行非测试代码）。

---

## 0. 结论摘要

| 维度 | 判断 |
| --- | --- |
| 整体难度 | **7 / 10**。TUI 界面本身不难，只是量大；真正难的是"终端里没有浏览器"，音频解码与播放引擎要从零造。 |
| 可复用度 | 后端约 **65%** 可直接复用（`models`/`db`/`services`/`lyrics`/`netguard`），前端 **0%**（功能要按终端交互重新设计一遍）。 |
| 技术栈 | **Go + Bubble Tea v2 + lipgloss v2 + oto v3/beep + SQLite**。不建议换语言，换语言等于把 6k 行领域逻辑重写一遍。 |
| 音频方案 | 双层后端：**mpv（外部进程）先跑通 + 纯 Go 内嵌解码作为终极形态**。musicfox 自己也是这么做的（它同时维护 beep / mpv / mpd / DLNA 四套播放后端）。 |
| 工作量 | MVP（能登录、能放歌、有队列和歌词）**约 30 人日**；对齐现有桌面版功能**约 65–90 人日**；对标 musicfox 完整度**约 4–6 个月单人**。 |
| 最大风险 | 纯 Go 的 AAC 解码链（B 站 64k 轨是 HE-AAC，纯 Go 解码器普遍只支持 AAC-LC），其次是终端能力差异与中文输入法。 |
| 顺带发现 | 现有 `getAudioURL` 取 `dash.audio[0]`，实测拿到的是**最低码率 44kbps 那条**（`mp4a.40.5` HE-AAC）。桌面版很可能一直在放 64k 音质，这个 bug 在 TUI 版里必须修正。 |

---

## 1. 现有资产盘点

### 1.1 代码规模实测

| 区域 | 行数 | TUI 版命运 | 说明 |
| --- | ---: | --- | --- |
| `internal/services`（非测试） | 6,028 | 约 90% 复用 | 业务逻辑与 UI 无关，只依赖 GORM 和 `net/http` |
| `internal/proxy` | 874 | 内嵌引擎方案下**删除**；外部播放器方案下**保留** | 它解决的是浏览器 CORS/防盗链；Go 侧直接带 Referer/UA 请求即可 |
| `internal/models` + `internal/db` | 402 + 131 | 100% 复用 | 表结构可原样沿用，桌面版与 TUI 版共用同一个 `half-beat.db` |
| `internal/lyrics`（解析/匹配） | 631 | 100% 复用 | LRC 解析、模糊匹配、编码识别都是纯函数 |
| `internal/netguard` | 346 | 100% 复用 | SSRF 防护，抓远程图片/歌词仍需要 |
| Go 测试 | 2,668 | 100% 复用 | 领域层迁移时这批测试就是回归网 |
| `frontend/` | 19,742 | **全废弃** | 但设计资产（信息架构、交互流程、字段语义）要完整搬过去 |

### 1.2 与 Wails 的耦合点（比想象中小）

只有一处：`internal/services/service.go` 引用了 `github.com/wailsapp/wails/v2/pkg/runtime`，用于最小化/最大化/退出/托盘等 8 个窗口方法。另外 `services` 通过 `SetAudioProxy()` 持有一个代理句柄，供"下载后本地播放"和主题图片使用。

也就是说，把 window 相关的 8 个方法抽到 `internal/desktop` 后，`services` 就是一个干净的领域层，桌面版和 TUI 版可以同时挂上去。**这是整个重写计划里性价比最高的一步**，建议先做。

---

## 2. 为什么终端版不等于"少写点代码"

参考对标对象的实测数据（musicfox master，剔除 vendor）：

| 区域 | 非测试行数 | 备注 |
| --- | ---: | --- |
| `internal/ui` | 24,830（123 文件） | 含 46KB 的封面渲染器、kitty 图形协议封装、i18n、桌面歌词、登录 webview |
| `internal/player` | 5,465（23 文件） | beep / mpv / mpd / DLNA / Windows SMTC / macOS 多套后端 + 频谱 |
| `internal/macdriver` | 4,808 | macOS 系统集成（睡眠、耳机、菜单栏） |
| `internal/playlist` | 3,803 | 播放队列与歌单 |
| `internal/configs` | 2,868 | koanf + TOML 配置 |
| `internal/desktop_lyrics` | 2,714 | 桌面悬浮歌词 |
| `internal/remote_control` | 1,357 | MPRIS / DBus 远程控制 |
| 合计 | **46,856** + 9,188 测试 | |

对比你的项目：Go 8,189 行 + 前端 19,742 行。**musicfox 光 UI 层就比你现在整个前端还大**。它的体量主要花在"把桌面应用该有的东西在终端里补齐"：封面图、登录页、桌面歌词、系统媒体控制、i18n、配置体系。

所以准确的定位是：TUI 版不是"删掉 UI 剩下的就完事"，而是**换一套更笨拙的渲染介质，把原来的 UI 能力重新实现一遍，再补一个原生音频引擎**。

---

## 3. 难点排序（按真实体感，不按代码量）

### 难点 1：音频引擎（唯一"真难"的部分）

桌面版把这件事外包给了 WebKit 的 `<audio>`：解码、缓冲、seek、音量、设备切换、AAC/FLAC 支持全是免费的。终端版没有这层，必须自己解决：

- 拉流：B 站音频是 fMP4 分片，需要带 `Referer`/`User-Agent`，URL 带时效签名（现有代码里按 2 小时兜底刷新）。
- 解码：**AAC**。这是坑的源头，见第 5 节实测。
- 输出：跨平台音频设备（WASAPI / PulseAudio / CoreAudio），还要处理设备热插拔。
- 传输控制：seek、暂停后精确恢复、切歌不爆音、进度与歌词同步精度。
- DSP：现有的音量补偿（dB → 线性增益 + 软限幅）在纯 Go 里要自己写，浏览器里是 `GainNode` 一行的事。

### 难点 2：状态同步（TUI 的事件循环是一根独木桥）

Bubble Tea 是 Elm 架构：`Update` 必须快速返回，任何阻塞都会让界面卡死。而播放器天然是多线程的（解码线程、网络线程、设备回调）。音频位置每 100~200ms 要刷一次进度条，同时还要处理按键、歌词滚动、后台同步任务进度。

正确做法：音频引擎只往 channel 里发事件，由一个 `tea.Cmd` 转成 `Msg`；所有 UI 状态在 `Update` 里同步修改，禁止在回调里直接碰 model。这是架构纪律问题，不是技术难点，但写崩了会非常难查（表现是"偶尔按键没反应"）。

### 难点 3：终端能力差异

- 封面图：kitty 图形协议 / iTerm2 内联图 / sixel / 都不支持时退化成 ANSI 半块字符画。musicfox 为此写了 46KB 渲染器 + 50KB 的 kitty/tmux 封装（含 tmux 直通、限流、偏移重试），因为 tmux 会吞掉图形转义序列。
- 中文宽字符：`uniseg`/`runewidth` 截断，否则列表会错位。
- 窄终端、SSH、Windows 老 conhost、字体缺字形，都要降级路径。
- 中文输入法：Bubble Tea 的 textinput 在部分终端（尤其 tmux + 中文 IME）会出现候选框错位、吞字。搜索框建议避免"实时过滤"，改成回车提交。

### 难点 4：模态交互的退化

桌面版有十几个模态框（登录、BV 添加、歌单编辑、歌曲信息、主题管理、下载管理、设置、同步进度……）。终端里没有"窗口"，要么做成居中浮层（需要自己算层级和遮罩），要么做成整屏页面。musicfox 的选择是"菜单 + 整屏页面 + 少量弹窗"，因为浮层在终端里维护成本高。

---

## 4. 技术栈选型

### 4.1 方案对比

| 方案 | 音频 | 复用现有后端 | 生态成熟度 | 结论 |
| --- | --- | --- | --- | --- |
| **Go + Bubble Tea v2** | oto/beep 或 mpv | 65%（6k 行现成） | 高（charm 生态 + musicfox 同栈） | **推荐** |
| Rust + ratatui | symphonia + cpal（AAC-LC 支持好） | 0%，B 站 API/同步/歌词全重写 | UI 生态好，音频库强 | 除非你想顺便换成 Rust，否则不划算 |
| TypeScript + Ink | Node 音频栈弱，得靠 mpv | 0% | 一般 | 不推荐，启动慢、打包重 |
| Go + tview/tcell | 同上 | 65% | 老牌但样式能力弱 | 备选，Bubble Tea 的样式与生态更好 |

推荐 Go 的核心原因是**后端已经在那儿了**，不是语言偏好。桌面版和 TUI 版还能共用同一个 SQLite 库，用户数据互通。

### 4.2 推荐组合

```go
// UI 层
charm.land/bubbletea/v2        // v2：cell-based 渲染器、声明式 View、终端的 ProgressBar/WindowTitle
charm.land/lipgloss/v2         // 样式与布局
charm.land/bubbles/v2          // list / textinput / viewport / spinner / help / paginator
charmbracelet/x/ansi           // 转义序列、宽度裁剪

// 音频
github.com/ebitengine/oto/v3   // v3.5：Linux/macOS/Windows 均无需 cgo
github.com/gopxl/beep/v2       // 若用 beep 的 Resampler/Volume/Queue 设施
github.com/tphakala/go-aac     // 纯 Go AAC-LC 解码（LGPL-2.1，很新，见风险）
github.com/tphakala/go-m4a     // MP4/fMP4 容器解封装
github.com/edgly/mp4ff         // 备选：更成熟的 MP4 box 解析，sidx/tfdt 自己算
github.com/mewkiz/flac         // Hi-Res FLAC 轨

// 其它
github.com/skip2/go-qrcode     // 登录二维码（ToSmallString 直接贴终端）
github.com/knadh/koanf/v2      // TOML 配置（结构化，比塞进 PlayerSetting.config 干净）
github.com/godbus/dbus/v5      // MPRIS（Linux 媒体键/系统托盘媒体信息）
github.com/gen2brain/beeep     // 桌面通知
gorm.io/gorm + sqlite          // 原样保留
```

几点说明：

- **Bubble Tea v2 已经在用 `charm.land/*` 这个新模块路径**（musicfox 用 `v2.0.8`），v2 的 `View` 是结构体，可以设 `AltScreen`、`WindowTitle`、`ProgressBar`（OSC 9;4，Windows Terminal / iTerm 等支持在标题栏和任务栏显示进度）。对音乐播放器来说这两个白送的能力很实用：标题栏显示"正在播放 - 歌手"，任务栏显示进度。
- `oto` v3 已经做到三大平台免 cgo，纯 Go 交叉编译单文件分发，这对 TUI 工具的分发体验很重要。
- 现有项目是 AGPLv3，所以 LGPL（`go-aac`）甚至 LGPL 版 ffmpeg 都没有许可证障碍。

---

## 5. 音频子系统（重点，决定整个项目的难度）

### 5.1 B 站音频格式实测（2026-10，未登录）

以 `BV1GJ411x7h7` 为例，`/x/player/playurl?fnval=4048` 返回 `dash.audio` 三条轨道，按码率**升序**排列：

| id | codecs | 带宽 | 实测体积 | 说明 |
| --- | --- | ---: | --- | --- |
| 30216 | `mp4a.40.5` | 43,962 | 1,167,163 B / 213 s ≈ 44 kbps | **HE-AAC（SBR）**，纯 Go 解码器普遍不支持 |
| 30232 | `mp4a.40.2` | 102,931 | — | AAC-LC，约 132k |
| 30280 | `mp4a.40.2` | 203,786 | 实测 2,635,113 B（另一支视频） | AAC-LC，约 192k，**首选** |

`dash` 里还有 `flac` 与 `dolby` 字段，未登录时均为 `null`；登录且是大会员时会出现 FLAC（Hi-Res）和 `ec-3`（杜比）轨道，**这两类都要额外解码器**。

容器结构（抓前 128KB 解析）：

```
ftyp(iso5/dash, GPAC 0.7.2)  free  moov(mvhd, mvex{mehd,trex,trep}, trak{...esds...})
  sidx(548B)  moof{mfhd, traf{tfhd, tfdt, sbgp, trun}}  mdat ...
```

即**标准 fragmented MP4**：`moov` 在前，`sidx` 给出时间→字节偏移映射，每个分片自带 `tfdt` 时间戳。HTTP 行为实测：**支持 Range（206），不支持 HEAD（返回 404）**。这意味着"先 HEAD 探测大小"的做法会失败，但按字节范围拉流是可行的。

### 5.2 三个可选架构

| 架构 | 实现要点 | 新增代码 | 外部依赖 | 风险 | 体验 |
| --- | --- | ---: | --- | --- | --- |
| **A. 纯 Go 内嵌** | 自研/复用 fMP4 解封装 → AAC-LC 解码（`go-aac`）→ oto 输出；seek 靠 `sidx` 算偏移发 Range 请求 | 2,500–4,000 行 | 无 | **高**（新库、HE-AAC 不支持、seek 边界要调） | 最好：单文件、无感、可做淡入淡出 |
| **B. mpv 子进程** | `mpv --no-video --input-ipc-server` + JSON IPC：`loadfile` / `seek` / `set volume` / `observe time-pos` | 600–900 行 | 需装 mpv | 低 | 好：解码/seek/缓存全由 mpv 兜底，延迟略高 |
| **C. 本地网关 + 外部播放器** | 复用现有 `internal/proxy`（874 行）作 header 注入网关，把本地 http 流交给 mpv/mpd | 300–500 行 | mpv 或 mpd | 最低 | 中：多一层进程，暂停/恢复语义要映射 |

补充：C 方案顺带解决了"URL 需要 Referer"和"netguard 让远程资源可控"两件事，现有 proxy 从"给浏览器擦屁股"变成"给外部播放器喂流"，不算白写。

### 5.3 建议：先 B 后 A，不要一上来啃 A

顺序建议：

1. **第一个 spike 用 mpv**（半天到一天）。它能立刻验证"B 站音频能不能在终端里出声"，把 URL 选择、Referer、过期刷新这些边界一次性摸清。
2. **同时用 200 行实验代码验证纯 Go 路径**：用 A 方案解码 30280（AAC-LC 192k）能否出声、CPU 占用多少、seek 精度如何。
3. 根据实验结果定主路径。**我的判断是：先发 mpv 版，把交互和功能做完整；纯 Go 作为第二个迭代**，避免在解码器上调两周还没听到声音。

顺便必须做的两件事：

- **选轨策略要改**：现在 `getAudioURL` 取 `audio[0]`，实测就是最低码率那条（44kbps 的 HE-AAC）。应改为按 `bandwidth` 取最大、或按 `codecid` 优先级（`flac` > `30280` > `30232` > `30216`），并把"能不能解码"作为降级条件。**这个改动同时修了桌面版的音质 bug。**
- **探测兜底**：读 `codecs` 字段判断 `mp4a.40.2`（可纯 Go）/ `mp4a.40.5`（需外部解码器）/ `ec-3`（需 ffmpeg/mpv），不支持就自动切后端或切轨。

### 5.4 DSP：音量补偿怎么落地

现有语义是"全局默认 dB + 单曲覆盖"，`gain = 10^(dB/20)`。终端版实现：

```
线性增益 = 10^(dB/20) × 用户音量（0..1）
峰值检测 → 超过 0 dBFS 时用软限幅（tanh 或 lookahead limiter）防削波
切歌/暂停恢复 → 20~40ms 淡入淡出，消除爆音
```

用 beep 的话可以挂在 `beep.Volume` 之后，但**必须自己加限幅**，否则 +6 dB 补偿会直接破音。这部分 200~300 行，可单测（纯函数，输入/输出采样数组）。

---

## 6. 交互设计映射

| 现有桌面 UI | 终端等价物 | 工作量 |
| --- | --- | --- |
| 左侧收藏夹列表 + 中间歌曲列表 + 右侧歌词 | 三栏（侧栏可折叠）；窄屏切单栏 | M |
| 底部 PlayerBar（进度/模式/音量/补偿） | 底部状态栏 + 进度条 + 快捷键；`:` 命令面板 | M |
| 十几个模态框 | 整屏页面 + 少量居中浮层 | L |
| 主题系统（多主题、背景图、透明度） | 4~6 套调色板（lipgloss AdaptiveColor）；**丢弃背景图** | S |
| 滚动文本 / 动画 | 丢弃（终端里收益为负） | — |
| 窗口控制 / 托盘 / 拖拽区 | 丢弃（TUI 无窗口） | — |
| 扫码登录（弹窗内二维码） | 终端内渲染二维码 + "按 o 用浏览器打开" | S |
| 歌词逐行高亮 | viewport 内高亮当前行（比网页还好做） | S |
| 搜索（本地 + 远程） | `/` 本地模糊搜索；`:add BV号` 远程解析 | M |
| 收藏夹同步进度 | 后台任务 + 状态栏角标 + `:tasks` 页面 | M |
| 下载管理 | 异步任务队列 + 进度页 | M |
| 多 P 选择 | 列表内展开二级列表 | S |
| — | **新增**：MPRIS/DBus 媒体键、桌面通知、`zero play <bvid>` 子命令、`--json` headless 模式、SSH 友好 | M |

`S/M/L` = 1–3 人日 / 4–8 人日 / 10+ 人日。总体：**该丢的丢掉大约能省 15% 工作量**（主题背景图、滚动文本、窗口控制、音效动画）。

---

## 7. 代码组织建议

桌面版和 TUI 版共存，共用领域层：

```
half-beat-player/
├── cmd/
│   ├── half-beat/            # 现有 Wails 入口（保留）
│   └── zero-tui/              # TUI 入口 + CLI 子命令
├── internal/
│   ├── models/  db/  lyrics/  netguard/    # 原样复用
│   ├── core/                 # ← 由 services 抽出的领域层（无 UI、无 Wails）
│   │   ├── catalog/  library/  sync/  download/  auth/  settings/
│   ├── audio/                # 新：播放引擎
│   │   ├── engine.go         # 接口：Play/Pause/Seek/SetGain/Position/Event()
│   │   ├── mpv/              # B 方案：JSON IPC 客户端
│   │   ├── native/           # A 方案：fMP4 demux + AAC-LC/FLAC 解码 + oto
│   │   └── dsp/              # dB→gain、软限幅、淡入淡出（纯函数，可单测）
│   ├── proxy/                # 仅在 C 方案 / 本地文件播放时保留
│   ├── tui/
│   │   ├── app/              # 根 model、路由、键位表、命令面板
│   │   ├── pages/            # favorites songs queue lyrics search settings login downloads tasks
│   │   ├── components/       # list、progress、popup、toast、qrcode
│   │   └── theme/
│   ├── config/               # TOML（koanf）
│   └── desktop/              # 8 个 Wails 窗口方法挪到这里，services 不再依赖 Wails
```

两个原则：

1. **音频引擎必须先有接口，再有实现**。接口设计对了，mpv 和纯 Go 可以互换，甚至能同时存在（mpv 播放，纯 Go 兜底）。
2. **先做 `zero-tui play <bvid>` 这种 headless 子命令**。音频引擎在没有 UI 干扰的情况下最容易调通，这也是降风险最快的一步。

---

## 8. 里程碑与工时

按"一个人 + AI 辅助"的节奏估（人日）：

| 阶段 | 内容 | 人日 | 交付判据 |
| --- | --- | ---: | --- |
| M0 Spike | 抽 core（去 Wails 依赖）+ 两个音频后端各做一个验证 demo | 3–5 | 终端里能听到 192k 声音，能 seek |
| M1 骨架 | Bubble Tea 主循环、三栏布局、键位表、命令面板、进度 tick、配置加载 | 8–12 | 空壳能跑，键位顺手 |
| M2 播放闭环 | 队列、上/下一首、三种模式、音量+补偿、跳过片头尾、播放历史、断流重试 | 6–9 | 连续听 1 小时不炸 |
| M3 曲库 | 收藏夹列表、分页、本地搜索、BV 添加、多 P、歌曲编辑 | 8–12 | 能完全脱离桌面版管理曲库 |
| M4 歌词 | 同步高亮、偏移调节、多源搜索、手动导入、锁定 | 6–9 | 歌词跟得住，切歌不残留 |
| M5 登录 | 二维码渲染、轮询、会话持久化、用户信息 | 3–5 | 冷启动恢复登录 |
| M6 同步与下载 | 收藏夹双向同步、后台任务、进度页、下载/缓存管理 | 7–10 | 长任务不阻塞 UI |
| M7 打磨 | 主题、封面渲染（ANSI→kitty）、MPRIS、通知、i18n、帮助、打包/CI | 10–16 | 可分发给别人用 |
| 测试 | 领域层回归 + DSP 单测 + 音频回环 | 8–12 | — |
| **合计** | | **59–90** | **约 3–4.5 个月单人** |

分档看：

- **MVP（M0–M5，砍封面/MPRIS/i18n）**：约 25–30 人日，**单人 5–6 周**，AI 辅助可以压到 3 周左右。
- **对齐现有桌面版**：约 60–75 人日。
- **对标 musicfox（含封面、桌面歌词、i18n、多播放后端、系统集成）**：4–6 个月。

注意：AI 能显著压缩 UI 代码量（列表、表单、样式这类模板化代码），但**压缩不了音频调试和终端兼容性排查**——那部分必须一台台终端、一个个音频设备试。

---

## 9. 风险登记

| # | 风险 | 概率 | 影响 | 缓解 |
| --- | --- | --- | --- | --- |
| R1 | 纯 Go AAC 解码：`go-aac` 只做 AAC-LC，不支持 SBR/PS；库很新（2026-07 建仓） | 中 | 高 | 只选 30280 轨；mpv 兜底；先 spike 再投入；必要时换 ffmpeg 管道 |
| R2 | 会员 FLAC / 杜比 `ec-3` 轨无法纯 Go 解码 | 中 | 中 | 读 `codecs` 判定，降级到 AAC 或切后端 |
| R3 | seek 精度与歌词漂移（fMP4 `tfdt` 累积误差、URL 过期重签） | 中 | 中 | 统一以毫秒为时间基准；每 100ms 校准一次；URL 过期前主动刷新 |
| R4 | 中文输入法在 textinput 里吞字/错位 | 高 | 中 | 避免实时过滤；回车提交；提供 `$EDITOR`/参数入口 |
| R5 | 封面图：kitty 协议在 tmux/SSH 下退化 | 高 | 低 | ANSI 半块降级 + 开关（默认关） |
| R6 | 音频设备热插拔/独占导致无声 | 中 | 高 | 监听错误重建 context；状态栏提示 |
| R7 | 与桌面版同时写 SQLite | 中 | 中 | 单实例锁（flock）+ 共用 db 路径 |
| R8 | 事件循环里做阻塞操作，界面卡顿 | 中 | 中 | 音频/网络全走 channel + `tea.Cmd`；后台任务加超时 |
| R9 | 打包分发（mpv 依赖 vs 单文件） | 中 | 中 | mpv 方案在 README 写清依赖；纯 Go 方案保持单文件 |
| R10 | 中文宽字符导致列表错位 | 高 | 低 | `runewidth`/`uniseg` 统一截断工具函数 |

---

## 10. 三天 spike 验收清单

在动手写 UI 之前，先把下面 5 条跑通，任何一条失败都要重新评估方案：

1. **选轨**：改 `getAudioURL` 取 `max_by(bandwidth)`，确认拿到 `mp4a.40.2`。
2. **出声（mpv）**：`mpv --no-video --http-header-fields="Referer: https://www.bilibili.com" "$URL"` 能播、能 `seek`、能调音量，测出首播延迟和 seek 延迟。
3. **出声（纯 Go）**：`go-aac` + `oto` 解码 30280 前 30 秒，记录 CPU 占用、内存、能否无缝接续下一分片。
4. **seek 精度**：解 `sidx` 后按偏移发 Range 请求，从 60s 处开始播放，测量实际起点误差（目标 < 150ms）。
5. **DSP**：写一个 50 行测试，验证 +6 dB 补偿后不削波、切歌有 30ms 淡入。

通过后，再进入 M1。**不要先写 UI 再去解音频**——顺序反了会陷入"界面都好了就是没声音"的困境。

---

## 11. 需要你拍板的四个决策

1. **音频主路径**：mpv 优先求快，还是纯 Go 优先求零依赖？（建议：先 mpv 落地，纯 Go 作为 v2）
2. **是否共用数据库**：TUI 与桌面版共用一个 `half-beat.db`（数据互通，但需要处理并发写与配置差异），还是 TUI 独立库？（建议共用，TUI 配置单独放 TOML）
3. **完成度目标**：MVP / 对齐桌面版 / 对标 musicfox？
4. **仓库形态**：同仓库加 `cmd/zero-tui`（推荐，共享 `internal/`），还是另开仓库（会被迫复制领域层）？

---

## 附录：参考数据来源

- 本仓库：`git log` 至 `049b574`；Go 非测试 8,189 行 / 测试 2,668 行；前端 19,742 行。
- B 站接口：`/x/web-interface/view`、`/x/player/playurl?fnval=4048`（2026-10 实测，未登录），音频 Range 请求与 fMP4 box 结构均为实际抓包解析结果。
- musicfox：[仓库](https://github.com/go-musicfox/go-musicfox) master 分支源码，剔除 `vendor/` 统计；播放后端见 `internal/player/{beep_player,mpv_player,mpd_player,dlna_player}.go`，封面见 `internal/ui/cover_renderer.go` 与 `internal/ui/kitty/`。
- 依赖事实：[oto v3 README](https://github.com/ebitengine/oto)（三平台免 cgo）、[Bubble Tea v2](https://github.com/charmbracelet/bubbletea)（`charm.land/bubbletea/v2`，`View` 结构体含 AltScreen/WindowTitle/ProgressBar）、[go-aac](https://github.com/tphakala/go-aac)（纯 Go AAC-LC，LGPL-2.1，不支持 SBR/PS）。
