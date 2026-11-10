package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

func newLocalTUI(t *testing.T) (*Model, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "tui-local.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	m := fixtureModel()
	m.store = st
	return m, st
}

// TestLibraryMarksFolderAndLocalOnlyPlaylists 验证侧栏徽标与头部状态。
func TestLibraryMarksFolderAndLocalOnlyPlaylists(t *testing.T) {
	m, st := newLocalTUI(t)
	folder, err := st.CreateFolderPlaylist("我的文件夹", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	localOnly, err := st.CreatePlaylistEx("这里只有本机", true)
	if err != nil {
		t.Fatal(err)
	}
	m.playlists = []store.Playlist{folder, localOnly}
	m.playlistIdx = 0
	m.libraryFocus = focusPlaylists

	body := ansi.Strip(m.View().Content)
	for _, want := range []string{"[夹]", "我的文件夹", "[仅]", "这里只有本机"} {
		if !strings.Contains(body, want) {
			t.Fatalf("侧栏应显示 %q，实际：\n%s", want, body)
		}
	}
	// 选中仅本机歌单时，主区标题应说明它不同步。
	m.playlistIdx = 1
	body = ansi.Strip(m.View().Content)
	if !strings.Contains(body, "仅本机") {
		t.Fatalf("仅本机歌单标题应提示：\n%s", body)
	}
}

// TestFolderPlaylistIsReadOnlyInTUI 验证 d/a 在文件夹歌单上被拒绝并提示。
func TestFolderPlaylistIsReadOnlyInTUI(t *testing.T) {
	m, st := newLocalTUI(t)
	dir := t.TempDir()
	folder, err := st.CreateFolderPlaylist("", dir)
	if err != nil {
		t.Fatal(err)
	}
	song, err := st.UpsertSong(store.Song{Source: "local", RemoteID: "m:one", Page: 1, Title: "本地曲目"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceLibraryItems(folder.ID, []store.LibraryItem{{
		Song: song, Path: filepath.Join(dir, "one.flac"), ContentHash: "h",
	}}); err != nil {
		t.Fatal(err)
	}
	m.playlists = []store.Playlist{folder}
	m.playlistIdx = 0
	m.libraryFocus = focusSongs
	m.songs = []store.Song{song}
	m.songIdx = 0

	updated, _ := m.Update(keyPress("d", 'd'))
	m = updated.(*Model)
	if !m.note.isErr || !strings.Contains(m.note.text, "只读") {
		t.Fatalf("文件夹歌单删歌应被拒绝并提示只读: %+v", m.note)
	}
}

// TestPickerExcludesFoldersAndNonLocalOnlyForLocalSongs 验证选目标歌单时的过滤。
func TestPickerExcludesFoldersAndNonLocalOnlyForLocalSongs(t *testing.T) {
	m, st := newLocalTUI(t)
	synced, _ := st.CreatePlaylist("同步歌单")
	localOnly, _ := st.CreatePlaylistEx("仅本机", true)
	folder, _ := st.CreateFolderPlaylist("", t.TempDir())
	m.playlists = []store.Playlist{synced, localOnly, folder}
	m.picker = &targetPickerState{
		mode:  targetPickerAddSong,
		songs: []store.Song{{ID: "local:m:x:1", Source: "local", RemoteID: "m:x", Page: 1, Title: "本地"}},
	}
	targets := m.localTargets(false, "")
	for _, target := range targets {
		if target.Create {
			continue
		}
		if target.Kind == store.PlaylistKindFolder {
			t.Fatalf("文件夹歌单不应作为加歌目标: %+v", targets)
		}
		if target.ID == synced.ID {
			t.Fatalf("本地曲目不应能加入同步歌单: %+v", targets)
		}
	}
	foundLocalOnly := false
	for _, target := range targets {
		if target.ID == localOnly.ID {
			foundLocalOnly = true
		}
	}
	if !foundLocalOnly {
		t.Fatalf("仅本机歌单应可选: %+v", targets)
	}
}

func newLyricBindModel(t *testing.T) *Model {
	m, st := newLocalTUI(t)
	song, err := st.UpsertSong(store.Song{Source: "bilibili", RemoteID: "BV1", Page: 1, Title: "晴天", Artist: "周杰伦", Duration: 200})
	if err != nil {
		t.Fatal(err)
	}
	m.current = &song
	m.songs = []store.Song{song}
	m.lyricSource = "B 站字幕"
	m.lyricOffsetKey = store.BindingAuto
	m.lyricBind = &lyricBindState{
		song:  song,
		query: "晴天 周杰伦",
		cands: []app.LyricCandidate{
			{Kind: store.BindingKindSource, Source: source.NetEase, ExternalID: "1", Title: "晴天", Artist: "周杰伦", Duration: 200, Mark: "netease"},
			{Kind: store.BindingKindLRCLIB, LRCLIBID: 9, Title: "晴天", Artist: "周杰伦", Duration: 201, HasSynced: true, Known: true, Mark: "lrclib"},
			{Kind: store.BindingKindLRCLIB, LRCLIBID: 10, Title: "晴天（纯文本）", Artist: "x", Known: true, Mark: "lrclib"},
		},
	}
	return m
}

// TestLyricBindOverlayFitsSmallAndWide 验证浮层在两种终端尺寸下不超宽高。
func TestLyricBindOverlayFitsSmallAndWide(t *testing.T) {
	for _, size := range [][2]int{{60, 18}, {100, 30}} {
		m := newLyricBindModel(t)
		m.width, m.height = size[0], size[1]
		lines := assertFrame(t, m.View().Content, size[0], size[1])
		body := ansi.Strip(strings.Join(lines, "\n"))
		for _, want := range []string{"歌词来源", "搜索", "网易云", "LRCLIB", "enter 绑定并预览"} {
			if !strings.Contains(body, want) {
				t.Fatalf("%dx%d 浮层应包含 %q：\n%s", size[0], size[1], want, body)
			}
		}
	}
}

// TestLyricBindPreviewAndLocalOverlaysFit 覆盖预览、输入与本机曲目列表三种状态。
func TestLyricBindPreviewAndLocalOverlaysFit(t *testing.T) {
	states := []func(b *lyricBindState){
		func(b *lyricBindState) {
			b.stage = bindStagePreview
			b.preview = app.LyricPreview{
				Label: "绑定 · 网易云歌词",
				Count: 2,
				Lines: []lyrics.Line{{StartMS: 1000, Text: "第一句"}, {StartMS: 2000, Text: "第二句"}},
			}
		},
		func(b *lyricBindState) {
			b.stage = bindStageInput
			b.inputHint = "粘贴链接/ID"
			b.input = "netease:123"
		},
		func(b *lyricBindState) {
			b.stage = bindStageLocalSongs
			b.localSongs = []store.Song{{ID: "local:a:1", Source: "local", Title: "本机一"}, {ID: "local:b:1", Source: "local", Title: "本机二"}}
		},
	}
	for _, mutate := range states {
		m := newLyricBindModel(t)
		m.width, m.height = 60, 18
		mutate(m.lyricBind)
		assertFrame(t, m.View().Content, 60, 18)
	}
}

// TestLyricBindKeys 验证 B 打开、esc 关闭、tab 切源、x 解除。
func TestLyricBindKeys(t *testing.T) {
	m, _ := newLocalTUI(t)
	song, _ := m.store.UpsertSong(store.Song{Source: "bilibili", RemoteID: "BV1", Page: 1, Title: "晴天", Duration: 200})
	m.current = &song
	m.songs = []store.Song{song}

	updated, cmd := m.Update(keyPress("B", 'B'))
	m = updated.(*Model)
	if m.lyricBind == nil || cmd == nil {
		t.Fatal("B 应打开歌词来源浮层并开始搜索")
	}
	if m.lyricBind.filter != "" {
		t.Fatalf("默认应查全部来源: %q", m.lyricBind.filter)
	}
	updated, _ = m.Update(keyPress("", tea.KeyTab))
	m = updated.(*Model)
	if m.lyricBind.filter != "netease" {
		t.Fatalf("tab 应切到网易云: %q", m.lyricBind.filter)
	}
	updated, _ = m.Update(keyPress("", tea.KeyEscape))
	m = updated.(*Model)
	if m.lyricBind != nil {
		t.Fatal("esc 应关闭浮层")
	}
}

// TestLyricOffsetWritesBindingScope 验证 offset 随当前来源写入。
func TestLyricOffsetWritesBindingScope(t *testing.T) {
	m, st := newLocalTUI(t)
	song, _ := st.UpsertSong(store.Song{Source: "bilibili", RemoteID: "BV1", Page: 1, Title: "晴天", Duration: 200})
	m.current = &song
	if err := st.SaveLyricBinding(song.ID, store.LyricBinding{Kind: store.BindingKindLocalFile, Path: "/x.lrc"}); err != nil {
		t.Fatal(err)
	}
	m.lyricBase = []lyrics.Line{{StartMS: 1000, Text: "一句"}}
	m.lyricOffsetKey = store.BindingLocal
	m.shiftLyrics(500)
	if binding, ok := st.LocalLyricBinding(song.ID); !ok || binding.OffsetMS != 500 {
		t.Fatalf("offset 应写进本机绑定: %+v ok=%v", binding, ok)
	}
	if got := st.LyricOffset(song.ID, store.BindingAuto); got != 0 {
		t.Fatalf("本机绑定 offset 不应写到 auto: %d", got)
	}
	m.lyricOffsetKey = store.BindingAuto
	m.shiftLyrics(-500)
	if binding, _ := st.LocalLyricBinding(song.ID); binding.OffsetMS != 500 {
		t.Fatalf("切到 auto 后不应再改本机绑定: %+v", binding)
	}
}

// TestLyricsPageShowsSourceAndOffset 验证歌词页底部的来源/偏移状态行。
func TestLyricsPageShowsSourceAndOffset(t *testing.T) {
	m := fixtureModel()
	m.view = viewLyrics
	m.lyricLines = []lyrics.Line{{StartMS: 0, Text: "第一句"}, {StartMS: 1000, Text: "第二句"}}
	m.lyricIdx = 1
	m.lyricSource = "绑定 · 网易云歌词"
	m.lyricBound = true
	m.lyricOffsetMS = 500
	m.width, m.height = 100, 30
	lines := assertFrame(t, m.View().Content, 100, 30)
	footer := ansi.Strip(lines[len(lines)-1])
	_ = footer
	body := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(body, "已绑定 · 网易云歌词") || !strings.Contains(body, "偏移 +0.5s") {
		t.Fatalf("歌词页底部应显示来源与偏移：\n%s", body)
	}
}

// TestLyricMsgDropsStaleGeneration 防止慢请求覆盖新绑定。
func TestLyricMsgDropsStaleGeneration(t *testing.T) {
	m := fixtureModel()
	m.lyricGen = 5
	m.current = &m.songs[0]
	m.lyricLines = []lyrics.Line{{StartMS: 0, Text: "旧"}}
	updated, _ := m.Update(lyricMsg{songID: m.current.ID, lines: []lyrics.Line{{StartMS: 0, Text: "新"}}, gen: 4})
	m = updated.(*Model)
	if len(m.lyricLines) != 1 || m.lyricLines[0].Text != "旧" {
		t.Fatalf("过期响应应被丢弃: %+v", m.lyricLines)
	}
}

// TestLyricMsgMarksDegraded 验证绑定失效降级时的提示。
func TestLyricMsgMarksDegraded(t *testing.T) {
	m := fixtureModel()
	m.lyricGen = 2
	m.current = &m.songs[0]
	updated, _ := m.Update(lyricMsg{
		songID: m.current.ID, lines: []lyrics.Line{{StartMS: 0, Text: "自动"}},
		gen: 2, degraded: true, warning: "绑定失效，已回退", offsetKey: store.BindingAuto,
	})
	m = updated.(*Model)
	if !m.lyricDegraded || m.note.text != "绑定失效，已回退" || !m.note.isWarn {
		t.Fatalf("降级提示缺失: %+v", m.note)
	}
}

// TestFolderPlaylistFitsNarrowFrame 验证窄终端下文件夹歌单与长曲名不溢出。
func TestFolderPlaylistFitsNarrowFrame(t *testing.T) {
	m, st := newLocalTUI(t)
	folder, err := st.CreateFolderPlaylist("一个很长很长很长的文件夹歌单名字", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.playlists = []store.Playlist{folder}
	m.playlistIdx = 0
	m.songs = []store.Song{{ID: "local:m:x:1", Source: "local", RemoteID: "m:x", Page: 1, Title: "一个很长很长的本地曲目标题用于测试截断", Artist: "本机"}}
	m.songsBusy = false
	m.width, m.height = 60, 18
	lines := assertFrame(t, m.View().Content, 60, 18)
	body := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(body, "[夹]") {
		t.Fatalf("窄屏也应显示文件夹徽标：\n%s", body)
	}
}

// TestLyricBindKeywordEditing 覆盖「搜索歌词时改关键字」的完整按键路径。
func TestLyricBindKeywordEditing(t *testing.T) {
	m := newLyricBindModel(t)
	m.width, m.height = 100, 30
	m.lyricBind.query = "晴天 周杰伦"
	m.lyricBind.editing = false

	// 未进入编辑态时字母是快捷键，不会改搜索词。
	updated, _ := m.Update(keyPress("a", 'a'))
	m = updated.(*Model)
	if m.lyricBind.query != "晴天 周杰伦" {
		t.Fatalf("未进入编辑态时不应改搜索词: %q", m.lyricBind.query)
	}

	// / 进入编辑，光标在末尾，可以继续追加。
	updated, _ = m.Update(keyPress("/", '/'))
	m = updated.(*Model)
	if !m.lyricBind.editing || m.lyricBind.cursor != len([]rune(m.lyricBind.query)) {
		t.Fatalf("/ 应进入编辑并把光标放到末尾: editing=%v cursor=%d", m.lyricBind.editing, m.lyricBind.cursor)
	}
	for _, r := range "(live)" {
		updated, _ = m.Update(keyPress(string(r), r))
		m = updated.(*Model)
	}
	if !strings.HasSuffix(m.lyricBind.query, "(live)") {
		t.Fatalf("输入应追加到末尾: %q", m.lyricBind.query)
	}

	// 左移到末尾字符前插入，证明不是简单追加。
	updated, _ = m.Update(keyPress("", tea.KeyLeft))
	m = updated.(*Model)
	updated, _ = m.Update(keyPress("X", 'X'))
	m = updated.(*Model)
	if !strings.HasSuffix(m.lyricBind.query, "X)") {
		t.Fatalf("左移后应在中间插入: %q", m.lyricBind.query)
	}
	// backspace 删除光标前一个字符（刚插入的 X）。
	updated, _ = m.Update(keyPress("", tea.KeyBackspace))
	m = updated.(*Model)
	if !strings.HasSuffix(m.lyricBind.query, "(live)") {
		t.Fatalf("backspace 应删除光标前字符: %q", m.lyricBind.query)
	}

	// ctrl+u 清空。
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 'u', Mod: tea.ModCtrl}))
	m = updated.(*Model)
	if m.lyricBind.query != "" || m.lyricBind.cursor != 0 {
		t.Fatalf("ctrl+u 应清空搜索词: %q cursor=%d", m.lyricBind.query, m.lyricBind.cursor)
	}

	// esc 只退出编辑，浮层保留。
	updated, _ = m.Update(keyPress("", tea.KeyEscape))
	m = updated.(*Model)
	if m.lyricBind == nil || m.lyricBind.editing {
		t.Fatal("esc 应退出编辑而不关闭浮层")
	}

	// enter 触发一次新的候选搜索。
	m.lyricBind.query = "晴天"
	m.lyricBind.editing = true
	updated, cmd := m.Update(keyPress("", tea.KeyEnter))
	m = updated.(*Model)
	if m.lyricBind.editing || cmd == nil {
		t.Fatal("enter 应退出编辑并重新搜索")
	}
}

// TestLyricBindEditHintsRendered 验证浮层里能看到「/ 编辑」与编辑态提示。
func TestLyricBindEditHintsRendered(t *testing.T) {
	m := newLyricBindModel(t)
	m.width, m.height = 100, 30
	body := ansi.Strip(m.View().Content)
	if !strings.Contains(body, "/ 编辑") {
		t.Fatalf("非编辑态应提示 / 编辑：\n%s", body)
	}
	m.lyricBind.editing = true
	m.lyricBind.cursor = 2
	body = ansi.Strip(m.View().Content)
	if !strings.Contains(body, "ctrl+u 清空") {
		t.Fatalf("编辑态应显示编辑提示：\n%s", body)
	}
	if !strings.Contains(body, "搜索 晴天▏ 周杰伦") {
		t.Fatalf("光标应显示在搜索词中间：\n%s", body)
	}
}
