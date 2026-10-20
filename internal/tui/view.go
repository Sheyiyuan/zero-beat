package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/audio"
)

// View 渲染整屏。
func (m *Model) View() tea.View {
	if m.width <= 0 || m.height <= 0 {
		v := tea.NewView("正在启动 zero-beat…")
		v.AltScreen = true
		return v
	}
	v := tea.NewView(strings.Join(m.render(), "\n"))
	v.AltScreen = true
	v.WindowTitle = m.windowTitle()
	return v
}

func (m *Model) windowTitle() string {
	if m.current != nil && m.current.Title != "" {
		return "zero · " + m.current.Title
	}
	return "zero · " + viewNames[m.view]
}

func (m *Model) render() []string {
	w, h := m.width, m.height
	bodyH := h - 6
	if bodyH < 3 {
		bodyH = 3
	}

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

	lines = append(lines,
		st.dim.Render(strings.Repeat("─", w)),
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

func (m *Model) renderHeader() string {
	left := st.title.Render(" zero ") + st.dim.Render("│") + " " + st.text.Render(viewNames[m.view])
	right := st.muted.Render("未登录 · L 扫码")
	if m.user.LoggedIn {
		right = st.muted.Render(m.user.Name)
	}
	gap := m.width - visWidth(left) - visWidth(right) - 1
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) renderNowPlaying() string {
	icon := "■"
	switch m.state {
	case audio.StatePlaying:
		icon = "▶"
	case audio.StatePaused:
		icon = "⏸"
	case audio.StateBuffering:
		icon = "…"
	case audio.StateFailed:
		icon = "✕"
	}

	title := "未在播放"
	if m.current != nil && m.current.Title != "" {
		title = m.current.Title
		if m.current.Artist != "" {
			title += " — " + m.current.Artist
		}
	}

	meta := fmt.Sprintf("%s · %s · 音量 %d%%", m.qualityLabel(), m.mode.String(), int(m.volume*100))
	if m.compDB != 0 {
		meta += fmt.Sprintf(" · 补偿 %+.0f dB", m.compDB)
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
	bar := st.accent.Render(progressBar(barW, m.position, m.duration))
	return " " + bar + "  " + st.muted.Render(timeText)
}

func (m *Model) renderHintLine() string {
	left := st.dim.Render(" " + m.hintLine())
	right := ""
	switch {
	case m.note.text != "" && m.note.isErr:
		right = st.err.Render(m.note.text)
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
	}
	return nil
}

// ---- 曲库 ----

func (m *Model) renderLibrary(h int) []string {
	sideW := 20
	if m.width < 76 {
		sideW = 14
	}
	if m.width-sideW-1 < 24 {
		sideW = 0
	}
	mainW := m.width - sideW - 1

	side := make([]string, h)
	if sideW > 0 {
		side[0] = st.muted.Render(padRight(" 歌单", sideW))
		start, end := windowRange(len(m.playlists), m.playlistIdx, h-1)
		for i := start; i < end; i++ {
			p := m.playlists[i]
			label := fmt.Sprintf(" %s", p.Title)
			if p.Count > 0 {
				label = fmt.Sprintf(" %s %d", p.Title, p.Count)
			}
			row := padRight(truncate(label, sideW), sideW)
			if i == m.playlistIdx {
				side[1+i-start] = st.selected.Render(row)
			} else {
				side[1+i-start] = st.text.Render(row)
			}
		}
	}

	main := make([]string, 0, h)
	header := fmt.Sprintf(" 曲目 · %d 首", len(m.songs))
	if m.songsBusy {
		header = " 载入中…"
	}
	main = append(main, st.muted.Render(truncate(header, mainW)))

	if len(m.songs) == 0 && !m.songsBusy {
		main = append(main, "")
		main = append(main, st.dim.Render(" 这个歌单还是空的"))
		main = append(main, st.dim.Render(" 按 / 搜索 B 站，选中后按 a 加入歌单"))
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
	marker := "  "
	if m.current != nil && s.ID != "" && s.ID == m.current.ID {
		marker = "▶ "
	}
	idx := fmt.Sprintf("%2d", i+1)
	artistW := 14
	titleW := width - artistW - 14
	if titleW < 8 {
		artistW = 8
		titleW = width - artistW - 14
	}
	if titleW < 4 {
		titleW = 4
	}
	dur := formatDuration(time.Duration(s.Duration) * time.Second)
	row := marker + st.dim.Render(idx) + " " +
		padRight(s.Title, titleW) + " " +
		padRight(truncate(s.Artist, artistW), artistW) + " " +
		padLeft(dur, 5)
	row = padRight(row, width)
	if i == m.songIdx {
		return st.selected.Render(row)
	}
	return row
}

// ---- 搜索 ----

func (m *Model) renderSearch(h int) []string {
	out := make([]string, 0, h)
	prompt := st.accent.Render(" / ")
	input := m.searchInput
	if !m.searchEditing {
		input = m.searchKeyword
	}
	if input == "" {
		input = "输入关键词搜索 B 站视频"
	}
	cursor := ""
	if m.searchEditing {
		cursor = st.accent.Render("▏")
	}
	out = append(out, prompt+st.text.Render(truncate(input, m.width-5))+cursor)
	out = append(out, "")

	if m.searchBusy {
		out = append(out, st.muted.Render(" 搜索中…"))
		return out
	}
	if len(m.searchResults) == 0 {
		out = append(out, st.dim.Render(" 按 / 输入关键词，enter 搜索"))
		out = append(out, st.dim.Render(" 结果里 enter 直接播放，a 加入歌单，A 加入队列"))
		return out
	}

	start, end := windowRange(len(m.searchResults), m.searchIdx, h-2)
	for i := start; i < end; i++ {
		item := m.searchResults[i]
		play := ""
		if item.Play > 0 {
			play = fmt.Sprintf("%s 播放", comma(item.Play))
		}
		dur := formatDuration(time.Duration(item.Duration) * time.Second)
		metaW := 22
		titleW := m.width - metaW - 2
		if titleW < 10 {
			titleW = 10
		}
		row := " " + padRight(item.Title, titleW) + st.muted.Render(padLeft(dur, 6)) + "  " +
			st.dim.Render(padLeft(play, metaW-8))
		row = padRight(row, m.width)
		if i == m.searchIdx {
			out = append(out, st.selected.Render(row))
		} else {
			out = append(out, row)
		}
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
		marker := "  "
		if i == m.queueIdx {
			marker = "▶ "
		}
		dur := formatDuration(time.Duration(s.Duration) * time.Second)
		artistW := 16
		titleW := m.width - artistW - 15
		if titleW < 8 {
			artistW = 8
			titleW = m.width - artistW - 15
		}
		if titleW < 4 {
			titleW = 4
		}
		row := marker + st.dim.Render(fmt.Sprintf("%2d", i+1)) + " " +
			padRight(s.Title, titleW) + " " +
			padRight(truncate(s.Artist, artistW), artistW) + " " +
			padLeft(dur, 5)
		row = padRight(row, m.width)
		if i == m.queueCursor {
			out = append(out, st.selected.Render(row))
		} else {
			out = append(out, row)
		}
	}
	return out
}

// ---- 歌词 ----

func (m *Model) renderLyricsView(h int) []string {
	out := make([]string, 0, h)
	if len(m.lyricLines) == 0 {
		out = append(out, "", "", centerText(st.dim.Render("暂无歌词"), m.width), "",
			centerText(st.dim.Render("B 站视频有字幕时才会显示歌词"), m.width))
		return out
	}
	center := h / 2
	start := m.lyricIdx - center
	for i := 0; i < h; i++ {
		idx := start + i
		switch {
		case idx < 0 || idx >= len(m.lyricLines):
			out = append(out, "")
		case i == center:
			out = append(out, centerText(st.accent.Render(truncate(m.lyricLines[idx].Text, m.width)), m.width))
		case i < center:
			out = append(out, centerText(st.dim.Render(truncate(m.lyricLines[idx].Text, m.width)), m.width))
		default:
			out = append(out, centerText(st.muted.Render(truncate(m.lyricLines[idx].Text, m.width)), m.width))
		}
	}
	return out
}

// ---- 帮助 ----

func (m *Model) renderHelp(h int) []string {
	out := make([]string, 0, h)
	out = append(out, st.muted.Render(" 快捷键"))
	keyW := 12
	for _, section := range helpSections {
		out = append(out, "")
		out = append(out, st.accent.Render(" "+section.title))
		for _, e := range section.entries {
			if len(out) >= h {
				return out
			}
			out = append(out, "  "+st.text.Render(padRight(e.keys, keyW))+st.muted.Render(e.desc))
		}
	}
	out = append(out, "")
	out = append(out, st.dim.Render(" 配置文件与数据库在 ~/.config/zero-beat/"))
	return out
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
		if len(lines) > 0 && len(lines)+1 <= h {
			return lines
		}
	}
	return nil
}

func (m *Model) renderLogin(h int) []string {
	header := st.muted.Render(" 扫码登录 B 站 · cookie 只保存在本地")
	if m.qrASCII == "" && m.qrASCIICompact == "" {
		return []string{header, "", st.dim.Render(" 正在获取二维码…")}
	}

	qrLines := m.qrLinesFor(h)
	if len(qrLines) == 0 {
		out := []string{header, "", st.warn.Render(" 终端太小，放不下二维码，请调高窗口后按 r 重试")}
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
	right := []string{
		"  " + st.accent.Render(status),
		"",
		"  " + st.text.Render("1. 打开 B 站 App"),
		"  " + st.text.Render("2. 右上角「扫一扫」"),
		"  " + st.text.Render("3. 确认登录"),
		"",
		"  " + st.dim.Render("登录后可读取收藏夹与高音质音轨"),
		"  " + st.dim.Render("r 重新获取 · esc 返回"),
	}
	if m.user.LoggedIn {
		right = append(right, "", "  "+st.ok.Render("当前已登录："+m.user.Name))
	}

	out := []string{header}
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
