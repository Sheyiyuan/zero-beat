# macOS

- 状态：**P1**（v0.1 交付），与 Linux 共用桌面 UI
- 前提：已有 macOS + Xcode 构建环境；**iOS 仍然不做**

## 目标

- macOS 12+（先 Apple Silicon，Intel 通用包后续）；
- 曲库、搜索、播放、歌词、登录、下载、同步与 Android/Linux 对齐；
- 与 TUI 共用数据目录和实例锁；
- 菜单栏图标、迷你窗口、Dock 行为、系统「正在播放」/控制中心/媒体键；
- DMG 分发，Developer ID 签名 + 公证（不走 Mac App Store）。

## 数据与非沙盒

- 数据目录：`~/Library/Application Support/zero-beat`（`os.UserConfigDir`），与 TUI 完全相同；
- **必须关闭 App Sandbox**：沙盒应用的路径会变成 `~/Library/Containers/<bundle-id>/...`，与 TUI 的目录不互通；
- 关闭沙盒后仍可启用 Hardened Runtime 并公证；分发渠道是 DMG，不是 Mac App Store（AGPL 与商店条款也不匹配）；
- 代价：没有商店的分发便利；首次安装需要 Developer ID 公证，否则 Gatekeeper 会拦。

## 构建

| 项 | 方案 |
| --- | --- |
| Go 核心 | `c-shared` → `libzero.dylib`，`CGO_ENABLED=1`，与 Linux/Android 同一入口 |
| Flutter | `flutter build macos --release`；Xcode + CocoaPods |
| 架构 | 先 arm64；需要 Intel 时分别编 arm64/amd64 再 `lipo` 成通用 dylib |
| 嵌入 | dylib 放进 `Contents/Frameworks`，install_name 设为 `@rpath/libzero.dylib` |
| 签名 | `codesign --options runtime` 签名 App 与 dylib；`notarytool` 公证；`stapler` 装订 |
| 最低版本 | `macOS 12.0`（Flutter/Podfile 同步） |
| 本地布局 | 统一仓库内 zero-beat-core / zero-beat-tui / zero-beat-gui；根 go.work 联调，固定 module 版本发布 |

## 系统集成

- **播放**：media_kit（libmpv）+ CoreAudio，loudnorm 与桌面一致；
- **系统媒体**：[`audio_service`](https://pub.dev/packages/audio_service) 支持 macOS，提供 Control Center / Now Playing、耳机键、封面、进度；锁屏控制 macOS 没有对应形态；
- **媒体键**：由 Now Playing 会话接管，不额外抓键；
- **菜单栏/托盘**：`tray_manager` 的 macOS 后端；菜单项与 Linux 对齐；
- **窗口**：`window_manager`；关闭主窗口 = 隐藏并继续播放（macOS 常规行为），Cmd+Q 才退出；
- **Dock**：点击 Dock 图标恢复窗口；不接受「关窗即退」；
- **休眠/唤醒**：由 CoreAudio/mpv 处理，唤醒后校准位置；
- **音频设备切换**：跟随系统默认输出，不锁设备。

## 下载与本地库

- 非沙盒后可以直接读写用户选择的目录；
- 下载仍依赖 ffmpeg：优先检测 Homebrew/系统 ffmpeg，没有则禁用下载并提示，或随包附带；
- 本地文件夹歌单可直接扫描，不需要 SAF 之类的桥。

## 实例锁与二次启动

- macOS 用 `flock`，与 TUI 共用 `instance.lock`；GUI 在后台/托盘期间继续持有；
- 二次启动：尝试连接 `gui.sock` 激活已有实例，失败则提示「已在运行（PID x）」；
- Android 不需要锁；Windows 用 `LockFileEx`。

## 分发

- Developer ID Application 证书 + App 专用密码（notarytool）；
- 产物：`.app` → 签名 → 公证 → 装订 → DMG；
- 未公证的构建仅限本机/自用，外发会被 Gatekeeper 拦；
- 自动更新（Sparkle）后置，不在 v0.1；
- AGPL：DMG 附带源码链接与许可证。

## CI

- `macos-14`/`macos-15` runner 构建 App + 公证（需要证书与 Apple ID secrets）；
- Go 核心可在 macOS runner 上直接编 dylib，或在 Linux 交叉编 arm64/amd64 后合并；
- 不建 iOS job。

## 测试矩阵

| 项 | 覆盖 |
| --- | --- |
| 系统 | macOS 13 / 14 / 15，Apple Silicon（Intel 可选） |
| 媒体 | 控制中心、Now Playing、媒体键、蓝牙耳机、音量 |
| 窗口 | 关窗隐藏、Dock 恢复、迷你窗口、菜单栏、多显示器 |
| 数据 | 与 TUI 共用 `~/Library/Application Support/zero-beat`、互斥、队列恢复 |
| 同步 | git CLI / go-git、与 Linux TUI 双端同步、冲突页 |
| 分发 | 公证、装订、Gatekeeper 首次打开、卸载残留 |
| 性能 | 冷启动、1000+ 曲目滚动、休眠唤醒 |

## 估算

| 工作 | 人周 |
| --- | --- |
| dylib 嵌入、签名、公证、DMG | 1–1.5 |
| 菜单栏/窗口/Dock/迷你窗口 | 1 |
| audio_service macOS（Now Playing/媒体键） | 0.5–1 |
| 测试与修平台差异 | 0.5–1.5 |
| **合计** | **3–5** |

## 风险

- **公证需要 Apple Developer 账号**（付费）；没有账号只能本机/自签，外发体验差；
- **关闭沙盒**降低隔离性，需要说明并只从官方渠道分发；
- **通用二进制**（Intel + ARM）会增加构建与签名复杂度，v0.1 可以先 arm64；
- **audio_service 的 macOS 维护**不如 Android 活跃，媒体键要实测；
- **Flutter macOS 关窗默认行为**要显式改成隐藏，否则会退出应用。
