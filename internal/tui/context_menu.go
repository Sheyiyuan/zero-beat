package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

func (m *Model) selectHit(zone hitZone) tea.Cmd {
	switch zone.target {
	case hitLibraryPlaylist:
		m.libraryFocus = focusPlaylists
		return m.selectPlaylist(zone.index)
	case hitLibrarySong:
		m.libraryFocus = focusSongs
		m.songIdx = zone.index
		m.resetMarquee()
	case hitQueueSong:
		m.queueCursor = zone.index
	case hitSearchResult:
		m.searchIdx = zone.index
	case hitDownloadRow:
		m.downloadIdx = zone.index
	case hitFolderRow:
		m.folderIdx = zone.index
	}
	return nil
}

func (m *Model) songAt(zone hitZone) (store.Song, view, bool) {
	switch zone.target {
	case hitLibrarySong:
		if zone.index >= 0 && zone.index < len(m.songs) {
			return m.songs[zone.index], viewLibrary, true
		}
	case hitQueueSong:
		if zone.index >= 0 && zone.index < len(m.queue) {
			return m.queue[zone.index], viewQueue, true
		}
	case hitSearchResult:
		if m.searchMode == searchSource {
			if zone.index >= 0 && zone.index < len(m.searchResults) {
				return m.searchResultSong(zone.index), viewSearch, true
			}
		} else if zone.index >= 0 && zone.index < len(m.localResults) {
			return m.localResults[zone.index], viewSearch, true
		}
	case hitDownloadRow:
		if zone.index >= 0 && zone.index < len(m.downloadItems) {
			return m.downloadItems[zone.index].entry.Song, viewDownloads, true
		}
	}
	return store.Song{}, 0, false
}

func (m *Model) openSongMenu(zone hitZone, x, y int) (tea.Model, tea.Cmd) {
	song, origin, ok := m.songAt(zone)
	if !ok {
		return m, nil
	}
	cmd := m.selectHit(zone)
	items := []menuItem{
		{label: "播放", hint: "enter", act: actPlay},
		{label: "下一首播放", hint: m.firstKey(keymap.PlayNext), act: actPlayNext},
		{label: "加入队列末尾", hint: m.firstKey(keymap.Enqueue), act: actEnqueue},
		{label: "加入歌单", hint: m.firstKey(keymap.AddPlaylist), act: actAddPlaylist},
		{label: "下载", hint: m.firstKey(keymap.DownloadSelected), act: actDownload},
	}
	switch zone.target {
	case hitQueueSong:
		items = append(items, menuItem{label: "从队列移除", hint: m.firstKey(keymap.Remove), act: actRemove})
	case hitLibrarySong:
		items = append(items, menuItem{label: "从歌单移除", hint: m.firstKey(keymap.Remove), act: actRemove})
	}
	menu := newMenu(menuSong, "歌曲 · "+song.Title, items, x, y, m.width)
	menu.song = song
	menu.songIdx = zone.index
	menu.origin = origin
	m.menu = menu
	return m, cmd
}

func (m *Model) openPlaylistMenu(zone hitZone, x, y int) (tea.Model, tea.Cmd) {
	if zone.index < 0 || zone.index >= len(m.playlists) {
		return m, nil
	}
	p := m.playlists[zone.index]
	cmd := m.selectHit(zone)
	items := []menuItem{
		{label: "播放歌单", hint: "enter", act: actPlaylistPlay},
		{label: "下载整个歌单", hint: m.firstKey(keymap.DownloadSelected), act: actPlaylistDownload},
	}
	switch p.Kind {
	case "mixin":
		items = append(items,
			menuItem{label: "编辑引用", hint: m.firstKey(keymap.MixEdit), act: actPlaylistMix},
			menuItem{label: "重命名", hint: m.firstKey(keymap.Rename), act: actPlaylistRename},
			menuItem{label: "删除歌单", hint: m.firstKey(keymap.Delete), act: actPlaylistDelete},
		)
	case "local":
		items = append(items,
			menuItem{label: "重命名", hint: m.firstKey(keymap.Rename), act: actPlaylistRename},
			menuItem{label: "删除歌单", hint: m.firstKey(keymap.Delete), act: actPlaylistDelete},
		)
	case "remote":
		if !app.IsDailyPlaylist(p) {
			items = append(items, menuItem{label: "解锁为本地歌单", hint: m.firstKey(keymap.Unlock), act: actPlaylistUnlock})
		}
	}
	menu := newMenu(menuPlaylist, "歌单 · "+p.Title, items, x, y, m.width)
	menu.playlist = p
	menu.playlistIdx = zone.index
	m.menu = menu
	return m, cmd
}

func (m *Model) openFolderMenu(zone hitZone, x, y int) (tea.Model, tea.Cmd) {
	if zone.index < 0 || zone.index >= len(m.folderItems) {
		return m, nil
	}
	p := m.folderItems[zone.index]
	cmd := m.selectHit(zone)
	items := []menuItem{
		{label: "添加这个歌单", hint: m.firstKey(keymap.PlaySelected), act: actFolderAdd},
	}
	menu := newMenu(menuPlaylist, "导入 · "+p.Title, items, x, y, m.width)
	menu.playlist = store.Playlist{ID: p.ID, Title: p.Title, Count: p.Count, Source: string(p.Source)}
	menu.playlistIdx = zone.index
	m.menu = menu
	return m, cmd
}

func (m *Model) openLyricsMenu(line int, x, y int) {
	items := []menuItem{
		{label: "播放 / 暂停", hint: m.firstKey(keymap.PlayPause), act: actPlayPause},
		{label: "上一首", hint: m.firstKey(keymap.Previous), act: actPrevious},
		{label: "下一首", hint: m.firstKey(keymap.Next), act: actNext},
	}
	if line >= 0 && line < len(m.lyricLines) {
		items = append(items, menuItem{label: "跳到这句", hint: "enter / 双击", act: actLyricSeek})
	}
	items = append(items,
		menuItem{label: "下载当前歌曲", hint: m.firstKey(keymap.Download), act: actDownload},
		menuItem{label: "加入歌单", hint: m.firstKey(keymap.AddPlaylist), act: actAddPlaylist},
		menuItem{label: "编辑播放片段", hint: m.firstKey(keymap.EditClip), act: actClip},
	)
	if m.lyricOffsetMS != 0 {
		items = append(items, menuItem{label: "歌词偏移归零", hint: m.firstKey(keymap.OffsetReset), act: actOffsetReset})
	}
	menu := newMenu(menuLyrics, "歌词", items, x, y, m.width)
	menu.lyricLine = line
	m.menu = menu
}
