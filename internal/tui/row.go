package tui

import (
	"fmt"
	"strings"

	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// 统一列表行的列宽。歌曲行与歌单行都按这套常量排布，
// 这样曲库、队列、搜索、下载、侧栏、导入页、选择器与混合编辑器
// 在同一终端宽度下的列起点完全一致。
const (
	rowMarkerW = 2 // "▶ " / "  "
	rowTagW    = 4 // "[B] " / "[云] " 左对齐补满
	rowIndexW  = 3 // 所有歌曲列表统一 3 位序号
	rowArtistW = 14
	rowCountW  = 4 // 歌单数量右对齐
)

const (
	rowArtistCompactW = 8 // 中等宽度下的艺人列
	rowTitleMinW      = 4 // 标题至少保留的列数
	rowTitleCompactW  = 8 // 歌单行保留数量列所需的最小标题宽度

	rowTagCompactW = 2 // 极窄宽度下 tag 截成 2 列

	// 右列固定宽度：同一列表内艺人/时长/额外列必须对齐，
	// 所以调用方按列表语义给出固定宽度，而不是让每行按内容撑开。
	rowDurW      = 7  // 时长 "h:mm:ss" / "m:ss"
	rowExtraW    = 18 // 搜索页播放量 / VIP / 专辑
	rowDownloadW = 17 // 下载页 "大小 · 编码"
)

// songRowSpec 是歌曲列表一行的语义描述；所有歌曲列表都只构造它。
type songRowSpec struct {
	Playing  bool   // 正在播放
	Selected bool   // 光标（优先于 Playing）
	Tag      string // "B" / "云" / "?"，渲染成 "[X]" 再补到 rowTagW
	Index    int    // 1-based；<=0 时不渲染序号列
	Title    string
	Artist   string
	Meta     string // 右对齐主列：时长 "4:47" 或 "38.0 MiB · flac"
	Extra    string // 右对齐次列：搜索页 "1,234,567 播放" / "VIP" / "《专辑》"
	MetaW    int    // meta 固定列宽；0 = 按内容宽度（调用方应按列表传入固定值）
	ExtraW   int    // extra 固定列宽；0 = 按内容宽度
	MetaErr  bool   // meta 用 st.err（文件缺失）
	Marquee  bool   // 仅光标行滚动标题/艺术家，用 m.marqueeOffset
}

// renderSongRow 按统一列布局渲染一行歌曲，保证 visWidth(结果) == width。
//
//	mkr(2) + tag(4) + idx(3) + " " + title(弹性) + " " + artist(14) + " " +
//	meta(右对齐) + [" " + extra(右对齐)]
//
// 宽度不足时按阶梯降级，任何 width >= 16 都不会溢出。
func (m *Model) renderSongRow(s songRowSpec, width int) string {
	if width <= 0 {
		return ""
	}

	marker := "  "
	if s.Playing {
		marker = "▶ "
	}
	tagPlain := ""
	if s.Tag != "" {
		tagPlain = "[" + s.Tag + "]"
	}
	idxPlain := ""
	if s.Index > 0 {
		idxPlain = fmt.Sprintf("%3d", s.Index)
	}

	artist := s.Artist
	meta := s.Meta
	extra := s.Extra
	artistW := 0
	tagW := rowTagW
	showArtist, showMeta, showExtra := false, false, false

	switch {
	case width >= 72:
		// 完整布局：艺人 14 列，meta 保持所需宽度，extra 保留。
		artistW, showArtist, showMeta, showExtra = rowArtistW, true, true, true
	case width >= 48:
		// 艺人压缩到 8 列，去掉 extra；meta 为空时让 extra 顶上主列。
		artistW, showArtist, showMeta = rowArtistCompactW, true, true
		if meta == "" && extra != "" {
			meta, extra = extra, ""
		}
	case width >= 34:
		// 去艺人列、去 extra，只留序号/标题/时长。
		showMeta = true
	case width >= 16:
		// 只留 marker + tag + 序号 + 标题，极窄时 tag 截成 2 列。
		if width < 20 {
			tagW = rowTagCompactW
		}
	default:
		// 病态窄终端：连 tag 列也放弃，保证函数不 panic。
		tagW = 0
	}

	metaW, extraW := 0, 0
	if showMeta && meta != "" {
		metaW = s.MetaW
		if metaW <= 0 {
			metaW = visWidth(meta)
		}
	}
	if showExtra && extra != "" {
		extraW = s.ExtraW
		if extraW <= 0 {
			extraW = visWidth(extra)
		}
	}

	fixedWidth := func() int {
		n := rowMarkerW + tagW
		if idxPlain != "" {
			n += rowIndexW
		}
		n++ // 标题前的空格
		if showArtist {
			n += 1 + artistW
		}
		if metaW > 0 {
			n += 1 + metaW
		}
		if extraW > 0 {
			n += 1 + extraW
		}
		return n
	}
	titleW := width - fixedWidth()
	// 防御：万一右列本身就超宽，按 extra → 艺人 → meta 的顺序让列。
	for titleW < rowTitleMinW {
		changed := false
		switch {
		case extraW > 0:
			showExtra, extraW = false, 0
			changed = true
		case showArtist:
			showArtist = false
			changed = true
		case metaW > 0:
			showMeta, metaW = false, 0
			changed = true
		}
		if !changed {
			break
		}
		titleW = width - fixedWidth()
	}
	if titleW < 0 {
		titleW = 0
	}

	title := s.Title
	artist = s.Artist
	if showArtist {
		artist = truncate(s.Artist, artistW)
	}
	if s.Marquee {
		title = scrollText(s.Title, titleW, m.marqueeOffset)
		if showArtist {
			artist = scrollText(s.Artist, artistW, m.marqueeOffset)
		}
	}
	// 右列按固定宽度左补空格，保证同一列表内所有行的艺人/时长列对齐。
	metaText, extraText := "", ""
	if metaW > 0 {
		metaText = padLeft(meta, metaW)
	}
	if extraW > 0 {
		extraText = padLeft(extra, extraW)
	}

	// 先拼纯文本：高亮行整行套背景，避免嵌套 ANSI reset 截断背景。
	var plain strings.Builder
	plain.WriteString(marker)
	plain.WriteString(padRight(tagPlain, tagW))
	plain.WriteString(idxPlain)
	plain.WriteString(" ")
	plain.WriteString(padRight(title, titleW))
	if showArtist {
		plain.WriteString(" ")
		plain.WriteString(padRight(artist, artistW))
	}
	if metaText != "" {
		plain.WriteString(" ")
		plain.WriteString(metaText)
	}
	if extraText != "" {
		plain.WriteString(" ")
		plain.WriteString(extraText)
	}
	line := padRight(plain.String(), width)

	switch {
	case s.Selected:
		return st.selected.Render(line)
	case s.Playing:
		return st.playing.Render(line)
	}

	// 普通行逐列着色：tag/序号 dim，艺人/meta muted，标题默认。
	var b strings.Builder
	b.WriteString(marker)
	if tagPlain != "" {
		b.WriteString(st.dim.Render(padRight(tagPlain, tagW)))
	} else if tagW > 0 {
		b.WriteString(strings.Repeat(" ", tagW))
	}
	if idxPlain != "" {
		b.WriteString(st.dim.Render(idxPlain))
	}
	b.WriteString(" ")
	b.WriteString(padRight(title, titleW))
	if showArtist {
		b.WriteString(" ")
		b.WriteString(st.muted.Render(padRight(artist, artistW)))
	}
	if metaText != "" {
		b.WriteString(" ")
		if s.MetaErr {
			b.WriteString(st.err.Render(metaText))
		} else {
			b.WriteString(st.muted.Render(metaText))
		}
	}
	if extraText != "" {
		b.WriteString(" ")
		b.WriteString(st.dim.Render(extraText))
	}
	return padRight(b.String(), width)
}

// playlistRowSpec 是歌单列表一行的语义描述。
type playlistRowSpec struct {
	Playing  bool   // 正在播放的来源歌单 → ▶ + 灰底
	Selected bool   // 光标 → 蓝底
	Tag      string // "B"/"云"/"混"/"本"/"新"；空串=不显示
	Title    string
	Count    int   // >0 时右对齐 rowCountW
	Check    *bool // 非 nil 时先渲染 "[x] "/"[ ] "（混合歌单编辑器用）
	Marquee  bool
}

// renderPlaylistRow 按统一列布局渲染一行歌单，保证 visWidth(结果) == width。
//
//	mkr(2) + tag(4，空则省略整列) + [check(4)] + title(弹性) + count(右对齐 4)
func (m *Model) renderPlaylistRow(s playlistRowSpec, width int) string {
	if width <= 0 {
		return ""
	}

	marker := "  "
	if s.Playing {
		marker = "▶ "
	}
	tagPlain := ""
	if s.Tag != "" {
		tagPlain = "[" + s.Tag + "]"
	}
	tagW := 0
	if tagPlain != "" {
		tagW = rowTagW
	}
	checkPlain := ""
	if s.Check != nil {
		if *s.Check {
			checkPlain = "[x] "
		} else {
			checkPlain = "[ ] "
		}
	}
	checkW := visWidth(checkPlain)
	countPlain := ""
	countW := 0
	if s.Count > 0 {
		countW = rowCountW
		countPlain = fmt.Sprintf("%d", s.Count)
	}

	fixed := func() int {
		return rowMarkerW + tagW + checkW + countW
	}
	titleW := width - fixed()
	// 先保标题：数量列可以不要，标题列不要挤到读不出来。
	if titleW < rowTitleCompactW && countW > 0 {
		countW, countPlain = 0, ""
		titleW = width - fixed()
	}
	if titleW < rowTitleMinW && tagW > 0 {
		tagW = 0
		titleW = width - fixed()
	}
	if titleW < 0 {
		titleW = 0
	}

	title := s.Title
	if s.Marquee {
		title = scrollText(s.Title, titleW, m.marqueeOffset)
	}

	var plain strings.Builder
	plain.WriteString(marker)
	plain.WriteString(padRight(tagPlain, tagW))
	plain.WriteString(checkPlain)
	plain.WriteString(padRight(title, titleW))
	if countW > 0 {
		plain.WriteString(padLeft(countPlain, countW))
	}
	line := padRight(plain.String(), width)

	switch {
	case s.Selected:
		return st.selected.Render(line)
	case s.Playing:
		return st.playing.Render(line)
	}

	var b strings.Builder
	b.WriteString(marker)
	if tagPlain != "" {
		b.WriteString(st.dim.Render(padRight(tagPlain, tagW)))
	} else if tagW > 0 {
		b.WriteString(strings.Repeat(" ", tagW))
	}
	if checkW > 0 {
		b.WriteString(st.dim.Render(checkPlain))
	}
	b.WriteString(padRight(title, titleW))
	if countW > 0 {
		b.WriteString(st.muted.Render(padLeft(countPlain, countW)))
	}
	return padRight(b.String(), width)
}

// playlistTag 返回歌单短标记：混合/本地用固定字，远端用音源短标记。
func (m *Model) playlistTag(p store.Playlist) string {
	switch p.Kind {
	case "mixin":
		return "混"
	case "local":
		return "本"
	}
	if p.Source != "" {
		return m.sourceMark(source.ID(p.Source))
	}
	return "?"
}
