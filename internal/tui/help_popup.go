package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/keymap"
)

// helpPopupBoxWidth 返回快捷键弹窗的整框宽度（含左右边框）。
func (m *Model) helpPopupBoxWidth() int {
	width := m.width - 4
	if width > 80 {
		width = 80
	}
	if width < 24 {
		width = max(16, m.width-2)
	}
	return max(1, width)
}

// helpPopupTextWidth 返回弹窗正文可用宽度：去掉左右边框与 boxLine 的左侧留白。
func (m *Model) helpPopupTextWidth() int {
	return max(1, m.helpPopupBoxWidth()-3)
}

// helpContentLines 返回当前帮助界面实际渲染（已按各自宽度换行）的行。
func (m *Model) helpContentLines() []string {
	if m.helpPopup {
		return m.helpLines(m.helpPopupTextWidth())
	}
	return m.helpLines(m.width)
}

func (m *Model) helpPopupContentHeight() int {
	// 上下各留 1 行，让弹窗仍然「悬浮」在整屏之上。
	n := m.height - 5
	if n < 1 {
		n = 1
	}
	lines := m.helpLines(m.helpPopupTextWidth())
	if n > len(lines) {
		n = len(lines)
	}
	return n
}

// helpScrollState 返回当前帮助界面的滚动位置与下界：弹窗优先，其次整页。
// 两处帮助共用同一份内容与同一套滚动边界，避免键盘、滚轮各自算一套。
func (m *Model) helpScrollState() (offset *int, maxOffset int, ok bool) {
	var visible int
	switch {
	case m.helpPopup:
		offset, visible = &m.helpPopupOffset, m.helpPopupContentHeight()
	case m.view == viewHelp:
		offset, visible = &m.helpOffset, m.helpViewport()
	default:
		return nil, 0, false
	}
	return offset, max(0, len(m.helpContentLines())-visible), true
}

// scrollHelp 按行滚动当前帮助界面（正数向下），键盘与滚轮共用这个入口。
func (m *Model) scrollHelp(delta int) {
	offset, maxOffset, ok := m.helpScrollState()
	if !ok {
		return
	}
	*offset = clampInt(*offset+delta, 0, maxOffset)
}

// scrollHelpToTop/scrollHelpToBottom 跳到帮助顶部或底部。
func (m *Model) scrollHelpToTop() {
	if offset, _, ok := m.helpScrollState(); ok {
		*offset = 0
	}
}

func (m *Model) scrollHelpToBottom() {
	if offset, maxOffset, ok := m.helpScrollState(); ok {
		*offset = maxOffset
	}
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
		m.scrollHelp(-1)
		return m, nil
	case m.match(keymap.Down, key):
		m.scrollHelp(1)
		return m, nil
	case m.match(keymap.PageUp, key):
		m.scrollHelp(-m.helpPopupContentHeight())
		return m, nil
	case m.match(keymap.PageDown, key):
		m.scrollHelp(m.helpPopupContentHeight())
		return m, nil
	case m.match(keymap.Top, key):
		m.scrollHelpToTop()
		return m, nil
	case m.match(keymap.Bottom, key):
		m.scrollHelpToBottom()
		return m, nil
	}
	return m, nil
}

func (m *Model) helpPopupLines() []string {
	width := m.helpPopupBoxWidth()
	inner := width - 2
	lines := m.helpLines(m.helpPopupTextWidth())
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

	out := []string{boxTop("快捷键", width)}
	for _, line := range lines[start:end] {
		out = append(out, boxLine(line, inner))
	}
	footer := m.firstKey(keymap.HelpPopup) + " / esc 关闭 · " + m.firstKey(keymap.Down) + " / " + m.firstKey(keymap.Up) + " / 滚轮 滚动"
	for _, line := range wrapText(footer, max(1, inner-1)) {
		out = append(out, boxLine(st.dim.Render(line), inner))
	}
	out = append(out, boxBottom(width))
	return out
}
