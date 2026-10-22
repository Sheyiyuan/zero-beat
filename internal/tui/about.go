package tui

import (
	"fmt"
	"runtime"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/keymap"
)

// Version 是界面显示的软件版本，由 cmd/zero 在启动时注入。
var Version = "dev"

func (m *Model) handleAboutKey(key tea.Key) (tea.Model, tea.Cmd) {
	switch {
	case isCtrl(key, 'c'):
		m.shutdown()
		return m, tea.Quit
	case key.Code == tea.KeyEscape || key.Code == tea.KeyEnter || m.match(keymap.Back, key):
		m.aboutPopup = false
	}
	return m, nil
}

func (m *Model) aboutLines() []string {
	width := m.width - 4
	if width > 64 {
		width = 64
	}
	if width < 24 {
		width = max(16, m.width-2)
	}
	inner := width - 2

	field := func(label, value string) string {
		return st.muted.Render(padRight(label, 6)) + " " + st.text.Render(value)
	}
	base := []string{
		st.title.Render("zero-beat") + st.dim.Render(" · 零拍"),
		st.text.Render("B 站 + 网易云音乐终端播放器"),
		field("版本", Version),
		field("作者", "Sheyiyuan"),
		field("许可证", "AGPL-3.0"),
		field("项目", "github.com/Sheyiyuan/zero-beat"),
		field("运行", fmt.Sprintf("%s · %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)),
		field("引擎", "ffmpeg + oto"),
		st.dim.Render("esc / enter 关闭"),
	}

	var rows []string
	switch {
	case m.height >= 16:
		rows = []string{base[0], "", base[1], ""}
		rows = append(rows, base[2:8]...)
		rows = append(rows, "", base[8])
	case m.height >= 12:
		rows = append(rows, base[0], base[1])
		rows = append(rows, base[2:]...)
	case m.height >= 10:
		rows = append(rows, base[0])
		rows = append(rows, base[2:7]...)
		rows = append(rows, base[8])
	default:
		rows = []string{base[0], base[2], base[4], base[5], base[8]}
	}

	out := []string{boxTop("关于", width)}
	for _, line := range rows {
		out = append(out, boxLine(truncate(line, inner), inner))
	}
	out = append(out, boxBottom(width))
	return out
}
