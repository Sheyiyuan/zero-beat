package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
)

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

func (m *Model) helpKeys(actions ...keymap.Action) string {
	parts := make([]string, 0, len(actions))
	for _, a := range actions {
		if label := m.keyLabel(a); label != "" {
			parts = append(parts, label)
		}
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " / ")
}

func (m *Model) firstKey(a keymap.Action) string {
	var keys []string
	if m.keys == nil {
		keys = defaultKeys.Keys(a)
	} else {
		keys = m.keys.Keys(a)
	}
	if len(keys) == 0 {
		return "—"
	}
	return keys[0]
}

func joinKeys(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" && p != "—" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return "—"
	}
	return strings.Join(out, " / ")
}

// helpRow 是帮助页的一行：section 非空表示分节标题，否则为按键 + 说明。
type helpRow struct {
	section string
	keys    string
	desc    string
}

// helpLines 返回帮助页的完整内容；由 renderHelp 按窗口裁剪。
//
// 先收集再渲染：按键列宽按最长组合自适应（下限 12、上限 26），
// 避免「ctrl+u / PgUp / ctrl+d / PgDn」这类组合被固定列宽截断。
func (m *Model) helpLines() []string {
	var rows []helpRow
	add := func(keys, desc string) {
		rows = append(rows, helpRow{keys: keys, desc: desc})
	}
	section := func(title string) {
		rows = append(rows, helpRow{section: title})
	}

	section("播放")
	add(joinKeys(m.helpKeys(keymap.PlaySelected), "enter"), "播放选中项（曲库/搜索/队列）")
	add(m.helpKeys(keymap.PlayPause), "暂停 / 继续；混合歌单内为勾选引用")
	add(joinKeys(m.helpKeys(keymap.Previous), m.helpKeys(keymap.Next)), "上一首 / 下一首")
	add(joinKeys(m.helpKeys(keymap.SeekBack1), m.helpKeys(keymap.SeekBack5)), "快退 1 秒 / 5 秒")
	add(joinKeys(m.helpKeys(keymap.SeekFwd5), m.helpKeys(keymap.SeekFwd10)), "快进 5 秒 / 10 秒")
	add(m.helpKeys(keymap.EditClip), "编辑当前歌曲片段（{ / } 设起止，enter 保存）")
	add(joinKeys(m.helpKeys(keymap.VolumeDown), m.helpKeys(keymap.VolumeUp)), "音量 ∓5%")
	add(joinKeys(m.helpKeys(keymap.CompDown), m.helpKeys(keymap.CompUp)), "音量补偿 ∓1 dB")
	add(joinKeys(m.helpKeys(keymap.Mode), m.helpKeys(keymap.Mute)), "播放模式 / 静音")
	add(joinKeys(m.helpKeys(keymap.Quality), m.helpKeys(keymap.Sleep)), "音质偏好 / 睡眠定时")
	add(joinKeys(m.helpKeys(keymap.OffsetDown), m.helpKeys(keymap.OffsetUp), m.helpKeys(keymap.OffsetReset)), "歌词偏移 0.5 秒 / 归零（任意视图）")

	section("浏览")
	add("Tab / Shift+Tab", "正向 / 反向轮转全部视图（也可点页头页面名打开菜单）")
	add("1-9", "直达曲库、本地搜索、音源搜索、队列、歌词、帮助、登录、导入歌单、下载")
	add(joinKeys(m.helpKeys(keymap.Down), m.helpKeys(keymap.Up)), "上下移动")
	add(joinKeys(m.helpKeys(keymap.Top), m.helpKeys(keymap.Bottom)), "跳到列表顶部 / 底部")
	add(joinKeys(m.helpKeys(keymap.PageUp), m.helpKeys(keymap.PageDown)), "向上 / 向下翻页")
	add(joinKeys(m.helpKeys(keymap.Left), m.helpKeys(keymap.Right)), "切换歌单 / 歌曲操作焦点（曲库）")
	add(m.helpKeys(keymap.QueueView), "打开播放列表")
	add(m.helpKeys(keymap.LyricsView), "打开歌词")
	add(m.helpKeys(keymap.New), "打开命令面板（新建歌单 / 关于等）")
	add(m.helpKeys(keymap.Folders), "浏览并导入远端歌单")
	add(m.helpKeys(keymap.Downloads), "打开下载管理（播放/重新下载/取消/删除/查看缺失文件）")
	add(joinKeys(m.helpKeys(keymap.MixEdit), m.helpKeys(keymap.Unlock)), "编辑混合歌单引用 / 解锁同步歌单（歌单焦点）")
	add(m.helpKeys(keymap.MergePlaylist), "把当前歌单以引用混入混合歌单（曲库）")
	add(joinKeys(m.helpKeys(keymap.PlayNext), m.helpKeys(keymap.Enqueue)), "插到下一首播放 / 追加到队列末尾（歌曲焦点）")
	add(joinKeys(m.helpKeys(keymap.AddPlaylist), m.helpKeys(keymap.Remove)), "加入指定歌单（本地/混合/同源云端）/ 移除")
	add(joinKeys(m.helpKeys(keymap.Download), m.helpKeys(keymap.DownloadSelected)), "下载正在播放 / 下载选中（歌曲或整个歌单）")
	add(joinKeys(m.helpKeys(keymap.QueueUp), m.helpKeys(keymap.QueueDown)), "队列内上移 / 下移")
	add(m.helpKeys(keymap.Delete), "清空队列；歌单焦点删除当前歌单")
	add(joinKeys(m.helpKeys(keymap.Rename), m.helpKeys(keymap.Refresh)), "重命名歌单（歌单焦点）/ 刷新当前列表")

	section("鼠标")
	add("左键", "点击按钮、页头或列表项；点底部音质档位切换音质（等同 y）；双击歌曲播放；单击歌词移动焦点、双击跳转")
	add("右键", "歌曲 / 歌单 / 歌词上下文菜单（菜单项旁标注快捷键）")
	add("滚轮", "歌词页滚动；在底部播放状态行上调节音量")
	add("歌词页", "↑/↓/滚轮移动焦点（居中）；enter 跳到该句，esc 回到正在播放的行")

	section("其它")
	add(m.helpKeys(keymap.LocalSearch), "搜索当前歌单；r 切到本地全部")
	add(m.helpKeys(keymap.Search), "搜索音源")
	add(m.helpKeys(keymap.Source), "音源搜索换源；登录页切换登录源")
	add(joinKeys(m.helpKeys(keymap.Login), m.helpKeys(keymap.Logout)), "登录管理 / 退出登录（登录页，W 仍兼容）")
	add(m.helpKeys(keymap.LoginImage), "打开二维码图片（登录页）")
	add(m.helpKeys(keymap.HelpPopup), "打开/关闭快捷键弹窗")
	add(joinKeys(m.helpKeys(keymap.Back), "esc"), "返回曲库 / 取消输入")
	add(joinKeys(m.helpKeys(keymap.Quit), "ctrl+c"), "退出")

	keyW := 12
	for _, row := range rows {
		if w := visWidth(row.keys); w > keyW {
			keyW = w
		}
	}
	if keyW > 26 {
		keyW = 26
	}

	out := []string{st.muted.Render(" 快捷键 · " + m.firstKey(keymap.Down) + " / " + m.firstKey(keymap.Up) + " 滚动 · 配置 ~/.config/zero-beat/keymap.toml")}
	for _, row := range rows {
		if row.section != "" {
			out = append(out, "", st.accent.Render(" "+row.section))
			continue
		}
		out = append(out, "  "+st.text.Render(padRight(row.keys, keyW))+st.muted.Render(row.desc))
	}

	out = append(out, "")
	out = append(out, st.dim.Render(" 配置文件与数据库在 ~/.config/zero-beat/"))
	return out
}

// hintLine 返回底部快捷键提示；完整列表交给 H 弹窗。
func (m *Model) hintLine() string {
	switch m.view {
	case viewSearch:
		if m.searchEditing {
			return "enter 搜索 · esc 取消"
		}
		if m.searchMode == searchSource {
			return "enter 播放 · " + m.firstKey(keymap.Search) + " 编辑 · " + m.firstKey(keymap.DownloadSelected) + " 下载选中 · " + m.firstKey(keymap.Download) + " 下载当前 · " + m.firstKey(keymap.Source) + " 换源 · " + m.firstKey(keymap.HelpPopup) + " 帮助"
		}
		return "enter 播放 · " + m.firstKey(keymap.LocalSearch) + " 编辑 · " + m.firstKey(keymap.DownloadSelected) + " 下载选中 · " + m.firstKey(keymap.Download) + " 下载当前 · r 切换歌单/全部 · " + m.firstKey(keymap.HelpPopup) + " 帮助"
	case viewQueue:
		return "enter 播放 · " + m.firstKey(keymap.DownloadSelected) + " 下载选中 · " + m.firstKey(keymap.Download) + " 下载当前 · " + m.firstKey(keymap.PlayNext) + " 下一首 · " + m.firstKey(keymap.Remove) + " 移除 · " + m.firstKey(keymap.HelpPopup) + " 帮助"
	case viewFolders:
		return "enter 添加 · " + m.firstKey(keymap.Source) + " 切源 · " + m.firstKey(keymap.HelpPopup) + " 帮助"
	case viewMix:
		return "space 勾选 · enter 下一步 · " + m.firstKey(keymap.HelpPopup) + " 帮助"
	case viewDownloads:
		return "enter 播放 · " + m.firstKey(keymap.DownloadSelected) + " 重下 · x 取消 · X 全部取消 · " + m.firstKey(keymap.Remove) + " 移除记录 · " + m.firstKey(keymap.Delete) + " 删文件 · " + m.firstKey(keymap.LoginImage) + " 目录 · " + m.firstKey(keymap.HelpPopup) + " 帮助"
	case viewLibrary:
		if m.resumeAt > 0 && m.state == audio.StatePaused {
			return "space 继续 · enter 播放 · H 帮助"
		}
		if m.libraryFocus == focusPlaylists {
			return "↑/↓ 选歌单 · → 切到歌曲 · enter 播放歌单 · " + m.firstKey(keymap.DownloadSelected) + " 下载歌单 · " + m.firstKey(keymap.HelpPopup) + " 帮助"
		}
		return "↑/↓ 选歌曲 · ← 切到歌单 · enter 播放 · " + m.firstKey(keymap.Download) + " 下载当前 · " + m.firstKey(keymap.DownloadSelected) + " 下载选中 · " + m.firstKey(keymap.HelpPopup) + " 帮助"
	case viewLyrics:
		source := ""
		if m.lyricOffsetMS != 0 {
			source = " · 偏移 " + signedSeconds(m.lyricOffsetMS)
		}
		return "↑/↓ 选句 · enter 跳转 · esc 回播放 · " + m.firstKey(keymap.Download) + " 下载 · " + m.firstKey(keymap.PlayPause) + " 暂停 · " + m.firstKey(keymap.HelpPopup) + " 帮助" + source
	case viewHelp:
		return m.firstKey(keymap.HelpPopup) + " 弹窗 · esc 返回"
	case viewLogin:
		if _, ok := m.userFor(m.loginSource); ok {
			return "↑/↓ 切源 · " + m.firstKey(keymap.Refresh) + " 刷新账号 · W/" + m.firstKey(keymap.Logout) + " 退出 · esc 返回"
		}
		return "↑/↓ 切源 · " + m.firstKey(keymap.Refresh) + " 刷新 · " + m.firstKey(keymap.LoginImage) + " 图片 · esc 返回"
	default:
		return m.firstKey(keymap.HelpPopup) + " 帮助 · " + m.firstKey(keymap.Login) + " 账户 · " + m.firstKey(keymap.New) + " 新建"
	}
}

func signedSeconds(ms int) string {
	return fmt.Sprintf("%+.1fs", float64(ms)/1000)
}
