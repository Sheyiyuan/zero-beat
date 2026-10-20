package tui

import (
	"fmt"
	"strings"
	"time"

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

// progressBar 画一条固定宽度的进度条。
func progressBar(width int, pos, total time.Duration) string {
	if width <= 0 {
		return ""
	}
	if total <= 0 {
		return strings.Repeat("─", width)
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
	return strings.Repeat("━", filled) + strings.Repeat("─", width-filled)
}

// centerText 在给定宽度内居中（按可见宽度）。
func centerText(s string, w int) string {
	pad := (w - visWidth(s)) / 2
	if pad <= 0 {
		return truncate(s, w)
	}
	return strings.Repeat(" ", pad) + truncate(s, w-pad)
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
