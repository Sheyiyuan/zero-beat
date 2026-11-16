# 决策记录（ADR）

| 编号 | 标题 | 状态 |
| --- | --- | --- |
| [0001](0001-targets.md) | 目标平台：Android + Linux + macOS，Windows P7 候选，iOS 不做 | 接受 |
| [0002](0002-stack.md) | 技术栈：Flutter UI + Go 核心 + 本机 RPC | 接受 |
| [0003](0003-playback.md) | 播放：Go controller + Dart engine（media_kit / loudnorm） | 接受 |
| [0004](0004-single-instance.md) | 单进程、单实例与窗口生命周期 | 接受 |
| [0005](0005-sync.md) | 数据与同步：SQLite 本机，sync v2 仓库为唯一跨端契约 | 接受 |
| [0006](0006-module-boundary.md) | 模块边界：zero-beat 暴露公开 SDK，GUI 不 import internal | 接受 |
| [0007](0007-core-tui-split.md) | 核心/TUI 逻辑解耦与物理拆分 | 接受；模块迁移已落地 |
| [0008](0008-macos.md) | macOS：非沙盒共享 TUI 数据、Developer ID 公证、不做 iOS | 接受 |
| [0009](0009-android-download.md) | Android 下载：保存完整媒体流，桌面保留 ffmpeg | 接受；容器范围待验证 |

格式：状态 / 日期 / 背景 / 决策 / 后果 / 备选。新决策编号递增，不删除旧记录；被推翻时把状态改为「废弃」并链接新 ADR。
