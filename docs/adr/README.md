# 架构决策记录（ADR）

记录「已经定下来的事」和「为什么这么定」，避免以后重新吵一遍。

| 编号 | 决策 | 状态 |
| --- | --- | --- |
| [0001](0001-use-go.md) | 用 Go 重写，而不是 Rust | 已接受 |
| [0002](0002-tui-bubbletea-v2.md) | TUI 框架用 Bubble Tea v2 | 已接受 |
| [0003](0003-audio-decoder-abstraction.md) | 音频走解码器抽象：native 优先，mpv 兜底 | 被 0008 取代 |
| [0004](0004-track-and-tracksource.md) | 数据模型分 Track / TrackSource 两层，直链不落库 | 部分实现（未拆表） |
| [0005](0005-single-source-first.md) | 先单源落地，接口按多源设计，能力位驱动 UI | 已由 0010 完成 |
| [0006](0006-migrate-not-rewrite.md) | 搬迁而不是重写：领域层与测试成建制搬过来 | 推迟 |
| [0007](0007-project-name.md) | 项目命名：仓库 `zero-beat`，命令 `zero` | 已接受 |
| [0008](0008-ffmpeg-first.md) | 首版音频用 ffmpeg 外部解码，而不是纯 Go | 已接受 |
| [0009](0009-concrete-first.md) | 先写具体实现，第二个音源出现时再抽 Source 接口 | 被 0010 取代 |
| [0010](0010-multi-source.md) | 接入网易云：补齐 source.Source，歌曲改源无关标识 | 已接受 |
| [0011](0011-mixin-playlists.md) | 混合歌单用引用+排除，不复制源曲目；同步歌单可解锁为本地 | 已接受 |
| [0012](0012-configurable-keymap.md) | 快捷键改为动作 ID + TOML 覆盖，冲突回退默认 | 已接受 |
| [0013](0013-clip-and-cloud-write.md) | 片段存 settings + 引擎 `-ss/-t`；云端写入走可选 PlaylistWriter；混入只加引用 | 已接受 |
| [0014](0014-platform-metadata-and-app-service.md) | 平台元数据下沉适配器，业务抽到 `internal/app` 服务 | 已接受 |

新增 ADR 的格式：标题、状态、背景、决策、理由、后果、替代方案。
