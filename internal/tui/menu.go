package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

type menuKind int

const (
	menuPage menuKind = iota
	menuSong
	menuPlaylist
	menuLyrics
)

type menuAct int

const (
	actPage menuAct = iota
	actPlay
	actPlayNext
	actEnqueue
	actAddPlaylist
	actDownload
	actRemove
	actPlaylistPlay
	actPlaylistDownload
	actPlaylistRename
	actPlaylistMix
	actPlaylistUnlock
	actPlaylistDelete
	actPlayPause
	actPrevious
	actNext
	actLyricSeek
	actClip
	actOffsetReset
	actFolderAdd
)

type menuItem struct {
	label    string
	hint     string
	act      menuAct
	index    int
	current  bool
	disabled bool
}

type menuState struct {
	kind   menuKind
	title  string
	items  []menuItem
	idx    int
	x, y   int
	width  int
	height int

	song        store.Song
	songIdx     int
	origin      view
	playlist    store.Playlist
	playlistIdx int
	lyricLine   int
}

func newMenu(kind menuKind, title string, items []menuItem, x, y, maxWidth int) *menuState {
	inner := 20
	maxLabel, maxHint := 0, 0
	for _, item := range items {
		if w := visWidth(item.label); w > maxLabel {
			maxLabel = w
		}
		if w := visWidth(item.hint); w > maxHint {
			maxHint = w
		}
	}
	if want := maxLabel + maxHint + 2; want > inner {
		inner = want
	}
	if inner+2 > maxWidth {
		inner = max(8, maxWidth-2)
	}
	return &menuState{
		kind:   kind,
		title:  title,
		items:  items,
		x:      x,
		y:      y,
		width:  inner + 2,
		height: len(items) + 2,
	}
}

func (m *Model) menuLayout() (x, y, width, height int, ok bool) {
	if m.menu == nil {
		return 0, 0, 0, 0, false
	}
	width, height = m.menu.width, m.menu.height
	x = clampInt(m.menu.x, 0, max(0, m.width-width))
	y = clampInt(m.menu.y, 0, max(0, m.height-height))
	return x, y, width, height, true
}

func (m *Model) menuLines() []string {
	_, _, width, _, ok := m.menuLayout()
	if !ok {
		return nil
	}
	m.menu.idx = clampInt(m.menu.idx, 0, max(0, len(m.menu.items)-1))
	inner := width - 2
	out := []string{boxTop(truncate(m.menu.title, inner), width)}
	for i, item := range m.menu.items {
		content := menuContent(item, inner)
		if i == m.menu.idx {
			out = append(out, boxLine(st.selected.Render(padRight(content, inner-1)), inner))
			continue
		}
		if item.current {
			content = st.accent.Render(content)
		} else if item.disabled {
			content = st.dim.Render(content)
		}
		out = append(out, boxLine(content, inner))
	}
	out = append(out, boxBottom(width))
	return out
}

func menuContent(item menuItem, inner int) string {
	contentW := inner - 1
	hintW := visWidth(item.hint)
	labelW := contentW - hintW - 1
	if labelW < 4 {
		labelW = 4
	}
	label := truncate(item.label, labelW)
	gap := contentW - visWidth(label) - hintW
	if gap < 1 {
		gap = 1
	}
	return label + strings.Repeat(" ", gap) + item.hint
}

func (m *Model) overlayMenu(frame []string) []string {
	x, y, _, _, ok := m.menuLayout()
	if !ok {
		return frame
	}
	return overlayBoxAt(frame, m.menuLines(), x, y, m.width, m.height)
}

func (m *Model) handleMenuKey(key tea.Key) (tea.Model, tea.Cmd) {
	if m.menu == nil {
		return m, nil
	}
	switch {
	case key.Code == tea.KeyEscape || m.match(keymap.Back, key):
		m.menu = nil
		return m, nil
	case m.match(keymap.Up, key):
		m.menu.idx = clampInt(m.menu.idx-1, 0, max(0, len(m.menu.items)-1))
		return m, nil
	case m.match(keymap.Down, key):
		m.menu.idx = clampInt(m.menu.idx+1, 0, max(0, len(m.menu.items)-1))
		return m, nil
	case key.Code == tea.KeyEnter || key.Code == tea.KeySpace:
		return m.applyMenu()
	}
	if m.menu.kind == menuPage && len(key.Text) == 1 {
		if n := int(key.Text[0] - '1'); n >= 0 && n < len(m.menu.items) {
			m.menu.idx = n
			return m.applyMenu()
		}
	}
	return m, nil
}

func (m *Model) handleMenuClick(mouse tea.Mouse) (tea.Model, tea.Cmd) {
	x, y, width, height, ok := m.menuLayout()
	if !ok {
		return m, nil
	}
	if mouse.Button == tea.MouseLeft &&
		mouse.X >= x && mouse.X < x+width &&
		mouse.Y > y && mouse.Y < y+height-1 {
		idx := mouse.Y - y - 1
		if idx >= 0 && idx < len(m.menu.items) {
			m.menu.idx = idx
			return m.applyMenu()
		}
	}
	if mouse.Button != tea.MouseWheelUp && mouse.Button != tea.MouseWheelDown {
		m.menu = nil
	}
	return m, nil
}

func (m *Model) applyMenu() (tea.Model, tea.Cmd) {
	if m.menu == nil || len(m.menu.items) == 0 {
		return m, nil
	}
	menu := *m.menu
	menu.idx = clampInt(menu.idx, 0, len(menu.items)-1)
	item := menu.items[menu.idx]
	if item.disabled {
		return m, nil
	}
	m.menu = nil
	if item.act == actPage {
		return m.openPage(item.index)
	}
	return m.runMenuAction(menu, item)
}

func (m *Model) runMenuAction(menu menuState, item menuItem) (tea.Model, tea.Cmd) {
	switch item.act {
	case actPlay:
		switch menu.origin {
		case viewQueue:
			return m, m.playQueueIndex(menu.songIdx)
		case viewSearch:
			if m.searchMode == searchSource {
				return m, m.playSearchResult(menu.songIdx)
			}
			return m, m.playLocalResult(menu.songIdx)
		case viewDownloads:
			return m, m.playSong(menu.song)
		default:
			m.songIdx = menu.songIdx
			return m, m.playPlaylistIndex(menu.songIdx)
		}
	case actPlayNext:
		return m, m.insertNext(menu.song)
	case actEnqueue:
		return m, m.enqueueSong(menu.song)
	case actAddPlaylist:
		if menu.kind == menuLyrics {
			if m.current != nil {
				return m, m.openAddSongPicker(*m.current)
			}
			return m, nil
		}
		return m, m.openAddSongPicker(menu.song)
	case actDownload:
		if menu.kind == menuLyrics {
			return m, m.startCurrentDownload()
		}
		return m, m.enqueueDownload(menu.song, "开始下载：")
	case actRemove:
		switch menu.origin {
		case viewQueue:
			m.queueCursor = menu.songIdx
			return m, m.removeQueueItem()
		case viewLibrary:
			m.songIdx = menu.songIdx
			return m, m.removeSelected()
		}
		return m, nil
	case actPlaylistPlay:
		return m, m.playPlaylistFromMenu(menu.playlistIdx)
	case actPlaylistDownload:
		m.libraryFocus = focusPlaylists
		m.playlistIdx = menu.playlistIdx
		return m, m.startPlaylistDownload()
	case actPlaylistRename:
		m.libraryFocus = focusPlaylists
		m.playlistIdx = menu.playlistIdx
		if menu.playlist.Kind == "local" || menu.playlist.Kind == "mixin" {
			m.beginPrompt(promptRenamePlaylist)
		} else {
			m.setNotice("只有本地或混合歌单可以重命名", true)
		}
		return m, nil
	case actPlaylistMix:
		return m, m.openMixEditor(menu.playlist.ID)
	case actPlaylistUnlock:
		m.libraryFocus = focusPlaylists
		m.playlistIdx = menu.playlistIdx
		if menu.playlist.Kind == "remote" && !app.IsDailyPlaylist(menu.playlist) {
			m.beginPrompt(promptUnlockPlaylist)
		} else {
			m.setNotice("只有普通同步歌单可以解锁", true)
		}
		return m, nil
	case actPlaylistDelete:
		m.libraryFocus = focusPlaylists
		m.playlistIdx = menu.playlistIdx
		if menu.playlist.Kind == "local" || menu.playlist.Kind == "mixin" {
			m.beginPrompt(promptDeletePlaylist)
		} else {
			m.setNotice("远端同步歌单不能在这里删除", true)
		}
		return m, nil
	case actPlayPause:
		return m, m.togglePlayback()
	case actPrevious:
		return m, m.rewind()
	case actNext:
		return m, m.advance()
	case actClip:
		return m, m.openClipEditor()
	case actLyricSeek:
		if menu.lyricLine >= 0 && menu.lyricLine < len(m.lyricLines) {
			return m, m.seekTo(time.Duration(m.lyricLines[menu.lyricLine].StartMS) * time.Millisecond)
		}
		return m, nil
	case actOffsetReset:
		return m, m.resetLyricOffset()
	case actFolderAdd:
		m.folderIdx = menu.playlistIdx
		return m, m.addSelectedFolder()
	}
	return m, nil
}
