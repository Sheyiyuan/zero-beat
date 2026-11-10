package sync

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

// seedLocalLibrary 在同一个库里放：可同步歌单、文件夹歌单、仅本机歌单、
// 本地曲目注释、平台歌曲上的 portable/local 绑定。
func seedLocalLibrary(t *testing.T, st *store.Store) (syncedID, folderID, localOnlyID, localSongID string) {
	t.Helper()
	synced, err := st.CreatePlaylist("通勤")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSongToPlaylist(synced.ID, store.Song{Source: "bilibili", RemoteID: "BV1", BVID: "BV1", Page: 1, Title: "平台歌", Duration: 200}); err != nil {
		t.Fatal(err)
	}
	folder, err := st.CreateFolderPlaylist("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	localOnly, err := st.CreatePlaylistEx("仅本机", true)
	if err != nil {
		t.Fatal(err)
	}
	localSong, err := st.UpsertSong(store.Song{Source: "local", RemoteID: "m:local", Page: 1, Title: "本地歌"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSongToPlaylist(localOnly.ID, localSong); err != nil {
		t.Fatal(err)
	}
	// 本地曲目上的注释：不能进仓库。
	if err := st.SaveAnnotation(store.SongAnnotation{SongID: localSong.ID, LyricOffsetMS: 300}); err != nil {
		t.Fatal(err)
	}
	// 平台歌曲上的 portable 绑定 + 本机绑定：只有 portable 进仓库。
	if err := st.SaveAnnotation(store.SongAnnotation{
		SongID:    "bilibili:BV1:1",
		LyricBind: &store.LyricBinding{Kind: store.BindingKindSource, Source: "netease", ExternalID: "42", Page: 1, Title: "晴天"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveLyricBinding("bilibili:BV1:1", store.LyricBinding{Kind: store.BindingKindLocalFile, Path: "/tmp/secret.lrc"}); err != nil {
		t.Fatal(err)
	}
	return synced.ID, folder.ID, localOnly.ID, localSong.ID
}

func TestSnapshotExcludesLocalLibraryData(t *testing.T) {
	st := openTestStore(t)
	syncedID, folderID, localOnlyID, localSongID := seedLocalLibrary(t, st)
	snap, err := BuildSnapshot(SnapshotInput{Store: st, Now: time.Unix(1761980000, 0)})
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if _, ok := snap.Files[PlaylistPath(folderID)]; ok {
		t.Fatal("文件夹歌单不应出现在仓库")
	}
	if _, ok := snap.Files[PlaylistPath(localOnlyID)]; ok {
		t.Fatal("仅本机歌单不应出现在仓库")
	}
	if _, ok := snap.Files[PlaylistPath(syncedID)]; !ok {
		t.Fatal("普通歌单应在仓库")
	}
	if _, ok := snap.Files[AnnotationPath(localSongID)]; ok {
		t.Fatal("本地曲目注释不应出现在仓库")
	}
	data, ok := snap.Files[AnnotationPath("bilibili:BV1:1")]
	if !ok {
		t.Fatal("平台歌曲注释应在仓库")
	}
	var file AnnotationFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("解析注释失败: %v", err)
	}
	if file.LyricBind == nil || file.LyricBind.Kind != store.BindingKindSource {
		t.Fatalf("portable 绑定应导出: %+v", file)
	}
	if file.LyricBind.Path != "" {
		t.Fatal("本机绑定不应混进导出")
	}
	if snap.Playlists != 1 {
		t.Fatalf("只应导出一个歌单: %d", snap.Playlists)
	}
}

func TestSnapshotGuardRejectsLocalSongInSyncedPlaylist(t *testing.T) {
	st := openTestStore(t)
	synced, err := st.CreatePlaylist("同步")
	if err != nil {
		t.Fatal(err)
	}
	// ReplacePlaylistSongs 是远端镜像路径，没有本地曲目守卫；用它制造不一致状态。
	if err := st.ReplacePlaylistSongs(synced.ID, []store.Song{
		{Source: "local", RemoteID: "m:oops", Page: 1, Title: "混进来的本地曲目"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildSnapshot(SnapshotInput{Store: st}); err == nil {
		t.Fatal("含本地曲目的同步歌单应直接报错，而不是静默过滤")
	}
}

func TestImportKeepLocalFalsePreservesLocalOnlyData(t *testing.T) {
	src := openTestStore(t)
	_, folderID, localOnlyID, _ := seedLocalLibrary(t, src)
	dir := t.TempDir()
	snap, err := BuildSnapshot(SnapshotInput{Store: src, Now: time.Unix(1761980000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "repo")
	if err := WriteSnapshot(repo, snap); err != nil {
		t.Fatal(err)
	}

	// 目标库先有本机数据：文件夹歌单、仅本机歌单、本地注释、本机绑定。
	dst := openTestStore(t)
	_, dstFolder, dstLocalOnly, dstLocalSong := seedLocalLibrary(t, dst)
	res, err := ImportSnapshot(ImportInput{
		Store: dst, Dir: repo, ArchiveDir: filepath.Join(dir, "sync"), KeepLocal: false,
		Now: time.Unix(1761980000, 0),
	})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	for _, id := range []string{dstFolder, dstLocalOnly} {
		if _, err := dst.Playlist(id); err != nil {
			t.Fatalf("KeepLocal=false 不应删除本机歌单 %s: %v (warnings=%v)", id, err, res.Warnings)
		}
	}
	if _, err := dst.Song(dstLocalSong); err != nil {
		t.Fatalf("本地曲目行不应被删除: %v", err)
	}
	if a, err := dst.Clip(dstLocalSong); err != nil || a.Active() {
		_ = a
	}
	// 本地曲目注释仍在。
	local, _ := dst.ListAnnotations()
	foundLocalAnnotation := false
	for _, a := range local {
		if a.SongID == dstLocalSong && a.LyricOffsetMS == 300 {
			foundLocalAnnotation = true
		}
	}
	if !foundLocalAnnotation {
		t.Fatalf("本地注释不应被导入删除: %+v", local)
	}
	// 本机绑定仍在，portable 绑定来自仓库。
	if b, ok := dst.LocalLyricBinding("bilibili:BV1:1"); !ok || b.Path != "/tmp/secret.lrc" {
		t.Fatalf("本机绑定不应被导入删除: %+v ok=%v", b, ok)
	}
	if b, ok := dst.PortableLyricBinding("bilibili:BV1:1"); !ok || b.ExternalID != "42" {
		t.Fatalf("portable 绑定应随导入恢复: %+v ok=%v", b, ok)
	}
	_ = folderID
	_ = localOnlyID
}

func TestImportSkipsFolderPlaylistFile(t *testing.T) {
	dst := openTestStore(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, PlaylistsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	// 仓库里不该出现 folder；即使出现也只跳过，不建没有 root_path 的占位。
	file := PlaylistFile{ID: "folder-1", Kind: "folder", Title: "非法文件夹", CreatedAt: 1}
	data, err := encodeJSON(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, PlaylistsDir, "folder-1.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := ImportSnapshot(ImportInput{Store: dst, Dir: repo, KeepLocal: true})
	if err != nil {
		t.Fatalf("导入不应因单个坏文件失败: %v", err)
	}
	if _, err := dst.Playlist("folder-1"); err == nil {
		t.Fatal("不应创建文件夹歌单占位")
	}
	foundWarning := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "folder-1") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("应给出跳过警告: %+v", res.Warnings)
	}
}

func TestAnnotationWithoutBindingStillImports(t *testing.T) {
	dst := openTestStore(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, AnnotationsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	// 旧版本写出的注释没有 lyric_bind 字段。
	old := `{"song_id":"bilibili:BV1:1","lyric_offset_ms":-500}`
	path := filepath.Join(repo, AnnotationsDir, "bilibili%3ABV1%3A1.json")
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportSnapshot(ImportInput{Store: dst, Dir: repo, KeepLocal: true}); err != nil {
		t.Fatalf("导入旧注释失败: %v", err)
	}
	annotations, _ := dst.ListAnnotations()
	if len(annotations) != 1 || annotations[0].LyricOffsetMS != -500 || annotations[0].LyricBind != nil {
		t.Fatalf("旧注释应按无绑定导入: %+v", annotations)
	}
}

func TestUpgradeMetaWritesV2Once(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	meta, _, err := NewMeta(testPassphrase(), time.Unix(1761980000, 0))
	if err != nil {
		t.Fatal(err)
	}
	meta.V = 1
	data, err := meta.Encode()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, MetaFile)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewClient(Options{Dir: dir})
	upgraded, err := c.upgradeMeta()
	if err != nil || !upgraded {
		t.Fatalf("v1 仓库应升级: upgraded=%v err=%v", upgraded, err)
	}
	after, err := LoadMetaFile(repo)
	if err != nil {
		t.Fatal(err)
	}
	if after.V != MetaVersion || MetaVersion != 2 {
		t.Fatalf("升级后应为 v2: %d", after.V)
	}
	// 密钥参数不变，升级只是格式变更。
	if after.Keycheck != meta.Keycheck || after.Salt != meta.Salt || after.KeyVersion != meta.KeyVersion {
		t.Fatal("升级不应改变密钥参数")
	}
	// 重复调用是空操作。
	again, err := c.upgradeMeta()
	if err != nil || again {
		t.Fatalf("已是 v2 时不应再次升级: %v %v", again, err)
	}
}

func TestAnnotationFileBindingFieldEncoding(t *testing.T) {
	binding := &store.LyricBinding{
		Kind: "lrclib", LRCLIBID: 12, Title: "晴天", Artist: "周杰伦",
		Duration: 200, OffsetMS: 300, BoundAt: 1761980000,
	}
	file := annotationFileFrom(store.SongAnnotation{SongID: "bilibili:BV1:1", LyricBind: binding})
	data, err := encodeJSON(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"lyric_bind"`)) {
		t.Fatalf("注释文件应包含 lyric_bind: %s", data)
	}
	// 本机绑定不能出现在注释文件里。
	local := annotationFileFrom(store.SongAnnotation{
		SongID:    "bilibili:BV1:1",
		LyricBind: &store.LyricBinding{Kind: store.BindingKindLocalFile, Path: "/x.lrc"},
	})
	if local.LyricBind != nil {
		t.Fatal("本机绑定不应写进注释文件")
	}
}

// TestImportSkipsLocalSongAnnotation 仓库里出现本地曲目注释时只跳过并告警。
func TestImportSkipsLocalSongAnnotation(t *testing.T) {
	dst := openTestStore(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, AnnotationsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	id := "local:m:abc:1"
	path := filepath.Join(repo, AnnotationsDir, "local%3Am%3Aabc%3A1.json")
	if err := os.WriteFile(path, []byte(`{"song_id":"`+id+`","lyric_offset_ms":300}`), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := ImportSnapshot(ImportInput{Store: dst, Dir: repo, KeepLocal: true})
	if err != nil {
		t.Fatalf("导入不应失败: %v", err)
	}
	if got := dst.LyricOffset(id, store.BindingAuto); got != 0 {
		t.Fatalf("本地曲目注释不应导入: %d", got)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "本地曲目注释") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应给出跳过警告: %+v", res.Warnings)
	}
}
