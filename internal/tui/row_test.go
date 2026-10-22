package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

func rowCells(row string) string { return ansi.Strip(row) }

func assertRowWidth(t *testing.T, label, row string, width int) {
	t.Helper()
	if got := visWidth(row); got != width {
		t.Fatalf("%s 行宽应为 %d，实际 %d：%q", label, width, got, rowCells(row))
	}
}

// TestUniformSongRowColumns 验证曲库/队列/音源搜索/本地搜索/下载管理
// 在同一宽度 80 下使用同一套列布局：marker(2) + tag(4) + 序号(3) + 空格 + 标题。
func TestUniformSongRowColumns(t *testing.T) {
	m := fixtureModel()
	// 关掉播放/歌词等与列布局无关的状态，保证各视图前缀可逐列比较。
	m.current = nil
	m.playingPlaylistID = ""
	m.libraryFocus = focusSongs
	m.songIdx = 0

	bili := store.NormalizeSong(store.Song{
		Source: "bilibili", BVID: "BV1", Page: 1, Title: "夜航星", Artist: "不才", Duration: 287,
	})
	m.songs = []store.Song{bili}
	m.queue = []store.Song{bili}
	m.queueIdx = 0
	m.queueCursor = 0
	m.downloadItems = []downloadItem{{
		entry: store.DownloadEntry{
			Download: store.Download{SongID: bili.ID, Size: 5 << 20, Codec: "flac"},
			Song:     bili,
		},
	}}

	const wantWidth = 80
	const wantTitleCol = rowMarkerW + rowTagW + rowIndexW + 1 // = 10
	const wantPrefix = "  [B]   1 "

	type rowCase struct {
		name  string
		row   string
		title string
	}
	var rows []rowCase

	// 曲库：主区固定 80，侧栏宽度由 librarySideWidth 决定（宽度阈值会变）。
	m.width, m.height = wantWidth+1, 30
	sideW := m.librarySideWidth()
	m.width = wantWidth + sideW + 1
	m.view = viewLibrary
	lines := m.renderLibrary(8)
	libRow := ansi.Cut(rowCells(lines[1]), sideW+1, m.width)
	rows = append(rows, rowCase{"曲库", libRow, bili.Title})

	m.width = wantWidth
	// 队列。
	m.view = viewQueue
	queueLines := m.renderQueue(8)
	rows = append(rows, rowCase{"队列", queueLines[1], bili.Title})

	// 音源搜索。
	m.view = viewSearch
	m.searchMode = searchSource
	m.searchKeyword = "夜"
	m.searchIdx = 0
	m.searchResults = []source.Candidate{{
		Track: source.Track{Source: source.Bilibili, ExternalID: "BV1", Page: 1, Title: "夜航星", Artist: "不才", Duration: 287},
		Play:  1234567,
	}}
	searchLines := m.renderSearch(8)
	rows = append(rows, rowCase{"音源搜索", searchLines[2], "夜航星"})

	// 本地搜索。
	m.searchMode = searchLocalAll
	m.localResults = []store.Song{bili}
	localLines := m.renderSearch(8)
	rows = append(rows, rowCase{"本地搜索", localLines[2], bili.Title})

	// 下载管理（保留 downloadRow 签名）。
	rows = append(rows, rowCase{"下载管理", m.downloadRow(0, wantWidth), bili.Title})

	for _, c := range rows {
		assertRowWidth(t, c.name, c.row, wantWidth)
		stripped := rowCells(c.row)
		if got := ansi.Cut(stripped, 0, wantTitleCol); got != wantPrefix {
			t.Fatalf("%s 的 marker/tag/序号列不一致：%q，期望 %q", c.name, got, wantPrefix)
		}
		if got := ansi.Cut(stripped, wantTitleCol, wantTitleCol+visWidth(c.title)); got != c.title {
			t.Fatalf("%s 的标题应从第 %d 列开始，实际 %q", c.name, wantTitleCol, got)
		}
	}
}

// TestSongRowPlayingAndCursorStyles 验证播放行是灰底 + ▶、光标行是蓝底。
func TestSongRowPlayingAndCursorStyles(t *testing.T) {
	m := fixtureModel()
	a := store.NormalizeSong(store.Song{Source: "bilibili", BVID: "BV1", Page: 1, Title: "夜航星", Artist: "不才", Duration: 287})
	b := store.NormalizeSong(store.Song{Source: "netease", RemoteID: "2", Page: 1, Title: "春庭雪", Artist: "等什么君", Duration: 192})
	m.width = 80

	m.queue = []store.Song{a, b}
	m.queueIdx = 0
	m.queueCursor = 1
	m.current = &m.queue[0]
	queueLines := m.renderQueue(8)
	playing, cursor := queueLines[1], queueLines[2]
	if !strings.Contains(playing, "\x1b[") {
		t.Skip("当前终端未启用颜色，跳过背景断言")
	}
	if !strings.Contains(playing, "48;2;58;63;74") || !strings.Contains(rowCells(playing), "▶") {
		t.Fatalf("队列播放行应为灰底 + ▶：%q", playing)
	}
	if !strings.Contains(cursor, "48;2;102;204;255") {
		t.Fatalf("队列光标行应为蓝底：%q", cursor)
	}

	// 音源搜索：第一行正在播放，光标在第二行。
	m.view = viewSearch
	m.searchMode = searchSource
	m.searchIdx = 1
	m.searchResults = []source.Candidate{
		{Track: source.Track{Source: source.Bilibili, ExternalID: "BV1", Page: 1, Title: "夜航星", Artist: "不才", Duration: 287}},
		{Track: source.Track{Source: source.NetEase, ExternalID: "2", Page: 1, Title: "春庭雪", Artist: "等什么君", Duration: 192}},
	}
	track := m.searchResults[0].Track
	currentSong := store.NormalizeSong(store.Song{
		Source: string(track.Source), RemoteID: track.ExternalID, Page: track.Page,
		Title: track.Title, Artist: track.Artist, Duration: track.Duration,
	})
	m.current = &currentSong
	searchLines := m.renderSearch(8)
	if !strings.Contains(searchLines[2], "48;2;58;63;74") || !strings.Contains(rowCells(searchLines[2]), "▶") {
		t.Fatalf("搜索播放行应为灰底 + ▶：%q", searchLines[2])
	}
	if !strings.Contains(searchLines[3], "48;2;102;204;255") {
		t.Fatalf("搜索光标行应为蓝底：%q", searchLines[3])
	}

	// 下载管理：第一行正在播放，光标在第二行。
	m.view = viewDownloads
	m.downloadItems = []downloadItem{
		{entry: store.DownloadEntry{Download: store.Download{SongID: a.ID, Size: 5 << 20, Codec: "flac"}, Song: a}},
		{entry: store.DownloadEntry{Download: store.Download{SongID: b.ID, Size: 9 << 20, Codec: "aac"}, Song: b}},
	}
	m.downloadIdx = 1
	m.current = &m.downloadItems[0].entry.Song
	if !strings.Contains(m.downloadRow(0, 80), "48;2;58;63;74") || !strings.Contains(rowCells(m.downloadRow(0, 80)), "▶") {
		t.Fatalf("下载播放行应为灰底 + ▶：%q", m.downloadRow(0, 80))
	}
	if !strings.Contains(m.downloadRow(1, 80), "48;2;102;204;255") {
		t.Fatalf("下载光标行应为蓝底：%q", m.downloadRow(1, 80))
	}
}

// TestSongRowNarrowWidths 验证任何窄宽度下行宽都精确等于 width。
func TestSongRowNarrowWidths(t *testing.T) {
	m := fixtureModel()
	m.current = nil
	spec := songRowSpec{
		Tag:    "云",
		Index:  100,
		Title:  strings.Repeat("长标题", 12),
		Artist: "一位名字很长的艺术家",
		Meta:   "1:01:01",
		Extra:  "1,234,567 播放 · VIP",
	}
	for _, width := range []int{16, 20, 24, 32, 34, 40, 47, 48, 60, 71, 72, 80, 120} {
		assertRowWidth(t, "普通歌曲行", m.renderSongRow(spec, width), width)
		selected := spec
		selected.Selected = true
		selected.Marquee = true
		assertRowWidth(t, "选中歌曲行", m.renderSongRow(selected, width), width)
		playing := spec
		playing.Playing = true
		assertRowWidth(t, "播放歌曲行", m.renderSongRow(playing, width), width)
	}
}

// TestPlaylistRowWidthsAndCount 验证歌单行在侧栏/导入页/选择器/混合编辑器
// 宽度下的行宽与数量右对齐。
func TestPlaylistRowWidthsAndCount(t *testing.T) {
	m := fixtureModel()

	// 侧栏 20 列：标题挤压但数量列仍保留并右对齐。
	sideRow := m.renderPlaylistRow(playlistRowSpec{Tag: "B", Title: "我喜欢的音乐", Count: 128}, 20)
	assertRowWidth(t, "侧栏歌单行", sideRow, 20)
	if got := ansi.Cut(rowCells(sideRow), 20-rowCountW, 20); got != " 128" {
		t.Fatalf("侧栏 20 列数量应右对齐，实际 %q（整行 %q）", got, rowCells(sideRow))
	}

	// 侧栏 16 列：标题优先，数量列被降级掉，行宽仍精确。
	tinyRow := m.renderPlaylistRow(playlistRowSpec{Tag: "B", Title: "我喜欢的音乐", Count: 128}, 16)
	assertRowWidth(t, "侧栏窄歌单行", tinyRow, 16)
	if strings.Contains(rowCells(tinyRow), "128") {
		t.Fatalf("侧栏 16 列时应去掉数量列：%q", rowCells(tinyRow))
	}

	// 导入页 80 列：数量右对齐在最后一列结束。
	m.width, m.height = 80, 30
	m.view = viewFolders
	m.folderSource = source.Bilibili
	m.folderItems = []source.Playlist{
		{ID: "bili-fav-1", Source: source.Bilibili, Title: "我喜欢的音乐", Count: 128},
		{ID: "bili-fav-2", Source: source.Bilibili, Title: "通勤路上", Count: 42},
	}
	m.folderIdx = 1
	folderLines := m.renderFolders(8)
	for i, line := range folderLines[1:] {
		assertRowWidth(t, "导入页歌单行", line, 80)
		if got := ansi.Cut(rowCells(line), 80-rowCountW, 80); got == "" || got == strings.Repeat(" ", rowCountW) {
			t.Fatalf("导入页第 %d 行数量列缺失：%q", i, rowCells(line))
		}
	}
	if got := ansi.Cut(rowCells(folderLines[1]), 76, 80); got != " 128" {
		t.Fatalf("导入页数量应右对齐到行尾，实际 %q（整行 %q）", got, rowCells(folderLines[1]))
	}

	// 目标选择器：每行占 box 的 inner-1，数量右对齐。
	m.width, m.height = 100, 30
	m.picker = &targetPickerState{
		mode: targetPickerAddSong,
		items: []playlistTarget{
			{ID: "local-default", Title: "本地歌单", Kind: "local", Count: 7},
			{ID: "mix-1", Title: "通勤合集", Kind: "mixin", Count: 38},
			{ID: "bili-fav-1", Title: "我喜欢的音乐", Kind: "remote", Source: "bilibili", RemoteID: "1", Count: 128, Cloud: true},
			{Title: "＋ 新建本地歌单并加入", Create: true},
		},
	}
	pickerLines := m.targetPickerLines()
	pickerWidth := 72
	inner := pickerWidth - 2
	for _, line := range pickerLines {
		assertRowWidth(t, "选择器浮层行", line, pickerWidth)
	}
	// 浮层行顺序：顶框、输入框、目标行，因此第一个目标在 pickerLines[2]。
	stripped := rowCells(pickerLines[2])
	if got := ansi.Cut(stripped, 1, pickerWidth-1); visWidth(got) != inner {
		t.Fatalf("选择器行内容宽度不符：%q", got)
	}
	if got := ansi.Cut(stripped, pickerWidth-1-rowCountW, pickerWidth-1); got != "   7" {
		t.Fatalf("选择器数量应右对齐，实际 %q（整行 %q）", got, stripped)
	}

	// 混合编辑器：横跨整屏宽度，数量右对齐，勾选框仍在 tag 之后。
	m.view = viewMix
	m.mix = &mixState{
		items: []store.Playlist{
			{ID: "a", Title: "本地歌单", Kind: "local", Count: 7},
			{ID: "b", Title: "我喜欢的音乐", Kind: "remote", Source: "bilibili", Count: 128},
		},
		selected: map[string]bool{"a": true},
	}
	mixLines := m.renderMix(8)
	assertRowWidth(t, "混合编辑器行", mixLines[1], 100)
	if got := ansi.Cut(rowCells(mixLines[1]), 96, 100); got != "   7" {
		t.Fatalf("混合编辑器数量应右对齐，实际 %q（整行 %q）", got, rowCells(mixLines[1]))
	}
}

// TestMixRowCheckboxes 验证混合歌单的 [x]/[ ] 勾选仍正确，
// 且勾选框位于 tag 列之后、标题之前。
func TestMixRowCheckboxes(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 100, 30
	m.view = viewMix
	m.mix = &mixState{
		items: []store.Playlist{
			{ID: "a", Title: "本地歌单", Kind: "local", Count: 7},
			{ID: "b", Title: "我喜欢的音乐", Kind: "remote", Source: "bilibili", Count: 128},
		},
		selected: map[string]bool{"a": true},
	}
	lines := m.renderMix(8)
	checked := rowCells(lines[1])
	unchecked := rowCells(lines[2])
	if !strings.Contains(checked, "[x] ") {
		t.Fatalf("已勾选行应渲染 [x]：%q", checked)
	}
	if !strings.Contains(unchecked, "[ ] ") {
		t.Fatalf("未勾选行应渲染 [ ]：%q", unchecked)
	}
	// marker2 + tag4 之后就是勾选框，保证所有歌单行列结构一致。
	if got := ansi.Cut(checked, rowMarkerW+rowTagW, rowMarkerW+rowTagW+4); got != "[x] " {
		t.Fatalf("勾选框应紧跟 tag 列，实际 %q", got)
	}
	if got := ansi.Cut(unchecked, rowMarkerW+rowTagW, rowMarkerW+rowTagW+4); got != "[ ] " {
		t.Fatalf("勾选框应紧跟 tag 列，实际 %q", got)
	}

	// 光标行不再用 ▶，只用蓝底。
	if strings.Contains(checked, "▶") || strings.Contains(unchecked, "▶") {
		t.Fatalf("混合编辑器不应再使用 ▶ 表示光标：%q / %q", checked, unchecked)
	}
}

// TestPlaylistRowsUsePlayMarkerOnlyWhenPlaying 验证 ▶ 只表示正在播放：
// 导入页与选择器里的光标行只用蓝底。
func TestPlaylistRowsUsePlayMarkerOnlyWhenPlaying(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 100, 30

	m.view = viewFolders
	m.folderSource = source.Bilibili
	m.folderItems = []source.Playlist{
		{ID: "bili-fav-1", Source: source.Bilibili, Title: "我喜欢的音乐", Count: 128},
	}
	m.folderIdx = 0
	folder := rowCells(m.renderFolders(6)[1])
	if strings.Contains(folder, "▶") {
		t.Fatalf("导入页光标行不应出现 ▶：%q", folder)
	}

	m.picker = &targetPickerState{
		mode:  targetPickerAddSong,
		items: []playlistTarget{{ID: "local-default", Title: "本地歌单", Kind: "local", Count: 7}},
	}
	var pickerRow string
	for _, line := range m.targetPickerLines() {
		if strings.Contains(rowCells(line), "本地歌单") {
			pickerRow = rowCells(line)
		}
	}
	if pickerRow == "" {
		t.Fatal("选择器应渲染出目标歌单行")
	}
	if strings.Contains(pickerRow, "▶") {
		t.Fatalf("选择器光标行不应出现 ▶：%q", pickerRow)
	}

	// 正在播放的歌单才用 ▶。
	playing := m.renderPlaylistRow(playlistRowSpec{Playing: true, Tag: "B", Title: "我喜欢的音乐", Count: 128}, 80)
	if !strings.Contains(rowCells(playing), "▶") {
		t.Fatalf("正在播放的歌单行应有 ▶：%q", rowCells(playing))
	}
	if !strings.Contains(playing, "\x1b[") {
		t.Skip("当前终端未启用颜色，跳过背景断言")
	}
	if !strings.Contains(playing, "48;2;58;63;74") {
		t.Fatalf("正在播放的歌单行应有灰底：%q", playing)
	}
}

// TestSongRowRightColumnsAlign 验证同一列表内即使 meta/extra 内容长度不同，
// 艺人列、时长列、额外列也按固定列宽对齐（右列不再随内容浮动）。
func TestSongRowRightColumnsAlign(t *testing.T) {
	m := fixtureModel()
	const width = 80
	base := songRowSpec{
		Tag: "B", Index: 1, Title: "稻香", Artist: "周杰伦",
		Meta: "3:43", MetaW: rowDurW,
		Extra: "1,234,567 播放", ExtraW: rowExtraW,
	}
	longer := base
	longer.Extra = "VIP"
	other := base
	other.Title = "夜航星"
	other.Artist = "不才"
	other.Meta = "1:01:01"

	rows := []struct {
		name string
		row  string
	}{
		{"播放量", rowCells(m.renderSongRow(base, width))},
		{"VIP", rowCells(m.renderSongRow(longer, width))},
		{"另一个标题", rowCells(m.renderSongRow(other, width))},
	}
	artistCol := width - (rowArtistW + 1 + rowDurW + 1 + rowExtraW)
	metaCol := artistCol + rowArtistW + 1
	for _, c := range rows {
		assertRowWidth(t, c.name, c.row, width)
		if got := ansi.Cut(c.row, artistCol, metaCol); strings.TrimSpace(got) == "" {
			t.Fatalf("%s 的艺人列为空，列起点不对：%q", c.name, c.row)
		}
		if got := ansi.Cut(c.row, artistCol, metaCol); len(strings.Split(got, "")) > rowArtistW {
			t.Fatalf("%s 的艺人列超出固定宽度：%q", c.name, got)
		}
		if got := ansi.Cut(c.row, metaCol, metaCol+rowDurW); strings.TrimSpace(got) == "" {
			t.Fatalf("%s 的时长列缺失：%q", c.name, c.row)
		}
	}
	// extra 内容不同不应改变右列起点：两行的时长列位置一致。
	metaA := ansi.Cut(rows[0].row, metaCol, metaCol+rowDurW)
	metaB := ansi.Cut(rows[1].row, metaCol, metaCol+rowDurW)
	if metaA != metaB {
		t.Fatalf("时长列未对齐：%q vs %q", metaA, metaB)
	}
}
