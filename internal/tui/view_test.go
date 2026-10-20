package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/bili"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

func fixtureModel() *Model {
	songs := []store.Song{
		{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "Never Gonna Give You Up", Artist: "Rick Astley", Duration: 213},
		{ID: "bilibili:BV2:1", Source: "bilibili", BVID: "BV2", Page: 1, Title: "夜航星", Artist: "不才", Duration: 287},
	}
	m := &Model{
		width:  100,
		height: 30,
		view:   viewLibrary,
		volume: 0.8,
		mode:   modeSequence,
		state:  audio.StatePlaying,
	}
	m.playlists = []store.Playlist{
		{ID: "bili-fav-1", Title: "我喜欢的音乐", Kind: "remote", Count: 2},
		{ID: "local-default", Title: "本地歌单", Kind: "local", Count: 0},
	}
	m.songs = songs
	m.queue = append([]store.Song(nil), songs...)
	m.current = &songs[0]
	m.duration = 213 * time.Second
	m.position = 42 * time.Second
	return m
}

func assertFrame(t *testing.T, frame string, w, h int) []string {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != h {
		t.Fatalf("帧高度应为 %d，实际 %d", h, len(lines))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != w {
			t.Fatalf("第 %d 行宽度应为 %d，实际 %d：%q", i, w, got, ansi.Strip(line))
		}
	}
	return lines
}

func TestRenderLibrary(t *testing.T) {
	m := fixtureModel()
	lines := assertFrame(t, m.View().Content, 100, 30)
	body := ansi.Strip(strings.Join(lines, "\n"))

	for _, want := range []string{"zero", "曲库", "我喜欢的音乐", "本地歌单", "Never Gonna Give You Up", "夜航星", "0:42 / 3:33", "顺序"} {
		if !strings.Contains(body, want) {
			t.Fatalf("界面里应该出现 %q", want)
		}
	}
}

func TestRenderAllViews(t *testing.T) {
	m := fixtureModel()
	m.searchKeyword = "周杰伦"
	m.searchResults = []bili.SearchItem{
		{BVID: "BV1", Title: "稻香", Author: "周杰伦", Duration: 223, Play: 1234567},
		{BVID: "BV2", Title: "晴天", Author: "周杰伦", Duration: 269},
	}
	m.lyricLines = []lyrics.Line{
		{StartMS: 0, Text: "第一句歌词"},
		{StartMS: 5000, Text: "第二句歌词"},
		{StartMS: 10000, Text: "第三句歌词"},
	}
	m.lyricIdx = 1

	for _, v := range []view{viewLibrary, viewSearch, viewQueue, viewLyrics, viewHelp, viewLogin} {
		m.view = v
		frame := m.View().Content
		assertFrame(t, frame, 100, 30)
	}
}

func TestRenderNarrowTerminal(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 60, 18
	assertFrame(t, m.View().Content, 60, 18)
}

func TestSearchViewContent(t *testing.T) {
	m := fixtureModel()
	m.view = viewSearch
	m.searchKeyword = "周杰伦"
	m.searchResults = []bili.SearchItem{{BVID: "BV1", Title: "稻香", Author: "周杰伦", Duration: 223, Play: 1234567}}
	body := ansi.Strip(m.View().Content)
	for _, want := range []string{"搜索", "稻香", "周杰伦", "3:43", "1,234,567 播放"} {
		if !strings.Contains(body, want) {
			t.Fatalf("搜索页应包含 %q，实际：\n%s", want, body)
		}
	}
}

func TestLyricsHighlightFollowsIndex(t *testing.T) {
	m := fixtureModel()
	m.view = viewLyrics
	m.lyricLines = []lyrics.Line{{StartMS: 0, Text: "甲"}, {StartMS: 1000, Text: "乙"}, {StartMS: 2000, Text: "丙"}}
	m.lyricIdx = 1
	frame := m.View().Content
	if !strings.Contains(frame, "乙") {
		t.Fatal("当前歌词应被渲染")
	}
	lines := strings.Split(frame, "\n")
	bodyH := 30 - 6
	centerIdx := 2 + bodyH/2 // header + 分隔线之后是正文
	if !strings.Contains(lines[centerIdx], "乙") {
		t.Fatalf("当前行应在正文中间，实际中心行：%q", ansi.Strip(lines[centerIdx]))
	}
}

func TestSlashEntersSearchEditing(t *testing.T) {
	m := fixtureModel()
	updated, _ := m.Update(keyPress("/", '/'))
	mm := updated.(*Model)
	if mm.view != viewSearch || !mm.searchEditing {
		t.Fatalf("按 / 应进入搜索输入态，实际 view=%v editing=%v", mm.view, mm.searchEditing)
	}
	for _, r := range []rune("稻香") {
		updated, _ = mm.Update(keyPress(string(r), r))
		mm = updated.(*Model)
	}
	if mm.searchInput != "稻香" {
		t.Fatalf("输入未累积: %q", mm.searchInput)
	}
}

func TestSearchEnterStartsSearch(t *testing.T) {
	m := fixtureModel()
	m.view = viewSearch
	m.searchEditing = true
	m.searchInput = "周杰伦"
	updated, cmd := m.Update(keyPress("", tea.KeyEnter))
	mm := updated.(*Model)
	if mm.searchEditing {
		t.Fatal("回车后应退出输入态")
	}
	if mm.searchKeyword != "周杰伦" {
		t.Fatalf("关键词未生效: %q", mm.searchKeyword)
	}
	if cmd == nil {
		t.Fatal("回车后应发起搜索命令")
	}
}

func TestTabCyclesViews(t *testing.T) {
	m := fixtureModel()
	seen := map[view]bool{}
	for i := 0; i < len(cycleViews); i++ {
		updated, _ := m.Update(keyPress("", tea.KeyTab))
		m = updated.(*Model)
		seen[m.view] = true
	}
	if len(seen) != len(cycleViews) {
		t.Fatalf("tab 应轮转到全部视图，实际覆盖 %d/%d", len(seen), len(cycleViews))
	}
}

func TestAdvanceWrapsAndRespectsLoopOne(t *testing.T) {
	m := fixtureModel()
	m.queueIdx = len(m.queue) - 1
	m.mode = modeSequence
	_ = m.advance()
	if m.queueIdx != 0 {
		t.Fatalf("顺序模式应回到第一首，实际 %d", m.queueIdx)
	}

	m.mode = modeLoopOne
	m.queueIdx = 1
	_ = m.advance()
	if m.queueIdx != 1 {
		t.Fatalf("单曲循环应停在原地，实际 %d", m.queueIdx)
	}
}

func TestVolumeAndCompensationClamp(t *testing.T) {
	m := fixtureModel()
	m.changeVolume(0.5)
	if m.volume != 1 {
		t.Fatalf("音量应被夹到 1，实际 %v", m.volume)
	}
	for i := 0; i < 30; i++ {
		m.changeComp(1)
	}
	if m.compDB != 12 {
		t.Fatalf("补偿上限应为 12 dB，实际 %v", m.compDB)
	}
	for i := 0; i < 40; i++ {
		m.changeComp(-1)
	}
	if m.compDB != -20 {
		t.Fatalf("补偿下限应为 -20 dB，实际 %v", m.compDB)
	}
}

func keyPress(text string, code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: text, Code: code})
}
