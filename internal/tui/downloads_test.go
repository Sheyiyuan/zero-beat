package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

func seedDownload(t *testing.T, st *store.Store, title string) (store.Song, string) {
	t.Helper()
	song, err := st.UpsertSong(store.Song{
		Source: "bilibili", BVID: "BV" + title, Page: 1, Title: title, Artist: "歌手", Duration: 120,
	})
	if err != nil {
		t.Fatalf("写入歌曲失败: %v", err)
	}
	path := filepath.Join(t.TempDir(), title+".flac")
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		t.Fatalf("写入音频失败: %v", err)
	}
	if err := st.UpsertDownload(store.Download{
		SongID: song.ID, Path: path, Size: 5, Codec: "flac", DurationMS: 120000,
	}); err != nil {
		t.Fatalf("写入下载记录失败: %v", err)
	}
	return song, path
}

func TestDownloadsViewLoadAndPlayLocal(t *testing.T) {
	st := openFeatureStore(t)
	_, path := seedDownload(t, st, "本地曲目")
	m := fixtureModel()
	m.store = st
	m.view = viewDownloads
	m.downloadsBusy = true

	updated, _ := m.Update(m.loadDownloadsCmd()())
	mm := updated.(*Model)
	if len(mm.downloadItems) != 1 || mm.downloadItems[0].missing {
		t.Fatalf("下载列表不符合预期: %+v", mm.downloadItems)
	}
	body := ansi.Strip(strings.Join(mm.render(), "\n"))
	if !strings.Contains(body, "下载管理") || !strings.Contains(body, "本地曲目") {
		t.Fatalf("下载页应显示记录: %q", body)
	}

	cmd := mm.playDownloadIndex(0)
	if cmd == nil {
		t.Fatal("播放下载项应返回命令")
	}
	msg := cmd().(streamMsg)
	if msg.err != nil || msg.stream.URL != path || msg.stream.Quality != "本地" {
		t.Fatalf("应从本地文件起播: %+v", msg)
	}
}

func TestDownloadsViewMarksMissingFiles(t *testing.T) {
	st := openFeatureStore(t)
	_, path := seedDownload(t, st, "已删除")
	if err := os.Remove(path); err != nil {
		t.Fatalf("删除文件失败: %v", err)
	}
	m := fixtureModel()
	m.store = st
	m.view = viewDownloads

	updated, _ := m.Update(m.loadDownloadsCmd()())
	mm := updated.(*Model)
	if len(mm.downloadItems) != 1 || !mm.downloadItems[0].missing {
		t.Fatalf("应标记文件缺失: %+v", mm.downloadItems)
	}
	body := ansi.Strip(strings.Join(mm.render(), "\n"))
	if !strings.Contains(body, "文件缺失") {
		t.Fatalf("界面应提示文件缺失: %q", body)
	}
}

func TestDownloadsRemoveAndDelete(t *testing.T) {
	st := openFeatureStore(t)
	song, path := seedDownload(t, st, "待删除")
	m := fixtureModel()
	m.store = st
	m.view = viewDownloads
	updated, _ := m.Update(m.loadDownloadsCmd()())
	mm := updated.(*Model)

	_ = mm.removeDownloadRecord(song.ID)
	if _, err := st.Download(song.ID); err == nil {
		t.Fatal("移除记录后不应还能查到")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("移除记录不应删除文件: %v", err)
	}

	if err := st.UpsertDownload(store.Download{SongID: song.ID, Path: path, Size: 5, Codec: "flac"}); err != nil {
		t.Fatalf("重新登记失败: %v", err)
	}
	updated, _ = mm.Update(mm.loadDownloadsCmd()())
	mm = updated.(*Model)
	_ = mm.deleteDownloadFile(song.ID)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("删除文件后应不存在: %v", err)
	}
	if _, err := st.Download(song.ID); err == nil {
		t.Fatal("删除文件后应同时删除记录")
	}
}

func TestDownloadsKeyOpensPage(t *testing.T) {
	m := fixtureModel()
	updated, cmd := m.Update(keyPress("F", 'F'))
	mm := updated.(*Model)
	if mm.view != viewDownloads {
		t.Fatalf("F 应打开下载页，实际 %v", viewNames[mm.view])
	}
	if cmd == nil {
		t.Fatal("打开下载页应返回加载命令")
	}
}

func TestLocalFirstPaletteToggle(t *testing.T) {
	st := openFeatureStore(t)
	m := fixtureModel()
	m.store = st
	m.localFirst = true

	_ = m.runPaletteAction(palLocalFirst)
	if m.localFirst {
		t.Fatal("第一次切换应关闭本地优先")
	}
	if m.service().Preferences().LocalFirst {
		t.Fatal("设置未持久化")
	}
	_ = m.runPaletteAction(palLocalFirst)
	if !m.localFirst || !m.service().Preferences().LocalFirst {
		t.Fatal("再次切换应重新开启并持久化")
	}
}

func TestLocalPlaybackRetryFallsBackToRemote(t *testing.T) {
	st := openFeatureStore(t)
	song, localPath := seedDownload(t, st, "回退曲目")
	m := fixtureModel()
	m.store = st
	m.localFirst = true
	if src, ok := m.sources.Get(source.Bilibili).(*fakeSource); ok {
		src.resolved = source.Resolved{
			Track:  source.Track{Source: source.Bilibili, ExternalID: "BV回退曲目", Page: 1, Title: "回退曲目"},
			Stream: source.Stream{URL: "https://example.invalid/remote.m4a", Quality: "远程"},
		}
	}

	msg := m.playLocalSongCmdAt(song, 0, 0)().(streamMsg)
	if msg.stream.URL != localPath {
		t.Fatalf("首轮应播放本地文件: %+v", msg)
	}
	msg = m.playLocalSongCmdAt(song, 0, 1)().(streamMsg)
	if msg.stream.URL != "https://example.invalid/remote.m4a" {
		t.Fatalf("重试应回落远程: %+v", msg)
	}
}

func TestDownloadsRedownloadMissingWithW(t *testing.T) {
	st := openFeatureStore(t)
	song, path := seedDownload(t, st, "待补全")
	if err := os.Remove(path); err != nil {
		t.Fatalf("删除文件失败: %v", err)
	}
	m := fixtureModel()
	m.store = st
	m.view = viewDownloads
	m.downloads = testDownloads(t)

	updated, _ := m.Update(m.loadDownloadsCmd()())
	mm := updated.(*Model)
	if len(mm.downloadItems) != 1 || !mm.downloadItems[0].missing {
		t.Fatalf("应加载到缺失项: %+v", mm.downloadItems)
	}

	updated, cmd := mm.Update(keyPress("W", 'W'))
	mm = updated.(*Model)
	tasks := mm.downloads.Tasks()
	if len(tasks) != 1 || tasks[0].Song.ID != song.ID {
		t.Fatalf("W 应把缺失歌曲加入下载队列: %+v", tasks)
	}
	if cmd == nil {
		t.Fatal("重新下载应返回 tea.Cmd")
	}
	if strings.Contains(mm.note.text, "没有选中项") {
		t.Fatalf("不应提示没有选中项: %q", mm.note.text)
	}
}
