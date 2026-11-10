// Package library 扫描本地音乐目录，把音频文件归一成可入库的曲目。
//
// 这里的产出是「一次扫描的结果」，不碰数据库：身份查找（路径 → 内容指纹 →
// 歌曲表）由 app 层按顺序决定，扫描只负责读文件、读标签、算身份键。
package library

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/dhowden/tag"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// LyricsDirName 是本地歌词的定点目录，不算「未扫描的子文件夹」。
const LyricsDirName = "lyrics"

// quickHashLimit 是内容指纹读取上限：超过这个大小只读头尾各 64 KiB。
// 本地库扫描不该把大文件整个读一遍。
const quickHashLimit = 128 << 10

// audioExtensions 是本地库接受的音频扩展名（小写、带点）。
var audioExtensions = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".aac": true,
	".ogg": true, ".opus": true, ".wav": true, ".ape": true,
	".wv": true, ".aiff": true, ".aif": true, ".wma": true,
}

// IsAudioFile 判断文件名扩展名是否在白名单内（大小写不敏感）。
func IsAudioFile(name string) bool {
	return audioExtensions[strings.ToLower(filepath.Ext(name))]
}

// CodecOf 返回播放流里用的编码名（扩展名去掉点）。
func CodecOf(path string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
}

// File 是一个扫描出来的音频文件。
//
// Path 是绝对路径；MetaKey 由音频元数据生成，是曲目身份；ContentHash 是
// 文件内容指纹，用于「文件改名/移动但内容没变」时复用身份。
type File struct {
	Path        string
	Size        int64
	ModTime     int64
	ContentHash string
	MetaKey     string
	Title       string
	Artist      string
	Album       string
	Disc        int
	Track       int
	Duration    int // 秒；0 表示未知，播放时再探测
	Warnings    []string
}

// Warning 是单个文件的问题；扫描不因它中断。
type Warning struct {
	Path   string
	Reason string
	// Skip 表示这个文件被静默跳过；界面不必提示它。
	Skip bool
}

// Result 是一次目录扫描的结果。
type Result struct {
	Dir      string
	Files    []File
	Subdirs  int
	Warnings []Warning
}

// Scan 扫描目录当前层的音频文件。
//
// 只读当前层、不递归：子目录数量单独返回给界面提示。目录本身不可读时返回
// error，调用方据此把歌单标记为缺失，而不是清空已有条目。
func Scan(dir string) (Result, error) {
	dir = filepath.Clean(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Result{Dir: dir}, fmt.Errorf("library: 读取目录: %w", err)
	}
	out := Result{Dir: dir}
	for _, entry := range entries {
		name := entry.Name()
		full := filepath.Join(dir, name)
		if entry.IsDir() {
			// lyrics/ 是歌词定点目录，不算「发现但没扫描」的子文件夹。
			if !strings.EqualFold(name, LyricsDirName) {
				out.Subdirs++
			}
			continue
		}
		if strings.HasPrefix(name, ".") {
			continue
		}
		// 符号链接一律跳过：链接可能指回父目录或仓库外，语义不值得 v1 承担。
		// 跳过本身不是问题，不为它刷警告。
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if !entry.Type().IsRegular() || !IsAudioFile(name) {
			continue
		}
		file, warn := readFile(full, entry)
		if warn != nil {
			if !warn.Skip {
				out.Warnings = append(out.Warnings, *warn)
			}
			continue
		}
		// 标签读不出来不影响入库（标题回退文件名），但要让用户看到原因。
		for _, reason := range file.Warnings {
			out.Warnings = append(out.Warnings, Warning{Path: full, Reason: reason})
		}
		out.Files = append(out.Files, file)
	}
	SortFiles(out.Files)
	return out, nil
}

// readFile 读取一个文件的标签、时长与指纹。返回的 warning 表示这个文件被跳过。
func readFile(path string, entry os.DirEntry) (File, *Warning) {
	info, err := entry.Info()
	if err != nil {
		return File{}, &Warning{Path: path, Reason: "读取文件信息失败"}
	}
	if info.Size() <= 0 {
		// 0 字节文件按「不是音频」静默跳过，提示它只会制造噪音。
		return File{}, &Warning{Path: path, Reason: "空文件已跳过", Skip: true}
	}
	file := File{
		Path:    path,
		Size:    info.Size(),
		ModTime: info.ModTime().Unix(),
		Title:   strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
	}
	if applied, err := readMeta(&file); err != nil {
		// 标签坏了不影响播放：保留文件名兜底，只记一条警告。
		file.Warnings = append(file.Warnings, applied+"标签读取失败："+err.Error())
	}
	hash, err := QuickHash(path, info.Size())
	if err != nil {
		return File{}, &Warning{Path: path, Reason: "读取文件失败：" + err.Error()}
	}
	file.ContentHash = hash
	file.MetaKey = MetaKey(file)
	return file, nil
}

// SortFiles 按 disc → track → 文件名自然排序；缺失字段按 0 处理。
func SortFiles(files []File) {
	sort.SliceStable(files, func(i, j int) bool {
		a, b := files[i], files[j]
		if a.Disc != b.Disc {
			return a.Disc < b.Disc
		}
		if a.Track != b.Track {
			return a.Track < b.Track
		}
		return NaturalLess(filepath.Base(a.Path), filepath.Base(b.Path))
	})
}

// NaturalLess 按自然顺序比较文件名：track2 排在 track10 前面。
func NaturalLess(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	for i, j := 0, 0; i < len(ra) && j < len(rb); {
		if unicode.IsDigit(ra[i]) && unicode.IsDigit(rb[j]) {
			si, sj := i, j
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			na, _ := strconv.Atoi(string(ra[si:i]))
			nb, _ := strconv.Atoi(string(rb[sj:j]))
			if na != nb {
				return na < nb
			}
			continue
		}
		la, lb := unicode.ToLower(ra[i]), unicode.ToLower(rb[j])
		if la != lb {
			return la < lb
		}
		i++
		j++
	}
	return len(ra) < len(rb)
}

// Normalize 归一化用于身份比较的文本：NFKC → 去首尾空白 → 大小写折叠 → 折叠空白。
func Normalize(s string) string {
	s = norm.NFKC.String(strings.TrimSpace(s))
	s = cases.Fold().String(s)
	return strings.Join(strings.Fields(s), " ")
}

// MetaKey 由音频元数据生成曲目身份键。
//
// 身份刻意不含 duration：时长经常缺失或取整，包含它会让同一文件的转码副本
// 分成两条曲目。代价是标题/艺术家/专辑/碟号/轨号完全相同的不同录音会被合并，
// 这是「从元数据生成身份」的已知取舍。
func MetaKey(f File) string {
	title := Normalize(f.Title)
	if title == "" {
		return "file:" + f.ContentHash
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		Normalize(f.Artist), title, Normalize(f.Album),
		strconv.Itoa(f.Disc), strconv.Itoa(f.Track),
	}, "\n")))
	return "m:" + hex.EncodeToString(sum[:])[:32]
}

// QuickHash 计算内容指纹：小文件取全文，大文件取「大小 + 头 64 KiB + 尾 64 KiB」。
func QuickHash(path string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if size <= quickHashLimit {
		buf := make([]byte, size)
		if _, err := readFull(f, buf); err != nil {
			return "", err
		}
		h.Write(buf)
		return hex.EncodeToString(h.Sum(nil))[:32], nil
	}
	const half = 64 << 10
	head := make([]byte, half)
	if _, err := readFull(f, head); err != nil {
		return "", err
	}
	tail := make([]byte, half)
	if _, err := f.ReadAt(tail, size-half); err != nil {
		return "", err
	}
	fmt.Fprintf(h, "%d\n", size)
	h.Write(head)
	h.Write(tail)
	return hex.EncodeToString(h.Sum(nil))[:32], nil
}

// readFull 读满整个缓冲区；短读按错误处理，避免指纹静默变短。
func readFull(f *os.File, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := f.Read(buf[total:])
		total += n
		if err != nil {
			if total == len(buf) {
				return total, nil
			}
			return total, err
		}
	}
	return total, nil
}

// readMeta 用标签补全标题/艺术家/专辑/碟号/轨号与时长，并把内嵌歌词摘出来
// 供本地歌词链使用。返回的字符串描述标签来源，只用于错误信息。
func readMeta(file *File) (string, error) {
	f, err := os.Open(file.Path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	meta, err := tag.ReadFrom(f)
	if err != nil {
		file.Duration = probeDuration(file.Path)
		return "", err
	}
	if v := strings.TrimSpace(meta.Title()); v != "" {
		file.Title = v
	}
	file.Artist = strings.TrimSpace(meta.Artist())
	file.Album = strings.TrimSpace(meta.Album())
	file.Disc, _ = meta.Disc()
	file.Track, _ = meta.Track()
	file.Duration = probeDuration(file.Path)
	if file.Duration == 0 {
		file.Duration = durationFromMeta(meta)
	}
	return string(meta.Format()), nil
}

// durationFromMeta 从标签的原始字段里找时长（部分格式会写成秒数）。
func durationFromMeta(meta tag.Metadata) int {
	raw := meta.Raw()
	for _, key := range []string{"length", "LENGTH", "duration", "DURATION"} {
		if v, ok := raw[key].(string); ok {
			// LRC 风格的 [length:03:21] 不在标签里，这里只接受纯秒数。
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

// EmbeddedLyrics 返回音频文件里内嵌的歌词文本（可能为空）。
// 只读标签，不做时间轴校验：是否有时间轴由歌词层判断。
func EmbeddedLyrics(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	meta, err := tag.ReadFrom(f)
	if err != nil {
		// 没有标签或标签损坏时当作「没有内嵌歌词」，不阻断歌词链的下一级。
		return "", nil
	}
	if lyrics := strings.TrimSpace(meta.Lyrics()); lyrics != "" {
		return lyrics, nil
	}
	// 有些 FLAC/OGG 只写了 UNSYNCEDLYRICS，库不认这个键，退回原始映射。
	raw := meta.Raw()
	for _, key := range []string{"unsyncedlyrics", "UNSYNCEDLYRICS", "Lyrics", "LYRICS"} {
		if v, ok := raw[key].(string); ok && strings.TrimSpace(v) != "" {
			return v, nil
		}
	}
	return "", nil
}
