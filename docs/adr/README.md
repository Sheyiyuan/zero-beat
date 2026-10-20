# 架构决策记录（ADR）

记录"已经定下来的事"和"为什么这么定"，避免以后重新吵一遍。

| 编号 | 决策 | 状态 |
| --- | --- | --- |
| [0001](0001-use-go.md) | 用 Go 重写，而不是 Rust | 已接受 |
| [0002](0002-tui-bubbletea-v2.md) | TUI 框架用 Bubble Tea v2 | 已接受 |
| [0003](0003-audio-decoder-abstraction.md) | 音频走解码器抽象：native 优先，mpv 兜底 | 被 0008 取代 |
| [0004](0004-track-and-tracksource.md) | 数据模型分 Track / TrackSource 两层，直链不落库 | 部分实现 |
| [0005](0005-single-source-first.md) | 先单源落地，接口按多源设计，能力位驱动 UI | 已接受（接口推迟） |
| [0006](0006-migrate-not-rewrite.md) | 搬迁而不是重写：领域层与测试成建制搬过来 | 推迟 |
| [0007](0007-project-name.md) | 项目命名：仓库 `zero-beat`，命令 `zero` | 已接受 |
| [0008](0008-ffmpeg-first.md) | 首版音频用 ffmpeg 外部解码，而不是纯 Go | 已接受 |
| [0009](0009-concrete-first.md) | 先写具体实现，第二个音源出现时再抽 Source 接口 | 已接受 |

新增 ADR 的格式：标题、状态、背景、决策、理由、后果、替代方案。
