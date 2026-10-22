package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/source"
)

type paletteMode int

const (
	paletteCommands paletteMode = iota
	paletteName
)

type paletteAction string

const (
	palBlank      paletteAction = "blank"
	palImport     paletteAction = "import"
	palMix        paletteAction = "mix"
	palNewMix     paletteAction = "new-mix"
	palEditMix    paletteAction = "edit-mix"
	palUnlock     paletteAction = "unlock"
	palAddCurrent paletteAction = "add-current"
	palLocalFirst paletteAction = "local-first"
)

type paletteItem struct {
	action paletteAction
	label  string
	desc   string
}

type paletteState struct {
	mode   paletteMode
	input  string
	idx    int
	action paletteAction
}

func (m *Model) openPalette() {
	m.palette = &paletteState{mode: paletteCommands}
}

func (m *Model) paletteItems() []paletteItem {
	localState := "开启"
	if !m.localFirst {
		localState = "关闭"
	}
	items := []paletteItem{
		{palBlank, "新建空白歌单", "创建一个本地歌单"},
		{palMix, "新建混合歌单", "引用多个歌单，自己加歌不影响源歌单"},
		{palAddCurrent, "将正在播放加入歌单", "选择本地、混合或同源云端歌单"},
		{palLocalFirst, "本地优先：" + localState, "已下载歌曲优先播放本地文件"},
	}
	if m.sources != nil {
		for _, src := range m.sources.All() {
			if !src.Caps().Has(source.CapRemotePlaylists) {
				continue
			}
			items = append(items, paletteItem{
				action: paletteAction("import:" + string(src.ID())),
				label:  "同步 " + src.Name() + " 歌单/收藏夹",
				desc:   "按需加入并保持同步",
			})
		}
	}

	if p, ok := m.currentPlaylist(); ok {
		switch p.Kind {
		case "remote":
			if !app.IsDailyPlaylist(p) {
				items = append(items, paletteItem{palUnlock, "解锁当前同步歌单", "保留曲目，断开远端同步"})
			}
		case "mixin":
			items = append(items, paletteItem{palEditMix, "编辑当前混合歌单", "增删引用的歌单"})
		}
	}
	return items
}

func (m *Model) filteredPaletteItems() []paletteItem {
	items := m.paletteItems()
	q := strings.ToLower(strings.TrimSpace(m.palette.input))
	if q == "" {
		return items
	}
	out := make([]paletteItem, 0, len(items))
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.label+" "+item.desc), q) {
			out = append(out, item)
		}
	}
	return out
}

func (m *Model) handlePaletteKey(key tea.Key) (tea.Model, tea.Cmd) {
	p := m.palette
	if p == nil {
		return m, nil
	}
	if isCtrl(key, 'c') {
		m.shutdown()
		return m, tea.Quit
	}
	switch key.Code {
	case tea.KeyEscape:
		if p.mode == paletteName {
			p.mode = paletteCommands
			p.input = ""
			p.idx = 0
			return m, nil
		}
		m.palette = nil
		return m, nil
	case tea.KeyEnter:
		if p.mode == paletteName {
			return m, m.commitPaletteName()
		}
		items := m.filteredPaletteItems()
		if len(items) == 0 {
			return m, nil
		}
		if p.idx >= len(items) {
			p.idx = len(items) - 1
		}
		return m, m.runPaletteAction(items[p.idx].action)
	case tea.KeyUp:
		if p.mode == paletteCommands {
			items := m.filteredPaletteItems()
			if len(items) > 0 {
				p.idx = (p.idx + len(items) - 1) % len(items)
			}
		}
		return m, nil
	case tea.KeyDown:
		if p.mode == paletteCommands {
			items := m.filteredPaletteItems()
			if len(items) > 0 {
				p.idx = (p.idx + 1) % len(items)
			}
		}
		return m, nil
	case tea.KeyBackspace:
		if r := []rune(p.input); len(r) > 0 {
			p.input = string(r[:len(r)-1])
		}
		p.idx = 0
		return m, nil
	}
	if isCtrl(key, 'u') {
		p.input = ""
		p.idx = 0
		return m, nil
	}
	if key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) == 0 {
		p.input += key.Text
		p.idx = 0
	}
	return m, nil
}

func (m *Model) runPaletteAction(action paletteAction) tea.Cmd {
	m.palette = nil
	if strings.HasPrefix(string(action), string(palImport)+":") {
		return m.openFolders(source.ID(strings.TrimPrefix(string(action), string(palImport)+":")))
	}
	switch action {
	case palBlank:
		m.palette = &paletteState{mode: paletteName, action: palBlank}
		return nil
	case palMix:
		return m.openMixEditor("")
	case palEditMix:
		if p, ok := m.currentPlaylist(); ok {
			return m.openMixEditor(p.ID)
		}
		return nil
	case palUnlock:
		if p, ok := m.currentPlaylist(); ok && p.Kind == "remote" {
			m.beginPrompt(promptUnlockPlaylist)
		}
		return nil
	case palAddCurrent:
		if m.current == nil {
			m.setNotice("当前没有正在播放的歌曲", true)
			return nil
		}
		return m.openAddSongPicker(*m.current)
	case palLocalFirst:
		m.localFirst = !m.localFirst
		m.service().SaveLocalFirst(m.localFirst)
		state := "关闭"
		if m.localFirst {
			state = "开启"
		}
		m.setNotice("本地优先已"+state+"（下一首生效）", false)
		return nil
	}
	return nil
}

func (m *Model) commitPaletteName() tea.Cmd {
	if m.store == nil || m.palette == nil {
		m.palette = nil
		return nil
	}
	title := strings.TrimSpace(m.palette.input)
	action := m.palette.action
	m.palette = nil
	if title == "" {
		m.setNotice("歌单名不能为空", true)
		return nil
	}
	switch action {
	case palBlank:
		p, err := m.service().CreatePlaylist(title)
		if err != nil {
			m.setError(err)
			return nil
		}
		m.pendingPlaylistID = p.ID
		m.view = viewLibrary
		m.setNotice("已新建歌单："+p.Title, false)
		return m.loadLocalPlaylistsCmd()
	case palNewMix:
		if m.mix == nil {
			m.setNotice("混合歌单编辑状态已丢失", true)
			return nil
		}
		refs := m.mixSelectedIDs()
		if len(refs) == 0 {
			m.setNotice("至少选择一个要引用的歌单", true)
			return nil
		}
		p, err := m.service().CreateMixin(title, refs)
		if err != nil {
			m.setError(err)
			return nil
		}
		m.pendingPlaylistID = p.ID
		m.mix = nil
		m.view = viewLibrary
		m.setNotice("已新建混合歌单："+p.Title, false)
		return m.loadLocalPlaylistsCmd()
	}
	return nil
}

func (m *Model) paletteLines() []string {
	p := m.palette
	if p == nil {
		return nil
	}
	width := m.width - 6
	if width > 64 {
		width = 64
	}
	if width < 24 {
		width = max(16, m.width-2)
	}
	inner := width - 2

	title := "命令"
	if p.mode == paletteName {
		title = "歌单名称"
		if p.action == palNewMix {
			title = "混合歌单名称"
		}
	}
	out := []string{boxTop(title, width)}
	input := "> " + p.input + "▏"
	out = append(out, boxLine(input, inner))
	if p.mode == paletteName {
		out = append(out, boxLine(st.dim.Render("enter 创建 · esc 返回"), inner))
	} else {
		items := m.filteredPaletteItems()
		if len(items) == 0 {
			out = append(out, boxLine(st.dim.Render("没有匹配的命令"), inner))
		}
		if p.idx >= len(items) {
			p.idx = max(0, len(items)-1)
		}
		for i, item := range items {
			line := "▸ " + item.label
			if item.desc != "" {
				line += "  " + st.dim.Render(item.desc)
			}
			if i == p.idx {
				out = append(out, boxLine(st.selected.Render(padRight(line, inner-1)), inner))
			} else {
				out = append(out, boxLine(line, inner))
			}
		}
		out = append(out, boxLine(st.dim.Render("↑/↓ 选择 · enter 确认 · esc 关闭"), inner))
	}
	out = append(out, boxBottom(width))
	return out
}

func boxTop(title string, width int) string {
	label := " " + title + " "
	dashes := width - 2 - visWidth(label)
	if dashes < 0 {
		dashes = 0
	}
	return st.accent.Render("╭" + label + strings.Repeat("─", dashes) + "╮")
}

func boxBottom(width int) string {
	n := width - 2
	if n < 0 {
		n = 0
	}
	return st.accent.Render("╰" + strings.Repeat("─", n) + "╯")
}

func boxLine(content string, inner int) string {
	return st.accent.Render("│") + padRight(" "+content, inner) + st.accent.Render("│")
}

func (m *Model) overlayPalette(frame []string) []string {
	return overlayBox(frame, m.paletteLines(), m.width, m.height)
}

func overlayBox(frame []string, box []string, width, height int) []string {
	if len(box) == 0 || len(frame) == 0 {
		return frame
	}
	bw := 0
	for _, line := range box {
		if w := visWidth(line); w > bw {
			bw = w
		}
	}
	if bw > width {
		bw = width
	}
	left := (width - bw) / 2
	top := (height - len(box)) / 2
	if left < 0 {
		left = 0
	}
	if top < 0 {
		top = 0
	}
	return overlayBoxAt(frame, box, left, top, width, height)
}

// overlayBoxAt 在指定左上角覆盖一个等宽浮层。
func overlayBoxAt(frame []string, box []string, left, top, width, height int) []string {
	if len(box) == 0 || len(frame) == 0 {
		return frame
	}
	bw := 0
	for _, line := range box {
		if w := visWidth(line); w > bw {
			bw = w
		}
	}
	if bw > width {
		bw = width
	}
	for i, line := range box {
		y := top + i
		if y < 0 || y >= len(frame) {
			continue
		}
		base := padRight(frame[y], width)
		before := padRight(ansi.Cut(base, 0, left), left)
		after := ""
		if left+bw < width {
			after = ansi.Cut(base, left+bw, width)
		}
		frame[y] = before + padRight(line, bw) + after
	}
	return frame
}
