package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestUpsertSongIsIdempotent(t *testing.T) {
	st := openTestStore(t)

	first, err := st.UpsertSong(Song{Source: "bilibili", BVID: "BV1xx411c7mD", Page: 1, Title: "旧标题", Artist: "UP"})
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	second, err := st.UpsertSong(Song{Source: "bilibili", BVID: "BV1xx411c7mD", Page: 1, Title: "新标题", Artist: "UP"})
	if err != nil {
		t.Fatalf("二次写入失败: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("同一 BV 同一分 P 应得到同一 ID: %s vs %s", first.ID, second.ID)
	}
	if second.Title != "新标题" {
		t.Fatalf("标题未更新: %q", second.Title)
	}

	// 不同分 P 是不同条目
	third, err := st.UpsertSong(Song{Source: "bilibili", BVID: "BV1xx411c7mD", Page: 2, Title: "P2"})
	if err != nil {
		t.Fatalf("写入 P2 失败: %v", err)
	}
	if third.ID == first.ID {
		t.Fatal("不同分 P 的 ID 不应相同")
	}
}

func TestPlaylistLifecycle(t *testing.T) {
	st := openTestStore(t)

	if _, err := st.EnsurePlaylist(Playlist{ID: "local-default", Title: "本地歌单", Kind: "local"}); err != nil {
		t.Fatalf("建歌单失败: %v", err)
	}
	songs := []Song{
		{Source: "bilibili", BVID: "BV1", Page: 1, Title: "A", Duration: 100},
		{Source: "bilibili", BVID: "BV2", Page: 1, Title: "B", Duration: 200},
	}
	for _, s := range songs {
		if err := st.AddSongToPlaylist("local-default", s); err != nil {
			t.Fatalf("加入歌单失败: %v", err)
		}
	}
	// 重复加入不应产生两条
	if err := st.AddSongToPlaylist("local-default", songs[0]); err != nil {
		t.Fatalf("重复加入失败: %v", err)
	}

	got, err := st.PlaylistSongs("local-default")
	if err != nil {
		t.Fatalf("读取歌单失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("期望 2 首，实际 %d", len(got))
	}

	p, err := st.Playlist("local-default")
	if err != nil {
		t.Fatalf("读取歌单元信息失败: %v", err)
	}
	if p.Count != 2 {
		t.Fatalf("歌单计数应为 2，实际 %d", p.Count)
	}

	if err := st.RemoveSongFromPlaylist("local-default", got[0].ID); err != nil {
		t.Fatalf("移除失败: %v", err)
	}
	got, _ = st.PlaylistSongs("local-default")
	if len(got) != 1 {
		t.Fatalf("移除后期望 1 首，实际 %d", len(got))
	}
}

func TestReplacePlaylistSongs(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.EnsurePlaylist(Playlist{ID: "fav", Title: "收藏夹", Kind: "remote", RemoteID: "123"}); err != nil {
		t.Fatalf("建歌单失败: %v", err)
	}
	first := []Song{{Source: "bilibili", BVID: "BV1", Page: 1, Title: "A"}}
	if err := st.ReplacePlaylistSongs("fav", first); err != nil {
		t.Fatalf("替换失败: %v", err)
	}
	second := []Song{
		{Source: "bilibili", BVID: "BV2", Page: 1, Title: "B"},
		{Source: "bilibili", BVID: "BV3", Page: 1, Title: "C"},
	}
	if err := st.ReplacePlaylistSongs("fav", second); err != nil {
		t.Fatalf("二次替换失败: %v", err)
	}
	got, _ := st.PlaylistSongs("fav")
	if len(got) != 2 || got[0].BVID != "BV2" || got[1].BVID != "BV3" {
		t.Fatalf("替换结果不符: %+v", got)
	}
}

func TestQueueRoundTrip(t *testing.T) {
	st := openTestStore(t)
	for _, bv := range []string{"BV1", "BV2", "BV3"} {
		if _, err := st.UpsertSong(Song{Source: "bilibili", BVID: bv, Page: 1, Title: bv}); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}
	ids := []string{SongID("bilibili", "BV2", 1), SongID("bilibili", "BV3", 1)}
	if err := st.SaveQueue(QueueState{IDs: ids, Index: 1, Position: 1234}); err != nil {
		t.Fatalf("保存队列失败: %v", err)
	}
	state, err := st.LoadQueue()
	if err != nil {
		t.Fatalf("读取队列失败: %v", err)
	}
	if len(state.IDs) != 2 || state.IDs[0] != ids[0] || state.Index != 1 || state.Position != 1234 {
		t.Fatalf("队列状态不符: %+v", state)
	}
	songs, err := st.SongsByIDs(state.IDs)
	if err != nil || len(songs) != 2 {
		t.Fatalf("按 ID 取歌失败: %v %+v", err, songs)
	}
}

func TestSettingsAndAccount(t *testing.T) {
	st := openTestStore(t)

	if got := st.Setting("volume", "0.8"); got != "0.8" {
		t.Fatalf("默认值未生效: %q", got)
	}
	if err := st.SetSetting("volume", "0.5"); err != nil {
		t.Fatalf("写设置失败: %v", err)
	}
	if got := st.Setting("volume", "0.8"); got != "0.5" {
		t.Fatalf("设置未持久化: %q", got)
	}

	if _, err := st.LoadAccount("bilibili"); err != ErrNotFound {
		t.Fatalf("未登录时应返回 ErrNotFound，实际 %v", err)
	}
	if err := st.SaveAccount("bilibili", []byte(`{"SESSDATA":"x"}`)); err != nil {
		t.Fatalf("保存登录态失败: %v", err)
	}
	payload, err := st.LoadAccount("bilibili")
	if err != nil || string(payload) != `{"SESSDATA":"x"}` {
		t.Fatalf("登录态读取不符: %v %q", err, payload)
	}
}

func TestPlaylistCRUD(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.EnsurePlaylist(Playlist{ID: "local-default", Title: "本地歌单", Kind: "local"}); err != nil {
		t.Fatalf("建内置歌单失败: %v", err)
	}
	p, err := st.CreatePlaylist("  通勤  ")
	if err != nil {
		t.Fatalf("新建歌单失败: %v", err)
	}
	if p.Title != "通勤" || p.Kind != "local" || p.Count != 0 {
		t.Fatalf("新歌单字段不符: %+v", p)
	}
	song := Song{Source: "bilibili", BVID: "BV1", Page: 1, Title: "A"}
	if err := st.AddSongToPlaylist(p.ID, song); err != nil {
		t.Fatalf("加入新歌单失败: %v", err)
	}
	if err := st.RenamePlaylist(p.ID, "上班路上"); err != nil {
		t.Fatalf("重命名失败: %v", err)
	}
	got, err := st.Playlist(p.ID)
	if err != nil || got.Title != "上班路上" || got.Count != 1 {
		t.Fatalf("重命名后不符: %+v %v", got, err)
	}
	if err := st.DeletePlaylist(p.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := st.Playlist(p.ID); err != ErrNotFound {
		t.Fatalf("删除后应不存在，实际 %v", err)
	}
	if err := st.DeletePlaylist("local-default"); err != nil {
		t.Fatalf("默认本地歌单应允许删除: %v", err)
	}
	if _, err := st.Playlist("local-default"); err != ErrNotFound {
		t.Fatalf("删除后应不存在，实际 %v", err)
	}
	if _, err := st.CreatePlaylist("   "); err == nil {
		t.Fatal("空歌单名应报错")
	}
}

func TestRemotePlaylistCannotBeRenamedOrDeleted(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.EnsurePlaylist(Playlist{ID: "bili-fav-1", Title: "收藏夹", Kind: "remote", RemoteID: "1"}); err != nil {
		t.Fatalf("建远端歌单失败: %v", err)
	}
	if err := st.RenamePlaylist("bili-fav-1", "改名"); err == nil {
		t.Fatal("远端歌单不应允许重命名")
	}
	if err := st.DeletePlaylist("bili-fav-1"); err == nil {
		t.Fatal("远端歌单不应允许删除")
	}
}

func TestDeleteRemotePlaylists(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.EnsurePlaylist(Playlist{ID: "bili-fav-1", Title: "收藏夹", Kind: "remote", RemoteID: "1"}); err != nil {
		t.Fatalf("建远端歌单失败: %v", err)
	}
	if _, err := st.EnsurePlaylist(Playlist{ID: "local-default", Title: "本地歌单", Kind: "local"}); err != nil {
		t.Fatalf("建本地歌单失败: %v", err)
	}
	song := Song{Source: "bilibili", BVID: "BV1", Page: 1, Title: "A"}
	if err := st.AddSongToPlaylist("bili-fav-1", song); err != nil {
		t.Fatalf("加入远端歌单失败: %v", err)
	}
	if err := st.DeleteRemotePlaylists(); err != nil {
		t.Fatalf("清理远端歌单失败: %v", err)
	}
	items, err := st.ListPlaylists()
	if err != nil {
		t.Fatalf("列出歌单失败: %v", err)
	}
	if len(items) != 1 || items[0].ID != "local-default" {
		t.Fatalf("应只剩本地歌单: %+v", items)
	}
}

func TestMigrateLegacyBilibiliSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开旧库失败: %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE songs (
	id TEXT PRIMARY KEY, source TEXT NOT NULL, bvid TEXT NOT NULL,
	page INTEGER NOT NULL DEFAULT 1, cid INTEGER NOT NULL DEFAULT 0,
	title TEXT NOT NULL, artist TEXT NOT NULL DEFAULT '',
	duration INTEGER NOT NULL DEFAULT 0, cover TEXT NOT NULL DEFAULT '',
	added_at INTEGER NOT NULL, UNIQUE(source, bvid, page)
);
CREATE TABLE playlists (
	id TEXT PRIMARY KEY, title TEXT NOT NULL, kind TEXT NOT NULL DEFAULT 'local',
	remote_id TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL
);
CREATE TABLE playlist_items (
	playlist_id TEXT NOT NULL, song_id TEXT NOT NULL,
	position INTEGER NOT NULL, added_at INTEGER NOT NULL,
	PRIMARY KEY (playlist_id, song_id)
);
INSERT INTO songs (id,source,bvid,page,cid,title,artist,duration,cover,added_at)
VALUES ('bilibili:BV1:1','bilibili','BV1',1,123,'旧歌','UP',100,'',1);
INSERT INTO playlists (id,title,kind,remote_id,created_at)
VALUES ('bili-fav-9','旧收藏夹','remote','9',1);
INSERT INTO playlist_items (playlist_id,song_id,position,added_at)
VALUES ('bili-fav-9','bilibili:BV1:1',1,1);
`)
	if err != nil {
		t.Fatalf("准备旧库失败: %v", err)
	}
	_ = db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	song, err := st.Song("bilibili:BV1:1")
	if err != nil {
		t.Fatalf("迁移后读歌失败: %v", err)
	}
	if song.RemoteID != "BV1" || song.BVID != "BV1" || song.CID != 123 {
		t.Fatalf("迁移后字段不符: %+v", song)
	}
	p, err := st.Playlist("bili-fav-9")
	if err != nil || p.Source != "bilibili" {
		t.Fatalf("歌单来源未回填: %+v %v", p, err)
	}

	// 新表必须允许 BVID 为空的网易云歌曲，且不同来源可共用 page=1。
	ne, err := st.UpsertSong(Song{Source: "netease", RemoteID: "1431292823", Page: 1, Title: "夜航星", Artist: "不才"})
	if err != nil {
		t.Fatalf("写入网易云歌曲失败: %v", err)
	}
	if ne.ID != "netease:1431292823:1" || ne.BVID != "" {
		t.Fatalf("网易云歌曲 ID 不符: %+v", ne)
	}
}

func TestCleanRemotePlaylistsOnce(t *testing.T) {
	st := openTestStore(t)
	for _, p := range []Playlist{
		{ID: "local-default", Title: "本地歌单", Kind: "local"},
		{ID: "netease-pl-1", Title: "云歌单", Kind: "remote", Source: "netease", RemoteID: "1"},
	} {
		if _, err := st.EnsurePlaylist(p); err != nil {
			t.Fatalf("建歌单失败: %v", err)
		}
	}
	cleaned, err := st.CleanRemotePlaylistsOnce("cleanup-test")
	if err != nil || !cleaned {
		t.Fatalf("首次清理应执行: cleaned=%v err=%v", cleaned, err)
	}
	items, err := st.ListPlaylists()
	if err != nil {
		t.Fatalf("列出歌单失败: %v", err)
	}
	if len(items) != 1 || items[0].ID != "local-default" {
		t.Fatalf("远端歌单应被清空，本地保留: %+v", items)
	}
	cleaned, err = st.CleanRemotePlaylistsOnce("cleanup-test")
	if err != nil || cleaned {
		t.Fatalf("第二次清理不应再执行: cleaned=%v err=%v", cleaned, err)
	}
}

func TestMixinPlaylistAndUnlock(t *testing.T) {
	st := openTestStore(t)
	for _, p := range []Playlist{
		{ID: "a", Title: "A", Kind: "local"},
		{ID: "b", Title: "B", Kind: "remote", Source: "netease", RemoteID: "2"},
	} {
		if _, err := st.EnsurePlaylist(p); err != nil {
			t.Fatalf("建歌单失败: %v", err)
		}
	}
	s1 := Song{Source: "bilibili", BVID: "BV1", Page: 1, Title: "共同"}
	s2 := Song{Source: "bilibili", BVID: "BV2", Page: 1, Title: "A 独有"}
	s3 := Song{Source: "netease", RemoteID: "3", Page: 1, Title: "B 独有"}
	for _, add := range []struct {
		id   string
		song Song
	}{{"a", s1}, {"a", s2}, {"b", s1}, {"b", s3}} {
		if err := st.AddSongToPlaylist(add.id, add.song); err != nil {
			t.Fatalf("加歌失败: %v", err)
		}
	}

	mix, err := st.CreateMixin("混合", []string{"a", "b"})
	if err != nil {
		t.Fatalf("建混合歌单失败: %v", err)
	}
	if mix.Kind != "mixin" || mix.Count != 3 {
		t.Fatalf("混合歌单初始状态不符: %+v", mix)
	}
	songs, err := st.PlaylistSongs(mix.ID)
	if err != nil {
		t.Fatalf("读混合歌单失败: %v", err)
	}
	if len(songs) != 3 || songs[0].Title != "共同" || songs[1].Title != "A 独有" || songs[2].Title != "B 独有" {
		t.Fatalf("混合顺序/去重不符: %+v", songs)
	}

	own := Song{Source: "bilibili", BVID: "BV4", Page: 1, Title: "自己加的"}
	if err := st.AddSongToPlaylist(mix.ID, own); err != nil {
		t.Fatalf("混合歌单加歌失败: %v", err)
	}
	songs, _ = st.PlaylistSongs(mix.ID)
	if len(songs) != 4 || songs[3].Title != "自己加的" {
		t.Fatalf("自己加的应排在末尾: %+v", songs)
	}

	// 删除继承曲目只在混合歌单里隐藏，源歌单不变。
	if err := st.RemoveSongFromPlaylist(mix.ID, SongID("bilibili", "BV1", 1)); err != nil {
		t.Fatalf("隐藏继承曲目失败: %v", err)
	}
	songs, _ = st.PlaylistSongs(mix.ID)
	if len(songs) != 3 {
		t.Fatalf("隐藏后数量不符: %+v", songs)
	}
	srcSongs, _ := st.PlaylistSongs("a")
	if len(srcSongs) != 2 {
		t.Fatalf("源歌单不应被修改: %+v", srcSongs)
	}

	// 被引用的歌单不能删除；移除引用后可以。
	if err := st.DeletePlaylist("a"); err == nil {
		t.Fatal("被引用的歌单不应允许删除")
	}
	if err := st.SetMixinRefs(mix.ID, []string{"b"}); err != nil {
		t.Fatalf("更新引用失败: %v", err)
	}
	if err := st.DeletePlaylist("a"); err != nil {
		t.Fatalf("移除引用后应可删除: %v", err)
	}

	// 解锁远端歌单：保留曲目，变成普通本地歌单，混合歌单继续引用。
	unlocked, err := st.UnlockPlaylist("b")
	if err != nil {
		t.Fatalf("解锁失败: %v", err)
	}
	if unlocked.Kind != "local" || unlocked.Source != "" || unlocked.Count != 2 {
		t.Fatalf("解锁结果不符: %+v", unlocked)
	}
	songs, _ = st.PlaylistSongs(mix.ID)
	if len(songs) != 2 {
		t.Fatalf("解锁后混合歌单应继续可用: %+v", songs)
	}

	if _, err := st.CreateMixin("嵌套", []string{mix.ID}); err == nil {
		t.Fatal("混合歌单不应允许嵌套引用")
	}
}

func TestSearchSongs(t *testing.T) {
	st := openTestStore(t)
	for _, s := range []Song{
		{Source: "bilibili", BVID: "BV1", Page: 1, Title: "夜航星", Artist: "不才"},
		{Source: "netease", RemoteID: "2", Page: 1, Title: "Night Voyager", Artist: "蔡明希", Album: "夜航"},
	} {
		if _, err := st.UpsertSong(s); err != nil {
			t.Fatalf("写入歌曲失败: %v", err)
		}
	}
	got, err := st.SearchSongs("夜航", 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("本地搜索不符: %+v err=%v", got, err)
	}
	got, err = st.SearchSongs("night", 10)
	if err != nil || len(got) != 1 || got[0].Title != "Night Voyager" {
		t.Fatalf("大小写搜索不符: %+v err=%v", got, err)
	}
}

func TestDownloadLifecycle(t *testing.T) {
	st := openTestStore(t)
	song, err := st.UpsertSong(Song{Source: "bilibili", BVID: "BV1", Page: 1, Title: "夜航星", Duration: 287})
	if err != nil {
		t.Fatalf("写入歌曲失败: %v", err)
	}
	first := Download{SongID: song.ID, Path: "/music/夜航星.flac", Size: 10, Codec: "flac", DurationMS: 287000}
	if err := st.UpsertDownload(first); err != nil {
		t.Fatalf("写入下载记录失败: %v", err)
	}
	got, err := st.Download(song.ID)
	if err != nil {
		t.Fatalf("读取下载记录失败: %v", err)
	}
	if got.Path != first.Path || got.Codec != "flac" || got.DurationMS != 287000 {
		t.Fatalf("下载记录不符: %+v", got)
	}
	created := got.CreatedAt

	second := first
	second.Path = "/music/夜航星.m4a"
	second.Codec = "aac"
	if err := st.UpsertDownload(second); err != nil {
		t.Fatalf("更新下载记录失败: %v", err)
	}
	got, _ = st.Download(song.ID)
	if got.Path != second.Path || got.Codec != "aac" {
		t.Fatalf("更新结果不符: %+v", got)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("重复下载应保留 created_at: %v -> %v", created, got.CreatedAt)
	}

	entries, err := st.DownloadEntries()
	if err != nil || len(entries) != 1 || entries[0].Song.Title != "夜航星" {
		t.Fatalf("下载条目不符: %+v err=%v", entries, err)
	}

	if err := st.DeleteDownload(song.ID); err != nil {
		t.Fatalf("删除下载记录失败: %v", err)
	}
	if _, err := st.Download(song.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后应返回 ErrNotFound: %v", err)
	}
}

func TestDownloadEntriesKeepOrphanRows(t *testing.T) {
	st := openTestStore(t)
	if err := st.UpsertDownload(Download{SongID: "missing", Path: "/tmp/x"}); err != nil {
		t.Fatalf("写入孤立下载记录失败: %v", err)
	}
	entries, err := st.DownloadEntries()
	if err != nil || len(entries) != 1 {
		t.Fatalf("孤立记录应保留: %+v err=%v", entries, err)
	}
	if entries[0].Song.ID != "" {
		t.Fatalf("孤立记录不应有歌曲元数据: %+v", entries[0].Song)
	}
}
