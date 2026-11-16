package store

import (
	"path/filepath"
	"testing"
	"time"
)

// TestMigrateAddsLocalLibraryColumns 验证旧库升级：新列可重复添加，新表与索引就位。
func TestMigrateAddsLocalLibraryColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	// 模拟 v0.6 的 playlists：删掉新列再让 migrate 补回来。
	for _, col := range []struct {
		name string
		sql  string
	}{
		{"sync_excluded", `ALTER TABLE playlists DROP COLUMN sync_excluded`},
		{"root_path", `ALTER TABLE playlists DROP COLUMN root_path`},
		{"scan_status", `ALTER TABLE playlists DROP COLUMN scan_status`},
		{"scan_error", `ALTER TABLE playlists DROP COLUMN scan_error`},
		{"scanned_at", `ALTER TABLE playlists DROP COLUMN scanned_at`},
	} {
		if _, err := st.db.Exec(col.sql); err != nil {
			t.Fatalf("删除列 %s 失败: %v", col.name, err)
		}
	}
	st.Close()

	st, err = Open(path)
	if err != nil {
		t.Fatalf("升级数据库失败: %v", err)
	}
	defer st.Close()
	for _, col := range []string{"sync_excluded", "root_path", "scan_status", "scan_error", "scanned_at"} {
		has, err := st.hasColumn("playlists", col)
		if err != nil || !has {
			t.Fatalf("升级后应有列 %s: has=%v err=%v", col, has, err)
		}
	}
	// 再开一次不应报错（可重复执行）。
	st.Close()
	again, err := Open(path)
	if err != nil {
		t.Fatalf("重复升级应幂等: %v", err)
	}
	again.Close()
}

// TestFolderPlaylistCRUD 覆盖文件夹歌单的创建、改名、删除与文件索引清理。
func TestFolderPlaylistCRUD(t *testing.T) {
	st := openTestStore(t)
	dir := t.TempDir()
	p, err := st.CreateFolderPlaylist("", dir)
	if err != nil {
		t.Fatalf("创建文件夹歌单失败: %v", err)
	}
	if p.Kind != PlaylistKindFolder || !p.SyncExcluded || p.RootPath != filepath.Clean(dir) {
		t.Fatalf("文件夹歌单字段不符: %+v", p)
	}
	if p.Title != filepath.Base(dir) {
		t.Fatalf("标题应默认取目录名: %q", p.Title)
	}
	if err := st.RenamePlaylist(p.ID, "我的文件夹"); err != nil {
		t.Fatalf("文件夹歌单可以改名: %v", err)
	}
	items := []LibraryItem{{
		Song:        Song{Source: "local", RemoteID: "m:one", Page: 1, Title: "一"},
		Path:        filepath.Join(dir, "a.mp3"),
		Size:        10,
		ContentHash: "hash-a",
	}}
	if err := st.ReplaceLibraryItems(p.ID, items); err != nil {
		t.Fatalf("写入扫描结果失败: %v", err)
	}
	if files, err := st.LibraryFiles(p.ID); err != nil || len(files) != 1 || !files[0].Present {
		t.Fatalf("文件索引不符: %+v err=%v", files, err)
	}
	if err := st.DeletePlaylist(p.ID); err != nil {
		t.Fatalf("删除文件夹歌单失败: %v", err)
	}
	if files, err := st.LibraryFiles(p.ID); err != nil || len(files) != 0 {
		t.Fatalf("删除后文件索引应清空: %+v err=%v", files, err)
	}
	// songs 行保留，方便重新添加同一目录时复用注释。
	if _, err := st.Song(SongID("local", "m:one", 1)); err != nil {
		t.Fatalf("删除文件夹歌单不应删歌曲行: %v", err)
	}
}

// TestReplaceLibraryItemsMarksMissing 验证同一 meta 两份文件、缺失标记与恢复。
func TestReplaceLibraryItemsMarksMissing(t *testing.T) {
	st := openTestStore(t)
	dir := t.TempDir()
	p, err := st.CreateFolderPlaylist("", dir)
	if err != nil {
		t.Fatal(err)
	}
	song := NormalizeSong(Song{Source: "local", RemoteID: "m:same", Page: 1, Title: "同一首"})
	items := []LibraryItem{
		{Song: song, Path: filepath.Join(dir, "a.mp3"), ContentHash: "hash-a"},
		{Song: song, Path: filepath.Join(dir, "b.mp3"), ContentHash: "hash-b"},
	}
	if err := st.ReplaceLibraryItems(p.ID, items); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	songs, _ := st.PlaylistSongs(p.ID)
	if len(songs) != 1 {
		t.Fatalf("同一 meta 只应有一个歌单条目: %+v", songs)
	}
	files, _ := st.LibraryFiles(p.ID)
	if len(files) != 2 {
		t.Fatalf("两份文件都应留下索引: %+v", files)
	}

	// 第二次扫描只剩 b：a 标记为 missing，歌单条目仍在。
	if err := st.ReplaceLibraryItems(p.ID, items[1:]); err != nil {
		t.Fatal(err)
	}
	files, _ = st.LibraryFiles(p.ID)
	missing := 0
	for _, f := range files {
		if !f.Present {
			missing++
		}
	}
	if missing != 1 {
		t.Fatalf("应有一个缺失文件: %+v", files)
	}
	if paths, err := st.LibraryFilesBySong(song.ID); err != nil || len(paths) != 1 {
		t.Fatalf("本地曲目路径只应返回现存文件: %+v err=%v", paths, err)
	}
}

// TestLocalSongGuard 验证本地曲目只能进仅本机歌单，文件夹歌单只读。
func TestLocalSongGuard(t *testing.T) {
	st := openTestStore(t)
	synced, err := st.CreatePlaylist("同步歌单")
	if err != nil {
		t.Fatal(err)
	}
	localOnly, err := st.CreatePlaylistEx("仅本机", true)
	if err != nil {
		t.Fatal(err)
	}
	song := Song{Source: "local", RemoteID: "m:x", Page: 1, Title: "本地"}
	if err := st.AddSongToPlaylist(synced.ID, song); err == nil {
		t.Fatal("本地曲目不应能加入同步歌单")
	}
	if err := st.AddSongToPlaylist(localOnly.ID, song); err != nil {
		t.Fatalf("本地曲目应能加入仅本机歌单: %v", err)
	}
	folder, err := st.CreateFolderPlaylist("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSongToPlaylist(folder.ID, song); err == nil {
		t.Fatal("文件夹歌单应拒绝手动加歌")
	}
	if err := st.RemoveSongFromPlaylist(folder.ID, song.ID); err == nil {
		t.Fatal("文件夹歌单应拒绝手动删歌")
	}
}

// TestMixinSyncExclusion 验证引用仅本机歌单的混合歌单自动变成仅本机，
// 已同步的混合歌单不允许追加这类引用。
func TestMixinSyncExclusion(t *testing.T) {
	st := openTestStore(t)
	synced, _ := st.CreatePlaylist("同步歌单")
	localOnly, _ := st.CreatePlaylistEx("仅本机", true)

	mix, err := st.CreateMixin("仅本机混合", []string{localOnly.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !mix.SyncExcluded {
		t.Fatal("引用仅本机歌单的新混合歌单应自动仅本机")
	}

	syncedMix, err := st.CreateMixin("同步混合", []string{synced.ID})
	if err != nil {
		t.Fatal(err)
	}
	if syncedMix.SyncExcluded {
		t.Fatal("只引用同步歌单的混合歌单应参与同步")
	}
	if _, err := st.AddMixinRef(syncedMix.ID, localOnly.ID); err == nil {
		t.Fatal("已同步的混合歌单不应能引用仅本机歌单")
	}
	if err := st.SetMixinRefs(syncedMix.ID, []string{synced.ID, localOnly.ID}); err == nil {
		t.Fatal("SetMixinRefs 也应拦住同步→仅本机的引用")
	}
}

// TestSnapshotFiltersSyncExcluded 验证导出与导入删除共用同一份可同步集合。
func TestSnapshotFiltersSyncExcluded(t *testing.T) {
	st := openTestStore(t)
	synced, _ := st.CreatePlaylist("同步歌单")
	if err := st.AddSongToPlaylist(synced.ID, Song{Source: "bilibili", RemoteID: "BV1", Page: 1, Title: "平台歌"}); err != nil {
		t.Fatal(err)
	}
	localOnly, _ := st.CreatePlaylistEx("仅本机", true)
	folder, err := st.CreateFolderPlaylist("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snaps, err := st.ListPlaylistSnapshots("local", "mixin")
	if err != nil {
		t.Fatalf("列出快照失败: %v", err)
	}
	for _, snap := range snaps {
		if snap.ID == localOnly.ID || snap.ID == folder.ID {
			t.Fatalf("仅本机歌单不应出现在可同步快照里: %+v", snap.Playlist)
		}
	}
	if len(snaps) != 1 || snaps[0].ID != synced.ID {
		t.Fatalf("应只剩同步歌单: %+v", snaps)
	}
	if playlists, annotations, err := st.LocalOnlyCounts(); err != nil || playlists != 2 || annotations != 0 {
		t.Fatalf("仅本机计数不符: playlists=%d annotations=%d err=%v", playlists, annotations, err)
	}
}

// TestDownloadByPath 验证按路径复用平台下载曲目。
func TestDownloadByPath(t *testing.T) {
	st := openTestStore(t)
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := st.UpsertDownload(Download{SongID: "bilibili:BV1:1", Path: path, Size: 1, Codec: "mp3"}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.DownloadByPath(path + "/")
	if err != nil || !ok || got.SongID != "bilibili:BV1:1" {
		t.Fatalf("按路径应命中下载记录（含 / 归一）: %+v ok=%v err=%v", got, ok, err)
	}
	if _, ok, _ := st.DownloadByPath(path + ".other"); ok {
		t.Fatal("未登记路径不应命中")
	}
}

// TestLyricBindingKeysAndOffset 验证 portable/local 分开存、本机优先、offset 随源。
func TestLyricBindingKeysAndOffset(t *testing.T) {
	st := openTestStore(t)
	const songID = "bilibili:BV1:1"
	portable := LyricBinding{Kind: BindingKindSource, Source: "netease", ExternalID: "123", Page: 1, Title: "晴天"}
	if err := st.SaveLyricBinding(songID, portable); err != nil {
		t.Fatalf("保存平台绑定失败: %v", err)
	}
	if got, scope, _ := st.LyricBinding(songID); scope != BindingPortable || got.ExternalID != "123" {
		t.Fatalf("只存平台绑定时应返回 portable: %+v scope=%s", got, scope)
	}
	local := LyricBinding{Kind: BindingKindLocalFile, Path: "/tmp/x.lrc"}
	if err := st.SaveLyricBinding(songID, local); err != nil {
		t.Fatalf("保存本机绑定失败: %v", err)
	}
	if got, scope, _ := st.LyricBinding(songID); scope != BindingLocal || got.Path != "/tmp/x.lrc" {
		t.Fatalf("本机绑定应优先: %+v scope=%s", got, scope)
	}
	// 两个 key 互不覆盖。
	if _, ok := st.PortableLyricBinding(songID); !ok {
		t.Fatal("本机绑定不应覆盖平台绑定")
	}
	// offset 写进各自来源。
	if err := st.SetLyricOffset(songID, BindingPortable, 500); err != nil {
		t.Fatal(err)
	}
	if got := st.LyricOffset(songID, BindingPortable); got != 500 {
		t.Fatalf("平台绑定 offset 不符: %d", got)
	}
	if got := st.LyricOffset(songID, BindingLocal); got != 0 {
		t.Fatalf("本机绑定 offset 不应被平台绑定影响: %d", got)
	}
	if err := st.SetLyricOffset(songID, BindingLocal, -300); err != nil {
		t.Fatal(err)
	}
	if got := st.LyricOffset(songID, BindingLocal); got != -300 {
		t.Fatalf("本机绑定 offset 不符: %d", got)
	}
	if err := st.SetLyricOffset(songID, BindingAuto, 200); err != nil {
		t.Fatal(err)
	}
	if got := st.LyricOffset(songID, BindingAuto); got != 200 {
		t.Fatalf("自动 offset 不符: %d", got)
	}
	// portable 绑定缺失时 offset 回落到 auto，不写脏不存在的绑定。
	if err := st.SetLyricOffset("bilibili:BV2:1", BindingPortable, 100); err != nil {
		t.Fatal(err)
	}
	if got := st.LyricOffset("bilibili:BV2:1", BindingAuto); got != 100 {
		t.Fatalf("无绑定时 offset 应写 auto: %d", got)
	}
	if err := st.ClearLyricBinding(songID); err != nil {
		t.Fatal(err)
	}
	if _, scope, _ := st.LyricBinding(songID); scope != BindingAuto {
		t.Fatalf("解除后不应再有绑定: %s", scope)
	}
}

// TestAnnotationLyricBinding 验证注释里的绑定只导出 portable，删除注释不动本机绑定。
func TestAnnotationLyricBinding(t *testing.T) {
	st := openTestStore(t)
	const songID = "bilibili:BV1:1"
	bind := &LyricBinding{Kind: BindingKindLRCLIB, LRCLIBID: 42, Title: "晴天"}
	if err := st.SaveAnnotation(SongAnnotation{SongID: songID, LyricBind: bind}); err != nil {
		t.Fatalf("保存注释失败: %v", err)
	}
	all, err := st.ListAnnotations()
	if err != nil || len(all) != 1 || all[0].LyricBind == nil || all[0].LyricBind.LRCLIBID != 42 {
		t.Fatalf("注释未包含绑定: %+v err=%v", all, err)
	}
	if !all[0].Active() {
		t.Fatal("只有绑定的注释也应算 Active")
	}
	// 本机绑定不写进注释 key。
	if err := st.SaveLyricBinding(songID, LyricBinding{Kind: BindingKindLocalFile, Path: "/tmp/x.lrc"}); err != nil {
		t.Fatal(err)
	}
	all, _ = st.ListAnnotations()
	if len(all) != 1 || all[0].LyricBind == nil {
		t.Fatalf("本机绑定不应混进注释: %+v", all)
	}
	if err := st.DeleteAnnotation(songID); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.LocalLyricBinding(songID); !ok {
		t.Fatal("删除注释不应清掉本机绑定")
	}
}

// TestListSyncAnnotationsSkipsLocal 验证本地曲目上的注释不进同步集合。
func TestListSyncAnnotationsSkipsLocal(t *testing.T) {
	st := openTestStore(t)
	if err := st.SetClip("bilibili:BV1:1", Clip{StartMS: 1000}); err != nil {
		t.Fatal(err)
	}
	// 本地曲目行 + 一条 local: 孤儿注释。
	if _, err := st.UpsertSong(Song{Source: "local", RemoteID: "m:x", Page: 1, Title: "本地"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetClip(SongID("local", "m:x", 1), Clip{StartMS: 500}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetClip("local:orphan:1", Clip{StartMS: 700}); err != nil {
		t.Fatal(err)
	}
	all, _ := st.ListAnnotations()
	if len(all) != 3 {
		t.Fatalf("应有三条注释: %+v", all)
	}
	syncable, err := st.ListSyncAnnotations()
	if err != nil {
		t.Fatal(err)
	}
	if len(syncable) != 1 || syncable[0].SongID != "bilibili:BV1:1" {
		t.Fatalf("同步注释只应保留平台歌曲: %+v", syncable)
	}
	if _, annotations, _ := st.LocalOnlyCounts(); annotations != 2 {
		t.Fatalf("本机注释计数不符: %d", annotations)
	}
}

// TestSetSongDurationIfEmpty 只补空缺时长，不覆盖已有值。
func TestSetSongDurationIfEmpty(t *testing.T) {
	st := openTestStore(t)
	song, err := st.UpsertSong(Song{Source: "local", RemoteID: "m:y", Page: 1, Title: "本地"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSongDurationIfEmpty(song.ID, 123); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Song(song.ID)
	if got.Duration != 123 {
		t.Fatalf("应回写时长: %d", got.Duration)
	}
	if err := st.SetSongDurationIfEmpty(song.ID, 999); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Song(song.ID)
	if got.Duration != 123 {
		t.Fatalf("不应覆盖已有时长: %d", got.Duration)
	}
}

// TestUpdatePlaylistScanKeepsItems 验证目录不可用只改状态、不清条目。
func TestUpdatePlaylistScanKeepsItems(t *testing.T) {
	st := openTestStore(t)
	dir := t.TempDir()
	p, _ := st.CreateFolderPlaylist("", dir)
	if err := st.ReplaceLibraryItems(p.ID, []LibraryItem{{
		Song: Song{Source: "local", RemoteID: "m:z", Page: 1, Title: "本地"},
		Path: filepath.Join(dir, "z.mp3"), ContentHash: "h",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdatePlaylistScan(p.ID, "missing", "文件夹不可用"); err != nil {
		t.Fatal(err)
	}
	updated, _ := st.Playlist(p.ID)
	if updated.ScanStatus != "missing" || updated.ScanError == "" {
		t.Fatalf("扫描状态未更新: %+v", updated)
	}
	if songs, _ := st.PlaylistSongs(p.ID); len(songs) != 1 {
		t.Fatalf("目录不可用不应清空条目: %+v", songs)
	}
	if updated.ScannedAt == 0 || time.Since(time.Unix(updated.ScannedAt, 0)) > time.Minute {
		t.Fatalf("scanned_at 未更新: %d", updated.ScannedAt)
	}
}
