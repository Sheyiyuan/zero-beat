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
	"github.com/sheyiyuan/zero-beat/internal/config"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/lrclib"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
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
	viewFolders
	viewMix
)

var viewNames = []string{"曲库", "搜索", "队列", "歌词", "帮助", "登录", "导入歌单", "混合歌单"}

var defaultKeys = keymap.Default()

// dailyPlaylistID 是网易云「每日推荐」的固定歌单 ID。
const dailyPlaylistID = "netease-daily"

// remotePlaylistsCleanupMarker 标记「清空旧远端歌单镜像」的一次性维护已执行。
const remotePlaylistsCleanupMarker = "maintenance.remote_playlists.cleared_v1"

// qrCacheTTL 是登录二维码的本地缓存时间；到期或轮询返回失效时再重新申请。
const qrCacheTTL = 150 * time.Second

type qrCacheEntry struct {
	qr           *source.QRCode
	ascii        string
	asciiCompact string
	path         string
	status       string
	expiresAt    time.Time
	fetching     bool
}

// cycleViews 是 Tab 轮转的顺序；搜索/帮助/登录用各自快捷键直达，不进轮转。
var cycleViews = []view{viewLibrary, viewQueue, viewLyrics}

type searchMode int

const (
	searchLocalPlaylist searchMode = iota
	searchLocalAll
	searchSource
)

func (s searchMode) String() string {
	switch s {
	case searchLocalAll:
		return "本地全部"
	case searchSource:
		return "音源搜索"
	default:
		return "当前歌单"
	}
}

type libraryFocus int

const (
	focusSongs libraryFocus = iota
	focusPlaylists
)

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

type promptKind int

const (
	promptNone promptKind = iota
	promptNewPlaylist
	promptRenamePlaylist
	promptDeletePlaylist
	promptClearQueue
	promptUnlockPlaylist
)

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

	sources *source.Registry
	store   *store.Store
	engine  *audio.Engine
	quality source.Quality
	keys    *keymap.Keymap

	users map[source.ID]source.User

	playlists         []store.Playlist
	playlistIdx       int
	songs             []store.Song
	songIdx           int
	songsBusy         bool
	libraryFocus      libraryFocus
	playingPlaylistID string

	queue          []store.Song
	queueIdx       int
	queueCursor    int
	shuffleBase    []store.Song
	shuffleBaseIdx int
	shuffleFn      func([]store.Song) // 测试可注入确定性的洗牌

	current  *store.Song
	state    audio.State
	position time.Duration
	duration time.Duration
	volume   float64
	compDB   float64
	mode     playMode
	muted    bool

	// resumeAt > 0 表示当前只是恢复出来的会话，尚未真正起播；space 从这里继续。
	resumeAt       time.Duration
	retryCount     int
	lastPersist    time.Time
	awaitingStream bool
	playRequest    int

	// 下一首预解析结果；只缓存一首，过期或队列变化即作废。
	prefetchID     string
	prefetchStream *source.Stream

	lyricBase     []lyrics.Line
	lyricLines    []lyrics.Line
	lyricIdx      int
	lyricOffsetMS int
	lyricSource   string
	lyricLoaded   bool
	lrclib        *lrclib.Client

	searchEditing bool
	searchInput   string
	searchKeyword string
	searchResults []source.Candidate
	localResults  []store.Song
	searchMode    searchMode
	searchIdx     int
	searchBusy    bool
	searchPage    int
	searchPages   int
	searchSource  source.ID // 空表示全部音源

	downloads   []*downloadTask
	downloadSeq int

	loginSource     source.ID
	qrCache         map[source.ID]*qrCacheEntry
	helpPopup       bool
	helpPopupOffset int

	folderSource source.ID
	folderItems  []source.Playlist
	folderIdx    int
	folderBusy   bool

	mix *mixState

	clip       *clipState
	clipCache  store.Clip
	clipSongID string

	picker *targetPickerState

	palette *paletteState

	marqueeSong   string
	marqueeOffset int

	prompt            promptKind
	promptInput       string
	pendingPlaylistID string
	pendingAddSong    *store.Song
	helpOffset        int

	sleepUntil time.Time
	sleepSlot  int

	qr             *source.QRCode
	qrSource       source.ID
	qrPath         string
	qrASCII        string // 完整登录链接的二维码
	qrASCIICompact string // 去掉跟踪参数后的二维码，终端矮时用
	qrStatus       string

	// demoQuality 仅用于 zero demo 截图，真实运行时以引擎里的流为准。
	demoQuality string

	note notice
}

// New 构造界面模型，并恢复上次的音量、模式与队列。
func New(sources *source.Registry, st *store.Store, engine *audio.Engine) *Model {
	var cleaned bool
	if st != nil {
		cleaned, _ = st.CleanRemotePlaylistsOnce(remotePlaylistsCleanupMarker)
	}
	keys := defaultKeys
	var keyErr error
	if path, err := config.KeymapPath(); err == nil {
		if loaded, err := keymap.Load(path); err != nil {
			keyErr = err
		} else {
			keys = loaded
		}
	}
	m := &Model{
		ctx:         context.Background(),
		sources:     sources,
		store:       st,
		engine:      engine,
		quality:     source.QualityAuto,
		keys:        keys,
		volume:      0.8,
		mode:        modeSequence,
		state:       audio.StateIdle,
		searchPage:  1,
		loginSource: source.Bilibili,
		lrclib:      lrclib.New(),
		users:       map[source.ID]source.User{},
		qrCache:     map[source.ID]*qrCacheEntry{},
	}
	if sources != nil {
		for _, src := range sources.All() {
			if u := src.User(); u.LoggedIn {
				m.users[src.ID()] = u
			}
		}
		if ids := sources.IDs(); len(ids) > 0 {
			m.loginSource = ids[0]
		}
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
	if v, err := strconv.Atoi(st.Setting("quality", "0")); err == nil {
		m.quality = source.ParseQuality(v)
	}
	m.playingPlaylistID = st.Setting("playing_playlist", "")
	engine.SetVolume(m.volume)
	engine.SetCompensationDB(m.compDB)
	if cleaned {
		m.setNotice("已清理旧的远端歌单，按 f 重新导入", false)
	}
	if keyErr != nil {
		m.setNotice("快捷键配置无效，已回退默认："+keyErr.Error(), true)
	}
	applyTheme(resolveThemeMode(st).palette())
	return m
}

func (m *Model) match(a keymap.Action, key tea.Key) bool {
	if m.keys == nil {
		return defaultKeys.Match(a, key)
	}
	return m.keys.Match(a, key)
}

func (m *Model) keyLabel(a keymap.Action) string {
	if m.keys == nil {
		return defaultKeys.Label(a)
	}
	return m.keys.Label(a)
}

// Init 启动定时器、事件监听与首批数据加载。
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		waitEngineCmd(m.engine.Events()),
		m.loadUsersCmd(),
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

	case tea.PasteMsg:
		m.handlePaste(msg.Content)
		return m, nil

	case downloadMsg:
		return m.handleDownloadMsg(msg)

	case downloadClosedMsg:
		return m.handleDownloadClosedMsg(msg)

	case tickMsg:
		// 恢复会话时引擎还是 Idle，不能拿引擎的 0 位置覆盖保存的断点。
		if m.engine != nil && m.engine.State() != audio.StateIdle && !m.awaitingStream {
			m.position = m.engine.Position()
			if d := m.engine.Duration(); d > 0 {
				m.duration = d
			}
		} else if m.duration == 0 && m.current != nil && m.current.Duration > 0 {
			m.duration = time.Duration(m.current.Duration) * time.Second
		}
		if m.duration > 0 && m.position > m.duration {
			m.position = m.duration
		}
		if len(m.lyricLines) > 0 {
			// 前奏/回退到第一句之前时 ActiveIndex 返回 -1，也要如实写回，
			// 否则底部歌词条会停在上一次高亮的那句。
			m.lyricIdx = lyrics.ActiveIndex(m.lyricLines, int(m.position/time.Millisecond))
		}
		m.advanceMarquee()
		if m.note.text != "" && time.Since(m.note.at) > 4*time.Second {
			m.note = notice{}
		}
		if !m.sleepUntil.IsZero() && !time.Now().Before(m.sleepUntil) {
			m.sleepUntil = time.Time{}
			if m.engine != nil && m.engine.State() == audio.StatePlaying {
				m.engine.Pause()
				m.state = audio.StatePaused
			}
			m.setNotice("睡眠定时结束，已暂停", false)
			m.persistQueue()
		}
		// 每 5 秒落一次播放进度，异常退出最多丢 5 秒，而不是整段会话。
		if m.needsPersist() && (m.lastPersist.IsZero() || time.Since(m.lastPersist) >= 5*time.Second) {
			m.persistQueue()
		}
		return m, tickCmd()

	case engineEventMsg:
		return m.handleEngineEvent(msg.ev)

	case usersMsg:
		if msg.users != nil {
			m.users = msg.users
		}
		return m, nil

	case playlistsMsg:
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.playlists = msg.items
		if m.pendingPlaylistID != "" {
			for i, p := range m.playlists {
				if p.ID == m.pendingPlaylistID {
					m.playlistIdx = i
					m.songIdx = 0
					m.songs = nil
					break
				}
			}
			m.pendingPlaylistID = ""
		}
		if m.playlistIdx >= len(m.playlists) {
			m.playlistIdx = max(0, len(m.playlists)-1)
		}
		if m.playlistIdx < 0 {
			m.playlistIdx = 0
		}
		if msg.warn != nil {
			m.setNotice(msg.warn.Error(), true)
		}
		if len(m.playlists) == 0 {
			// 删掉最后一个歌单后清掉残留曲目，别让右栏继续显示已删除歌单。
			m.playlistIdx = 0
			m.songIdx = 0
			m.songs = nil
			m.songsBusy = false
			return m, nil
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

	case foldersMsg:
		if msg.source != m.folderSource {
			return m, nil
		}
		m.folderBusy = false
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.folderItems = msg.items
		if m.folderIdx >= len(m.folderItems) {
			m.folderIdx = max(0, len(m.folderItems)-1)
		}
		if len(msg.items) == 0 {
			m.setNotice("没有可添加的歌单", false)
		}
		return m, nil

	case targetRemotesMsg:
		if m.picker == nil || m.picker.mode != targetPickerAddSong {
			return m, nil
		}
		if m.picker.song.Source != string(msg.source) {
			return m, nil
		}
		m.picker.remoteBusy = false
		if msg.err != nil {
			m.picker.remoteErr = msg.err.Error()
			return m, nil
		}
		seen := map[string]bool{}
		for _, item := range m.picker.items {
			seen[item.ID] = true
			if item.RemoteID != "" {
				seen["remote:"+item.RemoteID] = true
			}
		}
		for _, p := range msg.items {
			if seen[p.ID] || seen["remote:"+p.RemoteID] {
				continue
			}
			m.picker.items = append(m.picker.items, playlistTarget{
				ID:       p.ID,
				Title:    p.Title,
				Source:   string(p.Source),
				RemoteID: p.RemoteID,
				Kind:     "remote",
				Count:    p.Count,
				Cloud:    true,
			})
		}
		m.picker.normalize()
		return m, nil

	case playlistAddMsg:
		if msg.err != nil {
			m.setError(fmt.Errorf("加入云端歌单失败（%s · %s）：%w", source.ID(msg.target.Source).Label(), msg.target.Title, msg.err))
			return m, nil
		}
		imported := false
		for _, p := range m.playlists {
			if p.ID != msg.target.ID {
				continue
			}
			imported = true
			if m.currentPlaylistID() == p.ID {
				m.songsBusy = true
				m.setNotice("已加入云端歌单："+msg.target.Title, false)
				return m, m.loadSongsCmdFor(p, true)
			}
			break
		}
		if imported {
			m.setNotice("已加入云端歌单："+msg.target.Title, false)
		} else {
			m.setNotice("已加入云端歌单：「"+msg.target.Title+"」（按 f 可导入曲库）", false)
		}
		return m, nil

	case searchMsg:
		if msg.keyword != m.searchKeyword || msg.page != m.searchPage || msg.source != m.searchSource {
			return m, nil
		}
		m.searchBusy = false
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.searchResults = msg.candidates
		m.searchPages = msg.pages
		m.searchIdx = 0
		if msg.warn != nil {
			m.setNotice(msg.warn.Error(), true)
		} else if len(msg.candidates) == 0 {
			m.setNotice("没有搜到结果", false)
		}
		return m, nil

	case streamMsg:
		return m.handleStream(msg)

	case prefetchMsg:
		if msg.err == nil && msg.songID != "" && msg.songID == m.nextQueueSongID() {
			s := msg.stream
			m.prefetchID = msg.songID
			m.prefetchStream = &s
			// 把预解析补出的 CID / 封面 / 时长写回队列条目，切歌后歌词才能走 B 站字幕。
			if msg.song.ID != "" {
				for i := range m.queue {
					if normalizeSong(m.queue[i]).ID == msg.songID {
						m.queue[i] = msg.song
						break
					}
				}
				// 分 P 回退时 ID 可能变化，缓存键跟着改，否则 takePrefetch 会错过。
				m.prefetchID = normalizeSong(msg.song).ID
			}
		}
		return m, nil

	case lyricMsg:
		if m.current == nil || msg.songID != m.current.ID {
			return m, nil
		}
		m.lyricBase = msg.lines
		m.lyricOffsetMS = msg.offsetMS
		m.lyricLines = lyrics.Clone(msg.lines, msg.offsetMS)
		m.lyricSource = msg.source
		m.lyricLoaded = true
		m.lyricIdx = lyrics.ActiveIndex(m.lyricLines, int(m.position/time.Millisecond))
		if msg.err != nil && len(msg.lines) == 0 {
			m.setNotice("歌词获取失败："+msg.err.Error(), true)
		}
		return m, nil

	case queueRestoredMsg:
		if len(msg.songs) > 0 {
			m.queue = msg.songs
			idx := msg.index
			if idx < 0 || idx >= len(msg.songs) {
				idx = 0
			}
			m.queueIdx = idx
			m.queueCursor = idx
			song := msg.songs[idx]
			m.current = &song
			if song.Duration > 0 {
				m.duration = time.Duration(song.Duration) * time.Second
			}
			clip := sanitizeClip(m.loadClip(song.ID), m.duration)
			m.setActiveClip(song.ID, clip)
			m.position = clampClipStart(msg.position, clip, m.duration)
			if m.duration > 0 && m.position > m.duration {
				m.position = m.duration
			}
			m.resumeAt = m.position
			m.state = audio.StatePaused
			m.lyricLoaded = false
			m.lyricLines = nil
			m.lyricIdx = -1
			if m.mode == modeShuffle {
				m.startShuffleQueue()
			}
			if m.position > 0 {
				m.setNotice(fmt.Sprintf("已恢复上次进度 %s · space 继续", formatDuration(m.position)), false)
			} else {
				m.setNotice("已恢复上次队列 · space 开始播放", false)
			}
			return m, m.lyricsCmd(song)
		}
		return m, nil

	case qrMsg:
		if m.qrCache == nil {
			m.qrCache = map[source.ID]*qrCacheEntry{}
		}
		if msg.err != nil {
			if entry := m.qrCache[msg.source]; entry != nil {
				entry.fetching = false
				entry.status = "获取二维码失败"
			}
			if msg.source == m.loginSource {
				m.qrStatus = "获取二维码失败"
				m.setError(msg.err)
			}
			return m, nil
		}
		ascii, compact, path := buildQRArt(msg.qr.URL, msg.source)
		entry := &qrCacheEntry{
			qr:           msg.qr,
			ascii:        ascii,
			asciiCompact: compact,
			path:         path,
			status:       "用 " + loginAppName(msg.source) + " 扫码登录",
			expiresAt:    time.Now().Add(qrCacheTTL),
		}
		m.qrCache[msg.source] = entry
		if msg.source != m.loginSource {
			return m, nil // 用户已经切走，缓存起来等切回来
		}
		m.applyQREntry(msg.source, entry)
		return m, pollLoginCmd(m.sources.Get(msg.source), msg.qr.Key)

	case qrPollMsg:
		if msg.source != m.qrSource {
			return m, nil // 用户已经切到别的音源，旧轮询不再续
		}
		src := m.sources.Get(msg.source)
		entry := m.qrCache[msg.source]
		if msg.err != nil {
			m.qrStatus = "轮询失败：" + msg.err.Error()
			if entry != nil {
				entry.status = m.qrStatus
			}
			return m, pollLoginTick(src, m.qrKey())
		}
		m.qrStatus = msg.status.Message
		if entry != nil {
			entry.status = msg.status.Message
		}
		if msg.status.LoggedIn {
			m.qrStatus = "登录成功"
			label := "登录成功"
			if src != nil {
				label = src.Name() + " 登录成功"
				if payload, err := json.Marshal(src.Session()); err == nil {
					_ = m.store.SaveAccount(string(src.ID()), payload)
				}
			}
			m.setNotice(label, false)
			m.view = viewLibrary
			// 登录成功后清掉这个源的二维码缓存。
			delete(m.qrCache, msg.source)
			m.qr = nil
			m.qrSource = ""
			m.qrASCII = ""
			m.qrASCIICompact = ""
			m.qrPath = ""
			m.qrStatus = ""
			return m, tea.Batch(m.loadUsersCmd(), m.loadPlaylistsCmd())
		}
		if msg.status.Expired {
			if entry != nil {
				entry.status = "二维码已失效"
				entry.fetching = false
				entry.expiresAt = time.Time{}
			}
			m.qr = nil
			m.qrASCII = ""
			m.qrASCIICompact = ""
			m.qrPath = ""
			m.qrStatus = "二维码已失效，正在重新获取…"
			if entry != nil {
				entry.fetching = true
				entry.status = m.qrStatus
			}
			return m, m.startLoginCmd(msg.source)
		}
		return m, pollLoginTick(src, m.qrKey())
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
	// 正在切到新曲目时，旧流的失败/结束/停止事件不能打断新请求，也不能把缓冲态改成失败态。
	if m.awaitingStream && (ev.Type == audio.EventFailed || ev.Type == audio.EventEnded || ev.State == audio.StateIdle) {
		return m, waitEngineCmd(m.engine.Events())
	}
	m.state = ev.State
	switch ev.Type {
	case audio.EventFailed:
		if m.current != nil && m.retryCount < 1 {
			m.retryCount++
			m.state = audio.StateBuffering
			m.setNotice("播放中断，正在重新解析并重试…", true)
			return m, tea.Batch(
				m.playSongCmdAt(*m.current, m.position, m.retryCount),
				waitEngineCmd(m.engine.Events()),
			)
		}
		if ev.Err != nil {
			m.setError(ev.Err)
		}
		return m, waitEngineCmd(m.engine.Events())
	case audio.EventEnded:
		m.retryCount = 0
		m.resumeAt = 0
		if m.clip != nil {
			// 编辑片段时用整首预览，播到结尾不要自动切歌，否则草稿会被关掉。
			m.state = audio.StateEnded
			return m, waitEngineCmd(m.engine.Events())
		}
		cmd := m.advance()
		return m, tea.Batch(cmd, waitEngineCmd(m.engine.Events()))
	default:
		return m, waitEngineCmd(m.engine.Events())
	}
}

func (m *Model) handleStream(msg streamMsg) (tea.Model, tea.Cmd) {
	// 用户快速切歌时，旧请求的返回必须丢弃，不能把上一首的流打到新曲目上。
	if msg.request != m.playRequest {
		return m, nil
	}
	// 保留重试计数：成功重试后若再次失败，不再无限重试。
	m.retryCount = msg.retry
	if msg.err != nil {
		// 网络抖动或签名过期时自动重试一次，仍失败才把错误交给用户。
		if msg.retry < 1 {
			m.retryCount = msg.retry + 1
			m.state = audio.StateBuffering
			m.setNotice("解析播放地址失败，正在重试…", true)
			return m, m.playSongCmdAt(msg.song, msg.startAt, m.retryCount)
		}
		m.awaitingStream = false
		if m.engine != nil {
			// 旧曲目可能还在响，失败时必须停掉，避免「界面是新歌、耳朵是旧歌」。
			m.engine.Stop()
		}
		m.state = audio.StateFailed
		m.setError(fmt.Errorf("解析播放地址失败：%w", msg.err))
		return m, nil
	}
	return m, m.startStream(msg.song, msg.stream, msg.startAt)
}

// startStream 把已解析的音频流交给引擎，并刷新界面状态。
func (m *Model) startStream(song store.Song, stream source.Stream, startAt time.Duration) tea.Cmd {
	song = normalizeSong(song)
	if m.clip != nil && m.clip.songID != song.ID {
		m.clip = nil
	}
	m.awaitingStream = false
	// 解析可能补出标题/CID/封面，同步回队列，切歌面板与重启恢复都用得上。
	for i := range m.queue {
		if normalizeSong(m.queue[i]).ID == song.ID {
			m.queue[i] = song
			break
		}
	}
	m.current = &song
	m.duration = stream.Duration
	if m.duration == 0 && song.Duration > 0 {
		m.duration = time.Duration(song.Duration) * time.Second
	}
	clip := sanitizeClip(m.loadClip(song.ID), m.duration)
	if m.clip != nil && m.clip.songID == song.ID {
		// 编辑器打开期间用整首预览，松开旧区间的 -t，光标才能走到区间外。
		m.clip.previewing = true
		clip = store.Clip{}
	}
	startAt = clampClipStart(startAt, clip, m.duration)
	m.setActiveClip(song.ID, clip)
	if m.engine == nil {
		m.state = audio.StatePlaying
		m.position = startAt
		return nil
	}
	m.position = startAt
	if m.duration > 0 && m.position > m.duration {
		m.position = m.duration
	}
	m.lyricBase = nil
	m.lyricLines = nil
	m.lyricIdx = -1
	m.lyricSource = ""
	m.lyricLoaded = false
	m.lyricOffsetMS = 0
	m.resumeAt = 0
	m.awaitingStream = false
	m.invalidatePrefetch()

	durationMS := stream.Duration.Milliseconds()
	if durationMS == 0 && song.Duration > 0 {
		durationMS = int64(song.Duration) * 1000
	}
	audioStream := audio.Stream{
		URL:         stream.URL,
		Headers:     stream.Headers,
		Codec:       stream.Codec,
		DurationMS:  durationMS,
		ExpiresAt:   stream.ExpiresAt,
		Quality:     stream.Quality,
		ClipStartMS: clip.StartMS,
		ClipEndMS:   clip.EndMS,
	}
	if err := m.engine.PlayAt(m.ctx, audioStream, startAt); err != nil {
		m.state = audio.StateFailed
		m.setError(err)
		return nil
	}
	m.state = audio.StatePlaying
	quality := stream.Quality
	if quality == "" {
		quality = "未知音质"
	}
	m.setNotice("正在播放 · "+quality, false)
	m.persistQueue()
	return tea.Batch(m.lyricsCmd(song), m.prefetchNextCmd())
}

func (m *Model) handleKey(key tea.Key) (tea.Model, tea.Cmd) {
	if m.palette != nil {
		return m.handlePaletteKey(key)
	}
	if m.prompt != promptNone {
		return m.handlePromptKey(key)
	}
	if m.clip != nil {
		return m.handleClipKey(key)
	}
	if m.picker != nil {
		return m.handlePickerKey(key)
	}
	if m.view == viewSearch && m.searchEditing {
		return m.handleSearchInput(key)
	}
	if m.helpPopup {
		return m.handleHelpPopupKey(key)
	}

	switch {
	case isCtrl(key, 'c'):
		m.shutdown()
		return m, tea.Quit
	case key.Code == tea.KeyEscape || m.match(keymap.Back, key):
		if m.view == viewMix {
			m.mix = nil
		}
		if m.view != viewLibrary {
			m.view = viewLibrary
		}
		return m, nil
	case key.Code == tea.KeyTab:
		if m.view == viewMix {
			m.mix = nil
		}
		m.view = m.nextView()
		return m, m.onViewChanged()
	case key.Code == tea.KeySpace || key.Text == " ":
		if m.view == viewMix {
			return m.handleMixKey(key)
		}
		return m, m.togglePlayback()
	case m.match(keymap.Quit, key):
		m.shutdown()
		return m, tea.Quit
	case m.match(keymap.QueueView, key):
		m.view = viewQueue
		return m, m.onViewChanged()
	case m.match(keymap.LocalSearch, key):
		m.openLocalSearch(searchLocalPlaylist)
		return m, nil
	case m.match(keymap.Search, key):
		m.openSourceSearch()
		return m, nil
	case m.match(keymap.HelpPage, key):
		if m.view == viewHelp {
			m.view = viewLibrary
		} else {
			m.view = viewHelp
		}
		return m, nil
	case m.match(keymap.HelpPopup, key):
		m.helpPopup = !m.helpPopup
		m.helpPopupOffset = 0
		return m, nil
	case m.match(keymap.Login, key):
		m.view = viewLogin
		return m, m.ensureLoginQR()
	case m.match(keymap.Mute, key):
		m.toggleMute()
		return m, nil
	case m.match(keymap.Quality, key):
		m.cycleQuality()
		return m, nil
	case m.match(keymap.Sleep, key):
		m.cycleSleep()
		return m, nil
	// 歌词偏移在任意视图都能调，底部的常驻歌词条会立刻反映结果。
	case m.match(keymap.OffsetDown, key):
		return m, m.shiftLyrics(-500)
	case m.match(keymap.OffsetUp, key):
		return m, m.shiftLyrics(500)
	case m.match(keymap.OffsetReset, key):
		return m, m.resetLyricOffset()
	case m.match(keymap.Download, key):
		if m.view == viewHelp || m.view == viewLogin {
			break
		}
		return m, m.startCurrentDownload()
	case m.match(keymap.DownloadSelected, key):
		if m.view == viewHelp || m.view == viewLogin {
			break
		}
		return m, m.startSelectedDownload()
	case m.match(keymap.EditClip, key):
		if m.view == viewHelp || m.view == viewLogin {
			break
		}
		return m, m.openClipEditor()
	case m.match(keymap.AddPlaylist, key):
		if song, ok := m.songForPlaylistAdd(); ok {
			return m, m.openAddSongPicker(song)
		}
		if m.view == viewLibrary && m.libraryFocus == focusPlaylists {
			return m, m.needSongFocus()
		}
		return m, nil
	case m.match(keymap.MergePlaylist, key):
		if m.view != viewLibrary {
			return m, nil
		}
		return m, m.openMergePicker()
	}

	switch m.view {
	case viewLibrary:
		return m.handleLibraryKey(key)
	case viewSearch:
		return m.handleSearchKey(key)
	case viewQueue:
		return m.handleQueueKey(key)
	case viewLyrics:
		return m.handleCommonPlaybackKey(key)
	case viewHelp:
		return m.handleHelpKey(key)
	case viewFolders:
		return m.handleFolderKey(key)
	case viewMix:
		return m.handleMixKey(key)
	case viewLogin:
		if m.match(keymap.Up, key) {
			return m, m.switchLoginSource(-1)
		}
		if m.match(keymap.Down, key) {
			return m, m.switchLoginSource(1)
		}
		if m.match(keymap.Refresh, key) {
			if _, ok := m.userFor(m.loginSource); ok {
				return m, m.loadUsersCmd()
			}
			return m, m.refreshLoginQR()
		}
		if m.match(keymap.Source, key) {
			return m, m.switchLoginSource(1)
		}
		if isKey(key, "v") {
			if m.qrPath == "" {
				m.setNotice("二维码图片还没生成", false)
				return m, nil
			}
			if err := openLocalFile(m.qrPath); err != nil {
				m.setError(err)
			} else {
				m.setNotice("已打开二维码图片："+m.qrPath, false)
			}
			return m, nil
		}
		if m.match(keymap.Logout, key) || isKey(key, "W") || isKey(key, "X") {
			return m, m.logout(m.loginSource)
		}
		return m.handleCommonPlaybackKey(key)
	}
	return m.handleCommonPlaybackKey(key)
}

func (m *Model) openLocalSearch(mode searchMode) {
	m.searchMode = mode
	m.view = viewSearch
	m.searchEditing = true
	m.searchInput = m.searchKeyword
	m.searchIdx = 0
	m.searchBusy = false
	m.searchResults = nil
	m.searchPage = 1
	m.searchPages = 0
}

func (m *Model) openSourceSearch() {
	m.searchMode = searchSource
	m.view = viewSearch
	m.searchEditing = true
	m.searchInput = m.searchKeyword
	m.searchIdx = 0
}

func (m *Model) handleCommonPlaybackKey(key tea.Key) (tea.Model, tea.Cmd) {
	switch {
	case m.match(keymap.Next, key):
		return m, m.advance()
	case m.match(keymap.Previous, key):
		return m, m.rewind()
	case m.match(keymap.SeekBack5, key) || key.Code == tea.KeyLeft:
		return m, m.seekBy(-5 * time.Second)
	case m.match(keymap.SeekBack1, key):
		return m, m.seekBy(-1 * time.Second)
	case m.match(keymap.SeekFwd5, key) || key.Code == tea.KeyRight:
		return m, m.seekBy(5 * time.Second)
	case m.match(keymap.SeekFwd10, key):
		return m, m.seekBy(10 * time.Second)
	case m.match(keymap.VolumeUp, key):
		m.changeVolume(0.05)
		return m, nil
	case m.match(keymap.VolumeDown, key):
		m.changeVolume(-0.05)
		return m, nil
	case m.match(keymap.CompUp, key):
		m.changeComp(1)
		return m, nil
	case m.match(keymap.CompDown, key):
		m.changeComp(-1)
		return m, nil
	case m.match(keymap.Mode, key):
		m.setMode((m.mode + 1) % 3)
		return m, nil
	case m.match(keymap.PageUp, key):
		return m, m.pageMove(-1)
	case m.match(keymap.PageDown, key):
		return m, m.pageMove(1)
	case m.match(keymap.Top, key):
		if m.view == viewLibrary && m.libraryFocus == focusPlaylists {
			return m, m.selectPlaylist(0)
		}
		m.moveTop()
		return m, nil
	case m.match(keymap.Bottom, key):
		if m.view == viewLibrary && m.libraryFocus == focusPlaylists {
			return m, m.selectPlaylist(len(m.playlists) - 1)
		}
		m.moveBottom()
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

// pageMove 实现 musicfox 风格的翻页：列表移动光标，搜索翻页，帮助滚动。
func (m *Model) pageMove(delta int) tea.Cmd {
	page := m.bodyHeight()
	switch m.view {
	case viewLibrary:
		if m.libraryFocus == focusPlaylists {
			return m.selectPlaylist(m.playlistIdx + delta*page)
		}
		m.songIdx = clampInt(m.songIdx+delta*page, 0, max(0, len(m.songs)-1))
	case viewSearch:
		if m.searchMode != searchSource {
			return nil
		}
		return m.changeSearchPage(delta)
	case viewQueue:
		m.queueCursor = clampInt(m.queueCursor+delta*page, 0, max(0, len(m.queue)-1))
	case viewFolders:
		m.folderIdx = clampInt(m.folderIdx+delta*page, 0, max(0, len(m.folderItems)-1))
	case viewHelp:
		m.helpOffset = clampInt(m.helpOffset+delta*page, 0, max(0, len(m.helpLines())-m.helpViewport()))
	}
	return nil
}

func (m *Model) moveTop() {
	switch m.view {
	case viewLibrary:
		m.songIdx = 0
	case viewSearch:
		m.searchIdx = 0
	case viewQueue:
		m.queueCursor = 0
	case viewFolders:
		m.folderIdx = 0
	case viewHelp:
		m.helpOffset = 0
	}
}

func (m *Model) moveBottom() {
	switch m.view {
	case viewLibrary:
		m.songIdx = max(0, len(m.songs)-1)
	case viewSearch:
		m.searchIdx = max(0, m.currentSearchLen()-1)
	case viewQueue:
		m.queueCursor = max(0, len(m.queue)-1)
	case viewFolders:
		m.folderIdx = max(0, len(m.folderItems)-1)
	case viewHelp:
		m.helpOffset = max(0, len(m.helpLines())-m.helpViewport())
	}
}

func clampInt(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func (m *Model) onViewChanged() tea.Cmd {
	if m.view == viewQueue {
		m.queueCursor = m.queueIdx
	}
	if m.view == viewHelp {
		m.helpOffset = 0
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
	case m.match(keymap.Left, key):
		m.libraryFocus = focusPlaylists
		return m, nil
	case m.match(keymap.Right, key):
		m.libraryFocus = focusSongs
		return m, nil
	case m.match(keymap.Up, key):
		if m.libraryFocus == focusPlaylists {
			return m, m.movePlaylist(-1)
		}
		if m.songIdx > 0 {
			m.songIdx--
			m.resetMarquee()
		}
		return m, nil
	case m.match(keymap.Down, key):
		if m.libraryFocus == focusPlaylists {
			return m, m.movePlaylist(1)
		}
		if m.songIdx < len(m.songs)-1 {
			m.songIdx++
			m.resetMarquee()
		}
		return m, nil
	case key.Code == tea.KeyEnter || m.match(keymap.PlaySelected, key):
		if m.libraryFocus == focusPlaylists {
			if m.songsBusy {
				m.setNotice("歌单载入中…", false)
				return m, nil
			}
			if len(m.songs) == 0 {
				m.setNotice("这个歌单还是空的", false)
				return m, nil
			}
			m.songIdx = 0
			return m, m.playPlaylistIndex(0)
		}
		if len(m.songs) == 0 {
			return m, nil
		}
		return m, m.playPlaylistIndex(m.songIdx)
	case m.match(keymap.Enqueue, key):
		if m.libraryFocus == focusPlaylists {
			return m, m.needSongFocus()
		}
		return m, m.enqueueCurrent()
	case m.match(keymap.PlayNext, key):
		if m.libraryFocus == focusPlaylists {
			return m, m.needSongFocus()
		}
		if m.songIdx >= 0 && m.songIdx < len(m.songs) {
			return m, m.insertNext(m.songs[m.songIdx])
		}
		return m, nil
	case m.match(keymap.Folders, key):
		return m, m.openFolders(m.defaultFolderSource())
	case m.match(keymap.New, key):
		m.openPalette()
		return m, nil
	case m.match(keymap.MixEdit, key):
		if m.libraryFocus == focusSongs {
			return m, m.needPlaylistFocus()
		}
		if p, ok := m.currentPlaylist(); ok && p.Kind == "mixin" {
			return m, m.openMixEditor(p.ID)
		}
		m.setNotice("只有混合歌单可以编辑引用", true)
		return m, nil
	case m.match(keymap.Unlock, key):
		if m.libraryFocus == focusSongs {
			return m, m.needPlaylistFocus()
		}
		if p, ok := m.currentPlaylist(); ok && p.Kind == "remote" && p.ID != dailyPlaylistID {
			m.beginPrompt(promptUnlockPlaylist)
			return m, nil
		}
		m.setNotice("只有普通同步歌单可以解锁", true)
		return m, nil
	case m.match(keymap.Rename, key):
		if m.libraryFocus == focusSongs {
			return m, m.needPlaylistFocus()
		}
		p, ok := m.currentPlaylist()
		if !ok || (p.Kind != "local" && p.Kind != "mixin") {
			m.setNotice("只有本地或混合歌单可以重命名", true)
			return m, nil
		}
		m.beginPrompt(promptRenamePlaylist)
		return m, nil
	case m.match(keymap.Delete, key):
		if m.libraryFocus == focusSongs {
			return m, m.needPlaylistFocus()
		}
		p, ok := m.currentPlaylist()
		if !ok {
			m.setNotice("没有选中的歌单", true)
			return m, nil
		}
		if p.Kind != "local" && p.Kind != "mixin" {
			m.setNotice("远端同步歌单不能在这里删除", true)
			return m, nil
		}
		m.beginPrompt(promptDeletePlaylist)
		return m, nil
	case m.match(keymap.Remove, key):
		if m.libraryFocus == focusPlaylists {
			return m, m.needSongFocus()
		}
		return m, m.removeSelected()
	case m.match(keymap.Refresh, key):
		if p, ok := m.currentPlaylist(); ok {
			m.songsBusy = true
			return m, m.loadSongsCmdFor(p, true)
		}
		return m, nil
	}
	return m.handleCommonPlaybackKey(key)
}

func (m *Model) needSongFocus() tea.Cmd {
	m.setNotice("先按 → / l 切到歌曲列表", false)
	return nil
}

func (m *Model) needPlaylistFocus() tea.Cmd {
	m.setNotice("先按 ← / h 切到歌单列表", false)
	return nil
}

func (m *Model) handleSearchKey(key tea.Key) (tea.Model, tea.Cmd) {
	switch {
	case m.match(keymap.LocalSearch, key):
		m.openLocalSearch(searchLocalPlaylist)
		return m, nil
	case m.match(keymap.Search, key):
		m.openSourceSearch()
		return m, nil
	case m.match(keymap.Up, key):
		if m.searchIdx > 0 {
			m.searchIdx--
		}
		return m, nil
	case m.match(keymap.Down, key):
		if m.searchIdx < m.currentSearchLen()-1 {
			m.searchIdx++
		}
		return m, nil
	case key.Code == tea.KeyEnter || m.match(keymap.PlaySelected, key):
		if m.searchMode == searchSource {
			return m, m.playSearchResult(m.searchIdx)
		}
		return m, m.playLocalResult(m.searchIdx)
	case m.match(keymap.Enqueue, key):
		if m.searchMode == searchSource {
			return m, m.enqueueSearchResult(m.searchIdx)
		}
		return m, m.enqueueLocalResult(m.searchIdx)
	case m.match(keymap.PlayNext, key):
		if m.searchMode == searchSource {
			if m.searchIdx >= 0 && m.searchIdx < len(m.searchResults) {
				return m, m.insertNext(m.searchResultSong(m.searchIdx))
			}
			return m, nil
		}
		return m, m.insertNextLocalResult(m.searchIdx)
	case m.match(keymap.Refresh, key):
		return m, m.toggleSearchScope()
	case m.match(keymap.Source, key):
		if m.searchMode != searchSource {
			m.setNotice("r 切换当前歌单/本地全部；s 仅用于音源搜索", false)
			return m, nil
		}
		return m, m.cycleSearchSource()
	}
	return m.handleCommonPlaybackKey(key)
}

func (m *Model) currentSearchLen() int {
	if m.searchMode == searchSource {
		return len(m.searchResults)
	}
	return len(m.localResults)
}

func (m *Model) toggleSearchScope() tea.Cmd {
	if m.searchMode == searchSource {
		if strings.TrimSpace(m.searchKeyword) == "" {
			return nil
		}
		m.searchPage = 1
		m.searchPages = 0
		m.searchResults = nil
		m.searchIdx = 0
		m.searchBusy = true
		return m.searchCmd(m.searchKeyword, 1)
	}
	if m.searchMode == searchLocalPlaylist {
		m.searchMode = searchLocalAll
	} else {
		m.searchMode = searchLocalPlaylist
	}
	m.searchIdx = 0
	m.searchBusy = false
	return m.runLocalSearch()
}

func (m *Model) runLocalSearch() tea.Cmd {
	keyword := strings.ToLower(strings.TrimSpace(m.searchKeyword))
	m.searchIdx = 0
	if keyword == "" {
		m.localResults = nil
		return nil
	}
	if m.searchMode == searchLocalPlaylist {
		out := make([]store.Song, 0)
		for _, song := range m.songs {
			if songMatches(song, keyword) {
				out = append(out, song)
			}
		}
		m.localResults = out
		return nil
	}
	if m.store == nil {
		m.localResults = nil
		return nil
	}
	songs, err := m.store.SearchSongs(keyword, 500)
	if err != nil {
		m.setError(err)
		return nil
	}
	m.localResults = songs
	return nil
}

func songMatches(song store.Song, lowerQuery string) bool {
	return strings.Contains(strings.ToLower(song.Title), lowerQuery) ||
		strings.Contains(strings.ToLower(song.Artist), lowerQuery) ||
		strings.Contains(strings.ToLower(song.Album), lowerQuery)
}

func (m *Model) playLocalResult(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.localResults) {
		return nil
	}
	song := normalizeSong(m.localResults[idx])
	queue := append([]store.Song(nil), m.localResults...)
	queueIdx := idx
	if m.searchMode == searchLocalPlaylist {
		queue = append([]store.Song(nil), m.songs...)
		queueIdx = -1
		for i := range queue {
			if queue[i].ID == song.ID {
				queueIdx = i
				break
			}
		}
		if queueIdx < 0 {
			queue = []store.Song{song}
			queueIdx = 0
		}
	}
	if m.searchMode == searchLocalPlaylist {
		m.setPlayingPlaylist(m.currentPlaylistID())
	} else {
		m.setPlayingPlaylist("")
	}
	m.loadQueue(queue, queueIdx)
	return m.playSong(m.queue[m.queueIdx])
}

func (m *Model) addLocalResultToPlaylist(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.localResults) {
		return nil
	}
	return m.openAddSongPicker(m.localResults[idx])
}

func (m *Model) enqueueLocalResult(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.localResults) {
		return nil
	}
	return m.enqueueSong(m.localResults[idx])
}

func (m *Model) insertNextLocalResult(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.localResults) {
		return nil
	}
	return m.insertNext(m.localResults[idx])
}

// cycleSearchSource 在「全部 / 各音源」之间切换搜索范围。
func (m *Model) cycleSearchSource() tea.Cmd {
	if m.sources == nil {
		return nil
	}
	order := []source.ID{""}
	for _, src := range m.sources.All() {
		if src.Caps().Has(source.CapSearch) {
			order = append(order, src.ID())
		}
	}
	idx := 0
	for i, id := range order {
		if id == m.searchSource {
			idx = i
			break
		}
	}
	m.searchSource = order[(idx+1)%len(order)]
	label := "全部音源"
	if m.searchSource != "" {
		label = m.searchSource.Label()
		if src := m.sources.Get(m.searchSource); src != nil {
			label = src.Name()
		}
	}
	m.searchPage = 1
	m.searchPages = 0
	m.searchResults = nil
	m.searchIdx = 0
	if m.searchKeyword != "" {
		m.searchBusy = true
		return m.searchCmd(m.searchKeyword, 1)
	}
	m.setNotice("搜索源："+label, false)
	return nil
}

// changeSearchPage 在搜索结果里翻页；page 结果返回前忽略重复按键。
func (m *Model) changeSearchPage(delta int) tea.Cmd {
	if m.searchKeyword == "" || m.searchBusy {
		return nil
	}
	target := m.searchPage + delta
	if target < 1 {
		m.setNotice("已经是第一页", false)
		return nil
	}
	if m.searchPages > 0 && target > m.searchPages {
		m.setNotice("已经是最后一页", false)
		return nil
	}
	m.searchPage = target
	m.searchBusy = true
	return m.searchCmd(m.searchKeyword, target)
}

func (m *Model) handleSearchInput(key tea.Key) (tea.Model, tea.Cmd) {
	if isCtrl(key, 'c') {
		m.shutdown()
		return m, tea.Quit
	}
	switch key.Code {
	case tea.KeyEnter:
		m.searchEditing = false
		keyword := strings.TrimSpace(m.searchInput)
		m.searchKeyword = keyword
		m.searchIdx = 0
		if m.searchMode == searchSource {
			m.searchPage = 1
			m.searchPages = 0
			m.searchResults = nil
			if keyword == "" {
				m.searchBusy = false
				return m, nil
			}
			m.searchBusy = true
			return m, m.searchCmd(keyword, 1)
		}
		m.searchBusy = false
		m.searchResults = nil
		return m, m.runLocalSearch()
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
	if key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) == 0 {
		m.searchInput += key.Text
	}
	return m, nil
}

func (m *Model) handleQueueKey(key tea.Key) (tea.Model, tea.Cmd) {
	switch {
	case m.match(keymap.Up, key):
		if m.queueCursor > 0 {
			m.queueCursor--
		}
		return m, nil
	case m.match(keymap.Down, key):
		if m.queueCursor < len(m.queue)-1 {
			m.queueCursor++
		}
		return m, nil
	case key.Code == tea.KeyEnter || m.match(keymap.PlaySelected, key):
		if m.queueCursor >= 0 && m.queueCursor < len(m.queue) {
			m.queueIdx = m.queueCursor
			return m, m.playQueueIndex(m.queueCursor)
		}
		return m, nil
	case m.match(keymap.QueueDown, key):
		return m, m.moveQueueItem(1)
	case m.match(keymap.QueueUp, key):
		return m, m.moveQueueItem(-1)
	case m.match(keymap.PlayNext, key):
		return m, m.playQueueItemNext()
	case m.match(keymap.Delete, key):
		if len(m.queue) == 0 {
			return m, nil
		}
		m.beginPrompt(promptClearQueue)
		return m, nil
	case m.match(keymap.Remove, key):
		return m, m.removeQueueItem()
	}
	return m.handleCommonPlaybackKey(key)
}

// handleFolderKey 处理远端歌单/收藏夹导入页。
func (m *Model) handleFolderKey(key tea.Key) (tea.Model, tea.Cmd) {
	switch {
	case m.match(keymap.Up, key):
		if m.folderIdx > 0 {
			m.folderIdx--
		}
		return m, nil
	case m.match(keymap.Down, key):
		if m.folderIdx < len(m.folderItems)-1 {
			m.folderIdx++
		}
		return m, nil
	case key.Code == tea.KeyEnter || m.match(keymap.PlaySelected, key):
		return m, m.addSelectedFolder()
	case m.match(keymap.Source, key):
		return m, m.switchFolderSource()
	case m.match(keymap.Refresh, key):
		m.folderBusy = true
		return m, m.loadRemoteFoldersCmd(m.folderSource)
	}
	return m.handleCommonPlaybackKey(key)
}

func (m *Model) defaultFolderSource() source.ID {
	if m.sources == nil {
		return source.Bilibili
	}
	if src := m.sources.Get(m.folderSource); src != nil && src.LoggedIn() && src.Caps().Has(source.CapRemotePlaylists) {
		return m.folderSource
	}
	if src := m.sources.Get(m.loginSource); src != nil && src.LoggedIn() && src.Caps().Has(source.CapRemotePlaylists) {
		return m.loginSource
	}
	for _, src := range m.sources.All() {
		if src.LoggedIn() && src.Caps().Has(source.CapRemotePlaylists) {
			return src.ID()
		}
	}
	for _, src := range m.sources.All() {
		if src.Caps().Has(source.CapRemotePlaylists) {
			return src.ID()
		}
	}
	return source.Bilibili
}

func (m *Model) switchFolderSource() tea.Cmd {
	if m.sources == nil {
		return nil
	}
	var ids []source.ID
	for _, src := range m.sources.All() {
		if src.Caps().Has(source.CapRemotePlaylists) {
			ids = append(ids, src.ID())
		}
	}
	if len(ids) <= 1 {
		return nil
	}
	idx := 0
	for i, id := range ids {
		if id == m.folderSource {
			idx = i
			break
		}
	}
	m.folderSource = ids[(idx+1)%len(ids)]
	m.folderItems = nil
	m.folderIdx = 0
	m.folderBusy = true
	return m.loadRemoteFoldersCmd(m.folderSource)
}

func (m *Model) openFolders(srcID source.ID) tea.Cmd {
	src := m.sources.Get(srcID)
	if src == nil || !src.LoggedIn() {
		m.setNotice("请先登录 "+srcID.Label()+" 再浏览歌单", true)
		return nil
	}
	m.folderSource = srcID
	m.folderItems = nil
	m.folderIdx = 0
	m.folderBusy = true
	m.view = viewFolders
	return m.loadRemoteFoldersCmd(srcID)
}

func (m *Model) addSelectedFolder() tea.Cmd {
	if m.folderIdx < 0 || m.folderIdx >= len(m.folderItems) {
		return nil
	}
	if m.store == nil {
		return nil
	}
	p := m.folderItems[m.folderIdx]
	if _, err := m.store.EnsurePlaylist(store.Playlist{
		ID:       p.ID,
		Source:   string(p.Source),
		Title:    p.Title,
		Kind:     "remote",
		RemoteID: p.RemoteID,
	}); err != nil {
		m.setError(err)
		return nil
	}
	m.pendingPlaylistID = p.ID
	m.view = viewLibrary
	m.setNotice("已添加歌单："+p.Title, false)
	return m.loadLocalPlaylistsCmd()
}

// handleHelpKey 处理帮助页滚动。
func (m *Model) handleHelpKey(key tea.Key) (tea.Model, tea.Cmd) {
	maxOffset := len(m.helpLines()) - m.helpViewport()
	if maxOffset < 0 {
		maxOffset = 0
	}
	switch {
	case m.match(keymap.Up, key):
		if m.helpOffset > 0 {
			m.helpOffset--
		}
		return m, nil
	case m.match(keymap.Down, key):
		if m.helpOffset < maxOffset {
			m.helpOffset++
		}
		return m, nil
	case m.match(keymap.Top, key):
		m.helpOffset = 0
		return m, nil
	case m.match(keymap.Bottom, key):
		m.helpOffset = maxOffset
		return m, nil
	}
	return m.handleCommonPlaybackKey(key)
}

// helpViewport 返回帮助页可见行数，与 render() 的正文高度保持一致。
func (m *Model) helpViewport() int {
	h := m.height - 6
	if h < 3 {
		h = 3
	}
	return h
}

// ---- 动作 ----

func normalizeSong(song store.Song) store.Song {
	if song.Source == "" {
		song.Source = "bilibili"
	}
	if song.Page <= 0 {
		song.Page = 1
	}
	if song.RemoteID == "" {
		song.RemoteID = song.BVID
	}
	if song.BVID == "" && song.Source == "bilibili" {
		song.BVID = song.RemoteID
	}
	if song.ID == "" {
		song.ID = store.SongID(song.Source, song.ExternalID(), song.Page)
	}
	return song
}

func (m *Model) ensureStored(song store.Song) store.Song {
	song = normalizeSong(song)
	if m.store == nil {
		return song
	}
	if stored, err := m.store.UpsertSong(song); err == nil {
		return stored
	}
	return song
}

func (m *Model) searchResultSong(idx int) store.Song {
	return normalizeSong(songFromTrack(m.searchResults[idx].Track))
}

func (m *Model) playPlaylistIndex(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.songs) {
		return nil
	}
	queue := make([]store.Song, len(m.songs))
	for i := range m.songs {
		queue[i] = normalizeSong(m.songs[i])
	}
	m.setPlayingPlaylist(m.currentPlaylistID())
	m.loadQueue(queue, idx)
	if p, ok := m.currentPlaylist(); ok {
		queueLabel := "已载入队列"
		if m.mode == modeShuffle {
			queueLabel = "已洗牌入队"
		}
		m.setNotice(fmt.Sprintf("播放歌单：%s（%d 首，%s）", p.Title, len(queue), queueLabel), false)
	}
	return m.playSong(m.queue[m.queueIdx])
}

func (m *Model) playQueueIndex(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.queue) {
		return nil
	}
	m.queueIdx = idx
	m.queueCursor = idx
	return m.playSong(m.queue[idx])
}

func (m *Model) playSearchResult(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.searchResults) {
		return nil
	}
	song := m.ensureStored(m.searchResultSong(idx))
	m.queue = append(m.queue, song)
	m.queueIdx = len(m.queue) - 1
	m.queueCursor = m.queueIdx
	m.setPlayingPlaylist("")
	m.invalidatePrefetch()
	m.persistQueue()
	return m.playSong(song)
}

func (m *Model) playSong(song store.Song) tea.Cmd {
	song = normalizeSong(song)
	m.state = audio.StateBuffering
	m.current = &song
	m.position = 0
	m.resumeAt = 0
	m.clip = nil
	m.clipSongID = ""
	m.clipCache = store.Clip{}
	m.retryCount = 0
	m.duration = time.Duration(song.Duration) * time.Second
	m.lyricBase = nil
	m.lyricLines = nil
	m.lyricIdx = -1
	m.lyricSource = ""
	m.lyricLoaded = false
	m.lyricOffsetMS = 0
	m.invalidatePrefetch()
	m.persistQueue()
	return m.playSongCmdAt(song, 0, 0)
}

// setMode 切换播放模式；进入/离开随机模式时重建队列顺序。
func (m *Model) setMode(next playMode) {
	if next == m.mode {
		return
	}
	wasShuffle := m.mode == modeShuffle
	hadShuffleBase := len(m.shuffleBase) > 0
	if wasShuffle {
		m.restoreShuffleQueue()
	}
	m.mode = next
	if next == modeShuffle {
		m.startShuffleQueue()
	}
	if m.store != nil {
		_ = m.store.SetSetting("mode", strconv.Itoa(int(m.mode)))
	}
	m.invalidatePrefetch()
	label := m.mode.String()
	if next == modeShuffle && len(m.queue) > 1 {
		label += "（队列已洗牌）"
	}
	if wasShuffle && next != modeShuffle && hadShuffleBase {
		label += "（队列已恢复）"
	}
	m.setNotice("播放模式："+label, false)
}

// startShuffleQueue 保存当前顺序并把队列洗牌；当前歌曲保持在新的位置。
func (m *Model) startShuffleQueue() {
	if len(m.queue) < 2 {
		m.shuffleBase = nil
		return
	}
	m.shuffleBase = append([]store.Song(nil), m.queue...)
	m.shuffleBaseIdx = m.queueIdx
	id := ""
	if m.current != nil {
		id = m.current.ID
	}
	m.shuffleQueueKeeping(id)
	m.persistQueue()
}

// restoreShuffleQueue 退出随机时按进入前的顺序恢复队列；随机期间新增的歌追加到末尾。
func (m *Model) restoreShuffleQueue() {
	if len(m.shuffleBase) == 0 {
		return
	}
	currentID := ""
	if m.current != nil {
		currentID = m.current.ID
	}
	counts := make(map[string]int, len(m.queue))
	for _, song := range m.queue {
		song = normalizeSong(song)
		counts[song.ID]++
	}
	restored := make([]store.Song, 0, len(m.queue))
	appendSong := func(song store.Song) {
		song = normalizeSong(song)
		if counts[song.ID] <= 0 {
			return
		}
		restored = append(restored, song)
		counts[song.ID]--
	}
	for _, song := range m.shuffleBase {
		appendSong(song)
	}
	for _, song := range m.queue {
		appendSong(song)
	}
	m.queue = restored
	m.shuffleBase = nil
	m.queueIdx = m.findQueueIndex(currentID)
	if m.queueIdx < 0 {
		m.queueIdx = clampInt(m.shuffleBaseIdx, 0, max(0, len(m.queue)-1))
	}
	m.queueCursor = m.queueIdx
	m.invalidatePrefetch()
	m.persistQueue()
}

func (m *Model) shuffleQueueKeeping(id string) {
	if len(m.queue) < 2 {
		m.queueIdx = clampInt(m.queueIdx, 0, max(0, len(m.queue)-1))
		m.queueCursor = m.queueIdx
		return
	}
	if m.shuffleFn != nil {
		m.shuffleFn(m.queue)
	} else {
		rand.Shuffle(len(m.queue), func(i, j int) {
			m.queue[i], m.queue[j] = m.queue[j], m.queue[i]
		})
	}
	if idx := m.findQueueIndex(id); idx >= 0 {
		m.queueIdx = idx
	}
	m.queueIdx = clampInt(m.queueIdx, 0, len(m.queue)-1)
	m.queueCursor = m.queueIdx
	m.invalidatePrefetch()
}

func (m *Model) findQueueIndex(id string) int {
	if id == "" {
		return -1
	}
	for i := range m.queue {
		if m.queue[i].ID == id {
			return i
		}
	}
	return -1
}

// loadQueue 用新歌单/新搜索结果替换队列；随机模式下保存原始顺序并洗牌。
func (m *Model) loadQueue(queue []store.Song, idx int) {
	m.shuffleBase = nil
	m.queue = make([]store.Song, len(queue))
	for i := range queue {
		m.queue[i] = normalizeSong(queue[i])
	}
	if len(m.queue) == 0 {
		m.queueIdx = 0
		m.queueCursor = 0
		m.invalidatePrefetch()
		return
	}
	idx = clampInt(idx, 0, len(m.queue)-1)
	m.queueIdx = idx
	m.queueCursor = idx
	targetID := m.queue[idx].ID
	if m.mode == modeShuffle {
		m.shuffleBase = append([]store.Song(nil), m.queue...)
		m.shuffleBaseIdx = idx
		m.shuffleQueueKeeping(targetID)
	}
	m.invalidatePrefetch()
}

// advance 按播放模式前进到下一首；顺序模式优先使用预解析结果。
func (m *Model) advance() tea.Cmd {
	if len(m.queue) == 0 {
		return nil
	}
	if m.mode == modeLoopOne && m.current != nil {
		return m.playSong(*m.current)
	}
	next := m.queueIdx + 1
	if next >= len(m.queue) {
		next = 0
	}
	m.queueIdx = next
	m.queueCursor = next
	if m.mode == modeSequence {
		if s, ok := m.takePrefetch(m.queue[next]); ok {
			m.retryCount = 0
			m.playRequest++ // 作废仍在路上的旧解析请求
			m.awaitingStream = false
			m.state = audio.StateBuffering
			return m.startStream(m.queue[next], s, 0)
		}
	}
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
	m.invalidatePrefetch()
	return m.playSong(m.queue[prev])
}

// togglePlayback 统一处理「起播 / 暂停 / 继续」。恢复出来的会话在第一次 space 时才起播。
func (m *Model) togglePlayback() tea.Cmd {
	if m.engine == nil {
		return nil
	}
	if m.awaitingStream {
		m.setNotice("正在缓冲，请稍候…", false)
		return nil
	}
	switch m.engine.State() {
	case audio.StatePlaying:
		m.engine.Pause()
		m.state = audio.StatePaused
		m.persistQueue()
		return nil
	case audio.StatePaused:
		s := m.engine.CurrentStream()
		if s.URL != "" && m.current != nil && !s.ExpiresAt.IsZero() && time.Now().After(s.ExpiresAt) {
			m.state = audio.StateBuffering
			m.setNotice("播放地址已过期，正在重新解析…", false)
			return m.playSongCmdAt(*m.current, m.position, 0)
		}
		if s.URL != "" {
			m.engine.Resume()
			m.state = audio.StatePlaying
			m.persistQueue()
			return nil
		}
	case audio.StateEnded:
		if m.current != nil {
			m.position = 0
			m.resumeAt = 0
			m.state = audio.StateBuffering
			return m.playSongCmdAt(*m.current, 0, 0)
		}
	}
	if m.current == nil {
		return nil
	}
	at := m.resumeAt
	if at <= 0 {
		at = m.position
	}
	m.state = audio.StateBuffering
	return m.playSongCmdAt(*m.current, at, 0)
}

// seekBy 跳转；地址临近过期时顺手续签，避免 seek 触发 403。
func (m *Model) seekBy(delta time.Duration) tea.Cmd {
	if m.current == nil {
		return nil
	}
	if m.awaitingStream {
		m.setNotice("正在缓冲，暂时不能跳转", false)
		return nil
	}
	target := m.position + delta
	if m.engine != nil && m.engine.State() != audio.StateIdle && m.engine.CurrentStream().URL != "" {
		target = m.engine.Position() + delta
	}
	if target < 0 {
		target = 0
	}
	if m.duration > 0 && target > m.duration {
		target = m.duration
	}
	// 片段编辑时临时切到整首预览，光标必须能走到旧区间之外；
	// 保存或取消后再按新区间/原区间恢复限制。
	if m.clip == nil {
		if clip := m.activeClip(); clip.Active() {
			start := clipStart(clip)
			end := clipEnd(clip, m.duration)
			clamped := false
			if target < start {
				target = start
				clamped = true
			}
			if end > 0 && target >= end {
				target = end - time.Millisecond
				if target < start {
					target = start
				}
				clamped = true
			}
			if clamped {
				m.setNotice("已限制在当前片段内", false)
			}
		}
	} else if handled, cmd := m.previewForSeek(target); handled {
		return cmd
	}
	m.position = target

	// 还没起播（恢复会话）时只移动进度，space 会从这里开始。
	if m.engine == nil || m.engine.State() == audio.StateIdle || m.engine.CurrentStream().URL == "" {
		m.resumeAt = target
		m.persistQueue()
		return nil
	}
	stream := m.engine.CurrentStream()
	if !stream.ExpiresAt.IsZero() && time.Until(stream.ExpiresAt) < 5*time.Minute {
		m.state = audio.StateBuffering
		m.setNotice("播放地址即将过期，正在续签…", false)
		return m.playSongCmdAt(*m.current, target, 0)
	}
	if err := m.engine.Seek(m.ctx, target); err != nil {
		m.state = audio.StateBuffering
		m.setNotice("跳转失败，正在重新解析…", true)
		return m.playSongCmdAt(*m.current, target, 0)
	}
	m.resumeAt = 0
	m.persistQueue()
	return nil
}

func (m *Model) changeVolume(delta float64) {
	m.volume = clamp01(m.volume + delta)
	m.muted = false
	if m.engine != nil {
		m.engine.SetVolume(m.volume)
	}
	if m.store != nil {
		_ = m.store.SetSetting("volume", strconv.FormatFloat(m.volume, 'f', 2, 64))
	}
	m.setNotice(fmt.Sprintf("音量 %d%%", int(m.volume*100)), false)
}

func (m *Model) toggleMute() {
	m.muted = !m.muted
	if m.engine != nil {
		if m.muted {
			m.engine.SetVolume(0)
		} else {
			m.engine.SetVolume(m.volume)
		}
	}
	if m.muted {
		m.setNotice("已静音 · M 恢复", false)
	} else {
		m.setNotice(fmt.Sprintf("取消静音 · 音量 %d%%", int(m.volume*100)), false)
	}
}

func (m *Model) cycleQuality() {
	m.quality = source.Quality((int(m.quality) + 1) % 3)
	if m.store != nil {
		_ = m.store.SetSetting("quality", strconv.Itoa(int(m.quality)))
	}
	m.invalidatePrefetch()
	m.setNotice("音质偏好："+m.qualityPrefLabel()+"（下一首生效）", false)
}

func (m *Model) cycleSleep() {
	slots := []time.Duration{0, 15 * time.Minute, 30 * time.Minute, 60 * time.Minute, 120 * time.Minute}
	m.sleepSlot = (m.sleepSlot + 1) % len(slots)
	d := slots[m.sleepSlot]
	if d == 0 {
		m.sleepUntil = time.Time{}
		m.setNotice("睡眠定时已关闭", false)
		return
	}
	m.sleepUntil = time.Now().Add(d)
	m.setNotice(fmt.Sprintf("睡眠定时：%d 分钟后暂停", int(d.Minutes())), false)
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
	return m.selectPlaylist(m.playlistIdx + delta)
}

func (m *Model) selectPlaylist(idx int) tea.Cmd {
	if len(m.playlists) == 0 {
		return nil
	}
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
	return m.openAddSongPicker(m.songs[m.songIdx])
}

func (m *Model) addSearchResultToPlaylist(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.searchResults) {
		return nil
	}
	return m.openAddSongPicker(m.searchResultSong(idx))
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
	return m.enqueueSong(m.searchResultSong(idx))
}

func (m *Model) enqueueSong(song store.Song) tea.Cmd {
	song = m.ensureStored(song)
	m.queue = append(m.queue, song)
	if len(m.queue) == 1 && m.current == nil {
		m.queueIdx = 0
	}
	m.queueCursor = len(m.queue) - 1
	m.invalidatePrefetch()
	m.persistQueue()
	m.setNotice("已加入队列："+song.Title, false)
	return nil
}

// insertNext 把一首歌插到当前曲目之后；已存在时会先移除旧位置，避免重复。
func (m *Model) insertNext(song store.Song) tea.Cmd {
	song = m.ensureStored(song)
	if m.current != nil && song.ID == m.current.ID {
		m.setNotice("这首歌正在播放", false)
		return nil
	}
	insertAt := 0
	if m.current != nil {
		insertAt = m.queueIdx + 1
	}
	if insertAt > len(m.queue) {
		insertAt = len(m.queue)
	}
	for i := 0; i < len(m.queue); i++ {
		if m.queue[i].ID != song.ID {
			continue
		}
		if i < insertAt {
			insertAt--
		}
		if i < m.queueIdx {
			m.queueIdx--
		}
		m.queue = append(m.queue[:i], m.queue[i+1:]...)
		break
	}
	if insertAt < 0 {
		insertAt = 0
	}
	if insertAt > len(m.queue) {
		insertAt = len(m.queue)
	}
	m.queue = append(m.queue, store.Song{})
	copy(m.queue[insertAt+1:], m.queue[insertAt:])
	m.queue[insertAt] = song
	m.queueCursor = insertAt
	if m.queueIdx < 0 {
		m.queueIdx = 0
	}
	if m.queueIdx >= len(m.queue) {
		m.queueIdx = len(m.queue) - 1
	}
	m.invalidatePrefetch()
	m.persistQueue()
	m.setNotice("下一首播放："+song.Title, false)
	return nil
}

func (m *Model) moveQueueItem(delta int) tea.Cmd {
	i := m.queueCursor
	j := i + delta
	if i < 0 || i >= len(m.queue) || j < 0 || j >= len(m.queue) {
		return nil
	}
	m.queue[i], m.queue[j] = m.queue[j], m.queue[i]
	if m.queueIdx == i {
		m.queueIdx = j
	} else if m.queueIdx == j {
		m.queueIdx = i
	}
	m.queueCursor = j
	m.invalidatePrefetch()
	m.persistQueue()
	return nil
}

// playQueueItemNext 把队列里选中的条目移动到当前曲目之后。
func (m *Model) playQueueItemNext() tea.Cmd {
	cursor := m.queueCursor
	if cursor < 0 || cursor >= len(m.queue) {
		return nil
	}
	if m.current != nil && cursor == m.queueIdx {
		m.setNotice("这首歌正在播放", false)
		return nil
	}
	item := m.queue[cursor]
	m.queue = append(m.queue[:cursor], m.queue[cursor+1:]...)

	insertAt := 0
	if m.current != nil {
		if cursor < m.queueIdx {
			m.queueIdx--
		}
		insertAt = m.queueIdx + 1
	}
	if insertAt > len(m.queue) {
		insertAt = len(m.queue)
	}
	m.queue = append(m.queue, store.Song{})
	copy(m.queue[insertAt+1:], m.queue[insertAt:])
	m.queue[insertAt] = item
	m.queueCursor = insertAt
	if m.queueIdx >= len(m.queue) {
		m.queueIdx = max(0, len(m.queue)-1)
	}
	m.invalidatePrefetch()
	m.persistQueue()
	m.setNotice("下一首播放："+item.Title, false)
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
	song := normalizeSong(m.songs[m.songIdx])
	if err := m.store.RemoveSongFromPlaylist(p.ID, song.ID); err != nil {
		m.setError(err)
		return nil
	}
	switch p.Kind {
	case "remote":
		m.setNotice("已从本地缓存移除（刷新同步会恢复）："+song.Title, false)
	case "mixin":
		m.setNotice("已在本歌单隐藏（源歌单不变）："+song.Title, false)
	default:
		m.setNotice("已从歌单移除："+song.Title, false)
	}
	return m.loadSongsCmd(false)
}

func (m *Model) removeQueueItem() tea.Cmd {
	if m.queueCursor < 0 || m.queueCursor >= len(m.queue) {
		return nil
	}
	cursor := m.queueCursor
	removed := m.queue[cursor]
	removingCurrent := cursor == m.queueIdx
	m.queue = append(m.queue[:cursor], m.queue[cursor+1:]...)
	m.invalidatePrefetch()

	if removingCurrent {
		if len(m.queue) == 0 {
			if m.engine != nil {
				m.engine.Stop()
			}
			m.current = nil
			m.state = audio.StateIdle
			m.position = 0
			m.duration = 0
			m.resumeAt = 0
			m.lyricBase = nil
			m.lyricLines = nil
			m.lyricIdx = -1
			m.lyricSource = ""
			m.lyricLoaded = false
			m.queueIdx = 0
			m.queueCursor = 0
			m.setPlayingPlaylist("")
			m.persistQueue()
			m.setNotice("已移除当前曲目，队列已空", false)
			return nil
		}
		idx := min(cursor, len(m.queue)-1)
		m.queueIdx = idx
		m.queueCursor = idx
		m.persistQueue()
		m.setNotice("已移除当前曲目，播放下一首", false)
		return m.playSong(m.queue[idx])
	}

	if cursor < m.queueIdx {
		m.queueIdx--
	}
	if m.queueIdx >= len(m.queue) {
		m.queueIdx = max(0, len(m.queue)-1)
	}
	if m.queueCursor >= len(m.queue) {
		m.queueCursor = max(0, len(m.queue)-1)
	}
	m.persistQueue()
	m.setNotice("已从队列移除："+removed.Title, false)
	return nil
}

// clearQueue 清空待播队列，但保留正在播放/暂停的当前曲目，避免「清空即静音」的意外。
func (m *Model) clearQueue() {
	if m.current != nil {
		m.queue = []store.Song{*m.current}
		m.queueIdx = 0
	} else {
		m.queue = nil
		m.queueIdx = 0
	}
	m.queueCursor = 0
	m.invalidatePrefetch()
	m.persistQueue()
	if m.current != nil {
		m.setNotice("已清空队列（保留当前曲目）", false)
	} else {
		m.setNotice("已清空队列", false)
	}
}

func (m *Model) takePrefetch(song store.Song) (source.Stream, bool) {
	song = normalizeSong(song)
	if m.prefetchStream == nil || m.prefetchID != song.ID {
		return source.Stream{}, false
	}
	s := *m.prefetchStream
	expired := !s.ExpiresAt.IsZero() && time.Until(s.ExpiresAt) < 2*time.Minute
	m.invalidatePrefetch()
	if expired {
		return source.Stream{}, false
	}
	return s, true
}

func (m *Model) invalidatePrefetch() {
	m.prefetchID = ""
	m.prefetchStream = nil
}

func (m *Model) nextQueueSongID() string {
	if m.mode != modeSequence || len(m.queue) < 2 {
		return ""
	}
	next := m.queueIdx + 1
	if next >= len(m.queue) {
		next = 0
	}
	if next == m.queueIdx {
		return ""
	}
	return normalizeSong(m.queue[next]).ID
}

// prefetchNextCmd 提前解析下一首，缩短切歌时「已缓冲」的等待。
func (m *Model) prefetchNextCmd() tea.Cmd {
	if m.engine == nil || m.sources == nil || m.store == nil || m.mode != modeSequence || len(m.queue) < 2 {
		return nil
	}
	next := m.queueIdx + 1
	if next >= len(m.queue) {
		next = 0
	}
	if next == m.queueIdx {
		return nil
	}
	song := normalizeSong(m.queue[next])
	if m.prefetchID == song.ID && m.prefetchStream != nil {
		if m.prefetchStream.ExpiresAt.IsZero() || time.Until(m.prefetchStream.ExpiresAt) > 2*time.Minute {
			return nil
		}
	}
	sources, st, quality := m.sources, m.store, m.quality
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		resolved, stream, err := resolveSong(ctx, sources, st, song, quality)
		return prefetchMsg{songID: song.ID, song: resolved, stream: stream, err: err}
	}
}

func (m *Model) persistQueue() {
	if m.store == nil {
		return
	}
	ids := make([]string, 0, len(m.queue))
	for _, s := range m.queue {
		s = normalizeSong(s)
		ids = append(ids, s.ID)
	}
	position := int64(m.position / time.Millisecond)
	if m.position < 0 {
		position = 0
	}
	_ = m.store.SaveQueue(store.QueueState{
		IDs:      ids,
		Index:    m.queueIdx,
		Position: position,
	})
	m.lastPersist = time.Now()
}

func (m *Model) needsPersist() bool {
	return m.current != nil && (m.state == audio.StatePlaying || m.state == audio.StatePaused || m.resumeAt > 0)
}

// ---- 歌单/队列确认输入 ----

func (m *Model) beginPrompt(kind promptKind) {
	m.prompt = kind
	m.promptInput = ""
	if kind == promptRenamePlaylist {
		if p, ok := m.currentPlaylist(); ok {
			m.promptInput = p.Title
		}
	}
}

func (m *Model) handlePromptKey(key tea.Key) (tea.Model, tea.Cmd) {
	if isCtrl(key, 'c') {
		m.shutdown()
		return m, tea.Quit
	}
	switch key.Code {
	case tea.KeyEscape:
		m.prompt = promptNone
		m.promptInput = ""
		m.pendingAddSong = nil
		return m, nil
	case tea.KeyEnter:
		return m, m.commitPrompt()
	case tea.KeyBackspace:
		if r := []rune(m.promptInput); len(r) > 0 {
			m.promptInput = string(r[:len(r)-1])
		}
		return m, nil
	}
	if isCtrl(key, 'u') {
		m.promptInput = ""
		return m, nil
	}
	if key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) == 0 {
		m.promptInput += key.Text
	}
	return m, nil
}

func (m *Model) commitPrompt() tea.Cmd {
	kind := m.prompt
	input := strings.TrimSpace(m.promptInput)
	m.prompt = promptNone
	m.promptInput = ""
	if m.store == nil {
		return nil
	}
	switch kind {
	case promptNewPlaylist:
		if input == "" {
			m.setNotice("歌单名不能为空", true)
			return nil
		}
		p, err := m.store.CreatePlaylist(input)
		if err != nil {
			m.setError(err)
			return nil
		}
		m.pendingPlaylistID = p.ID
		if m.pendingAddSong != nil {
			if err := m.store.AddSongToPlaylist(p.ID, *m.pendingAddSong); err != nil {
				m.pendingAddSong = nil
				m.setError(err)
				return nil
			}
			m.pendingAddSong = nil
			m.setNotice("已新建歌单并加入："+p.Title, false)
		} else {
			m.setNotice("已新建歌单："+p.Title, false)
		}
		return m.loadLocalPlaylistsCmd()
	case promptRenamePlaylist:
		p, ok := m.currentPlaylist()
		if !ok {
			return nil
		}
		if input == "" {
			m.setNotice("歌单名不能为空", true)
			return nil
		}
		if err := m.store.RenamePlaylist(p.ID, input); err != nil {
			m.setError(err)
			return nil
		}
		m.pendingPlaylistID = p.ID
		m.setNotice("已重命名歌单："+input, false)
		return m.loadLocalPlaylistsCmd()
	case promptDeletePlaylist:
		p, ok := m.currentPlaylist()
		if !ok {
			return nil
		}
		if err := m.store.DeletePlaylist(p.ID); err != nil {
			m.setError(err)
			return nil
		}
		m.pendingPlaylistID = ""
		m.setNotice("已删除歌单："+p.Title, false)
		return m.loadLocalPlaylistsCmd()
	case promptClearQueue:
		m.clearQueue()
		return nil
	case promptUnlockPlaylist:
		p, ok := m.currentPlaylist()
		if !ok {
			return nil
		}
		unlocked, err := m.store.UnlockPlaylist(p.ID)
		if err != nil {
			m.setError(err)
			return nil
		}
		m.pendingPlaylistID = unlocked.ID
		m.setNotice("已解锁为本地歌单："+unlocked.Title, false)
		return m.loadLocalPlaylistsCmd()
	}
	return nil
}

// ---- 歌词偏移 ----

func (m *Model) shiftLyrics(deltaMS int) tea.Cmd {
	if len(m.lyricBase) == 0 {
		m.setNotice("当前没有歌词可调整", true)
		return nil
	}
	offset := m.lyricOffsetMS + deltaMS
	if offset < -10000 {
		offset = -10000
	}
	if offset > 10000 {
		offset = 10000
	}
	m.lyricOffsetMS = offset
	m.lyricLines = lyrics.Clone(m.lyricBase, offset)
	m.lyricIdx = lyrics.ActiveIndex(m.lyricLines, int(m.position/time.Millisecond))
	if m.current != nil && m.store != nil {
		_ = m.store.SetSetting("lyric_offset:"+m.current.ID, strconv.Itoa(offset))
	}
	m.setNotice(fmt.Sprintf("歌词偏移 %+.1f 秒", float64(offset)/1000), false)
	return nil
}

func (m *Model) resetLyricOffset() tea.Cmd {
	if m.lyricOffsetMS == 0 {
		return nil
	}
	return m.shiftLyrics(-m.lyricOffsetMS)
}

// buildQRArt 生成终端二维码与图片路径，不修改 Model，便于按音源缓存。
func buildQRArt(rawURL string, srcID source.ID) (ascii, compact, path string) {
	ascii = renderQR(rawURL)
	path = writeQRCodePNG(rawURL, srcID)
	if compactURL := stripTracking(rawURL); compactURL != rawURL {
		compact = renderQR(compactURL)
	}
	return ascii, compact, path
}

// setQRCode 同时生成完整与精简两份二维码，供不同终端高度使用。
func (m *Model) setQRCode(rawURL string) {
	m.qrASCII, m.qrASCIICompact, m.qrPath = buildQRArt(rawURL, m.loginSource)
}

func (m *Model) applyQREntry(srcID source.ID, entry *qrCacheEntry) {
	m.loginSource = srcID
	m.qrSource = srcID
	m.qr = entry.qr
	m.qrASCII = entry.ascii
	m.qrASCIICompact = entry.asciiCompact
	m.qrPath = entry.path
	m.qrStatus = entry.status
}

func (m *Model) ensureLoginQR() tea.Cmd {
	if m.sources == nil {
		m.setNotice("没有可用的登录源", true)
		return nil
	}
	src := m.sources.Get(m.loginSource)
	if src == nil {
		m.setNotice("未知登录源", true)
		return nil
	}
	if _, ok := m.userFor(m.loginSource); ok {
		if m.qrCache != nil {
			delete(m.qrCache, m.loginSource)
		}
		m.qrSource = m.loginSource
		m.qr = nil
		m.qrASCII = ""
		m.qrASCIICompact = ""
		m.qrPath = ""
		m.qrStatus = "已登录"
		if _, known := m.users[m.loginSource]; !known {
			return m.loadUsersCmd()
		}
		return nil
	}
	if m.qrCache == nil {
		m.qrCache = map[source.ID]*qrCacheEntry{}
	}
	entry := m.qrCache[m.loginSource]
	if entry != nil && entry.qr != nil && time.Now().Before(entry.expiresAt) {
		same := m.qrSource == m.loginSource && m.qr != nil
		m.applyQREntry(m.loginSource, entry)
		if same {
			return nil
		}
		return pollLoginCmd(src, entry.qr.Key)
	}
	if entry != nil && entry.fetching {
		m.qrSource = m.loginSource
		m.qr = nil
		m.qrASCII = ""
		m.qrASCIICompact = ""
		m.qrPath = ""
		m.qrStatus = "正在获取二维码…"
		return nil
	}
	if entry == nil {
		entry = &qrCacheEntry{}
		m.qrCache[m.loginSource] = entry
	}
	entry.fetching = true
	entry.status = "正在获取二维码…"
	m.qrSource = m.loginSource
	m.qr = nil
	m.qrASCII = ""
	m.qrASCIICompact = ""
	m.qrPath = ""
	m.qrStatus = entry.status
	return m.startLoginCmd(m.loginSource)
}

// refreshLoginQR 强制丢弃当前源的缓存并重新申请。
func (m *Model) refreshLoginQR() tea.Cmd {
	if m.qrCache != nil {
		if entry := m.qrCache[m.loginSource]; entry != nil {
			entry.expiresAt = time.Time{}
			entry.fetching = false
		}
	}
	m.qr = nil
	m.qrSource = ""
	return m.ensureLoginQR()
}

// qualityLabel 返回当前音轨档位。
func (m *Model) qualityLabel() string {
	if m.awaitingStream {
		return "解析中"
	}
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

// qualityPrefLabel 返回音质偏好档位。
func (m *Model) qualityPrefLabel() string {
	return m.quality.Label()
}

// promptLine 返回底部输入/确认提示。
func (m *Model) promptLine() string {
	switch m.prompt {
	case promptNewPlaylist:
		return "新建歌单：" + m.promptInput + "▏  enter 确认 · esc 取消"
	case promptRenamePlaylist:
		return "重命名歌单：" + m.promptInput + "▏  enter 确认 · esc 取消"
	case promptDeletePlaylist:
		if p, ok := m.currentPlaylist(); ok {
			return "删除歌单「" + p.Title + "」及其条目？enter 确认 · esc 取消"
		}
		return "删除歌单？enter 确认 · esc 取消"
	case promptClearQueue:
		return "清空播放队列？当前曲目会保留 · enter 确认 · esc 取消"
	case promptUnlockPlaylist:
		if p, ok := m.currentPlaylist(); ok {
			return "解锁「" + p.Title + "」为本地歌单？保留曲目并停止同步 · enter 确认 · esc 取消"
		}
		return "解锁为本地歌单？enter 确认 · esc 取消"
	}
	return ""
}

// userFor 返回某音源当前登录用户；优先使用已加载的账号信息，必要时回退到 Source。
func (m *Model) userFor(srcID source.ID) (source.User, bool) {
	if m.users != nil {
		if u, ok := m.users[srcID]; ok && u.LoggedIn {
			return u, true
		}
	}
	if m.sources != nil {
		if src := m.sources.Get(srcID); src != nil && src.LoggedIn() {
			return src.User(), true
		}
	}
	return source.User{}, false
}

func (m *Model) accountSummary() string {
	ids := []source.ID{source.Bilibili, source.NetEase}
	if m.sources != nil {
		if got := m.sources.IDs(); len(got) > 0 {
			ids = got
		}
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		name := id.Label()
		var src source.Source
		if m.sources != nil {
			src = m.sources.Get(id)
			if src != nil {
				name = src.Name()
			}
		}
		u, known := m.users[id]
		logged := u.LoggedIn
		if !known && src != nil {
			logged = src.LoggedIn()
			u = src.User()
		}
		if !logged {
			parts = append(parts, name+" 未登录")
			continue
		}
		who := u.Name
		if who == "" {
			who = "已登录"
		}
		parts = append(parts, name+" "+who)
	}
	return strings.Join(parts, " · ")
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

func (m *Model) setPlayingPlaylist(id string) {
	if m.playingPlaylistID == id {
		return
	}
	m.playingPlaylistID = id
	if m.store != nil {
		_ = m.store.SetSetting("playing_playlist", id)
	}
}

// logout 清除指定音源的 cookie、用户信息与远端歌单镜像。
func (m *Model) logout(srcID source.ID) tea.Cmd {
	src := m.sources.Get(srcID)
	if src != nil {
		src.Logout()
	}
	if m.store != nil {
		_ = m.store.DeleteAccount(string(srcID))
		_ = m.store.DeleteRemotePlaylistsBySource(string(srcID))
	}
	if m.users != nil {
		delete(m.users, srcID)
	}
	if m.qrCache != nil {
		delete(m.qrCache, srcID)
	}
	m.qr = nil
	m.qrSource = ""
	m.qrASCII = ""
	m.qrASCIICompact = ""
	m.qrPath = ""
	m.qrStatus = ""
	m.pendingPlaylistID = "local-default"
	m.playlistIdx = 0
	m.songs = nil
	m.songIdx = 0
	label := "已退出登录，本地歌单与队列保留"
	if src != nil {
		label = src.Name() + " 已退出登录，本地歌单与队列保留"
	}
	m.setNotice(label, false)
	return tea.Batch(m.loadUsersCmd(), m.loadLocalPlaylistsCmd(), m.ensureLoginQR())
}

// switchLoginSource 在登录页按上下键/ s 切换音源；优先复用缓存，没有才拉取。
func (m *Model) switchLoginSource(delta int) tea.Cmd {
	if m.sources == nil {
		return nil
	}
	ids := m.sources.IDs()
	if len(ids) == 0 {
		return nil
	}
	idx := 0
	for i, id := range ids {
		if id == m.loginSource {
			idx = i
			break
		}
	}
	n := len(ids)
	m.loginSource = ids[((idx+delta)%n+n)%n]
	return m.ensureLoginQR()
}

func loginAppName(id source.ID) string {
	switch id {
	case source.Bilibili:
		return "B 站 App"
	case source.NetEase:
		return "网易云音乐 App"
	default:
		return "手机 App"
	}
}

func (m *Model) shutdown() {
	m.cancelDownloads()
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
