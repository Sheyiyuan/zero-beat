package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sheyiyuan/zero-beat/internal/config"
	"github.com/sheyiyuan/zero-beat/internal/library"
	"github.com/sheyiyuan/zero-beat/internal/lrclib"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	localsource "github.com/sheyiyuan/zero-beat/internal/source/local"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// LyricsResult 是一次歌词解析的结果。
//
// OffsetKey 表示「这一版歌词实际来自哪一层」，offset 按它读写：绑定失效降级
// 到自动时是 auto，不会把偏移写进失效绑定。
type LyricsResult struct {
	Lines       []lyrics.Line
	OffsetMS    int
	OffsetKey   store.BindingScope
	Source      string
	Bound       bool
	Degraded    bool
	Warning     string
	Binding     *store.LyricBinding
	BindingKind store.BindingScope
}

// LyricCandidate 是歌词来源浮层里的一条候选。
type LyricCandidate struct {
	Kind       string // source | lrclib | local_file | local_song
	Source     source.ID
	ExternalID string
	Page       int
	CID        int64
	LRCLIBID   int64
	Path       string
	SongID     string
	Title      string
	Artist     string
	Duration   int
	// HasSynced 表示已知这条候选带时间轴；false 且 Known=true 表示确定没有。
	HasSynced bool
	Known     bool
	Mark      string
}

// lrclibClient 在没有配置 LRCLIB 客户端时按需创建，避免测试里每个 Service 都建。
func (s *Service) lrclibClient() *lrclib.Client {
	if s.lrclib != nil {
		return s.lrclib
	}
	return lrclib.New()
}

// localLyricsConfig 读取 lyrics.toml；文件按 mtime+size 缓存，改配置后下一次读取生效。
func localLyricsConfig() (lyrics.LocalConfig, []string) {
	cachedCfg.mu.Lock()
	defer cachedCfg.mu.Unlock()
	path, err := config.LyricsPath()
	if err != nil {
		return lyrics.DefaultLocalConfig(), nil
	}
	var (
		mod  time.Time
		size int64
	)
	if info, statErr := os.Stat(path); statErr == nil {
		mod, size = info.ModTime(), info.Size()
	}
	if cachedCfg.path == path && cachedCfg.mod.Equal(mod) && cachedCfg.size == size && cachedCfg.loaded {
		return cachedCfg.cfg, cachedCfg.warnings
	}
	cfg, warnings := lyrics.LoadLocalConfig(path)
	cachedCfg.path, cachedCfg.mod, cachedCfg.size = path, mod, size
	cachedCfg.cfg, cachedCfg.warnings, cachedCfg.loaded = cfg, warnings, true
	return cfg, warnings
}

var cachedCfg struct {
	mu       sync.Mutex
	path     string
	mod      time.Time
	size     int64
	cfg      lyrics.LocalConfig
	warnings []string
	loaded   bool
}

// Lyrics 解析一首歌的歌词：用户绑定 > 自动来源 > LRCLIB。
//
// 失败不返回 error（歌词是增强信息），但绑定失效降级时会带上 Degraded 与
// Warning，让界面能提示「绑定失效，已回退」。
func (s *Service) Lyrics(ctx context.Context, song store.Song) LyricsResult {
	song = store.NormalizeSong(song)
	out := LyricsResult{OffsetKey: store.BindingAuto}
	cfg, cfgWarnings := localLyricsConfig()
	if len(cfgWarnings) > 0 && out.Warning == "" {
		out.Warning = strings.Join(cfgWarnings, "；")
	}
	var degradation string
	if s.store != nil {
		if binding, scope, err := s.store.LyricBinding(song.ID); err == nil && !binding.Empty() {
			lines, label, berr := s.linesForBinding(ctx, song, binding, cfg)
			if len(lines) > 0 {
				out.Lines = lines
				out.Source = label
				out.Bound = true
				out.OffsetKey = scope
				out.BindingKind = scope
				out.OffsetMS = s.store.LyricOffset(song.ID, scope)
				bound := binding
				out.Binding = &bound
				return out
			}
			degradation = bindingFailure(binding, berr)
		}
	}
	out.Lines, out.Source = s.autoLyrics(ctx, song)
	out.OffsetKey = store.BindingAuto
	if s.store != nil {
		out.OffsetMS = s.store.LyricOffset(song.ID, store.BindingAuto)
	}
	if degradation != "" {
		out.Degraded = true
		if out.Warning == "" {
			out.Warning = degradation
		} else {
			out.Warning = degradation + "；" + out.Warning
		}
	}
	return out
}

// bindingFailure 把「取不到」的原因写成人话，同时区分「源不可用」与「没有歌词」。
func bindingFailure(binding store.LyricBinding, err error) string {
	if err != nil {
		return "绑定失效，已回退：" + err.Error()
	}
	if binding.Kind == store.BindingKindSource {
		return "绑定失效，已回退：该平台歌词不存在"
	}
	return "绑定失效，已回退：没有找到带时间轴的歌词"
}

// autoLyrics 按音源类型解析自动歌词，最后用 LRCLIB 兜底。
func (s *Service) autoLyrics(ctx context.Context, song store.Song) ([]lyrics.Line, string) {
	sourceLabel := ""
	if s.sources != nil {
		if src := s.sources.GetString(song.Source); src != nil {
			got, label, err := src.Lyrics(ctx, RefFromSong(song))
			if err != nil {
				sourceLabel = src.Name() + "歌词不可用"
			} else if len(got) > 0 {
				return got, label
			} else {
				sourceLabel = label
			}
		}
	}
	if song.Title != "" {
		if lines, label, ok := s.lrclibFallback(ctx, song); ok {
			return lines, label
		}
	}
	return nil, sourceLabel
}

// lrclibFallback 用本地/平台元数据查 LRCLIB，只接受带时间轴的结果。
func (s *Service) lrclibFallback(ctx context.Context, song store.Song) ([]lyrics.Line, string, bool) {
	res, ok, err := s.lrclibClient().Lookup(ctx, lrclib.Track{
		Title:    song.Title,
		Artist:   song.Artist,
		Duration: time.Duration(song.Duration) * time.Second,
	})
	if err != nil || !ok {
		return nil, "", false
	}
	lines := lyrics.ParseLRC(res.SyncedLyrics)
	if len(lines) == 0 {
		return nil, "", false
	}
	return lines, "LRCLIB", true
}

// linesForBinding 按绑定取歌词。取不到时返回空行与原因，交给调用方降级。
func (s *Service) linesForBinding(ctx context.Context, song store.Song, binding store.LyricBinding, cfg lyrics.LocalConfig) ([]lyrics.Line, string, error) {
	switch binding.Kind {
	case store.BindingKindSource:
		if s.sources == nil {
			return nil, "", fmt.Errorf("没有可用的音源")
		}
		src := s.sources.GetString(binding.Source)
		if src == nil {
			return nil, "", fmt.Errorf("音源 %q 不可用", binding.Source)
		}
		if !src.LoggedIn() {
			return nil, "", fmt.Errorf("%s 未登录", src.Name())
		}
		ref := source.Ref{
			Source:     source.ID(binding.Source),
			ExternalID: binding.ExternalID,
			Page:       binding.Page,
			CID:        binding.CID,
		}
		lines, label, err := src.Lyrics(ctx, ref)
		if err != nil {
			return nil, "", err
		}
		if len(lines) == 0 {
			return nil, "", nil
		}
		return lines, "绑定 · " + label, nil
	case store.BindingKindLRCLIB:
		res, ok, err := s.lrclibClient().Get(ctx, binding.LRCLIBID)
		if err != nil {
			return nil, "", err
		}
		if !ok {
			return nil, "", fmt.Errorf("LRCLIB 记录不存在")
		}
		lines := lyrics.ParseLRC(res.SyncedLyrics)
		if len(lines) == 0 {
			return nil, "", nil
		}
		return lines, "绑定 · LRCLIB", nil
	case store.BindingKindLocalFile:
		lines, err := localsource.LoadLyricsFile(binding.Path, cfg)
		if err != nil {
			return nil, "", err
		}
		if len(lines) == 0 {
			return nil, "", nil
		}
		return lines, "绑定 · 本地 LRC", nil
	case store.BindingKindLocalSong:
		lines, label, err := s.localSongLyrics(ctx, binding.SongID)
		if err != nil {
			return nil, "", err
		}
		if len(lines) == 0 {
			return nil, "", nil
		}
		return lines, "绑定 · " + label, nil
	}
	return nil, "", fmt.Errorf("未知歌词绑定类型 %q", binding.Kind)
}

// localSongLyrics 读另一首本地曲目的旁挂/内嵌歌词。
// 刻意不跟随目标曲目自己的绑定，避免两首歌互相绑定成环。
func (s *Service) localSongLyrics(ctx context.Context, songID string) ([]lyrics.Line, string, error) {
	if s.store == nil {
		return nil, "", fmt.Errorf("没有可用的存储")
	}
	target, err := s.store.Song(songID)
	if err != nil {
		return nil, "", fmt.Errorf("目标本地曲目不存在")
	}
	if target.Source != string(source.Local) {
		return nil, "", fmt.Errorf("目标曲目不是本地曲目")
	}
	if s.sources == nil {
		return nil, "", fmt.Errorf("本地音源不可用")
	}
	src := s.sources.Get(source.Local)
	if src == nil {
		return nil, "", fmt.Errorf("本地音源不可用")
	}
	return src.Lyrics(ctx, RefFromSong(target))
}

// SaveLyricOffset 保存当前来源的歌词偏移。
func (s *Service) SaveLyricOffset(songID string, scope store.BindingScope, offsetMS int) {
	if s.store == nil {
		return
	}
	// 本地绑定的 offset 是本机数据，不触发同步版本号；自动与 portable 会进仓库。
	if scope == store.BindingLocal {
		_ = s.store.SetLyricOffset(songID, scope, offsetMS)
		return
	}
	defer bumpLibraryRevision()
	_ = s.store.SetLyricOffset(songID, scope, offsetMS)
}

// ClearLyricBinding 解除一首歌的歌词绑定，回到自动来源。
func (s *Service) ClearLyricBinding(songID string) error {
	if s.store == nil {
		return fmt.Errorf("没有可用的存储")
	}
	_, portable := s.store.PortableLyricBinding(songID)
	if err := s.store.ClearLyricBinding(songID); err != nil {
		return err
	}
	// 解除了本机绑定但没有 portable 绑定时，仓库内容没有变化。
	if portable {
		bumpLibraryRevision()
	}
	return nil
}

// SearchLyricCandidates 在网易云与 LRCLIB 里搜歌词候选。
//
// filter 为空表示全部，否则只查指定来源（netease / lrclib）。两个来源互不
// 阻塞：一边失败只影响它自己的候选。
func (s *Service) SearchLyricCandidates(ctx context.Context, song store.Song, query, filter string) ([]LyricCandidate, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		query = lrclib.CleanTitle(song.Title)
	}
	if query == "" {
		return nil, fmt.Errorf("请先输入歌名")
	}
	var out []LyricCandidate
	var errs []string
	wantNetEase := filter == "" || filter == string(source.NetEase)
	wantLRCLIB := filter == "" || filter == "lrclib"
	if wantNetEase && s.sources != nil {
		if src := s.sources.Get(source.NetEase); src != nil {
			page, err := src.Search(ctx, query, 1)
			if err != nil {
				errs = append(errs, "网易云："+err.Error())
			} else {
				cands := make([]LyricCandidate, 0, len(page.Candidates))
				for _, c := range page.Candidates {
					cands = append(cands, LyricCandidate{
						Kind:       store.BindingKindSource,
						Source:     source.NetEase,
						ExternalID: c.ExternalID,
						Page:       c.Page,
						Title:      c.Title,
						Artist:     c.Artist,
						Duration:   c.Duration,
						Mark:       string(source.NetEase),
					})
				}
				out = append(out, rankCandidates(song, cands)...)
			}
		}
	}
	if wantLRCLIB {
		results, err := s.lrclibClient().Search(ctx, query)
		if err != nil {
			errs = append(errs, "LRCLIB："+err.Error())
		} else {
			cands := make([]LyricCandidate, 0, len(results))
			for _, r := range results {
				cands = append(cands, LyricCandidate{
					Kind:      store.BindingKindLRCLIB,
					LRCLIBID:  r.ID,
					Title:     r.TrackName,
					Artist:    r.ArtistName,
					Duration:  int(r.Duration / time.Second),
					HasSynced: strings.TrimSpace(r.SyncedLyrics) != "",
					Known:     true,
					Mark:      "lrclib",
				})
			}
			out = append(out, rankCandidates(song, cands)...)
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("搜索歌词失败：%s", strings.Join(errs, "；"))
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out, nil
}

// rankCandidates 按标题/歌手/时长的接近度排序；同分保持来源顺序。
func rankCandidates(song store.Song, cands []LyricCandidate) []LyricCandidate {
	type scored struct {
		cand  LyricCandidate
		score int
	}
	list := make([]scored, len(cands))
	for i, c := range cands {
		list[i] = scored{cand: c, score: closeness(song, c)}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].score > list[j].score })
	for i := range list {
		cands[i] = list[i].cand
	}
	return cands
}

// closeness 给候选打接近度分：歌名完全一致最重要，歌手与时长做加权。
func closeness(song store.Song, c LyricCandidate) int {
	wantTitle := lyricsKey(song.Title)
	gotTitle := lyricsKey(c.Title)
	score := 0
	switch {
	case wantTitle != "" && gotTitle == wantTitle:
		score += 100
	case wantTitle != "" && gotTitle != "" &&
		(strings.Contains(gotTitle, wantTitle) || strings.Contains(wantTitle, gotTitle)):
		score += 40
	}
	if wantArtist := lyricsKey(song.Artist); wantArtist != "" {
		if lyricsKey(c.Artist) == wantArtist {
			score += 30
		}
	}
	if song.Duration > 0 && c.Duration > 0 {
		diff := song.Duration - c.Duration
		if diff < 0 {
			diff = -diff
		}
		switch {
		case diff <= 2:
			score += 50
		case diff <= 5:
			score += 25
		case diff <= 15:
			score += 5
		}
	}
	return score
}

// lyricsKey 是用于接近度比较的紧凑键：只保留字母数字，忽略空白与标点。
func lyricsKey(s string) string {
	var b strings.Builder
	for _, r := range library.Normalize(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// LyricPreview 是绑定前的预览结果：必须至少有带时间轴的行才允许落库。
type LyricPreview struct {
	Lines   []lyrics.Line
	Label   string
	Count   int
	Binding store.LyricBinding
}

// bindingFromCandidate 把候选转成可持久化的绑定。
func bindingFromCandidate(song store.Song, cand LyricCandidate) store.LyricBinding {
	binding := store.LyricBinding{
		Kind:     cand.Kind,
		Title:    cand.Title,
		Artist:   cand.Artist,
		Duration: cand.Duration,
	}
	switch cand.Kind {
	case store.BindingKindSource:
		binding.Source = string(cand.Source)
		binding.ExternalID = cand.ExternalID
		binding.Page = cand.Page
		if binding.Page <= 0 {
			binding.Page = 1
		}
		binding.CID = cand.CID
	case store.BindingKindLRCLIB:
		binding.LRCLIBID = cand.LRCLIBID
	case store.BindingKindLocalFile:
		binding.Path = cand.Path
	case store.BindingKindLocalSong:
		binding.SongID = cand.SongID
	}
	return binding
}

// PreviewLyricBinding 先取一遍歌词并校验时间轴，不落库。
func (s *Service) PreviewLyricBinding(ctx context.Context, song store.Song, cand LyricCandidate) (LyricPreview, error) {
	song = store.NormalizeSong(song)
	binding := bindingFromCandidate(song, cand)
	if err := binding.Validate(); err != nil {
		return LyricPreview{}, err
	}
	cfg, _ := localLyricsConfig()
	lines, label, err := s.linesForBinding(ctx, song, binding, cfg)
	if err != nil {
		return LyricPreview{}, err
	}
	if len(lines) == 0 {
		return LyricPreview{Binding: binding}, fmt.Errorf("没有找到带时间轴的歌词")
	}
	return LyricPreview{Lines: lines, Label: label, Count: len(lines), Binding: binding}, nil
}

// BindLyrics 绑定一首歌的歌词来源；绑定前必须成功预览到带时间轴的歌词。
//
// 返回 cleared=true 表示候选就是这首歌自己，按「解除绑定」处理。
func (s *Service) BindLyrics(ctx context.Context, song store.Song, cand LyricCandidate) (store.LyricBinding, bool, error) {
	if s.store == nil {
		return store.LyricBinding{}, false, fmt.Errorf("没有可用的存储")
	}
	song = store.NormalizeSong(song)
	if cand.Kind == store.BindingKindSource && sameSongRef(song, cand) {
		return store.LyricBinding{}, true, s.ClearLyricBinding(song.ID)
	}
	preview, err := s.PreviewLyricBinding(ctx, song, cand)
	if err != nil {
		return store.LyricBinding{}, false, err
	}
	binding := preview.Binding
	// 新绑定从 0 开始：上一个来源的偏移不该带过来。
	// 例外是「重新定位」：本地文件绑定换个路径仍是同一个来源，保留原来的 offset。
	binding.OffsetMS = 0
	if binding.Kind == store.BindingKindLocalFile {
		if old, scope, _ := s.store.LyricBinding(song.ID); scope == store.BindingLocal &&
			old.Kind == store.BindingKindLocalFile {
			binding.OffsetMS = old.OffsetMS
		}
	}
	binding.BoundAt = time.Now().Unix()
	if err := s.store.SaveLyricBinding(song.ID, binding); err != nil {
		return store.LyricBinding{}, false, err
	}
	// 只有可跨设备复现的绑定才会改变仓库内容；本机绑定不触发同步。
	if binding.Portable() && song.Source != "local" {
		bumpLibraryRevision()
	}
	return binding, false, nil
}

// sameSongRef 判断候选是否指向歌曲自己；指向自己等于解除绑定。
func sameSongRef(song store.Song, cand LyricCandidate) bool {
	page := cand.Page
	if page <= 0 {
		page = 1
	}
	return string(cand.Source) == song.Source &&
		cand.ExternalID == song.ExternalID() &&
		page == song.Page
}
