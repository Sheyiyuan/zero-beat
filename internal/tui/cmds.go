package tui

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/skip2/go-qrcode"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/bili"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

type tickMsg struct{}

type engineEventMsg struct{ ev audio.Event }

type userMsg struct {
	user bili.User
	err  error
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

type searchMsg struct {
	keyword string
	items   []bili.SearchItem
	pages   int
	err     error
}

type streamMsg struct {
	song   store.Song
	stream bili.Stream
	err    error
}

type lyricMsg struct {
	songID string
	lines  []lyrics.Line
	err    error
}

type queueRestoredMsg struct {
	songs []store.Song
	index int
}

type qrMsg struct {
	qr  *bili.QRCode
	err error
}

type qrPollMsg struct {
	status bili.LoginStatus
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

func (m *Model) loadUserCmd() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		user, err := client.Nav(ctx)
		return userMsg{user: user, err: err}
	}
}

func (m *Model) loadPlaylistsCmd() tea.Cmd {
	client, st := m.client, m.store
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()

		var warn error
		if client.LoggedIn() {
			folders, err := client.FavoriteFolders(ctx)
			if err != nil {
				warn = fmt.Errorf("读取收藏夹列表失败：%w", err)
			} else {
				for _, f := range folders {
					_, err := st.EnsurePlaylist(store.Playlist{
						ID:       fmt.Sprintf("bili-fav-%d", f.MediaID),
						Title:    f.Title,
						Kind:     "remote",
						RemoteID: strconv.FormatInt(f.MediaID, 10),
					})
					if err != nil && warn == nil {
						warn = err
					}
				}
			}
		}
		if _, err := st.EnsurePlaylist(store.Playlist{ID: "local-default", Title: "本地歌单", Kind: "local"}); err != nil && warn == nil {
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
	client, st := m.client, m.store
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		var warn error
		needRemote := p.Kind == "remote" && (force || p.Count == 0)
		if needRemote {
			mediaID, err := strconv.ParseInt(p.RemoteID, 10, 64)
			switch {
			case err != nil:
				warn = fmt.Errorf("收藏夹 %s 的 ID 无效", p.Title)
			default:
				favs, err := client.AllFavorites(ctx, mediaID, 300)
				if err != nil {
					warn = fmt.Errorf("读取收藏夹失败：%w", err)
					break
				}
				songs := make([]store.Song, 0, len(favs))
				for _, f := range favs {
					songs = append(songs, store.Song{
						Source: "bilibili", BVID: f.BVID, Page: 1,
						Title: f.Title, Artist: f.Artist, Duration: f.Duration, Cover: f.Cover,
					})
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

func (m *Model) searchCmd(keyword string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		items, pages, err := client.Search(ctx, keyword, 1)
		return searchMsg{keyword: keyword, items: items, pages: pages, err: err}
	}
}

// playSongCmd 解析播放地址；缺 CID 时先查视频信息补齐。
func (m *Model) playSongCmd(song store.Song) tea.Cmd {
	client, st, quality := m.client, m.store, m.quality
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if song.Page <= 0 {
			song.Page = 1
		}
		cid := song.CID
		if cid == 0 {
			info, err := client.VideoInfo(ctx, song.BVID)
			if err != nil {
				return streamMsg{song: song, err: err}
			}
			for _, p := range info.Pages {
				if p.Page == song.Page {
					cid = p.CID
					break
				}
			}
			if cid == 0 && len(info.Pages) > 0 {
				cid = info.Pages[0].CID
			}
			if song.Duration == 0 {
				song.Duration = int(info.Duration)
			}
			if song.Title == "" {
				song.Title = info.Title
			}
			if song.Artist == "" {
				song.Artist = info.Owner
			}
			song.CID = cid
			if _, err := st.UpsertSong(song); err != nil {
				// 补写失败不影响播放。
				_ = err
			}
		}
		stream, err := client.PlayURL(ctx, song.BVID, cid, quality)
		return streamMsg{song: song, stream: stream, err: err}
	}
}

func (m *Model) lyricsCmd(song store.Song) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		if song.CID == 0 {
			return lyricMsg{songID: song.ID}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		subs, err := client.Subtitles(ctx, song.BVID, song.CID)
		lines := make([]lyrics.Line, 0, len(subs))
		for _, s := range subs {
			text := strings.TrimSpace(s.Text)
			if text == "" {
				continue
			}
			lines = append(lines, lyrics.Line{StartMS: s.StartMS, Text: text})
		}
		return lyricMsg{songID: song.ID, lines: lines, err: err}
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
		return queueRestoredMsg{songs: songs, index: state.Index}
	}
}

func (m *Model) startLoginCmd() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		qr, err := client.StartQRLogin(ctx)
		return qrMsg{qr: &qr, err: err}
	}
}

func pollLoginCmd(client *bili.Client, key string) tea.Cmd {
	return func() tea.Msg {
		return pollOnce(client, key)
	}
}

// pollLoginTick 等 2 秒再查一次状态，避免打满接口。
func pollLoginTick(client *bili.Client, key string) tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return pollOnce(client, key)
	})
}

func pollOnce(client *bili.Client, key string) tea.Msg {
	if key == "" {
		return qrPollMsg{err: fmt.Errorf("二维码已失效，按 L 重新获取")}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	status, err := client.PollQRLogin(ctx, key)
	return qrPollMsg{status: status, err: err}
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

// renderQR 把登录链接渲染成终端二维码（半块字符，尺寸减半）。
func renderQR(content string) string {
	// 低纠错级别模块更少，普通终端高度也能完整显示。
	qr, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return ""
	}
	return qr.ToSmallString(false)
}
