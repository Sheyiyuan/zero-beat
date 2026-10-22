package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const doubleClickWindow = 450 * time.Millisecond

// handleMouseClick 把鼠标点击映射到已有的快捷键动作。
// 菜单与浮层优先，避免点击穿透到后台界面。
func (m *Model) handleMouseClick(mouse tea.Mouse) (tea.Model, tea.Cmd) {
	if m.menu != nil {
		return m.handleMenuClick(mouse)
	}
	if handled, cmd := m.handleOverlayClick(mouse); handled {
		return m, cmd
	}
	if m.hardModal() {
		return m, nil
	}
	zone, hit := m.hitAt(mouse.X, mouse.Y)
	// 只有可双击的行（歌曲行、歌词行）保留上一次点击用于双击判定。
	if !hit || (!isSongRow(zone.target) && zone.target != hitLyricRow) {
		m.lastClickAt = time.Time{}
	}

	if mouse.Button == tea.MouseRight {
		if m.searchEditing {
			return m, nil
		}
		return m.handleRightClick(mouse, zone, hit)
	}
	if mouse.Button != tea.MouseLeft {
		return m, nil
	}

	// 页头区域即使在搜索输入中也保持可用；点页头先退出输入态，
	// 避免「弹窗盖着输入框、按键却还进搜索框」的层级错乱。
	if hit {
		switch zone.target {
		case hitAbout:
			m.searchEditing = false
			m.aboutPopup = true
			return m, nil
		case hitPageMenu:
			m.searchEditing = false
			m.openPageMenu(mouse.X, mouse.Y+1)
			return m, nil
		case hitAccount:
			m.searchEditing = false
			m.view = viewLogin
			return m, m.onViewChanged()
		}
	}

	if m.searchEditing {
		if !hit {
			m.searchEditing = false
			return m, nil
		}
		m.searchEditing = false
	}
	if !hit {
		return m, nil
	}

	switch zone.target {
	case hitPlayPause:
		return m, m.togglePlayback()
	case hitMode:
		m.setMode((m.mode + 1) % 3)
		return m, nil
	case hitLyricStrip:
		m.view = viewLyrics
		return m, m.onViewChanged()
	case hitLyricRow:
		// 单击只把焦点移到该行（不跳转）；双击同一屏幕位置才提交跳转。
		// 单击后视口会重新居中，所以双击只比对屏幕坐标与时间，不比对行号——
		// 第二次点击命中的行已经变了，要提交的是第一次点中的焦点行。
		if m.isDoubleClickPos(mouse) {
			return m, m.commitLyricFocus()
		}
		m.focusLyricLine(zone.index)
		return m, nil
	case hitLibrarySong, hitQueueSong, hitSearchResult, hitDownloadRow:
		cmd := m.selectHit(zone)
		if m.isDoubleClick(mouse, zone) {
			return m, tea.Batch(cmd, m.playZone(zone))
		}
		return m, cmd
	}
	return m, m.selectHit(zone)
}

func isSongRow(target hitTarget) bool {
	switch target {
	case hitLibrarySong, hitQueueSong, hitSearchResult, hitDownloadRow:
		return true
	}
	return false
}

// isDoubleClickPos 只按屏幕位置 + 时间判定双击（用于会重新居中的歌词页）。
func (m *Model) isDoubleClickPos(mouse tea.Mouse) bool {
	now := time.Now()
	matched := !m.lastClickAt.IsZero() &&
		now.Sub(m.lastClickAt) <= doubleClickWindow &&
		mouse.X == m.lastClickX && mouse.Y == m.lastClickY
	m.lastClickAt = now
	m.lastClickX = mouse.X
	m.lastClickY = mouse.Y
	m.lastClickTarget = hitLyricRow
	m.lastClickIndex = 0
	return matched
}

func (m *Model) isDoubleClick(mouse tea.Mouse, zone hitZone) bool {
	now := time.Now()
	matched := !m.lastClickAt.IsZero() &&
		now.Sub(m.lastClickAt) <= doubleClickWindow &&
		mouse.X == m.lastClickX && mouse.Y == m.lastClickY &&
		m.lastClickTarget == zone.target && m.lastClickIndex == zone.index
	m.lastClickAt = now
	m.lastClickX = mouse.X
	m.lastClickY = mouse.Y
	m.lastClickTarget = zone.target
	m.lastClickIndex = zone.index
	return matched
}

func (m *Model) playZone(zone hitZone) tea.Cmd {
	switch zone.target {
	case hitLibrarySong:
		m.libraryFocus = focusSongs
		m.songIdx = zone.index
		return m.playPlaylistIndex(zone.index)
	case hitQueueSong:
		m.queueCursor = zone.index
		return m.playQueueIndex(zone.index)
	case hitSearchResult:
		m.searchIdx = zone.index
		if m.searchMode == searchSource {
			return m.playSearchResult(zone.index)
		}
		return m.playLocalResult(zone.index)
	case hitDownloadRow:
		m.downloadIdx = zone.index
		return m.playDownloadIndex(zone.index)
	}
	return nil
}

func (m *Model) handleRightClick(mouse tea.Mouse, zone hitZone, hit bool) (tea.Model, tea.Cmd) {
	if !hit {
		return m, nil
	}
	switch zone.target {
	case hitLibraryPlaylist:
		return m.openPlaylistMenu(zone, mouse.X, mouse.Y)
	case hitFolderRow:
		return m.openFolderMenu(zone, mouse.X, mouse.Y)
	case hitLibrarySong, hitQueueSong, hitSearchResult, hitDownloadRow:
		return m.openSongMenu(zone, mouse.X, mouse.Y)
	case hitLyricRow:
		m.openLyricsMenu(zone.index, mouse.X, mouse.Y)
		return m, nil
	}
	return m, nil
}

// handleMouseWheel 在播放状态行上滚动调节音量；其他位置不拦截滚轮。
func (m *Model) handleMouseWheel(mouse tea.Mouse) (tea.Model, tea.Cmd) {
	if m.menu != nil {
		return m.handleMenuClick(mouse)
	}
	if m.hardModal() || m.searchEditing {
		return m, nil
	}
	dir := 0
	switch mouse.Button {
	case tea.MouseWheelUp:
		dir = -1
	case tea.MouseWheelDown:
		dir = 1
	default:
		return m, nil
	}
	if m.view == viewLyrics && mouse.Y >= 2 && mouse.Y < 2+m.bodyHeight() {
		m.moveLyricFocus(dir * 3)
		return m, nil
	}
	if m.frameFits() && mouse.Y == m.height-3 {
		m.changeVolume(float64(-dir) * 0.02)
		return m, nil
	}
	if mouse.Y < 2 || mouse.Y >= 2+m.bodyHeight() {
		return m, nil
	}
	step := dir * 3
	switch m.view {
	case viewLibrary:
		sideW := m.librarySideWidth()
		if sideW > 0 && mouse.X < sideW {
			m.libraryFocus = focusPlaylists
			return m, m.movePlaylist(step)
		}
		m.libraryFocus = focusSongs
		m.songIdx = clampInt(m.songIdx+step, 0, max(0, len(m.songs)-1))
		m.resetMarquee()
	case viewSearch:
		if m.searchBusy {
			return m, nil
		}
		m.searchIdx = clampInt(m.searchIdx+step, 0, max(0, m.currentSearchLen()-1))
	case viewQueue:
		m.queueCursor = clampInt(m.queueCursor+step, 0, max(0, len(m.queue)-1))
	case viewDownloads:
		m.downloadIdx = clampInt(m.downloadIdx+step, 0, max(0, len(m.downloadItems)-1))
	case viewFolders:
		m.folderIdx = clampInt(m.folderIdx+step, 0, max(0, len(m.folderItems)-1))
	case viewHelp:
		m.helpOffset = clampInt(m.helpOffset+step, 0, max(0, len(m.helpLines())-m.helpViewport()))
	}
	return m, nil
}

// hardModal 表示有对话框或文本输入占据交互；这类状态下除菜单外一律不响应鼠标。
func (m *Model) hardModal() bool {
	return m.palette != nil ||
		m.prompt != promptNone ||
		m.clip != nil ||
		m.picker != nil ||
		m.helpPopup ||
		m.aboutPopup ||
		m.view == viewLogin && m.menu == nil
}

func (m *Model) mouseBlocked() bool {
	return m.hardModal() || m.searchEditing
}

// handleOverlayClick 处理浮层（关于/帮助/命令面板/选择器/片段编辑器）上的点击：
// 浮层内部吞掉点击且不穿透；点在浮层外部则关闭当前最上层浮层（取消）。
func (m *Model) handleOverlayClick(mouse tea.Mouse) (bool, tea.Cmd) {
	if mouse.Button != tea.MouseLeft && mouse.Button != tea.MouseRight {
		return false, nil
	}
	// 顺序与 View() 里浮层的叠放顺序一致：后渲染的在上层。
	var lines []string
	var cancel func() tea.Cmd
	switch {
	case m.clip != nil:
		lines, cancel = m.clipLines(), m.cancelClipEditor
	case m.picker != nil:
		lines, cancel = m.targetPickerLines(), func() tea.Cmd {
			m.picker = nil
			return nil
		}
	case m.helpPopup:
		lines, cancel = m.helpPopupLines(), func() tea.Cmd {
			m.helpPopup = false
			return nil
		}
	case m.aboutPopup:
		lines, cancel = m.aboutLines(), func() tea.Cmd {
			m.aboutPopup = false
			return nil
		}
	case m.palette != nil:
		lines, cancel = m.paletteLines(), func() tea.Cmd {
			m.palette = nil
			return nil
		}
	default:
		return false, nil
	}
	left, top, w, h, ok := overlayBoxBounds(lines, m.width, m.height)
	if !ok {
		return false, nil
	}
	if mouse.X >= left && mouse.X < left+w && mouse.Y >= top && mouse.Y < top+h {
		return true, nil
	}
	return true, cancel()
}
