package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// visWidth 返回含 ANSI 转义序列的可见宽度。
func visWidth(s string) int { return ansi.StringWidth(s) }

// truncate 按可见宽度截断，保留 ANSI 样式。
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if visWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// padRight 补空格到指定宽度；超出则截断。
func padRight(s string, w int) string {
	s = truncate(s, w)
	gap := w - visWidth(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}

// wrapText 按显示宽度把纯文本拆成多行；优先在空格或「·」处断行，
// 避免把中文/按键提示从中间硬切开。
func wrapText(s string, width int) []string {
	if s == "" {
		return nil
	}
	if width < 1 {
		width = 1
	}
	var out []string
	rest := []rune(s)
	for len(rest) > 0 {
		w := 0
		cut := 0
		for i, r := range rest {
			rw := visWidth(string(r))
			if w+rw > width {
				break
			}
			w += rw
			cut = i + 1
		}
		if cut == 0 {
			cut = 1
		}
		if cut < len(rest) {
			for i := cut - 1; i > 0; i-- {
				if rest[i] == ' ' || rest[i] == '·' {
					cut = i + 1
					break
				}
			}
		}
		if line := strings.TrimRight(string(rest[:cut]), " "); line != "" {
			out = append(out, line)
		}
		rest = rest[cut:]
		for len(rest) > 0 && rest[0] == ' ' {
			rest = rest[1:]
		}
	}
	return out
}

// padLeft 左补空格到指定宽度。
func padLeft(s string, w int) string {
	s = truncate(s, w)
	gap := w - visWidth(s)
	if gap <= 0 {
		return s
	}
	return strings.Repeat(" ", gap) + s
}

// formatDuration 输出 m:ss 或 h:mm:ss。
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Round(time.Second) / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// progressFill 返回已播放部分的字符宽度；总时长未知时整条按未播放处理。
func progressFill(width int, pos, total time.Duration) int {
	if width <= 0 || total <= 0 {
		return 0
	}
	ratio := float64(pos) / float64(total)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	if filled > width {
		filled = width
	}
	return filled
}

// centerText 在给定宽度内居中（按可见宽度）。
func centerText(s string, w int) string {
	pad := (w - visWidth(s)) / 2
	if pad <= 0 {
		return truncate(s, w)
	}
	return strings.Repeat(" ", pad) + truncate(s, w-pad)
}

// gradientText 按播放进度把整行歌词从 base 色平滑过渡到 highlight 色。
// 整行同一个颜色，不做逐字卡拉 OK 填充。
func gradientText(s string, progress float64, base, highlight color.Color) string {
	if s == "" {
		return ""
	}
	c := blendColor(base, highlight, clamp01(progress))
	return lipgloss.NewStyle().Foreground(c).Render(s)
}

// blendColor 在两种颜色之间线性插值；t=0 返回 a，t=1 返回 b。
func blendColor(a, b color.Color, t float64) color.Color {
	t = clamp01(t)
	if t == 0 {
		return a
	}
	if t == 1 {
		return b
	}
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	mix := func(x, y uint32) uint8 {
		// RGBA() 返回 16 位分量，除以 257 收回到 8 位。
		return uint8((float64(x)*(1-t) + float64(y)*t) / 257)
	}
	return color.RGBA{R: mix(ar, br), G: mix(ag, bg), B: mix(ab, bb), A: 255}
}

// comma 给播放量加千分位。
func comma(n int64) string {
	s := fmt.Sprint(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteString(",")
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// windowRange 返回让 cursor 保持在可视区内的窗口区间。
func windowRange(total, cursor, height int) (int, int) {
	if height <= 0 || total <= 0 {
		return 0, 0
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= total {
		cursor = total - 1
	}
	start := cursor - height/2
	if start < 0 {
		start = 0
	}
	if start+height > total {
		start = total - height
	}
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > total {
		end = total
	}
	return start, end
}

// scrollText 把超宽文本循环滚动；offset 以可见列计。
func scrollText(s string, width, offset int) string {
	if width <= 0 {
		return ""
	}
	if visWidth(s) <= width {
		return s
	}
	const gap = "   "
	period := visWidth(s) + visWidth(gap)
	if period <= 0 {
		return s
	}
	off := offset % period
	if off < 0 {
		off += period
	}
	full := s + gap + s
	for visWidth(full) < off+width {
		full += gap + s
	}
	return ansi.Cut(full, off, off+width)
}
