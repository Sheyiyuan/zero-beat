# ADR-0002 TUI 框架用 Bubble Tea v2

- 状态：已接受
- 日期：2026-10-18

## 决策

主框架用 `charm.land/bubbletea/v2`，样式用 `charm.land/lipgloss/v2`；控件自绘，不引入 `bubbles`。

## 理由

1. **Elm 架构贴合播放器**：单一状态源 + 消息驱动，音频事件通过 `tea.Cmd` 转成 `Msg`，天然隔离并发。
2. **v2 的 `View` 是结构体**，可直接设置 `AltScreen`、`WindowTitle`、`Cursor`；本项目用了 `AltScreen` 与标题栏。
3. 有可对标的大型先例：musicfox 用同一套（`v2.0.8`）。

## 后果

- **Go 1.26+**。
- `Update` 必须非阻塞：任何阻塞都会让键盘失灵。音频与网络一律 channel + `tea.Cmd`。
- 浮层需要自己算层级与遮罩，因此模态框优先做成整屏页面。

## 替代方案

- `tview` / `tcell`：更老、更底层，样式能力弱，弹窗与布局要自己写更多。
- `ratatui`（Rust）：见 ADR-0001。
