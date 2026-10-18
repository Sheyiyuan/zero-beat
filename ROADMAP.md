# 路线图

> 当前状态：**P0 骨架完成**（目录、契约、文档、可编译）。下一步是 M0 spike。

## 阶段

| 阶段 | 内容 | 人日 | 状态 |
| --- | --- | ---: | --- |
| P0 地基 | 仓库、领域模型、契约、文档、Makefile | 12–16 | 🟡 骨架已就位 |
| P1 音源 | B 站 source 搬迁 + 网易云 source + 双登录 + 统一搜索 | 25–35 | ⚪ 未开始 |
| P2 音频与播放语义 | decoder 抽象、引擎、混源队列、回退、预取、续签、DSP | 25–35 | ⚪ 未开始 |
| P3 TUI | 框架、键位、命令面板、全部页面 | 30–40 | ⚪ 未开始 |
| P4 功能补齐 | 歌词统一层、同步、下载、MPRIS、keyring、打包 | 25–35 | ⚪ 未开始 |
| P5 稳定与打磨 | 长跑、终端兼容矩阵、文档 | 12–18 | ⚪ 未开始 |

核心版（P0 + B 站 source + 音频 + TUI 主干 + 歌词）约 60–75 人日。

## M0 spike（动手写 UI 之前必须跑通）

1. **选轨**：B 站 `playurl?fnval=4048` 取 `max_by(bandwidth)`，确认拿到 `mp4a.40.2`。
2. **出声（mpv）**：`mpv --no-video --http-header-fields="Referer: https://www.bilibili.com" "$URL"`，测首播与 seek 延迟。
3. **出声（native）**：`go-aac` + `oto` 解 30280 前 30 秒，记录 CPU 与内存。
4. **seek 精度**：解析 `sidx` 后按偏移发 Range 请求，从 60s 起播，误差目标 < 150ms。
5. **DSP**：验证 +6 dB 补偿不削波、切歌 30ms 淡入。

任一条不通过都要回到 [docs/design/01-tui-feasibility.md](docs/design/01-tui-feasibility.md) 重新评估。

## 明确不做

- 窗口控制、托盘、拖拽区（TUI 无窗口）
- 主题背景图、滚动文字、动画（终端里收益为负）
- DLNA 投送、macOS 菜单栏集成（musicfox 做了，这里不是目标）
- 自建音源替换服务（不把"跨源替换"做成默认行为）
