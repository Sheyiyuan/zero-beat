package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sheyiyuan/zero-beat/internal/lyrics"
)

// lyricFocusModel 构造 40 句歌词、正在播放第 21 句（idx=20）的歌词页模型。
func lyricFocusModel() *Model {
	m := fixtureModel()
	m.view = viewLyrics
	m.lyricLoaded = true
	m.lyricLines = make([]lyrics.Line, 0, 40)
	for i := 0; i < 40; i++ {
		m.lyricLines = append(m.lyricLines, lyrics.Line{StartMS: i * 1000, Text: fmt.Sprintf("第 %d 句", i+1)})
	}
	m.lyricIdx = 20
	m.followLyricLine()
	return m
}

func lyricStart(m *Model) int {
	_, start := m.lyricWindow(m.bodyHeight())
	return start
}

// TestLyricsFollowCentersPlayingLine 跟随态：焦点行居中，中间就是正在播放的那句。
func TestLyricsFollowCentersPlayingLine(t *testing.T) {
	m := lyricFocusModel()
	start := lyricStart(m)
	if want := 20 - m.bodyHeight()/2; start != want {
		t.Fatalf("跟随态应把播放行居中，start=%d 期望 %d", start, want)
	}
	if got := m.lyricFocusLine(); got != 20 {
		t.Fatalf("跟随态焦点应为播放行 20，实际 %d", got)
	}
}

// TestLyricsArrowMovesFocusAndKeepsCenter 浏览态：↓/↑ 移动焦点一行，视口随之居中。
func TestLyricsArrowMovesFocusAndKeepsCenter(t *testing.T) {
	m := lyricFocusModel()
	before := lyricStart(m)

	updated, _ := m.Update(keyPress("", tea.KeyDown))
	m = updated.(*Model)
	if m.lyricFocus != 21 || m.lyricFollow {
		t.Fatalf("↓ 应把焦点移到 21 并进入浏览态，实际 focus=%d follow=%v", m.lyricFocus, m.lyricFollow)
	}
	if got := lyricStart(m); got != before+1 {
		t.Fatalf("视口应随焦点下移一行：%d -> %d", before, got)
	}
	updated, _ = m.Update(keyPress("", tea.KeyUp))
	m = updated.(*Model)
	if m.lyricFocus != 20 || lyricStart(m) != before {
		t.Fatalf("↑ 应回到原焦点与原视口：focus=%d start=%d", m.lyricFocus, lyricStart(m))
	}
}

// TestLyricsEnterSeeksFocusedLine Enter 跳到焦点行并恢复跟随。
func TestLyricsEnterSeeksFocusedLine(t *testing.T) {
	m := lyricFocusModel()
	updated, _ := m.Update(keyPress("", tea.KeyDown))
	m = updated.(*Model)
	updated, _ = m.Update(keyPress("", tea.KeyDown))
	m = updated.(*Model)
	if m.lyricFocus != 22 {
		t.Fatalf("焦点应为 22，实际 %d", m.lyricFocus)
	}
	updated, _ = m.Update(keyPress("", tea.KeyEnter))
	m = updated.(*Model)
	if m.position.Milliseconds() != 22000 {
		t.Fatalf("enter 应跳到第 22 句起点 22s，实际 %v", m.position)
	}
	if !m.lyricFollow {
		t.Fatal("跳转后应恢复跟随")
	}
}

// TestLyricsEscReturnsToPlayingLine esc 第一次回播放行，再按才返回曲库。
func TestLyricsEscReturnsToPlayingLine(t *testing.T) {
	m := lyricFocusModel()
	updated, _ := m.Update(keyPress("", tea.KeyDown))
	m = updated.(*Model)
	if !m.lyricBrowsing() {
		t.Fatal("↓ 后应进入浏览态")
	}
	updated, _ = m.Update(keyPress("", tea.KeyEscape))
	m = updated.(*Model)
	if m.view != viewLyrics {
		t.Fatalf("第一次 esc 应留在歌词页，实际 %v", viewNames[m.view])
	}
	if m.lyricBrowsing() || m.lyricFocus != m.lyricIdx {
		t.Fatalf("esc 应回到播放行，实际 focus=%d idx=%d", m.lyricFocus, m.lyricIdx)
	}
	updated, _ = m.Update(keyPress("", tea.KeyEscape))
	m = updated.(*Model)
	if m.view != viewLibrary {
		t.Fatalf("第二次 esc 应返回曲库，实际 %v", viewNames[m.view])
	}
}

// TestLyricsTopBottomAndClamp g/G 到首末句，移动不会越界。
func TestLyricsTopBottomAndClamp(t *testing.T) {
	m := lyricFocusModel()
	updated, _ := m.Update(keyPress("g", 'g'))
	m = updated.(*Model)
	if m.lyricFocus != 0 {
		t.Fatalf("g 应到首句，实际 %d", m.lyricFocus)
	}
	updated, _ = m.Update(keyPress("G", 'G'))
	m = updated.(*Model)
	if m.lyricFocus != len(m.lyricLines)-1 {
		t.Fatalf("G 应到末句，实际 %d", m.lyricFocus)
	}
	updated, _ = m.Update(keyPress("", tea.KeyDown))
	m = updated.(*Model)
	if m.lyricFocus != len(m.lyricLines)-1 {
		t.Fatalf("末句再按下应夹住，实际 %d", m.lyricFocus)
	}
}

// TestLyricsFocusUsesGrayBackground 浏览态：中央焦点行用灰底表示选中，
// 正在播放行保留蓝色渐变 + ▶，不再用蓝色背景表示焦点。
func TestLyricsFocusUsesGrayBackground(t *testing.T) {
	m := lyricFocusModel()
	m.width, m.height = 100, 30
	updated, _ := m.Update(keyPress("", tea.KeyDown)) // 焦点 21，播放 20
	m = updated.(*Model)

	lines := strings.Split(m.View().Content, "\n")
	center := 2 + m.bodyHeight()/2
	focusLine := lines[center]
	if !strings.Contains(focusLine, "48;2;58;63;74") {
		t.Fatalf("焦点行应使用灰底：%q", focusLine)
	}
	if strings.Contains(focusLine, "48;2;102;204;255") {
		t.Fatalf("焦点行不应再用蓝色选中底：%q", focusLine)
	}

	playingLine := lines[center-1]
	if !strings.Contains(ansi.Strip(playingLine), "▶") {
		t.Fatalf("正在播放行应有 ▶ 标记：%q", ansi.Strip(playingLine))
	}
	if strings.Contains(playingLine, "48;2;58;63;74") {
		t.Fatalf("正在播放行不应套焦点灰底：%q", playingLine)
	}
}
