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
