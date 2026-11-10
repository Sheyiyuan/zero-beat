package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/sheyiyuan/zero-beat/internal/library"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// dirCache 缓存目录列表，键是目录的 mtime：同一首歌反复进歌词页时不用重复 ReadDir。
type dirCache struct {
	mu      sync.Mutex
	entries map[string]dirListing
}

type dirListing struct {
	dir   string
	mtime int64
	files []string // 文件名（不含目录）
	dirs  map[string]string
}

func newDirCache() *dirCache { return &dirCache{entries: map[string]dirListing{}} }

func (c *dirCache) list(dir string) (dirListing, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return dirListing{}, err
	}
	mtime := info.ModTime().UnixNano()
	c.mu.Lock()
	if cached, ok := c.entries[dir]; ok && cached.mtime == mtime {
		c.mu.Unlock()
		return cached, nil
	}
	c.mu.Unlock()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return dirListing{}, err
	}
	listing := dirListing{dir: dir, mtime: mtime, dirs: map[string]string{}}
	for _, entry := range entries {
		if entry.IsDir() {
			listing.dirs[strings.ToLower(entry.Name())] = filepath.Join(dir, entry.Name())
			continue
		}
		listing.files = append(listing.files, entry.Name())
	}
	c.mu.Lock()
	c.entries[dir] = listing
	c.mu.Unlock()
	return listing, nil
}

// Lyrics 实现 source.Source：按 4.1 的本地链取歌词。
//
// 只负责本地旁挂与内嵌歌词；LRCLIB 兜底由 app.Lyrics 在拿到空结果后处理，
// 这样平台歌曲和本地歌曲共用同一条兜底路径。
func (s *Source) Lyrics(ctx context.Context, ref source.Ref) ([]lyrics.Line, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	song, err := s.song(ref)
	if err != nil {
		return nil, "", err
	}
	path, perr := s.songPath(song)
	if perr != nil {
		// 文件不在时仍然可以按索引里的路径找旁挂歌词：目录可能还在。
		path = s.anyIndexedPath(song)
	}
	if path == "" {
		return nil, "", perr
	}
	lines, ferr := s.localLRC(path, song)
	if len(lines) > 0 {
		return lines, "本地 LRC", nil
	}
	// 旁挂文件存在但坏了时不直接放弃：内嵌歌词可能是好的。
	if embedded, err := library.EmbeddedLyrics(path); err == nil && strings.TrimSpace(embedded) != "" {
		if parsed := s.parseText(embedded); len(parsed) > 0 {
			return parsed, "内嵌歌词", nil
		}
	}
	if ferr != nil {
		return nil, "", ferr
	}
	return nil, "", nil
}

// anyIndexedPath 返回索引里第一个路径，不要求文件现在存在。
func (s *Source) anyIndexedPath(song store.Song) string {
	if s.store == nil {
		return ""
	}
	files, err := s.store.LibraryFilesBySongAll(song.ID)
	if err != nil {
		return ""
	}
	for _, f := range files {
		if f.Path != "" {
			return f.Path
		}
	}
	return ""
}

// candidate 是一个候选取歌词文件。
type candidate struct {
	path string
	kind string // stem / artist-title / title-artist / title / tag
	// dirRank 0 表示音频同目录，1 表示 lyrics/ 子目录。
	dirRank int
	name    string
}

// localLRC 在音频同目录与 lyrics/ 子目录里找最匹配的 .lrc。
func (s *Source) localLRC(audioPath string, song store.Song) ([]lyrics.Line, error) {
	dir := filepath.Dir(audioPath)
	stem := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	ext := strings.ToLower(filepath.Ext(audioPath))

	main, err := s.cache.list(dir)
	if err != nil {
		return nil, fmt.Errorf("本地歌词目录不可读：%w", err)
	}
	type searchDir struct {
		listing dirListing
		rank    int
	}
	dirs := []searchDir{{listing: main, rank: 0}}
	if lyricsDir, ok := main.dirs[strings.ToLower(library.LyricsDirName)]; ok {
		if sub, lerr := s.cache.list(lyricsDir); lerr == nil {
			dirs = append(dirs, searchDir{listing: sub, rank: 1})
		}
	}

	var candidates []candidate
	for _, search := range dirs {
		for _, name := range search.listing.files {
			if !strings.HasSuffix(strings.ToLower(name), ".lrc") || isSidecar(name) {
				continue
			}
			candidates = append(candidates, candidate{
				path:    filepath.Join(search.listing.dir, name),
				kind:    s.matchKind(name, stem, ext, song),
				dirRank: search.rank,
				name:    name,
			})
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	// 名字匹配不上的候选再试 [ti:]/[ar:]；限制数量，避免每次进歌词页都读大量文件。
	s.fillTagMatches(candidates, song)
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if ra, rb := s.cfg.Rank(a.kind), s.cfg.Rank(b.kind); ra != rb {
			return ra < rb
		}
		if a.dirRank != b.dirRank {
			return a.dirRank < b.dirRank
		}
		return library.NaturalLess(a.name, b.name)
	})
	var firstErr error
	for _, cand := range candidates {
		if cand.kind == "" {
			continue
		}
		lines, err := lyrics.LoadLocalFile(cand.path, s.cfg.MaxBytes())
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		return s.withTranslation(lines, cand.path), nil
	}
	return nil, firstErr
}

// matchKind 判断候选文件名属于哪种匹配：先同名，再 艺术家-歌名/歌名-艺术家，最后歌名。
// 一个都对不上返回空串，后续交给 [ti:]/[ar:] 打分。
func (s *Source) matchKind(name, stem, ext string, song store.Song) string {
	baseKey := matchKey(stripTrackNumber(strings.TrimSuffix(name, filepath.Ext(name))))
	stemKey := matchKey(stripTrackNumber(stem))
	// 「song.flac.lrc」这种写法也要认：stem 带上原扩展名再比一次。
	stemExtKey := matchKey(stripTrackNumber(stem + ext))
	switch {
	case baseKey == stemKey || baseKey == stemExtKey:
		return lyrics.MatchStem
	}
	titleKey := matchKey(song.Title)
	artistKey := matchKey(song.Artist)
	if titleKey == "" {
		return ""
	}
	switch {
	case artistKey != "" && baseKey == artistKey+titleKey:
		return lyrics.MatchArtistTitle
	case artistKey != "" && baseKey == titleKey+artistKey:
		return lyrics.MatchTitleArtist
	case baseKey == titleKey:
		return lyrics.MatchTitle
	}
	return ""
}

// fillTagMatches 给还没有名字匹配的候选补上 tag 匹配。
func (s *Source) fillTagMatches(candidates []candidate, song store.Song) {
	if s.cfg.Rank(lyrics.MatchTag) > len(s.cfg.CandidateOrder) {
		return
	}
	titleKey := matchKey(song.Title)
	artistKey := matchKey(song.Artist)
	if titleKey == "" {
		return
	}
	budget := 20
	for i := range candidates {
		if candidates[i].kind != "" || budget <= 0 {
			continue
		}
		text, err := readText(candidates[i].path, s.cfg.MaxBytes())
		if err != nil {
			continue
		}
		budget--
		tagTitle, tagArtist := lyrics.ParseLocalLRCMeta(text)
		if matchKey(tagTitle) != titleKey {
			continue
		}
		if artistKey != "" && matchKey(tagArtist) != "" && matchKey(tagArtist) != artistKey {
			continue
		}
		candidates[i].kind = lyrics.MatchTag
	}
}

// withTranslation 按配置合并同时间戳双语行与独立翻译文件。
func (s *Source) withTranslation(lines []lyrics.Line, lrcPath string) []lyrics.Line {
	if s.cfg.WantsSameTimestamp() {
		lines = lyrics.MergeSameTimestamp(lines)
	}
	if !s.cfg.WantsSidecar() {
		return lines
	}
	if sidecar, ok := s.sidecarFor(lrcPath); ok {
		if translated, err := lyrics.LoadLocalFile(sidecar, s.cfg.MaxBytes()); err == nil {
			lines = lyrics.MergeSidecar(lines, translated)
		}
	}
	return lines
}

// sidecarFor 找 `<stem>.trans.lrc` / `<stem>.zh.lrc`：先同目录，再 lyrics/ 子目录。
func (s *Source) sidecarFor(lrcPath string) (string, bool) {
	dir := filepath.Dir(lrcPath)
	stem := strings.TrimSuffix(filepath.Base(lrcPath), filepath.Ext(lrcPath))
	for _, suffix := range []string{".trans.lrc", ".zh.lrc"} {
		name := stem + suffix
		if fileExists(filepath.Join(dir, name)) {
			return filepath.Join(dir, name), true
		}
		if sub, err := s.cache.list(dir); err == nil {
			if lyricsDir, ok := sub.dirs[strings.ToLower(library.LyricsDirName)]; ok {
				if fileExists(filepath.Join(lyricsDir, name)) {
					return filepath.Join(lyricsDir, name), true
				}
			}
		}
	}
	return "", false
}

// parseText 解析已经解码好的歌词文本（内嵌歌词用）。
func (s *Source) parseText(text string) []lyrics.Line {
	lines := lyrics.ParseLocalLRC(text)
	if s.cfg.WantsSameTimestamp() {
		lines = lyrics.MergeSameTimestamp(lines)
	}
	return lines
}

// isSidecar 判断文件名是否是独立翻译文件。
func isSidecar(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".trans.lrc") || strings.HasSuffix(lower, ".zh.lrc")
}

var trackNumber = regexp.MustCompile(`^\d{1,3}[\s._\-]+`)

// stripTrackNumber 去掉文件名开头的音轨号，让「01 - 晴天」也能按歌名匹配。
func stripTrackNumber(name string) string { return trackNumber.ReplaceAllString(name, "") }

// matchKey 把文件名/标签压成可比较的键：只保留字母与数字，忽略空白和标点。
func matchKey(s string) string {
	var b strings.Builder
	for _, r := range library.Normalize(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func readText(path string, maxBytes int64) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return "", fmt.Errorf("lyrics: 歌词文件超过大小限制")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return lyrics.DecodeText(data)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// LoadLyricsFile 读取一个独立 .lrc 并按配置合并双语行与翻译文件。
//
// 供歌词绑定（local_file）复用同一套解析与翻译规则，避免绑定路径与自动路径
// 对同一个文件给出不同结果。
func LoadLyricsFile(path string, cfg lyrics.LocalConfig) ([]lyrics.Line, error) {
	lines, err := lyrics.LoadLocalFile(path, cfg.MaxBytes())
	if err != nil {
		return nil, err
	}
	if cfg.WantsSameTimestamp() {
		lines = lyrics.MergeSameTimestamp(lines)
	}
	if !cfg.WantsSidecar() {
		return lines, nil
	}
	if sidecar, ok := sidecarPath(path); ok {
		if translated, err := lyrics.LoadLocalFile(sidecar, cfg.MaxBytes()); err == nil {
			lines = lyrics.MergeSidecar(lines, translated)
		}
	}
	return lines, nil
}

// sidecarPath 找 `<stem>.trans.lrc` / `<stem>.zh.lrc`：先同目录，再 lyrics/ 子目录。
func sidecarPath(lrcPath string) (string, bool) {
	dir := filepath.Dir(lrcPath)
	stem := strings.TrimSuffix(filepath.Base(lrcPath), filepath.Ext(lrcPath))
	for _, suffix := range []string{".trans.lrc", ".zh.lrc"} {
		name := stem + suffix
		if fileExists(filepath.Join(dir, name)) {
			return filepath.Join(dir, name), true
		}
		if fileExists(filepath.Join(dir, library.LyricsDirName, name)) {
			return filepath.Join(dir, library.LyricsDirName, name), true
		}
	}
	return "", false
}
