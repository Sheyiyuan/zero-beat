package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/download"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/player"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// downloadItem 是下载管理页的一行；missing 表示索引里有记录但文件已失效。
type downloadItem struct {
	entry   store.DownloadEntry
	missing bool
}

func (m *Model) openDownloads() tea.Cmd {
	m.view = viewDownloads
	m.downloadIdx = 0
	m.downloadsBusy = true
	m.resetMarquee()
	return m.loadDownloadsCmd()
}

func (m *Model) loadDownloadsCmd() tea.Cmd {
	svc := m.service()
	return func() tea.Msg {
		entries, err := svc.Downloads()
		if err != nil {
			return downloadsMsg{err: err}
		}
		items := make([]downloadItem, 0, len(entries))
		for _, entry := range entries {
			missing := true
			if info, serr := os.Stat(entry.Download.Path); serr == nil && !info.IsDir() && info.Size() > 0 {
				missing = false
			}
			items = append(items, downloadItem{entry: entry, missing: missing})
		}
		return downloadsMsg{items: items}
	}
}

func (m *Model) selectedDownload() (downloadItem, bool) {
	if m.downloadIdx < 0 || m.downloadIdx >= len(m.downloadItems) {
		return downloadItem{}, false
	}
	return m.downloadItems[m.downloadIdx], true
}

func (m *Model) handleDownloadsKey(key tea.Key) (tea.Model, tea.Cmd) {
	switch {
	case m.match(keymap.Up, key):
		if m.downloadIdx > 0 {
			m.downloadIdx--
		}
		return m, nil
	case m.match(keymap.Down, key):
		if m.downloadIdx < len(m.downloadItems)-1 {
			m.downloadIdx++
		}
		return m, nil
	case key.Code == tea.KeyEnter || m.match(keymap.PlaySelected, key):
		return m, m.playDownloadIndex(m.downloadIdx)
	case m.match(keymap.Enqueue, key):
		if item, ok := m.selectedDownload(); ok && item.entry.Song.ID != "" {
			return m, m.enqueueSong(item.entry.Song)
		}
		return m, nil
	case m.match(keymap.AddPlaylist, key):
		if item, ok := m.selectedDownload(); ok && item.entry.Song.ID != "" {
			return m, m.openAddSongPicker(item.entry.Song)
		}
		return m, nil
	case m.match(keymap.Remove, key):
		item, ok := m.selectedDownload()
		if !ok {
			return m, nil
		}
		m.pendingDownloadID = item.entry.Download.SongID
		m.prompt = promptRemoveDownload
		return m, nil
	case m.match(keymap.Delete, key):
		item, ok := m.selectedDownload()
		if !ok {
			return m, nil
		}
		m.pendingDownloadID = item.entry.Download.SongID
		m.prompt = promptDeleteDownload
		return m, nil
	case m.match(keymap.Refresh, key):
		m.downloadsBusy = true
		return m, m.loadDownloadsCmd()
	case m.match(keymap.LoginImage, key):
		item, ok := m.selectedDownload()
		if !ok {
			return m, nil
		}
		if err := openLocalFile(filepath.Dir(item.entry.Download.Path)); err != nil {
			m.setError(err)
		}
		return m, nil
	}
	return m.handleCommonPlaybackKey(key)
}

func (m *Model) playDownloadIndex(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.downloadItems) {
		return nil
	}
	queue := make([]store.Song, 0, len(m.downloadItems))
	for _, item := range m.downloadItems {
		queue = append(queue, store.NormalizeSong(item.entry.Song))
	}
	song := queue[idx]
	if song.ID == "" {
		m.setNotice("这条下载记录缺少歌曲元数据，无法播放", true)
		return nil
	}
	m.setPlayingPlaylist("")
	m.loadQueue(queue, idx)
	return m.playLocalSongCmdAt(song, 0, 0)
}

func (m *Model) removeDownloadRecord(id string) tea.Cmd {
	if id == "" {
		return nil
	}
	if err := m.service().DeleteDownload(id); err != nil {
		m.setError(err)
		return nil
	}
	m.setNotice("已移除下载记录（文件保留）", false)
	m.downloadsBusy = true
	return m.loadDownloadsCmd()
}

func (m *Model) deleteDownloadFile(id string) tea.Cmd {
	item, ok := m.downloadByID(id)
	if !ok {
		return m.removeDownloadRecord(id)
	}
	if m.current != nil && m.current.ID == id {
		m.withController(func(c *player.Controller) { c.Stop() })
	}
	if err := os.Remove(item.entry.Download.Path); err != nil && !os.IsNotExist(err) {
		m.setError(err)
		return nil
	}
	if err := m.service().DeleteDownload(id); err != nil {
		m.setError(err)
		return nil
	}
	m.setNotice("已删除本地文件", false)
	m.downloadsBusy = true
	return m.loadDownloadsCmd()
}

func (m *Model) downloadByID(id string) (downloadItem, bool) {
	for _, item := range m.downloadItems {
		if item.entry.Download.SongID == id {
			return item, true
		}
	}
	return downloadItem{}, false
}

func humanSize(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	return download.HumanBytes(n)
}

func downloadTitle(item downloadItem) string {
	title := strings.TrimSpace(item.entry.Song.Title)
	if title == "" {
		title = item.entry.Download.SongID
	}
	return title
}
