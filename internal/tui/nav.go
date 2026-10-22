package tui

import (
	tea "charm.land/bubbletea/v2"
)

// navPage 是页面菜单与 Tab 轮转的一项。搜索拆成两个独立页面，
// 通过 view + searchMode 区分；混合歌单编辑器只是临时视图，不在这里。
type navPage struct {
	name string
	view view
	mode searchMode
}

var navPages = []navPage{
	{name: "曲库", view: viewLibrary},
	{name: "本地搜索", view: viewSearch, mode: searchLocalPlaylist},
	{name: "音源搜索", view: viewSearch, mode: searchSource},
	{name: "队列", view: viewQueue},
	{name: "歌词", view: viewLyrics},
	{name: "帮助", view: viewHelp},
	{name: "登录", view: viewLogin},
	{name: "导入歌单", view: viewFolders},
	{name: "下载", view: viewDownloads},
}

// currentPage 返回当前页面在 navPages 中的下标；混合歌单编辑器返回 -1。
func (m *Model) currentPage() int {
	for i, p := range navPages {
		if p.view != m.view {
			continue
		}
		if m.view != viewSearch {
			return i
		}
		if (p.mode == searchSource) == (m.searchMode == searchSource) {
			return i
		}
	}
	return -1
}

// openPage 打开页面菜单中的一项。
func (m *Model) openPage(idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(navPages) {
		return m, nil
	}
	m.menu = nil
	p := navPages[idx]
	if p.view == viewSearch {
		if p.mode == searchSource {
			m.openSourceSearch()
		} else {
			m.openLocalSearch(searchLocalPlaylist)
		}
		return m, nil
	}
	m.view = p.view
	return m, m.onViewChanged()
}

func (m *Model) nextView() view {
	idx := m.currentPage()
	if idx < 0 {
		return viewLibrary
	}
	return navPages[(idx+1)%len(navPages)].view
}

// openPageMenu 在页头下方打开页面菜单。
func (m *Model) openPageMenu(x, y int) {
	items := make([]menuItem, 0, len(navPages))
	current := m.currentPage()
	for i, p := range navPages {
		items = append(items, menuItem{
			label:   p.name,
			hint:    pageNumber(i),
			act:     actPage,
			index:   i,
			current: i == current,
		})
	}
	m.menu = newMenu(menuPage, "页面", items, x, y, m.width)
}

func pageNumber(i int) string {
	if i < 9 {
		return string(rune('1' + i))
	}
	return ""
}
