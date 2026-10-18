// Package tui 是终端界面层，基于 Bubble Tea v2。
//
// 约束：
//   - Update 必须非阻塞，音频与网络事件一律走 channel + tea.Cmd；
//   - 控件按音源的 Capability 显隐，禁止按平台名分支；
//   - 终端宽字符用 runewidth/uniseg 统一截断。
//
// 页面规划见 docs/design/01-tui-feasibility.md 第 6 节。
package tui
