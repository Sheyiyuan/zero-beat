package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/keymap"
)

func (m *Model) helpPopupContentHeight() int {
	// 上下各留 1 行，让弹窗仍然「悬浮」在整屏之上。
	n := m.height - 5
	if n < 1 {
		n = 1
	}
	lines := m.helpLines()
	if n > len(lines) {
		n = len(lines)
	}
	return n
}

func (m *Model) handleHelpPopupKey(key tea.Key) (tea.Model, tea.Cmd) {
	switch {
	case isCtrl(key, 'c'):
		m.shutdown()
		return m, tea.Quit
	case m.match(keymap.HelpPopup, key), m.match(keymap.HelpPage, key), key.Code == tea.KeyEscape:
		m.helpPopup = false
		return m, nil
	case m.match(keymap.Up, key):
		if m.helpPopupOffset > 0 {
			m.helpPopupOffset--
		}
		return m, nil
	case m.match(keymap.Down, key):
		max := len(m.helpLines()) - m.helpPopupContentHeight()
		if max < 0 {
			max = 0
		}
		if m.helpPopupOffset < max {
			m.helpPopupOffset++
		}
		return m, nil
	case m.match(keymap.Top, key):
		m.helpPopupOffset = 0
		return m, nil
	case m.match(keymap.Bottom, key):
		max := len(m.helpLines()) - m.helpPopupContentHeight()
		if max < 0 {
			max = 0
		}
		m.helpPopupOffset = max
		return m, nil
	case m.match(keymap.PageUp, key):
		m.helpPopupOffset -= m.helpPopupContentHeight()
		if m.helpPopupOffset < 0 {
			m.helpPopupOffset = 0
		}
		return m, nil
	case m.match(keymap.PageDown, key):
		max := len(m.helpLines()) - m.helpPopupContentHeight()
		if max < 0 {
			max = 0
		}
		m.helpPopupOffset += m.helpPopupContentHeight()
		if m.helpPopupOffset > max {
			m.helpPopupOffset = max
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) helpPopupLines() []string {
	lines := m.helpLines()
	visible := m.helpPopupContentHeight()
	start := m.helpPopupOffset
	limit := len(lines) - visible
	if limit < 0 {
		limit = 0
	}
	if start > limit {
		start = limit
	}
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > len(lines) {
		end = len(lines)
	}

	width := m.width - 4
	if width > 80 {
		width = 80
	}
	if width < 24 {
		width = max(16, m.width-2)
	}
	inner := width - 2
	out := []string{boxTop("快捷键", width)}
	for _, line := range lines[start:end] {
		out = append(out, boxLine(line, inner))
	}
	out = append(out, boxLine(st.dim.Render(m.firstKey(keymap.HelpPopup)+" / esc 关闭 · "+m.firstKey(keymap.Down)+" / "+m.firstKey(keymap.Up)+" 滚动"), inner))
	out = append(out, boxBottom(width))
	return out
}
