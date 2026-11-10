package local

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// fixture 建一个只含一首本地曲目的库，返回音源与音频路径。
type fixture struct {
	store *store.Store
	src   *Source
	dir   string
	audio string
	song  store.Song
}

func newFixture(t *testing.T, cfg lyrics.LocalConfig, extension string) *fixture {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	playlist, err := st.CreateFolderPlaylist("", dir)
	if err != nil {
		t.Fatalf("创建文件夹歌单失败: %v", err)
	}
	audio := filepath.Join(dir, "01 - 晴天"+extension)
	if err := os.WriteFile(audio, []byte("fake audio bytes"), 0o644); err != nil {
		t.Fatalf("写入音频失败: %v", err)
	}
	song := store.NormalizeSong(store.Song{
		Source:   string(source.Local),
		RemoteID: "m:test",
		Page:     1,
		Title:    "晴天",
		Artist:   "周杰伦",
		Duration: 200,
	})
	if err := st.ReplaceLibraryItems(playlist.ID, []store.LibraryItem{{
		Song:        song,
		Path:        audio,
		Size:        16,
		ContentHash: "hash",
	}}); err != nil {
		t.Fatalf("写入文件索引失败: %v", err)
	}
	return &fixture{store: st, src: New(st, cfg), dir: dir, audio: audio, song: song}
}

func (f *fixture) ref() source.Ref {
	return source.Ref{Source: source.Local, ExternalID: f.song.ExternalID(), Page: 1}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLyricsSameStemBeatsEverything(t *testing.T) {
	f := newFixture(t, lyrics.DefaultLocalConfig(), ".mp3")
	write(t, filepath.Join(f.dir, "01 - 晴天.lrc"), "[00:01.00]同名歌词")
	write(t, filepath.Join(f.dir, "周杰伦 - 晴天.lrc"), "[00:01.00]艺术家歌词")
	write(t, filepath.Join(f.dir, "lyrics", "01 - 晴天.lrc"), "[00:01.00]目录歌词")

	lines, label, err := f.src.Lyrics(context.Background(), f.ref())
	if err != nil {
		t.Fatalf("取歌词失败: %v", err)
	}
	if label != "本地 LRC" {
		t.Fatalf("来源标签不符: %q", label)
	}
	if len(lines) != 1 || lines[0].Text != "同名歌词" {
		t.Fatalf("同名 stem 应优先: %+v", lines)
	}
}

func TestLyricsLyricsSubdirFallback(t *testing.T) {
	f := newFixture(t, lyrics.DefaultLocalConfig(), ".flac")
	write(t, filepath.Join(f.dir, "lyrics", "01 - 晴天.lrc"), "[00:01.00]目录歌词")

	lines, _, err := f.src.Lyrics(context.Background(), f.ref())
	if err != nil || len(lines) != 1 || lines[0].Text != "目录歌词" {
		t.Fatalf("lyrics/ 子目录应被定点查找: %+v err=%v", lines, err)
	}
}

func TestLyricsArtistTitleAndTitleArtist(t *testing.T) {
	cases := []struct {
		name string
		file string
		want string
	}{
		{"艺术家-歌名", "周杰伦 - 晴天.lrc", "艺术家在前"},
		{"歌名-艺术家", "晴天 - 周杰伦.lrc", "歌名在前"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, lyrics.DefaultLocalConfig(), ".mp3")
			// 让音频文件名不参与 stem 匹配，才能走到「艺术家-歌名」这一级。
			write(t, filepath.Join(f.dir, tc.file), "[00:02.00]"+tc.want)
			lines, _, err := f.src.Lyrics(context.Background(), f.ref())
			if err != nil || len(lines) != 1 || lines[0].Text != tc.want {
				t.Fatalf("应按文件名匹配: %+v err=%v", lines, err)
			}
		})
	}
}

func TestLyricsTagMatchAsLastResort(t *testing.T) {
	f := newFixture(t, lyrics.DefaultLocalConfig(), ".mp3")
	write(t, filepath.Join(f.dir, "随便一个名字.lrc"), "[ti:晴天]\n[ar:周杰伦]\n[00:03.00]标签匹配")

	lines, _, err := f.src.Lyrics(context.Background(), f.ref())
	if err != nil || len(lines) != 1 || lines[0].Text != "标签匹配" {
		t.Fatalf("[ti:]/[ar:] 应作为最后一级匹配: %+v err=%v", lines, err)
	}
}

func TestLyricsCandidateOrderFromConfig(t *testing.T) {
	// 两个候选都是文件名匹配；配置把「歌名-艺术家」排前面，应选它。
	cfg := lyrics.DefaultLocalConfig()
	cfg.CandidateOrder = []string{lyrics.MatchTitleArtist, lyrics.MatchArtistTitle}
	f := newFixture(t, cfg, ".mp3")
	write(t, filepath.Join(f.dir, "周杰伦 - 晴天.lrc"), "[00:02.00]艺术家候选")
	write(t, filepath.Join(f.dir, "晴天 - 周杰伦.lrc"), "[00:01.00]歌名候选")

	lines, _, err := f.src.Lyrics(context.Background(), f.ref())
	if err != nil || len(lines) != 1 || lines[0].Text != "歌名候选" {
		t.Fatalf("候选顺序配置未生效: %+v err=%v", lines, err)
	}
}

func TestLyricsNoCandidateReturnsEmpty(t *testing.T) {
	f := newFixture(t, lyrics.DefaultLocalConfig(), ".mp3")
	write(t, filepath.Join(f.dir, "完全无关.lrc"), "[00:01.00]不是这首歌")

	lines, label, err := f.src.Lyrics(context.Background(), f.ref())
	if err != nil {
		t.Fatalf("没有候选不应报错: %v", err)
	}
	if len(lines) != 0 || label != "" {
		t.Fatalf("不应误用无关歌词: %+v label=%q", lines, label)
	}
}

func TestLyricsTranslationSidecar(t *testing.T) {
	f := newFixture(t, lyrics.DefaultLocalConfig(), ".mp3")
	write(t, filepath.Join(f.dir, "01 - 晴天.lrc"), "[00:01.00]第一句\n[00:02.00]第二句")
	write(t, filepath.Join(f.dir, "01 - 晴天.trans.lrc"), "[00:01.00]First\n[00:02.00]Second")

	lines, _, err := f.src.Lyrics(context.Background(), f.ref())
	if err != nil || len(lines) != 2 {
		t.Fatalf("取歌词失败: %+v err=%v", lines, err)
	}
	if lines[0].Translation != "First" || lines[1].Translation != "Second" {
		t.Fatalf("独立翻译文件未合并: %+v", lines)
	}
}

func TestLyricsSameTimestampBilingualMerge(t *testing.T) {
	f := newFixture(t, lyrics.DefaultLocalConfig(), ".mp3")
	write(t, filepath.Join(f.dir, "01 - 晴天.lrc"), "[00:01.00]第一句\n[00:01.00]First line\n[00:02.00]第二句")

	lines, _, err := f.src.Lyrics(context.Background(), f.ref())
	if err != nil || len(lines) != 2 {
		t.Fatalf("双语行应合并成两行: %+v err=%v", lines, err)
	}
	if lines[0].Text != "第一句" || lines[0].Translation != "First line" {
		t.Fatalf("同时间戳双语行未合并: %+v", lines)
	}
}

func TestLyricsTranslationDisabled(t *testing.T) {
	cfg := lyrics.DefaultLocalConfig()
	cfg.Translation = lyrics.TranslationSameTimestamp
	f := newFixture(t, cfg, ".mp3")
	write(t, filepath.Join(f.dir, "01 - 晴天.lrc"), "[00:01.00]第一句")
	write(t, filepath.Join(f.dir, "01 - 晴天.trans.lrc"), "[00:01.00]First")

	lines, _, err := f.src.Lyrics(context.Background(), f.ref())
	if err != nil || len(lines) != 1 {
		t.Fatalf("取歌词失败: %+v err=%v", lines, err)
	}
	if lines[0].Translation != "" {
		t.Fatalf("配置为 same-timestamp 时不应合并 sidecar: %+v", lines)
	}
}

func TestLyricsEmbeddedWhenNoFile(t *testing.T) {
	f := newFixture(t, lyrics.DefaultLocalConfig(), ".mp3")
	// 用带 USLT 的 ID3v2 覆盖音频文件：没有旁挂 .lrc 时应读内嵌歌词。
	writeTaggedMP3(t, f.audio, "[00:01.00]内嵌第一句\n[00:02.00]内嵌第二句")

	lines, label, err := f.src.Lyrics(context.Background(), f.ref())
	if err != nil {
		t.Fatalf("取内嵌歌词失败: %v", err)
	}
	if label != "内嵌歌词" {
		t.Fatalf("来源标签应为内嵌歌词: %q", label)
	}
	if len(lines) != 2 || lines[0].Text != "内嵌第一句" {
		t.Fatalf("内嵌歌词内容不符: %+v", lines)
	}
}

// TestLoadLyricsFileReusesTranslation 验证绑定走同一套解析与翻译规则。
func TestLoadLyricsFileReusesTranslation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.lrc")
	write(t, path, "[00:01.00]第一句\n[00:02.00]第二句")
	write(t, filepath.Join(dir, "x.zh.lrc"), "[00:02.00]Translation")

	lines, err := LoadLyricsFile(path, lyrics.DefaultLocalConfig())
	if err != nil || len(lines) != 2 {
		t.Fatalf("读取失败: %+v err=%v", lines, err)
	}
	if lines[1].Translation != "Translation" {
		t.Fatalf(".zh.lrc 未合并: %+v", lines)
	}
	if _, err := LoadLyricsFile(filepath.Join(dir, "missing.lrc"), lyrics.DefaultLocalConfig()); err == nil {
		t.Fatal("缺失文件应返回错误")
	}
}

func TestResolveRequiresExistingFile(t *testing.T) {
	f := newFixture(t, lyrics.DefaultLocalConfig(), ".mp3")
	resolved, err := f.src.Resolve(context.Background(), f.ref(), source.QualityAuto)
	if err != nil {
		t.Fatalf("解析本地文件失败: %v", err)
	}
	if resolved.Stream.URL != f.audio || resolved.Stream.Codec != "mp3" {
		t.Fatalf("取流结果不符: %+v", resolved.Stream)
	}
	if resolved.Stream.Duration != 200*time.Second {
		t.Fatalf("时长应直接取歌曲行: %v", resolved.Stream.Duration)
	}
	if resolved.Stream.Quality != "本地文件" || resolved.Stream.ExpiresAt != (time.Time{}) {
		t.Fatalf("本地流不应有过期时间: %+v", resolved.Stream)
	}
	if len(resolved.Stream.Headers) != 0 {
		t.Fatalf("本地流不需要请求头: %+v", resolved.Stream.Headers)
	}
	if err := os.Remove(f.audio); err != nil {
		t.Fatal(err)
	}
	if _, err := f.src.Resolve(context.Background(), f.ref(), source.QualityAuto); err == nil {
		t.Fatal("文件不存在时应报错")
	}
}

func TestDirCacheRefreshesOnMtimeChange(t *testing.T) {
	f := newFixture(t, lyrics.DefaultLocalConfig(), ".mp3")
	write(t, filepath.Join(f.dir, "01 - 晴天.lrc"), "[00:01.00]第一版")
	if lines, _, _ := f.src.Lyrics(context.Background(), f.ref()); len(lines) != 1 || lines[0].Text != "第一版" {
		t.Fatalf("首次读取失败: %+v", lines)
	}
	// 覆盖文件并推进目录 mtime；缓存应因 mtime 变化而失效。
	write(t, filepath.Join(f.dir, "01 - 晴天.lrc"), "[00:01.00]第二版，长度不同")
	dir := filepath.Dir(f.audio)
	now := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(dir, now, now); err != nil {
		t.Skip("当前文件系统不支持修改目录时间")
	}
	lines, _, _ := f.src.Lyrics(context.Background(), f.ref())
	if len(lines) != 1 || !strings.Contains(lines[0].Text, "第二版") {
		t.Fatalf("目录变化后缓存未刷新: %+v", lines)
	}
}

func TestSongPathFallsBackToIndexWhenFileMissing(t *testing.T) {
	f := newFixture(t, lyrics.DefaultLocalConfig(), ".mp3")
	write(t, filepath.Join(f.dir, "01 - 晴天.lrc"), "[00:01.00]文件不在也能找到歌词")
	if err := os.Remove(f.audio); err != nil {
		t.Fatal(err)
	}
	lines, _, err := f.src.Lyrics(context.Background(), f.ref())
	if err != nil {
		t.Fatalf("文件缺失时仍应按索引找旁挂歌词: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("应找到旁挂歌词: %+v", lines)
	}
}

// ---- ID3v2 测试夹具 ----

func id3Frame(id string, payload []byte) []byte {
	buf := bytes.NewBuffer(nil)
	buf.WriteString(id)
	size := make([]byte, 4)
	binary.BigEndian.PutUint32(size, uint32(len(payload)))
	buf.Write(size)
	buf.Write([]byte{0, 0})
	buf.Write(payload)
	return buf.Bytes()
}

func writeTaggedMP3(t *testing.T, path, lyrics string) {
	t.Helper()
	payload := append([]byte{3}, []byte("eng")...)
	payload = append(payload, 0)
	payload = append(payload, []byte(lyrics)...)
	body := bytes.NewBuffer(nil)
	body.Write(id3Frame("TIT2", append([]byte{3}, []byte("晴天")...)))
	body.Write(id3Frame("USLT", payload))
	n := body.Len()
	size := []byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	out := append([]byte("ID3\x03\x00\x00"), size...)
	out = append(out, body.Bytes()...)
	out = append(out, make([]byte, 512)...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("写入内嵌歌词夹具失败: %v", err)
	}
}
