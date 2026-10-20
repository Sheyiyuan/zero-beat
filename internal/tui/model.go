// Package tui 是 zero-beat 的终端界面。
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/bili"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

type view int

const (
	viewLibrary view = iota
	viewSearch
	viewQueue
	viewLyrics
	viewHelp
	viewLogin
)

var viewNames = []string{"曲库", "搜索", "队列", "歌词", "帮助", "登录"}

// cycleViews 是 tab 轮转的顺序（登录页靠 L 进入）。
var cycleViews = []view{viewLibrary, viewSearch, viewQueue, viewLyrics, viewHelp}

type playMode int

const (
	modeSequence playMode = iota
	modeLoopOne
	modeShuffle
)

func (p playMode) String() string {
	switch p {
	case modeLoopOne:
		return "单曲"
	case modeShuffle:
		return "随机"
	default:
		return "顺序"
	}
}

type notice struct {
	text  string
	isErr bool
	at    time.Time
}

// Model 是 TUI 的全部状态。
type Model struct {
	width, height int
	view          view
	ctx           context.Context

	client  *bili.Client
	store   *store.Store
	engine  *audio.Engine
	quality bili.Quality

	user bili.User

	playlists   []store.Playlist
	playlistIdx int
	songs       []store.Song
	songIdx     int
	songsBusy   bool

	queue       []store.Song
	queueIdx    int
	queueCursor int

	current  *store.Song
	state    audio.State
	position time.Duration
	duration time.Duration
	volume   float64
	compDB   float64
	mode     playMode

	lyricLines []lyrics.Line
	lyricIdx   int

	searchEditing bool
	searchInput   string
	searchKeyword string
	searchResults []bili.SearchItem
	searchIdx     int
	searchBusy    bool

	qr             *bili.QRCode
	qrASCII        string // 完整登录链接的二维码
	qrASCIICompact string // 去掉跟踪参数后的二维码，终端矮时用
	qrStatus       string

	// demoQuality 仅用于 zero demo 截图，真实运行时以引擎里的流为准。
	demoQuality string

	note notice
}

// New 构造界面模型，并恢复上次的音量、模式与队列。
func New(client *bili.Client, st *store.Store, engine *audio.Engine) *Model {
	m := &Model{
		ctx:     context.Background(),
		client:  client,
		store:   st,
		engine:  engine,
		quality: bili.QualityAuto,
		volume:  0.8,
		mode:    modeSequence,
		state:   audio.StateIdle,
	}
	if v, err := strconv.ParseFloat(st.Setting("volume", "0.8"), 64); err == nil {
		m.volume = v
	}
	if v, err := strconv.ParseFloat(st.Setting("comp_db", "0"), 64); err == nil {
		m.compDB = v
	}
	if v, err := strconv.Atoi(st.Setting("mode", "0")); err == nil && v >= 0 && v <= 2 {
		m.mode = playMode(v)
	}
	engine.SetVolume(m.volume)
	engine.SetCompensationDB(m.compDB)
	return m
}

// Init 启动定时器、事件监听与首批数据加载。
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		waitEngineCmd(m.engine.Events()),
		m.loadUserCmd(),
		m.loadPlaylistsCmd(),
		m.restoreQueueCmd(),
	)
}

// Update 处理消息。
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(tea.Key(msg))

	case tickMsg:
		if m.engine != nil {
			m.position = m.engine.Position()
			if d := m.engine.Duration(); d > 0 {
				m.duration = d
			}
		}
		if idx := lyrics.ActiveIndex(m.lyricLines, int(m.position/time.Millisecond)); idx >= 0 {
			m.lyricIdx = idx
		}
		if m.note.text != "" && time.Since(m.note.at) > 4*time.Second {
			m.note = notice{}
		}
		return m, tickCmd()

	case engineEventMsg:
		return m.handleEngineEvent(msg.ev)

	case userMsg:
		if msg.err == nil {
			m.user = msg.user
		}
		return m, nil

	case playlistsMsg:
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.playlists = msg.items
		if m.playlistIdx >= len(m.playlists) {
			m.playlistIdx = 0
		}
		if msg.warn != nil {
			m.setNotice(msg.warn.Error(), true)
		}
		return m, m.loadSongsCmd(false)

	case songsMsg:
		m.songsBusy = false
		if msg.err != nil {
			if len(msg.items) == 0 {
				m.setError(msg.err)
				return m, nil
			}
			m.setNotice(msg.err.Error(), true)
		}
		if msg.playlistID != m.currentPlaylistID() {
			return m, nil
		}
		m.songs = msg.items
		if m.songIdx >= len(m.songs) {
			m.songIdx = max(0, len(m.songs)-1)
		}
		return m, nil

	case searchMsg:
		m.searchBusy = false
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		if msg.keyword != m.searchKeyword {
			return m, nil
		}
		m.searchResults = msg.items
		m.searchIdx = 0
		if len(msg.items) == 0 {
			m.setNotice("没有搜到结果", false)
		}
		return m, nil

	case streamMsg:
		return m.handleStream(msg)

	case lyricMsg:
		if m.current == nil || msg.songID != m.current.ID {
			return m, nil
		}
		m.lyricLines = msg.lines
		m.lyricIdx = 0
		if msg.err != nil && len(msg.lines) == 0 {
			m.setNotice("歌词获取失败："+msg.err.Error(), true)
		}
		return m, nil

	case queueRestoredMsg:
		if len(msg.songs) > 0 {
			m.queue = msg.songs
			m.queueIdx = min(msg.index, len(msg.songs)-1)
			m.queueCursor = m.queueIdx
			song := msg.songs[m.queueIdx]
			m.current = &song
			if song.Duration > 0 {
				m.duration = time.Duration(song.Duration) * time.Second
			}
		}
		return m, nil

	case qrMsg:
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.qr = msg.qr
		m.setQRCode(msg.qr.URL)
		m.qrStatus = "用 B 站 App 扫码登录"
		return m, pollLoginCmd(m.client, msg.qr.Key)

	case qrPollMsg:
		if msg.err != nil {
			m.qrStatus = "轮询失败：" + msg.err.Error()
			return m, pollLoginTick(m.client, m.qrKey())
		}
		m.qrStatus = msg.status.Message
		if msg.status.LoggedIn {
			m.qrStatus = "登录成功"
			m.setNotice("登录成功", false)
			m.view = viewLibrary
			if payload, err := json.Marshal(m.client.Cookies()); err == nil {
				_ = m.store.SaveAccount("bilibili", payload)
			}
			return m, tea.Batch(m.loadUserCmd(), m.loadPlaylistsCmd())
		}
		if msg.status.Code == 86038 {
			m.qrStatus = "二维码已失效，按 L 重新获取"
			return m, nil
		}
		return m, pollLoginTick(m.client, m.qrKey())
	}
	return m, nil
}

func (m *Model) qrKey() string {
	if m.qr == nil {
		return ""
	}
	return m.qr.Key
}

func (m *Model) handleEngineEvent(ev audio.Event) (tea.Model, tea.Cmd) {
	m.state = ev.State
	switch ev.Type {
	case audio.EventFailed:
		if ev.Err != nil {
			m.setError(ev.Err)
		}
		return m, waitEngineCmd(m.engine.Events())
	case audio.EventEnded:
		cmd := m.advance()
		return m, tea.Batch(cmd, waitEngineCmd(m.engine.Events()))
	default:
		return m, waitEngineCmd(m.engine.Events())
	}
}

func (m *Model) handleStream(msg streamMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.state = audio.StateFailed
		m.setError(fmt.Errorf("解析播放地址失败：%w", msg.err))
		return m, nil
	}
	song := msg.song
	m.current = &song
	m.duration = time.Duration(msg.stream.DurationMS) * time.Millisecond
	if m.duration == 0 && song.Duration > 0 {
		m.duration = time.Duration(song.Duration) * time.Second
	}
	m.position = 0
	m.lyricLines = nil
	m.lyricIdx = 0

	stream := audio.Stream{
		URL:        msg.stream.URL,
		Headers:    msg.stream.Headers,
		Codec:      msg.stream.Codec,
		DurationMS: msg.stream.DurationMS,
		ExpiresAt:  msg.stream.ExpiresAt,
		Quality:    msg.stream.Quality,
	}
	if err := m.engine.Play(m.ctx, stream); err != nil {
		m.setError(err)
		return m, nil
	}
	m.state = audio.StatePlaying
	m.setNotice("正在播放 · "+msg.stream.Quality, false)
	return m, m.lyricsCmd(song)
}

func (m *Model) handleKey(key tea.Key) (tea.Model, tea.Cmd) {
	if m.view == viewSearch && m.searchEditing {
		return m.handleSearchInput(key)
	}

	switch {
	case isCtrl(key, 'c'):
		m.shutdown()
		return m, tea.Quit
	case key.Code == tea.KeyEscape:
		if m.view != viewLibrary {
			m.view = viewLibrary
		}
		return m, nil
	case key.Code == tea.KeyTab:
		m.view = m.nextView()
		return m, m.onViewChanged()
	case key.Code == tea.KeySpace || key.Text == " ":
		if m.engine != nil {
			m.engine.TogglePause()
			m.state = m.engine.State()
		}
		return m, nil
	case isKey(key, "q"):
		m.shutdown()
		return m, tea.Quit
	case isKey(key, "?"):
		if m.view == viewHelp {
			m.view = viewLibrary
		} else {
			m.view = viewHelp
		}
		return m, nil
	case isKey(key, "/"):
		m.view = viewSearch
		m.searchEditing = true
		m.searchInput = m.searchKeyword
		return m, nil
	case isKey(key, "L"):
		m.view = viewLogin
		if m.qr == nil {
			return m, m.startLoginCmd()
		}
		return m, nil
	}

	switch m.view {
	case viewLibrary:
		return m.handleLibraryKey(key)
	case viewSearch:
		return m.handleSearchKey(key)
	case viewQueue:
		return m.handleQueueKey(key)
	case viewLogin:
		if isKey(key, "r") {
			return m, m.startLoginCmd()
		}
		return m.handleCommonPlaybackKey(key)
	}
	return m.handleCommonPlaybackKey(key)
}

func (m *Model) handleCommonPlaybackKey(key tea.Key) (tea.Model, tea.Cmd) {
	switch {
	case isKey(key, "n"):
		return m, m.advance()
	case isKey(key, "p"):
		return m, m.rewind()
	case key.Code == tea.KeyLeft:
		m.seekBy(-5 * time.Second)
		return m, nil
	case key.Code == tea.KeyRight:
		m.seekBy(5 * time.Second)
		return m, nil
	case isKey(key, "+") || isKey(key, "="):
		m.changeVolume(0.05)
		return m, nil
	case isKey(key, "-"):
		m.changeVolume(-0.05)
		return m, nil
	case isKey(key, "]"):
		m.changeComp(1)
		return m, nil
	case isKey(key, "["):
		m.changeComp(-1)
		return m, nil
	case isKey(key, "m"):
		m.mode = (m.mode + 1) % 3
		if m.store != nil {
			_ = m.store.SetSetting("mode", strconv.Itoa(int(m.mode)))
		}
		m.setNotice("播放模式："+m.mode.String(), false)
		return m, nil
	case isKey(key, "1"), isKey(key, "2"), isKey(key, "3"), isKey(key, "4"), isKey(key, "5"), isKey(key, "6"):
		idx, _ := strconv.Atoi(key.Text)
		if idx >= 1 && idx <= len(viewNames) {
			m.view = view(idx - 1)
			return m, m.onViewChanged()
		}
	}
	return m, nil
}

func (m *Model) onViewChanged() tea.Cmd {
	if m.view == viewQueue {
		m.queueCursor = m.queueIdx
	}
	return nil
}

func (m *Model) nextView() view {
	for i, v := range cycleViews {
		if v == m.view {
			return cycleViews[(i+1)%len(cycleViews)]
		}
	}
	return viewLibrary
}

func (m *Model) handleLibraryKey(key tea.Key) (tea.Model, tea.Cmd) {
	switch {
	case key.Code == tea.KeyUp || isKey(key, "k"):
		if m.songIdx > 0 {
			m.songIdx--
		}
		return m, nil
	case key.Code == tea.KeyDown || isKey(key, "j"):
		if m.songIdx < len(m.songs)-1 {
			m.songIdx++
		}
		return m, nil
	case key.Code == tea.KeyLeft || isKey(key, "h"):
		return m, m.movePlaylist(-1)
	case key.Code == tea.KeyRight || isKey(key, "l"):
		return m, m.movePlaylist(1)
	case key.Code == tea.KeyEnter:
		if len(m.songs) == 0 {
			return m, nil
		}
		return m, m.playPlaylistIndex(m.songIdx)
	case isKey(key, "a"):
		return m, m.addCurrentToPlaylist()
	case isKey(key, "A"):
		return m, m.enqueueCurrent()
	case isKey(key, "d"):
		return m, m.removeSelected()
	case isKey(key, "r"):
		if p, ok := m.currentPlaylist(); ok {
			m.songsBusy = true
			return m, m.loadSongsCmdFor(p, true)
		}
		return m, nil
	}
	return m.handleCommonPlaybackKey(key)
}

func (m *Model) handleSearchKey(key tea.Key) (tea.Model, tea.Cmd) {
	switch {
	case isKey(key, "/"):
		m.searchEditing = true
		m.searchInput = m.searchKeyword
		return m, nil
	case key.Code == tea.KeyUp || isKey(key, "k"):
		if m.searchIdx > 0 {
			m.searchIdx--
		}
		return m, nil
	case key.Code == tea.KeyDown || isKey(key, "j"):
		if m.searchIdx < len(m.searchResults)-1 {
			m.searchIdx++
		}
		return m, nil
	case key.Code == tea.KeyEnter:
		return m, m.playSearchResult(m.searchIdx)
	case isKey(key, "a"):
		return m, m.addSearchResultToPlaylist(m.searchIdx)
	case isKey(key, "A"):
		return m, m.enqueueSearchResult(m.searchIdx)
	}
	return m.handleCommonPlaybackKey(key)
}

func (m *Model) handleSearchInput(key tea.Key) (tea.Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEnter:
		m.searchEditing = false
		keyword := strings.TrimSpace(m.searchInput)
		if keyword == "" {
			return m, nil
		}
		m.searchKeyword = keyword
		m.searchBusy = true
		return m, m.searchCmd(keyword)
	case tea.KeyEscape:
		m.searchEditing = false
		return m, nil
	case tea.KeyBackspace:
		if r := []rune(m.searchInput); len(r) > 0 {
			m.searchInput = string(r[:len(r)-1])
		}
		return m, nil
	}
	if isCtrl(key, 'u') {
		m.searchInput = ""
		return m, nil
	}
	if key.Text != "" && key.Mod&tea.ModAlt == 0 {
		m.searchInput += key.Text
	}
	return m, nil
}

func (m *Model) handleQueueKey(key tea.Key) (tea.Model, tea.Cmd) {
	switch {
	case key.Code == tea.KeyUp || isKey(key, "k"):
		if m.queueCursor > 0 {
			m.queueCursor--
		}
		return m, nil
	case key.Code == tea.KeyDown || isKey(key, "j"):
		if m.queueCursor < len(m.queue)-1 {
			m.queueCursor++
		}
		return m, nil
	case key.Code == tea.KeyEnter:
		if m.queueCursor >= 0 && m.queueCursor < len(m.queue) {
			m.queueIdx = m.queueCursor
			return m, m.playQueueIndex(m.queueCursor)
		}
		return m, nil
	case isKey(key, "d"):
		return m, m.removeQueueItem()
	}
	return m.handleCommonPlaybackKey(key)
}

// ---- 动作 ----

func (m *Model) playPlaylistIndex(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.songs) {
		return nil
	}
	m.queue = append([]store.Song(nil), m.songs...)
	m.queueIdx = idx
	m.queueCursor = idx
	m.persistQueue()
	return m.playSong(m.queue[idx])
}

func (m *Model) playQueueIndex(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.queue) {
		return nil
	}
	m.persistQueue()
	return m.playSong(m.queue[idx])
}

func (m *Model) playSearchResult(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.searchResults) {
		return nil
	}
	item := m.searchResults[idx]
	song := store.Song{
		Source:   "bilibili",
		BVID:     item.BVID,
		Page:     1,
		Title:    item.Title,
		Artist:   item.Author,
		Duration: item.Duration,
		Cover:    item.Cover,
	}
	m.queue = append(m.queue, song)
	m.queueIdx = len(m.queue) - 1
	m.queueCursor = m.queueIdx
	m.persistQueue()
	return m.playSong(song)
}

func (m *Model) playSong(song store.Song) tea.Cmd {
	m.state = audio.StateBuffering
	m.current = &song
	m.position = 0
	m.duration = time.Duration(song.Duration) * time.Second
	return m.playSongCmd(song)
}

// advance 按播放模式前进到下一首。
func (m *Model) advance() tea.Cmd {
	if len(m.queue) == 0 {
		return nil
	}
	if m.mode == modeLoopOne && m.current != nil {
		return m.playSong(*m.current)
	}
	next := m.queueIdx + 1
	if m.mode == modeShuffle && len(m.queue) > 1 {
		for next == m.queueIdx {
			next = rand.Intn(len(m.queue))
		}
	} else if next >= len(m.queue) {
		next = 0
	}
	m.queueIdx = next
	m.queueCursor = next
	m.persistQueue()
	return m.playSong(m.queue[next])
}

// rewind 回到上一首。
func (m *Model) rewind() tea.Cmd {
	if len(m.queue) == 0 {
		return nil
	}
	prev := m.queueIdx - 1
	if prev < 0 {
		prev = len(m.queue) - 1
	}
	m.queueIdx = prev
	m.queueCursor = prev
	m.persistQueue()
	return m.playSong(m.queue[prev])
}

func (m *Model) seekBy(delta time.Duration) {
	if m.engine == nil {
		return
	}
	target := m.engine.Position() + delta
	if target < 0 {
		target = 0
	}
	if m.duration > 0 && target > m.duration {
		target = m.duration
	}
	if err := m.engine.Seek(m.ctx, target); err != nil {
		m.setError(err)
		return
	}
	m.position = target
}

func (m *Model) changeVolume(delta float64) {
	m.volume = clamp01(m.volume + delta)
	if m.engine != nil {
		m.engine.SetVolume(m.volume)
	}
	if m.store != nil {
		_ = m.store.SetSetting("volume", strconv.FormatFloat(m.volume, 'f', 2, 64))
	}
	m.setNotice(fmt.Sprintf("音量 %d%%", int(m.volume*100)), false)
}

func (m *Model) changeComp(delta float64) {
	m.compDB += delta
	if m.compDB < -20 {
		m.compDB = -20
	}
	if m.compDB > 12 {
		m.compDB = 12
	}
	if m.engine != nil {
		m.engine.SetCompensationDB(m.compDB)
	}
	if m.store != nil {
		_ = m.store.SetSetting("comp_db", strconv.FormatFloat(m.compDB, 'f', 1, 64))
	}
	m.setNotice(fmt.Sprintf("音量补偿 %+.0f dB", m.compDB), false)
}

func (m *Model) movePlaylist(delta int) tea.Cmd {
	if len(m.playlists) == 0 {
		return nil
	}
	idx := m.playlistIdx + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.playlists) {
		idx = len(m.playlists) - 1
	}
	if idx == m.playlistIdx {
		return nil
	}
	m.playlistIdx = idx
	m.songIdx = 0
	m.songs = nil
	m.songsBusy = true
	return m.loadSongsCmd(false)
}

func (m *Model) addCurrentToPlaylist() tea.Cmd {
	if m.songIdx < 0 || m.songIdx >= len(m.songs) {
		return nil
	}
	return m.addSongToPlaylist(m.songs[m.songIdx], "已加入歌单")
}

func (m *Model) addSearchResultToPlaylist(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.searchResults) {
		return nil
	}
	item := m.searchResults[idx]
	return m.addSongToPlaylist(store.Song{
		Source: "bilibili", BVID: item.BVID, Page: 1,
		Title: item.Title, Artist: item.Author, Duration: item.Duration, Cover: item.Cover,
	}, "已加入歌单")
}

func (m *Model) addSongToPlaylist(song store.Song, label string) tea.Cmd {
	target := "local-default"
	title := "本地歌单"
	if p, ok := m.currentPlaylist(); ok && p.Kind == "local" {
		target, title = p.ID, p.Title
	}
	if err := m.store.AddSongToPlaylist(target, song); err != nil {
		m.setError(err)
		return nil
	}
	m.setNotice(fmt.Sprintf("%s：%s", label, title), false)
	return m.loadPlaylistsCmd()
}

func (m *Model) enqueueCurrent() tea.Cmd {
	if m.songIdx < 0 || m.songIdx >= len(m.songs) {
		return nil
	}
	return m.enqueueSong(m.songs[m.songIdx])
}

func (m *Model) enqueueSearchResult(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.searchResults) {
		return nil
	}
	item := m.searchResults[idx]
	return m.enqueueSong(store.Song{
		Source: "bilibili", BVID: item.BVID, Page: 1,
		Title: item.Title, Artist: item.Author, Duration: item.Duration, Cover: item.Cover,
	})
}

func (m *Model) enqueueSong(song store.Song) tea.Cmd {
	m.queue = append(m.queue, song)
	if len(m.queue) == 1 {
		m.queueIdx = 0
	}
	m.queueCursor = len(m.queue) - 1
	m.persistQueue()
	m.setNotice("已加入队列："+song.Title, false)
	return nil
}

func (m *Model) removeSelected() tea.Cmd {
	if m.songIdx < 0 || m.songIdx >= len(m.songs) {
		return nil
	}
	p, ok := m.currentPlaylist()
	if !ok {
		return nil
	}
	song := m.songs[m.songIdx]
	if err := m.store.RemoveSongFromPlaylist(p.ID, song.ID); err != nil {
		m.setError(err)
		return nil
	}
	m.setNotice("已从歌单移除："+song.Title, false)
	return m.loadSongsCmd(false)
}

func (m *Model) removeQueueItem() tea.Cmd {
	if m.queueCursor < 0 || m.queueCursor >= len(m.queue) {
		return nil
	}
	removed := m.queue[m.queueCursor]
	m.queue = append(m.queue[:m.queueCursor], m.queue[m.queueCursor+1:]...)
	if m.queueCursor >= len(m.queue) {
		m.queueCursor = max(0, len(m.queue)-1)
	}
	if m.queueIdx >= len(m.queue) {
		m.queueIdx = max(0, len(m.queue)-1)
	}
	m.persistQueue()
	m.setNotice("已从队列移除："+removed.Title, false)
	return nil
}

func (m *Model) persistQueue() {
	if m.store == nil {
		return
	}
	ids := make([]string, 0, len(m.queue))
	for _, s := range m.queue {
		if s.ID == "" {
			s.ID = store.SongID(s.Source, s.BVID, s.Page)
		}
		ids = append(ids, s.ID)
	}
	_ = m.store.SaveQueue(store.QueueState{
		IDs:      ids,
		Index:    m.queueIdx,
		Position: int64(m.position / time.Millisecond),
	})
}

// setQRCode 同时生成完整与精简两份二维码，供不同终端高度使用。
func (m *Model) setQRCode(rawURL string) {
	m.qrASCII = renderQR(rawURL)
	m.qrASCIICompact = ""
	if compact := stripTracking(rawURL); compact != rawURL {
		m.qrASCIICompact = renderQR(compact)
	}
}

// qualityLabel 返回当前音轨档位。
func (m *Model) qualityLabel() string {
	if m.engine != nil {
		if q := m.engine.CurrentStream().Quality; q != "" {
			return q
		}
	}
	if m.demoQuality != "" {
		return m.demoQuality
	}
	return "—"
}

func (m *Model) currentPlaylist() (store.Playlist, bool) {
	if m.playlistIdx < 0 || m.playlistIdx >= len(m.playlists) {
		return store.Playlist{}, false
	}
	return m.playlists[m.playlistIdx], true
}

func (m *Model) currentPlaylistID() string {
	if p, ok := m.currentPlaylist(); ok {
		return p.ID
	}
	return ""
}

func (m *Model) shutdown() {
	m.persistQueue()
	if m.engine != nil {
		_ = m.engine.Close()
	}
}

func (m *Model) setNotice(text string, isErr bool) {
	m.note = notice{text: text, isErr: isErr, at: time.Now()}
}

func (m *Model) setError(err error) {
	if err == nil {
		return
	}
	m.setNotice(err.Error(), true)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
