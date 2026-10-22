package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/player"
	"github.com/sheyiyuan/zero-beat/internal/source"
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

// TestSongMenuDownloadHintUsesDownloadSelectedKey 验证歌曲右键菜单里的「下载」
// 作用对象是该行歌曲，提示键必须是 download_selected（W），而不是「下载正在播放」（w）。
func TestSongMenuDownloadHintUsesDownloadSelectedKey(t *testing.T) {
	m := fixtureModel()
	zone := findZone(t, m, hitLibrarySong)
	updated, _ := m.Update(tea.MouseClickMsg{X: zone.x + 2, Y: zone.y, Button: tea.MouseRight})
	mm := updated.(*Model)
	if mm.menu == nil || mm.menu.kind != menuSong {
		t.Fatalf("右键歌曲应打开歌曲菜单，实际 %+v", mm.menu)
	}
	want := m.firstKey(keymap.DownloadSelected)
	if want != "W" {
		t.Fatalf("默认 download_selected 键应为 W，实际 %q", want)
	}
	for _, item := range mm.menu.items {
		if item.label != "下载" {
			continue
		}
		if item.hint != want {
			t.Fatalf("歌曲菜单「下载」应提示 %q，实际 %q", want, item.hint)
		}
		if item.act != actDownload {
			t.Fatalf("歌曲菜单「下载」动作应保持 actDownload，实际 %v", item.act)
		}
		return
	}
	t.Fatal("歌曲菜单缺少「下载」项")
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

func TestMouseClickLyricFocusesAndDoubleClickSeeks(t *testing.T) {
	m := fixtureModel()
	m.pc = player.New(newMouseFakeEngine(audio.StatePlaying), nil)
	m.position = 0
	m.view = viewLyrics
	m.lyricLoaded = true
	m.lyricLines = []lyrics.Line{
		{StartMS: 0, Text: "第一句"},
		{StartMS: 5000, Text: "第二句"},
		{StartMS: 9000, Text: "第三句"},
	}
	m.lyricIdx = 0
	m.followLyricLine()

	findLine := func(mm *Model, line int) hitZone {
		t.Helper()
		for _, z := range mm.hitZones() {
			if z.target == hitLyricRow && z.index == line {
				return z
			}
		}
		t.Fatalf("未找到第 %d 句的命中区", line)
		return hitZone{}
	}

	zone := findLine(m, 1)
	// 单击：焦点移到第二句，但不跳转。
	updated, _ := m.Update(tea.MouseClickMsg{X: 50, Y: zone.y, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if mm.position != 0 {
		t.Fatalf("单击歌词不应跳转，实际 %v", mm.position)
	}
	if mm.lyricFocus != 1 || !mm.lyricBrowsing() {
		t.Fatalf("单击应把焦点移到第二句并进入浏览态，实际 focus=%d", mm.lyricFocus)
	}
	// 双击（同一屏幕位置）：提交第一次点中的焦点行并恢复跟随。
	updated, _ = mm.Update(tea.MouseClickMsg{X: 50, Y: zone.y, Button: tea.MouseLeft})
	mm = updated.(*Model)
	if mm.position.Milliseconds() != 5000 {
		t.Fatalf("双击应跳到第二句 5s，实际 %v", mm.position)
	}
	if !mm.lyricFollow {
		t.Fatal("双击跳转后应恢复跟随")
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

// TestMouseClickOutsideOverlayClosesIt 验证浮层内部点击不关闭、外部点击自动关闭。
func TestMouseClickOutsideOverlayClosesIt(t *testing.T) {
	cases := []struct {
		name   string
		open   func(m *Model)
		lines  func(m *Model) []string
		closed func(m *Model) bool
	}{
		{"关于", func(m *Model) { m.aboutPopup = true },
			func(m *Model) []string { return m.aboutLines() },
			func(m *Model) bool { return !m.aboutPopup }},
		{"帮助", func(m *Model) { m.helpPopup = true },
			func(m *Model) []string { return m.helpPopupLines() },
			func(m *Model) bool { return !m.helpPopup }},
		{"选择器", func(m *Model) {
			m.picker = &targetPickerState{
				mode:  targetPickerAddSong,
				items: []playlistTarget{{ID: "local-default", Title: "本地歌单", Kind: "local", Count: 3}},
			}
		},
			func(m *Model) []string { return m.targetPickerLines() },
			func(m *Model) bool { return m.picker == nil }},
	}
	for _, c := range cases {
		m := fixtureModel()
		c.open(m)
		left, top, w, h, ok := overlayBoxBounds(c.lines(m), m.width, m.height)
		if !ok {
			t.Fatalf("%s: 浮层没有生成", c.name)
		}
		updated, _ := m.Update(tea.MouseClickMsg{X: left + w/2, Y: top + h/2, Button: tea.MouseLeft})
		mi := updated.(*Model)
		if c.closed(mi) {
			t.Fatalf("%s: 点击浮层内部不应关闭", c.name)
		}
		x, y := 0, 0
		if left == 0 && top == 0 {
			x, y = m.width-1, m.height-1
		}
		updated, _ = mi.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		if mo := updated.(*Model); !c.closed(mo) {
			t.Fatalf("%s: 点击浮层外部应关闭", c.name)
		}
	}
}

// TestViewMouseCaptureCanBeDisabled 验证关闭鼠标后不再上报鼠标模式
// （环境变量 ZERO_MOUSE=0 或设置里关掉，用于恢复终端自身的右键/拖选）。
func TestViewMouseCaptureCanBeDisabled(t *testing.T) {
	m := fixtureModel()
	m.mouseOff = true
	if got := m.View().MouseMode; got != tea.MouseModeNone {
		t.Fatalf("关闭鼠标后不应启用鼠标捕获，实际 %v", got)
	}
	m.mouseOff = false
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("默认应启用鼠标点击/滚轮，实际 %v", got)
	}
}

// TestPlaylistMenuPlaysSelectedPlaylist 验证右键歌单菜单的「播放歌单」真的开始播放，
// 而不是只重新选中一次（旧实现在已选中时直接 return nil，表现为点啥都没发生）。
func TestPlaylistMenuPlaysSelectedPlaylist(t *testing.T) {
	m := fixtureModel()
	m.playlistIdx = 1 // 「本地歌单」，已选中、曲目已载入
	m.songIdx = 0
	m.libraryFocus = focusSongs

	var zone hitZone
	found := false
	for _, z := range m.hitZones() {
		if z.target == hitLibraryPlaylist && z.index == 1 {
			zone, found = z, true
			break
		}
	}
	if !found {
		t.Fatal("未找到歌单命中区")
	}
	updated, _ := m.Update(tea.MouseClickMsg{X: zone.x + 1, Y: zone.y, Button: tea.MouseRight})
	mm := updated.(*Model)
	if mm.menu == nil || mm.menu.kind != menuPlaylist {
		t.Fatalf("右键歌单应打开歌单菜单，实际 %+v", mm.menu)
	}
	itemIdx := -1
	for i, item := range mm.menu.items {
		if item.act == actPlaylistPlay {
			itemIdx = i
		}
	}
	if itemIdx < 0 {
		t.Fatal("歌单菜单缺少「播放歌单」项")
	}
	if got := mm.menu.items[itemIdx].label; got != "播放歌单" {
		t.Fatalf("菜单文案应为「播放歌单」，实际 %q", got)
	}
	mm.menu.idx = itemIdx
	updated, cmd := mm.applyMenu()
	mm = updated.(*Model)
	if cmd == nil {
		t.Fatal("播放歌单应返回播放命令")
	}
	if mm.current == nil || mm.current.ID != mm.songs[0].ID {
		t.Fatalf("应从歌单第一首开始播放，实际 %+v", mm.current)
	}
	if mm.queueIdx != 0 || len(mm.queue) != len(mm.songs) {
		t.Fatalf("队列应载入整个歌单：idx=%d len=%d", mm.queueIdx, len(mm.queue))
	}
	if mm.playingPlaylistID != mm.playlists[1].ID {
		t.Fatalf("应记录正在播放的来源歌单，实际 %q", mm.playingPlaylistID)
	}
}

// TestPlaylistMenuPlayWaitsForSongs 曲目还在载入时点「播放歌单」，
// 等 songsMsg 到达后自动从头播放。
func TestPlaylistMenuPlayWaitsForSongs(t *testing.T) {
	m := fixtureModel()
	m.playlistIdx = 0
	m.songsBusy = true
	m.songs = nil

	if cmd := m.playPlaylistFromMenu(0); cmd != nil {
		t.Fatal("载入中不应立刻起播")
	}
	if !m.pendingPlayPlaylist {
		t.Fatal("载入中应记下待播放标记")
	}

	items := []store.Song{
		{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "甲", Duration: 10},
		{ID: "bilibili:BV2:1", Source: "bilibili", BVID: "BV2", Page: 1, Title: "乙", Duration: 10},
	}
	updated, cmd := m.Update(songsMsg{playlistID: m.playlists[0].ID, items: items})
	mm := updated.(*Model)
	if mm.pendingPlayPlaylist {
		t.Fatal("载入完成后应清掉待播放标记")
	}
	if cmd == nil {
		t.Fatal("载入完成后应自动起播")
	}
	if mm.current == nil || mm.current.ID != items[0].ID || mm.queueIdx != 0 {
		t.Fatalf("应从第一首起播，实际 current=%+v idx=%d", mm.current, mm.queueIdx)
	}
}

// TestMouseClickQualityCycles 验证点击底部音质档位等同按 y 切换音质偏好，
// 且不会误切播放模式；两次点击后再次更新位置仍可命中。
func TestMouseClickQualityCycles(t *testing.T) {
	m := fixtureModel()
	m.quality = source.QualityAuto
	beforeMode := m.mode

	zone := findZone(t, m, hitQuality)
	updated, _ := m.Update(tea.MouseClickMsg{X: zone.x + 1, Y: zone.y, Button: tea.MouseLeft})
	mm := updated.(*Model)
	if mm.quality != source.QualityHigh {
		t.Fatalf("点击音质应切到高音质，实际 %v", mm.quality)
	}
	if mm.mode != beforeMode {
		t.Fatalf("点击音质不应切换播放模式：%v -> %v", beforeMode, mm.mode)
	}
	if !strings.Contains(mm.note.text, "音质偏好") {
		t.Fatalf("应提示音质偏好已切换，实际 %q", mm.note.text)
	}

	zone = findZone(t, mm, hitQuality) // 标签宽度变化后重新取位置
	updated, _ = mm.Update(tea.MouseClickMsg{X: zone.x + 1, Y: zone.y, Button: tea.MouseLeft})
	mm = updated.(*Model)
	if mm.quality != source.QualityLossless {
		t.Fatalf("再次点击应切到无损，实际 %v", mm.quality)
	}

	// 音质与模式两个命中区不能重叠。
	qz, qw, qok := mm.qualityZone()
	mz, _, mok := mm.modeZone()
	if qok && mok && qz+qw > mz {
		t.Fatalf("音质区与模式区重叠：quality=%d..%d mode=%d", qz, qz+qw, mz)
	}
}

// TestQualityZoneHiddenWhenMetaTruncated 窄终端 meta 被截断时不注册音质命中区。
func TestQualityZoneHiddenWhenMetaTruncated(t *testing.T) {
	m := fixtureModel()
	m.width = 30
	if _, _, ok := m.qualityZone(); ok {
		t.Fatal("meta 被截断时不应有音质命中区")
	}
}
