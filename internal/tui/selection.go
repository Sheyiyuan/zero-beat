package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/player"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// ---- 列表批量选择 ----
//
// 选择只作用于「当前歌单的歌曲列表」，用歌曲 ID 记录而不是行号：
// 异步载入、刷新、删除后行号会变，ID 不会。选择同时记下所属歌单，
// 切换歌单后旧选择自动失效，避免把 A 歌单的选中项操作到 B 歌单上。
//
// 没有独立的选择模式：标记第一首后批量操作自动生效，esc 取消选择。

// selectionScope 返回当前选择应归属的歌单 ID。
func (m *Model) selectionScope() string {
	return m.currentPlaylistID()
}

// selectionValid 判断当前选择是否属于当前歌单。map 非空但歌单已切换时视为无效。
func (m *Model) selectionValid() bool {
	return len(m.selectionIDs) > 0 && m.selPlaylistID == m.selectionScope()
}

// selectionCount 只统计当前歌单里仍存在的选中歌曲。
func (m *Model) selectionCount() int {
	if !m.selectionValid() {
		return 0
	}
	n := 0
	for _, song := range m.songs {
		if m.selectionIDs[song.ID] {
			n++
		}
	}
	return n
}

func (m *Model) hasSelection() bool {
	return m.selectionCount() > 0
}

func (m *Model) isSongSelected(song store.Song) bool {
	return m.selectionValid() && m.selectionIDs[song.ID]
}

func (m *Model) clearSelection() {
	m.selectionIDs = nil
	m.selPlaylistID = ""
	m.selAnchor = -1
}

// ensureSelection 把选择重置到当前歌单；切换歌单后第一次标记时调用。
func (m *Model) ensureSelection() {
	if m.selPlaylistID != m.selectionScope() {
		m.clearSelection()
	}
	if m.selectionIDs == nil {
		m.selectionIDs = map[string]bool{}
	}
	m.selPlaylistID = m.selectionScope()
}

// toggleSongSelection 标记/取消标记第 idx 首；idx 越界时返回 false。
func (m *Model) toggleSongSelection(idx int) bool {
	if idx < 0 || idx >= len(m.songs) {
		return false
	}
	song := m.songs[idx]
	if song.ID == "" {
		m.setNotice("这首歌缺少来源信息，无法标记", true)
		return false
	}
	m.ensureSelection()
	if m.selectionIDs[song.ID] {
		delete(m.selectionIDs, song.ID)
	} else {
		m.selectionIDs[song.ID] = true
	}
	m.selAnchor = idx
	if len(m.selectionIDs) == 0 {
		m.clearSelection()
	}
	return true
}

// selectAllSongs 全选当前歌单；已经全选时再按一次取消全选。
func (m *Model) selectAllSongs() {
	if len(m.songs) == 0 {
		m.setNotice("当前歌单没有可选择的歌曲", false)
		return
	}
	if m.selectionCount() == len(m.songs) {
		m.clearSelection()
		m.setNotice("已取消全选", false)
		return
	}
	m.clearSelection()
	m.selectionIDs = make(map[string]bool, len(m.songs))
	for _, song := range m.songs {
		if song.ID != "" {
			m.selectionIDs[song.ID] = true
		}
	}
	m.selPlaylistID = m.selectionScope()
	if len(m.selectionIDs) == 0 {
		m.clearSelection()
		m.setNotice("当前歌单没有可选择的歌曲", false)
		return
	}
	m.setNotice(fmt.Sprintf("已全选 %d 首", len(m.selectionIDs)), false)
}

// selectSongRange 把 [from, to] 区间内的歌曲加入选择（Shift+点击）。
func (m *Model) selectSongRange(from, to int) {
	if len(m.songs) == 0 {
		return
	}
	from = clampInt(from, 0, len(m.songs)-1)
	to = clampInt(to, 0, len(m.songs)-1)
	if from > to {
		from, to = to, from
	}
	m.ensureSelection()
	for i := from; i <= to; i++ {
		if m.songs[i].ID != "" {
			m.selectionIDs[m.songs[i].ID] = true
		}
	}
	m.selAnchor = from
}

// selectedSongs 按列表顺序返回选中的歌曲，供批量操作使用。
func (m *Model) selectedSongs() []store.Song {
	if !m.selectionValid() {
		return nil
	}
	out := make([]store.Song, 0, len(m.selectionIDs))
	for _, song := range m.songs {
		if m.selectionIDs[song.ID] {
			out = append(out, song)
		}
	}
	return out
}

// batchSongs 返回批量操作的作用对象：有选中项时取全部选中项，否则取光标项。
func (m *Model) batchSongs() []store.Song {
	if m.view == viewLibrary && m.libraryFocus == focusSongs {
		if songs := m.selectedSongs(); len(songs) > 0 {
			return songs
		}
	}
	if m.songIdx >= 0 && m.songIdx < len(m.songs) {
		return []store.Song{m.songs[m.songIdx]}
	}
	return nil
}

// pruneSelection 在歌单重新载入后丢掉已经不存在的歌曲。
func (m *Model) pruneSelection() {
	if len(m.selectionIDs) == 0 {
		return
	}
	if m.selPlaylistID != m.selectionScope() {
		m.clearSelection()
		return
	}
	keep := make(map[string]bool, len(m.selectionIDs))
	for _, song := range m.songs {
		if m.selectionIDs[song.ID] {
			keep[song.ID] = true
		}
	}
	if len(keep) == 0 {
		m.clearSelection()
		return
	}
	m.selectionIDs = keep
	if m.selAnchor >= len(m.songs) {
		m.selAnchor = len(m.songs) - 1
	}
}

// reportSelection 用一条提示反映当前选择数量，键盘和鼠标共用。
func (m *Model) reportSelection() {
	if n := m.selectionCount(); n > 0 {
		m.setNotice(fmt.Sprintf("已选 %d 首", n), false)
	} else {
		m.setNotice("已取消选择", false)
	}
}

// toggleSelectionAt 标记/取消标记第 idx 首并给出提示。
func (m *Model) toggleSelectionAt(idx int) {
	if !m.toggleSongSelection(idx) {
		return
	}
	m.reportSelection()
}

// handleSelectKey 处理「标记/取消标记」。
func (m *Model) handleSelectKey() tea.Cmd {
	if m.view != viewLibrary {
		return nil
	}
	if m.libraryFocus != focusSongs {
		return m.needSongFocus()
	}
	m.toggleSelectionAt(m.songIdx)
	return nil
}

// handleSelectAllKey 处理全选/取消全选。
func (m *Model) handleSelectAllKey() tea.Cmd {
	if m.view != viewLibrary {
		return nil
	}
	if m.libraryFocus != focusSongs {
		return m.needSongFocus()
	}
	m.selectAllSongs()
	return nil
}

// playSelectedSongs 播放选中项：只有一首时沿用歌单上下文从该首开始，
// 多首时用选中项替换队列并从第一首开始。
func (m *Model) playSelectedSongs() tea.Cmd {
	songs := m.selectedSongs()
	if len(songs) == 0 {
		return m.playPlaylistIndex(m.songIdx)
	}
	if len(songs) == 1 {
		for i, song := range m.songs {
			if song.ID == songs[0].ID {
				m.songIdx = i
				return m.playPlaylistIndex(i)
			}
		}
		return m.playSong(songs[0])
	}
	queue := make([]store.Song, len(songs))
	for i := range songs {
		queue[i] = store.NormalizeSong(songs[i])
	}
	// 队列来自临时选择而不是整个歌单，不再标记来源歌单。
	m.setPlayingPlaylist("")
	m.loadQueue(queue, 0)
	m.setNotice(fmt.Sprintf("播放选中：%d 首", len(queue)), false)
	return m.playSong(m.queue[m.queueIdx])
}

// enqueueSelectedSongs 把选中项按列表顺序追加到队列末尾。
func (m *Model) enqueueSelectedSongs() tea.Cmd {
	songs := m.selectedSongs()
	if len(songs) == 0 {
		return m.enqueueCurrent()
	}
	if len(songs) == 1 {
		return m.enqueueSong(songs[0])
	}
	prepared := make([]store.Song, 0, len(songs))
	for _, song := range songs {
		prepared = append(prepared, m.ensureStored(song))
	}
	m.withController(func(c *player.Controller) {
		for _, song := range prepared {
			_ = c.Enqueue(song)
		}
	})
	m.queueCursor = len(m.queue) - 1
	m.persistQueue()
	m.setNotice(fmt.Sprintf("已加入队列：%d 首", len(prepared)), false)
	return nil
}

// insertSelectedNext 把选中项按列表顺序插到当前曲目之后。
// 逆序插入，保证最终顺序与列表一致。
func (m *Model) insertSelectedNext() tea.Cmd {
	songs := m.selectedSongs()
	if len(songs) == 0 {
		if m.songIdx >= 0 && m.songIdx < len(m.songs) {
			return m.insertNext(m.songs[m.songIdx])
		}
		return nil
	}
	if len(songs) == 1 {
		return m.insertNext(songs[0])
	}
	prepared := make([]store.Song, 0, len(songs))
	for _, song := range songs {
		if m.current != nil && song.ID == m.current.ID {
			continue
		}
		prepared = append(prepared, m.ensureStored(song))
	}
	if len(prepared) == 0 {
		m.setNotice("选中的歌已经在播放", false)
		return nil
	}
	m.withController(func(c *player.Controller) {
		for i := len(prepared) - 1; i >= 0; i-- {
			m.queueCursor = c.InsertNext(prepared[i])
		}
	})
	m.persistQueue()
	m.setNotice(fmt.Sprintf("已插到下一首：%d 首", len(prepared)), false)
	return nil
}
