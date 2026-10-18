# 架构决策记录（ADR）

记录"已经定下来的事"和"为什么这么定"，避免以后重新吵一遍。

| 编号 | 决策 | 状态 |
| --- | --- | --- |
| [0001](0001-use-go.md) | 用 Go 重写，而不是 Rust | 已接受 |
| [0002](0002-tui-bubbletea-v2.md) | TUI 框架用 Bubble Tea v2 | 已接受 |
| [0003](0003-audio-decoder-abstraction.md) | 音频走解码器抽象：native 优先，mpv 兜底 | 已接受 |
| [0004](0004-track-and-tracksource.md) | 数据模型分 Track / TrackSource 两层，直链不落库 | 已接受 |
| [0005](0005-single-source-first.md) | 先单源落地，接口按多源设计，能力位驱动 UI | 已接受 |
| [0006](0006-migrate-not-rewrite.md) | 搬迁而不是重写：领域层与测试成建制搬过来 | 已接受 |
| [0007](0007-provisional-name.md) | 项目名暂用 hbp，定名后脚本改名 | 提议中 |

新增 ADR 的格式：标题、状态、背景、决策、理由、后果、替代方案。
