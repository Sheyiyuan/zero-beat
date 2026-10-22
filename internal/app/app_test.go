package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

type baseFake struct {
	id         source.ID
	caps       source.Capability
	logged     bool
	search     source.SearchPage
	searchErr  error
	resolved   source.Resolved
	resolveErr error
	lyricLines []lyrics.Line
	lyricName  string
	playlists  []source.Playlist
	tracks     []source.Track
}

func (f *baseFake) ID() source.ID           { return f.id }
func (f *baseFake) Name() string            { return string(f.id) }
func (f *baseFake) Mark() string            { return "F" }
func (f *baseFake) Caps() source.Capability { return f.caps }
func (f *baseFake) LoggedIn() bool          { return f.logged }
func (f *baseFake) User() source.User       { return source.User{LoggedIn: f.logged} }
func (f *baseFake) Account(context.Context) (source.User, error) {
	return f.User(), nil
}
func (f *baseFake) StartQRLogin(context.Context) (source.QRCode, error) {
	return source.QRCode{}, nil
}
func (f *baseFake) PollQRLogin(context.Context, string) (source.LoginStatus, error) {
	return source.LoginStatus{}, nil
}
func (f *baseFake) Search(context.Context, string, int) (source.SearchPage, error) {
	return f.search, f.searchErr
}
func (f *baseFake) Resolve(context.Context, source.Ref, source.Quality) (source.Resolved, error) {
	return f.resolved, f.resolveErr
}
func (f *baseFake) Lyrics(context.Context, source.Ref) ([]lyrics.Line, string, error) {
	return f.lyricLines, f.lyricName, nil
}
func (f *baseFake) RemotePlaylists(context.Context) ([]source.Playlist, error) {
	return f.playlists, nil
}
func (f *baseFake) PlaylistTracks(context.Context, string) ([]source.Track, error) {
	return f.tracks, nil
}
func (f *baseFake) Session() map[string]string   { return nil }
func (f *baseFake) SetSession(map[string]string) {}
func (f *baseFake) Logout()                      {}

type dailyFake struct {
	*baseFake
	daily []source.Track
}

func (f *dailyFake) DailyRecommendations(context.Context) ([]source.Track, error) {
	return f.daily, nil
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestPlaylistsCreatesDefaultAndDaily(t *testing.T) {
	st := openTestStore(t)
	src := &dailyFake{
		baseFake: &baseFake{id: "fake", caps: source.CapDailyRecommendations, logged: true},
		daily:    []source.Track{{Source: "fake", ExternalID: "1", Title: "日推"}},
	}
	svc := New(source.NewRegistry(src), st, nil)

	items, warn, err := svc.Playlists(context.Background())
	if err != nil || warn != nil {
		t.Fatalf("加载歌单失败: err=%v warn=%v", err, warn)
	}
	found := map[string]store.Playlist{}
	for _, p := range items {
		found[p.ID] = p
	}
	if _, ok := found["local-default"]; !ok {
		t.Fatalf("默认本地歌单缺失: %+v", items)
	}
	daily, ok := found[DailyPlaylistID("fake")]
	if !ok || daily.Source != "fake" || daily.RemoteID != "daily" {
		t.Fatalf("每日推荐入口不符: %+v", daily)
	}
	if !IsDailyPlaylist(daily) {
		t.Fatal("每日推荐歌单应被识别")
	}

	songs, warn, err := svc.PlaylistSongs(context.Background(), daily, false)
	if err != nil || warn != nil || len(songs) != 1 || songs[0].Title != "日推" {
		t.Fatalf("日推曲目不符: %+v err=%v warn=%v", songs, err, warn)
	}
}

func TestSearchPartialFailureKeepsResults(t *testing.T) {
	ok := &baseFake{
		id:     "ok",
		caps:   source.CapSearch,
		search: source.SearchPage{Page: 1, Pages: 2, Candidates: []source.Candidate{{Track: source.Track{Source: "ok", ExternalID: "1", Title: "A"}}}},
	}
	bad := &baseFake{id: "bad", caps: source.CapSearch, searchErr: errors.New("boom")}
	svc := New(source.NewRegistry(ok, bad), nil, nil)

	res, err := svc.Search(context.Background(), "x", 1, "")
	if err != nil || res.Warn == nil || len(res.Candidates) != 1 || res.Pages != 2 {
		t.Fatalf("部分失败结果不符: %+v err=%v", res, err)
	}
	if _, err := svc.Search(context.Background(), "x", 1, "missing"); err == nil {
		t.Fatal("未知音源应报错")
	}
}

func TestResolveMergesAndPersists(t *testing.T) {
	st := openTestStore(t)
	src := &baseFake{
		id:   "fake",
		caps: source.CapSearch,
		resolved: source.Resolved{
			Track:  source.Track{Source: "fake", ExternalID: "42", Page: 1, Title: "标题", Artist: "歌手", Duration: 120},
			Stream: source.Stream{URL: "https://example/a.mp3", Codec: "mp3", Duration: 2 * time.Minute},
		},
	}
	svc := New(source.NewRegistry(src), st, nil)

	song, stream, err := svc.Resolve(context.Background(), store.Song{Source: "fake", RemoteID: "42", Page: 1}, source.QualityAuto)
	if err != nil || stream.URL == "" || song.Title != "标题" || song.Artist != "歌手" {
		t.Fatalf("解析结果不符: song=%+v stream=%+v err=%v", song, stream, err)
	}
	stored, err := st.Song(song.ID)
	if err != nil || stored.Title != "标题" {
		t.Fatalf("解析结果未落库: %+v err=%v", stored, err)
	}
}

func TestPreferencesRoundTrip(t *testing.T) {
	st := openTestStore(t)
	svc := New(nil, st, nil)
	prefs := svc.Preferences()
	if prefs.Volume != 0.8 || prefs.Quality != source.QualityAuto {
		t.Fatalf("默认偏好不符: %+v", prefs)
	}
	svc.SaveVolume(0.5)
	svc.SaveCompDB(2)
	svc.SaveQuality(source.QualityLossless)
	svc.SaveMode(2)
	svc.SavePlayingPlaylist("local-default")

	got := New(nil, st, nil).Preferences()
	if got.Volume != 0.5 || got.CompDB != 2 || got.Quality != source.QualityLossless || got.Mode != 2 || got.PlayingPlaylist != "local-default" {
		t.Fatalf("偏好回读不符: %+v", got)
	}
}

func TestPlaylistOpsAndQueueSave(t *testing.T) {
	st := openTestStore(t)
	svc := New(nil, st, nil)
	song := store.Song{Source: "fake", RemoteID: "1", Page: 1, Title: "A"}

	p, err := svc.CreatePlaylistWithSong("测试歌单", song)
	if err != nil {
		t.Fatalf("新建歌单失败: %v", err)
	}
	items, err := st.PlaylistSongs(p.ID)
	if err != nil || len(items) != 1 || items[0].Title != "A" {
		t.Fatalf("歌单曲目不符: %+v err=%v", items, err)
	}
	if err := svc.RemoveSongFromPlaylist(p.ID, items[0].ID); err != nil {
		t.Fatalf("移除歌曲失败: %v", err)
	}
	if err := svc.SaveQueue([]store.Song{song}, 0, 1234); err != nil {
		t.Fatalf("保存队列失败: %v", err)
	}
	q, err := st.LoadQueue()
	if err != nil || len(q.IDs) != 1 || q.Position != 1234 {
		t.Fatalf("队列回读不符: %+v err=%v", q, err)
	}
}

func TestResolvePrefersLocalDownload(t *testing.T) {
	st := openTestStore(t)
	remote := source.Stream{URL: "https://example.invalid/remote.flac", Quality: "无损"}
	src := &baseFake{
		id: "fake",
		resolved: source.Resolved{
			Track:  source.Track{Source: "fake", ExternalID: "1", Title: "远程标题"},
			Stream: remote,
		},
	}
	svc := New(source.NewRegistry(src), st, nil)
	song := SongFromTrack(source.Track{Source: "fake", ExternalID: "1", Title: "本地标题", Duration: 120})

	path := filepath.Join(t.TempDir(), "song.flac")
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		t.Fatalf("写入临时音频失败: %v", err)
	}
	if err := svc.RecordDownload(song, path, 5, "flac", 2*time.Minute); err != nil {
		t.Fatalf("登记下载失败: %v", err)
	}

	got, stream, err := svc.Resolve(context.Background(), song, source.QualityAuto)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if stream.URL != path || stream.Quality != "本地" || stream.Duration != 2*time.Minute {
		t.Fatalf("应优先本地流: %+v", stream)
	}
	if got.ID != song.ID {
		t.Fatalf("本地解析不应改变歌曲 ID: %s", got.ID)
	}

	svc.SaveLocalFirst(false)
	if _, stream, _ = svc.Resolve(context.Background(), song, source.QualityAuto); stream.URL != remote.URL {
		t.Fatalf("关闭本地优先后应走远程: %+v", stream)
	}

	svc.SaveLocalFirst(true)
	if _, stream, _ = svc.ResolveWithLocal(context.Background(), song, source.QualityAuto, false); stream.URL != remote.URL {
		t.Fatalf("显式禁用本地后应走远程: %+v", stream)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("删除临时音频失败: %v", err)
	}
	if _, stream, _ = svc.Resolve(context.Background(), song, source.QualityAuto); stream.URL != remote.URL {
		t.Fatalf("本地文件失效后应回落远程: %+v", stream)
	}

	entries, err := svc.Downloads()
	if err != nil || len(entries) != 1 || entries[0].Song.ID != song.ID {
		t.Fatalf("下载列表不符: %+v err=%v", entries, err)
	}
	if err := svc.DeleteDownload(song.ID); err != nil {
		t.Fatalf("删除下载记录失败: %v", err)
	}
	if entries, _ = svc.Downloads(); len(entries) != 0 {
		t.Fatalf("删除后应为空: %+v", entries)
	}
}
