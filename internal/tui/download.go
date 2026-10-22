package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/download"
	"github.com/sheyiyuan/zero-beat/internal/downloader"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

const maxConcurrentDownloads = 2

type downloadEventMsg struct{ ev downloader.Event }

// downloadManager 懒加载下载管理器；测试不启动真实下载时也能构造 Model。
func (m *Model) downloadManager() *downloader.Manager {
	if m.downloads == nil {
		m.downloads = downloader.New(maxConcurrentDownloads, func(ctx context.Context, song store.Song, job downloader.Job, progress func(done, total int64)) (string, int64, error) {
			// job 是入队时的快照：后台协程不读 Model 字段（quality 会变）。
			res := m.service().DownloadSong(ctx, song, source.Quality(job.Quality), job.Force, progress)
			return res.Path, res.Size, res.Err
		})
	}
	return m.downloads
}

func waitDownloadEventCmd(mgr *downloader.Manager) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-mgr.Events()
		if !ok {
			return nil
		}
		return downloadEventMsg{ev: ev}
	}
}

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
	job := downloader.Job{Quality: int(m.quality)}
	// 下载管理页对「文件缺失」的行强制重下，否则会被 ErrExists 拦住。
	if m.view == viewDownloads {
		if item, ok := m.selectedDownload(); ok && item.missing {
			job.Force = true
		}
	}
	return m.enqueueDownloadJob(song, "开始下载：", job)
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
	case viewDownloads:
		if item, ok := m.selectedDownload(); ok && item.entry.Song.ID != "" {
			return item.entry.Song, true
		}
	}
	return store.Song{}, false
}

func (m *Model) enqueueDownload(raw store.Song, verb string) tea.Cmd {
	return m.enqueueDownloadJob(raw, verb, downloader.Job{Quality: int(m.quality)})
}

func (m *Model) enqueueDownloadJob(raw store.Song, verb string, job downloader.Job) tea.Cmd {
	song := store.NormalizeSong(raw)
	if song.ExternalID() == "" {
		m.setNotice("这首缺少音源 ID，无法下载", true)
		return nil
	}
	mgr := m.downloadManager()
	if mgr.Has(song.ID) {
		m.setNotice("已在下载队列："+song.Title, false)
		return nil
	}
	title := strings.TrimSpace(song.Title)
	if title == "" {
		title = song.ExternalID()
	}
	id, added := mgr.Add(song, title, job)
	if !added {
		m.setNotice("已在下载队列："+title, false)
		return nil
	}
	mgr.Start()
	running := false
	for _, task := range mgr.Tasks() {
		if task.ID == id && task.State == downloader.Running {
			running = true
			break
		}
	}
	if running {
		m.setNotice(verb+title, false)
	} else {
		m.setNotice("已加入下载队列："+title, false)
	}
	return waitDownloadEventCmd(mgr)
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
	mgr := m.downloadManager()
	added, skipped := 0, 0
	for _, raw := range m.songs {
		song := store.NormalizeSong(raw)
		title := strings.TrimSpace(song.Title)
		if title == "" {
			title = song.ExternalID()
		}
		if song.ExternalID() == "" {
			skipped++
			continue
		}
		if _, ok := mgr.Add(song, title, downloader.Job{Quality: int(m.quality)}); ok {
			added++
		} else {
			skipped++
		}
	}
	if added == 0 {
		m.setNotice("没有可新增的下载任务（已在队列或缺少音源 ID）", false)
		return nil
	}
	mgr.Start()
	msg := fmt.Sprintf("已加入歌单下载：%d 首", added)
	if skipped > 0 {
		msg += fmt.Sprintf("（跳过 %d 首）", skipped)
	}
	m.setNotice(msg, false)
	return waitDownloadEventCmd(mgr)
}

func (m *Model) handleDownloadEvent(msg downloadEventMsg) (tea.Model, tea.Cmd) {
	mgr := m.downloadManager()
	if !msg.ev.Finished {
		mgr.SetProgress(msg.ev.ID, msg.ev.Done, msg.ev.Total)
		return m, waitDownloadEventCmd(mgr)
	}
	mgr.Remove(msg.ev.ID)
	if msg.ev.Err != nil {
		if errors.Is(msg.ev.Err, download.ErrExists) {
			m.setNotice("已存在，跳过："+msg.ev.Path, false)
		} else {
			m.setError(msg.ev.Err)
		}
	} else {
		m.setNotice("已下载："+msg.ev.Path, false)
	}
	wait := waitDownloadEventCmd(mgr)
	if m.view == viewDownloads {
		m.downloadsBusy = true
		return m, tea.Batch(wait, m.loadDownloadsCmd())
	}
	return m, wait
}

func (m *Model) cancelDownloads() {
	if m.downloads != nil {
		m.downloads.CancelAll()
	}
}

// downloadLineText 渲染底部下载进度行；多个任务时只显示第一个运行中的任务。
func (m *Model) downloadLineText() (string, bool) {
	if m.downloads == nil {
		return "", false
	}
	tasks := m.downloads.Tasks()
	if len(tasks) == 0 || m.view == viewHelp || m.view == viewLogin {
		return "", false
	}
	running, queued := 0, 0
	var primary *downloader.Task
	for i := range tasks {
		task := &tasks[i]
		if task.State == downloader.Running {
			running++
			if primary == nil {
				primary = task
			}
		} else {
			queued++
		}
	}
	if primary == nil {
		primary = &tasks[0]
	}

	limit := m.width / 3
	if limit < 8 {
		limit = 8
	}
	name := truncate(primary.Title, limit)
	text := st.accent.Render(" ↓ ") + st.text.Render(name)
	switch {
	case primary.State == downloader.Queued:
		text += st.muted.Render("  等待下载")
	case primary.Total > 0:
		pct := int(float64(primary.Done) * 100 / float64(primary.Total))
		if pct > 100 {
			pct = 100
		}
		if m.width < 60 {
			text += st.muted.Render(fmt.Sprintf("  %d%%", pct))
		} else {
			text += st.muted.Render(fmt.Sprintf("  %d%% · %s / %s", pct,
				download.HumanBytes(primary.Done), download.HumanBytes(primary.Total)))
		}
	case primary.Done > 0:
		text += st.muted.Render("  " + download.HumanBytes(primary.Done))
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
