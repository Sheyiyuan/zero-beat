package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

func TestPasteIntoSearchInput(t *testing.T) {
	m := fixtureModel()
	m.openSourceSearch()

	m.Update(tea.PasteMsg{Content: "周杰伦\n晴天"})

	if m.searchInput != "周杰伦 晴天" {
		t.Fatalf("搜索框粘贴结果不符: %q", m.searchInput)
	}
}

func TestPasteIntoPromptInput(t *testing.T) {
	m := fixtureModel()
	m.beginPrompt(promptNewPlaylist)

	m.Update(tea.PasteMsg{Content: "我的歌单"})

	if m.promptInput != "我的歌单" {
		t.Fatalf("提示框粘贴结果不符: %q", m.promptInput)
	}
}

func TestPasteIntoPaletteInput(t *testing.T) {
	m := fixtureModel()
	m.openPalette()
	m.palette.idx = 3

	m.Update(tea.PasteMsg{Content: "网易云"})

	if m.palette == nil || m.palette.input != "网易云" {
		t.Fatalf("命令面板粘贴结果不符: %+v", m.palette)
	}
	if m.palette.idx != 0 {
		t.Fatalf("粘贴后应重置选中项，实际 %d", m.palette.idx)
	}
}

func TestPasteIgnoredOutsideInput(t *testing.T) {
	m := fixtureModel()
	m.Update(tea.PasteMsg{Content: "周杰伦"})

	if m.searchInput != "" || m.promptInput != "" || m.palette != nil {
		t.Fatalf("非输入状态不应接收粘贴: search=%q prompt=%q palette=%+v", m.searchInput, m.promptInput, m.palette)
	}
}

func TestPasteIgnoredWhenSearchNotEditing(t *testing.T) {
	m := fixtureModel()
	m.view = viewSearch
	m.searchEditing = false

	m.Update(tea.PasteMsg{Content: "周杰伦"})

	if m.searchInput != "" {
		t.Fatalf("搜索结果页未进入编辑时不应接收粘贴: %q", m.searchInput)
	}
}

func TestSanitizePastedText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  a\n\tb  ", "a b"},
		{"\x1b[31mred\x1b[0m", "[31mred[0m"},
		{"\r\n", ""},
		{"周杰伦", "周杰伦"},
	}
	for _, tc := range cases {
		if got := sanitizePastedText(tc.in); got != tc.want {
			t.Fatalf("sanitizePastedText(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestPasteIsCappedAndStaysSingleLine(t *testing.T) {
	m := fixtureModel()
	m.openSourceSearch()
	m.Update(tea.PasteMsg{Content: "\x1b[31m" + strings.Repeat("歌", maxPasteRunes+100) + "\n次"})

	got := m.searchInput
	if strings.ContainsAny(got, "\x1b\n\r\t") {
		t.Fatalf("粘贴结果仍含控制字符: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != maxPasteRunes {
		t.Fatalf("粘贴结果应截断到 %d 个字符，实际 %d", maxPasteRunes, n)
	}
}
