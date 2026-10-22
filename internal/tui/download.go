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
	// 已下载且文件有效时直接跳过，避免无谓的重新解析与「已存在」提示。
	if !job.Force {
		if _, ok := m.service().DownloadedPath(song.ID); ok {
			m.setNotice("已下载，跳过："+title, false)
			return nil
		}
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
	added, skipped, downloaded := 0, 0, 0
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
		if _, ok := m.service().DownloadedPath(song.ID); ok {
			downloaded++
			continue
		}
		if _, ok := mgr.Add(song, title, downloader.Job{Quality: int(m.quality)}); ok {
			added++
		} else {
			skipped++
		}
	}
	if added == 0 {
		if downloaded > 0 {
			m.setNotice(fmt.Sprintf("这个歌单的 %d 首都已下载，无需重复下载", downloaded), false)
		} else {
			m.setNotice("没有可新增的下载任务（已在队列或缺少音源 ID）", false)
		}
		return nil
	}
	mgr.Start()
	msg := fmt.Sprintf("已加入歌单下载：%d 首", added)
	if downloaded > 0 || skipped > 0 {
		var reasons []string
		if downloaded > 0 {
			reasons = append(reasons, fmt.Sprintf("%d 首已下载", downloaded))
		}
		if skipped > 0 {
			reasons = append(reasons, fmt.Sprintf("%d 首已在队列或缺少音源 ID", skipped))
		}
		msg += "（跳过 " + strings.Join(reasons, "、") + "）"
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
	// 失败任务保留在 Manager 里（底部状态行会持续提示），其余情况移除。
	if msg.ev.Err != nil {
		switch {
		case errors.Is(msg.ev.Err, download.ErrExists):
			mgr.Remove(msg.ev.ID)
			m.setNotice("已存在，跳过："+msg.ev.Path, false)
		case errors.Is(msg.ev.Err, context.Canceled):
			mgr.Remove(msg.ev.ID)
			m.setNotice("已取消下载", false)
		default:
			m.setError(msg.ev.Err)
		}
	} else {
		mgr.Remove(msg.ev.ID)
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

// downloadLineText 渲染底部下载进度行；失败任务优先提示（常驻直到打开下载页）。
func (m *Model) downloadLineText() (string, bool) {
	if m.downloads == nil {
		return "", false
	}
	tasks := m.downloads.Tasks()
	if len(tasks) == 0 || m.view == viewHelp || m.view == viewLogin {
		return "", false
	}
	running, queued, failed := 0, 0, 0
	var primary *downloader.Task
	var lastFailed *downloader.Task
	for i := range tasks {
		task := &tasks[i]
		switch task.State {
		case downloader.Failed:
			failed++
			lastFailed = task
		case downloader.Queued:
			queued++
		default:
			running++
			if primary == nil {
				primary = task
			}
		}
	}
	if running == 0 && queued == 0 && lastFailed != nil {
		name := truncate(lastFailed.Title, max(8, m.width/3))
		text := st.err.Render(" ✗ ") + st.text.Render(name) +
			st.err.Render(fmt.Sprintf("  下载失败 · 共 %d 个", failed))
		if m.view != viewDownloads {
			text += st.muted.Render(" · F 查看")
		}
		return text, true
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
	if failed > 0 {
		text += st.err.Render(fmt.Sprintf("  · %d 个失败", failed))
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
