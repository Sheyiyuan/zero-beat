package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

func ctrlPress(code rune, text string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Text: text, Mod: tea.ModCtrl})
}

func TestSelectionToggleAndEscape(t *testing.T) {
	m := fixtureModel()

	updated, _ := m.Update(keyPress("z", 'z'))
	mm := updated.(*Model)
	if mm.selectionCount() != 1 || !mm.isSongSelected(mm.songs[0]) {
		t.Fatalf("z 应标记当前歌曲，实际 count=%d", mm.selectionCount())
	}
	if !strings.Contains(mm.View().Content, "已选 1 首") {
		t.Fatal("标记后标题/提示里应显示已选数量")
	}

	updated, _ = mm.Update(keyPress("z", 'z'))
	mm = updated.(*Model)
	if mm.hasSelection() {
		t.Fatalf("再按 z 应取消标记，实际 count=%d", mm.selectionCount())
	}

	mm.toggleSelectionAt(0)
	updated, _ = mm.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	mm = updated.(*Model)
	if mm.hasSelection() {
		t.Fatal("esc 应取消批量选择")
	}
}

func TestSelectionSelectAllAndToggle(t *testing.T) {
	m := fixtureModel()
	m.songs = append(m.songs, store.Song{ID: "bilibili:BV3:1", Source: "bilibili", BVID: "BV3", Page: 1, Title: "C"})

	updated, _ := m.Update(ctrlPress('a', "a"))
	mm := updated.(*Model)
	if mm.selectionCount() != len(mm.songs) {
		t.Fatalf("ctrl+a 应全选 %d 首，实际 %d", len(mm.songs), mm.selectionCount())
	}

	updated, _ = mm.Update(ctrlPress('a', "a"))
	mm = updated.(*Model)
	if mm.hasSelection() {
		t.Fatal("已全选时再按 ctrl+a 应取消全选")
	}
}

func TestSelectionCtrlSpaceAndCtrlARealKeys(t *testing.T) {
	m := fixtureModel()
	// 真实终端里 ctrl+space 解出 Code=space、Text 为空；ctrl+a 解出 Code='a'、Text 为空。
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl}))
	mm := updated.(*Model)
	if mm.selectionCount() != 1 {
		t.Fatalf("ctrl+space 应标记当前歌曲而不是暂停播放，实际 count=%d", mm.selectionCount())
	}
	updated, _ = mm.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModCtrl}))
	mm = updated.(*Model)
	if mm.selectionCount() != len(mm.songs) {
		t.Fatalf("ctrl+a 应全选，实际 count=%d", mm.selectionCount())
	}
}

func TestSelectionScopeResetsWhenPlaylistChanges(t *testing.T) {
	m := fixtureModel()
	m.toggleSelectionAt(0)
	if !m.hasSelection() {
		t.Fatal("前置条件：应已标记一首")
	}
	m.selectPlaylist(1)
	if m.hasSelection() {
		t.Fatal("切换歌单后旧选择应失效")
	}
}

func TestSelectionBatchDownload(t *testing.T) {
	m := fixtureModel()
	m.downloads = testDownloads(t)
	m.toggleSelectionAt(0)
	m.toggleSelectionAt(1)

	updated, cmd := m.Update(keyPress("W", 'W'))
	mm := updated.(*Model)
	tasks := mm.downloads.Tasks()
	if len(tasks) != 2 {
		t.Fatalf("选中两首按 W 应入队两个下载任务，实际 %d", len(tasks))
	}
	if cmd == nil {
		t.Fatal("批量下载应返回下载事件监听命令")
	}
	if !strings.Contains(mm.note.text, "已加入下载：2 首") {
		t.Fatalf("应提示批量下载数量，实际 %q", mm.note.text)
	}
}

func TestSelectionBatchDownloadSkipsDownloaded(t *testing.T) {
	m := fixtureModel()
	m.downloads = testDownloads(t)
	m.toggleSelectionAt(0)
	m.toggleSelectionAt(1)

	// fixture 没有真实下载索引，两首都会入队；重复按 W 时第二首已在队列。
	updated, _ := m.Update(keyPress("W", 'W'))
	mm := updated.(*Model)
	updated, _ = mm.Update(keyPress("W", 'W'))
	mm = updated.(*Model)
	if len(mm.downloads.Tasks()) != 2 {
		t.Fatalf("重复批量下载不应重复入队，实际 %d", len(mm.downloads.Tasks()))
	}
	if !strings.Contains(mm.note.text, "已在队列") && !strings.Contains(mm.note.text, "已下载") {
		t.Fatalf("重复下载应提示跳过原因，实际 %q", mm.note.text)
	}
}

func TestSelectionBatchEnqueueAndInsertNext(t *testing.T) {
	m := fixtureModel()
	m.queue = threeSongQueue()
	m.current = &m.queue[0]
	m.queueIdx = 0
	m.queueCursor = 0
	m.songs = append([]store.Song(nil), m.queue...)
	m.songIdx = 0
	m.toggleSelectionAt(1)
	m.toggleSelectionAt(2)

	_ = m.insertSelectedNext()
	if len(m.queue) != 3 || m.queue[1].BVID != "BV2" || m.queue[2].BVID != "BV3" {
		t.Fatalf("批量下一首播放顺序不符: %+v", m.queue)
	}
	if !strings.Contains(m.note.text, "已插到下一首：2 首") {
		t.Fatalf("应提示批量下一首数量，实际 %q", m.note.text)
	}

	// 选择仍在 BV2 / BV3 上，继续测试批量入队。
	_ = m.enqueueSelectedSongs()
	if len(m.queue) != 5 {
		t.Fatalf("批量加入队列末尾应追加两首，实际 %d", len(m.queue))
	}
	if !strings.Contains(m.note.text, "已加入队列：2 首") {
		t.Fatalf("应提示批量入队数量，实际 %q", m.note.text)
	}
}

func TestSelectionBatchPlay(t *testing.T) {
	m := fixtureModel()
	m.queue = threeSongQueue()
	m.current = &m.queue[0]
	m.queueIdx = 0
	m.queueCursor = 0
	m.songs = append([]store.Song(nil), m.queue...)
	m.songIdx = 0
	m.toggleSelectionAt(1)
	m.toggleSelectionAt(2)

	cmd := m.playSelectedSongs()
	if cmd == nil {
		t.Fatal("批量播放应返回起播命令")
	}
	if len(m.queue) != 2 || m.queue[0].BVID != "BV2" || m.queue[1].BVID != "BV3" {
		t.Fatalf("批量播放应只把选中项载入队列: %+v", m.queue)
	}
	if m.current == nil || m.current.BVID != "BV2" {
		t.Fatalf("应从选中项第一首开始播放，实际 %+v", m.current)
	}
}

func TestSelectionBatchRemoveSongsFromPlaylist(t *testing.T) {
	st := openFeatureStore(t)
	p, err := st.CreatePlaylist("批量")
	if err != nil {
		t.Fatalf("建歌单失败: %v", err)
	}
	for _, song := range threeSongQueue() {
		if err := st.AddSongToPlaylist(p.ID, song); err != nil {
			t.Fatalf("加歌失败: %v", err)
		}
	}
	songs, err := st.PlaylistSongs(p.ID)
	if err != nil {
		t.Fatalf("读歌单失败: %v", err)
	}

	m := fixtureModel()
	m.store = st
	m.playlists = []store.Playlist{p}
	m.playlistIdx = 0
	m.songs = songs
	m.songIdx = 0
	m.toggleSelectionAt(0)
	m.toggleSelectionAt(1)

	_ = m.removeSelected()
	remaining, err := st.PlaylistSongs(p.ID)
	if err != nil {
		t.Fatalf("读歌单失败: %v", err)
	}
	if len(remaining) != 1 || remaining[0].BVID != "BV3" {
		t.Fatalf("应只删掉选中的两首，剩余 %+v", remaining)
	}
	if m.hasSelection() {
		t.Fatal("删除后应清空选择")
	}
	if !strings.Contains(m.note.text, "已从歌单移除：2 首") {
		t.Fatalf("应提示批量删除数量，实际 %q", m.note.text)
	}
}

func TestSelectionBatchAddOpensPickerAndWritesAll(t *testing.T) {
	st := openFeatureStore(t)
	a, _ := st.CreatePlaylist("A")
	b, _ := st.CreatePlaylist("B")
	m := fixtureModel()
	m.store = st
	m.playlists = []store.Playlist{a, b}
	m.playlistIdx = 0
	m.songs = threeSongQueue()[:2]
	m.songIdx = 0
	m.toggleSelectionAt(0)
	m.toggleSelectionAt(1)

	if cmd := m.openAddSongsPicker(m.selectedSongs()); cmd != nil {
		t.Fatal("没有可写云端音源时不应发起远端请求")
	}
	if m.picker == nil || len(m.picker.songs) != 2 {
		t.Fatalf("选择器应带上两首歌: %+v", m.picker)
	}
	if got := m.picker.displayTitle(); !strings.Contains(got, "已选 2 首") {
		t.Fatalf("选择器标题应显示批量数量，实际 %q", got)
	}
	idx := -1
	for i, item := range m.picker.items {
		if item.ID == b.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("目标选择器里应有 B: %+v", m.picker.items)
	}
	m.picker.idx = idx
	_ = m.chooseTarget()

	targetSongs, err := st.PlaylistSongs(b.ID)
	if err != nil {
		t.Fatalf("读目标歌单失败: %v", err)
	}
	if len(targetSongs) != 2 || targetSongs[0].Title != "A" || targetSongs[1].Title != "B" {
		t.Fatalf("批量加入顺序不符: %+v", targetSongs)
	}
	if !strings.Contains(m.note.text, "已加入歌单：B（2 首）") {
		t.Fatalf("应提示批量加入数量，实际 %q", m.note.text)
	}
}

func TestSelectionNewPlaylistFromPickerAddsAll(t *testing.T) {
	st := openFeatureStore(t)
	m := fixtureModel()
	m.store = st
	m.playlists, _ = st.ListPlaylists()
	m.playlistIdx = 0
	m.songs = threeSongQueue()[:2]
	m.toggleSelectionAt(0)
	m.toggleSelectionAt(1)

	_ = m.openAddSongsPicker(m.selectedSongs())
	idx := -1
	for i, item := range m.picker.items {
		if item.Create {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("目标选择器缺少新建项: %+v", m.picker.items)
	}
	m.picker.idx = idx
	_ = m.chooseTarget()
	if m.prompt != promptNewPlaylist || len(m.pendingAddSongs) != 2 {
		t.Fatalf("应进入新建歌单并保留两首歌: prompt=%v songs=%d", m.prompt, len(m.pendingAddSongs))
	}
	m.promptInput = "新建批量歌单"
	_ = m.commitPrompt()

	lists, err := st.ListPlaylists()
	if err != nil {
		t.Fatalf("读歌单失败: %v", err)
	}
	var id string
	for _, p := range lists {
		if p.Title == "新建批量歌单" {
			id = p.ID
		}
	}
	if id == "" {
		t.Fatalf("新歌单没有建出来: %+v", lists)
	}
	songs, err := st.PlaylistSongs(id)
	if err != nil || len(songs) != 2 {
		t.Fatalf("新歌单应有两首歌: %+v err=%v", songs, err)
	}
	if !strings.Contains(m.note.text, "新建本地歌单并加入：新建批量歌单（2 首）") {
		t.Fatalf("应提示批量新建数量，实际 %q", m.note.text)
	}
}

func TestSelectionBatchContextMenu(t *testing.T) {
	m := fixtureModel()
	m.toggleSelectionAt(0)
	m.toggleSelectionAt(1)

	updated, _ := m.openSongMenu(hitZone{target: hitLibrarySong, index: 0}, 10, 10)
	mm := updated.(*Model)
	if mm.menu == nil || !mm.menu.batch {
		t.Fatalf("右键已勾选歌曲应打开批量菜单，实际 %+v", mm.menu)
	}
	if mm.menu.title != "已选 2 首" {
		t.Fatalf("批量菜单标题不符: %q", mm.menu.title)
	}
	found := false
	for _, item := range mm.menu.items {
		if item.act == actClearSelection {
			found = true
		}
	}
	if !found {
		t.Fatal("批量菜单应有「取消选择」")
	}
}

func TestSelectionMouseCtrlAndShiftClick(t *testing.T) {
	m := fixtureModel()
	var row0, row1 hitZone
	for _, zone := range m.hitZones() {
		switch {
		case zone.target == hitLibrarySong && zone.index == 0:
			row0 = zone
		case zone.target == hitLibrarySong && zone.index == 1:
			row1 = zone
		}
	}
	if row0.w == 0 || row1.w == 0 {
		t.Fatal("未找到歌曲行命中区")
	}

	updated, _ := m.Update(tea.MouseClickMsg{X: row1.x + 1, Y: row1.y, Button: tea.MouseLeft, Mod: tea.ModCtrl})
	mm := updated.(*Model)
	if !mm.isSongSelected(mm.songs[1]) {
		t.Fatal("Ctrl+左键应勾选该行")
	}

	updated, _ = mm.Update(tea.MouseClickMsg{X: row0.x + 1, Y: row0.y, Button: tea.MouseLeft, Mod: tea.ModShift})
	mm = updated.(*Model)
	if mm.selectionCount() != 2 {
		t.Fatalf("Shift+左键应连选两行，实际 %d", mm.selectionCount())
	}
}

func TestSelectionSingleActsOnSelectedNotCursor(t *testing.T) {
	m := fixtureModel()
	m.downloads = testDownloads(t)
	m.songIdx = 1 // 光标停在第二首
	m.toggleSelectionAt(0)

	updated, _ := m.Update(keyPress("W", 'W'))
	mm := updated.(*Model)
	tasks := mm.downloads.Tasks()
	if len(tasks) != 1 || tasks[0].Song.ID != mm.songs[0].ID {
		t.Fatalf("只勾选一首时 W 应下载勾选的那首，实际 %+v", tasks)
	}
}

func TestSelectionBatchCloudAdd(t *testing.T) {
	b, _, reg := twoFakes()
	m := fixtureModel()
	m.sources = reg
	songs := []store.Song{
		{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "A"},
		{ID: "bilibili:BV2:1", Source: "bilibili", BVID: "BV2", Page: 1, Title: "B"},
	}
	cmd := m.addSongsToCloudCmd(songs, playlistTarget{ID: "cloud", Source: "bilibili", RemoteID: "1", Title: "收藏", Cloud: true})
	if cmd == nil {
		t.Fatal("应返回云端写入命令")
	}
	msg, ok := cmd().(playlistAddMsg)
	if !ok {
		t.Fatal("应返回 playlistAddMsg")
	}
	if msg.count != 2 || msg.err != nil {
		t.Fatalf("应成功写入两首: count=%d err=%v", msg.count, msg.err)
	}
	if b.addedTo != "1" || b.addedRef.ExternalID != "BV2" {
		t.Fatalf("应逐首写入同一个云端歌单，最后一首应为 BV2: remote=%q ref=%+v", b.addedTo, b.addedRef)
	}
}

func TestSelectionFrameNarrow(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 40, 12
	m.toggleSelectionAt(0)
	m.toggleSelectionAt(1)
	assertFrame(t, m.View().Content, 40, 12)
}

func TestSelectionFrameShowsMarkerAndCount(t *testing.T) {
	m := fixtureModel()
	m.toggleSelectionAt(0)
	m.toggleSelectionAt(1)

	lines := assertFrame(t, m.View().Content, 100, 30)
	body := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"✓", "已选 2 首", "加入歌单"} {
		if !strings.Contains(body, want) {
			t.Fatalf("选中状态界面应出现 %q: %q", want, body)
		}
	}
}
