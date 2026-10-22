package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

type targetPickerMode int

const (
	targetPickerAddSong targetPickerMode = iota
	targetPickerMerge
)

// playlistTarget 是目标歌单浮层里的一行。Cloud 为 true 时写远端，
// 否则写本地 SQLite（local/mixin）。
type playlistTarget struct {
	ID       string
	Title    string
	Source   string
	RemoteID string
	Kind     string
	Count    int
	Cloud    bool
	Create   bool
}

type targetPickerState struct {
	mode             targetPickerMode
	song             store.Song
	sourcePlaylistID string
	sourceTitle      string

	items  []playlistTarget
	idx    int
	filter string

	remoteBusy bool
	remoteErr  string
	err        string
}

type targetRemotesMsg struct {
	source source.ID
	items  []source.Playlist
	err    error
}

type playlistAddMsg struct {
	target playlistTarget
	song   store.Song
	err    error
}

func (m *Model) openAddSongPicker(song store.Song) tea.Cmd {
	song = store.NormalizeSong(song)
	if song.ID == "" {
		m.setNotice("这首歌缺少来源信息，无法加入歌单", true)
		return nil
	}
	p := &targetPickerState{mode: targetPickerAddSong, song: song}
	m.picker = p
	p.items = m.localTargets(false, "")
	p.items = m.preferCurrentTarget(p.items)
	p.normalize()

	var cmd tea.Cmd
	if src := m.sources.GetString(song.Source); src != nil && src.LoggedIn() && src.Caps().Has(source.CapPlaylistWrite) {
		p.remoteBusy = true
		cmd = m.loadTargetRemotesCmd(src.ID())
	}
	return cmd
}

// openMergePicker 把当前选中的歌单以「引用」方式加入另一个混合歌单。
func (m *Model) openMergePicker() tea.Cmd {
	if m.store == nil {
		return nil
	}
	sourcePlaylist, ok := m.currentPlaylist()
	if !ok {
		m.setNotice("先选一个源歌单", true)
		return nil
	}
	if sourcePlaylist.Kind == "mixin" {
		m.setNotice("混合歌单不能作为引用源（不支持嵌套）", true)
		return nil
	}
	if app.IsDailyPlaylist(sourcePlaylist) {
		m.setNotice("「每日推荐」是动态列表，不能作为引用源", true)
		return nil
	}
	items := m.localTargets(true, sourcePlaylist.ID)
	if len(items) == 0 {
		m.setNotice("没有可混入的混合歌单，先按 N 新建一个", true)
		return nil
	}
	m.picker = &targetPickerState{
		mode:             targetPickerMerge,
		sourcePlaylistID: sourcePlaylist.ID,
		sourceTitle:      sourcePlaylist.Title,
		items:            items,
	}
	m.picker.normalize()
	return nil
}

// localTargets 构造本地目标列表。mergeOnly 时只保留混合歌单。
func (m *Model) localTargets(mergeOnly bool, excludeID string) []playlistTarget {
	out := make([]playlistTarget, 0, len(m.playlists))
	for _, p := range m.playlists {
		if p.ID == excludeID {
			continue
		}
		if mergeOnly {
			if p.Kind != "mixin" {
				continue
			}
		} else if p.Kind != "local" && p.Kind != "mixin" {
			continue
		}
		out = append(out, playlistTarget{
			ID:     p.ID,
			Title:  p.Title,
			Source: p.Source,
			Kind:   p.Kind,
			Count:  p.Count,
		})
	}
	if mergeOnly {
		return out
	}
	out = append(out, playlistTarget{Title: "＋ 新建本地歌单并加入", Create: true})
	// 已导入的远端歌单也能直接写云端，先放进来；未导入的等异步列表补齐。
	songSource := m.pickerSongSource()
	src := m.sources.GetString(songSource)
	writableCloud := src != nil && src.LoggedIn() && src.Caps().Has(source.CapPlaylistWrite)
	for _, p := range m.playlists {
		if !writableCloud || p.Kind != "remote" || p.RemoteID == "" || app.IsDailyPlaylist(p) || p.Source != songSource {
			continue
		}
		out = append(out, playlistTarget{
			ID:       p.ID,
			Title:    p.Title,
			Source:   p.Source,
			RemoteID: p.RemoteID,
			Kind:     p.Kind,
			Count:    p.Count,
			Cloud:    true,
		})
	}
	sortTargetsCreateLast(out)
	return out
}

func (m *Model) pickerSongSource() string {
	if m.picker != nil && m.picker.song.Source != "" {
		return m.picker.song.Source
	}
	return ""
}

func (m *Model) preferCurrentTarget(items []playlistTarget) []playlistTarget {
	id := m.currentPlaylistID()
	if id == "" && m.store != nil {
		id = m.service().LastAddPlaylist()
	}
	if id == "" {
		return items
	}
	for i, item := range items {
		if item.ID == id {
			copy(items[1:i+1], items[0:i])
			items[0] = item
			return items
		}
	}
	return items
}

func (m *Model) loadTargetRemotesCmd(srcID source.ID) tea.Cmd {
	svc := m.service()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := svc.CloudPlaylists(ctx, srcID)
		return targetRemotesMsg{source: srcID, items: items, err: err}
	}
}

func (m *Model) handlePickerKey(key tea.Key) (tea.Model, tea.Cmd) {
	p := m.picker
	if p == nil {
		return m, nil
	}
	if isCtrl(key, 'c') {
		m.shutdown()
		return m, tea.Quit
	}
	switch key.Code {
	case tea.KeyEscape:
		m.picker = nil
		return m, nil
	case tea.KeyEnter:
		return m, m.chooseTarget()
	case tea.KeyUp:
		if p.idx > 0 {
			p.idx--
		}
		return m, nil
	case tea.KeyDown:
		if p.idx < len(m.filteredTargets())-1 {
			p.idx++
		}
		return m, nil
	case tea.KeyBackspace:
		if r := []rune(p.filter); len(r) > 0 {
			p.filter = string(r[:len(r)-1])
			p.idx = 0
		}
		return m, nil
	}
	if isCtrl(key, 'u') {
		p.filter = ""
		p.idx = 0
		return m, nil
	}
	if key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) == 0 {
		p.filter += key.Text
		p.idx = 0
	}
	return m, nil
}

func (m *Model) filteredTargets() []playlistTarget {
	if m.picker == nil {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(m.picker.filter))
	if q == "" {
		return m.picker.items
	}
	out := make([]playlistTarget, 0, len(m.picker.items))
	for _, item := range m.picker.items {
		if strings.Contains(strings.ToLower(item.Title+" "+item.Kind+" "+item.Source), q) {
			out = append(out, item)
		}
	}
	return out
}

func sortTargetsCreateLast(items []playlistTarget) {
	sort.SliceStable(items, func(i, j int) bool {
		return !items[i].Create && items[j].Create
	})
}

func (p *targetPickerState) normalize() {
	if p.idx < 0 {
		p.idx = 0
	}
	if p.idx >= len(p.items) {
		p.idx = max(0, len(p.items)-1)
	}
}

func (m *Model) chooseTarget() tea.Cmd {
	items := m.filteredTargets()
	if len(items) == 0 {
		return nil
	}
	p := m.picker
	if p == nil {
		return nil
	}
	if p.idx < 0 || p.idx >= len(items) {
		p.idx = clampInt(p.idx, 0, len(items)-1)
	}
	target := items[p.idx]
	switch p.mode {
	case targetPickerMerge:
		return m.commitMergeRef(target)
	default:
		if target.Create {
			song := p.song
			m.picker = nil
			m.pendingAddSong = &song
			m.beginPrompt(promptNewPlaylist)
			return nil
		}
		if target.Cloud {
			return m.addSongToCloudCmd(p.song, target)
		}
		return m.commitLocalAdd(target, p.song)
	}
}

func (m *Model) commitLocalAdd(target playlistTarget, song store.Song) tea.Cmd {
	if m.store == nil {
		return nil
	}
	if err := m.service().AddSongToPlaylist(target.ID, song); err != nil {
		if m.picker != nil {
			m.picker.err = err.Error()
		}
		m.setError(err)
		return nil
	}
	m.picker = nil
	if m.store != nil {
		m.service().SaveLastAddPlaylist(target.ID)
	}
	m.setNotice(fmt.Sprintf("已加入歌单：%s", target.Title), false)
	return m.loadLocalPlaylistsCmd()
}

func (m *Model) addSongToCloudCmd(song store.Song, target playlistTarget) tea.Cmd {
	sources := m.sources
	song = store.NormalizeSong(song)
	if m.picker != nil {
		m.picker = nil
	}
	m.setNotice("正在加入云端歌单…", false)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := app.New(sources, nil, nil).AddToCloudPlaylist(ctx, song, target.RemoteID)
		return playlistAddMsg{target: target, song: song, err: err}
	}
}

func (m *Model) commitMergeRef(target playlistTarget) tea.Cmd {
	if m.store == nil || m.picker == nil {
		return nil
	}
	added, err := m.service().AddMixinRef(target.ID, m.picker.sourcePlaylistID)
	if err != nil {
		m.picker.err = err.Error()
		m.setError(err)
		return nil
	}
	sourceTitle := m.picker.sourceTitle
	m.picker = nil
	if added {
		m.setNotice(fmt.Sprintf("已将「%s」引用到「%s」", sourceTitle, target.Title), false)
	} else {
		m.setNotice(fmt.Sprintf("「%s」已经引用了「%s」", target.Title, sourceTitle), false)
	}
	return m.loadLocalPlaylistsCmd()
}

func (m *Model) targetPickerLines() []string {
	p := m.picker
	if p == nil {
		return nil
	}
	width := m.width - 6
	if width > 72 {
		width = 72
	}
	if width < 30 {
		width = max(20, m.width-2)
	}
	inner := width - 2
	title := "加入歌单 · " + p.song.Title
	if p.mode == targetPickerMerge {
		title = "混入混合歌单 · " + p.sourceTitle
	}
	out := []string{boxTop(truncate(title, width-2), width)}
	input := "> " + p.filter + "▏"
	out = append(out, boxLine(input, inner))

	items := m.filteredTargets()
	if p.idx >= len(items) {
		p.idx = max(0, len(items)-1)
	}
	if len(items) == 0 {
		if p.remoteBusy {
			out = append(out, boxLine(st.dim.Render("正在读取云端歌单…"), inner))
		} else {
			out = append(out, boxLine(st.dim.Render("没有匹配的歌单"), inner))
		}
	} else {
		maxRows := m.height - 8
		if maxRows < 4 {
			maxRows = 4
		}
		if maxRows > 12 {
			maxRows = 12
		}
		start, end := windowRange(len(items), p.idx, maxRows)
		for i := start; i < end; i++ {
			item := items[i]
			marker := "  "
			if i == p.idx {
				marker = "▶ "
			}
			tag := "本地"
			switch {
			case item.Create:
				tag = "新建"
			case item.Cloud:
				tag = m.sourceMark(source.ID(item.Source)) + " 云端"
			case item.Kind == "mixin":
				tag = "混合"
			}
			row := marker + st.dim.Render(padRight("["+tag+"]", 9)) + " " + item.Title
			if item.Count > 0 {
				row += st.muted.Render(fmt.Sprintf("  %d", item.Count))
			}
			row = truncate(row, inner-1)
			if i == p.idx {
				out = append(out, boxLine(st.selected.Render(padRight(row, inner-1)), inner))
			} else {
				out = append(out, boxLine(row, inner))
			}
		}
	}

	if p.remoteBusy {
		out = append(out, boxLine(st.dim.Render("云端歌单加载中…"), inner))
	}
	if p.remoteErr != "" {
		out = append(out, boxLine(st.warn.Render(truncate(p.remoteErr, inner-1)), inner))
	}
	if p.err != "" {
		out = append(out, boxLine(st.err.Render(truncate(p.err, inner-1)), inner))
	}
	if p.mode == targetPickerMerge {
		out = append(out, boxLine(st.dim.Render("enter 添加引用 · 源歌单内容会随源更新"), inner))
	} else {
		out = append(out, boxLine(st.dim.Render("enter 加入 · ↑/↓ 选择 · esc 取消"), inner))
	}
	out = append(out, boxBottom(width))
	return out
}

// songForPlaylistAdd 按当前视图取「要加入歌单」的那首歌：
// 曲库/搜索取选中项，队列取光标项，歌词页取正在播放。
func (m *Model) songForPlaylistAdd() (store.Song, bool) {
	switch m.view {
	case viewLibrary:
		if m.libraryFocus != focusSongs || m.songIdx < 0 || m.songIdx >= len(m.songs) {
			return store.Song{}, false
		}
		return m.songs[m.songIdx], true
	case viewSearch:
		if m.searchMode == searchSource {
			if m.searchIdx < 0 || m.searchIdx >= len(m.searchResults) {
				return store.Song{}, false
			}
			return m.searchResultSong(m.searchIdx), true
		}
		if m.searchIdx < 0 || m.searchIdx >= len(m.localResults) {
			return store.Song{}, false
		}
		return m.localResults[m.searchIdx], true
	case viewQueue:
		if m.queueCursor < 0 || m.queueCursor >= len(m.queue) {
			return store.Song{}, false
		}
		return m.queue[m.queueCursor], true
	case viewLyrics:
		if m.current == nil {
			return store.Song{}, false
		}
		return *m.current, true
	}
	return store.Song{}, false
}
