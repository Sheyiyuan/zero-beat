package tui

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/skip2/go-qrcode"

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/config"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/player"
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

// service 把当前依赖装成一个不依赖 Bubble Tea 的业务服务。
func (m *Model) service() *app.Service {
	return app.New(m.sources, m.store, m.lrclib)
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
	svc := m.service()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		return usersMsg{users: svc.Users(ctx)}
	}
}

func (m *Model) loadPlaylistsCmd() tea.Cmd {
	svc := m.service()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		items, warn, err := svc.Playlists(ctx)
		return playlistsMsg{items: items, warn: warn, err: err}
	}
}

// loadRemoteFoldersCmd 拉取一个音源的远端收藏夹/歌单，过滤掉已经加入本地的。
func (m *Model) loadRemoteFoldersCmd(srcID source.ID) tea.Cmd {
	svc := m.service()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := svc.RemoteFolders(ctx, srcID)
		return foldersMsg{source: srcID, items: items, err: err}
	}
}

// loadLocalPlaylistsCmd 只读本地库，用于加歌/改名/删除/退出登录，避免每次都打远端接口。
func (m *Model) loadLocalPlaylistsCmd() tea.Cmd {
	svc := m.service()
	return func() tea.Msg {
		items, warn, err := svc.LocalPlaylists()
		return playlistsMsg{items: items, warn: warn, err: err}
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
	svc := m.service()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		items, warn, err := svc.PlaylistSongs(ctx, p, force)
		if err != nil {
			return songsMsg{playlistID: p.ID, err: err}
		}
		if warn != nil {
			return songsMsg{playlistID: p.ID, items: items, err: warn}
		}
		return songsMsg{playlistID: p.ID, items: items}
	}
}

func (m *Model) searchCmd(keyword string, page int) tea.Cmd {
	svc, filter := m.service(), m.searchSource
	return func() tea.Msg {
		msg := searchMsg{keyword: keyword, page: page, source: filter}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		res, err := svc.Search(ctx, keyword, page, filter)
		msg.candidates = res.Candidates
		msg.pages = res.Pages
		msg.warn = res.Warn
		msg.err = err
		return msg
	}
}

// playSongCmdAt 解析播放地址并从 startAt 起播；retry 用于自动重试计数。
func (m *Model) playSongCmdAt(song store.Song, startAt time.Duration, retry int) tea.Cmd {
	var request int
	m.withController(func(c *player.Controller) {
		request, song = c.BeginPlay(song, startAt, retry)
	})
	svc, quality := m.service(), m.quality
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resolved, stream, err := svc.Resolve(ctx, song, quality)
		return streamMsg{song: resolved, stream: stream, startAt: startAt, retry: retry, request: request, err: err}
	}
}

func (m *Model) lyricsCmd(song store.Song) tea.Cmd {
	svc := m.service()
	song = store.NormalizeSong(song)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res := svc.Lyrics(ctx, song)
		return lyricMsg{songID: song.ID, lines: res.Lines, offsetMS: res.OffsetMS, source: res.Source}
	}
}

func (m *Model) restoreQueueCmd() tea.Cmd {
	svc := m.service()
	return func() tea.Msg {
		q := svc.RestoreQueue()
		if !q.OK {
			return queueRestoredMsg{}
		}
		return queueRestoredMsg{songs: q.Songs, index: q.Index, position: q.Position}
	}
}

func (m *Model) startLoginCmd(srcID source.ID) tea.Cmd {
	svc := m.service()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		qr, err := svc.StartLogin(ctx, srcID)
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
	status, err := app.New(nil, nil, nil).PollLogin(ctx, src, key)
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
