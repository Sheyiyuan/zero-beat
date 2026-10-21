package tui

import (
	"strings"
	"unicode"
)

// maxPasteRunes 限制粘贴写入输入框的长度，避免误贴整篇文章把界面和歌单名撑坏。
const maxPasteRunes = 512

// handlePaste 把终端粘贴内容写入当前激活的输入框。
// 粘贴不是按键，不能走 handleKey；分支顺序要和 handleKey 保持一致。
func (m *Model) handlePaste(content string) {
	text := sanitizePastedText(content)
	if text == "" {
		return
	}
	switch {
	case m.palette != nil:
		m.palette.input = appendInput(m.palette.input, text)
		m.palette.idx = 0
	case m.prompt != promptNone:
		m.promptInput = appendInput(m.promptInput, text)
	case m.view == viewSearch && m.searchEditing:
		m.searchInput = appendInput(m.searchInput, text)
	}
}

// sanitizePastedText 把多行粘贴压成单行，并剔掉控制字符（包括 ESC），
// 防止粘贴内容破坏整屏渲染。
func sanitizePastedText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			// 连续空白合并成一个空格；开头不写，结尾由循环自然丢掉。
			space = b.Len() > 0
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// appendInput 在不超过 maxPasteRunes 的前提下追加粘贴文本。
func appendInput(current, add string) string {
	if add == "" {
		return current
	}
	cur := []rune(current)
	if len(cur) >= maxPasteRunes {
		return current
	}
	next := []rune(add)
	if room := maxPasteRunes - len(cur); len(next) > room {
		next = next[:room]
	}
	return current + string(next)
}
