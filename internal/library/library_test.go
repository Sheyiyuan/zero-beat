package library

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// id3Frame 构造一个 ID3v2.3 帧（长度字段为大端，不是 syncsafe）。
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

func id3Text(id, value string) []byte {
	return id3Frame(id, append([]byte{3}, []byte(value)...))
}

func id3USLT(lyrics string) []byte {
	payload := append([]byte{3}, []byte("eng")...)
	payload = append(payload, 0) // 空 descriptor + 结束符
	payload = append(payload, []byte(lyrics)...)
	return id3Frame("USLT", payload)
}

// writeTaggedMP3 写一个只有 ID3v2 标签和填充字节的 MP3；
// 标签库只读标签头，不校验音频帧，足够覆盖元数据与内嵌歌词。
func writeTaggedMP3(t *testing.T, path, title, artist, album, lyrics string, track, disc int) {
	t.Helper()
	body := bytes.NewBuffer(nil)
	body.Write(id3Text("TIT2", title))
	body.Write(id3Text("TPE1", artist))
	body.Write(id3Text("TALB", album))
	body.Write(id3Text("TRCK", itoa(track)))
	body.Write(id3Text("TPOS", itoa(disc)))
	if lyrics != "" {
		body.Write(id3USLT(lyrics))
	}
	n := body.Len()
	size := []byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	out := append([]byte("ID3\x03\x00\x00"), size...)
	out = append(out, body.Bytes()...)
	out = append(out, make([]byte, 1024)...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
}

func itoa(v int) string {
	if v == 0 {
		return ""
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	return digits
}

func TestScanReadsTagsAndFallsBackToFilename(t *testing.T) {
	dir := t.TempDir()
	writeTaggedMP3(t, filepath.Join(dir, "01 - 晴天.MP3"), "晴 天", "周杰伦", "叶惠美", "", 3, 1)
	// 没有标签的文件：标题回退文件名 stem。
	if err := os.WriteFile(filepath.Join(dir, "未知曲目.flac"), []byte("not really flac"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Scan(dir)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if len(res.Files) != 2 {
		t.Fatalf("应扫描到两个文件: %+v", res.Files)
	}
	var tagged, plain File
	for _, f := range res.Files {
		switch filepath.Base(f.Path) {
		case "01 - 晴天.MP3":
			tagged = f
		case "未知曲目.flac":
			plain = f
		}
	}
	if tagged.Title != "晴 天" || tagged.Artist != "周杰伦" || tagged.Album != "叶惠美" {
		t.Fatalf("标签未读入: %+v", tagged)
	}
	if tagged.Track != 3 || tagged.Disc != 1 {
		t.Fatalf("碟号/轨号未读入: %+v", tagged)
	}
	if CodecOf(tagged.Path) != "mp3" {
		t.Fatalf("codec 应由扩展名推导: %q", CodecOf(tagged.Path))
	}
	if plain.Title != "未知曲目" {
		t.Fatalf("无标签时应回退文件名: %+v", plain)
	}
	if len(plain.Warnings) == 0 {
		t.Fatalf("标签读取失败应留下警告: %+v", plain)
	}
	if tagged.MetaKey == "" || plain.MetaKey == "" {
		t.Fatal("身份键不能为空")
	}
}

func TestScanSkipsNonAudioAndCountsSubdirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.mp3"), []byte("x"))
	writeFile(t, filepath.Join(dir, "b.opus"), []byte("x"))
	writeFile(t, filepath.Join(dir, "cover.jpg"), []byte("x"))
	writeFile(t, filepath.Join(dir, ".hidden.mp3"), []byte("x"))
	writeFile(t, filepath.Join(dir, "empty.mp3"), nil)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "Lyrics"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "a.mp3"), filepath.Join(dir, "link.mp3")); err != nil {
		t.Skip("当前文件系统不支持符号链接")
	}

	res, err := Scan(dir)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	names := make([]string, 0, len(res.Files))
	for _, f := range res.Files {
		names = append(names, filepath.Base(f.Path))
	}
	want := map[string]bool{"a.mp3": true, "b.opus": true}
	for _, name := range names {
		if !want[name] {
			t.Fatalf("不应收录 %q（全部：%v）", name, names)
		}
	}
	if len(names) != 2 {
		t.Fatalf("应只收录两个扩展名白名单文件: %v", names)
	}
	if res.Subdirs != 1 {
		t.Fatalf("lyrics/ 不应计入未扫描子目录，实际 %d", res.Subdirs)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w.Path, "empty.mp3") {
			t.Fatalf("空文件应静默跳过，不应产生警告: %+v", w)
		}
		if strings.Contains(w.Path, "link.mp3") {
			t.Fatalf("符号链接应静默跳过，不应产生警告: %+v", w)
		}
	}
}

func TestScanMissingDir(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("目录不存在时应返回错误，交给上层标记缺失")
	}
}

func TestMetaKeyStableAcrossFormatting(t *testing.T) {
	base := File{Title: "晴 天", Artist: "周杰伦", Album: "叶惠美", Track: 3, Disc: 1}
	same := File{Title: " 晴  天 ", Artist: "周杰伦", Album: "叶惠美", Track: 3, Disc: 1}
	if MetaKey(base) != MetaKey(same) {
		t.Fatal("空白差异不应改变身份键")
	}
	// 全角/半角、大小写由 NFKC + casefold 归一。
	a := File{Title: "ＡＢＣ", Artist: "X"}
	b := File{Title: "abc", Artist: "x"}
	if MetaKey(a) != MetaKey(b) {
		t.Fatal("大小写/全角差异不应改变身份键")
	}
	if MetaKey(base) == MetaKey(File{Title: "晴天", Artist: "周杰伦", Album: "叶惠美", Track: 3, Disc: 1}) {
		t.Fatal("不同标题应得到不同身份键")
	}
	// 时长不参与身份：同一录音转码后时长微差不应分家。
	withDuration := base
	withDuration.Duration = 300
	if MetaKey(base) != MetaKey(withDuration) {
		t.Fatal("时长不应参与身份")
	}
	// 标题为空时退回内容指纹。
	noTitle := File{ContentHash: "abc123"}
	if MetaKey(noTitle) != "file:abc123" {
		t.Fatalf("无标题应退回内容指纹: %q", MetaKey(noTitle))
	}
}

func TestQuickHashSmallAndLarge(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.bin")
	content := []byte("hello local library")
	writeFile(t, small, content)
	got, err := QuickHash(small, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 32 {
		t.Fatalf("指纹长度应为 32 位十六进制: %q", got)
	}
	again, _ := QuickHash(small, int64(len(content)))
	if got != again {
		t.Fatal("同一内容指纹应稳定")
	}

	large := filepath.Join(dir, "large.bin")
	big := make([]byte, 400<<10)
	for i := range big {
		big[i] = byte(i % 251)
	}
	writeFile(t, large, big)
	h1, err := QuickHash(large, int64(len(big)))
	if err != nil {
		t.Fatal(err)
	}
	// 改中间内容不影响大文件指纹（只读头尾）；改尾部会变。
	mid := append([]byte(nil), big...)
	mid[len(mid)/2] ^= 0xff
	writeFile(t, large, mid)
	h2, _ := QuickHash(large, int64(len(mid)))
	if h1 != h2 {
		t.Fatal("大文件指纹只应读头尾，中部变化不应影响")
	}
	tail := append([]byte(nil), big...)
	tail[len(tail)-1] ^= 0xff
	writeFile(t, large, tail)
	h3, _ := QuickHash(large, int64(len(tail)))
	if h1 == h3 {
		t.Fatal("尾部变化应改变指纹")
	}
}

func TestSortFilesUsesDiscTrackThenNaturalName(t *testing.T) {
	files := []File{
		{Path: "/x/track10.mp3", Track: 10, Disc: 1},
		{Path: "/x/track2.mp3", Track: 2, Disc: 1},
		{Path: "/x/track2-disc2.mp3", Track: 2, Disc: 2},
		{Path: "/x/no-track.mp3"},
	}
	SortFiles(files)
	want := []string{"no-track.mp3", "track2.mp3", "track10.mp3", "track2-disc2.mp3"}
	for i, name := range want {
		if got := filepath.Base(files[i].Path); got != name {
			t.Fatalf("排序第 %d 位应为 %s，实际 %s", i, name, got)
		}
	}
}

func TestProbeDurationContainers(t *testing.T) {
	dir := t.TempDir()
	// FLAC：STREAMINFO 里 44100 Hz、441000 采样 = 10 秒。
	flac := filepath.Join(dir, "a.flac")
	buf := bytes.NewBufferString("fLaC")
	buf.Write([]byte{0x80, 0x00, 0x00, 34}) // 最后一个元数据块、类型 0、长度 34
	streamInfo := make([]byte, 34)
	rate := uint32(44100)
	total := uint64(441000) // 44100 Hz 下正好 10 秒
	streamInfo[10] = byte(rate >> 12)
	streamInfo[11] = byte(rate >> 4)
	// 低 4 位是声道数-1（2 声道=1）与位深-1 的高位（16 bit => 15>>4=0）。
	streamInfo[12] = byte(rate&0x0f)<<4 | 0b0010
	streamInfo[13] = byte(total>>32) & 0x0f
	streamInfo[14] = byte(total >> 24 & 0xff)
	streamInfo[15] = byte(total >> 16 & 0xff)
	streamInfo[16] = byte(total >> 8 & 0xff)
	streamInfo[17] = byte(total & 0xff)
	buf.Write(streamInfo)
	writeFile(t, flac, buf.Bytes())
	if got := probeDuration(flac); got != 10 {
		t.Fatalf("FLAC 时长应为 10 秒，实际 %d", got)
	}

	// WAV：byteRate=176400，data=1764000 → 10 秒。
	wav := filepath.Join(dir, "a.wav")
	wb := bytes.NewBufferString("RIFF")
	wb.Write(make([]byte, 4))
	wb.WriteString("WAVE")
	wb.WriteString("fmt ")
	writeLE32(wb, 16)
	writeLE16(wb, 1)      // PCM
	writeLE16(wb, 2)      // channels
	writeLE32(wb, 44100)  // sample rate
	writeLE32(wb, 176400) // byte rate
	writeLE16(wb, 4)
	writeLE16(wb, 16)
	wb.WriteString("data")
	writeLE32(wb, 1764000)
	wb.Write(make([]byte, 64))
	writeFile(t, wav, wb.Bytes())
	if got := probeDuration(wav); got != 10 {
		t.Fatalf("WAV 时长应为 10 秒，实际 %d", got)
	}
}

func TestEmbeddedLyricsReadsUSLT(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tagged.mp3")
	writeTaggedMP3(t, path, "晴天", "周杰伦", "叶惠美", "[00:01.00]第一句\n[00:02.00]第二句", 3, 1)
	text, err := EmbeddedLyrics(path)
	if err != nil {
		t.Fatalf("读取内嵌歌词失败: %v", err)
	}
	if !strings.Contains(text, "第一句") || !strings.Contains(text, "第二句") {
		t.Fatalf("内嵌歌词内容不符: %q", text)
	}
	// 没有内嵌歌词的文件返回空串而不是错误。
	plain := filepath.Join(dir, "plain.mp3")
	writeFile(t, plain, []byte("nothing"))
	if text, err := EmbeddedLyrics(plain); err != nil || text != "" {
		t.Fatalf("无歌词应返回空串: %q err=%v", text, err)
	}
}

func TestIsAudioFileCaseInsensitive(t *testing.T) {
	for _, name := range []string{"a.MP3", "a.Flac", "a.m4a", "a.opus", "a.wv"} {
		if !IsAudioFile(name) {
			t.Fatalf("%s 应被识别为音频", name)
		}
	}
	for _, name := range []string{"a.txt", "a.lrc", "a.jpg", "a"} {
		if IsAudioFile(name) {
			t.Fatalf("%s 不应被识别为音频", name)
		}
	}
}

func TestNormalizeAndNaturalLess(t *testing.T) {
	if got := Normalize("  Ａ  b  "); got != "a b" {
		t.Fatalf("归一化不符: %q", got)
	}
	if !NaturalLess("track2.mp3", "track10.mp3") {
		t.Fatal("自然排序应把 2 排在 10 前面")
	}
	if NaturalLess("track10.mp3", "track2.mp3") {
		t.Fatal("自然排序方向错误")
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写入 %s 失败: %v", path, err)
	}
}

func writeLE32(buf *bytes.Buffer, v uint32) {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	buf.Write(b)
}

func writeLE16(buf *bytes.Buffer, v uint16) {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, v)
	buf.Write(b)
}
