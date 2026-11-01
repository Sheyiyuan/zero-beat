package sync

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "zero-beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func seedSnapshot(t *testing.T, st *store.Store) (localID, mixID string) {
	t.Helper()
	local, err := st.CreatePlaylist("通勤")
	if err != nil {
		t.Fatal(err)
	}
	songs := []store.Song{
		{Source: "bilibili", RemoteID: "BV1xx411c7mD", BVID: "BV1xx411c7mD", Page: 1, Title: "曲名一", Artist: "UP 主", Duration: 213},
		{Source: "netease", RemoteID: "1431292823", Page: 1, Title: "曲名二", Artist: "歌手", Album: "专辑", Duration: 304},
	}
	for _, song := range songs {
		if err := st.AddSongToPlaylist(local.ID, song); err != nil {
			t.Fatal(err)
		}
	}
	ref, err := st.CreatePlaylist("收藏")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSongToPlaylist(ref.ID, store.Song{Source: "bilibili", RemoteID: "BV2", BVID: "BV2", Page: 1, Title: "引用曲目", Duration: 100}); err != nil {
		t.Fatal(err)
	}

	mix, err := st.CreateMixin("周末", []string{ref.ID, local.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSongToPlaylist(mix.ID, store.Song{Source: "bilibili", RemoteID: "BV3", BVID: "BV3", Page: 1, Title: "自己加的", Duration: 120}); err != nil {
		t.Fatal(err)
	}
	// 隐藏一首继承曲目，验证 excludes 导出。
	inherited := store.SongID("bilibili", "BV2", 1)
	if err := st.RemoveSongFromPlaylist(mix.ID, inherited); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveAnnotation(store.SongAnnotation{SongID: "bilibili:BV1xx411c7mD:1", LyricOffsetMS: -500, ClipStartMS: 1000, ClipEndMS: 2000}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveAccountAt("bilibili", []byte(`{"SESSDATA":"secret"}`), 1761980000); err != nil {
		t.Fatal(err)
	}
	return local.ID, mix.ID
}

func TestSnapshotDeterministic(t *testing.T) {
	st := openTestStore(t)
	_, _ = seedSnapshot(t, st)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	key := bytes.Repeat([]byte{3}, KeySize)
	keymapPath := filepath.Join(dir, "keymap.toml")
	if err := os.WriteFile(keymapPath, []byte("[keys]\nquit = [\"Q\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1761980000, 0)
	first, err := BuildSnapshot(SnapshotInput{Store: st, KeymapPath: keymapPath, Key: key, KeyVersion: 1, Device: "dev1", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSnapshot(repo, first); err != nil {
		t.Fatal(err)
	}
	second, err := BuildSnapshot(SnapshotInput{
		Store: st, KeymapPath: keymapPath, Key: key, KeyVersion: 1, Device: "dev1",
		ExistingDir: repo, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Files) != len(second.Files) {
		t.Fatalf("两次导出文件数不同: %d vs %d", len(first.Files), len(second.Files))
	}
	for path, want := range first.Files {
		got, ok := second.Files[path]
		if !ok {
			t.Fatalf("第二次导出缺少 %s", path)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("文件 %s 两次导出不一致", path)
		}
	}
	if first.Playlists != 3 || first.Annotations != 1 || first.Accounts != 1 {
		t.Fatalf("导出计数不符: %+v", first)
	}
}

func TestSnapshotImportRoundTrip(t *testing.T) {
	src := openTestStore(t)
	localID, mixID := seedSnapshot(t, src)
	dir := t.TempDir()
	key := bytes.Repeat([]byte{4}, KeySize)
	snap, err := BuildSnapshot(SnapshotInput{Store: src, Key: key, KeyVersion: 1, Device: "dev1", Now: time.Unix(1761980000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "repo")
	if err := WriteSnapshot(repo, snap); err != nil {
		t.Fatal(err)
	}

	dst := openTestStore(t)
	res, err := ImportSnapshot(ImportInput{Store: dst, Dir: repo, ArchiveDir: filepath.Join(dir, "sync"), Now: time.Unix(1761980000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Playlists != 3 || res.Annotations != 1 {
		t.Fatalf("导入计数不符: %+v", res)
	}
	gotLocal, err := dst.PlaylistSnapshot(localID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotLocal.Songs) != 2 || gotLocal.Songs[0].Title != "曲名一" || gotLocal.Songs[1].Title != "曲名二" {
		t.Fatalf("本地歌单导入顺序不符: %+v", gotLocal.Songs)
	}
	if gotLocal.CreatedAt == 0 {
		t.Fatal("应保留 created_at")
	}
	if gotLocal.Songs[0].AddedAt.IsZero() {
		t.Fatal("应保留 added_at")
	}
	gotMix, err := dst.PlaylistSnapshot(mixID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotMix.Refs) != 2 || gotMix.Refs[0].Title != "收藏" || gotMix.Refs[1].Title != "通勤" {
		t.Fatalf("混合歌单引用顺序不符: %+v", gotMix.Refs)
	}
	if len(gotMix.Excludes) != 1 || gotMix.Excludes[0] != store.SongID("bilibili", "BV2", 1) {
		t.Fatalf("excludes 不符: %+v", gotMix.Excludes)
	}
	annotations, err := dst.ListAnnotations()
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 1 || annotations[0].ClipEndMS != 2000 || annotations[0].LyricOffsetMS != -500 {
		t.Fatalf("注释导入不符: %+v", annotations)
	}

	// 幂等：再导一次结果不变。
	if _, err := ImportSnapshot(ImportInput{Store: dst, Dir: repo, ArchiveDir: filepath.Join(dir, "sync"), Now: time.Unix(1761980000, 1)}); err != nil {
		t.Fatal(err)
	}
	after, err := dst.PlaylistSnapshot(localID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Songs) != 2 {
		t.Fatalf("重复导入不应改变曲目数: %+v", after.Songs)
	}
}

func TestImportDeletesMissingPlaylist(t *testing.T) {
	src := openTestStore(t)
	_, _ = seedSnapshot(t, src)
	dir := t.TempDir()
	snap, err := BuildSnapshot(SnapshotInput{Store: src})
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "repo")
	if err := WriteSnapshot(repo, snap); err != nil {
		t.Fatal(err)
	}
	dst := openTestStore(t)
	if _, err := ImportSnapshot(ImportInput{Store: dst, Dir: repo, ArchiveDir: filepath.Join(dir, "sync")}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(repo, PlaylistsDir))
	if err != nil || len(entries) == 0 {
		t.Fatalf("仓库里应有歌单文件: %v", err)
	}
	if err := os.Remove(filepath.Join(repo, PlaylistsDir, entries[0].Name())); err != nil {
		t.Fatal(err)
	}
	res, err := ImportSnapshot(ImportInput{Store: dst, Dir: repo, ArchiveDir: filepath.Join(dir, "sync"), Now: time.Unix(1761980000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Deleted) != 1 {
		t.Fatalf("应删除一个歌单，实际 %+v", res.Deleted)
	}
	archived, err := filepath.Glob(filepath.Join(dir, "sync", "archive", "*", "deleted-playlists", "*.json"))
	if err != nil || len(archived) != 1 {
		t.Fatalf("删除前应归档，实际 %v err=%v", archived, err)
	}
}

func TestValidatePlaylistSnapshot(t *testing.T) {
	cases := []struct {
		name string
		snap store.PlaylistSnapshot
		ok   bool
	}{
		{"正常", store.PlaylistSnapshot{Playlist: store.Playlist{ID: "local-1", Title: "a", Kind: "local"}}, true},
		{"混合", store.PlaylistSnapshot{Playlist: store.Playlist{ID: "mix-1", Title: "a", Kind: "mixin"}}, true},
		{"路径字符", store.PlaylistSnapshot{Playlist: store.Playlist{ID: "local-../x", Title: "a", Kind: "local"}}, false},
		{"斜杠", store.PlaylistSnapshot{Playlist: store.Playlist{ID: "local-a/b", Title: "a", Kind: "local"}}, false},
		{"前缀不符", store.PlaylistSnapshot{Playlist: store.Playlist{ID: "mix-1", Title: "a", Kind: "local"}}, false},
		{"未知类型", store.PlaylistSnapshot{Playlist: store.Playlist{ID: "local-1", Title: "a", Kind: "weird"}}, false},
		{"空标题", store.PlaylistSnapshot{Playlist: store.Playlist{ID: "local-1", Kind: "local"}}, false},
		{"引用自己", store.PlaylistSnapshot{Playlist: store.Playlist{ID: "mix-1", Title: "a", Kind: "mixin"}, Refs: []store.Playlist{{ID: "mix-1", Kind: "remote"}}}, false},
	}
	for _, c := range cases {
		err := store.ValidatePlaylistSnapshot(c.snap)
		if (err == nil) != c.ok {
			t.Fatalf("%s: err=%v", c.name, err)
		}
	}
}
