package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/lrclib"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// lyricBindStage 是歌词来源浮层的三个步骤：搜索候选 → 预览确认 → 落库。
type lyricBindStage int

const (
	bindStageSearch lyricBindStage = iota
	bindStagePreview
	bindStageInput
	bindStageLocalSongs
)

// lyricBindState 是浮层的临时状态；绑定成功前不改库。
type lyricBindState struct {
	song  store.Song
	stage lyricBindStage

	query   string
	editing bool // 搜索框正在输入；此时字母进 query 而不是当快捷键
	// cursor 是 query 的光标位置（rune 下标）；编辑时保留，便于改中间的字。
	cursor  int
	filter  string
	cands   []app.LyricCandidate
	idx     int
	busy    bool
	err     string
	preview app.LyricPreview

	input     string
	inputHint string

	localSongs []store.Song
	localIdx   int
}

// lyricCandidatesMsg 是一次候选搜索的结果。
type lyricCandidatesMsg struct {
	songID     string
	query      string
	filter     string
	candidates []app.LyricCandidate
	err        error
}

// lyricPreviewMsg 是候选的预览结果；确认后才会绑定。
type lyricPreviewMsg struct {
	songID  string
	cand    app.LyricCandidate
	preview app.LyricPreview
	err     error
}

// lyricBindDoneMsg 表示绑定或解除已经完成。
type lyricBindDoneMsg struct {
	songID  string
	label   string
	cleared bool
	err     error
}

// openLyricBind 打开当前歌曲的歌词来源浮层。
func (m *Model) openLyricBind() tea.Cmd {
	if m.current == nil {
		m.setNotice("当前没有正在播放的歌曲", true)
		return nil
	}
	song := store.NormalizeSong(*m.current)
	m.lyricBind = &lyricBindState{
		song:    song,
		query:   lrclib.CleanTitle(song.Title),
		editing: false,
	}
	m.lyricBind.cands = nil
	return m.searchLyricCandidatesCmd(song, m.lyricBind.query, "")
}

func (m *Model) searchLyricCandidatesCmd(song store.Song, query, filter string) tea.Cmd {
	svc := m.service()
	state := m.lyricBind
	if state != nil {
		state.busy = true
		state.err = ""
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cands, err := svc.SearchLyricCandidates(ctx, song, query, filter)
		return lyricCandidatesMsg{songID: song.ID, query: query, filter: filter, candidates: cands, err: err}
	}
}

func (m *Model) previewLyricBindingCmd(song store.Song, cand app.LyricCandidate) tea.Cmd {
	svc := m.service()
	if m.lyricBind != nil {
		m.lyricBind.busy = true
		m.lyricBind.err = ""
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		preview, err := svc.PreviewLyricBinding(ctx, song, cand)
		return lyricPreviewMsg{songID: song.ID, cand: cand, preview: preview, err: err}
	}
}

func (m *Model) bindLyricsCmd(song store.Song, cand app.LyricCandidate) tea.Cmd {
	svc := m.service()
	if m.lyricBind != nil {
		m.lyricBind.busy = true
		m.lyricBind.err = ""
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, cleared, err := svc.BindLyrics(ctx, song, cand)
		label := candidateLabel(cand)
		return lyricBindDoneMsg{songID: song.ID, label: label, cleared: cleared, err: err}
	}
}

func (m *Model) clearLyricBindingCmd(song store.Song) tea.Cmd {
	svc := m.service()
	return func() tea.Msg {
		err := svc.ClearLyricBinding(song.ID)
		return lyricBindDoneMsg{songID: song.ID, cleared: true, err: err}
	}
}

func (m *Model) localSongsCmd() tea.Cmd {
	svc := m.service()
	return func() tea.Msg {
		songs, err := svc.LocalSongs()
		return bindLocalSongsMsg{songs: songs, err: err}
	}
}

// candidateLabel 是候选在提示与来源标签里的短名。
func candidateLabel(c app.LyricCandidate) string {
	switch c.Kind {
	case store.BindingKindSource:
		return string(c.Source) + " · " + c.Title
	case store.BindingKindLRCLIB:
		return "LRCLIB · " + c.Title
	case store.BindingKindLocalFile:
		return "本地 LRC · " + fileName(c.Path)
	case store.BindingKindLocalSong:
		return "本机曲目 · " + c.Title
	}
	return c.Title
}

func fileName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 && i+1 < len(path) {
		return path[i+1:]
	}
	return path
}

// handleLyricBindKey 处理歌词来源浮层的按键。
func (m *Model) handleLyricBindKey(key tea.Key) (tea.Model, tea.Cmd) {
	b := m.lyricBind
	if b == nil {
		return m, nil
	}
	if isCtrl(key, 'c') {
		m.shutdown()
		return m, tea.Quit
	}
	// 再按一次 B 关掉浮层；输入态下 B 仍然当作字符。
	if !b.editing && b.stage == bindStageSearch && m.match(keymap.BindLyrics, key) {
		m.lyricBind = nil
		return m, nil
	}
	if key.Code == tea.KeyEscape {
		switch {
		case b.editing:
			// esc 先退出输入态：关键字保留，浮层不关，避免打一半全丢。
			b.editing = false
		case b.stage == bindStageInput || b.stage == bindStageLocalSongs || b.stage == bindStagePreview:
			b.stage, b.err = bindStageSearch, ""
		default:
			m.lyricBind = nil
		}
		return m, nil
	}
	switch b.stage {
	case bindStageInput:
		return m.handleLyricBindInput(key)
	case bindStageLocalSongs:
		return m.handleLyricBindLocalSongs(key)
	case bindStagePreview:
		return m.handleLyricBindPreview(key)
	}
	return m.handleLyricBindSearch(key)
}

func (m *Model) handleLyricBindSearch(key tea.Key) (tea.Model, tea.Cmd) {
	b := m.lyricBind
	if b.editing {
		runes := []rune(b.query)
		if b.cursor < 0 || b.cursor > len(runes) {
			b.cursor = len(runes)
		}
		switch key.Code {
		case tea.KeyEnter:
			b.editing = false
			return m, m.searchLyricCandidatesCmd(b.song, b.query, b.filter)
		case tea.KeyBackspace:
			if b.cursor > 0 {
				b.query = string(append(runes[:b.cursor-1], runes[b.cursor:]...))
				b.cursor--
			}
			return m, nil
		case tea.KeyDelete:
			if b.cursor < len(runes) {
				b.query = string(append(runes[:b.cursor], runes[b.cursor+1:]...))
			}
			return m, nil
		case tea.KeyLeft:
			if b.cursor > 0 {
				b.cursor--
			}
			return m, nil
		case tea.KeyRight:
			if b.cursor < len(runes) {
				b.cursor++
			}
			return m, nil
		case tea.KeyHome:
			b.cursor = 0
			return m, nil
		case tea.KeyEnd:
			b.cursor = len(runes)
			return m, nil
		case tea.KeyTab:
			return m.cycleLyricBindFilter()
		}
		if isCtrl(key, 'u') {
			b.query = ""
			b.cursor = 0
			return m, nil
		}
		if key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) == 0 {
			text := []rune(key.Text)
			merged := append(append(append([]rune(nil), runes[:b.cursor]...), text...), runes[b.cursor:]...)
			b.query = string(merged)
			b.cursor += len(text)
			return m, nil
		}
		return m, nil
	}
	switch {
	case key.Code == tea.KeyTab:
		return m.cycleLyricBindFilter()
	case isKey(key, "l"):
		b.stage = bindStageLocalSongs
		b.localIdx = 0
		b.err = ""
		return m, m.localSongsCmd()
	case isKey(key, "u"):
		b.stage = bindStageInput
		b.input = ""
		b.inputHint = "粘贴链接/ID，或以 / ~ . 开头的本地文件路径"
		b.err = ""
		return m, nil
	case isKey(key, "x"):
		return m, m.clearLyricBindingCmd(b.song)
	case isKey(key, "/"):
		b.editing = true
		b.cursor = len([]rune(b.query))
		return m, nil
	case key.Code == tea.KeyUp:
		if len(b.cands) > 0 {
			b.idx = (b.idx + len(b.cands) - 1) % len(b.cands)
		}
		return m, nil
	case key.Code == tea.KeyDown:
		if len(b.cands) > 0 {
			b.idx = (b.idx + 1) % len(b.cands)
		}
		return m, nil
	case key.Code == tea.KeyEnter:
		if b.busy {
			return m, nil
		}
		cand, ok := b.selected()
		if !ok {
			return m, nil
		}
		if cand.Kind == store.BindingKindLRCLIB && cand.Known && !cand.HasSynced {
			b.err = "这条 LRCLIB 记录没有时间轴，不能绑定"
			return m, nil
		}
		return m, m.previewLyricBindingCmd(b.song, cand)
	}
	return m, nil
}

func (m *Model) handleLyricBindPreview(key tea.Key) (tea.Model, tea.Cmd) {
	b := m.lyricBind
	switch key.Code {
	case tea.KeyEnter:
		if b.busy {
			return m, nil
		}
		cand, ok := b.selected()
		if !ok {
			return m, nil
		}
		return m, m.bindLyricsCmd(b.song, cand)
	case tea.KeyTab:
		return m.cycleLyricBindFilter()
	}
	return m, nil
}

func (m *Model) handleLyricBindInput(key tea.Key) (tea.Model, tea.Cmd) {
	b := m.lyricBind
	switch key.Code {
	case tea.KeyEnter:
		cand, err := m.candidateFromInput(b.input)
		if err != "" {
			b.err = err
			return m, nil
		}
		return m, m.previewLyricBindingCmd(b.song, cand)
	case tea.KeyBackspace:
		if r := []rune(b.input); len(r) > 0 {
			b.input = string(r[:len(r)-1])
		}
		return m, nil
	}
	if isCtrl(key, 'u') {
		b.input = ""
		return m, nil
	}
	if key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) == 0 {
		b.input += key.Text
	}
	return m, nil
}

func (m *Model) handleLyricBindLocalSongs(key tea.Key) (tea.Model, tea.Cmd) {
	b := m.lyricBind
	if isKey(key, "u") || isKey(key, "p") {
		b.stage = bindStageInput
		b.input = ""
		b.inputHint = "输入本地 .lrc 路径（支持 ~ 与相对路径）"
		b.err = ""
		return m, nil
	}
	switch key.Code {
	case tea.KeyUp:
		if len(b.localSongs) > 0 {
			b.localIdx = (b.localIdx + len(b.localSongs) - 1) % len(b.localSongs)
		}
		return m, nil
	case tea.KeyDown:
		if len(b.localSongs) > 0 {
			b.localIdx = (b.localIdx + 1) % len(b.localSongs)
		}
		return m, nil
	case tea.KeyEnter:
		if b.localIdx < 0 || b.localIdx >= len(b.localSongs) {
			return m, nil
		}
		target := b.localSongs[b.localIdx]
		cand := app.LyricCandidate{
			Kind:   store.BindingKindLocalSong,
			SongID: target.ID,
			Title:  target.Title,
			Artist: target.Artist,
		}
		return m, m.previewLyricBindingCmd(b.song, cand)
	}
	return m, nil
}

// candidateFromInput 把手动输入的链接/ID/路径转成候选。
// 返回的字符串是给用户看的错误说明。
func (m *Model) candidateFromInput(input string) (app.LyricCandidate, string) {
	target := strings.TrimSpace(input)
	if target == "" {
		return app.LyricCandidate{}, "请输入链接、ID 或本地路径"
	}
	lower := strings.ToLower(target)
	switch {
	case strings.HasPrefix(lower, "lrclib:"):
		id, err := parsePositiveInt(strings.TrimPrefix(lower, "lrclib:"))
		if err != nil {
			return app.LyricCandidate{}, "LRCLIB 记录 ID 无效"
		}
		return app.LyricCandidate{Kind: store.BindingKindLRCLIB, LRCLIBID: id, Mark: "lrclib"}, ""
	case strings.HasPrefix(lower, "netease:"):
		return app.LyricCandidate{Kind: store.BindingKindSource, Source: "netease",
			ExternalID: target[len("netease:"):], Page: 1, Mark: "netease"}, ""
	case strings.HasPrefix(target, "/") || strings.HasPrefix(target, "~") || strings.HasPrefix(target, "."):
		path, err := app.ExpandPath(target)
		if err != nil {
			return app.LyricCandidate{}, err.Error()
		}
		return app.LyricCandidate{Kind: store.BindingKindLocalFile, Path: path}, ""
	case strings.Contains(target, "bilibili.com") || strings.HasPrefix(lower, "bv"):
		return app.LyricCandidate{}, "暂不支持 B 站字幕作为歌词来源"
	}
	// 纯数字按网易云歌曲 ID 处理（与 u 输入的约定一致）。
	if id, err := parsePositiveInt(target); err == nil {
		return app.LyricCandidate{Kind: store.BindingKindSource, Source: "netease",
			ExternalID: fmt.Sprintf("%d", id), Page: 1, Mark: "netease"}, ""
	}
	// 其余输入交给各音源的 TargetParser：网易云网页链接等由适配器解析。
	if m.sources != nil {
		if ref, matched, err := m.sources.ParseTarget(target, 0); matched {
			if err != nil {
				return app.LyricCandidate{}, err.Error()
			}
			if ref.Source == source.Bilibili {
				return app.LyricCandidate{}, "暂不支持 B 站字幕作为歌词来源"
			}
			if src := m.sources.Get(ref.Source); src != nil {
				return app.LyricCandidate{
					Kind:       store.BindingKindSource,
					Source:     ref.Source,
					ExternalID: ref.ExternalID,
					Page:       ref.Page,
					CID:        ref.CID,
					Mark:       string(ref.Source),
				}, ""
			}
		}
	}
	return app.LyricCandidate{}, "无法识别的输入：支持网易云链接/ID、lrclib:ID 或本地路径"
}

func parsePositiveInt(s string) (int64, error) {
	var v int64
	for _, r := range strings.TrimSpace(s) {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number")
		}
		v = v*10 + int64(r-'0')
		if v > 1<<40 {
			return 0, fmt.Errorf("too large")
		}
	}
	if v <= 0 {
		return 0, fmt.Errorf("not a number")
	}
	return v, nil
}

// selected 返回当前聚焦的候选。
func (b *lyricBindState) selected() (app.LyricCandidate, bool) {
	if b.idx < 0 || b.idx >= len(b.cands) {
		return app.LyricCandidate{}, false
	}
	return b.cands[b.idx], true
}

// cycleLyricBindFilter 在 全部 / 网易云 / LRCLIB 之间切换并重新搜索。
func (m *Model) cycleLyricBindFilter() (tea.Model, tea.Cmd) {
	b := m.lyricBind
	switch b.filter {
	case "":
		b.filter = "netease"
	case "netease":
		b.filter = "lrclib"
	default:
		b.filter = ""
	}
	b.idx = 0
	// 切源后回到候选列表：否则会停在旧的预览上，enter 绑的还是上一个来源。
	b.stage = bindStageSearch
	return m, m.searchLyricCandidatesCmd(b.song, b.query, b.filter)
}

// handleLyricBindMsg 处理浮层相关的异步消息；返回 handled=false 表示不是它的消息。
func (m *Model) handleLyricBindMsg(msg tea.Msg) (tea.Cmd, bool) {
	b := m.lyricBind
	switch v := msg.(type) {
	case lyricCandidatesMsg:
		if b == nil || b.song.ID != v.songID {
			return nil, true
		}
		b.busy = false
		b.cands = v.candidates
		// 用户可能已经在改搜索词：迟到的响应只更新候选，不覆盖正在编辑的输入。
		if !b.editing {
			b.query = v.query
			b.cursor = len([]rune(v.query))
		}
		b.filter = v.filter
		if b.idx >= len(b.cands) {
			b.idx = 0
		}
		b.err = ""
		if v.err != nil {
			b.err = v.err.Error()
		}
		return nil, true
	case lyricPreviewMsg:
		if b == nil || b.song.ID != v.songID {
			return nil, true
		}
		b.busy = false
		if v.err != nil {
			b.err = v.err.Error()
			return nil, true
		}
		b.preview = v.preview
		b.stage = bindStagePreview
		b.err = ""
		return nil, true
	case lyricBindDoneMsg:
		if b != nil && b.song.ID == v.songID {
			b.busy = false
		}
		if v.err != nil {
			if b != nil {
				b.err = v.err.Error()
			} else {
				m.setError(v.err)
			}
			return nil, true
		}
		m.lyricBind = nil
		if v.cleared {
			m.setNotice("已解除歌词绑定，回到自动来源", false)
		} else {
			m.setNotice("已绑定歌词来源："+v.label, false)
		}
		if m.current != nil && m.current.ID == v.songID {
			return m.lyricsCmd(*m.current), true
		}
		return nil, true
	}
	return nil, false
}

// bindLocalSongsMsg 是「选本机曲目」列表的异步结果。
type bindLocalSongsMsg struct {
	songs []store.Song
	err   error
}

// lyricBindLines 渲染歌词来源浮层。
func (m *Model) lyricBindLines() []string {
	b := m.lyricBind
	if b == nil {
		return nil
	}
	width := m.width - 6
	if width > 72 {
		width = 72
	}
	if width < 24 {
		width = max(16, m.width-2)
	}
	inner := width - 2
	title := "歌词来源 · " + truncate(b.song.Title+" · "+b.song.Artist, width-12)
	out := []string{boxTop(title, width)}

	current := "自动"
	if m.current != nil && m.current.ID == b.song.ID {
		if m.lyricSource != "" {
			current = m.lyricSource
		}
	}
	out = append(out, boxLine(st.muted.Render(truncate(
		fmt.Sprintf("当前：%s · 偏移 %s", current, signedSeconds(m.lyricOffsetMS)), inner-1)), inner))

	switch b.stage {
	case bindStagePreview:
		label := b.preview.Label
		if label == "" {
			label = "歌词预览"
		}
		out = append(out, boxLine(st.accent.Render(truncate(
			fmt.Sprintf("%s · 共 %d 行", label, b.preview.Count), inner-1)), inner))
		out = append(out, boxLine(st.dim.Render(strings.Repeat("─", max(1, inner-1))), inner))
		for i, line := range b.preview.Lines {
			if i >= 3 {
				break
			}
			out = append(out, boxLine(truncate(formatLyricLine(line), inner-1), inner))
		}
		if b.busy {
			out = append(out, boxLine(st.muted.Render("正在绑定…"), inner))
		}
		if b.err != "" {
			out = append(out, boxLine(st.err.Render(truncate(b.err, inner-1)), inner))
		}
		out = append(out, boxLine(st.dim.Render("enter 确认绑定 · esc 返回候选项"), inner))
	case bindStageInput:
		out = append(out, boxLine(st.dim.Render(b.inputHint), inner))
		out = append(out, boxLine(st.text.Render("> "+b.input+"▏"), inner))
		if b.err != "" {
			out = append(out, boxLine(st.err.Render(truncate(b.err, inner-1)), inner))
		}
		out = append(out, boxLine(st.dim.Render("enter 预览 · esc 返回"), inner))
	case bindStageLocalSongs:
		out = append(out, boxLine(st.dim.Render("选择本机曲目：用它旁边的 .lrc 或内嵌歌词"), inner))
		if len(b.localSongs) == 0 {
			out = append(out, boxLine(st.muted.Render("曲库里还没有本地曲目，先添加文件夹歌单"), inner))
		}
		start, end := windowAround(len(b.localSongs), b.localIdx, 8)
		for i := start; i < end; i++ {
			song := b.localSongs[i]
			line := truncate(song.Title+" · "+song.Artist, inner-1)
			if i == b.localIdx {
				out = append(out, boxLine(st.selected.Render(padRight(line, inner-1)), inner))
			} else {
				out = append(out, boxLine(line, inner))
			}
		}
		out = append(out, boxLine(st.dim.Render("↑/↓ 选择 · enter 预览 · u 输入路径 · esc 返回"), inner))
	default:
		// 编辑态把光标按真实位置插进搜索词中间，而不是永远追加在末尾。
		input := "搜索 " + b.query
		if b.editing {
			runes := []rune(b.query)
			pos := clampInt(b.cursor, 0, len(runes))
			input = "搜索 " + string(runes[:pos]) + "▏" + string(runes[pos:])
			out = append(out, boxLine(st.text.Render(truncate(input, inner-1)), inner))
		} else {
			out = append(out, boxLine(st.muted.Render(truncate(input+"  ·  / 编辑", inner-1)), inner))
		}
		out = append(out, boxLine(st.dim.Render(b.filterTabs(inner)), inner))
		out = append(out, boxLine(st.dim.Render(strings.Repeat("─", max(1, inner-1))), inner))
		if b.busy {
			out = append(out, boxLine(st.muted.Render("搜索中…"), inner))
		}
		if b.err != "" {
			out = append(out, boxLine(st.err.Render(truncate(b.err, inner-1)), inner))
		}
		if len(b.cands) == 0 && !b.busy {
			out = append(out, boxLine(st.muted.Render("没有候选；改用 / 改搜索词，或按 l / u 指定本地文件"), inner))
		}
		start, end := windowAround(len(b.cands), b.idx, 8)
		for i := start; i < end; i++ {
			out = append(out, boxLine(m.renderLyricCandidate(b.cands[i], i == b.idx, inner), inner))
		}
		if b.editing {
			out = append(out, boxLine(st.dim.Render(truncate("enter 搜索 · ←/→ 移动 · ctrl+u 清空 · esc 退出编辑", inner-1)), inner))
		} else {
			out = append(out, boxLine(st.dim.Render(truncate("enter 绑定并预览 · / 改搜索词 · tab 切源 · l 本地 · u 链接/ID · x 解除", inner-1)), inner))
		}
	}
	out = append(out, boxBottom(width))
	return out
}

// filterTabs 渲染来源筛选页签。
func (b *lyricBindState) filterTabs(inner int) string {
	tabs := []struct {
		value string
		label string
	}{{"", "全部"}, {"netease", "网易云"}, {"lrclib", "LRCLIB"}}
	parts := make([]string, 0, len(tabs))
	for _, tab := range tabs {
		if b.filter == tab.value {
			parts = append(parts, "["+tab.label+"]")
			continue
		}
		parts = append(parts, " "+tab.label+" ")
	}
	return truncate("来源 "+strings.Join(parts, " "), inner-1)
}

// renderLyricCandidate 渲染一条候选项：标记 + 标题·歌手 + 时长 + 状态。
func (m *Model) renderLyricCandidate(c app.LyricCandidate, focused bool, inner int) string {
	mark := c.Mark
	if mark == "" {
		mark = "·"
	}
	badge := ""
	if c.Kind == store.BindingKindLRCLIB {
		if c.Known && !c.HasSynced {
			badge = "无时间轴"
		} else if c.HasSynced {
			badge = "同步歌词"
		}
	}
	left := fmt.Sprintf(" ● %-4s %s · %s", mark, c.Title, c.Artist)
	if c.Duration > 0 {
		left += "  " + formatDuration(time.Duration(c.Duration)*time.Second)
	}
	if badge != "" {
		left += "  [" + badge + "]"
	}
	line := truncate(left, inner-1)
	if focused {
		return st.selected.Render(padRight(line, inner-1))
	}
	if badge == "无时间轴" {
		return st.dim.Render(line)
	}
	return line
}

// formatLyricLine 把一行歌词渲染成预览文本。
func formatLyricLine(line lyrics.Line) string {
	ms := line.StartMS
	text := fmt.Sprintf("[%02d:%02d.%02d] %s", ms/60000, ms/1000%60, ms%1000/10, line.Text)
	if line.Translation != "" {
		text += " / " + line.Translation
	}
	return text
}

// windowAround 返回以 idx 为中心、最多 limit 行的窗口。
func windowAround(total, idx, limit int) (int, int) {
	if total <= limit {
		return 0, total
	}
	start := idx - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > total {
		start = total - limit
	}
	return start, start + limit
}
