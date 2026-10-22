package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/lrclib"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

func threeSongQueue() []store.Song {
	return []store.Song{
		{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "A", Duration: 100},
		{ID: "bilibili:BV2:1", Source: "bilibili", BVID: "BV2", Page: 1, Title: "B", Duration: 200},
		{ID: "bilibili:BV3:1", Source: "bilibili", BVID: "BV3", Page: 1, Title: "C", Duration: 300},
	}
}

func TestQueueRestorePreservesPosition(t *testing.T) {
	m := fixtureModel()
	m.current = nil
	m.queue = nil
	m.state = audio.StateIdle
	songs := threeSongQueue()

	updated, _ := m.Update(queueRestoredMsg{songs: songs, index: 1, position: 90 * time.Second})
	mm := updated.(*Model)
	if mm.current == nil || mm.current.BVID != "BV2" {
		t.Fatalf("应恢复当前曲目 BV2，实际 %+v", mm.current)
	}
	if mm.position != 90*time.Second || mm.resumeAt != 90*time.Second {
		t.Fatalf("断点未恢复: position=%v resumeAt=%v", mm.position, mm.resumeAt)
	}
	if mm.state != audio.StatePaused {
		t.Fatalf("恢复后应处于暂停态，实际 %v", mm.state)
	}
	if mm.queueIdx != 1 || mm.queueCursor != 1 {
		t.Fatalf("队列索引不符: idx=%d cursor=%d", mm.queueIdx, mm.queueCursor)
	}
}

func TestInsertNextMovesDuplicateAndKeepsCurrent(t *testing.T) {
	m := fixtureModel()
	m.queue = threeSongQueue()
	m.queueIdx = 0
	m.queueCursor = 0
	current := m.queue[0]
	m.current = &current

	m.insertNext(store.Song{Source: "bilibili", BVID: "BV3", Page: 1, Title: "C"})
	if len(m.queue) != 3 || m.queue[1].BVID != "BV3" || m.queueIdx != 0 || m.queueCursor != 1 {
		t.Fatalf("插入下一首结果不符: %+v idx=%d cursor=%d", m.queue, m.queueIdx, m.queueCursor)
	}
	// 已存在的歌应移动而不是重复。
	m.insertNext(store.Song{Source: "bilibili", BVID: "BV2", Page: 1, Title: "B"})
	if len(m.queue) != 3 || m.queue[1].BVID != "BV2" || m.queue[2].BVID != "BV3" {
		t.Fatalf("重复插入应移动旧条目: %+v", m.queue)
	}
}

func TestPlayQueueItemNext(t *testing.T) {
	m := fixtureModel()
	m.queue = threeSongQueue()
	m.queueIdx = 0
	m.queueCursor = 2
	current := m.queue[0]
	m.current = &current

	m.playQueueItemNext()
	if len(m.queue) != 3 || m.queue[1].BVID != "BV3" || m.queue[2].BVID != "BV2" {
		t.Fatalf("下一首移动结果不符: %+v", m.queue)
	}
	if m.queueIdx != 0 || m.queueCursor != 1 {
		t.Fatalf("索引未跟随: idx=%d cursor=%d", m.queueIdx, m.queueCursor)
	}
}

func TestMoveQueueItemKeepsCurrentIndex(t *testing.T) {
	m := fixtureModel()
	m.queue = threeSongQueue()
	m.queueIdx = 0
	m.queueCursor = 0
	current := m.queue[0]
	m.current = &current

	m.moveQueueItem(1)
	if m.queue[0].BVID != "BV2" || m.queue[1].BVID != "BV1" {
		t.Fatalf("移动结果不符: %+v", m.queue)
	}
	if m.queueIdx != 1 || m.queueCursor != 1 {
		t.Fatalf("当前索引未跟随: idx=%d cursor=%d", m.queueIdx, m.queueCursor)
	}
}

func TestClearQueueKeepsCurrent(t *testing.T) {
	m := fixtureModel()
	m.queue = threeSongQueue()
	m.queueIdx = 1
	m.queueCursor = 2
	current := m.queue[1]
	m.current = &current

	m.clearQueue()
	if len(m.queue) != 1 || m.queue[0].BVID != "BV2" {
		t.Fatalf("清空后应只保留当前曲目: %+v", m.queue)
	}
	if m.queueIdx != 0 || m.queueCursor != 0 {
		t.Fatalf("清空后索引应归零: %d %d", m.queueIdx, m.queueCursor)
	}
}

func TestRemoveQueueItemAdjustsIndexesAndAdvances(t *testing.T) {
	m := fixtureModel()
	m.queue = threeSongQueue()
	m.queueIdx = 1
	m.queueCursor = 0
	current := m.queue[1]
	m.current = &current

	m.removeQueueItem()
	if len(m.queue) != 2 || m.queueIdx != 0 || m.current == nil || m.current.BVID != "BV2" {
		t.Fatalf("删除前面的条目后当前曲目应保持: queue=%+v idx=%d current=%+v", m.queue, m.queueIdx, m.current)
	}

	m.queue = threeSongQueue()
	m.queueIdx = 1
	m.queueCursor = 1
	current = m.queue[1]
	m.current = &current
	_ = m.removeQueueItem()
	if len(m.queue) != 2 || m.current == nil || m.current.BVID != "BV3" {
		t.Fatalf("删除当前曲目后应播下一首: queue=%+v current=%+v", m.queue, m.current)
	}
}

func TestSeekOnRestoredSessionMovesResumePoint(t *testing.T) {
	m := fixtureModel()
	m.engine = nil
	current := store.Song{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "A", Duration: 200}
	m.current = &current
	m.duration = 200 * time.Second
	m.position = 50 * time.Second
	m.resumeAt = 50 * time.Second

	m.seekBy(10 * time.Second)
	if m.position != 60*time.Second || m.resumeAt != 60*time.Second {
		t.Fatalf("未起播时 seek 应移动断点: position=%v resumeAt=%v", m.position, m.resumeAt)
	}
	m.seekBy(-100 * time.Second)
	if m.position != 0 || m.resumeAt != 0 {
		t.Fatalf("seek 应夹到 0: position=%v resumeAt=%v", m.position, m.resumeAt)
	}
}

func TestMuteQualityAndSleepCycle(t *testing.T) {
	m := fixtureModel()
	m.toggleMute()
	if !m.muted {
		t.Fatal("应进入静音")
	}
	m.toggleMute()
	if m.muted {
		t.Fatal("应取消静音")
	}
	if m.quality != 0 {
		t.Fatalf("默认音质应为自动，实际 %v", m.quality)
	}
	m.cycleQuality()
	if m.quality != 1 {
		t.Fatalf("音质应切到 AAC 优先，实际 %v", m.quality)
	}
	m.cycleQuality()
	m.cycleQuality()
	if m.quality != 0 {
		t.Fatalf("音质应循环回自动，实际 %v", m.quality)
	}
	m.cycleSleep()
	if m.sleepUntil.IsZero() || m.sleepSlot != 1 {
		t.Fatalf("睡眠定时未开启: %+v slot=%d", m.sleepUntil, m.sleepSlot)
	}
	for i := 0; i < 4; i++ {
		m.cycleSleep()
	}
	if !m.sleepUntil.IsZero() || m.sleepSlot != 0 {
		t.Fatalf("睡眠定时应循环关闭: %+v slot=%d", m.sleepUntil, m.sleepSlot)
	}
}

func TestLyricOffsetShiftAndReset(t *testing.T) {
	m := fixtureModel()
	current := store.Song{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "A"}
	m.current = &current
	base := []lyrics.Line{{StartMS: 1000, Text: "a"}, {StartMS: 2000, Text: "b"}}
	m.lyricBase = base
	m.lyricLines = lyrics.Clone(base, 0)

	m.shiftLyrics(500)
	if m.lyricOffsetMS != 500 || m.lyricLines[0].StartMS != 1500 {
		t.Fatalf("正向偏移未生效: offset=%d lines=%+v", m.lyricOffsetMS, m.lyricLines)
	}
	m.resetLyricOffset()
	if m.lyricOffsetMS != 0 || m.lyricLines[0].StartMS != 1000 {
		t.Fatalf("归零未生效: offset=%d lines=%+v", m.lyricOffsetMS, m.lyricLines)
	}

	// 曲库页也能用 < / > / 0 调偏移，不必先切到歌词页。
	m.view = viewLibrary
	updated, _ := m.Update(keyPress("<", '<'))
	mm := updated.(*Model)
	if mm.lyricOffsetMS != -500 || mm.lyricLines[0].StartMS != 500 {
		t.Fatalf("曲库中 < 应减 0.5 秒: offset=%d lines=%+v", mm.lyricOffsetMS, mm.lyricLines)
	}
	updated, _ = mm.Update(keyPress(">", '>'))
	mm = updated.(*Model)
	if mm.lyricOffsetMS != 0 {
		t.Fatalf("曲库中 > 应加回 0.5 秒: offset=%d", mm.lyricOffsetMS)
	}
	updated, _ = mm.Update(keyPress(">", '>'))
	mm = updated.(*Model)
	updated, _ = mm.Update(keyPress("0", '0'))
	mm = updated.(*Model)
	if mm.lyricOffsetMS != 0 {
		t.Fatalf("曲库中 0 应归零: offset=%d", mm.lyricOffsetMS)
	}
}

func TestSearchPaginationBounds(t *testing.T) {
	m := fixtureModel()
	m.searchKeyword = "夜航星"
	m.searchPage = 1
	m.searchPages = 2
	m.searchBusy = false

	cmd := m.changeSearchPage(1)
	if cmd == nil || m.searchPage != 2 || !m.searchBusy {
		t.Fatalf("翻到第二页失败: cmd=%v page=%d busy=%v", cmd, m.searchPage, m.searchBusy)
	}
	m.searchBusy = false
	if cmd := m.changeSearchPage(1); cmd != nil || m.searchPage != 2 {
		t.Fatalf("超出末页应忽略: cmd=%v page=%d", cmd, m.searchPage)
	}
	if cmd := m.changeSearchPage(-3); cmd != nil || m.searchPage != 2 {
		t.Fatalf("超出首页应忽略: cmd=%v page=%d", cmd, m.searchPage)
	}
}

func TestPlaylistPromptCreatesPlaylist(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "tui.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.EnsurePlaylist(store.Playlist{ID: "local-default", Title: "本地歌单", Kind: "local"}); err != nil {
		t.Fatalf("建内置歌单失败: %v", err)
	}

	m := fixtureModel()
	m.store = st
	m.beginPrompt(promptNewPlaylist)
	m.promptInput = "通勤"
	cmd := m.commitPrompt()
	if cmd == nil {
		t.Fatal("创建成功后应重新加载歌单")
	}
	if m.pendingPlaylistID == "" {
		t.Fatal("应记住新建歌单的 ID")
	}
	items, err := st.ListPlaylists()
	if err != nil {
		t.Fatalf("列出歌单失败: %v", err)
	}
	found := false
	for _, p := range items {
		if p.Title == "通勤" {
			found = true
		}
	}
	if !found {
		t.Fatalf("数据库里没有新歌单: %+v", items)
	}
	if m.prompt != promptNone {
		t.Fatal("提交后应关闭输入态")
	}
}

func TestDeleteDefaultLocalPlaylistIsAllowed(t *testing.T) {
	m := fixtureModel()
	m.view = viewLibrary
	m.libraryFocus = focusPlaylists
	m.playlistIdx = 1 // fixture 里的 local-default
	updated, _ := m.Update(keyPress("D", 'D'))
	mm := updated.(*Model)
	if mm.prompt != promptDeletePlaylist {
		t.Fatalf("默认本地歌单应进入删除确认，实际 prompt=%v note=%q", mm.prompt, mm.note.text)
	}
}

func TestDefaultLocalPlaylistIsCreatedOnlyOnce(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "default-once.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if err := app.EnsureDefaultPlaylist(st); err != nil {
		t.Fatalf("首次补默认歌单失败: %v", err)
	}
	if _, err := st.Playlist("local-default"); err != nil {
		t.Fatalf("首次应创建默认歌单: %v", err)
	}
	if err := st.DeletePlaylist("local-default"); err != nil {
		t.Fatalf("默认歌单应允许删除: %v", err)
	}
	if err := app.EnsureDefaultPlaylist(st); err != nil {
		t.Fatalf("再次检查失败: %v", err)
	}
	if _, err := st.Playlist("local-default"); err != store.ErrNotFound {
		t.Fatalf("删除后不应自动重建，实际 err=%v", err)
	}
}

func TestEmptyPlaylistsClearSongs(t *testing.T) {
	m := fixtureModel()
	m.songs = []store.Song{{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "A"}}
	m.songsBusy = true
	updated, _ := m.Update(playlistsMsg{items: nil})
	mm := updated.(*Model)
	if len(mm.songs) != 0 || mm.songsBusy || mm.playlistIdx != 0 {
		t.Fatalf("歌单删空后应清掉残留曲目: songs=%d busy=%v idx=%d", len(mm.songs), mm.songsBusy, mm.playlistIdx)
	}
}

func TestAddSongCreatesLocalPlaylistWhenNone(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "auto-target.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	m := fixtureModel()
	m.store = st
	m.playlists = nil
	m.current = nil
	m.songs = []store.Song{{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "A"}}
	m.songIdx = 0
	_ = m.addCurrentToPlaylist()
	if m.picker == nil {
		t.Fatal("没有歌单时按 a 应打开目标选择器")
	}
	m.picker.idx = 0 // 「新建本地歌单并加入」
	_ = m.chooseTarget()
	if m.prompt != promptNewPlaylist {
		t.Fatalf("应进入新建歌单输入，实际 %v", m.prompt)
	}
	m.promptInput = "自动歌单"
	_ = m.commitPrompt()

	items, err := st.ListPlaylists()
	if err != nil || len(items) != 1 || items[0].Kind != "local" || items[0].Title != "自动歌单" {
		t.Fatalf("没有歌单时应能新建一个本地歌单: %+v err=%v", items, err)
	}
	songs, err := st.PlaylistSongs(items[0].ID)
	if err != nil || len(songs) != 1 {
		t.Fatalf("歌曲应加入自动创建的歌单: %+v err=%v", songs, err)
	}
}

func TestPromptAndMutedFrames(t *testing.T) {
	m := fixtureModel()
	m.muted = true
	m.sleepUntil = time.Now().Add(15 * time.Minute)
	m.prompt = promptNewPlaylist
	m.promptInput = "通勤"
	lines := assertFrame(t, m.View().Content, 100, 30)
	body := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(body, "静音") {
		t.Fatal("播放条应显示静音")
	}
	if !strings.Contains(body, "新建歌单：通勤") {
		t.Fatal("底部应显示新建歌单输入")
	}
}

func TestHelpScrollShowsTail(t *testing.T) {
	m := fixtureModel()
	m.view = viewHelp
	m.helpOffset = 0
	body := ansi.Strip(m.View().Content)
	if !strings.Contains(body, "快捷键") {
		t.Fatal("帮助页应显示标题")
	}
	m.helpOffset = 1000
	body = ansi.Strip(m.View().Content)
	if !strings.Contains(body, "q / Q / ctrl+c") || !strings.Contains(body, "配置文件") {
		t.Fatalf("滚动到底部应显示结尾帮助，实际：\n%s", body)
	}
	assertFrame(t, m.View().Content, 100, 30)
}

func TestStaleStreamResultIsDiscarded(t *testing.T) {
	m := fixtureModel()
	m.playRequest = 2
	m.awaitingStream = true
	before := *m.current
	updated, _ := m.Update(streamMsg{request: 1, song: store.Song{Title: "旧请求"}})
	mm := updated.(*Model)
	if mm.current.ID != before.ID || mm.current.Title != before.Title {
		t.Fatalf("旧请求不应改动当前曲目: %+v", mm.current)
	}
	if !mm.awaitingStream {
		t.Fatal("旧请求被丢弃后应仍在等待新流")
	}
}

func TestStartStreamClearsAwaiting(t *testing.T) {
	m := fixtureModel()
	m.playRequest = 1
	m.awaitingStream = true
	song := store.Song{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "A", Duration: 100}
	stream := source.Stream{URL: "https://cdn/a.m4s", Duration: 100 * time.Second, Quality: "192k"}
	_ = m.startStream(song, stream, 5*time.Second)
	if m.awaitingStream {
		t.Fatal("起播后应清除等待标记")
	}
	if m.state != audio.StatePlaying || m.position != 5*time.Second || m.current == nil {
		t.Fatalf("起播状态不符: state=%v pos=%v current=%+v", m.state, m.position, m.current)
	}
}

func TestPlayRequestInvalidatesPrevious(t *testing.T) {
	m := fixtureModel()
	m.awaitingStream = false
	_ = m.playSongCmdAt(store.Song{Source: "bilibili", BVID: "BV1"}, 0, 0)
	first := m.playRequest
	_ = m.playSongCmdAt(store.Song{Source: "bilibili", BVID: "BV2"}, 0, 0)
	if m.playRequest != first+1 {
		t.Fatalf("每次解析请求都应递增: %d -> %d", first, m.playRequest)
	}
	if !m.awaitingStream {
		t.Fatal("请求发出后应处于等待流状态")
	}
}

func TestQueuePersistAndRestoreRoundTrip(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	songs := threeSongQueue()
	for _, s := range songs {
		if _, err := st.UpsertSong(s); err != nil {
			t.Fatalf("写入歌曲失败: %v", err)
		}
	}

	m := fixtureModel()
	m.store = st
	m.queue = songs
	m.queueIdx = 2
	m.current = &m.queue[2]
	m.position = 42 * time.Second
	m.persistQueue()

	msg := m.restoreQueueCmd()()
	restored, ok := msg.(queueRestoredMsg)
	if !ok {
		t.Fatalf("恢复消息类型不符: %T", msg)
	}
	if len(restored.songs) != 3 || restored.index != 2 || restored.position != 42*time.Second {
		t.Fatalf("队列/进度恢复不符: %+v", restored)
	}
}

func TestRestoreSkipsMissingSongsAndKeepsCurrent(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	songs := threeSongQueue()
	for _, s := range songs {
		if _, err := st.UpsertSong(s); err != nil {
			t.Fatalf("写入歌曲失败: %v", err)
		}
	}
	ids := []string{songs[0].ID, "bilibili:BVmissing:1", songs[2].ID}
	if err := st.SaveQueue(store.QueueState{IDs: ids, Index: 2, Position: 9000}); err != nil {
		t.Fatalf("保存队列失败: %v", err)
	}
	m := fixtureModel()
	m.store = st
	msg := m.restoreQueueCmd()()
	restored := msg.(queueRestoredMsg)
	if len(restored.songs) != 2 || restored.index != 1 || restored.position != 9*time.Second {
		t.Fatalf("跳过失效条目后应按 ID 对齐: %+v", restored)
	}
	if restored.songs[restored.index].BVID != "BV3" {
		t.Fatalf("当前曲目应为 BV3，实际 %+v", restored.songs[restored.index])
	}
}

func TestLyricsFallbackToLRCLIB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"trackName": "夜航星", "artistName": "不才", "duration": 287,
			"syncedLyrics": "[00:01.00]第一句\n[00:05.50]第二句",
		}})
	}))
	defer srv.Close()

	m := fixtureModel()
	m.lrclib = lrclib.NewWithBaseURL(srv.URL)
	msg := m.lyricsCmd(store.Song{
		ID: "bilibili:BV2:1", Source: "bilibili", BVID: "BV2", Page: 1,
		Title: "夜航星【官方MV】", Artist: "不才", Duration: 287,
	})()
	got, ok := msg.(lyricMsg)
	if !ok {
		t.Fatalf("消息类型不符: %T", msg)
	}
	if len(got.lines) != 2 || got.source != "LRCLIB" {
		t.Fatalf("LRCLIB 歌词未生效: %+v", got)
	}
	if got.lines[1].StartMS != 5500 {
		t.Fatalf("LRC 时间轴解析错误: %+v", got.lines)
	}
}

func TestLogoutClearsAccountAndRemoteMirror(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "logout.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.EnsurePlaylist(store.Playlist{ID: "bili-fav-1", Source: "bilibili", Title: "收藏夹", Kind: "remote"}); err != nil {
		t.Fatalf("建远端歌单失败: %v", err)
	}
	if err := st.SaveAccount("bilibili", []byte(`{"SESSDATA":"x"}`)); err != nil {
		t.Fatalf("保存登录态失败: %v", err)
	}
	m := fixtureModel()
	m.store = st
	m.users = map[source.ID]source.User{
		source.Bilibili: {Name: "Sheyiyuan", LoggedIn: true},
	}
	cmd := m.logout(source.Bilibili)
	if cmd == nil {
		t.Fatal("退出登录后应重新加载账号与歌单")
	}
	if u := m.users[source.Bilibili]; u.LoggedIn || u.Name != "" {
		t.Fatalf("用户信息未清空: %+v", u)
	}
	if _, err := st.LoadAccount("bilibili"); err != store.ErrNotFound {
		t.Fatalf("登录态未清除: %v", err)
	}
	items, err := st.ListPlaylists()
	if err != nil {
		t.Fatalf("列出歌单失败: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("远端镜像应已清空: %+v", items)
	}
}

func TestMusicfoxTransportKeys(t *testing.T) {
	m := fixtureModel()
	m.queueIdx = 0
	m.queueCursor = 0
	current := m.queue[0]
	m.current = &current
	m.mode = modeSequence

	updated, _ := m.Update(keyPress("]", ']'))
	mm := updated.(*Model)
	if mm.queueIdx != 1 || mm.current == nil || mm.current.BVID != "BV2" {
		t.Fatalf("] 应切到下一首: idx=%d current=%+v", mm.queueIdx, mm.current)
	}

	updated, _ = mm.Update(keyPress("[", '['))
	mm = updated.(*Model)
	if mm.queueIdx != 0 || mm.current == nil || mm.current.BVID != "BV1" {
		t.Fatalf("[ 应切到上一首: idx=%d current=%+v", mm.queueIdx, mm.current)
	}

	// n 在音乐列表里是「播放选中项」，不是切歌。
	mm.songIdx = 1
	updated, _ = mm.Update(keyPress("n", 'n'))
	mm = updated.(*Model)
	if mm.queueIdx != 1 || mm.current == nil || mm.current.BVID != "BV2" {
		t.Fatalf("n 应播放选中项: idx=%d current=%+v", mm.queueIdx, mm.current)
	}

	updated, _ = mm.Update(keyPress("p", 'p'))
	mm = updated.(*Model)
	if mm.mode != modeLoopOne {
		t.Fatalf("p 应切换播放模式，实际 %v", mm.mode)
	}
	updated, _ = mm.Update(keyPress("y", 'y'))
	mm = updated.(*Model)
	if mm.quality != source.QualityHigh {
		t.Fatalf("y 应切换音质，实际 %v", mm.quality)
	}

	mm.songIdx = 1
	updated, _ = mm.Update(keyPress("g", 'g'))
	mm = updated.(*Model)
	if mm.songIdx != 0 {
		t.Fatalf("g 应到列表顶部，实际 %d", mm.songIdx)
	}
	updated, _ = mm.Update(keyPress("G", 'G'))
	mm = updated.(*Model)
	if mm.songIdx != len(mm.songs)-1 {
		t.Fatalf("G 应到列表底部，实际 %d", mm.songIdx)
	}

	updated, _ = mm.Update(tea.KeyPressMsg(tea.Key{Code: 'u', Mod: tea.ModCtrl}))
	_ = updated
}

func TestShuffleUsesShuffledQueue(t *testing.T) {
	m := fixtureModel()
	m.queue = nil
	for i := 0; i < 8; i++ {
		m.queue = append(m.queue, store.Song{
			ID:       store.SongID("bilibili", fmt.Sprintf("BV%d", i), 1),
			Source:   "bilibili",
			BVID:     fmt.Sprintf("BV%d", i),
			RemoteID: fmt.Sprintf("BV%d", i),
			Page:     1,
			Title:    fmt.Sprintf("song-%d", i),
		})
	}
	m.queueIdx = 1
	m.current = &m.queue[1]
	m.mode = modeSequence
	m.shuffleFn = func(songs []store.Song) {
		for i, j := 0, len(songs)-1; i < j; i, j = i+1, j-1 {
			songs[i], songs[j] = songs[j], songs[i]
		}
	}

	m.setMode(modeShuffle)
	if m.queue[0].Title != "song-7" || m.queue[7].Title != "song-0" {
		t.Fatalf("进入随机后队列应洗牌: %+v", m.queue)
	}
	if m.queueIdx != 6 || m.queue[m.queueIdx].Title != "song-1" {
		t.Fatalf("洗牌应保持当前歌曲，idx=%d", m.queueIdx)
	}
	if len(m.shuffleBase) != 8 {
		t.Fatalf("应保存洗牌前队列，实际 %d", len(m.shuffleBase))
	}

	m.advance()
	if m.queueIdx != 7 || m.queue[7].Title != "song-0" {
		t.Fatalf("洗牌后应顺序前进而不是再次随机抽取，idx=%d", m.queueIdx)
	}

	// 在随机队列里「下一首播放」必须插在当前曲目之后，并成为真正的下一首。
	next := store.Song{ID: store.SongID("bilibili", "BV-extra", 1), Source: "bilibili", BVID: "BV-extra", RemoteID: "BV-extra", Page: 1, Title: "extra"}
	m.insertNext(next)
	if m.queue[8].Title != "extra" {
		t.Fatalf("下一首播放应插在当前曲目之后: %+v", m.queue)
	}
	m.advance()
	if m.current == nil || m.current.Title != "extra" {
		t.Fatalf("下一首应播放刚插入的曲目，实际 %+v", m.current)
	}

	// 退出随机按进入前的顺序恢复，随机期间新增的曲目追加到末尾。
	m.setMode(modeSequence)
	if len(m.queue) != 9 || m.queue[0].Title != "song-0" || m.queue[8].Title != "extra" {
		t.Fatalf("退出随机应恢复原顺序并保留新增曲目: %+v", m.queue)
	}
}

func TestPlayPlaylistShufflesInShuffleMode(t *testing.T) {
	m := fixtureModel()
	m.mode = modeShuffle
	m.playlistIdx = 0
	m.shuffleFn = func(songs []store.Song) {
		for i, j := 0, len(songs)-1; i < j; i, j = i+1, j-1 {
			songs[i], songs[j] = songs[j], songs[i]
		}
	}

	m.playPlaylistIndex(0)
	if len(m.queue) != 2 || m.queue[0].Title != "夜航星" || m.queue[1].Title != "Never Gonna Give You Up" {
		t.Fatalf("随机模式播放歌单应洗牌: %+v", m.queue)
	}
	if m.current == nil || m.current.Title != "Never Gonna Give You Up" {
		t.Fatalf("应仍然播放入选的歌单首曲: %+v", m.current)
	}
	if m.queueIdx != 1 {
		t.Fatalf("洗牌后当前索引应指向入选项，实际 %d", m.queueIdx)
	}
	if len(m.shuffleBase) != 2 || m.shuffleBase[0].Title != "Never Gonna Give You Up" {
		t.Fatalf("进入随机播放歌单时应保存原顺序: %+v", m.shuffleBase)
	}
}

func TestCustomKeymapAffectsHandlingAndHelp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keymap.toml")
	if err := os.WriteFile(path, []byte("[keys]\nnext = [\"z\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	km, err := keymap.Load(path)
	if err != nil {
		t.Fatalf("加载键位失败: %v", err)
	}
	m := fixtureModel()
	m.keys = km
	m.mode = modeSequence
	m.queueIdx = 0
	updated, _ := m.Update(keyPress("z", 'z'))
	mm := updated.(*Model)
	if mm.queueIdx != 1 {
		t.Fatalf("自定义 next=z 未生效: %d", mm.queueIdx)
	}
	mm.queueIdx = 0
	updated, _ = mm.Update(keyPress("]", ']'))
	mm = updated.(*Model)
	if mm.queueIdx != 0 {
		t.Fatalf("覆盖后旧键 ] 不应再切歌: %d", mm.queueIdx)
	}
	if got := mm.helpLines(); len(got) == 0 {
		t.Fatal("帮助页为空")
	}
}
