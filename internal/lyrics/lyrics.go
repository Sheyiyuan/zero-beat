// Package lyrics 解析歌词并按时间轴定位当前行。
//
// 目前两类输入：标准 LRC 文本、B 站字幕（已带起止时间）。
package lyrics

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Line 是一行带起始时间的歌词。
type Line struct {
	StartMS int
	Text    string
}

var lrcTag = regexp.MustCompile(`\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)

// ParseLRC 解析 LRC 文本，按时间升序返回。
func ParseLRC(text string) []Line {
	var out []Line
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		matches := lrcTag.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			continue
		}
		content := strings.TrimSpace(lrcTag.ReplaceAllString(line, ""))
		if content == "" {
			continue
		}
		for _, m := range matches {
			min, err1 := strconv.Atoi(m[1])
			sec, err2 := strconv.Atoi(m[2])
			if err1 != nil || err2 != nil {
				continue
			}
			ms := 0
			if m[3] != "" {
				frac := m[3]
				switch len(frac) {
				case 1:
					frac += "00"
				case 2:
					frac += "0"
				}
				v, err := strconv.Atoi(frac)
				if err == nil {
					ms = v
				}
			}
			out = append(out, Line{StartMS: (min*60+sec)*1000 + ms, Text: content})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartMS < out[j].StartMS })
	return out
}

// ActiveIndex 返回 posMS 时刻应高亮的行号；尚未开始时返回 -1。
func ActiveIndex(lines []Line, posMS int) int {
	if len(lines) == 0 {
		return -1
	}
	idx := -1
	for i := range lines {
		if lines[i].StartMS <= posMS {
			idx = i
			continue
		}
		break
	}
	return idx
}

// Clone 返回带时间偏移的副本（用于负偏移等调整）。
func Clone(lines []Line, offsetMS int) []Line {
	if offsetMS == 0 || len(lines) == 0 {
		return lines
	}
	out := make([]Line, len(lines))
	for i, l := range lines {
		out[i] = Line{StartMS: l.StartMS + offsetMS, Text: l.Text}
	}
	return out
}
