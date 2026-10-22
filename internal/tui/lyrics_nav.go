package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/keymap"
)

// handleLyricsKey 在歌词页里先处理手动滚动，其余按键交回通用播放快捷键。
func (m *Model) handleLyricsKey(key tea.Key) (tea.Model, tea.Cmd) {
	step := max(1, m.bodyHeight()-1)
	switch {
	case m.match(keymap.Up, key):
		m.scrollLyrics(-1)
		return m, nil
	case m.match(keymap.Down, key):
		m.scrollLyrics(1)
		return m, nil
	case m.match(keymap.PageUp, key):
		m.scrollLyrics(-step)
		return m, nil
	case m.match(keymap.PageDown, key):
		m.scrollLyrics(step)
		return m, nil
	case m.match(keymap.Top, key):
		m.scrollLyricsTo(0)
		return m, nil
	case m.match(keymap.Bottom, key):
		m.scrollLyricsTo(m.lyricMaxStart())
		return m, nil
	}
	return m.handleCommonPlaybackKey(key)
}

func (m *Model) resetLyricScroll() {
	m.lyricScroll = 0
	m.lyricScrollAt = time.Time{}
}

func (m *Model) maybeResetLyricScroll(now time.Time) {
	if m.lyricScroll != 0 && !m.lyricScrollAt.IsZero() &&
		now.Sub(m.lyricScrollAt) > 5*time.Second {
		m.resetLyricScroll()
	}
}

func (m *Model) lyricMaxStart() int {
	rows, _ := m.lyricRows()
	if h := m.bodyHeight(); h > 0 && len(rows) > h {
		return len(rows) - h
	}
	return 0
}

// scrollLyrics 以当前显示位置为基准滚动 delta 行，并启动 5 秒回焦计时。
func (m *Model) scrollLyrics(delta int) {
	rows, current := m.lyricRows()
	if len(rows) == 0 {
		return
	}
	h := m.bodyHeight()
	if h <= 0 {
		return
	}
	base := current - h/2
	// 歌曲开头 / 短歌词时 base 为负（靠顶部留白实现居中）。下界必须允许这个
	// 负起点，否则一次 ↓ 会被 clamp 到 0、视觉上直接跳到顶部，↑ 也回不去。
	lo := min(0, base)
	maxStart := m.lyricMaxStart()
	start := clampInt(base+m.lyricScroll, lo, maxStart)
	start = clampInt(start+delta, lo, maxStart)
	m.lyricScroll = start - base
	m.lyricScrollAt = time.Now()
}

func (m *Model) scrollLyricsTo(start int) {
	rows, current := m.lyricRows()
	if len(rows) == 0 {
		return
	}
	h := m.bodyHeight()
	if h <= 0 {
		return
	}
	start = clampInt(start, 0, m.lyricMaxStart())
	m.lyricScroll = start - (current - h/2)
	m.lyricScrollAt = time.Now()
}
