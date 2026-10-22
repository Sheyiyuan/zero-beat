package tui

import (
	"context"
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/player"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

type mouseFakeEngine struct {
	state  audio.State
	events chan audio.Event
}

func newMouseFakeEngine(state audio.State) *mouseFakeEngine {
	return &mouseFakeEngine{state: state, events: make(chan audio.Event)}
}

func (f *mouseFakeEngine) Events() <-chan audio.Event { return f.events }
func (f *mouseFakeEngine) State() audio.State         { return f.state }
func (f *mouseFakeEngine) SetVolume(float64)          {}
func (f *mouseFakeEngine) SetCompensationDB(float64)  {}
func (f *mouseFakeEngine) Pause()                     { f.state = audio.StatePaused }
func (f *mouseFakeEngine) Resume()                    { f.state = audio.StatePlaying }
func (f *mouseFakeEngine) Stop()                      { f.state = audio.StateIdle }
func (f *mouseFakeEngine) Position() time.Duration    { return 0 }
func (f *mouseFakeEngine) Duration() time.Duration    { return 0 }
func (f *mouseFakeEngine) CurrentStream() audio.Stream {
	return audio.Stream{URL: "http://example.test/stream", ExpiresAt: time.Now().Add(time.Hour)}
}
func (f *mouseFakeEngine) Seek(context.Context, time.Duration) error { return nil }
func (f *mouseFakeEngine) PlayAt(context.Context, audio.Stream, time.Duration) error {
	f.state = audio.StatePlaying
	return nil
}

func TestPlaybackIconShowsAction(t *testing.T) {
	m := fixtureModel()
	m.state = audio.StatePlaying
	if got := m.playbackIcon(); got != "⏸" {
		t.Fatalf("播放中应显示暂停按钮，实际 %q", got)
	}
	m.state = audio.StatePaused
	if got := m.playbackIcon(); got != "▶" {
		t.Fatalf("暂停时应显示播放按钮，实际 %q", got)
	}
}

func TestViewEnablesMouse(t *testing.T) {
	m := fixtureModel()
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("应启用鼠标点击/滚轮模式，实际 %v", got)
	}
}

func TestMouseClickPlaybackButtonToggles(t *testing.T) {
	m := fixtureModel()
	m.engine = nil
	m.pc = player.New(newMouseFakeEngine(audio.StatePlaying), nil)

	updated, _ := m.Update(tea.MouseClickMsg{X: 1, Y: m.height - 3, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if mm.state != audio.StatePaused {
		t.Fatalf("点击播放中按钮后应暂停，实际 %v", mm.state)
	}

	updated, _ = mm.Update(tea.MouseClickMsg{X: 1, Y: mm.height - 3, Button: tea.MouseLeft})
	mm = updated.(*Model)
	if mm.state != audio.StatePlaying {
		t.Fatalf("再次点击应继续播放，实际 %v", mm.state)
	}
}

func TestMouseClickOutsideButtonDoesNotToggle(t *testing.T) {
	m := fixtureModel()
	m.pc = player.New(newMouseFakeEngine(audio.StatePlaying), nil)

	updated, _ := m.Update(tea.MouseClickMsg{X: 40, Y: m.height - 3, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if mm.state != audio.StatePlaying {
		t.Fatalf("点击标题区域不应切换播放状态，实际 %v", mm.state)
	}
}

func TestMouseClickLyricStripOpensLyrics(t *testing.T) {
	m := fixtureModel()
	m.lyricLoaded = true
	m.lyricLines = []lyrics.Line{{StartMS: 0, Text: "测试歌词"}}
	m.lyricIdx = 0

	updated, _ := m.Update(tea.MouseClickMsg{X: 10, Y: m.height - 4, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if mm.view != viewLyrics {
		t.Fatalf("点击歌词条应进入歌词页，实际 %v", mm.view)
	}
}

func TestMouseClickBlockedByPopup(t *testing.T) {
	m := fixtureModel()
	m.lyricLoaded = true
	m.lyricLines = []lyrics.Line{{StartMS: 0, Text: "测试歌词"}}
	m.helpPopup = true

	updated, _ := m.Update(tea.MouseClickMsg{X: 10, Y: m.height - 4, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if mm.view == viewLyrics {
		t.Fatal("浮层打开时点击不应穿透到歌词页")
	}
}

func findZone(t *testing.T, m *Model, target hitTarget) hitZone {
	t.Helper()
	for _, z := range m.hitZones() {
		if z.target == target {
			return z
		}
	}
	t.Fatalf("未找到命中区域 %v", target)
	return hitZone{}
}

func TestMouseClickHeaderAboutAndAccount(t *testing.T) {
	m := fixtureModel()
	zone := findZone(t, m, hitAbout)
	updated, _ := m.Update(tea.MouseClickMsg{X: zone.x, Y: zone.y, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if !mm.aboutPopup {
		t.Fatal("点击 zero 应打开关于弹窗")
	}

	m = fixtureModel()
	zone = findZone(t, m, hitAccount)
	updated, _ = m.Update(tea.MouseClickMsg{X: zone.x, Y: zone.y, Button: tea.MouseLeft})
	mm = updated.(*Model)
	if mm.view != viewLogin {
		t.Fatalf("点击账户应进入登录页，实际 %v", mm.view)
	}
}

func TestMouseClickPageNameOpensPageMenu(t *testing.T) {
	m := fixtureModel()
	zone := findZone(t, m, hitPageMenu)
	updated, _ := m.Update(tea.MouseClickMsg{X: zone.x + 1, Y: zone.y, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if mm.menu == nil || mm.menu.kind != menuPage {
		t.Fatalf("点击页面名应打开页面菜单，实际 %+v", mm.menu)
	}
}

func TestMouseClickModeCyclesMode(t *testing.T) {
	m := fixtureModel()
	zone := findZone(t, m, hitMode)
	updated, _ := m.Update(tea.MouseClickMsg{X: zone.x + 1, Y: zone.y, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if mm.mode == modeSequence {
		t.Fatalf("点击播放模式应切换模式，实际 %v", mm.mode)
	}
}

func TestMouseWheelOnNowPlayingChangesVolume(t *testing.T) {
	m := fixtureModel()
	updated, _ := m.Update(tea.MouseWheelMsg{X: 10, Y: m.height - 3, Button: tea.MouseWheelUp})
	mm := updated.(*Model)
	if mm.volume <= 0.8 {
		t.Fatalf("滚轮上滚应提高音量，实际 %v", mm.volume)
	}
	updated, _ = mm.Update(tea.MouseWheelMsg{X: 10, Y: mm.height - 3, Button: tea.MouseWheelDown})
	mm = updated.(*Model)
	if mm.volume >= 0.82 {
		t.Fatalf("滚轮下滚应降低音量，实际 %v", mm.volume)
	}
}

func TestMouseRightClickSongOpensContextMenu(t *testing.T) {
	m := fixtureModel()
	zone := findZone(t, m, hitLibrarySong)
	updated, _ := m.Update(tea.MouseClickMsg{X: zone.x + 2, Y: zone.y, Button: tea.MouseRight})
	mm := updated.(*Model)
	if mm.menu == nil || mm.menu.kind != menuSong {
		t.Fatalf("右键歌曲应打开歌曲菜单，实际 %+v", mm.menu)
	}
	if mm.menu.song.ID != m.songs[0].ID {
		t.Fatalf("菜单应指向右键的歌曲，实际 %+v", mm.menu.song)
	}
}

func TestMouseRightClickPlaylistOpensContextMenu(t *testing.T) {
	m := fixtureModel()
	zone := findZone(t, m, hitLibraryPlaylist)
	updated, _ := m.Update(tea.MouseClickMsg{X: zone.x, Y: zone.y, Button: tea.MouseRight})
	mm := updated.(*Model)
	if mm.menu == nil || mm.menu.kind != menuPlaylist {
		t.Fatalf("右键歌单应打开歌单菜单，实际 %+v", mm.menu)
	}
}

func TestMouseClickLyricSeeks(t *testing.T) {
	m := fixtureModel()
	m.pc = player.New(newMouseFakeEngine(audio.StatePlaying), nil)
	m.position = 0
	m.view = viewLyrics
	m.lyricLoaded = true
	m.lyricLines = []lyrics.Line{
		{StartMS: 0, Text: "第一句"},
		{StartMS: 5000, Text: "第二句"},
	}
	m.lyricIdx = 0
	zone := findZone(t, m, hitLyricRow)
	for _, z := range m.hitZones() {
		if z.target == hitLyricRow && z.index == 1 {
			zone = z
			break
		}
	}
	updated, _ := m.Update(tea.MouseClickMsg{X: 50, Y: zone.y, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if mm.position != 5*time.Second {
		t.Fatalf("单击歌词应跳到 5s，实际 %v", mm.position)
	}
}

func TestMouseDoubleClickSongPlays(t *testing.T) {
	m := fixtureModel()
	m.pc = player.New(newMouseFakeEngine(audio.StatePlaying), nil)
	var zone hitZone
	for _, z := range m.hitZones() {
		if z.target == hitLibrarySong && z.index == 1 {
			zone = z
			break
		}
	}
	updated, _ := m.Update(tea.MouseClickMsg{X: zone.x + 2, Y: zone.y, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if mm.current == nil || mm.current.ID != m.songs[0].ID {
		t.Fatal("第一次点击只应选中歌曲")
	}
	updated, _ = mm.Update(tea.MouseClickMsg{X: zone.x + 2, Y: zone.y, Button: tea.MouseLeft})
	mm = updated.(*Model)
	if mm.current == nil || mm.current.ID != m.songs[1].ID {
		t.Fatalf("双击歌曲应开始播放，实际 %+v", mm.current)
	}
}

func TestMouseRightClickLyricOpensMenu(t *testing.T) {
	m := fixtureModel()
	m.view = viewLyrics
	m.current = &store.Song{ID: "bilibili:BV1:1", Title: "Never Gonna Give You Up"}
	m.lyricLoaded = true
	m.lyricLines = []lyrics.Line{{StartMS: 0, Text: "第一句"}}
	zone := findZone(t, m, hitLyricRow)
	updated, _ := m.Update(tea.MouseClickMsg{X: 10, Y: zone.y, Button: tea.MouseRight})
	mm := updated.(*Model)
	if mm.menu == nil || mm.menu.kind != menuLyrics {
		t.Fatalf("右键歌词应打开歌词菜单，实际 %+v", mm.menu)
	}
}

func TestMouseClickPageMenuItemSwitchesView(t *testing.T) {
	m := fixtureModel()
	m.openPageMenu(6, 1)
	x, y, _, _, ok := m.menuLayout()
	if !ok {
		t.Fatal("页面菜单应已打开")
	}
	idx := 3 // 队列
	updated, _ := m.Update(tea.MouseClickMsg{X: x + 2, Y: y + 1 + idx, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if mm.view != viewQueue {
		t.Fatalf("点击页面菜单应切到队列，实际 %v", mm.view)
	}
	if mm.menu != nil {
		t.Fatal("切换页面后菜单应关闭")
	}
}

func TestMouseClickOutsideMenuClosesIt(t *testing.T) {
	m := fixtureModel()
	m.openPageMenu(6, 1)
	updated, _ := m.Update(tea.MouseClickMsg{X: m.width - 1, Y: m.height - 1, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if mm.menu != nil {
		t.Fatal("点击菜单外部应关闭菜单")
	}
}

func TestMouseWheelScrollsPlaylistSidebar(t *testing.T) {
	m := fixtureModel()
	for i := 0; i < 10; i++ {
		m.playlists = append(m.playlists, store.Playlist{ID: fmt.Sprintf("p%d", i), Title: fmt.Sprintf("歌单 %d", i), Kind: "local"})
	}
	m.libraryFocus = focusPlaylists
	m.playlistIdx = 0
	updated, _ := m.Update(tea.MouseWheelMsg{X: 1, Y: 5, Button: tea.MouseWheelDown})
	mm := updated.(*Model)
	if mm.playlistIdx != 3 {
		t.Fatalf("歌单栏滚轮下滚应移动三行，实际 %d", mm.playlistIdx)
	}
	updated, _ = mm.Update(tea.MouseWheelMsg{X: 1, Y: 5, Button: tea.MouseWheelUp})
	mm = updated.(*Model)
	if mm.playlistIdx != 0 {
		t.Fatalf("歌单栏滚轮上滚应回滚三行，实际 %d", mm.playlistIdx)
	}
}

// TestMouseHeaderClickWhileSearchEditingClearsEditing 验证搜索输入态点页头时
// 先退出输入态：关于弹窗在最上层且按键归弹窗，不再出现「弹窗提示 enter 关闭、
// 实际 enter 触发搜索」的层级错乱。
func TestMouseHeaderClickWhileSearchEditingClearsEditing(t *testing.T) {
	m := fixtureModel()
	m.view = viewSearch
	m.searchMode = searchLocalAll
	m.searchEditing = true

	zone := findZone(t, m, hitAbout)
	updated, _ := m.Update(tea.MouseClickMsg{X: zone.x, Y: zone.y, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if !mm.aboutPopup {
		t.Fatal("点击 zero 应打开关于弹窗")
	}
	if mm.searchEditing {
		t.Fatal("打开关于时应退出搜索输入态")
	}

	// 弹窗盖在搜索页上时，enter 只关闭弹窗，不应触发搜索。
	updated, _ = mm.Update(keyPress("", tea.KeyEnter))
	mm = updated.(*Model)
	if mm.aboutPopup {
		t.Fatal("enter 应关闭关于弹窗")
	}
	if mm.searchBusy {
		t.Fatal("弹窗打开时 enter 不应触发搜索")
	}

	// 页面名与账户同样先退出输入态。
	m = fixtureModel()
	m.view = viewSearch
	m.searchMode = searchLocalAll
	m.searchEditing = true
	zone = findZone(t, m, hitPageMenu)
	updated, _ = m.Update(tea.MouseClickMsg{X: zone.x + 1, Y: zone.y, Button: tea.MouseLeft})
	mm = updated.(*Model)
	if mm.searchEditing || mm.menu == nil {
		t.Fatalf("点页面名应先退出输入态并打开菜单：editing=%v menu=%+v", mm.searchEditing, mm.menu)
	}
}
