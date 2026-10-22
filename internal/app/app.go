// Package app 承载与界面无关的业务操作：搜索、取流、歌词、歌单同步、
// 登录与队列恢复。TUI 只负责把结果转成 tea.Msg，不在这里出现 Bubble Tea 类型。
package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/lrclib"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// Service 是一组无界面依赖的业务操作。它是轻量的，可以在每个 tea.Cmd 里新建。
type Service struct {
	sources *source.Registry
	store   *store.Store
	lrclib  *lrclib.Client
}

// New 构造业务服务；st 与 lrc 可以为 nil（测试或纯搜索场景）。
func New(sources *source.Registry, st *store.Store, lrc *lrclib.Client) *Service {
	return &Service{sources: sources, store: st, lrclib: lrc}
}

// Sources 返回注册表。
func (s *Service) Sources() *source.Registry { return s.sources }

// Store 返回存储层。
func (s *Service) Store() *store.Store { return s.store }

// SearchResult 是合并多源搜索后的结果。
type SearchResult struct {
	Candidates []source.Candidate
	Pages      int
	Warn       error
}

// Search 并发搜索注册表里的音源；单源失败只告警，全部失败才返回错误。
func (s *Service) Search(ctx context.Context, keyword string, page int, filter source.ID) (SearchResult, error) {
	var out SearchResult
	if s.sources == nil {
		return out, fmt.Errorf("没有可用的音源")
	}
	var list []source.Source
	if filter == "" {
		for _, src := range s.sources.All() {
			if src.Caps().Has(source.CapSearch) {
				list = append(list, src)
			}
		}
	} else if src := s.sources.Get(filter); src != nil && src.Caps().Has(source.CapSearch) {
		list = []source.Source{src}
	}
	if len(list) == 0 {
		return out, fmt.Errorf("未知音源 %q", filter)
	}

	type result struct {
		page source.SearchPage
		err  error
	}
	results := make([]result, len(list))
	var wg sync.WaitGroup
	wg.Add(len(list))
	for i, src := range list {
		go func(i int, src source.Source) {
			defer wg.Done()
			p, err := src.Search(ctx, keyword, page)
			results[i] = result{page: p, err: err}
		}(i, src)
	}
	wg.Wait()

	var errs []string
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err.Error())
			continue
		}
		out.Candidates = append(out.Candidates, r.page.Candidates...)
		if r.page.Pages > out.Pages {
			out.Pages = r.page.Pages
		}
	}
	switch {
	case len(errs) == len(list):
		return out, fmt.Errorf("搜索失败：%s", joinErrors(errs))
	case len(errs) > 0:
		out.Warn = fmt.Errorf("部分音源搜索失败：%s", joinErrors(errs))
	}
	return out, nil
}

// RefFromSong 把持久化歌曲转成音源定位信息。
func RefFromSong(song store.Song) source.Ref {
	return source.Ref{
		Source:     source.ID(song.Source),
		ExternalID: song.ExternalID(),
		Page:       song.Page,
		CID:        song.CID,
	}
}

// SongFromTrack 把音源曲目转成持久化歌曲；不写平台专属字段。
func SongFromTrack(t source.Track) store.Song {
	song := store.Song{
		Source:   string(t.Source),
		RemoteID: t.ExternalID,
		Page:     t.Page,
		CID:      t.CID,
		Title:    t.Title,
		Artist:   t.Artist,
		Album:    t.Album,
		Duration: t.Duration,
		Cover:    t.Cover,
	}
	return store.NormalizeSong(song)
}

// MergeResolved 只补齐解析结果里的非空字段，避免把已有元数据覆盖成空值。
func MergeResolved(song store.Song, t source.Track) store.Song {
	if t.Source != "" {
		song.Source = string(t.Source)
	}
	if t.ExternalID != "" {
		song.RemoteID = t.ExternalID
	}
	if t.Page > 0 {
		song.Page = t.Page
	}
	if t.CID > 0 {
		song.CID = t.CID
	}
	if t.Title != "" {
		song.Title = t.Title
	}
	if t.Artist != "" {
		song.Artist = t.Artist
	}
	if t.Album != "" {
		song.Album = t.Album
	}
	if t.Duration > 0 {
		song.Duration = t.Duration
	}
	if t.Cover != "" {
		song.Cover = t.Cover
	}
	return store.NormalizeSong(song)
}

// Stream 解析音频地址但不落库，供下载等只读场景使用。
func (s *Service) Stream(ctx context.Context, song store.Song, quality source.Quality) (source.Track, source.Stream, error) {
	song = store.NormalizeSong(song)
	if s.sources == nil {
		return source.Track{}, source.Stream{}, fmt.Errorf("没有可用的音源")
	}
	src := s.sources.GetString(song.Source)
	if src == nil {
		return source.Track{}, source.Stream{}, fmt.Errorf("未知音源 %q", song.Source)
	}
	resolved, err := src.Resolve(ctx, RefFromSong(song), quality)
	if err != nil {
		return source.Track{}, source.Stream{}, err
	}
	return resolved.Track, resolved.Stream, nil
}

// Resolve 解析音频地址并把缺失的元数据回写数据库。
func (s *Service) Resolve(ctx context.Context, song store.Song, quality source.Quality) (store.Song, source.Stream, error) {
	song = store.NormalizeSong(song)
	track, stream, err := s.Stream(ctx, song, quality)
	if err != nil {
		return song, source.Stream{}, err
	}
	song = MergeResolved(song, track)
	if s.store != nil {
		if stored, uerr := s.store.UpsertSong(song); uerr == nil {
			song = stored
		}
	}
	return song, stream, nil
}

// LyricsResult 是平台歌词或 LRCLIB 兜底的结果。
type LyricsResult struct {
	Lines    []lyrics.Line
	OffsetMS int
	Source   string
}

// Lyrics 读取歌词；这是增强信息，失败不返回错误，由调用方决定如何提示。
func (s *Service) Lyrics(ctx context.Context, song store.Song) LyricsResult {
	song = store.NormalizeSong(song)
	out := LyricsResult{}
	if s.store != nil {
		if v, err := s.store.SettingInt("lyric_offset:"+song.ID, 0); err == nil {
			out.OffsetMS = v
		}
	}
	if src := s.sources.GetString(song.Source); src != nil {
		got, label, err := src.Lyrics(ctx, RefFromSong(song))
		if err != nil {
			out.Source = src.Name() + "歌词不可用"
		} else {
			out.Lines = got
			out.Source = label
		}
	}
	if len(out.Lines) == 0 && s.lrclib != nil && song.Title != "" {
		res, ok, err := s.lrclib.Lookup(ctx, lrclib.Track{
			Title:    song.Title,
			Artist:   song.Artist,
			Duration: time.Duration(song.Duration) * time.Second,
		})
		switch {
		case err != nil:
			out.Source = "LRCLIB 暂不可用"
		case ok:
			out.Lines = lyrics.ParseLRC(res.SyncedLyrics)
			if len(out.Lines) > 0 {
				out.Source = "LRCLIB"
			}
		}
	}
	return out
}

// Users 读取所有已登录音源的账号信息。
func (s *Service) Users(ctx context.Context) map[source.ID]source.User {
	users := map[source.ID]source.User{}
	if s.sources == nil {
		return users
	}
	for _, src := range s.sources.All() {
		if !src.LoggedIn() {
			users[src.ID()] = source.User{}
			continue
		}
		if got, err := src.Account(ctx); err == nil {
			users[src.ID()] = got
		}
		// 网络错误时不写 entry，标题栏仍能用 cookie 状态显示「已登录」。
	}
	return users
}

// Queue 是恢复出来的播放队列。
type Queue struct {
	Songs    []store.Song
	Index    int
	Position time.Duration
	OK       bool
}

// RestoreQueue 读取持久化队列，并按 ID 修正失效曲目导致的索引偏移。
func (s *Service) RestoreQueue() Queue {
	if s.store == nil {
		return Queue{}
	}
	state, err := s.store.LoadQueue()
	if err != nil || len(state.IDs) == 0 {
		return Queue{}
	}
	songs, err := s.store.SongsByIDs(state.IDs)
	if err != nil || len(songs) == 0 {
		return Queue{}
	}
	idx := state.Index
	position := time.Duration(state.Position) * time.Millisecond
	if idx < 0 || idx >= len(state.IDs) {
		return Queue{Songs: songs, Position: 0, OK: true}
	}
	saved := state.IDs[idx]
	found := -1
	for i := range songs {
		if songs[i].ID == saved {
			found = i
			break
		}
	}
	if found < 0 {
		return Queue{Songs: songs, Position: 0, OK: true}
	}
	return Queue{Songs: songs, Index: found, Position: position, OK: true}
}

// StartLogin 申请一次扫码登录会话。
func (s *Service) StartLogin(ctx context.Context, id source.ID) (source.QRCode, error) {
	src := s.sources.Get(id)
	if src == nil {
		return source.QRCode{}, fmt.Errorf("未知音源 %q", id)
	}
	return src.StartQRLogin(ctx)
}

// PollLogin 轮询扫码状态；src 由调用方从当前登录页持有。
func (s *Service) PollLogin(ctx context.Context, src source.Source, key string) (source.LoginStatus, error) {
	if src == nil {
		return source.LoginStatus{}, fmt.Errorf("登录会话已失效，请重新获取")
	}
	if key == "" {
		return source.LoginStatus{}, fmt.Errorf("二维码已失效，请重新获取")
	}
	return src.PollQRLogin(ctx, key)
}

func joinErrors(errs []string) string {
	out := ""
	for i, e := range errs {
		if i > 0 {
			out += "；"
		}
		out += e
	}
	return out
}
