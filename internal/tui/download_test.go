package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/sheyiyuan/zero-beat/internal/downloader"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// testDownloads 返回一个不会真正下载、但会保持 Running 的管理器，便于断言队列状态。
func testDownloads(t *testing.T) *downloader.Manager {
	t.Helper()
	mgr := downloader.New(2, func(ctx context.Context, song store.Song, job downloader.Job, progress func(done, total int64)) (string, int64, error) {
		<-ctx.Done()
		return "", 0, ctx.Err()
	})
	t.Cleanup(mgr.CancelAll)
	return mgr
}

func TestDownloadSelectedKeyQueuesSelectedSong(t *testing.T) {
	m := fixtureModel()
	m.downloads = testDownloads(t)
	m.songIdx = 1

	updated, cmd := m.Update(keyPress("W", 'W'))
	mm := updated.(*Model)
	tasks := mm.downloads.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("按 w 后应有一个下载任务，实际 %d", len(tasks))
	}
	task := tasks[0]
	if task.Title != "夜航星" || task.State != downloader.Running {
		t.Fatalf("任务不符: title=%q state=%d", task.Title, task.State)
	}
	if cmd == nil {
		t.Fatal("开始下载应返回 tea.Cmd")
	}

	updated, _ = mm.Update(keyPress("W", 'W'))
	mm = updated.(*Model)
	if len(mm.downloads.Tasks()) != 1 {
		t.Fatalf("重复下载同一首歌不应重复入队，实际 %d", len(mm.downloads.Tasks()))
	}
	if !strings.Contains(mm.note.text, "已在下载队列") {
		t.Fatalf("应提示已在队列，实际 %q", mm.note.text)
	}
}

func TestDownloadKeyUsesSearchSelection(t *testing.T) {
	m := fixtureModel()
	m.downloads = testDownloads(t)
	m.view = viewSearch
	m.searchEditing = false
	m.searchMode = searchSource
	m.searchResults = []source.Candidate{{
		Track: source.Track{Source: source.NetEase, ExternalID: "2", Page: 1, Title: "晴天", Artist: "周杰伦", Duration: 269},
	}}
	m.searchIdx = 0

	updated, _ := m.Update(keyPress("W", 'W'))
	mm := updated.(*Model)
	tasks := mm.downloads.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("搜索页按 W 应下载选中结果，实际 %d", len(tasks))
	}
	if tasks[0].Song.Title != "晴天" || tasks[0].Song.Source != "netease" {
		t.Fatalf("下载的搜索项不符: %+v", tasks[0].Song)
	}
}

func TestDownloadProgressLineAndBodyHeight(t *testing.T) {
	m := fixtureModel()
	mgr := testDownloads(t)
	m.downloads = mgr
	song := store.Song{ID: "bilibili:BV2:1", Source: "bilibili", BVID: "BV2", Page: 1, Title: "夜航星"}
	id, _ := mgr.Add(song, "夜航星", downloader.Job{})
	mgr.Start()
	mgr.SetProgress(id, 3<<20, 10<<20)

	if got := m.bodyHeight(); got != 22 {
		t.Fatalf("有下载行时正文高度应为 22，实际 %d", got)
	}
	lines := assertFrame(t, strings.Join(m.render(), "\n"), 100, 30)
	body := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(body, "夜航星") || !strings.Contains(body, "30%") || !strings.Contains(body, "3.0 MiB") {
		t.Fatalf("底部应显示下载进度: %q", body)
	}

	updated, cmd := m.Update(downloadEventMsg{ev: downloader.Event{ID: id, Done: 7 << 20, Total: 10 << 20}})
	mm := updated.(*Model)
	if mm.downloads.Tasks()[0].Done != 7<<20 {
		t.Fatalf("进度未更新: %d", mm.downloads.Tasks()[0].Done)
	}
	if cmd == nil {
		t.Fatal("进度消息后应继续等待下一条")
	}

	updated, _ = mm.Update(downloadEventMsg{ev: downloader.Event{ID: id, Finished: true, Path: "/tmp/夜航星.mp3", Size: 10 << 20}})
	mm = updated.(*Model)
	if len(mm.downloads.Tasks()) != 0 {
		t.Fatalf("完成后应移除任务，实际 %d", len(mm.downloads.Tasks()))
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
	m.downloads = testDownloads(t)
	m.songs = append(m.songs, store.Song{ID: "bilibili:BV3:1", Source: "bilibili", BVID: "BV3", Page: 1, Title: "第三首"})

	for _, idx := range []int{0, 1, 2} {
		m.songIdx = idx
		updated, _ := m.Update(keyPress("W", 'W'))
		m = updated.(*Model)
	}
	tasks := m.downloads.Tasks()
	if len(tasks) != 3 {
		t.Fatalf("应有三个任务，实际 %d", len(tasks))
	}
	running, queued := 0, 0
	for _, task := range tasks {
		if task.State == downloader.Running {
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
	mgr := testDownloads(t)
	m.downloads = mgr
	if _, ok := mgr.Add(store.Song{ID: "bilibili:BV2:1", Source: "bilibili", BVID: "BV2", Page: 1, Title: "夜航星"}, "夜航星", downloader.Job{}); !ok {
		t.Fatal("加入任务失败")
	}
	for _, v := range []view{viewHelp, viewLogin} {
		m.view = v
		if _, ok := m.downloadLineText(); ok {
			t.Fatalf("%s 页不应显示下载行", viewNames[v])
		}
	}
}

func TestDownloadCurrentKeyIgnoresSelection(t *testing.T) {
	m := fixtureModel()
	m.downloads = testDownloads(t)
	m.songIdx = 1 // 选中夜航星，但当前播放是 Never Gonna Give You Up

	updated, _ := m.Update(keyPress("w", 'w'))
	mm := updated.(*Model)
	tasks := mm.downloads.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("按 w 应下载当前播放，实际 %d", len(tasks))
	}
	if tasks[0].Song.Title != "Never Gonna Give You Up" {
		t.Fatalf("w 不应下载选中项，实际 %q", tasks[0].Song.Title)
	}
}

func TestDownloadWholePlaylistWithWOnPlaylistFocus(t *testing.T) {
	m := fixtureModel()
	m.downloads = testDownloads(t)
	m.libraryFocus = focusPlaylists

	updated, cmd := m.Update(keyPress("W", 'W'))
	mm := updated.(*Model)
	if len(mm.downloads.Tasks()) != 2 {
		t.Fatalf("歌单焦点按 W 应入队整个歌单，实际 %d", len(mm.downloads.Tasks()))
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

	updated, _ := m.Update(keyPress("", tea.KeyLeft))
	mm := updated.(*Model)
	if mm.libraryFocus != focusPlaylists {
		t.Fatal("← 应把焦点切到歌单")
	}
	if mm.playlistIdx != 0 {
		t.Fatalf("切到歌单焦点不应改变选中歌单: %d", mm.playlistIdx)
	}

	updated, _ = mm.Update(keyPress("", tea.KeyRight))
	mm = updated.(*Model)
	if mm.libraryFocus != focusSongs {
		t.Fatal("→ 应把焦点切回歌曲")
	}
}

func TestDownloadLineProgressFormat(t *testing.T) {
	m := fixtureModel()
	mgr := testDownloads(t)
	m.downloads = mgr
	id, _ := mgr.Add(store.Song{ID: "bilibili:BV2:1", Source: "bilibili", BVID: "BV2", Page: 1, Title: "夜航星"}, "夜航星", downloader.Job{})
	mgr.Start()
	mgr.SetProgress(id, 1<<20, 4<<20)
	line, ok := m.downloadLineText()
	if !ok || !strings.Contains(ansi.Strip(line), "25%") {
		t.Fatalf("下载进度格式不符: %q", ansi.Strip(line))
	}
}

// TestDownloadJobSnapshotsQualityAndForce 验证入队时快照音质（后台协程不读
// Model 字段）以及补全缺失文件时带 force。
func TestDownloadJobSnapshotsQualityAndForce(t *testing.T) {
	m := fixtureModel()
	m.downloads = testDownloads(t)
	m.songIdx = 0
	m.quality = source.QualityLossless
	updated, _ := m.Update(keyPress("W", 'W'))
	mm := updated.(*Model)
	tasks := mm.downloads.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("应入队一个任务，实际 %d", len(tasks))
	}
	if tasks[0].Job.Quality != int(source.QualityLossless) {
		t.Fatalf("任务音质应为入队快照，实际 %+v", tasks[0].Job)
	}
	if tasks[0].Job.Force {
		t.Fatalf("文件正常时不应强制重下，实际 %+v", tasks[0].Job)
	}
}

// TestCancelSelectedDownload 验证下载页 x 取消选中曲目的下载任务。
func TestCancelSelectedDownload(t *testing.T) {
	block := make(chan struct{})
	mgr := downloader.New(1, func(ctx context.Context, _ store.Song, _ downloader.Job, _ func(int64, int64)) (string, int64, error) {
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-block:
			return "p", 1, nil
		}
	})
	t.Cleanup(func() {
		mgr.CancelAll()
		close(block)
	})
	a := store.NormalizeSong(store.Song{Source: "bilibili", BVID: "BV1", Page: 1, Title: "甲", Artist: "x", Duration: 10})
	b := store.NormalizeSong(store.Song{Source: "bilibili", BVID: "BV2", Page: 1, Title: "乙", Artist: "y", Duration: 10})
	m := fixtureModel()
	m.downloads = mgr
	m.view = viewDownloads
	m.downloadItems = []downloadItem{
		{entry: store.DownloadEntry{Download: store.Download{SongID: a.ID, Path: "/tmp/a.flac"}, Song: a}},
		{entry: store.DownloadEntry{Download: store.Download{SongID: b.ID, Path: "/tmp/b.flac"}, Song: b}},
	}
	m.downloadIdx = 0
	if _, ok := mgr.Add(a, "甲", downloader.Job{}); !ok {
		t.Fatal("入队失败")
	}
	if _, ok := mgr.Add(b, "乙", downloader.Job{}); !ok {
		t.Fatal("入队失败")
	}
	mgr.Start()

	updated, _ := m.Update(keyPress("x", 'x'))
	mm := updated.(*Model)
	if !strings.Contains(mm.note.text, "已取消下载") {
		t.Fatalf("x 应取消选中下载，实际提示 %q", mm.note.text)
	}
	if mgr.Has(a.ID) {
		t.Fatal("取消后任务不应再在下载队列里")
	}
}

// TestFailedDownloadKeepsTaskAndLine 验证失败任务保留、底部状态行常驻提示，
// 进入下载页后清理。
func TestFailedDownloadKeepsTaskAndLine(t *testing.T) {
	mgr := downloader.New(1, func(context.Context, store.Song, downloader.Job, func(int64, int64)) (string, int64, error) {
		return "", 0, errors.New("boom")
	})
	m := fixtureModel()
	m.downloads = mgr
	song := store.NormalizeSong(store.Song{Source: "bilibili", BVID: "BV1", Page: 1, Title: "失败曲", Artist: "x", Duration: 10})

	cmd := m.enqueueDownload(song, "开始下载：")
	if cmd == nil {
		t.Fatal("应返回下载事件监听命令")
	}
	msg := cmd()
	updated, _ := m.Update(msg)
	mm := updated.(*Model)

	tasks := mgr.Tasks()
	if len(tasks) != 1 || tasks[0].State != downloader.Failed {
		t.Fatalf("失败任务应保留为 Failed，实际 %+v", tasks)
	}
	if !strings.Contains(mm.note.text, "boom") {
		t.Fatalf("应显示错误提示，实际 %q", mm.note.text)
	}
	line, ok := mm.downloadLineText()
	if !ok || !strings.Contains(ansi.Strip(line), "下载失败") {
		t.Fatalf("底部状态行应提示下载失败，实际 %q", ansi.Strip(line))
	}

	// 进入下载页视为用户已看到，清掉失败任务。
	mm.openDownloads()
	if mgr.Len() != 0 {
		t.Fatalf("进入下载页应清理失败任务，剩余 %d", mgr.Len())
	}
}
