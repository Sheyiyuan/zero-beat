package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/download"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

const maxConcurrentDownloads = 2

type downloadState int

const (
	downloadQueued downloadState = iota
	downloadRunning
)

type downloadTask struct {
	id     int
	song   store.Song
	title  string
	state  downloadState
	done   int64
	total  int64
	ch     chan downloadMsg
	cancel context.CancelFunc
}

type downloadMsg struct {
	id       int
	done     int64
	total    int64
	path     string
	size     int64
	err      error
	finished bool
}

type downloadClosedMsg struct{ id int }

func (m *Model) startCurrentDownload() tea.Cmd {
	if m.current == nil {
		m.setNotice("没有正在播放的歌曲", true)
		return nil
	}
	return m.enqueueDownload(*m.current, "开始下载正在播放：")
}

func (m *Model) startSelectedDownload() tea.Cmd {
	if m.view == viewLibrary && m.libraryFocus == focusPlaylists {
		return m.startPlaylistDownload()
	}
	song, ok := m.selectedDownloadSong()
	if !ok {
		m.setNotice("当前页面没有选中项；按 w 下载正在播放", true)
		return nil
	}
	return m.enqueueDownload(song, "开始下载：")
}

func (m *Model) selectedDownloadSong() (store.Song, bool) {
	switch m.view {
	case viewLibrary:
		if m.libraryFocus == focusPlaylists {
			return store.Song{}, false
		}
		if m.songIdx >= 0 && m.songIdx < len(m.songs) {
			return m.songs[m.songIdx], true
		}
	case viewSearch:
		if m.searchMode == searchSource {
			if m.searchIdx >= 0 && m.searchIdx < len(m.searchResults) {
				return m.searchResultSong(m.searchIdx), true
			}
		} else if m.searchIdx >= 0 && m.searchIdx < len(m.localResults) {
			return m.localResults[m.searchIdx], true
		}
	case viewQueue:
		if m.queueCursor >= 0 && m.queueCursor < len(m.queue) {
			return m.queue[m.queueCursor], true
		}
	}
	return store.Song{}, false
}

func (m *Model) enqueueDownload(raw store.Song, verb string) tea.Cmd {
	song := store.NormalizeSong(raw)
	if song.ExternalID() == "" {
		m.setNotice("这首缺少音源 ID，无法下载", true)
		return nil
	}
	for _, task := range m.downloads {
		if task.song.ID == song.ID {
			m.setNotice("已在下载队列："+task.title, false)
			return nil
		}
	}

	title := strings.TrimSpace(song.Title)
	if title == "" {
		title = song.ExternalID()
	}
	m.downloadSeq++
	task := &downloadTask{
		id:    m.downloadSeq,
		song:  song,
		title: title,
		state: downloadQueued,
	}
	m.downloads = append(m.downloads, task)
	cmd := m.startQueuedDownloads()
	if task.state == downloadRunning {
		m.setNotice(verb+title, false)
	} else {
		m.setNotice("已加入下载队列："+title, false)
	}
	return cmd
}

func (m *Model) startPlaylistDownload() tea.Cmd {
	if m.songsBusy {
		m.setNotice("歌单载入中，稍后再试", false)
		return nil
	}
	if len(m.songs) == 0 {
		m.setNotice("这个歌单还是空的", false)
		return nil
	}
	existing := make(map[string]bool, len(m.downloads))
	for _, task := range m.downloads {
		existing[task.song.ID] = true
	}

	added, skipped := 0, 0
	for _, raw := range m.songs {
		song := store.NormalizeSong(raw)
		if song.ExternalID() == "" || existing[song.ID] {
			skipped++
			continue
		}
		existing[song.ID] = true
		title := strings.TrimSpace(song.Title)
		if title == "" {
			title = song.ExternalID()
		}
		m.downloadSeq++
		m.downloads = append(m.downloads, &downloadTask{
			id:    m.downloadSeq,
			song:  song,
			title: title,
			state: downloadQueued,
		})
		added++
	}
	if added == 0 {
		m.setNotice("没有可新增的下载任务（已在队列或缺少音源 ID）", false)
		return nil
	}
	cmd := m.startQueuedDownloads()
	msg := fmt.Sprintf("已加入歌单下载：%d 首", added)
	if skipped > 0 {
		msg += fmt.Sprintf("（跳过 %d 首）", skipped)
	}
	m.setNotice(msg, false)
	return cmd
}

func (m *Model) startQueuedDownloads() tea.Cmd {
	running := 0
	for _, task := range m.downloads {
		if task.state == downloadRunning {
			running++
		}
	}
	var cmds []tea.Cmd
	for _, task := range m.downloads {
		if running >= maxConcurrentDownloads {
			break
		}
		if task.state != downloadQueued {
			continue
		}
		task.state = downloadRunning
		ch := make(chan downloadMsg, 16)
		task.ch = ch
		ctx, cancel := context.WithCancel(context.Background())
		task.cancel = cancel
		cmds = append(cmds, m.downloadStartCmd(ctx, task.id, task.song, ch))
		running++
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *Model) downloadStartCmd(ctx context.Context, id int, song store.Song, ch chan downloadMsg) tea.Cmd {
	sources := m.sources
	quality := m.quality
	return func() tea.Msg {
		go runTUIDownload(ctx, id, song, sources, quality, ch)
		msg, ok := <-ch
		if !ok {
			return downloadClosedMsg{id: id}
		}
		return msg
	}
}

func runTUIDownload(ctx context.Context, id int, song store.Song, sources *source.Registry, quality source.Quality, ch chan downloadMsg) {
	defer close(ch)

	last := time.Time{}
	onProgress := func(done, total int64) {
		now := time.Now()
		if !last.IsZero() && now.Sub(last) < 200*time.Millisecond {
			return
		}
		last = now
		select {
		case ch <- downloadMsg{id: id, done: done, total: total}:
		default:
		}
	}

	res := app.New(sources, nil, nil).DownloadSong(ctx, song, quality, onProgress)
	if res.Err != nil {
		ch <- downloadMsg{id: id, finished: true, path: res.Path, err: res.Err}
		return
	}
	ch <- downloadMsg{id: id, finished: true, path: res.Path, size: res.Size}
}

func waitDownloadMsg(id int, ch <-chan downloadMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return downloadClosedMsg{id: id}
		}
		return msg
	}
}

func (m *Model) handleDownloadMsg(msg downloadMsg) (tea.Model, tea.Cmd) {
	task := m.downloadByID(msg.id)
	if task == nil {
		return m, nil
	}
	if !msg.finished {
		task.done = msg.done
		task.total = msg.total
		return m, waitDownloadMsg(task.id, task.ch)
	}

	path := msg.path
	m.removeDownloadTask(task.id)
	if msg.err != nil {
		if errors.Is(msg.err, download.ErrExists) {
			m.setNotice("已存在，跳过："+path, false)
		} else {
			m.setError(msg.err)
		}
	} else {
		m.setNotice("已下载："+path, false)
	}
	return m, m.startQueuedDownloads()
}

func (m *Model) handleDownloadClosedMsg(msg downloadClosedMsg) (tea.Model, tea.Cmd) {
	m.removeDownloadTask(msg.id)
	m.setNotice("下载任务中断", true)
	return m, m.startQueuedDownloads()
}

func (m *Model) downloadByID(id int) *downloadTask {
	for _, task := range m.downloads {
		if task.id == id {
			return task
		}
	}
	return nil
}

func (m *Model) cancelDownloads() {
	for _, task := range m.downloads {
		if task.cancel != nil {
			task.cancel()
		}
	}
}

func (m *Model) removeDownloadTask(id int) {
	for i, task := range m.downloads {
		if task.id == id {
			m.downloads = append(m.downloads[:i], m.downloads[i+1:]...)
			return
		}
	}
}

// downloadLineText 渲染底部下载进度行；多个任务时只显示第一个运行中的任务。
func (m *Model) downloadLineText() (string, bool) {
	if len(m.downloads) == 0 || m.view == viewHelp || m.view == viewLogin {
		return "", false
	}
	running, queued := 0, 0
	var primary *downloadTask
	for _, task := range m.downloads {
		if task.state == downloadRunning {
			running++
			if primary == nil {
				primary = task
			}
		} else {
			queued++
		}
	}
	if primary == nil {
		primary = m.downloads[0]
	}

	limit := m.width / 3
	if limit < 8 {
		limit = 8
	}
	name := truncate(primary.title, limit)
	text := st.accent.Render(" ↓ ") + st.text.Render(name)
	switch {
	case primary.state == downloadQueued:
		text += st.muted.Render("  等待下载")
	case primary.total > 0:
		pct := int(float64(primary.done) * 100 / float64(primary.total))
		if pct > 100 {
			pct = 100
		}
		if m.width < 60 {
			text += st.muted.Render(fmt.Sprintf("  %d%%", pct))
		} else {
			text += st.muted.Render(fmt.Sprintf("  %d%% · %s / %s", pct,
				download.HumanBytes(primary.done), download.HumanBytes(primary.total)))
		}
	case primary.done > 0:
		text += st.muted.Render("  " + download.HumanBytes(primary.done))
	default:
		text += st.muted.Render("  准备中…")
	}
	if extra := running + queued - 1; extra > 0 {
		if m.width >= 50 {
			text += st.muted.Render(fmt.Sprintf("  · 另 %d 个", extra))
		} else {
			text += st.muted.Render(fmt.Sprintf(" +%d", extra))
		}
	}
	return text, true
}
