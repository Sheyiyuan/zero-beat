package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/keymap"
)

// 歌词页交互：中央一行固定为焦点行。
//
//   - 跟随态：焦点 = 正在播放的行，播放推进时自动滚动；
//   - 浏览态：↑/↓、翻页、滚轮、单击移动焦点，播放继续但不再抢焦点；
//   - Enter 跳到焦点行并回到跟随，Esc 回到播放行；
//   - 双击歌词行 = 移动焦点并跳转。
func (m *Model) handleLyricsKey(key tea.Key) (tea.Model, tea.Cmd) {
	step := max(1, m.bodyHeight()/2)
	switch {
	case m.match(keymap.Up, key):
		m.moveLyricFocus(-1)
		return m, nil
	case m.match(keymap.Down, key):
		m.moveLyricFocus(1)
		return m, nil
	case m.match(keymap.PageUp, key):
		m.moveLyricFocus(-step)
		return m, nil
	case m.match(keymap.PageDown, key):
		m.moveLyricFocus(step)
		return m, nil
	case m.match(keymap.Top, key):
		m.focusLyricLine(0)
		return m, nil
	case m.match(keymap.Bottom, key):
		m.focusLyricLine(len(m.lyricLines) - 1)
		return m, nil
	case key.Code == tea.KeyEnter:
		return m, m.commitLyricFocus()
	}
	return m.handleCommonPlaybackKey(key)
}

// lyricBrowsing 判断当前是否在浏览态（焦点停在非播放行）。
func (m *Model) lyricBrowsing() bool {
	return m.lyricFocus >= 0 && m.lyricFocus != m.lyricIdx
}

// followLyricLine 让焦点回到正在播放的行（切歌、Esc、重新加载歌词）。
func (m *Model) followLyricLine() {
	m.lyricFollow = true
	m.lyricFocus = m.lyricIdx
}

// moveLyricFocus 在浏览态里移动焦点；从跟随态开始移动时以播放行为基准。
func (m *Model) moveLyricFocus(delta int) {
	if len(m.lyricLines) == 0 {
		return
	}
	base := m.lyricFocus
	if base < 0 || base >= len(m.lyricLines) {
		base = m.lyricIdx
	}
	if base < 0 {
		base = 0
	}
	m.lyricFollow = false
	m.lyricFocus = clampInt(base+delta, 0, len(m.lyricLines)-1)
}

// focusLyricLine 把焦点移到指定行（单击歌词行、g/G）。
func (m *Model) focusLyricLine(line int) {
	if len(m.lyricLines) == 0 {
		return
	}
	m.lyricFollow = false
	m.lyricFocus = clampInt(line, 0, len(m.lyricLines)-1)
}

// commitLyricFocus 跳到焦点行并恢复跟随（Enter / 双击）。
func (m *Model) commitLyricFocus() tea.Cmd {
	line, ok := m.focusedLyricLine()
	if !ok {
		return nil
	}
	m.lyricFollow = true
	return m.seekTo(m.lyricStart(line))
}

// lyricFocusLine 返回当前焦点行号；焦点未设置时回落到正在播放的行。
func (m *Model) lyricFocusLine() int {
	if m.lyricFocus >= 0 && m.lyricFocus < len(m.lyricLines) {
		return m.lyricFocus
	}
	if m.lyricIdx >= 0 && m.lyricIdx < len(m.lyricLines) {
		return m.lyricIdx
	}
	return 0
}

// focusedLyricLine 返回焦点行与其起始时间；没有歌词时 ok=false。
func (m *Model) focusedLyricLine() (int, bool) {
	if len(m.lyricLines) == 0 {
		return 0, false
	}
	line := m.lyricFocusLine()
	return line, true
}

// lyricStart 返回某句在音轨上的起始时间（已包含歌词偏移）。
func (m *Model) lyricStart(line int) time.Duration {
	return time.Duration(m.lyricLines[line].StartMS) * time.Millisecond
}
