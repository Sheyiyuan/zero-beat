package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

func TestDownloadSelectedKeyQueuesSelectedSong(t *testing.T) {
	m := fixtureModel()
	m.songIdx = 1

	updated, cmd := m.Update(keyPress("W", 'W'))
	mm := updated.(*Model)
	if len(mm.downloads) != 1 {
		t.Fatalf("按 w 后应有一个下载任务，实际 %d", len(mm.downloads))
	}
	task := mm.downloads[0]
	if task.title != "夜航星" || task.state != downloadRunning {
		t.Fatalf("任务不符: title=%q state=%d", task.title, task.state)
	}
	if task.ch == nil {
		t.Fatal("运行中的任务应创建进度通道")
	}
	if cmd == nil {
		t.Fatal("开始下载应返回 tea.Cmd")
	}

	// 同一首歌重复按 W 不重复入队。
	updated, _ = mm.Update(keyPress("W", 'W'))
	mm = updated.(*Model)
	if len(mm.downloads) != 1 {
		t.Fatalf("重复下载同一首歌不应重复入队，实际 %d", len(mm.downloads))
	}
	if !strings.Contains(mm.note.text, "已在下载队列") {
		t.Fatalf("应提示已在队列，实际 %q", mm.note.text)
	}
}

func TestDownloadKeyUsesSearchSelection(t *testing.T) {
	m := fixtureModel()
	m.view = viewSearch
	m.searchEditing = false
	m.searchMode = searchSource
	m.searchResults = []source.Candidate{{
		Track: source.Track{Source: source.NetEase, ExternalID: "2", Page: 1, Title: "晴天", Artist: "周杰伦", Duration: 269},
	}}
	m.searchIdx = 0

	updated, _ := m.Update(keyPress("W", 'W'))
	mm := updated.(*Model)
	if len(mm.downloads) != 1 {
		t.Fatalf("搜索页按 W 应下载选中结果，实际 %d", len(mm.downloads))
	}
	task := mm.downloads[0]
	if task.song.Title != "晴天" || task.song.Source != "netease" {
		t.Fatalf("下载的搜索项不符: %+v", task.song)
	}
}

func TestDownloadProgressLineAndBodyHeight(t *testing.T) {
	m := fixtureModel()
	m.downloads = []*downloadTask{{
		id:    1,
		song:  store.Song{ID: "bilibili:BV2:1", Source: "bilibili", BVID: "BV2", Page: 1, Title: "夜航星"},
		title: "夜航星",
		state: downloadRunning,
		done:  3 << 20,
		total: 10 << 20,
	}}
	if got := m.bodyHeight(); got != 22 {
		t.Fatalf("有下载行时正文高度应为 22，实际 %d", got)
	}
	lines := assertFrame(t, strings.Join(m.render(), "\n"), 100, 30)
	body := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(body, "夜航星") || !strings.Contains(body, "30%") || !strings.Contains(body, "3.0 MiB") {
		t.Fatalf("底部应显示下载进度: %q", body)
	}

	updated, cmd := m.Update(downloadMsg{id: 1, done: 7 << 20, total: 10 << 20})
	mm := updated.(*Model)
	if mm.downloads[0].done != 7<<20 {
		t.Fatalf("进度未更新: %d", mm.downloads[0].done)
	}
	if cmd == nil {
		t.Fatal("进度消息后应继续等待下一条")
	}

	updated, _ = mm.Update(downloadMsg{id: 1, finished: true, path: "/tmp/夜航星.mp3", size: 10 << 20})
	mm = updated.(*Model)
	if len(mm.downloads) != 0 {
		t.Fatalf("完成后应移除任务，实际 %d", len(mm.downloads))
	}
	if !strings.Contains(mm.note.text, "已下载") {
		t.Fatalf("完成后应提示已下载，实际 %q", mm.note.text)
	}
	if got := mm.bodyHeight(); got != 23 {
		t.Fatalf("下载行消失后正文高度应恢复为 23，实际 %d", got)
	}
}

func TestDownloadQueueLimit(t *testing.T) {
	m := fixtureModel()
	m.songs = append(m.songs, store.Song{ID: "bilibili:BV3:1", Source: "bilibili", BVID: "BV3", Page: 1, Title: "第三首"})

	for _, idx := range []int{0, 1, 2} {
		m.songIdx = idx
		updated, _ := m.Update(keyPress("W", 'W'))
		m = updated.(*Model)
	}
	if len(m.downloads) != 3 {
		t.Fatalf("应有三个任务，实际 %d", len(m.downloads))
	}
	running, queued := 0, 0
	for _, task := range m.downloads {
		if task.state == downloadRunning {
			running++
		} else {
			queued++
		}
	}
	if running != maxConcurrentDownloads || queued != 1 {
		t.Fatalf("并发限制不符: running=%d queued=%d", running, queued)
	}
	line, ok := m.downloadLineText()
	if !ok || !strings.Contains(ansi.Strip(line), "另 2 个") {
		t.Fatalf("多任务时下载行应显示剩余数量: %q", ansi.Strip(line))
	}
}

func TestDownloadLineHiddenOnHelpAndLogin(t *testing.T) {
	m := fixtureModel()
	m.downloads = []*downloadTask{{id: 1, title: "夜航星", state: downloadRunning}}
	for _, v := range []view{viewHelp, viewLogin} {
		m.view = v
		if _, ok := m.downloadLineText(); ok {
			t.Fatalf("%s 页不应显示下载行", viewNames[v])
		}
	}
}

func TestDownloadCurrentKeyIgnoresSelection(t *testing.T) {
	m := fixtureModel()
	m.songIdx = 1 // 选中夜航星，但当前播放是 Never Gonna Give You Up

	updated, _ := m.Update(keyPress("w", 'w'))
	mm := updated.(*Model)
	if len(mm.downloads) != 1 {
		t.Fatalf("按 w 应下载当前播放，实际 %d", len(mm.downloads))
	}
	if mm.downloads[0].song.Title != "Never Gonna Give You Up" {
		t.Fatalf("w 不应下载选中项，实际 %q", mm.downloads[0].song.Title)
	}
}

func TestDownloadWholePlaylistWithWOnPlaylistFocus(t *testing.T) {
	m := fixtureModel()
	m.libraryFocus = focusPlaylists

	updated, cmd := m.Update(keyPress("W", 'W'))
	mm := updated.(*Model)
	if len(mm.downloads) != 2 {
		t.Fatalf("歌单焦点按 W 应入队整个歌单，实际 %d", len(mm.downloads))
	}
	if cmd == nil {
		t.Fatal("歌单下载应返回 tea.Cmd")
	}
	if !strings.Contains(mm.note.text, "已加入歌单下载：2 首") {
		t.Fatalf("应提示歌单下载数量，实际 %q", mm.note.text)
	}
}

func TestLibraryFocusNavigation(t *testing.T) {
	m := fixtureModel()
	if m.libraryFocus != focusSongs {
		t.Fatal("默认焦点应在歌曲列表")
	}

	updated, _ := m.Update(keyPress("h", 'h'))
	mm := updated.(*Model)
	if mm.libraryFocus != focusPlaylists {
		t.Fatal("h 应把焦点切到歌单")
	}
	if mm.playlistIdx != 0 {
		t.Fatal("切换焦点不应改变歌单选择")
	}

	updated, cmd := mm.Update(keyPress("j", 'j'))
	mm = updated.(*Model)
	if mm.playlistIdx != 1 {
		t.Fatalf("歌单焦点下 j 应移动到下一个歌单，实际 %d", mm.playlistIdx)
	}
	if cmd == nil || !mm.songsBusy {
		t.Fatal("切换歌单应加载曲目")
	}

	updated, _ = mm.Update(keyPress("l", 'l'))
	mm = updated.(*Model)
	if mm.libraryFocus != focusSongs {
		t.Fatal("l 应把焦点切到歌曲")
	}
}

func TestLibraryFocusGuardsSongAndPlaylistActions(t *testing.T) {
	m := fixtureModel()
	m.libraryFocus = focusPlaylists
	updated, _ := m.Update(keyPress("a", 'a'))
	mm := updated.(*Model)
	if !strings.Contains(mm.note.text, "先按 →") {
		t.Fatalf("歌单焦点下歌曲操作应提示切换焦点，实际 %q", mm.note.text)
	}

	m = fixtureModel()
	m.libraryFocus = focusSongs
	updated, _ = m.Update(keyPress("D", 'D'))
	mm = updated.(*Model)
	if !strings.Contains(mm.note.text, "先按 ←") {
		t.Fatalf("歌曲焦点下歌单操作应提示切换焦点，实际 %q", mm.note.text)
	}
	if mm.prompt != promptNone {
		t.Fatalf("不应进入删除歌单确认，实际 %v", mm.prompt)
	}
}

func playlistRowText(p store.Playlist, sideW int, playing bool) string {
	marker := "  "
	if playing {
		marker = "▶ "
	}
	prefix := marker + " "
	if p.Kind == "remote" && p.Source != "" {
		mark := "?"
		switch source.ID(p.Source) {
		case source.Bilibili:
			mark = "B"
		case source.NetEase:
			mark = "云"
		}
		prefix = marker + " " + padRight(mark, 2) + " "
	}
	label := prefix + p.Title
	if p.Count > 0 {
		label = fmt.Sprintf("%s%s %d", prefix, p.Title, p.Count)
	}
	return padRight(truncate(label, sideW), sideW)
}

func stylePrefix(style lipgloss.Style) string {
	rendered := style.Render("X")
	return rendered[:strings.Index(rendered, "X")]
}

func TestLibraryFocusStyles(t *testing.T) {
	m := fixtureModel()
	m.playingPlaylistID = "local-default"
	m.playlistIdx = 0
	m.libraryFocus = focusSongs

	rows := m.renderLibrary(10)
	playingRow := playlistRowText(m.playlists[1], 20, true)
	if !strings.Contains(rows[2], st.playing.Render(playingRow)) {
		t.Fatalf("歌曲焦点下，正在播放歌单应为灰底: %q", ansi.Strip(rows[2]))
	}
	selectedRow := playlistRowText(m.playlists[0], 20, false)
	if strings.Contains(rows[1], st.selected.Render(selectedRow)) {
		t.Fatal("歌曲焦点下不应高亮歌单")
	}
	if !strings.HasPrefix(m.songRow(0, 60), stylePrefix(st.selected)) {
		t.Fatal("歌曲焦点下选中歌曲应为蓝底")
	}

	m.libraryFocus = focusPlaylists
	rows = m.renderLibrary(10)
	if !strings.HasPrefix(rows[1], st.selected.Render(selectedRow)) {
		t.Fatalf("歌单焦点下选中歌单应为蓝底: %q", ansi.Strip(rows[1]))
	}
	if !strings.Contains(rows[2], st.playing.Render(playingRow)) {
		t.Fatalf("歌单焦点下正在播放歌单应仍为灰底: %q", ansi.Strip(rows[2]))
	}
	if !strings.HasPrefix(m.songRow(0, 60), stylePrefix(st.playing)) {
		t.Fatal("歌单焦点下当前歌曲应为灰底而不是选中蓝底")
	}
}

func TestPlayPlaylistRecordsOrigin(t *testing.T) {
	m := fixtureModel()
	m.playlistIdx = 1 // local-default
	_ = m.playPlaylistIndex(0)
	if m.playingPlaylistID != "local-default" {
		t.Fatalf("从歌单起播应记录来源歌单，实际 %q", m.playingPlaylistID)
	}
}

func TestPlayingPlaylistHasIconAndIndent(t *testing.T) {
	m := fixtureModel()
	m.playingPlaylistID = "local-default"
	rows := m.renderLibrary(10)
	side := func(row string) string {
		return strings.SplitN(ansi.Strip(row), "│", 2)[0]
	}
	if !strings.Contains(side(rows[2]), "▶  本地歌单") {
		t.Fatalf("正在播放的歌单应有播放图标和缩进: %q", side(rows[2]))
	}
	if strings.Contains(side(rows[1]), "▶") {
		t.Fatalf("非播放歌单不应有播放图标: %q", side(rows[1]))
	}
	if !strings.HasPrefix(side(rows[1]), "   我喜欢的音乐") {
		t.Fatalf("歌单左侧应留出图标位: %q", side(rows[1]))
	}
}
