# 构建与发布

## 工具链

| 组件 | 版本/要求 | 说明 |
| --- | --- | --- |
| Go | 1.26.x（对齐 zero-beat `go.mod`） | 编 `libzero.so` / `libzero.dylib`（c-shared）或 `zero-core.exe` |
| Flutter | stable，spike 时锁定具体版本 | Android / Linux / macOS / Windows |
| Android SDK | API 35（compile/target） | minSdk 23 |
| Android NDK | 26.3.x | Go c-shared 交叉编译 |
| JDK | 21 | Gradle |
| Linux 构建依赖 | clang、cmake、ninja、pkgconf、gtk3、libmpv、libayatana-appindicator3 | Flutter Linux + media_kit + 托盘 |
| macOS 构建依赖 | macOS 12+、Xcode（完整）、CocoaPods、Go 1.26 | Flutter macOS + `libzero.dylib` + 公证 |
| Windows 构建依赖 | Windows 10/11、Visual Studio 2022「Desktop development with C++」、CMake/Ninja、rustup | Flutter Windows + `smtc_windows`（Rust 插件） |

> iOS 不在构建矩阵；Windows 为 P7（见 [10-windows](10-windows.md)），v0.1 不发布；macOS 为 P1（见 [11-macos](11-macos.md)），v0.1 交付。

## 仓库布局与模块依赖

统一仓库结构：

```text
zero-beat/
├── go.work
├── zero-beat-core/          # 共享 Go module，含 sync/media/player 等公开包
├── zero-beat-tui/           # Go module，cmd/zero 与桌面/终端实现
├── zero-beat-site/          # VitePress 网站包，独立 Pages 工作流
└── zero-beat-gui/           # 目前为设计文档；后续独立应用包
    ├── app/                # 计划：Flutter，包标识 zero_beat_gui
    ├── cmd/lib/            # 计划：c-shared
    ├── cmd/exe/            # 计划：Windows sidecar
    ├── rpc/                # 计划：协议与 RemoteEngine 适配
    └── docs/
```

GUI 的 Go module 创建在 zero-beat-gui 根，路径为 github.com/sheyiyuan/zero-beat/zero-beat-gui；require 固定版本的 zero-beat-core，开发时加入根 go.work。**不在 go.mod 提交兄弟模块相对路径 replace**；core/v0.8.0 打 tag 前，go.work 用版本精确的 `replace` 指向本地 core，tag 后移除。第三方网易云依赖替换必须同步到消费模块，不能依赖其自动传递。

核心目前公开业务包；GUI Session 门面在 P0 实现。同步直接调用 zero-beat-core/sync，不复制快照、加密或合并实现。平台实现用 build tag 区分，Linux 专用 DBus 代码需排除 android。

根 make check 检查两个现有 Go module，make check-modules 验证 GOWORK=off 的独立消费。以下 GUI 命令仍是入口实现后的约定，尚不能当成已完成的构建。

## 入口

### c-shared（Linux/Android/macOS）

```go
// cmd/lib/main.go
package main

/*
#include <stdlib.h>
*/
import "C"

//export ZeroStart
func ZeroStart(cfg *C.char) *C.char { /* 防重复启动；先锁后开库，恢复队列，返回 JSON */ }

//export ZeroStop
func ZeroStop() { /* 停引擎、持久化、释放锁 */ }

//export ZeroFree
func ZeroFree(p *C.char) { /* 释放 ZeroStart 返回的 C 分配内存 */ }

func main() {}
```

返回：

```json
{"ok":true,"port":43123,"token":"…","api":1,"repo":2,"version":"0.1.0"}
```

失败时 `{"ok":false,"code":"already_running","message":"…","pid":1234}`。

`ZeroStart` 在工作 isolate 中调用，不能阻塞 UI；初始化失败须逆序清理。返回值必须为 C 分配内存，Dart 复制后调用 `ZeroFree`；不返回 Go 堆指针。`ZeroStop` 可重复调用。桌面缺省目录由 Go 统一解析；Android 显式传入沙盒根路径和下载完成处理器选项。

### sidecar（Windows）

```go
// cmd/exe/main.go
package main

func main() {
    // 解析 --port；核心按 TUI 规则确定目录，先锁后开库；随机生成 token
    // 启动成功后仅向 stdout 输出握手 JSON；诊断写 stderr，阻塞到退出信号
}
```

Flutter 启动它并读取 stdout 的握手行（`{"port":…,"token":…}`），再用同一套 RPC 连接。

## 构建 Linux

```bash
# 1) Go 核心
CGO_ENABLED=1 go -C core build -buildmode=c-shared -o ../app/linux/lib/libzero.so ./cmd/lib

# 2) Flutter
(cd app && flutter build linux --release)

# 3) 把 libzero.so 放进 bundle，设置 rpath=$ORIGIN/lib
```

- Flutter Linux 用 CMake 打包，需在 `linux/CMakeLists.txt` 里安装 `libzero.so` 并设置 `INSTALL_RPATH "$ORIGIN/lib"`；
- media_kit 在 Linux 依赖 libmpv（`media_kit_libs_linux` 会处理）；托盘依赖 libayatana-appindicator3；
- 产物用 AppImage 或 `.deb`；提供 `.desktop` 与图标（可参考 `zero-beat-tui/internal/desktop`）。

## 构建 Android

```bash
export NDK=$ANDROID_HOME/ndk/26.3.11579264
export CC=$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android23-clang
CGO_ENABLED=1 GOOS=android GOARCH=arm64 \
  go -C core build -buildmode=c-shared -o ../app/android/app/src/main/jniLibs/arm64-v8a/libzero.so ./cmd/lib

(cd app && flutter build apk --release --split-per-abi)
```

- 模拟器另编 `x86_64`；
- Android 15 要求 16 KB 页对齐，链接参数需包含 `-Wl,-z,max-page-size=16384`（spike 时用 `zipalign`/`check_elf_alignment` 验证）；
- `libzero.so` 与 media_kit 的 `.so` 一起进 APK/AAB；
- Android 下载注入完整媒体流落盘处理器，不打包/执行 ffmpeg；容器支持矩阵与无 ffmpeg 离线测试结果随版本记录（[ADR-0009](decisions/0009-android-download.md)）；
- 签名用 upload keystore；Play 用 AAB，侧载用 APK。

## 构建 Windows（P7）

```powershell
# 1) Go 核心：纯 Go，无 cgo，可在 Linux 交叉编译
$env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go -C core build -trimpath -ldflags "-s -w" -o ../app/windows/runner/zero-core.exe ./cmd/exe

# 2) Flutter Windows（必须在 Windows 上）
cd app
flutter build windows --release
```

- Flutter Windows 需要 Visual Studio 2022 的 C++ 桌面工作负载；Linux 不能交叉构建；
- 不用 Go DLL/MinGW：`zero-core.exe` 纯 Go，避免 cgo ABI 与运行库问题；
- `smtc_windows` 需要构建机有 rustup；CI 里显式安装；
- 安装包用 MSIX（`msix` 包）或 Inno Setup；代码签名可选；
- 下载功能依赖 ffmpeg：随包附带、检测系统 ffmpeg，或暂时禁用（[10-windows](10-windows.md)）。

## 构建 macOS（P1）

```bash
# 1) Go 核心：arm64（Intel 另编 amd64 后 lipo）
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
  go -C core build -buildmode=c-shared -o ../app/macos/Frameworks/libzero.dylib ./cmd/lib
install_name_tool -id @rpath/libzero.dylib app/macos/Frameworks/libzero.dylib

# 2) Flutter
(cd app && flutter build macos --release)

# 3) 签名、公证、装订
codesign --force --options runtime --sign "Developer ID Application: …" \
  app/build/macos/Build/Products/Release/zero-beat.app/Contents/Frameworks/libzero.dylib
codesign --force --options runtime --sign "Developer ID Application: …" \
  app/build/macos/Build/Products/Release/zero-beat.app
xcrun notarytool submit zero-beat.dmg --keychain-profile … --wait
xcrun stapler staple zero-beat.dmg
```

- Xcode 工程把 `libzero.dylib` 放进 **Frameworks and Libraries + Embed Frameworks**，rpath 指到 `@rpath`；
- **关闭 App Sandbox**（`macos/Runner/*.entitlements` 里 `com.apple.security.app-sandbox` = false），否则数据目录会被重定向到容器，无法与 TUI 共用；保留 Hardened Runtime；
- `macos/Podfile` 的 `platform :osx, '12.0'` 与最低版本一致；
- 关闭窗口隐藏而不是退出：`applicationShouldTerminateAfterLastWindowClosed` 返回 false，配合 `window_manager`；
- 通用包：分别编 `arm64`/`amd64` 的 dylib 后 `lipo -create`，Flutter App 也要按 universal 构建；
- 签名/公证需要 Apple Developer 账号；未公证的构建只适合本机自用；
- 下载依赖 ffmpeg：检测 Homebrew/系统 ffmpeg，没有时禁用下载。

## Flutter 依赖（预期）

| 依赖 | 用途 |
| --- | --- |
| `media_kit` + `media_kit_libs_*` | 播放（libmpv；Windows 也有对应 libs） |
| `audio_service` / `audio_session` | Android 通知、锁屏、焦点；macOS 的 Now Playing/控制中心 |
| `smtc_windows` | Windows SMTC（媒体键、系统媒体浮层） |
| `window_manager` / `tray_manager` | 桌面窗口与小窗、托盘 |
| `path_provider` | Android 数据目录 |
| `dart:io Socket` | TCP NDJSON RPC，与 02-rpc 的传输约定一致；不使用 WebSocket 帧 |
| `flutter_riverpod` / `bloc` | 状态管理（实现时定） |
| `dynamic_library` / `dart:ffi` | 加载 `libzero.so` / `libzero.dylib` 并调用 `ZeroStart`（Windows 不需要） |
| `local_notifier` | Windows 通知（可选） |

注意：插件最低 Flutter/Dart 版本在 P1 按实际选定版本核对并锁定。不引入 `flutter_ffmpeg`；各端播放不依赖 ffmpeg 可执行文件，桌面下载仍依赖它，Android 下载使用完整媒体流处理器。

## Android Manifest

- 权限：`INTERNET`、`FOREGROUND_SERVICE`、`FOREGROUND_SERVICE_MEDIA_PLAYBACK`、`POST_NOTIFICATIONS`、`WAKE_LOCK`；
- `audio_service` 的 Service/Receiver 声明；
- `android:launchMode="singleTask"`；
- `minSdk 23`、`targetSdk 35`。

## CI

| Job | 内容 |
| --- | --- |
| Linux | Go 单测 + Dart/RPC 契约测试 + Android APK/AAB + Linux AppImage/deb |
| macOS（P1） | `libzero.dylib` + Flutter macOS + 签名/公证 + DMG（需要证书与 Apple ID secrets） |
| Windows（P7） | Flutter Windows 构建 + `zero-core.exe`（Linux 交叉编或 Windows 编）+ MSIX |

- 缓存 Go module 与 Flutter SDK；
- P0 即加入核心依赖树/目标构建检查；Windows 的纯 Go 核心检查不代表提前交付 Flutter Windows；
- Session/RPC 回归覆盖旧结果、播放代次、终态去重、最多一次重试、双向调用和断线恢复；
- P5 加入 v2 字节样本、未知字段写前保护与双后端树/祖先/恢复测试；
- macOS job 用 `macos-14`/`macos-15`，安装 Xcode/CocoaPods；
- Windows job 需要 Visual Studio Build Tools、rustup（SMTC）；
- Android 模拟器冒烟（启动、列歌单、播放 30 秒）可选，先手动；
- Android 真机锁屏 30 分钟及无 ffmpeg 下载/离线验证为阶段必需验收，记录结果；模拟器冒烟不能替代它们；
- 不建 iOS job。

## 版本与兼容

- App 版本：语义化版本，独立于 zero-beat；
- 核心版本：随依赖的 zero-beat tag；
- `api`：RPC 协议版本，破坏性变更才升；
- `repo`：同步仓库版本，必须与 zero-beat 的 `meta.v` 对齐；
- 升级 zero-beat 依赖时，跑「同一仓库双端同步」集成测试。

## 发布物

| 产物 | 内容 |
| --- | --- |
| Android APK/AAB | arm64-v8a（+ x86_64 可选） |
| Linux AppImage / deb | Flutter bundle + libzero.so + .desktop/图标 |
| macOS DMG（P1） | 签名/公证后的 .app + libzero.dylib |
| Windows MSIX / zip（P7） | Flutter bundle + zero-core.exe（+ 可选 ffmpeg） |
| 校验和 | SHA256 |
| 源码 | 本仓库 + 依赖的 zero-beat tag（AGPL 要求） |

## 许可证

AGPL-3.0。Go 核心以 c-shared（Linux/Android/macOS）或 sidecar（Windows）随 App 分发，整个应用按 AGPL 分发；「关于」页放源码链接与许可证。Android 商店（Play/F-Droid）与 AGPL 兼容；macOS 不走 Mac App Store（AGPL 与商店条款冲突），iOS 不在范围内。
