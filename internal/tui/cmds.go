package tui

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/skip2/go-qrcode"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/config"
	"github.com/sheyiyuan/zero-beat/internal/lrclib"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

type tickMsg struct{}

type engineEventMsg struct{ ev audio.Event }

type usersMsg struct {
	users map[source.ID]source.User
	err   error
}

type playlistsMsg struct {
	items []store.Playlist
	warn  error
	err   error
}

type songsMsg struct {
	playlistID string
	items      []store.Song
	err        error
}

type foldersMsg struct {
	source source.ID
	items  []source.Playlist
	err    error
}

type searchMsg struct {
	keyword    string
	page       int
	source     source.ID // 空表示全部音源
	candidates []source.Candidate
	pages      int
	warn       error
	err        error
}

type streamMsg struct {
	song    store.Song
	stream  source.Stream
	startAt time.Duration
	retry   int
	request int
	err     error
}

type prefetchMsg struct {
	songID string
	song   store.Song
	stream source.Stream
	err    error
}

type lyricMsg struct {
	songID   string
	lines    []lyrics.Line
	offsetMS int
	source   string
	err      error
}

type queueRestoredMsg struct {
	songs    []store.Song
	index    int
	position time.Duration
}

type qrMsg struct {
	source source.ID
	qr     *source.QRCode
	err    error
}

type qrPollMsg struct {
	source source.ID
	status source.LoginStatus
	err    error
}

func tickCmd() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

// waitEngineCmd 把引擎事件桥接成 tea.Msg；每处理一次都要重新挂上。
func waitEngineCmd(events <-chan audio.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return nil
		}
		return engineEventMsg{ev: ev}
	}
}

func (m *Model) loadUsersCmd() tea.Cmd {
	sources := m.sources
	return func() tea.Msg {
		users := map[source.ID]source.User{}
		if sources == nil {
			return usersMsg{users: users}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		for _, src := range sources.All() {
			if !src.LoggedIn() {
				users[src.ID()] = source.User{}
				continue
			}
			if got, err := src.Account(ctx); err == nil {
				users[src.ID()] = got
			}
			// 网络错误时不写 entry，标题栏仍能用 cookie 状态显示「已登录」。
		}
		return usersMsg{users: users}
	}
}

// defaultPlaylistMarker 标记默认本地歌单已经创建过；删掉后不再自动重建。
const defaultPlaylistMarker = "ui.default_playlist_created"

// ensureDefaultPlaylist 只在首次启动时补一个默认本地歌单。
// 之后用户删掉它也不会「删了又回来」；已有任何本地歌单时也不会再补。
func ensureDefaultPlaylist(st *store.Store) error {
	if st == nil {
		return nil
	}
	if st.Setting(defaultPlaylistMarker, "") == "1" {
		return nil
	}
	if _, err := st.Playlist("local-default"); err == nil {
		return st.SetSetting(defaultPlaylistMarker, "1")
	}
	items, err := st.ListPlaylists()
	if err != nil {
		return err
	}
	for _, p := range items {
		if p.Kind == "local" {
			return st.SetSetting(defaultPlaylistMarker, "1")
		}
	}
	if _, err := st.EnsurePlaylist(store.Playlist{ID: "local-default", Title: "本地歌单", Kind: "local"}); err != nil {
		return err
	}
	return st.SetSetting(defaultPlaylistMarker, "1")
}

func (m *Model) loadPlaylistsCmd() tea.Cmd {
	sources, st := m.sources, m.store
	return func() tea.Msg {
		var warn error
		if err := ensureDefaultPlaylist(st); err != nil {
			warn = err
		}
		// 两个平台的远端歌单都不自动导入；只挂一个网易云「每日推荐」入口。
		if sources != nil {
			for _, src := range sources.All() {
				if src.ID() != source.NetEase || !src.LoggedIn() || !src.Caps().Has(source.CapDailyRecommendations) {
					continue
				}
				if _, err := st.EnsurePlaylist(store.Playlist{
					ID:       dailyPlaylistID,
					Source:   string(source.NetEase),
					Title:    "每日推荐",
					Kind:     "remote",
					RemoteID: "daily",
				}); err != nil && warn == nil {
					warn = err
				}
			}
		}
		items, err := st.ListPlaylists()
		if err != nil {
			return playlistsMsg{err: err}
		}
		return playlistsMsg{items: items, warn: warn}
	}
}

// loadRemoteFoldersCmd 拉取一个音源的远端收藏夹/歌单，过滤掉已经加入本地的。
func (m *Model) loadRemoteFoldersCmd(srcID source.ID) tea.Cmd {
	sources, st := m.sources, m.store
	return func() tea.Msg {
		src := sources.Get(srcID)
		if src == nil {
			return foldersMsg{source: srcID, err: fmt.Errorf("音源 %q 不可用", srcID)}
		}
		if !src.LoggedIn() {
			return foldersMsg{source: srcID, err: fmt.Errorf("请先登录 %s", src.Name())}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		lists, err := src.RemotePlaylists(ctx)
		if err != nil {
			return foldersMsg{source: srcID, err: fmt.Errorf("%s 歌单读取失败：%w", src.Name(), err)}
		}
		added := map[string]bool{}
		if st != nil {
			if existing, lerr := st.ListPlaylists(); lerr == nil {
				for _, p := range existing {
					added[p.ID] = true
				}
			}
		}
		out := make([]source.Playlist, 0, len(lists))
		for _, p := range lists {
			if !added[p.ID] {
				out = append(out, p)
			}
		}
		return foldersMsg{source: srcID, items: out}
	}
}

// loadLocalPlaylistsCmd 只读本地库，用于加歌/改名/删除/退出登录，避免每次都打远端接口。
func (m *Model) loadLocalPlaylistsCmd() tea.Cmd {
	st := m.store
	return func() tea.Msg {
		var warn error
		if err := ensureDefaultPlaylist(st); err != nil {
			warn = err
		}
		items, err := st.ListPlaylists()
		if err != nil {
			return playlistsMsg{err: err}
		}
		return playlistsMsg{items: items, warn: warn}
	}
}

func (m *Model) loadSongsCmd(force bool) tea.Cmd {
	p, ok := m.currentPlaylist()
	if !ok {
		return nil
	}
	return m.loadSongsCmdFor(p, force)
}

func (m *Model) loadSongsCmdFor(p store.Playlist, force bool) tea.Cmd {
	sources, st := m.sources, m.store
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		if p.Kind == "mixin" {
			warn := refreshMixinRefs(ctx, st, sources, p)
			items, err := st.PlaylistSongs(p.ID)
			if err != nil {
				return songsMsg{playlistID: p.ID, err: err}
			}
			if warn != nil {
				return songsMsg{playlistID: p.ID, items: items, err: warn}
			}
			return songsMsg{playlistID: p.ID, items: items}
		}

		var warn error
		isDaily := p.ID == dailyPlaylistID
		needRemote := p.Kind == "remote" && (force || p.Count == 0 || isDaily)
		if needRemote {
			srcID := p.Source
			if srcID == "" {
				switch {
				case strings.HasPrefix(p.ID, "bili-fav-"):
					srcID = string(source.Bilibili)
				case strings.HasPrefix(p.ID, "netease-"):
					srcID = string(source.NetEase)
				}
			}
			src := sources.GetString(srcID)
			switch {
			case src == nil:
				warn = fmt.Errorf("歌单 %s 的音源 %q 不可用", p.Title, srcID)
			default:
				var tracks []source.Track
				var err error
				if isDaily {
					rec, ok := src.(source.DailyRecommender)
					if !ok {
						warn = fmt.Errorf("%s 不支持每日推荐", src.Name())
						break
					}
					tracks, err = rec.DailyRecommendations(ctx)
				} else {
					tracks, err = src.PlaylistTracks(ctx, p.RemoteID)
				}
				if err != nil {
					warn = fmt.Errorf("%s 歌单读取失败：%w", src.Name(), err)
					break
				}
				songs := make([]store.Song, 0, len(tracks))
				for _, t := range tracks {
					songs = append(songs, songFromTrack(t))
				}
				if err := st.ReplacePlaylistSongs(p.ID, songs); err != nil {
					warn = err
				}
			}
		}
		items, err := st.PlaylistSongs(p.ID)
		if err != nil {
			return songsMsg{playlistID: p.ID, err: err}
		}
		return songsMsg{playlistID: p.ID, items: items, err: warn}
	}
}

// refreshMixinRefs 在打开混合歌单时刷新其中的远端引用；失败时保留缓存并返回警告。
func refreshMixinRefs(ctx context.Context, st *store.Store, sources *source.Registry, p store.Playlist) error {
	if sources == nil {
		return nil
	}
	refs, err := st.MixinRefs(p.ID)
	if err != nil {
		return err
	}
	var warn error
	for _, ref := range refs {
		if ref.Kind != "remote" {
			continue
		}
		src := sources.GetString(ref.Source)
		if src == nil || !src.LoggedIn() {
			continue
		}
		var tracks []source.Track
		if ref.ID == dailyPlaylistID {
			rec, ok := src.(source.DailyRecommender)
			if !ok {
				continue
			}
			tracks, err = rec.DailyRecommendations(ctx)
		} else {
			tracks, err = src.PlaylistTracks(ctx, ref.RemoteID)
		}
		if err != nil {
			if warn == nil {
				warn = fmt.Errorf("%s 同步失败：%w", ref.Title, err)
			}
			continue
		}
		songs := make([]store.Song, 0, len(tracks))
		for _, t := range tracks {
			songs = append(songs, songFromTrack(t))
		}
		if err := st.ReplacePlaylistSongs(ref.ID, songs); err != nil && warn == nil {
			warn = err
		}
	}
	return warn
}

func (m *Model) searchCmd(keyword string, page int) tea.Cmd {
	sources, filter := m.sources, m.searchSource
	return func() tea.Msg {
		msg := searchMsg{keyword: keyword, page: page, source: filter}
		if sources == nil {
			msg.err = fmt.Errorf("没有可用的音源")
			return msg
		}
		var list []source.Source
		if filter == "" {
			for _, src := range sources.All() {
				if src.Caps().Has(source.CapSearch) {
					list = append(list, src)
				}
			}
		} else if src := sources.Get(filter); src != nil && src.Caps().Has(source.CapSearch) {
			list = []source.Source{src}
		}
		if len(list) == 0 {
			msg.err = fmt.Errorf("未知音源 %q", filter)
			return msg
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()

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
			msg.candidates = append(msg.candidates, r.page.Candidates...)
			if r.page.Pages > msg.pages {
				msg.pages = r.page.Pages
			}
		}
		switch {
		case len(errs) == len(list):
			msg.err = fmt.Errorf("搜索失败：%s", strings.Join(errs, "；"))
		case len(errs) > 0:
			msg.warn = fmt.Errorf("部分音源搜索失败：%s", strings.Join(errs, "；"))
		}
		return msg
	}
}

// playSongCmdAt 解析播放地址并从 startAt 起播；retry 用于自动重试计数。
func (m *Model) playSongCmdAt(song store.Song, startAt time.Duration, retry int) tea.Cmd {
	m.playRequest++
	m.retryCount = retry
	request := m.playRequest
	m.awaitingStream = true
	sources, st, quality := m.sources, m.store, m.quality
	song = normalizeSong(song)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resolved, stream, err := resolveSong(ctx, sources, st, song, quality)
		return streamMsg{song: resolved, stream: stream, startAt: startAt, retry: retry, request: request, err: err}
	}
}

// resolveSong 通过注册表解析音频流，并把缺失的元数据回写数据库。
func resolveSong(ctx context.Context, sources *source.Registry, st *store.Store, song store.Song, quality source.Quality) (store.Song, source.Stream, error) {
	song = normalizeSong(song)
	if sources == nil {
		return song, source.Stream{}, fmt.Errorf("没有可用的音源")
	}
	src := sources.GetString(song.Source)
	if src == nil {
		return song, source.Stream{}, fmt.Errorf("未知音源 %q", song.Source)
	}
	resolved, err := src.Resolve(ctx, refFromSong(song), quality)
	if err != nil {
		return song, source.Stream{}, err
	}
	song = mergeResolved(song, resolved.Track)
	if st != nil {
		if stored, uerr := st.UpsertSong(song); uerr == nil {
			song = stored
		}
	}
	return song, resolved.Stream, nil
}

func refFromSong(song store.Song) source.Ref {
	return source.Ref{
		Source:     source.ID(song.Source),
		ExternalID: song.ExternalID(),
		Page:       song.Page,
		CID:        song.CID,
	}
}

func songFromTrack(t source.Track) store.Song {
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
	if t.Source == source.Bilibili {
		song.BVID = t.ExternalID
	}
	song.Page = max(1, song.Page)
	song.ID = store.SongID(song.Source, song.ExternalID(), song.Page)
	return song
}

// mergeResolved 只补齐解析结果里的非空字段，避免把已有元数据覆盖成空值。
func mergeResolved(song store.Song, t source.Track) store.Song {
	if t.Source != "" {
		song.Source = string(t.Source)
	}
	if t.ExternalID != "" {
		song.RemoteID = t.ExternalID
		if t.Source == source.Bilibili {
			song.BVID = t.ExternalID
		}
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
	song.Page = max(1, song.Page)
	song.ID = store.SongID(song.Source, song.ExternalID(), song.Page)
	return song
}

func (m *Model) lyricsCmd(song store.Song) tea.Cmd {
	sources, lrc, st := m.sources, m.lrclib, m.store
	song = normalizeSong(song)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		offsetMS := 0
		if st != nil {
			if v, err := strconv.Atoi(st.Setting("lyric_offset:"+song.ID, "0")); err == nil {
				offsetMS = v
			}
		}

		var (
			lines      []lyrics.Line
			sourceName string
		)
		if src := sources.GetString(song.Source); src != nil {
			got, label, err := src.Lyrics(ctx, refFromSong(song))
			if err != nil {
				sourceName = src.Name() + "歌词不可用"
			} else {
				lines = got
				sourceName = label
			}
		}
		if len(lines) == 0 && lrc != nil && song.Title != "" {
			res, ok, err := lrc.Lookup(ctx, lrclib.Track{
				Title:    song.Title,
				Artist:   song.Artist,
				Duration: time.Duration(song.Duration) * time.Second,
			})
			if err != nil {
				sourceName = "LRCLIB 暂不可用"
			} else if ok {
				lines = lyrics.ParseLRC(res.SyncedLyrics)
				if len(lines) > 0 {
					sourceName = "LRCLIB"
				}
			}
		}
		// 歌词是增强信息，外部歌词源失败不打断播放，也不弹错误通知。
		return lyricMsg{songID: song.ID, lines: lines, offsetMS: offsetMS, source: sourceName}
	}
}

func (m *Model) restoreQueueCmd() tea.Cmd {
	st := m.store
	return func() tea.Msg {
		state, err := st.LoadQueue()
		if err != nil || len(state.IDs) == 0 {
			return queueRestoredMsg{}
		}
		songs, err := st.SongsByIDs(state.IDs)
		if err != nil || len(songs) == 0 {
			return queueRestoredMsg{}
		}
		idx := state.Index
		position := time.Duration(state.Position) * time.Millisecond
		if idx < 0 || idx >= len(state.IDs) {
			idx = 0
			position = 0
		} else {
			// 失效曲目会被 SongsByIDs 跳过，按 ID 找回当前曲目的新位置。
			saved := state.IDs[idx]
			found := -1
			for i := range songs {
				if songs[i].ID == saved {
					found = i
					break
				}
			}
			if found < 0 {
				idx = 0
				position = 0
			} else {
				idx = found
			}
		}
		return queueRestoredMsg{songs: songs, index: idx, position: position}
	}
}

func (m *Model) startLoginCmd(srcID source.ID) tea.Cmd {
	src := m.sources.Get(srcID)
	return func() tea.Msg {
		if src == nil {
			return qrMsg{source: srcID, err: fmt.Errorf("未知音源 %q", srcID)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		qr, err := src.StartQRLogin(ctx)
		return qrMsg{source: srcID, qr: &qr, err: err}
	}
}

func pollLoginCmd(src source.Source, key string) tea.Cmd {
	return func() tea.Msg { return pollOnce(src, key) }
}

// pollLoginTick 等 2 秒再查一次状态，避免打满接口。
func pollLoginTick(src source.Source, key string) tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return pollOnce(src, key)
	})
}

func pollOnce(src source.Source, key string) tea.Msg {
	if src == nil {
		return qrPollMsg{err: fmt.Errorf("登录会话已失效，按 r 重新获取")}
	}
	if key == "" {
		return qrPollMsg{source: src.ID(), err: fmt.Errorf("二维码已失效，按 r 重新获取")}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	status, err := src.PollQRLogin(ctx, key)
	return qrPollMsg{source: src.ID(), status: status, err: err}
}

// stripTracking 去掉登录链接里的跟踪参数，让二维码在矮终端里也放得下。
// 扫码只依赖 qrcode_key，这些参数仅用于网页端埋点。
func stripTracking(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for _, key := range []string{"source", "spm_id_from", "from_spmid", "from_source"} {
		q.Del(key)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// renderQR 把登录链接渲染成终端二维码。
//
// 与 musicfox 一样使用半块字符：白色模块画亮块、黑色模块留空，
// 对深色终端是正确极性（不是反色）；只留 1 模块静区，比库默认的 4 模块更小。
// 终端太矮时登录页会给「v 打开二维码图片」的 PNG 兜底。
func renderQR(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	qr, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return ""
	}
	qr.DisableBorder = true
	bits := qr.Bitmap()
	if len(bits) == 0 {
		return ""
	}
	const quiet = 1
	size := len(bits) + quiet*2
	at := func(x, y int) bool {
		x -= quiet
		y -= quiet
		if x < 0 || y < 0 || y >= len(bits) || x >= len(bits[y]) {
			return false
		}
		return bits[y][x]
	}

	var out strings.Builder
	for y := 0; y < size; y += 2 {
		for x := 0; x < size; x++ {
			out.WriteRune(qrHalfBlock(at(x, y), at(x, y+1)))
		}
		out.WriteByte('\n')
	}
	return out.String()
}

// writeQRCodePNG 把二维码写成图片，终端太矮时按 v 用系统查看器打开。
func writeQRCodePNG(content string, srcID source.ID) string {
	dir, err := config.CacheDir()
	if err != nil {
		return ""
	}
	name := "login-qr.png"
	if srcID != "" {
		name = "login-qr-" + string(srcID) + ".png"
	}
	path := filepath.Join(dir, name)
	if err := qrcode.WriteFile(content, qrcode.Medium, 256, path); err != nil {
		return ""
	}
	return path
}

func openLocalFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开二维码图片失败：%w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// qrHalfBlock 与 musicfox/qrterminal 的默认映射一致：
// 白模块画亮块，黑模块留空；top/bottom 为 true 表示黑模块。
func qrHalfBlock(topBlack, bottomBlack bool) rune {
	switch {
	case topBlack && bottomBlack:
		return ' '
	case topBlack && !bottomBlack:
		return '▄'
	case !topBlack && bottomBlack:
		return '▀'
	default:
		return '█'
	}
}
