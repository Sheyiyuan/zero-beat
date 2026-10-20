package tui

import tea "charm.land/bubbletea/v2"

// isKey 判断按键是否为某个可打印字符。
func isKey(key tea.Key, s string) bool {
	if len(s) == 1 {
		return key.Text == s
	}
	return key.Keystroke() == s
}

// isCtrl 判断是否按下了 ctrl+某键。
func isCtrl(key tea.Key, r rune) bool {
	return key.Mod.Contains(tea.ModCtrl) && key.Code == r
}

// helpEntry 是帮助页的一行。
type helpEntry struct {
	keys string
	desc string
}

var helpSections = []struct {
	title   string
	entries []helpEntry
}{
	{"播放", []helpEntry{
		{"enter", "播放选中项（曲库/搜索/队列）"},
		{"space", "暂停 / 继续"},
		{"n / p", "下一首 / 上一首"},
		{"← / →", "后退 / 前进 5 秒"},
		{"+ / -", "音量 ±5%"},
		{"[ / ]", "音量补偿 ∓1 dB"},
		{"m", "切换播放模式（顺序/单曲/随机）"},
	}},
	{"浏览", []helpEntry{
		{"tab", "切换视图"},
		{"1 - 6", "直接跳到曲库/搜索/队列/歌词/帮助/登录"},
		{"j / k", "上下移动"},
		{"h / l", "切换歌单（曲库视图）"},
		{"r", "刷新当前列表"},
		{"a", "把选中项加入当前歌单"},
		{"A", "把选中项加入播放队列"},
		{"d", "从当前歌单/队列移除"},
	}},
	{"其它", []helpEntry{
		{"/", "搜索 B 站"},
		{"L", "扫码登录"},
		{"?", "打开/关闭帮助"},
		{"esc", "返回曲库"},
		{"q / ctrl+c", "退出"},
	}},
}

// hintLine 返回底部快捷键提示。
func (m *Model) hintLine() string {
	switch m.view {
	case viewSearch:
		if m.searchEditing {
			return "输入关键词 · enter 搜索 · esc 取消"
		}
		return "enter 播放 · a 加入歌单 · A 加入队列 · / 重新搜索 · esc 返回"
	case viewQueue:
		return "enter 播放 · d 移除 · j/k 移动 · tab 切换视图"
	case viewLibrary:
		return "enter 播放 · h/l 切换歌单 · a 加入歌单 · d 移除 · / 搜索 · tab 切换"
	case viewLogin:
		return "r 重新获取二维码 · esc 返回 · q 退出"
	default:
		return "tab 切换视图 · space 暂停 · n/p 切歌 · ? 帮助 · q 退出"
	}
}
