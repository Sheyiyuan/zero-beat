package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
)

// View 渲染整屏。
func (m *Model) View() tea.View {
	if m.width <= 0 || m.height <= 0 {
		v := tea.NewView("正在启动 zero-beat…")
		v.AltScreen = true
		return v
	}
	frame := m.render()
	if m.palette != nil {
		frame = m.overlayPalette(frame)
	}
	if m.aboutPopup {
		frame = overlayBox(frame, m.aboutLines(), m.width, m.height)
	}
	if m.helpPopup {
		frame = overlayBox(frame, m.helpPopupLines(), m.width, m.height)
	}
	if m.picker != nil {
		frame = overlayBox(frame, m.targetPickerLines(), m.width, m.height)
	}
	if m.clip != nil {
		frame = overlayBox(frame, m.clipLines(), m.width, m.height)
	}
	if m.menu != nil {
		frame = m.overlayMenu(frame)
	}
	v := tea.NewView(strings.Join(frame, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = m.windowTitle()
	return v
}

func (m *Model) windowTitle() string {
	if m.current != nil && m.current.Title != "" {
		return "zero · " + m.current.Title
	}
	return "zero · " + m.viewTitle()
}

func (m *Model) viewTitle() string {
	if m.view == viewSearch {
		if m.searchMode == searchSource {
			return "音源搜索"
		}
		return "本地搜索"
	}
	return viewNames[m.view]
}

func (m *Model) render() []string {
	w, h := m.width, m.height
	if m.view == viewLogin {
		// 登录页保留顶栏与分隔线，但右侧不显示账户摘要。
		left, _ := m.headerParts()
		lines := []string{
			padRight(truncate(left, w), w),
			st.dim.Render(strings.Repeat("─", w)),
		}
		bodyH := h - len(lines)
		if bodyH > 0 {
			login := m.renderLogin(bodyH)
			for _, line := range login {
				if len(lines) >= h {
					break
				}
				lines = append(lines, padRight(line, w))
			}
		}
		for len(lines) < h {
			lines = append(lines, padRight("", w))
		}
		return lines
	}
	bodyH := m.bodyHeight()

	lines := []string{
		padRight(m.renderHeader(), w),
		st.dim.Render(strings.Repeat("─", w)),
	}

	body := m.renderBody(bodyH)
	if len(body) > bodyH {
		body = body[:bodyH]
	}
	for len(body) < bodyH {
		body = append(body, "")
	}
	for _, line := range body {
		lines = append(lines, padRight(line, w))
	}

	lines = append(lines, st.dim.Render(strings.Repeat("─", w)))
	if dl, ok := m.downloadLineText(); ok {
		lines = append(lines, padRight(dl, w))
	}
	if lyric, ok := m.lyricStripLine(); ok {
		lines = append(lines, padRight(lyric, w))
	}
	lines = append(lines,
		padRight(m.renderNowPlaying(), w),
		padRight(m.renderProgress(), w),
		padRight(m.renderHintLine(), w),
	)

	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return lines
}

func (m *Model) headerParts() (left, right string) {
	left = st.title.Render(" zero ") + st.dim.Render("│") + " " + st.text.Render(m.viewTitle()) +
		st.dim.Render(" · ") + st.accent.Render("Tab") + st.dim.Render(" 切换")
	// 始终保留右上角的 L 账户入口；空间不足时优先截短账户名。
	loginKey := m.firstKey(keymap.Login)
	suffix := st.muted.Render(" · ") + st.accent.Render(loginKey) + st.muted.Render(" 账户")
	maxAccount := m.width - visWidth(left) - visWidth(suffix) - 1
	if maxAccount < 0 {
		maxAccount = 0
	}
	right = suffix
	if maxAccount > 0 {
		right = st.muted.Render(truncate(m.accountSummary(), maxAccount)) + suffix
	}
	return left, right
}

func (m *Model) renderHeader() string {
	left, right := m.headerParts()
	gap := m.width - visWidth(left) - visWidth(right) - 1
	if gap < 1 {
		gap = 1
	}
	return truncate(left+strings.Repeat(" ", gap)+right, m.width)
}

// bodyHeight 返回正文可用行数；底部常驻歌词条会占用一行。
func (m *Model) bodyHeight() int {
	h := m.height - 6
	if _, _, ok := m.lyricStripText(); ok {
		h--
	}
	if _, ok := m.downloadLineText(); ok {
		h--
	}
	if h < 3 {
		h = 3
	}
	return h
}

// lyricStripLine 渲染底部常驻歌词条；第二个返回值表示这一行是否存在。
func (m *Model) lyricStripLine() (string, bool) {
	text, placeholder, ok := m.lyricStripText()
	if !ok {
		return "", false
	}
	text = truncate(text, m.width-4)
	if placeholder {
		return st.accent.Render(" ♪ ") + st.dim.Render(text), true
	}
	progress := lyricProgress(m.lyricLines, m.lyricIdx,
		int(m.position/time.Millisecond), int(m.duration/time.Millisecond))
	return st.accent.Render(" ♪ ") + gradientText(text, progress, curPalette.text, curPalette.accent), true
}

// lyricStripText 选出一句用于底部常驻显示：优先当前行，LRC 空行时回溯最近一句。
// 歌词页整屏都是歌词，帮助页用于阅读，登录页没有底栏，都不重复显示。
func (m *Model) lyricStripText() (text string, placeholder, ok bool) {
	if m.current == nil {
		return "", false, false
	}
	switch m.view {
	case viewLyrics, viewHelp, viewLogin:
		return "", false, false
	}
	if text := currentLyricText(m.lyricLines, m.lyricIdx); text != "" {
		return text, false, true
	}
	if !m.lyricLoaded {
		return "歌词加载中…", true, true
	}
	return "暂无歌词", true, true
}

// currentLyricText 返回当前歌词行；前奏时预告第一句，空行/间奏保留最近一句。
func currentLyricText(lines []lyrics.Line, idx int) string {
	if len(lines) == 0 {
		return ""
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(lines) {
		idx = len(lines) - 1
	}
	for i := idx; i >= 0; i-- {
		if text := strings.TrimSpace(lines[i].Text); text != "" {
			return text
		}
	}
	for i := idx + 1; i < len(lines); i++ {
		if text := strings.TrimSpace(lines[i].Text); text != "" {
			return text
		}
	}
	return ""
}

// lyricProgress 返回当前行已唱比例；下一行开始时间作为本行结束，最后一行用歌曲时长。
// 没有有效区间（单行歌词/时长未知）时返回 1，让整句保持高亮而不是停在半截。
func lyricProgress(lines []lyrics.Line, idx, posMS, durationMS int) float64 {
	if idx < 0 || idx >= len(lines) {
		return 0
	}
	start := lines[idx].StartMS
	end := 0
	if idx+1 < len(lines) {
		end = lines[idx+1].StartMS
	}
	if end <= start && durationMS > start {
		end = durationMS
	}
	if end <= start {
		return 1
	}
	return clamp01(float64(posMS-start) / float64(end-start))
}

// platformLyricName 返回当前歌曲所在平台提供的歌词名称，用于空状态提示。
func (m *Model) platformLyricName() string {
	if m.current != nil && m.sources != nil {
		return m.sources.LyricName(source.ID(m.current.Source))
	}
	return "平台歌词"
}

// playbackIcon 返回左侧播放/暂停按钮的图标：显示的是点击后执行的动作。
func (m *Model) playbackIcon() string {
	switch m.state {
	case audio.StatePlaying:
		return "⏸"
	case audio.StatePaused:
		return "▶"
	case audio.StateBuffering:
		return "…"
	case audio.StateFailed:
		return "✕"
	}
	return "■"
}

// nowPlayingMeta 拼出底部状态行右侧的元信息，并返回播放模式片段的位置。
// modeStart/modeWidth 以显示宽度计；没有模式片段时返回 -1。
func (m *Model) nowPlayingMeta() (text string, modeStart, modeWidth int) {
	volume := fmt.Sprintf("音量 %d%%", int(m.volume*100))
	if m.muted {
		volume = "静音"
	}
	quality := m.qualityLabel()
	if quality == "—" {
		quality = "音质 " + m.qualityPrefLabel()
	}

	parts := make([]string, 0, 8)
	modeIdx := -1
	add := func(s string, isMode bool) {
		if isMode {
			modeIdx = len(parts)
		}
		parts = append(parts, s)
	}
	if m.current != nil && m.current.Source != "" {
		add(m.sourceName(source.ID(m.current.Source)), false)
	}
	add(quality, false)
	add(m.mode.String(), true)
	add(volume, false)
	if tag := m.clipTag(); tag != "" {
		add(tag, false)
	}
	if m.compDB != 0 {
		add(fmt.Sprintf("补偿 %+.0f dB", m.compDB), false)
	}
	if !m.sleepUntil.IsZero() {
		if left := time.Until(m.sleepUntil); left > 0 {
			add("睡眠 "+formatDuration(left), false)
		}
	}
	if m.resumeAt > 0 && m.state == audio.StatePaused {
		add("断点 "+formatDuration(m.resumeAt), false)
	}

	offset := 0
	for i, part := range parts {
		if i > 0 {
			offset += 3
		}
		if i == modeIdx {
			modeStart = offset
			modeWidth = visWidth(part)
		}
		offset += visWidth(part)
	}
	if modeIdx < 0 {
		modeStart = -1
	}
	return strings.Join(parts, " · "), modeStart, modeWidth
}

func (m *Model) nowPlayingTitle() string {
	title := "未在播放"
	if m.current != nil && m.current.Title != "" {
		title = m.current.Title
		if m.current.Artist != "" {
			title += " — " + m.current.Artist
		}
	}
	return title
}

func (m *Model) renderNowPlaying() string {
	icon := m.playbackIcon()
	title := m.nowPlayingTitle()

	meta, _, _ := m.nowPlayingMeta()
	if maxMeta := m.width - 6; maxMeta > 0 && visWidth(meta) > maxMeta {
		meta = truncate(meta, maxMeta)
	}

	left := st.accent.Render(" "+icon+" ") + st.text.Render(truncate(title, m.width-visWidth(meta)-6))
	gap := m.width - visWidth(left) - visWidth(meta) - 2
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + st.muted.Render(meta) + " "
}

func (m *Model) renderProgress() string {
	timeText := fmt.Sprintf("%s / %s", formatDuration(m.position), formatDuration(m.duration))
	barW := m.width - visWidth(timeText) - 4
	if barW < 10 {
		barW = 10
	}
	filled := progressFill(barW, m.position, m.duration)
	bar := st.accent.Render(strings.Repeat("─", filled)) +
		st.dim.Render(strings.Repeat("─", barW-filled))
	if startCol, endCol, ok := m.clipProgressSegments(barW); ok {
		var b strings.Builder
		for i := 0; i < barW; i++ {
			ch := "─"
			if i < startCol || i >= endCol {
				ch = "╌"
			}
			switch {
			case i == startCol:
				ch = "["
			case i == endCol-1:
				ch = "]"
			}
			if i < filled {
				b.WriteString(st.accent.Render(ch))
			} else {
				b.WriteString(st.dim.Render(ch))
			}
		}
		bar = b.String()
	}
	return " " + bar + "  " + st.muted.Render(timeText)
}

func (m *Model) renderHintLine() string {
	if m.prompt != promptNone {
		return st.warn.Render(truncate(" "+m.promptLine(), m.width))
	}
	left := st.dim.Render(" " + m.hintLine())
	right := ""
	switch {
	case m.note.text != "" && m.note.isErr:
		right = st.err.Render(m.note.text)
	case m.note.text != "" && m.note.isWarn:
		right = st.warn.Render(m.note.text)
	case m.note.text != "":
		right = st.ok.Render(m.note.text)
	case m.songsBusy:
		right = st.muted.Render("载入中…")
	case m.searchBusy:
		right = st.muted.Render("搜索中…")
	}
	if right == "" {
		return left
	}
	gap := m.width - visWidth(left) - visWidth(right) - 1
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) renderBody(h int) []string {
	switch m.view {
	case viewLibrary:
		return m.renderLibrary(h)
	case viewSearch:
		return m.renderSearch(h)
	case viewQueue:
		return m.renderQueue(h)
	case viewLyrics:
		return m.renderLyricsView(h)
	case viewHelp:
		return m.renderHelp(h)
	case viewLogin:
		return m.renderLogin(h)
	case viewFolders:
		return m.renderFolders(h)
	case viewMix:
		return m.renderMix(h)
	case viewDownloads:
		return m.renderDownloads(h)
	}
	return nil
}

// ---- 曲库 ----

func (m *Model) librarySideWidth() int {
	sideW := 22
	if m.width < 76 {
		sideW = 14
	}
	if m.width-sideW-1 < 24 {
		sideW = 0
	}
	return sideW
}

func (m *Model) renderLibrary(h int) []string {
	sideW := m.librarySideWidth()
	mainW := m.width - sideW - 1

	side := make([]string, h)
	if sideW > 0 {
		side[0] = st.muted.Render(padRight("   歌单", sideW))
		if m.libraryFocus == focusPlaylists {
			side[0] = st.accent.Render(padRight("   歌单", sideW))
		}
		start, end := windowRange(len(m.playlists), m.playlistIdx, h-1)
		for i := start; i < end; i++ {
			p := m.playlists[i]
			side[1+i-start] = m.renderPlaylistRow(playlistRowSpec{
				Playing:  m.current != nil && m.playingPlaylistID != "" && p.ID == m.playingPlaylistID,
				Selected: m.libraryFocus == focusPlaylists && i == m.playlistIdx,
				Tag:      m.playlistTag(p),
				Title:    p.Title,
				Count:    p.Count,
			}, sideW)
		}
	}

	main := make([]string, 0, h)
	header := fmt.Sprintf(" 曲目 · %d 首", len(m.songs))
	if p, ok := m.currentPlaylist(); ok && strings.TrimSpace(p.Title) != "" {
		header = fmt.Sprintf(" %s · %d 首", p.Title, len(m.songs))
	}
	if m.songsBusy {
		header = " 载入中…"
	}
	headerStyle := st.muted
	if m.libraryFocus == focusSongs {
		headerStyle = st.accent
	}
	main = append(main, headerStyle.Render(truncate(header, mainW)))

	if len(m.songs) == 0 && !m.songsBusy {
		main = append(main, "")
		if len(m.playlists) == 0 {
			main = append(main, st.dim.Render(" 还没有歌单"))
			main = append(main, st.dim.Render(" 按 N 新建歌单；搜索后按 a 会自动建一个"))
		} else {
			main = append(main, st.dim.Render(" 这个歌单还是空的"))
			main = append(main, st.dim.Render(" 按 ? 搜索音源，选中后按 a 加入歌单"))
		}
	} else {
		start, end := windowRange(len(m.songs), m.songIdx, h-1)
		for i := start; i < end; i++ {
			main = append(main, m.songRow(i, mainW))
		}
	}

	out := make([]string, h)
	for i := 0; i < h; i++ {
		left := ""
		if i < len(side) {
			left = side[i]
		}
		right := ""
		if i < len(main) {
			right = main[i]
		}
		if sideW > 0 {
			out[i] = padRight(left, sideW) + st.dim.Render("│") + padRight(right, mainW)
		} else {
			out[i] = padRight(right, m.width)
		}
	}
	return out
}

func (m *Model) songRow(i, width int) string {
	s := m.songs[i]
	playing := m.current != nil && s.ID != "" && s.ID == m.current.ID
	selected := m.libraryFocus == focusSongs && i == m.songIdx
	return m.renderSongRow(songRowSpec{
		Playing:  playing,
		Selected: selected,
		Tag:      m.sourceMark(source.ID(s.Source)),
		Index:    i + 1,
		Title:    s.Title,
		Artist:   s.Artist,
		Meta:     formatDuration(time.Duration(s.Duration) * time.Second),
		MetaW:    rowDurW,
		Marquee:  selected,
	}, width)
}

func (m *Model) resetMarquee() {
	m.marqueeSong = ""
	m.marqueeOffset = 0
}

func (m *Model) advanceMarquee() {
	if m.view != viewLibrary || len(m.songs) == 0 {
		return
	}
	idx := clampInt(m.songIdx, 0, len(m.songs)-1)
	id := m.songs[idx].ID
	if id != m.marqueeSong {
		m.marqueeSong = id
		m.marqueeOffset = 0
		return
	}
	// tick 为 250ms；每次前进 2 列，约 8 列/秒，比原来的 2 列/秒快 4 倍。
	m.marqueeOffset += 2
}

// ---- 搜索 ----

func (m *Model) renderSearch(h int) []string {
	out := make([]string, 0, h)
	promptText := " / "
	if m.searchMode == searchSource {
		promptText = " ? "
	}
	prompt := st.accent.Render(promptText)
	input := m.searchInput
	if !m.searchEditing {
		input = m.searchKeyword
	}
	if input == "" {
		if m.searchMode == searchSource {
			input = "输入关键词搜索音源（s 切换音源）"
		} else if m.searchMode == searchLocalAll {
			input = "输入关键词搜索本地全部（r 切换当前歌单）"
		} else {
			input = "输入关键词搜索当前歌单（r 切换本地全部）"
		}
	}
	cursor := ""
	if m.searchEditing {
		cursor = st.accent.Render("▏")
	}
	line := prompt + st.text.Render(truncate(input, m.width-5)) + cursor

	meta := ""
	if m.searchMode == searchSource {
		sourceName := "全部"
		if m.searchSource != "" {
			sourceName = m.sourceName(m.searchSource)
			if m.sources != nil {
				if src := m.sources.Get(m.searchSource); src != nil {
					sourceName = src.Name()
				}
			}
		}
		meta = sourceName
		if m.searchPages > 1 {
			meta = fmt.Sprintf("%s · %d/%d 页 · ctrl+u/d 翻页", sourceName, m.searchPage, m.searchPages)
		} else {
			meta += " · s 换源"
		}
	} else if m.searchMode == searchLocalAll {
		meta = fmt.Sprintf("本地全部 · %d 条结果 · r 切换当前歌单", len(m.localResults))
	} else {
		meta = fmt.Sprintf("当前歌单 · %d 条结果 · r 切换本地全部", len(m.localResults))
	}
	if m.searchKeyword != "" && meta != "" {
		gap := m.width - visWidth(line) - visWidth(meta) - 1
		if gap > 0 {
			line += strings.Repeat(" ", gap)
		} else {
			line += " "
		}
		line += st.muted.Render(meta)
	}
	out = append(out, padRight(line, m.width))
	out = append(out, "")

	if m.searchBusy {
		out = append(out, st.muted.Render(" 搜索中…"))
		return out
	}
	if m.searchMode == searchSource {
		if len(m.searchResults) == 0 {
			out = append(out, st.dim.Render(" 按 ? 输入关键词，enter 搜索音源"))
			out = append(out, st.dim.Render(" 结果里 enter 直接播放，a 加入歌单，A 加入队列"))
			return out
		}
		start, end := windowRange(len(m.searchResults), m.searchIdx, h-2)
		for i := start; i < end; i++ {
			item := m.searchResults[i]
			extra := ""
			if item.Play > 0 {
				extra = fmt.Sprintf("%s 播放", comma(item.Play))
			}
			if item.VIP {
				extra = strings.TrimSpace("VIP " + extra)
			}
			if extra == "" && item.Album != "" {
				extra = "《" + item.Album + "》"
			}
			song := m.searchResultSong(i)
			out = append(out, m.renderSongRow(songRowSpec{
				Playing:  m.current != nil && song.ID != "" && song.ID == m.current.ID,
				Selected: i == m.searchIdx,
				Tag:      m.sourceMark(item.Source),
				Index:    i + 1,
				Title:    item.Title,
				Artist:   item.Artist,
				Meta:     formatDuration(time.Duration(item.Duration) * time.Second),
				MetaW:    rowDurW,
				Extra:    extra,
				ExtraW:   rowExtraW,
				Marquee:  i == m.searchIdx,
			}, m.width))
		}
		return out
	}

	if len(m.localResults) == 0 {
		out = append(out, st.dim.Render(" 按 / 输入关键词，enter 搜索本地歌曲"))
		out = append(out, st.dim.Render(" r 切换当前歌单/本地全部；enter 播放，a 加入歌单"))
		return out
	}
	start, end := windowRange(len(m.localResults), m.searchIdx, h-2)
	for i := start; i < end; i++ {
		song := m.localResults[i]
		out = append(out, m.renderSongRow(songRowSpec{
			Playing:  m.current != nil && song.ID != "" && song.ID == m.current.ID,
			Selected: i == m.searchIdx,
			Tag:      m.sourceMark(source.ID(song.Source)),
			Index:    i + 1,
			Title:    song.Title,
			Artist:   song.Artist,
			Meta:     formatDuration(time.Duration(song.Duration) * time.Second),
			MetaW:    rowDurW,
			Marquee:  i == m.searchIdx,
		}, m.width))
	}
	return out
}

// ---- 队列 ----

func (m *Model) renderQueue(h int) []string {
	out := make([]string, 0, h)
	header := fmt.Sprintf(" 播放队列 · %d 首", len(m.queue))
	out = append(out, st.muted.Render(truncate(header, m.width)))
	if len(m.queue) == 0 {
		out = append(out, "", st.dim.Render(" 队列是空的：在曲库或搜索里按 enter 播放"))
		return out
	}
	start, end := windowRange(len(m.queue), m.queueCursor, h-1)
	for i := start; i < end; i++ {
		s := m.queue[i]
		out = append(out, m.renderSongRow(songRowSpec{
			Playing:  m.current != nil && i == m.queueIdx,
			Selected: i == m.queueCursor,
			Tag:      m.sourceMark(source.ID(s.Source)),
			Index:    i + 1,
			Title:    s.Title,
			Artist:   s.Artist,
			Meta:     formatDuration(time.Duration(s.Duration) * time.Second),
			MetaW:    rowDurW,
			Marquee:  i == m.queueCursor,
		}, m.width))
	}
	return out
}

// ---- 下载管理 ----

func (m *Model) renderDownloads(h int) []string {
	out := make([]string, 0, h)
	total := int64(0)
	missing := 0
	for _, item := range m.downloadItems {
		total += item.entry.Download.Size
		if item.missing {
			missing++
		}
	}
	header := fmt.Sprintf(" 下载管理 · %d 首 · 占用 %s", len(m.downloadItems), humanSize(total))
	if missing > 0 {
		header += fmt.Sprintf(" · %d 个文件缺失", missing)
	}
	if m.downloadsBusy {
		header = " 正在读取下载记录…"
	}
	out = append(out, st.muted.Render(truncate(header, m.width)))
	if m.downloadsBusy {
		out = append(out, "", st.dim.Render(" 请稍候"))
		return out
	}
	if len(m.downloadItems) == 0 {
		out = append(out, "", st.dim.Render(" 还没有下载记录"))
		out = append(out, st.dim.Render(" 在曲库或搜索里按 w / W 下载后会自动出现在这里"))
		return out
	}
	start, end := windowRange(len(m.downloadItems), m.downloadIdx, h-1)
	for i := start; i < end; i++ {
		out = append(out, m.downloadRow(i, m.width))
	}
	return out
}

func (m *Model) downloadRow(i, width int) string {
	item := m.downloadItems[i]
	entry := item.entry
	playing := m.current != nil && entry.Song.ID != "" && entry.Song.ID == m.current.ID
	meta := humanSize(entry.Download.Size)
	if entry.Download.Codec != "" {
		meta += " · " + entry.Download.Codec
	}
	if item.missing {
		// 文件缺失时保留可读文案；颜色由 MetaErr 走 st.err。
		meta = "文件缺失"
	}
	return m.renderSongRow(songRowSpec{
		Playing:  playing,
		Selected: i == m.downloadIdx,
		Tag:      m.sourceMark(source.ID(entry.Song.Source)),
		Index:    i + 1,
		Title:    downloadTitle(item),
		Artist:   entry.Song.Artist,
		Meta:     meta,
		MetaW:    rowDownloadW,
		MetaErr:  item.missing,
		Marquee:  i == m.downloadIdx,
	}, width)
}

// ---- 收藏夹选择 ----

func (m *Model) renderFolders(h int) []string {
	name := m.sourceName(m.folderSource)
	if src := m.sources.Get(m.folderSource); src != nil {
		name = src.Name()
	}
	out := make([]string, 0, h)
	header := fmt.Sprintf(" 添加 %s 歌单/收藏夹 · %d 个可添加 · s 切换音源", name, len(m.folderItems))
	if m.folderBusy {
		header = " 正在读取 " + name + " 歌单/收藏夹…"
	}
	out = append(out, st.muted.Render(truncate(header, m.width)))
	if m.folderBusy {
		out = append(out, "", st.dim.Render(" 请稍候"))
		return out
	}
	if len(m.folderItems) == 0 {
		out = append(out, "", st.dim.Render(" 没有可添加的歌单/收藏夹"))
		out = append(out, st.dim.Render(" 已添加的不会重复出现；s 切换音源，r 重新读取"))
		return out
	}
	start, end := windowRange(len(m.folderItems), m.folderIdx, h-1)
	for i := start; i < end; i++ {
		p := m.folderItems[i]
		// ▶ 只表示正在播放；导入页的光标只用蓝底。
		out = append(out, m.renderPlaylistRow(playlistRowSpec{
			Selected: i == m.folderIdx,
			Tag:      m.sourceMark(p.Source),
			Title:    p.Title,
			Count:    p.Count,
		}, m.width))
	}
	return out
}

// ---- 歌词 ----

// lyricRow 是歌词页的一行显示单元；译文紧跟在原文下一行，滚动时保持同步。
type lyricRow struct {
	text        string
	lineIdx     int
	translation bool
}

func (m *Model) lyricRows() ([]lyricRow, int) {
	rows := make([]lyricRow, 0, len(m.lyricLines)*2)
	current := -1
	for i, line := range m.lyricLines {
		if i == m.lyricIdx {
			current = len(rows)
		}
		rows = append(rows, lyricRow{text: line.Text, lineIdx: i})
		if t := strings.TrimSpace(line.Translation); t != "" && t != strings.TrimSpace(line.Text) {
			rows = append(rows, lyricRow{text: t, lineIdx: i, translation: true})
		}
	}
	return rows, current
}

// lyricWindow 返回当前高度下可见的歌词行与起始下标。
func (m *Model) lyricWindow(h int) ([]lyricRow, int) {
	rows, current := m.lyricRows()
	if len(rows) == 0 {
		return nil, 0
	}
	start := current - h/2
	if m.lyricScroll != 0 {
		// 手动滚动时同样保留负起点（顶部留白），否则滚一次就会贴顶跳变。
		start = clampInt(start+m.lyricScroll, min(0, start), max(0, len(rows)-h))
	}
	return rows, start
}

func (m *Model) renderLyricsView(h int) []string {
	out := make([]string, 0, h)
	if len(m.lyricLines) == 0 {
		hint := m.platformLyricName() + "与 LRCLIB 都没有收录"
		if m.lyricSource != "" {
			hint = "歌词来源：" + m.lyricSource
		}
		out = append(out, "", "", centerText(st.dim.Render("暂无歌词"), m.width), "",
			centerText(st.dim.Render(hint), m.width), "",
			centerText(st.dim.Render("< > 可在有歌词时调整偏移，0 归零"), m.width))
		return out
	}

	rows, start := m.lyricWindow(h)
	progress := lyricProgress(m.lyricLines, m.lyricIdx,
		int(m.position/time.Millisecond), int(m.duration/time.Millisecond))
	for i := 0; i < h; i++ {
		idx := start + i
		if idx < 0 || idx >= len(rows) {
			out = append(out, "")
			continue
		}
		row := rows[idx]
		text := truncate(row.text, m.width)
		switch {
		case row.lineIdx == m.lyricIdx && !row.translation:
			out = append(out, centerText(gradientText(text, progress, curPalette.text, curPalette.accent), m.width))
		case row.lineIdx == m.lyricIdx && row.translation:
			out = append(out, centerText(st.muted.Render(text), m.width))
		case row.translation:
			out = append(out, centerText(st.dim.Render(text), m.width))
		case row.lineIdx < m.lyricIdx:
			out = append(out, centerText(st.dim.Render(text), m.width))
		default:
			out = append(out, centerText(st.muted.Render(text), m.width))
		}
	}
	return out
}

// ---- 帮助 ----

func (m *Model) renderHelp(h int) []string {
	lines := m.helpLines()
	if h <= 0 {
		return nil
	}
	maxOffset := len(lines) - h
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := m.helpOffset
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	end := offset + h
	if end > len(lines) {
		end = len(lines)
	}
	return lines[offset:end]
}

// ---- 登录 ----

// qrLinesFor 选出当前终端高度放得下的二维码（优先完整链接，其次去掉跟踪参数）。
func (m *Model) qrLinesFor(h int) []string {
	split := func(art string) []string {
		if art == "" {
			return nil
		}
		return strings.Split(strings.TrimRight(art, "\n"), "\n")
	}
	for _, art := range []string{m.qrASCII, m.qrASCIICompact} {
		lines := split(art)
		if len(lines) > 0 && len(lines) <= h {
			return lines
		}
	}
	return nil
}

func (m *Model) renderLogin(h int) []string {
	name := m.sourceName(m.loginSource)
	if m.sources != nil {
		if src := m.sources.Get(m.loginSource); src != nil {
			name = src.Name()
		}
	}
	accounts := m.accountStatusLines()
	panelTitle := st.muted.Render(" 账户 · 多音源 · " + m.firstKey(keymap.Source) + " 切换登录源")

	if u, ok := m.userFor(m.loginSource); ok {
		return m.renderLoggedIn(h, u, name)
	}

	if m.qrASCII == "" && m.qrASCIICompact == "" {
		out := []string{panelTitle}
		out = append(out, accounts...)
		out = append(out, "", st.dim.Render(" 正在获取二维码…"), st.dim.Render(" s 切换登录源 · r 重新获取"))
		return out
	}

	qrLines := m.qrLinesFor(h)
	if len(qrLines) == 0 {
		out := []string{panelTitle}
		out = append(out, accounts...)
		out = append(out, "", st.warn.Render(" 终端放不下二维码，按 "+m.firstKey(keymap.LoginImage)+" 打开二维码图片扫码"))
		if m.qrPath != "" {
			out = append(out, st.dim.Render(" 图片："+m.qrPath))
		}
		if m.qr != nil && m.qr.URL != "" {
			out = append(out, "", st.text.Render(" 也可以把链接复制到浏览器打开："), st.accent.Render(" "+m.qr.URL))
		}
		return out
	}

	qrW := 0
	for _, line := range qrLines {
		if w := visWidth(line); w > qrW {
			qrW = w
		}
	}

	status := m.qrStatus
	if status == "" {
		status = "等待扫码"
	}
	// 右侧留给文字的空间；底部快捷键提示较长，按实际宽度换行，避免被截断。
	rightW := m.width - (qrW + 2)
	textW := rightW - 2
	if textW < 1 {
		textW = 1
	}
	right := []string{panelTitle}
	right = append(right, accounts...)
	right = append(right,
		"",
		"  "+st.accent.Render("扫码登录 "+name),
		"  "+st.accent.Render(status),
		"",
		"  "+st.text.Render("1. 打开 "+m.loginAppName(m.loginSource)),
		"  "+st.text.Render("2. 右上角「扫一扫」"),
		"  "+st.text.Render("3. 确认登录"),
		"",
	)
	for _, line := range wrapText("登录后可读取歌单/收藏夹与高音质音轨", textW) {
		right = append(right, "  "+st.dim.Render(line))
	}
	hint := m.firstKey(keymap.Refresh) + " 重新获取 · " +
		m.firstKey(keymap.Source) + " 切换音源 · " +
		m.firstKey(keymap.LoginImage) + " 二维码图片 · " +
		m.firstKey(keymap.Logout) + " 退出当前源"
	for _, line := range wrapText(hint, textW) {
		right = append(right, "  "+st.dim.Render(line))
	}

	out := make([]string, 0, h)
	rows := max(len(qrLines), len(right))
	for i := 0; i < rows && len(out) < h; i++ {
		left := ""
		if i < len(qrLines) {
			left = "  " + st.text.Render(qrLines[i])
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out = append(out, padRight(left, qrW+2)+r)
	}
	return out
}

func (m *Model) renderLoggedIn(h int, u source.User, sourceName string) []string {
	name := strings.TrimSpace(u.Name)
	if name == "" {
		name = "（未命名账号）"
	}
	uid := strings.TrimSpace(u.ID)
	if uid == "" {
		uid = "—"
	}
	leftW := 36
	if leftW > m.width/2 {
		leftW = m.width / 2
	}
	if leftW < 24 {
		leftW = 24
	}
	left := []string{
		"  " + st.ok.Render("已登录"),
		"",
		"  " + st.muted.Render("用户名"),
		"  " + st.title.Render(name),
		"",
		"  " + st.muted.Render("UID"),
		"  " + st.text.Render(uid),
		"",
		"  " + st.muted.Render("登录源"),
		"  " + st.text.Render(sourceName),
		"",
		"  " + st.dim.Render(m.firstKey(keymap.Logout)+" 退出当前账号 · "+m.firstKey(keymap.Up)+"/"+m.firstKey(keymap.Down)+" 切换账号"),
	}
	right := []string{st.muted.Render(" 账户 · 多音源 · " + m.firstKey(keymap.Source) + " 切换登录源")}
	right = append(right, m.accountStatusLines()...)
	right = append(right,
		"",
		"  "+st.muted.Render("当前账号"),
		"  "+st.text.Render(name),
		"",
		"  "+st.text.Render(m.firstKey(keymap.Up)+"/"+m.firstKey(keymap.Down)+" 切换登录源"),
		"  "+st.text.Render(m.firstKey(keymap.Refresh)+" 刷新账号信息"),
		"  "+st.text.Render(m.firstKey(keymap.Logout)+" 退出当前账号"),
		"  "+st.text.Render(m.firstKey(keymap.HelpPopup)+" 快捷键"),
	)
	out := make([]string, 0, h)
	rows := max(len(left), len(right))
	for i := 0; i < rows && len(out) < h; i++ {
		l := ""
		if i < len(left) {
			l = "  " + left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out = append(out, padRight(l, leftW+2)+r)
	}
	return out
}

// accountStatusLines 同时列出两个平台的登录状态，不随当前扫码源切换而隐藏。
func (m *Model) accountStatusLines() []string {
	var ids []source.ID
	if m.sources != nil {
		ids = m.sources.IDs()
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		name := m.sourceName(id)
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
		marker := "  "
		if id == m.loginSource {
			marker = "▶ "
		}
		state := st.dim.Render("未登录")
		if logged {
			who := u.Name
			if who == "" {
				who = "已登录"
			}
			state = st.ok.Render("已登录") + st.dim.Render(" · "+who)
		}
		out = append(out, "  "+st.accent.Render(marker)+st.text.Render(padRight(name, 10))+state)
	}
	return out
}
