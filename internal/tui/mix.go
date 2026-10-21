package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

type mixState struct {
	editingID string
	items     []store.Playlist
	selected  map[string]bool
	idx       int
}

func (m *Model) openMixEditor(id string) tea.Cmd {
	if m.store == nil {
		return nil
	}
	if len(m.playlists) == 0 {
		m.setNotice("没有可引用的歌单", true)
		return nil
	}
	selected := map[string]bool{}
	items := make([]store.Playlist, 0, len(m.playlists))
	for _, p := range m.playlists {
		if p.Kind != "mixin" {
			items = append(items, p)
		}
	}
	if id != "" {
		refs, err := m.store.MixinRefs(id)
		if err != nil {
			m.setError(err)
			return nil
		}
		// 保留原引用顺序，新勾选的引用按列表顺序追加。
		ordered := make([]store.Playlist, 0, len(items))
		for _, ref := range refs {
			for _, p := range items {
				if p.ID == ref.ID {
					selected[p.ID] = true
					ordered = append(ordered, p)
					break
				}
			}
		}
		for _, p := range items {
			if !selected[p.ID] {
				ordered = append(ordered, p)
			}
		}
		items = ordered
	}
	if len(items) == 0 {
		m.setNotice("没有可引用的歌单", true)
		return nil
	}
	m.mix = &mixState{editingID: id, items: items, selected: selected}
	m.view = viewMix
	return nil
}

func (m *Model) handleMixKey(key tea.Key) (tea.Model, tea.Cmd) {
	if m.mix == nil {
		return m, nil
	}
	switch {
	case isCtrl(key, 'c'):
		m.shutdown()
		return m, tea.Quit
	case key.Code == tea.KeyEscape || m.match(keymap.Back, key):
		m.mix = nil
		m.view = viewLibrary
		return m, nil
	case m.match(keymap.Up, key):
		if m.mix.idx > 0 {
			m.mix.idx--
		}
		return m, nil
	case m.match(keymap.Down, key):
		if m.mix.idx < len(m.mix.items)-1 {
			m.mix.idx++
		}
		return m, nil
	case key.Code == tea.KeySpace || key.Text == " ":
		if m.mix.idx >= 0 && m.mix.idx < len(m.mix.items) {
			id := m.mix.items[m.mix.idx].ID
			m.mix.selected[id] = !m.mix.selected[id]
		}
		return m, nil
	case key.Code == tea.KeyEnter || isKey(key, "n"):
		return m, m.commitMix()
	case m.match(keymap.Top, key):
		m.mix.idx = 0
		return m, nil
	case m.match(keymap.Bottom, key):
		m.mix.idx = max(0, len(m.mix.items)-1)
		return m, nil
	}
	return m, nil
}

func (m *Model) mixSelectedIDs() []string {
	if m.mix == nil {
		return nil
	}
	out := make([]string, 0, len(m.mix.selected))
	for _, p := range m.mix.items {
		if m.mix.selected[p.ID] {
			out = append(out, p.ID)
		}
	}
	return out
}

func (m *Model) commitMix() tea.Cmd {
	if m.mix == nil || m.store == nil {
		return nil
	}
	refs := m.mixSelectedIDs()
	if len(refs) == 0 {
		m.setNotice("至少选择一个要引用的歌单", true)
		return nil
	}
	if m.mix.editingID != "" {
		id := m.mix.editingID
		if err := m.store.SetMixinRefs(id, refs); err != nil {
			m.setError(err)
			return nil
		}
		m.mix = nil
		m.pendingPlaylistID = id
		m.view = viewLibrary
		m.setNotice("已更新混合歌单引用", false)
		return m.loadLocalPlaylistsCmd()
	}
	m.palette = &paletteState{mode: paletteName, action: palNewMix}
	return nil
}

func (m *Model) renderMix(h int) []string {
	out := make([]string, 0, h)
	title := " 新建混合歌单 · 引用多个歌单，自己的改动不写回源歌单"
	if m.mix != nil && m.mix.editingID != "" {
		title = " 编辑混合歌单引用 · 自己的改动不写回源歌单"
	}
	out = append(out, st.muted.Render(truncate(title, m.width)))
	if m.mix == nil {
		return out
	}
	start, end := windowRange(len(m.mix.items), m.mix.idx, h-2)
	for i := start; i < end; i++ {
		p := m.mix.items[i]
		check := "[ ]"
		if m.mix.selected[p.ID] {
			check = "[x]"
		}
		marker := "  "
		if i == m.mix.idx {
			marker = "▶ "
		}
		src := "本地"
		if p.Source != "" {
			src = source.ID(p.Source).Label()
		}
		titleW := m.width - 18
		if titleW < 8 {
			titleW = 8
		}
		row := marker + check + " " + st.dim.Render(padRight(src, 8)) +
			padRight(p.Title, titleW) + st.muted.Render(fmt.Sprintf("%4d", p.Count))
		row = padRight(row, m.width)
		if i == m.mix.idx {
			out = append(out, st.selected.Render(row))
		} else {
			out = append(out, row)
		}
	}
	out = append(out, st.dim.Render(" space 勾选 · enter 下一步 · j/k 移动 · esc 返回"))
	return out
}
