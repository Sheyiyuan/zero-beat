package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// clipState 是片段编辑浮层的临时状态；保存前不写库。
type clipState struct {
	songID  string
	startMS int64
	endMS   int64
	err     string

	// original 是打开编辑器前的区间；取消时恢复。
	original store.Clip
	// previewing 表示编辑器期间已经用「整首预览」替换了引擎里的旧区间。
	previewing bool
}

// openClipEditor 打开当前歌曲的片段编辑器。
func (m *Model) openClipEditor() tea.Cmd {
	if m.current == nil {
		m.setNotice("当前没有正在播放的歌曲", true)
		return nil
	}
	clip := sanitizeClip(m.loadClip(m.current.ID), m.duration)
	c := &clipState{
		songID:   m.current.ID,
		startMS:  clip.StartMS,
		endMS:    clip.EndMS,
		original: clip,
	}
	m.clip = c

	return nil
}

func (m *Model) beginFullPreview(pos time.Duration, paused bool) bool {
	if m.engine == nil || m.current == nil {
		return false
	}
	stream := m.engine.CurrentStream()
	if stream.URL == "" {
		return false
	}
	if !stream.ExpiresAt.IsZero() && time.Until(stream.ExpiresAt) <= time.Minute {
		return false // 直链即将过期，别为了预览把正在播放的流停掉
	}
	if pos < 0 {
		pos = 0
	}
	if m.duration > 0 && pos > m.duration {
		pos = m.duration
	}
	stream.ClipStartMS = 0
	stream.ClipEndMS = 0
	if err := m.engine.PlayAt(m.ctx, stream, pos); err != nil {
		m.setError(err)
		return false
	}
	m.playRequest++ // 直接复用当前流时，作废仍在路上的旧解析请求
	m.awaitingStream = false
	m.retryCount = 0
	m.resumeAt = 0
	m.position = pos
	if paused {
		m.engine.Pause()
		m.state = audio.StatePaused
	} else {
		m.state = audio.StatePlaying
	}
	return true
}

// clipAllowsPosition 判断位置是否仍在旧区间内；end 为 0 表示不限制结尾。
func clipAllowsPosition(target time.Duration, clip store.Clip, duration time.Duration) bool {
	if !clip.Active() {
		return true
	}
	start := clipStart(clip)
	if clip.EndMS == 0 {
		// 没设终点时原曲结尾也算允许；超出总时长由 seek 本身 clamp。
		return target >= start
	}
	end := time.Duration(clip.EndMS) * time.Millisecond
	return target >= start && target < end
}

// previewForSeek 在片段编辑时检查目标位置是否越出旧区间；
// 越界就切到整首预览，并把播放直接定位到目标，避免被旧 -t 截住。
func (m *Model) previewForSeek(target time.Duration) (bool, tea.Cmd) {
	if m.clip == nil || m.clip.previewing || m.current == nil {
		return false, nil
	}
	clip := m.activeClip()
	if !clip.Active() {
		return false, nil
	}
	if clipAllowsPosition(target, clip, m.duration) {
		return false, nil
	}
	if m.engine == nil {
		return false, nil
	}
	paused := m.engine.State() == audio.StatePaused
	if m.beginFullPreview(target, paused) {
		m.clip.previewing = true
		m.setActiveClip(m.current.ID, store.Clip{})
		m.persistQueue()
		return true, nil
	}
	switch m.engine.State() {
	case audio.StatePlaying, audio.StatePaused, audio.StateEnded, audio.StateFailed, audio.StateBuffering:
		m.clip.previewing = true
		m.position = target
		m.state = audio.StateBuffering
		return true, m.playSongCmdAt(*m.current, target, 0)
	}
	return false, nil
}

func (m *Model) loadClip(songID string) store.Clip {
	if m.store == nil || songID == "" {
		return store.Clip{}
	}
	clip, err := m.store.Clip(songID)
	if err != nil {
		return store.Clip{}
	}
	return clip
}

// activeClip 返回当前正在播放歌曲的片段设置；换歌后旧缓存不再生效。
func (m *Model) activeClip() store.Clip {
	if m.current == nil || m.clipSongID == "" || m.clipSongID != m.current.ID {
		return store.Clip{}
	}
	return m.clipCache
}

func (m *Model) setActiveClip(songID string, clip store.Clip) {
	m.clipSongID = songID
	m.clipCache = clip
}

func clipStart(clip store.Clip) time.Duration {
	if clip.StartMS <= 0 {
		return 0
	}
	return time.Duration(clip.StartMS) * time.Millisecond
}

func clipEnd(clip store.Clip, duration time.Duration) time.Duration {
	if clip.EndMS > 0 {
		return time.Duration(clip.EndMS) * time.Millisecond
	}
	return duration
}

// sanitizeClip 把区间夹到已知时长内；完全越界时返回零值（按整首播放）。
func sanitizeClip(clip store.Clip, duration time.Duration) store.Clip {
	if duration <= 0 {
		return clip
	}
	maxMS := int64(duration / time.Millisecond)
	if clip.StartMS >= maxMS {
		return store.Clip{}
	}
	if clip.EndMS >= maxMS {
		clip.EndMS = 0 // 贴着结尾的显式终点等价于「到原曲结尾」
	}
	if clip.EndMS > 0 && clip.EndMS <= clip.StartMS {
		return store.Clip{}
	}
	return clip
}

// clampClipStart 把起播点夹到区间内；越界时回到区间起点。
func clampClipStart(pos time.Duration, clip store.Clip, duration time.Duration) time.Duration {
	start := clipStart(clip)
	end := clipEnd(clip, duration)
	if duration > 0 && start > duration {
		start = duration
	}
	if end > 0 && start >= end {
		return pos // 数据不合法，忽略区间
	}
	if pos < start {
		pos = start
	}
	if end > 0 && pos >= end {
		pos = start
	}
	if pos < 0 {
		pos = 0
	}
	return pos
}

func (m *Model) handleClipKey(key tea.Key) (tea.Model, tea.Cmd) {
	if m.clip == nil {
		return m, nil
	}
	if isCtrl(key, 'c') {
		m.shutdown()
		return m, tea.Quit
	}
	switch {
	case key.Code == tea.KeyEscape || m.match(keymap.Back, key) || m.match(keymap.EditClip, key):
		return m, m.cancelClipEditor()
	case key.Code == tea.KeyEnter:
		return m, m.saveClip()
	case key.Code == tea.KeySpace || key.Text == " ":
		return m, m.togglePlayback()
	case m.match(keymap.ClipSetStart, key):
		m.setClipStart()
		return m, nil
	case m.match(keymap.ClipSetEnd, key):
		m.setClipEnd()
		return m, nil
	case m.match(keymap.ClipClear, key):
		m.clip.startMS = 0
		m.clip.endMS = 0
		m.clip.err = ""
		return m, nil
	case m.match(keymap.SeekBack5, key) || key.Code == tea.KeyLeft:
		return m, m.seekBy(-5 * time.Second)
	case m.match(keymap.SeekBack1, key):
		return m, m.seekBy(-1 * time.Second)
	case m.match(keymap.SeekFwd5, key) || key.Code == tea.KeyRight:
		return m, m.seekBy(5 * time.Second)
	case m.match(keymap.SeekFwd10, key):
		return m, m.seekBy(10 * time.Second)
	}
	return m, nil
}

func (m *Model) clipPosition() time.Duration {
	if m.engine != nil && m.engine.State() != audio.StateIdle && !m.awaitingStream {
		if pos := m.engine.Position(); pos > 0 {
			return pos
		}
	}
	return m.position
}

func (m *Model) setClipStart() {
	pos := m.clipPosition()
	m.position = pos
	posMS := int64(pos / time.Millisecond)
	if posMS < 0 {
		posMS = 0
	}
	if m.duration > 0 {
		maxMS := int64(m.duration / time.Millisecond)
		if posMS >= maxMS {
			m.clip.err = "起点必须在歌曲结束之前"
			return
		}
	}
	if m.clip.endMS > 0 && posMS >= m.clip.endMS {
		m.clip.err = "起点必须早于终点"
		return
	}
	m.clip.startMS = posMS
	m.clip.err = ""
}

func (m *Model) setClipEnd() {
	pos := m.clipPosition()
	m.position = pos
	posMS := int64(pos / time.Millisecond)
	if m.duration > 0 {
		maxMS := int64(m.duration / time.Millisecond)
		if posMS >= maxMS {
			// 0 表示「到原曲结尾」，和未设置终点是同一个语义。
			m.clip.endMS = 0
			m.clip.err = ""
			return
		}
	}
	if posMS <= m.clip.startMS {
		m.clip.err = "终点必须晚于起点"
		return
	}
	m.clip.endMS = posMS
	m.clip.err = ""
}

func (m *Model) cancelClipEditor() tea.Cmd {
	c := m.clip
	if c == nil {
		return nil
	}
	m.clip = nil
	if m.current == nil || m.current.ID != c.songID {
		return nil
	}
	original := sanitizeClip(c.original, m.duration)
	m.setActiveClip(c.songID, original)
	pos := clampClipStart(m.clipPosition(), original, m.duration)
	if m.engine == nil {
		m.position = pos
		if m.state == audio.StatePaused {
			m.resumeAt = pos
		}
		m.persistQueue()
		return nil
	}
	if !c.previewing {
		m.position = pos
		return nil
	}
	paused := m.engine.State() == audio.StatePaused
	return m.restartWithClip(original, pos, paused)
}

// restartWithClip 尽量复用当前直链，避免保存/取消片段时重新走一次解析。
func (m *Model) restartWithClip(clip store.Clip, pos time.Duration, paused bool) tea.Cmd {
	if m.current == nil {
		return nil
	}
	if m.engine == nil {
		m.position = pos
		if paused {
			m.resumeAt = pos
		}
		return nil
	}
	stream := m.engine.CurrentStream()
	if stream.URL != "" && (stream.ExpiresAt.IsZero() || time.Until(stream.ExpiresAt) > time.Minute) {
		stream.ClipStartMS = clip.StartMS
		stream.ClipEndMS = clip.EndMS
		if err := m.engine.PlayAt(m.ctx, stream, pos); err == nil {
			m.playRequest++
			m.awaitingStream = false
			m.retryCount = 0
			m.resumeAt = 0
			m.position = pos
			if paused {
				m.engine.Pause()
				m.state = audio.StatePaused
			} else {
				m.state = audio.StatePlaying
			}
			m.persistQueue()
			return nil
		}
	}
	m.state = audio.StateBuffering
	return m.playSongCmdAt(*m.current, pos, 0)
}

func (m *Model) saveClip() tea.Cmd {
	if m.clip == nil || m.current == nil || m.store == nil {
		return nil
	}
	c := m.clip
	clip := store.Clip{StartMS: c.startMS, EndMS: c.endMS}
	if clip.StartMS < 0 {
		clip.StartMS = 0
	}
	if m.duration > 0 {
		maxMS := int64(m.duration / time.Millisecond)
		if clip.StartMS >= maxMS {
			m.clip.err = "起点必须在歌曲结束之前"
			return nil
		}
		if clip.EndMS >= maxMS {
			clip.EndMS = 0 // 贴着原曲结尾就存成「到结尾」，时长变化时仍跟随
		}
	}
	if clip.EndMS > 0 && clip.EndMS <= clip.StartMS {
		m.clip.err = "终点必须晚于起点"
		return nil
	}
	effectiveEnd := clip.EndMS
	if effectiveEnd == 0 && m.duration > 0 {
		effectiveEnd = int64(m.duration / time.Millisecond)
	}
	if effectiveEnd > 0 && effectiveEnd-clip.StartMS < 100 {
		m.clip.err = "片段至少要有 0.1 秒"
		return nil
	}
	songID := c.songID
	if err := m.store.SetClip(songID, clip); err != nil {
		m.clip.err = err.Error()
		return nil
	}
	m.setActiveClip(songID, clip)
	m.clip = nil
	if m.current == nil || m.current.ID != songID {
		return nil
	}
	start := time.Duration(clip.StartMS) * time.Millisecond
	if m.engine == nil {
		m.position = start
		m.resumeAt = start
		m.persistQueue()
		return nil
	}
	m.setNotice("已保存片段，正在从起点重新播放", false)
	return m.restartWithClip(clip, start, false)
}

func formatClipTime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	tenths := int64(d.Round(100*time.Millisecond) / (100 * time.Millisecond))
	h := tenths / 36000
	m := (tenths % 36000) / 600
	s := (tenths % 600) / 10
	frac := tenths % 10
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d.%d", h, m, s, frac)
	}
	return fmt.Sprintf("%d:%02d.%d", m, s, frac)
}

func (m *Model) clipLines() []string {
	if m.clip == nil {
		return nil
	}
	width := m.width - 6
	if width > 70 {
		width = 70
	}
	if width < 30 {
		width = max(20, m.width-2)
	}
	inner := width - 2
	title := "片段"
	if m.current != nil && m.current.Title != "" {
		title = "片段 · " + m.current.Title
	}
	out := []string{boxTop(truncate(title, width-2), width)}

	start := time.Duration(m.clip.startMS) * time.Millisecond
	end := clipEnd(store.Clip{StartMS: m.clip.startMS, EndMS: m.clip.endMS}, m.duration)
	length := end - start
	if length < 0 {
		length = 0
	}
	startText := formatClipTime(start)
	if m.clip.startMS == 0 {
		startText += "（默认）"
	}
	endText := formatClipTime(end)
	if m.clip.endMS == 0 {
		endText += "（默认）"
	}
	out = append(out, boxLine(
		fmt.Sprintf("起点 %s   终点 %s   长度 %s", startText, endText, formatClipTime(length)),
		inner))
	if m.clip.startMS == 0 || m.clip.endMS == 0 {
		out = append(out, boxLine(st.dim.Render("未设置的边界按原曲处理：起点=原曲开头，终点=原曲结尾"), inner))
	}

	barW := inner - 2
	if barW < 12 {
		barW = 12
	}
	out = append(out, boxLine(st.accent.Render(clipBar(start, end, m.position, m.duration, barW)), inner))
	out = append(out, boxLine(st.muted.Render("当前位置 "+formatClipTime(m.position)+" / 全曲 "+formatClipTime(m.duration)), inner))
	if m.clip.err != "" {
		out = append(out, boxLine(st.err.Render(m.clip.err), inner))
	}
	if m.clip.previewing {
		out = append(out, boxLine(st.dim.Render("编辑中：整首预览，保存后只播放片段"), inner))
	}
	out = append(out, boxLine(st.dim.Render("space 播放/暂停 · x/X v/V 移动 · { 设起点 · } 设终点"), inner))
	out = append(out, boxLine(st.dim.Render("ctrl+r 清除 · enter 保存并从起点重播 · esc 取消"), inner))
	out = append(out, boxBottom(width))
	return out
}

func clipBar(start, end, pos, duration time.Duration, width int) string {
	if width <= 0 {
		return ""
	}
	cols := make([]rune, width)
	for i := range cols {
		cols[i] = '─'
	}
	if duration <= 0 {
		return string(cols)
	}
	col := func(t time.Duration) int {
		ratio := float64(t) / float64(duration)
		if ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}
		return int(ratio * float64(width-1))
	}
	sc, ec, pc := col(start), col(end), col(pos)
	if ec <= sc {
		ec = sc + 1
	}
	for i := sc; i < ec && i < width; i++ {
		cols[i] = '═'
	}
	if sc >= 0 && sc < width {
		cols[sc] = '['
	}
	if ec-1 >= 0 && ec-1 < width {
		cols[ec-1] = ']'
	}
	if pc >= 0 && pc < width {
		cols[pc] = '▲'
	}
	return string(cols)
}

func (m *Model) clipTag() string {
	clip := m.activeClip()
	if !clip.Active() || (m.duration <= 0 && clip.EndMS == 0) {
		return ""
	}
	start := clipStart(clip)
	end := clipEnd(clip, m.duration)
	return fmt.Sprintf("✂ %s–%s", formatDuration(start), formatDuration(end))
}

// clipProgressSegments 返回进度条上区间内外的列范围；duration 未知时返回空。
func (m *Model) clipProgressSegments(width int) (startCol, endCol int, ok bool) {
	clip := m.activeClip()
	if !clip.Active() || m.duration <= 0 || width <= 0 {
		return 0, 0, false
	}
	start := clipStart(clip)
	end := clipEnd(clip, m.duration)
	toCol := func(d time.Duration) int {
		ratio := float64(d) / float64(m.duration)
		if ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}
		return int(ratio * float64(width))
	}
	startCol = toCol(start)
	endCol = toCol(end)
	if startCol < 0 {
		startCol = 0
	}
	if endCol > width {
		endCol = width
	}
	if endCol <= startCol {
		endCol = startCol + 1
		if endCol > width {
			endCol = width
		}
	}
	return startCol, endCol, true
}
