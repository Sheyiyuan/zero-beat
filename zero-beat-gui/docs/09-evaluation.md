# 选型评估

记录 2026-11 的选型过程，便于以后回看「当时为什么这么定」。

## 问题

为 zero-beat 做一个图形客户端：

- **主目标 Android**，顺带 Linux 与 macOS 桌面；
- iOS 明确不做；Windows 最初不做，后在 GUI 前提下重新评估为 P7 候选；
- 和 TUI 共用数据与同步仓库；
- 移动端功能优先，系统组件（通知、锁屏、后台、媒体键）能适配最好；
- 自动响度均衡「最好有」。

## 候选

| 方案 | UI | 业务/数据 | 播放 |
| --- | --- | --- | --- |
| A. Wails v3 | Web（HTML/CSS/JS） | Go，进程内，自动 bindings | Go（oto）或 WebView |
| B. Flutter + Go 核心 | Flutter/Dart | Go 核心，c-shared + RPC | Dart（media_kit） |
| C. 纯 Flutter/Dart | Flutter/Dart | Dart 重写 | Dart（media_kit） |

## 先修正三个直觉

**1. 「Dart 的 UI 成本比 Wails 高」不成立。**
GUI 的视图层和 TUI 没有复用可能，两边都要重写；Wails 省的从来不是视图，而是视图下面约 30k 行的业务与数据层。

**2. 「Wails 能复用 Go，所以移动端更省」也被高估。**
Android 上两条路都必须把 Go 核心编进 App（Wails 是 `libwails.so`，Flutter 是 `libzero.so`），都要处理 NDK、cgo、数据目录、SQLite、git 替代。真正的差别只有两处：**绑定方式**（自动 bindings vs RPC）和**谁放音频**。

**3. 「直接 import zero-beat 的 internal 包」不可行。**
GUI 当前 module 路径不能导入 zero-beat 的 `internal/`；需先暴露公开 `session` / `sync`（[ADR-0006](decisions/0006-module-boundary.md)）。P0 的最小 SDK、Session 与核心依赖解耦合计 3–4 人周；仅加门面不会自动移除 oto/Bubble Tea。

## Windows 重新评估（2026-11-16）

最初把 Windows 排除，主要理由是终端难适配。GUI 之后这个理由消失：

- Flutter Windows 是官方目标，`window_manager` / `tray_manager` 支持窗口与托盘；
- media_kit 支持 Windows 音频与 HTTP headers，loudnorm 仍走 libmpv；
- 系统媒体用 `smtc_windows`（SMTC）替代 MPRIS/audio_service，媒体键、锁屏、时间轴都有；
- Go 核心可以用 `zero-core.exe` sidecar（纯 Go，Linux 交叉编译），避开 MinGW DLL；
- 数据/锁与 TUI zip 共用 `%APPDATA%\zero-beat`。

代价：

- **必须有 Windows 构建机**（Flutter Windows 需要 VS2022，Linux 不能交叉构建）；
- `smtc_windows` 需要 rustup；
- 下载仍依赖 ffmpeg；
- 安装包/签名、Win10/11/DPI/休眠 QA。

估算 5–8 人周（不含 ffmpeg 下载），因此作为 P7 排在 v0.1 之后，而不是塞进 v1。细节见 [10-windows](10-windows.md)。

## macOS 重估（2026-11-16）

最初把 macOS 列为「需要 Mac 构建，暂缓」；现在已有 macOS + Xcode 构建环境，且 TUI 官方支持 macOS，GUI 应该把桌面补齐：

- Flutter macOS 是官方稳定目标；media_kit 支持 macOS（libmpv/CoreAudio）；
- 系统媒体不需要 MPRIS：`audio_service` 支持 macOS，提供 Control Center/Now Playing、耳机键、封面、进度；
- Go 核心用 `libzero.dylib`（c-shared）嵌入，单进程，与 Linux/Android 同一入口；
- **数据共享要求关闭 App Sandbox**：沙盒路径会变成容器目录，无法与 TUI 的 `~/Library/Application Support/zero-beat` 共用；
- 分发走 Developer ID 签名 + 公证 + DMG，不走 Mac App Store（AGPL 条款冲突）；iOS 仍然不做，和 macOS 支持是两件事。

代价：约 3–5 人周（dylib 嵌入/签名/公证、菜单栏/关窗隐藏/Dock、Now Playing、QA），需要 Apple Developer 账号。结论：macOS 纳入 v0.1（P1），不单列阶段，放在桌面打磨里。

## Wails v3 的现状

- 截至 2026-11，Wails v3.1.0 已发布，周更；移动端文档齐全：[Mobile Overview](https://github.com/wailsapp/wails/blob/master/docs/mpress/content/guides/mobile/index.md)；
- Android：`WebView` + `WebViewAssetLoader`，Go 编成 `libwails.so`，**可以在 Linux 上构建**；
- iOS：`WKWebView` + UIKit，**必须 macOS + Xcode**（本项目不做 iOS，这条不影响）；
- Android/iOS 文档都标注 **Experimental**；
- 支持 bindings、events、对话框、剪贴板、生命周期、文件选择；**没有媒体通知/锁屏/音频焦点的现成能力**，要自己写 Kotlin/Java 并桥接。

对本项目，Wails 的吸引力是「一套 Go 代码 + 桌面 Web UI」，但：

- Android 上要么让 Go 放音（不能 exec ffmpeg，需要纯 Go 解码或链 libav；loudnorm 还要 libavfilter），要么让 WebView 的 `<audio>` 放音（后台与锁屏最弱）；
- 媒体通知/锁屏/焦点在 Wails 里没有生态，得手写原生层；
- 这两块加起来，抵消了自动 bindings 省下的 3–4 周。

结论：**Wails 仍可作为桌面 Web UI 的备选，但不是移动端最优解。**

## 为什么不是纯 Dart

看起来最干净，但要把 30k 行 Go 重写成 Dart：

- B 站 WBI 签名、网易云 weapi/eapi；
- 同步快照的确定性序列化、Argon2id + XChaCha20、accounts LWW、冲突合并；
- 本地库内容指纹（跨平台已知答案）、SQLite 迁移、混合歌单语义；
- 下载落盘、歌词绑定、会话过期冻结。

这些不是「换库」能解决的，是行为契约。重写 4–6 个月，而且从此和 TUI 双线维护、必然漂移。**放弃。**

## 为什么是 Flutter + Go 核心 + Dart 播放

1. **Android 系统组件**：`audio_service` + `audio_session` 提供通知、锁屏、蓝牙/耳机、音频焦点、前台服务；这是 Wails 没有的生态。
2. **loudnorm 可以保留**：media_kit 基于 libmpv，能挂 `af=loudnorm=I=-16:TP=-1.5:LRA=11`，与 TUI 同一套 libavfilter；`just_audio`（ExoPlayer）只有增益、没有 EBU R128，所以不用 just_audio。
3. **Go 继续当大脑**：复用队列、模式、`BeginPlay/CompletePlay`、`HandleEvent`、`Tick` 与持久化；session 保留请求号和重试预算，Dart 引擎事件带播放代次。现有测试仍需补跨 RPC 的乱序/断线场景。
4. **数据不漂移**：SQLite 与 sync 仓库仍由 Go 管；Dart 不直接读库、不写仓库。
5. **桌面顺带**：Flutter Linux/macOS 用同一套 UI；Linux MPRIS、macOS Now Playing；托盘/小窗用 Flutter 插件。
6. **单进程**：关窗后小窗/隐藏继续播放要求进程活着，而 Dart 的音频引擎本来就在这个进程里；Windows 是唯一 sidecar 例外。

## 工作与风险对比

| 工作 | Wails v3 | Flutter + Go |
| --- | --- | --- |
| 响应式 UI | Web 页面与交互重写 | Flutter 页面与交互重写 |
| Go 上 Android | 平台依赖解耦、SQLite、数据路径与同步后端 | 同样需要；公开门面不替代依赖解耦 |
| 语言边界 | 自动 bindings；仍需异步/生命周期契约 | SDK + 双向 RPC + DTO + 请求/播放代次 |
| 播放 | libav/纯 Go 或原生播放桥接 | media_kit + Go controller；loudnorm/后台需实测 |
| 系统组件 | 自写原生媒体集成 | audio_service/audio_session 集成与真机验证 |
| Android 下载 | 现有外部 ffmpeg 链路不能直接复用 | 同样需要完成处理器；v0.1 保存完整媒体流 |
| 同步 | 共享 Go 快照与生产 Git 后端 | 同样需要；包括分叉历史、冲突和恢复 |
| 桌面分发 | 各平台构建、系统媒体、签名与安装 | 各平台构建、系统媒体、签名与安装 |

主要差别在风险落点：Wails 落在「实验性移动宿主 + 原生媒体」，Flutter 落在「公开 SDK + 桥接 + 会话编排」。移动端是主目标，选择 Flutter，但不据此承诺桌面或手机集成无需额外工作。

实施估算统一见 [roadmap](07-roadmap.md)：当前 v0.1 为 **21–29 人周**，含逻辑解耦、原型、Android 下载与完整同步；Windows 另加 5–8 人周。原型通过后重估，不将本表工作项与阶段预算重复相加。

## 同步的「兜底」

这里的「独立同步」有两种理解，结论不同：

| 理解 | 结论 |
| --- | --- |
| GUI 自己带同步能力，不依赖 TUI 是否安装 | **本来就该这样**：sync 在 Go 核心里，GUI 启动即可用 |
| **TUI/GUI 都不依赖系统 git** | **TUI 前置实现**：zero-beat-core/sync 提供 go-git 与 CLI；Linux/macOS 的 auto 优先可用 CLI，Windows/Android 优先 go-git，GUI 接入时复用 |
| 把 zero-beat 的 `zero-beat-core/sync` 拷进 GUI 仓库 | **不建议**：格式/加密/合并是字节级契约，拷贝必然漂移；而且不解决 `internal/` 边界 |

实施顺序是最小 SDK（P0）→ 独立同步原型（P1）→ 生产双后端（P5）。失败时可降级为只读查看快照或延后同步，不复制 sync 代码；只读查看不能算双向同步验收通过。实体三方合并、双父提交与冲突恢复必须共用，并做 CLI/go-git 一致性测试（[ADR-0005](decisions/0005-sync.md)）。

## 什么情况下会改结论

| 触发条件 | 备选 |
| --- | --- |
| media_kit 后台播放无法稳定 30 分钟 | Flutter + ExoPlayer（just_audio），移动端放弃 loudnorm，或改用 Media3 + 自写 loudnorm 前置 |
| Flutter Linux 中文输入法不可用 | 桌面端改 Wails/Web 前端，移动端仍 Flutter（两套 UI，成本上升） |
| Go c-shared 在 Android 无法稳定运行 | 改 gomobile bind，或桌面 daemon 化 |
| go-git 无法满足同步（fetch/push 都成问题） | 延后手机写同步或评估 libgit2；可只读查看已有快照，但没有传输能力就不能声称能自动获取远端 |
| Android 完整媒体流下载不能满足必需音源 | 重估原生/libav 封装后端；若改为不提供离线播放，必须同步修改目标与验收 |
| 没有 Windows 构建机 | Windows 保持在 P7 候选，不排期；或只做 CI 构建 |
| 没有 Apple Developer 账号/公证不可用 | macOS 只做本机签名，暂不对外分发 DMG |
| audio_service 的 macOS 后端不稳定 | 写小型 Swift Now Playing 插件，或 macOS 系统媒体延后 |
| 不能接受关闭 App Sandbox | 改为只读导出/导入，或放弃与 TUI 共用数据（不推荐） |
| `smtc_windows` 不可用 | 写小型 C++ 原生插件，或 Windows 暂不做系统媒体 |
| 团队更熟 Web/TypeScript 且不介意移动端实验性 | Wails v3 |

任何一条触发都要更新 ADR，而不是悄悄加依赖。
