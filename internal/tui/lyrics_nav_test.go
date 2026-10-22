package tui

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/lyrics"
)

func lyricScrollModel() *Model {
	m := fixtureModel()
	m.view = viewLyrics
	m.lyricLoaded = true
	m.lyricLines = make([]lyrics.Line, 0, 40)
	for i := 0; i < 40; i++ {
		m.lyricLines = append(m.lyricLines, lyrics.Line{StartMS: i * 1000, Text: fmt.Sprintf("第 %d 句", i+1)})
	}
	m.lyricIdx = 20
	return m
}

func lyricStart(m *Model) int {
	_, start := m.lyricWindow(m.bodyHeight())
	return start
}

func TestLyricsArrowKeysScroll(t *testing.T) {
	m := lyricScrollModel()
	before := lyricStart(m)
	updated, _ := m.Update(keyPress("", tea.KeyDown))
	m = updated.(*Model)
	if got := lyricStart(m); got != before+1 {
		t.Fatalf("↓ 应向下滚动一行，实际 %d，期望 %d", got, before+1)
	}
	updated, _ = m.Update(keyPress("", tea.KeyUp))
	m = updated.(*Model)
	if got := lyricStart(m); got != before {
		t.Fatalf("↑ 应回滚一行，实际 %d，期望 %d", got, before)
	}
}

func TestLyricsWheelScrolls(t *testing.T) {
	m := lyricScrollModel()
	before := lyricStart(m)
	updated, _ := m.Update(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	m = updated.(*Model)
	if got := lyricStart(m); got != before+3 {
		t.Fatalf("滚轮下滚应滚动三行，实际 %d，期望 %d", got, before+3)
	}
	updated, _ = m.Update(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})
	m = updated.(*Model)
	if got := lyricStart(m); got != before {
		t.Fatalf("滚轮上滚应回滚三行，实际 %d，期望 %d", got, before)
	}
}

func TestLyricsScrollReturnsToFocusAfterFiveSeconds(t *testing.T) {
	m := lyricScrollModel()
	before := lyricStart(m)
	updated, _ := m.Update(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	m = updated.(*Model)
	if lyricStart(m) == before {
		t.Fatal("滚轮后应偏离焦点")
	}
	m.maybeResetLyricScroll(m.lyricScrollAt.Add(5*time.Second + time.Millisecond))
	if got := lyricStart(m); got != before {
		t.Fatalf("5 秒无操作后应回到焦点，实际 %d，期望 %d", got, before)
	}
}

// TestLyricsScrollFromTopMovesOneLine 验证歌曲开头（base 为负、靠顶部留白居中）
// 时按 ↓ 只移动一行，↑ 能原路返回，而不是一次跳到顶部。
func TestLyricsScrollFromTopMovesOneLine(t *testing.T) {
	m := lyricScrollModel()
	m.lyricIdx = 0
	m.resetLyricScroll()
	base := lyricStart(m)
	if base >= 0 {
		t.Fatalf("当前句在开头时窗口起点应为负（顶部留白），实际 %d", base)
	}

	m.scrollLyrics(1)
	if got := lyricStart(m); got != base+1 {
		t.Fatalf("开头按 ↓ 应只下移一行：%d -> %d", base, got)
	}
	m.scrollLyrics(-1)
	if got := lyricStart(m); got != base {
		t.Fatalf("开头按 ↑ 应回到居中视图：%d，期望 %d", got, base)
	}
	m.scrollLyrics(1)
	m.scrollLyrics(1)
	if got := lyricStart(m); got != base+2 {
		t.Fatalf("连续两行 ↓ 应累计两行：%d，期望 %d", got, base+2)
	}
}

// TestLyricsScrollShortLyrics 验证歌词行数少于正文高度时也能逐行滚动、到底停住、
// 再逐行滚回居中。
func TestLyricsScrollShortLyrics(t *testing.T) {
	m := lyricScrollModel()
	m.lyricLines = m.lyricLines[:12]
	m.lyricIdx = 5
	m.resetLyricScroll()
	base := lyricStart(m)

	m.scrollLyrics(1)
	if got := lyricStart(m); got != base+1 {
		t.Fatalf("短歌词开头按 ↓ 应下移一行：%d -> %d", base, got)
	}
	for i := 0; i < 20; i++ {
		m.scrollLyrics(1)
	}
	if got := lyricStart(m); got != 0 {
		t.Fatalf("滚到底应停在 0，实际 %d", got)
	}
	m.scrollLyrics(-1)
	if got := lyricStart(m); got != -1 {
		t.Fatalf("底端按 ↑ 应逐行回退，实际 %d", got)
	}
}
