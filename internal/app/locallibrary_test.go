package app

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

func writeAudio(t *testing.T, path string, tagged bool, title string) {
	t.Helper()
	var data []byte
	if tagged {
		data = buildTaggedMP3(title)
	} else {
		data = []byte("audio bytes " + title)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写入音频失败: %v", err)
	}
}

// buildTaggedMP3 生成只含 ID3v2 标签的 MP3 夹具（标签库只读标签头）。
func buildTaggedMP3(title string) []byte {
	frame := func(id string, payload []byte) []byte {
		buf := bytes.NewBuffer(nil)
		buf.WriteString(id)
		size := make([]byte, 4)
		binary.BigEndian.PutUint32(size, uint32(len(payload)))
		buf.Write(size)
		buf.Write([]byte{0, 0})
		buf.Write(payload)
		return buf.Bytes()
	}
	text := func(id, value string) []byte { return frame(id, append([]byte{3}, []byte(value)...)) }
	body := bytes.NewBuffer(nil)
	body.Write(text("TIT2", title))
	body.Write(text("TPE1", "周杰伦"))
	body.Write(text("TALB", "叶惠美"))
	body.Write(text("TRCK", "1"))
	body.Write(text("TPOS", "1"))
	n := body.Len()
	size := []byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	out := append([]byte("ID3\x03\x00\x00"), size...)
	out = append(out, body.Bytes()...)
	return append(out, make([]byte, 256)...)
}

func TestAddLocalFolderScansAndMarksSyncExcluded(t *testing.T) {
	st := openTestStore(t)
	dir := t.TempDir()
	writeAudio(t, filepath.Join(dir, "a.mp3"), true, "晴天")
	writeAudio(t, filepath.Join(dir, "b.flac"), false, "夜航星")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := New(nil, st, nil)
	playlist, scan, err := svc.AddLocalFolder(dir)
	if err != nil {
		t.Fatalf("添加文件夹失败: %v", err)
	}
	if playlist.Kind != store.PlaylistKindFolder || !playlist.SyncExcluded || playlist.RootPath != filepath.Clean(dir) {
		t.Fatalf("文件夹歌单字段不符: %+v", playlist)
	}
	if scan.Scanned != 2 || scan.Subdirs != 1 || scan.Added != 2 {
		t.Fatalf("扫描结果不符: %+v", scan)
	}
	songs, err := st.PlaylistSongs(playlist.ID)
	if err != nil || len(songs) != 2 {
		t.Fatalf("歌单条目不符: %+v err=%v", songs, err)
	}
	for _, song := range songs {
		if song.Source != "local" {
			t.Fatalf("应生成本地曲目: %+v", song)
		}
	}
}

func TestAddLocalFolderRejectsBadPaths(t *testing.T) {
	st := openTestStore(t)
	svc := New(nil, st, nil)
	if _, _, err := svc.AddLocalFolder(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("不存在的路径应报错")
	}
	file := filepath.Join(t.TempDir(), "file.mp3")
	writeAudio(t, file, false, "x")
	if _, _, err := svc.AddLocalFolder(file); err == nil {
		t.Fatal("文件不是文件夹，应报错")
	}
	if playlists, _ := st.ListPlaylists(); len(playlists) != 0 {
		t.Fatalf("失败时不应留下歌单: %+v", playlists)
	}
}

func TestRescanKeepsIdentityAcrossRenameAndRetag(t *testing.T) {
	st := openTestStore(t)
	dir := t.TempDir()
	original := filepath.Join(dir, "track.flac")
	writeAudio(t, original, false, "原始标题")
	svc := New(nil, st, nil)
	playlist, _, err := svc.AddLocalFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	songs, _ := st.PlaylistSongs(playlist.ID)
	if len(songs) != 1 {
		t.Fatalf("应有 1 首: %+v", songs)
	}
	firstID := songs[0].ID

	// 改名：路径变了，内容不变 → 复用身份。
	renamed := filepath.Join(dir, "renamed.flac")
	if err := os.Rename(original, renamed); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RescanLibrary(playlist.ID); err != nil {
		t.Fatal(err)
	}
	songs, _ = st.PlaylistSongs(playlist.ID)
	if len(songs) != 1 || songs[0].ID != firstID {
		t.Fatalf("改名后应复用身份: first=%s now=%+v", firstID, songs)
	}

	// 改标签：路径不变、内容变了 → 仍复用身份。
	writeAudio(t, renamed, true, "改过的标题")
	if _, err := svc.RescanLibrary(playlist.ID); err != nil {
		t.Fatal(err)
	}
	songs, _ = st.PlaylistSongs(playlist.ID)
	if len(songs) != 1 || songs[0].ID != firstID {
		t.Fatalf("改标签后应复用身份: first=%s now=%+v", firstID, songs)
	}
	// 无标签时标题来自文件名 stem，改标签不应覆盖已有信息。
	if songs[0].Title != "track" {
		t.Fatalf("refreshLocalSong 不应覆盖已有标题: %q", songs[0].Title)
	}

	// 两个都变（新文件名 + 新内容）→ 新曲目。
	writeAudio(t, filepath.Join(dir, "another.flac"), false, "完全不同")
	if _, err := svc.RescanLibrary(playlist.ID); err != nil {
		t.Fatal(err)
	}
	songs, _ = st.PlaylistSongs(playlist.ID)
	if len(songs) != 2 {
		t.Fatalf("应变成两首: %+v", songs)
	}
}

func TestRescanMissingFileKeepsSongAndAnnotation(t *testing.T) {
	st := openTestStore(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "gone.mp3")
	writeAudio(t, path, false, "会消失")
	svc := New(nil, st, nil)
	playlist, _, err := svc.AddLocalFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	songs, _ := st.PlaylistSongs(playlist.ID)
	songID := songs[0].ID
	if err := st.SetClip(songID, store.Clip{StartMS: 1000, EndMS: 2000}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	scan, err := svc.RescanLibrary(playlist.ID)
	if err != nil {
		t.Fatalf("文件消失不应算扫描失败: %v", err)
	}
	if scan.Missing != 1 {
		t.Fatalf("应统计到 1 个缺失文件: %+v", scan)
	}
	if items, _ := st.PlaylistSongs(playlist.ID); len(items) != 0 {
		t.Fatalf("缺失文件不应继续显示: %+v", items)
	}
	if _, err := st.Song(songID); err != nil {
		t.Fatalf("歌曲行与注释应保留: %v", err)
	}
	if clip, _ := st.Clip(songID); !clip.Active() {
		t.Fatal("注释不应被扫描删除")
	}
	files, _ := st.LibraryFiles(playlist.ID)
	if len(files) != 1 || files[0].Present {
		t.Fatalf("文件索引应标记为缺失: %+v", files)
	}
}

func TestRescanUnavailableRootKeepsItems(t *testing.T) {
	st := openTestStore(t)
	dir := t.TempDir()
	writeAudio(t, filepath.Join(dir, "a.mp3"), false, "在")
	svc := New(nil, st, nil)
	playlist, _, err := svc.AddLocalFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RescanLibrary(playlist.ID); err == nil {
		t.Fatal("目录不可用时应报错")
	}
	updated, _ := st.Playlist(playlist.ID)
	if updated.ScanStatus != "missing" {
		t.Fatalf("扫描状态应为 missing: %+v", updated)
	}
	if items, _ := st.PlaylistSongs(playlist.ID); len(items) != 1 {
		t.Fatalf("目录不可用时不应清空条目: %+v", items)
	}
}

func TestRescanReusesPlatformSongForDownloadedFile(t *testing.T) {
	st := openTestStore(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "downloaded.flac")
	writeAudio(t, path, false, "已下载")
	platform := store.NormalizeSong(store.Song{Source: "bilibili", RemoteID: "BV1", Page: 1, CID: 99, Title: "平台里的歌", Artist: "UP"})
	if _, err := st.UpsertSong(platform); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertDownload(store.Download{SongID: platform.ID, Path: path, Size: 10, Codec: "flac"}); err != nil {
		t.Fatal(err)
	}
	svc := New(nil, st, nil)
	playlist, _, err := svc.AddLocalFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	songs, _ := st.PlaylistSongs(playlist.ID)
	if len(songs) != 1 || songs[0].ID != platform.ID || songs[0].Source != "bilibili" {
		t.Fatalf("下载目录里的文件应复用平台身份: %+v", songs)
	}
	// 删除下载记录后，下次扫描会退回本地身份（预期行为）。
	if err := st.DeleteDownload(platform.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RescanLibrary(playlist.ID); err != nil {
		t.Fatal(err)
	}
	songs, _ = st.PlaylistSongs(playlist.ID)
	if len(songs) != 1 || songs[0].Source != "local" {
		t.Fatalf("删除下载记录后应变成本地曲目: %+v", songs)
	}
}

func TestPlaylistSongsRescansStaleFolder(t *testing.T) {
	st := openTestStore(t)
	dir := t.TempDir()
	writeAudio(t, filepath.Join(dir, "a.mp3"), false, "一")
	svc := New(nil, st, nil)
	playlist, _, err := svc.AddLocalFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 加一首文件后，超过 60 秒阈值之外的目录不会自动重扫；
	// 这里直接强制刷新（等于按 r）。
	writeAudio(t, filepath.Join(dir, "b.mp3"), false, "二")
	items, warn, err := svc.PlaylistSongs(nil, store.Playlist{ID: playlist.ID, Kind: store.PlaylistKindFolder, RootPath: dir, ScannedAt: 0}, true)
	if err != nil {
		t.Fatalf("刷新失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("强制刷新后应有 2 首: %+v", items)
	}
	_ = warn
}

func TestSetLibraryRootMovesFolder(t *testing.T) {
	st := openTestStore(t)
	first := t.TempDir()
	writeAudio(t, filepath.Join(first, "a.mp3"), false, "旧目录")
	svc := New(nil, st, nil)
	playlist, _, err := svc.AddLocalFolder(first)
	if err != nil {
		t.Fatal(err)
	}
	second := t.TempDir()
	writeAudio(t, filepath.Join(second, "b.mp3"), false, "新目录")
	scan, err := svc.SetLibraryRoot(playlist.ID, second)
	if err != nil {
		t.Fatalf("更换目录失败: %v", err)
	}
	if scan.Playlist.RootPath != filepath.Clean(second) || scan.Scanned != 1 {
		t.Fatalf("更换后结果不符: %+v", scan)
	}
	items, _ := st.PlaylistSongs(playlist.ID)
	if len(items) != 1 || items[0].Title != "b" {
		t.Fatalf("歌单内容应换成新目录: %+v", items)
	}
}
