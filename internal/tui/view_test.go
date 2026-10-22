package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

func fixtureModel() *Model {
	songs := []store.Song{
		{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "Never Gonna Give You Up", Artist: "Rick Astley", Duration: 213},
		{ID: "bilibili:BV2:1", Source: "bilibili", BVID: "BV2", Page: 1, Title: "夜航星", Artist: "不才", Duration: 287},
	}
	m := &Model{
		width:       100,
		height:      30,
		view:        viewLibrary,
		volume:      0.8,
		mode:        modeSequence,
		state:       audio.StatePlaying,
		loginSource: source.Bilibili,
		sources: source.NewRegistry(
			&fakeSource{id: source.Bilibili, name: "Bilibili", mark: "B", lyricName: "B 站字幕"},
			&fakeSource{id: source.NetEase, name: "网易云", mark: "云", lyricName: "网易云歌词"},
		),
	}
	m.playlists = []store.Playlist{
		{ID: "bili-fav-1", Title: "我喜欢的音乐", Kind: "remote", Count: 2},
		{ID: "local-default", Title: "本地歌单", Kind: "local", Count: 0},
	}
	m.songs = songs
	m.queue = append([]store.Song(nil), songs...)
	m.current = &songs[0]
	m.duration = 213 * time.Second
	m.position = 42 * time.Second
	return m
}

func assertFrame(t *testing.T, frame string, w, h int) []string {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != h {
		t.Fatalf("帧高度应为 %d，实际 %d", h, len(lines))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != w {
			t.Fatalf("第 %d 行宽度应为 %d，实际 %d：%q", i, w, got, ansi.Strip(line))
		}
	}
	return lines
}

func TestRenderLibrary(t *testing.T) {
	m := fixtureModel()
	lines := assertFrame(t, m.View().Content, 100, 30)
	body := ansi.Strip(strings.Join(lines, "\n"))

	for _, want := range []string{"zero", "曲库", "我喜欢的音乐", "本地歌单", "Never Gonna Give You Up", "夜航星", "0:42 / 3:33", "顺序"} {
		if !strings.Contains(body, want) {
			t.Fatalf("界面里应该出现 %q", want)
		}
	}
}

func TestRenderAllViews(t *testing.T) {
	m := fixtureModel()
	m.searchKeyword = "周杰伦"
	m.searchResults = []source.Candidate{
		{Track: source.Track{Source: source.Bilibili, ExternalID: "BV1", Page: 1, Title: "稻香", Artist: "周杰伦", Duration: 223}, Play: 1234567},
		{Track: source.Track{Source: source.NetEase, ExternalID: "2", Page: 1, Title: "晴天", Artist: "周杰伦", Duration: 269}},
	}
	m.lyricLines = []lyrics.Line{
		{StartMS: 0, Text: "第一句歌词"},
		{StartMS: 5000, Text: "第二句歌词"},
		{StartMS: 10000, Text: "第三句歌词"},
	}
	m.lyricIdx = 1

	for _, v := range []view{viewLibrary, viewSearch, viewQueue, viewLyrics, viewHelp, viewLogin} {
		m.view = v
		frame := m.View().Content
		assertFrame(t, frame, 100, 30)
	}
}

func TestRenderNarrowTerminal(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 60, 18
	assertFrame(t, m.View().Content, 60, 18)
}

func TestSearchViewContent(t *testing.T) {
	m := fixtureModel()
	m.view = viewSearch
	m.searchMode = searchSource
	m.searchKeyword = "周杰伦"
	m.searchResults = []source.Candidate{{Track: source.Track{Source: source.Bilibili, ExternalID: "BV1", Page: 1, Title: "稻香", Artist: "周杰伦", Duration: 223}, Play: 1234567}}
	body := ansi.Strip(m.View().Content)
	for _, want := range []string{"搜索", "稻香", "周杰伦", "3:43", "1,234,567 播放"} {
		if !strings.Contains(body, want) {
			t.Fatalf("搜索页应包含 %q，实际：\n%s", want, body)
		}
	}
}

func TestLyricsHighlightFollowsIndex(t *testing.T) {
	m := fixtureModel()
	m.view = viewLyrics
	m.lyricLines = []lyrics.Line{{StartMS: 0, Text: "甲"}, {StartMS: 1000, Text: "乙"}, {StartMS: 2000, Text: "丙"}}
	m.lyricIdx = 1
	frame := m.View().Content
	if !strings.Contains(frame, "乙") {
		t.Fatal("当前歌词应被渲染")
	}
	lines := strings.Split(frame, "\n")
	bodyH := 30 - 6
	centerIdx := 2 + bodyH/2 // header + 分隔线之后是正文
	if !strings.Contains(lines[centerIdx], "乙") {
		t.Fatalf("当前行应在正文中间，实际中心行：%q", ansi.Strip(lines[centerIdx]))
	}
}

func TestLyricsViewShowsTranslationUnderOriginal(t *testing.T) {
	m := fixtureModel()
	m.view = viewLyrics
	m.duration = 30 * time.Second
	m.position = 6 * time.Second
	m.lyricLoaded = true
	m.lyricLines = []lyrics.Line{
		{StartMS: 0, Text: "第一句", Translation: "first"},
		{StartMS: 5000, Text: "第二句", Translation: "second"},
		{StartMS: 10000, Text: "第三句"},
	}
	m.lyricIdx = 1
	lines := assertFrame(t, m.View().Content, 100, 30)

	// 当前原文固定在正文中心（行 14），译文紧接在下一行（行 15）。
	if got := ansi.Strip(lines[14]); !strings.Contains(got, "第二句") {
		t.Fatalf("当前原文应在中心：%q", got)
	}
	if got := ansi.Strip(lines[15]); !strings.Contains(got, "second") {
		t.Fatalf("当前译文应在原文下一行：%q", got)
	}
	// 上一行的译文同样紧跟原文。
	if got := ansi.Strip(lines[12]); !strings.Contains(got, "第一句") {
		t.Fatalf("上一句原文位置不符：%q", got)
	}
	if got := ansi.Strip(lines[13]); !strings.Contains(got, "first") {
		t.Fatalf("上一句译文位置不符：%q", got)
	}
	// 没有译文的行不生成空行。
	if got := ansi.Strip(lines[16]); !strings.Contains(got, "第三句") {
		t.Fatalf("下一句原文位置不符：%q", got)
	}
}

func TestSlashEntersSearchEditing(t *testing.T) {
	m := fixtureModel()
	updated, _ := m.Update(keyPress("/", '/'))
	mm := updated.(*Model)
	if mm.view != viewSearch || !mm.searchEditing {
		t.Fatalf("按 / 应进入搜索输入态，实际 view=%v editing=%v", mm.view, mm.searchEditing)
	}
	for _, r := range []rune("稻香") {
		updated, _ = mm.Update(keyPress(string(r), r))
		mm = updated.(*Model)
	}
	if mm.searchInput != "稻香" {
		t.Fatalf("输入未累积: %q", mm.searchInput)
	}
}

func TestSearchEnterStartsSearch(t *testing.T) {
	m := fixtureModel()
	m.view = viewSearch
	m.searchMode = searchSource
	m.searchEditing = true
	m.searchInput = "周杰伦"
	updated, cmd := m.Update(keyPress("", tea.KeyEnter))
	mm := updated.(*Model)
	if mm.searchEditing {
		t.Fatal("回车后应退出输入态")
	}
	if mm.searchKeyword != "周杰伦" {
		t.Fatalf("关键词未生效: %q", mm.searchKeyword)
	}
	if cmd == nil {
		t.Fatal("回车后应发起搜索命令")
	}
}

func TestTabCyclesPages(t *testing.T) {
	m := fixtureModel()
	want := []view{
		viewSearch, viewSearch, viewQueue, viewLyrics, viewHelp, viewLogin,
		viewFolders, viewDownloads, viewLibrary,
	}
	for i, w := range want {
		updated, _ := m.Update(keyPress("", tea.KeyTab))
		m = updated.(*Model)
		if m.view != w {
			t.Fatalf("第 %d 次 Tab 应切到 %v，实际 %v", i+1, viewNames[w], viewNames[m.view])
		}
	}
}

func TestVimDirectionKeysAndLyricsShortcut(t *testing.T) {
	m := fixtureModel()
	m.libraryFocus = focusSongs
	m.songIdx = 0

	// j/k 恢复为上下移动。
	updated, _ := m.Update(keyPress("j", 'j'))
	m = updated.(*Model)
	if m.songIdx != 1 {
		t.Fatalf("j 应下移选中项，实际 %d", m.songIdx)
	}
	updated, _ = m.Update(keyPress("k", 'k'))
	m = updated.(*Model)
	if m.songIdx != 0 {
		t.Fatalf("k 应上移选中项，实际 %d", m.songIdx)
	}

	// h/l 恢复为左右切换焦点。
	updated, _ = m.Update(keyPress("h", 'h'))
	m = updated.(*Model)
	if m.libraryFocus != focusPlaylists {
		t.Fatal("h 应切到歌单焦点")
	}
	updated, _ = m.Update(keyPress("l", 'l'))
	m = updated.(*Model)
	if m.libraryFocus != focusSongs {
		t.Fatalf("l 应切回歌曲焦点，实际 %v", m.libraryFocus)
	}

	// 歌词改用单键 T（ctrl+l 仍可），单键 l 不再切页。
	updated, _ = m.Update(keyPress("l", 'l'))
	m = updated.(*Model)
	if m.view != viewLibrary {
		t.Fatalf("l 不应再打开歌词页，实际 %v", viewNames[m.view])
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 't', Text: "T"}))
	m = updated.(*Model)
	if m.view != viewLyrics {
		t.Fatalf("T 应打开歌词页，实际 %v", viewNames[m.view])
	}
	m.view = viewLibrary
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 'l', Mod: tea.ModCtrl}))
	m = updated.(*Model)
	if m.view != viewLyrics {
		t.Fatalf("ctrl+l 应仍可打开歌词页，实际 %v", viewNames[m.view])
	}
}

func TestShiftTabCyclesPagesBackwards(t *testing.T) {
	m := fixtureModel()
	want := []view{
		viewDownloads, viewFolders, viewLogin, viewHelp, viewLyrics,
		viewQueue, viewSearch, viewSearch, viewLibrary,
	}
	for i, w := range want {
		updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}))
		m = updated.(*Model)
		if m.view != w {
			t.Fatalf("第 %d 次 Shift+Tab 应切到 %v，实际 %v", i+1, viewNames[w], viewNames[m.view])
		}
	}
}

func TestHeaderShowsTabHint(t *testing.T) {
	m := fixtureModel()
	for _, v := range []view{viewLibrary, viewSearch, viewQueue, viewLyrics} {
		m.view = v
		top := ansi.Strip(strings.Split(m.View().Content, "\n")[0])
		if !strings.Contains(top, "Tab") || !strings.Contains(top, "切换") {
			t.Fatalf("%s 页左上角应提示 Tab 切换，实际：%q", viewNames[v], top)
		}
	}
}

func TestLyricStripShowsCurrentLine(t *testing.T) {
	m := fixtureModel()
	m.lyricLoaded = true
	m.lyricLines = []lyrics.Line{
		{StartMS: 0, Text: "第一句"},
		{StartMS: 5000, Text: "第二句"},
		{StartMS: 9000, Text: "第三句"},
	}
	m.lyricIdx = 1
	lines := assertFrame(t, m.View().Content, 100, 30)
	// 30 行：头 1 + 分隔 1 + 正文 23（让出一行给歌词）+ 分隔 1 + 歌词 + 播放 + 进度 + 提示
	lyric := ansi.Strip(lines[26])
	if !strings.Contains(lyric, "♪") || !strings.Contains(lyric, "第二句") {
		t.Fatalf("歌词条应显示当前句，实际：%q", lyric)
	}
	if hint := ansi.Strip(lines[29]); !strings.Contains(hint, "enter") {
		t.Fatalf("底部提示行应该还在最后一行，实际：%q", hint)
	}
}

func TestLyricStripPlaceholderAndVisibility(t *testing.T) {
	m := fixtureModel()
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "歌词加载中…") {
		t.Fatalf("歌词未返回时应显示加载中，实际：\n%s", frame)
	}

	m.lyricLoaded = true
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "暂无歌词") {
		t.Fatalf("歌词返回为空时应显示暂无歌词，实际：\n%s", frame)
	}

	m.view = viewLyrics
	if frame := ansi.Strip(m.View().Content); strings.Contains(frame, "♪") {
		t.Fatalf("歌词页不应重复显示底部歌词条：\n%s", frame)
	}

	m.view = viewLibrary
	m.current = nil
	frame := m.View().Content
	if strings.Contains(ansi.Strip(frame), "♪") {
		t.Fatalf("没有当前曲目时不应显示歌词条：\n%s", ansi.Strip(frame))
	}
	assertFrame(t, frame, 100, 30)
}

func TestLyricStripGradientFollowsProgress(t *testing.T) {
	m := fixtureModel()
	m.lyricLoaded = true
	m.lyricLines = []lyrics.Line{{StartMS: 0, Text: "当前句"}, {StartMS: 10000, Text: "下一句"}}
	m.lyricIdx = 0

	m.position = 0
	start, _ := m.lyricStripLine()
	m.position = 5 * time.Second
	mid, _ := m.lyricStripLine()
	m.position = 10 * time.Second
	end, _ := m.lyricStripLine()

	if !strings.Contains(start, "\x1b[") {
		t.Skip("当前终端未启用颜色，跳过颜色断言")
	}
	if !strings.Contains(start, "38;2;215;218;224") {
		t.Fatalf("行首应为常规色：%q", start)
	}
	if !strings.Contains(end, "38;2;102;204;255") {
		t.Fatalf("行末应为高亮色：%q", end)
	}
	if start == mid || mid == end {
		t.Fatalf("歌词条颜色应随进度变化：\n%q\n%q\n%q", start, mid, end)
	}
	if ansi.Strip(mid) != " ♪ 当前句" {
		t.Fatalf("渐变不应改动歌词内容：%q", ansi.Strip(mid))
	}
}

func TestCurrentLyricTextSkipsBlankLines(t *testing.T) {
	lines := []lyrics.Line{{StartMS: 0, Text: "甲"}, {StartMS: 1000, Text: " "}, {StartMS: 2000, Text: "乙"}}
	if got := currentLyricText(lines, 1); got != "甲" {
		t.Fatalf("空行应保留上一句，实际：%q", got)
	}
	if got := currentLyricText(lines, -1); got != "甲" {
		t.Fatalf("前奏应预告第一句，实际：%q", got)
	}
	if got := currentLyricText(lines, 99); got != "乙" {
		t.Fatalf("越过末句应回退到最后一句，实际：%q", got)
	}
}

func TestLyricsEmptyHintNamesCurrentPlatform(t *testing.T) {
	m := fixtureModel()
	song := store.Song{ID: "netease:1:1", Source: "netease", RemoteID: "1", Title: "夜航星", Duration: 200}
	m.current = &song
	m.lyricLoaded = true
	m.view = viewLyrics

	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "网易云歌词与 LRCLIB 都没有收录") {
		t.Fatalf("网易云歌曲的无歌词提示应点名网易云：\n%s", frame)
	}

	m.current.Source = "bilibili"
	frame = ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "B 站字幕与 LRCLIB 都没有收录") {
		t.Fatalf("B 站歌曲的无歌词提示应点名 B 站字幕：\n%s", frame)
	}
}

func TestPlaySearchResultAppendsAndSelects(t *testing.T) {
	m := fixtureModel()
	m.searchMode = searchSource
	m.searchResults = []source.Candidate{{
		Track: source.Track{
			Source: source.Bilibili, ExternalID: "BV9", Page: 1,
			Title: "新搜到的歌", Artist: "测试", Duration: 100,
		},
	}}
	before := len(m.queue)
	_ = m.playSearchResult(0)
	if len(m.queue) != before+1 {
		t.Fatalf("结果入队后队列应为 %d 首，实际 %d", before+1, len(m.queue))
	}
	if m.queueIdx != before {
		t.Fatalf("当前索引应指向追加位置 %d，实际 %d", before, m.queueIdx)
	}
	if m.current == nil || m.current.ID != m.queue[before].ID {
		t.Fatalf("当前曲目应为追加的搜索结果，实际 %+v", m.current)
	}
	// 顺序模式播完应回绕到队首，而不是重播刚播的这首。
	_ = m.advance()
	if m.queueIdx != 0 {
		t.Fatalf("播完应回绕到队首，实际索引 %d", m.queueIdx)
	}
	if m.current == nil || m.current.ID != m.queue[0].ID {
		t.Fatalf("播完应播放队首，实际 %+v", m.current)
	}
}

func TestAdvanceWrapsAndRespectsLoopOne(t *testing.T) {
	m := fixtureModel()
	m.queueIdx = len(m.queue) - 1
	m.mode = modeSequence
	_ = m.advance()
	if m.queueIdx != 0 {
		t.Fatalf("顺序模式应回到第一首，实际 %d", m.queueIdx)
	}

	m.mode = modeLoopOne
	m.queueIdx = 1
	_ = m.advance()
	if m.queueIdx != 1 {
		t.Fatalf("单曲循环应停在原地，实际 %d", m.queueIdx)
	}
}

func TestVolumeAndCompensationClamp(t *testing.T) {
	m := fixtureModel()
	m.changeVolume(0.5)
	if m.volume != 1 {
		t.Fatalf("音量应被夹到 1，实际 %v", m.volume)
	}
	for i := 0; i < 30; i++ {
		m.changeComp(1)
	}
	if m.compDB != 12 {
		t.Fatalf("补偿上限应为 12 dB，实际 %v", m.compDB)
	}
	for i := 0; i < 40; i++ {
		m.changeComp(-1)
	}
	if m.compDB != -20 {
		t.Fatalf("补偿下限应为 -20 dB，实际 %v", m.compDB)
	}
}

func keyPress(text string, code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: text, Code: code})
}

func TestRenderAllViewsNarrow(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 40, 12
	m.searchKeyword = "周杰伦"
	m.searchResults = []source.Candidate{{Track: source.Track{Source: source.Bilibili, ExternalID: "BV1", Page: 1, Title: "稻香", Artist: "周杰伦", Duration: 223}, Play: 1234567}}
	m.searchPages = 3
	m.lyricLines = []lyrics.Line{{StartMS: 0, Text: "第一句"}, {StartMS: 1000, Text: "第二句"}}
	m.lyricIdx = 1
	m.sleepUntil = time.Now().Add(10 * time.Minute)

	for _, v := range []view{viewLibrary, viewSearch, viewQueue, viewLyrics, viewHelp, viewLogin} {
		m.view = v
		assertFrame(t, m.View().Content, 40, 12)
	}

	m.view = viewLibrary
	m.prompt = promptRenamePlaylist
	m.promptInput = "通勤"
	assertFrame(t, m.View().Content, 40, 12)
}

func TestMultiSourceMarksAndHeader(t *testing.T) {
	m := fixtureModel()
	m.users = map[source.ID]source.User{
		source.Bilibili: {Name: "B 用户", LoggedIn: true},
		source.NetEase:  {Name: "云用户", LoggedIn: true},
	}
	m.playlists = append(m.playlists, store.Playlist{ID: "netease-pl-9", Source: "netease", Title: "云歌单", Kind: "remote", Count: 1})
	m.view = viewSearch
	m.searchMode = searchSource
	m.searchKeyword = "夜航星"
	m.searchResults = []source.Candidate{
		{Track: source.Track{Source: source.Bilibili, ExternalID: "BV1", Page: 1, Title: "B 版", Artist: "不才", Duration: 287}},
		{Track: source.Track{Source: source.NetEase, ExternalID: "2", Page: 1, Title: "云版", Artist: "不才", Duration: 287}},
	}
	body := ansi.Strip(m.View().Content)
	for _, want := range []string{"B 用户", "云用户", "[B]", "[云]", "B 版", "云版"} {
		if !strings.Contains(body, want) {
			t.Fatalf("多源界面应出现 %q，实际：\n%s", want, body)
		}
	}
	assertFrame(t, m.View().Content, 100, 30)
}

func TestProgressFill(t *testing.T) {
	cases := []struct {
		width      int
		pos, total time.Duration
		want       int
	}{
		{10, 0, 100 * time.Second, 0},
		{10, 50 * time.Second, 100 * time.Second, 5},
		{10, 100 * time.Second, 100 * time.Second, 10},
		{10, 150 * time.Second, 100 * time.Second, 10},
		{10, 5 * time.Second, 0, 0},
		{0, 5 * time.Second, 10 * time.Second, 0},
	}
	for _, c := range cases {
		if got := progressFill(c.width, c.pos, c.total); got != c.want {
			t.Fatalf("progressFill(%d,%v,%v)=%d，期望 %d", c.width, c.pos, c.total, got, c.want)
		}
	}
}

func TestProgressBarUsesBlueAndGray(t *testing.T) {
	m := fixtureModel()
	m.position, m.duration = 42*time.Second, 213*time.Second
	line := m.renderProgress()
	if !strings.Contains(line, "\x1b[") {
		t.Skip("当前终端未启用颜色，跳过颜色断言")
	}
	if !strings.Contains(line, "38;2;102;204;255") {
		t.Fatalf("已播放部分应为蓝色：%q", line)
	}
	if !strings.Contains(line, "38;2;75;82;99") {
		t.Fatalf("未播放部分应为灰色：%q", line)
	}
	if strings.Contains(line, "━") {
		t.Fatalf("进度条不应再使用加粗字符：%q", line)
	}
}

func TestLoginViewShowsBothAccounts(t *testing.T) {
	m := fixtureModel()
	m.view = viewLogin
	m.loginSource = source.NetEase
	m.users = map[source.ID]source.User{
		source.Bilibili: {Name: "B 用户", LoggedIn: true},
		source.NetEase:  {Name: "", LoggedIn: false},
	}
	m.qrStatus = "等待扫码"
	m.setQRCode("https://music.163.com/login?codekey=6e1b30ce-b3f3-4252-978c-1dcdefcf5dcb&chainId=v1_2AF04B72C39BBD034D1A724B7732985C204B01C5FF9ED08DC7AE_web_login_1792491167803")
	body := ansi.Strip(m.View().Content)
	for _, want := range []string{"账户", "Bilibili", "网易云", "已登录", "未登录", "B 用户"} {
		if !strings.Contains(body, want) {
			t.Fatalf("登录页应同时显示两个账户状态，缺少 %q：\n%s", want, body)
		}
	}
	assertFrame(t, m.View().Content, 100, 30)
}

func TestFoldersView(t *testing.T) {
	m := fixtureModel()
	m.view = viewFolders
	m.folderSource = source.Bilibili
	m.folderItems = []source.Playlist{
		{ID: "bili-fav-1", Source: source.Bilibili, Title: "我喜欢的音乐", Count: 128},
		{ID: "bili-fav-2", Source: source.Bilibili, Title: "通勤路上", Count: 42},
	}
	m.folderIdx = 1
	body := ansi.Strip(m.View().Content)
	for _, want := range []string{"添加 Bilibili 歌单/收藏夹", "我喜欢的音乐", "通勤路上", "128", "42"} {
		if !strings.Contains(body, want) {
			t.Fatalf("收藏夹选择页缺少 %q：\n%s", want, body)
		}
	}
	assertFrame(t, m.View().Content, 100, 30)
}

func TestLoginViewFitsNormalTerminalHeight(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 100, 24
	m.view = viewLogin
	m.loginSource = source.Bilibili
	m.users = map[source.ID]source.User{
		source.Bilibili: {Name: "", LoggedIn: false},
		source.NetEase:  {Name: "", LoggedIn: false},
	}
	m.qrStatus = "等待扫码"
	m.setQRCode("https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=8f3c1d2e4b5a69788796a5b4c3d2e1f0")
	frame := m.View().Content
	body := ansi.Strip(frame)
	if strings.Contains(body, "终端放不下") {
		t.Fatalf("B 站二维码在普通 24 行终端不应退化：\n%s", body)
	}
	if !strings.Contains(body, "等待扫码") || !strings.Contains(body, "Bilibili") {
		t.Fatalf("登录页内容缺失：\n%s", body)
	}
	assertFrame(t, frame, 100, 24)
}

func TestLoginHintWrapsAtNarrowWidth(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 80, 30
	m.view = viewLogin
	m.loginSource = source.Bilibili
	m.users = map[source.ID]source.User{
		source.Bilibili: {Name: "", LoggedIn: false},
		source.NetEase:  {Name: "", LoggedIn: false},
	}
	m.qrStatus = "等待扫码"
	m.setQRCode("https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=8f3c1d2e4b5a69788796a5b4c3d2e1f0")
	frame := m.View().Content
	assertFrame(t, frame, 80, 30)
	body := ansi.Strip(frame)
	for _, want := range []string{"二维码图片", "退出当前源"} {
		if !strings.Contains(body, want) {
			t.Fatalf("窄窗口下提示应换行而不是被截断，缺少 %q：\n%s", want, body)
		}
	}
}

func TestLoginViewNetEaseFits24Rows(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 100, 24
	m.view = viewLogin
	m.loginSource = source.NetEase
	m.users = map[source.ID]source.User{
		source.Bilibili: {Name: "B 用户", LoggedIn: true},
		source.NetEase:  {Name: "", LoggedIn: false},
	}
	m.qrStatus = "等待扫码"
	m.setQRCode("https://music.163.com/login?codekey=6e1b30ce-b3f3-4252-978c-1dcdefcf5dcb&chainId=v1_2AF04B72C39BBD034D1A724B7732985C204B01C5FF9ED08DC7AE_web_login_1792491167803")
	body := ansi.Strip(m.View().Content)
	if strings.Contains(body, "终端放不下二维码") {
		t.Fatalf("网易云二维码应能在 24 行终端显示：\n%s", body)
	}
	if !strings.Contains(body, "网易云") || !strings.Contains(body, "B 用户") || !strings.Contains(body, "等待扫码") {
		t.Fatalf("登录页内容缺失：\n%s", body)
	}
	assertFrame(t, m.View().Content, 100, 24)
}

func TestLoginViewHasHeaderWithoutAccount(t *testing.T) {
	m := fixtureModel()
	m.view = viewLogin
	m.loginSource = source.Bilibili
	m.users = map[source.ID]source.User{
		source.Bilibili: {Name: "", LoggedIn: false},
		source.NetEase:  {Name: "", LoggedIn: false},
	}
	m.qrStatus = "等待扫码"
	m.setQRCode("https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=8f3c1d2e4b5a69788796a5b4c3d2e1f0")
	frame := m.View().Content
	assertFrame(t, frame, 100, 30)
	lines := strings.Split(frame, "\n")
	top := ansi.Strip(lines[0])
	if !strings.Contains(top, "zero") || !strings.Contains(top, "登录") || !strings.Contains(top, "Tab") {
		t.Fatalf("登录页顶栏缺失：%q", top)
	}
	if strings.Contains(top, "账户") {
		t.Fatalf("登录页顶栏不应显示右侧账户信息：%q", top)
	}
}

func TestPaletteOverlayAndCreateBlank(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 100, 30
	m.view = viewLibrary
	m.openPalette()
	frame := m.View().Content
	body := ansi.Strip(frame)
	for _, want := range []string{"新建空白歌单", "同步 Bilibili 歌单/收藏夹", "新建混合歌单", "╭"} {
		if !strings.Contains(body, want) {
			t.Fatalf("命令面板缺少 %q：\n%s", want, body)
		}
	}
	assertFrame(t, frame, 100, 30)

	m.palette.input = "混合"
	foundMix := false
	for _, item := range m.filteredPaletteItems() {
		if item.action == palMix {
			foundMix = true
		}
	}
	if !foundMix {
		t.Fatalf("过滤结果应包含新建混合歌单: %+v", m.filteredPaletteItems())
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "palette.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	m.store = st
	m.openPalette()
	updated, _ := m.Update(keyPress("", tea.KeyEnter))
	mm := updated.(*Model)
	if mm.palette == nil || mm.palette.mode != paletteName {
		t.Fatalf("空白歌单应进入名称输入模式: %+v", mm.palette)
	}
	mm.palette.input = "测试歌单"
	updated, _ = mm.Update(keyPress("", tea.KeyEnter))
	mm = updated.(*Model)
	if mm.palette != nil || mm.view != viewLibrary {
		t.Fatalf("创建后应关闭面板: palette=%+v view=%v", mm.palette, mm.view)
	}
	list, _ := st.ListPlaylists()
	found := false
	for _, p := range list {
		if p.Title == "测试歌单" && p.Kind == "local" {
			found = true
		}
	}
	if !found {
		t.Fatalf("未创建空白歌单: %+v", list)
	}
}

func TestMixEditorCreatesReferences(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "mix.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a, _ := st.EnsurePlaylist(store.Playlist{ID: "a", Title: "A", Kind: "local"})
	b, _ := st.EnsurePlaylist(store.Playlist{ID: "b", Title: "B", Kind: "remote", Source: "netease", RemoteID: "2"})

	m := fixtureModel()
	m.width, m.height = 100, 30
	m.store = st
	m.playlists = []store.Playlist{a, b}
	m.view = viewLibrary
	if cmd := m.openMixEditor(""); cmd != nil {
		t.Fatalf("打开混合编辑器不应有异步命令")
	}
	if m.view != viewMix || m.mix == nil {
		t.Fatalf("应进入混合编辑视图: view=%v mix=%+v", m.view, m.mix)
	}
	assertFrame(t, m.View().Content, 100, 30)
	updated, _ := m.Update(keyPress(" ", ' '))
	mm := updated.(*Model)
	updated, _ = mm.Update(keyPress("", tea.KeyDown))
	mm = updated.(*Model)
	updated, _ = mm.Update(keyPress(" ", ' '))
	mm = updated.(*Model)
	updated, _ = mm.Update(keyPress("", tea.KeyEnter))
	mm = updated.(*Model)
	if mm.palette == nil || mm.palette.mode != paletteName || mm.palette.action != palNewMix {
		t.Fatalf("应进入混合歌单命名面板: %+v", mm.palette)
	}
	mm.palette.input = "混合测试"
	_ = mm.commitPaletteName()
	list, _ := st.ListPlaylists()
	var mix store.Playlist
	for _, p := range list {
		if p.Title == "混合测试" {
			mix = p
		}
	}
	if mix.Kind != "mixin" {
		t.Fatalf("未创建混合歌单: %+v", list)
	}
	refs, err := st.MixinRefs(mix.ID)
	if err != nil || len(refs) != 2 || refs[0].ID != "a" || refs[1].ID != "b" {
		t.Fatalf("引用不符: %+v err=%v", refs, err)
	}
}

func TestMixinAndUnlockKeys(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "keys.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	remote, _ := st.EnsurePlaylist(store.Playlist{ID: "r", Title: "远端", Kind: "remote", Source: "netease", RemoteID: "2"})
	local, _ := st.EnsurePlaylist(store.Playlist{ID: "l", Title: "本地", Kind: "local"})

	m := fixtureModel()
	m.store = st
	m.playlists = []store.Playlist{remote}
	m.playlistIdx = 0
	m.view = viewLibrary
	m.libraryFocus = focusPlaylists
	updated, _ := m.Update(keyPress("U", 'U'))
	mm := updated.(*Model)
	if mm.prompt != promptUnlockPlaylist {
		t.Fatalf("U 应进入解锁确认: %v", mm.prompt)
	}
	_ = mm.commitPrompt()
	unlocked, _ := st.Playlist("r")
	if unlocked.Kind != "local" {
		t.Fatalf("解锁后应为本地歌单: %+v", unlocked)
	}

	mix, err := st.CreateMixin("混合", []string{"l"})
	if err != nil {
		t.Fatalf("建混合歌单失败: %v", err)
	}
	m.playlists = []store.Playlist{local, mix}
	m.playlistIdx = 1
	m.view = viewLibrary
	updated, _ = m.Update(keyPress("M", 'M'))
	mm = updated.(*Model)
	if mm.view != viewMix || mm.mix == nil {
		t.Fatalf("M 应进入混合引用编辑: view=%v mix=%+v", mm.view, mm.mix)
	}
}

// TestHelpLinesKeepLongKeyCombo 验证帮助页按键列按最长组合自适应，
// 不再把「ctrl+u / PgUp / ctrl+d / PgDn」硬截成 20 列。
func TestHelpLinesKeepLongKeyCombo(t *testing.T) {
	m := fixtureModel()
	m.view = viewHelp
	body := ansi.Strip(strings.Join(m.helpLines(), "\n"))
	if !strings.Contains(body, "ctrl+u / PgUp / ctrl+d") {
		t.Fatalf("帮助页应完整显示翻页按键组合，实际：\n%s", body)
	}
	assertFrame(t, m.View().Content, 100, 30)
}

func TestHelpPopupAndAccountHint(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 100, 30
	m.view = viewLibrary
	body := ansi.Strip(m.View().Content)
	if !strings.Contains(body, "L 账户") {
		t.Fatalf("标题栏应提示 L 账户：\n%s", body)
	}
	if !strings.Contains(body, "H 帮助") {
		t.Fatalf("底部提示应简化并显示 H 帮助：\n%s", body)
	}
	updated, _ := m.Update(keyPress("H", 'H'))
	mm := updated.(*Model)
	if !mm.helpPopup {
		t.Fatal("H 应打开快捷键弹窗")
	}
	popup := ansi.Strip(mm.View().Content)
	if !strings.Contains(popup, "快捷键") || !strings.Contains(popup, "H / esc 关闭") {
		t.Fatalf("快捷键弹窗内容缺失：\n%s", popup)
	}
	assertFrame(t, mm.View().Content, 100, 30)
	updated, _ = mm.Update(keyPress("H", 'H'))
	mm = updated.(*Model)
	if mm.helpPopup {
		t.Fatal("再次按 H 应关闭弹窗")
	}
}

// TestLoginPageOOpensQRImage 验证登录页按 login_image（默认 o）触发打开二维码图片；
// 旧的 v 键继续兼容。
func TestLoginPageOOpensQRImage(t *testing.T) {
	cases := []struct {
		name string
		text string
		code rune
	}{
		{"配置键 o", "o", 'o'},
		{"兼容键 v", "v", 'v'},
	}
	for _, c := range cases {
		m := fixtureModel()
		m.view = viewLogin
		m.qrPath = ""
		updated, _ := m.Update(keyPress(c.text, c.code))
		mm := updated.(*Model)
		if mm.note.text != "二维码图片还没生成" {
			t.Fatalf("%s：登录页按键应尝试打开二维码图片，实际提示 %q", c.name, mm.note.text)
		}
		if mm.note.isErr {
			t.Fatalf("%s：二维码未生成应是普通提示而非错误", c.name)
		}
	}
}

// TestPaletteAboutOpensPopup 验证命令面板里的「关于 zero-beat」入口：
// 选中后关闭命令面板并弹出关于（替代已移除的「曲库 esc」入口）。
func TestPaletteAboutOpensPopup(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 100, 30
	m.openPalette()
	items := m.filteredPaletteItems()
	idx := -1
	for i, item := range items {
		if item.action == palAbout {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("命令面板缺少「关于」入口")
	}
	m.palette.idx = idx

	updated, _ := m.Update(keyPress("", tea.KeyEnter))
	mm := updated.(*Model)
	if mm.palette != nil {
		t.Fatal("选中「关于」后命令面板应关闭")
	}
	if !mm.aboutPopup {
		t.Fatal("命令面板选「关于 zero-beat」应打开关于弹窗")
	}
	body := ansi.Strip(mm.View().Content)
	for _, want := range []string{"关于", "版本", "AGPL-3.0", "github.com/Sheyiyuan/zero-beat"} {
		if !strings.Contains(body, want) {
			t.Fatalf("关于弹窗缺少 %q：\n%s", want, body)
		}
	}
	assertFrame(t, mm.View().Content, 100, 30)

	updated, _ = mm.Update(keyPress("", tea.KeyEscape))
	mm = updated.(*Model)
	if mm.aboutPopup {
		t.Fatal("esc 应关闭关于弹窗")
	}
	if mm.view != viewLibrary {
		t.Fatalf("关闭关于弹窗后应留在曲库，实际 %v", mm.view)
	}
}

// TestRenderDemoMenuAndAbout 验证 zero demo 新增的页面菜单与关于视图可截图。
func TestRenderDemoMenuAndAbout(t *testing.T) {
	cases := []struct {
		name string
		want []string
	}{
		{"menu", []string{"页面", "曲库", "本地搜索", "下载"}},
		{"about", []string{"关于", "AGPL-3.0", "github.com/Sheyiyuan/zero-beat"}},
	}
	for _, c := range cases {
		frame := RenderDemo(c.name, 100, 30)
		assertFrame(t, frame, 100, 30)
		body := ansi.Strip(frame)
		for _, want := range c.want {
			if !strings.Contains(body, want) {
				t.Fatalf("zero demo %s 缺少 %q：\n%s", c.name, want, body)
			}
		}
	}
}

func TestLoginSourceUpDown(t *testing.T) {
	b, n, reg := twoFakes()
	b.logged, n.logged = false, false
	m := fixtureModel()
	m.sources = reg
	m.loginSource = source.Bilibili
	m.view = viewLogin
	updated, cmd := m.Update(keyPress("", tea.KeyDown))
	mm := updated.(*Model)
	if mm.loginSource != source.NetEase {
		t.Fatalf("↓ 应切到网易云: %v", mm.loginSource)
	}
	if cmd == nil {
		t.Fatal("切源后应懒加载/复用二维码")
	}
	_ = cmd().(qrMsg)
	updated, _ = mm.Update(keyPress("", tea.KeyUp))
	mm = updated.(*Model)
	if mm.loginSource != source.Bilibili {
		t.Fatalf("↑ 应切回 B 站: %v", mm.loginSource)
	}
}

func TestLoginViewShowsUserInfoWhenLoggedIn(t *testing.T) {
	b, n, reg := twoFakes()
	b.logged = true
	n.logged = false
	m := fixtureModel()
	m.sources = reg
	m.loginSource = source.Bilibili
	m.view = viewLogin
	m.users = map[source.ID]source.User{
		source.Bilibili: {ID: "42", Name: "B 用户", LoggedIn: true},
	}
	if cmd := m.ensureLoginQR(); cmd != nil {
		t.Fatalf("已登录时不应申请二维码: %v", cmd)
	}
	if b.qrStarts != 0 {
		t.Fatalf("已登录时不应请求二维码: %d", b.qrStarts)
	}
	frame := m.View().Content
	body := ansi.Strip(frame)
	for _, want := range []string{"已登录", "B 用户", "42", "UID", "退出当前账号"} {
		if !strings.Contains(body, want) {
			t.Fatalf("登录管理页缺少用户信息 %q：\n%s", want, body)
		}
	}
	if strings.Contains(body, "扫码登录") || strings.Contains(body, "█") {
		t.Fatalf("已登录时不应展示二维码：\n%s", body)
	}
	assertFrame(t, frame, 100, 30)
}

func TestLocalSearchAndScopeToggle(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, s := range []store.Song{
		{Source: "bilibili", BVID: "BV1", Page: 1, Title: "夜航星", Artist: "不才", Duration: 200},
		{Source: "netease", RemoteID: "2", Page: 1, Title: "夜航", Artist: "测试", Duration: 180},
	} {
		if _, err := st.UpsertSong(s); err != nil {
			t.Fatalf("写歌失败: %v", err)
		}
	}
	m := fixtureModel()
	m.store = st
	m.view = viewLibrary
	m.songs = []store.Song{{Source: "bilibili", BVID: "BV1", Page: 1, Title: "夜航星", Artist: "不才", Duration: 200}}
	updated, _ := m.Update(keyPress("/", '/'))
	mm := updated.(*Model)
	if mm.view != viewSearch || mm.searchMode != searchLocalPlaylist || !mm.searchEditing {
		t.Fatalf("/ 应进入当前歌单搜索: view=%v mode=%v edit=%v", mm.view, mm.searchMode, mm.searchEditing)
	}
	mm.searchInput = "夜航"
	updated, _ = mm.Update(keyPress("", tea.KeyEnter))
	mm = updated.(*Model)
	if len(mm.localResults) != 1 || mm.localResults[0].Title != "夜航星" {
		t.Fatalf("当前歌单搜索不符: %+v", mm.localResults)
	}
	updated, _ = mm.Update(keyPress("r", 'r'))
	mm = updated.(*Model)
	if mm.searchMode != searchLocalAll {
		t.Fatalf("r 应切到本地全部: %v", mm.searchMode)
	}
	if len(mm.localResults) != 2 {
		t.Fatalf("本地全局搜索不符: %+v", mm.localResults)
	}
	updated, _ = mm.Update(keyPress("?", '?'))
	mm = updated.(*Model)
	if mm.searchMode != searchSource || !mm.searchEditing {
		t.Fatalf("? 应进入音源搜索: mode=%v edit=%v", mm.searchMode, mm.searchEditing)
	}
}

func TestSongRowHighlightStyles(t *testing.T) {
	m := fixtureModel()
	m.songs = []store.Song{
		{ID: store.SongID("bilibili", "BV1", 1), Source: "bilibili", BVID: "BV1", Page: 1, Title: "A", Artist: "a", Duration: 100},
		{ID: store.SongID("netease", "2", 1), Source: "netease", RemoteID: "2", Page: 1, Title: "B", Artist: "b", Duration: 120},
	}
	m.songIdx = 0
	m.current = &m.songs[1]
	selected := m.songRow(0, 80)
	playing := m.songRow(1, 80)
	if !strings.Contains(selected, "\x1b[") {
		t.Skip("当前终端未启用颜色，跳过背景断言")
	}
	if !strings.Contains(selected, "48;2;102;204;255") {
		t.Fatalf("选中行应为蓝色背景：%q", selected)
	}
	if !strings.Contains(playing, "48;2;58;63;74") {
		t.Fatalf("正在播放行应为灰色背景：%q", playing)
	}
	if !strings.Contains(ansi.Strip(playing), "▶") {
		t.Fatalf("正在播放行应保留播放图标：%q", ansi.Strip(playing))
	}
	if visWidth(selected) != 80 || visWidth(playing) != 80 {
		t.Fatalf("高亮行宽度不符: %d/%d", visWidth(selected), visWidth(playing))
	}
	m.current = &m.songs[0]
	both := m.songRow(0, 80)
	if !strings.Contains(both, "48;2;102;204;255") || !strings.Contains(ansi.Strip(both), "▶") {
		t.Fatalf("选中且正在播放时应保持蓝色背景和图标：%q", both)
	}
}

func TestSongRowThreeDigitIndexAndScroll(t *testing.T) {
	m := fixtureModel()
	m.songs = nil
	for i := 0; i < 100; i++ {
		m.songs = append(m.songs, store.Song{
			ID: store.SongID("bilibili", fmt.Sprintf("BV%d", i), 1), Source: "bilibili",
			BVID: fmt.Sprintf("BV%d", i), Page: 1,
			Title: "这是一个很长的歌曲名称用于滚动显示", Artist: "一个很长很长的艺术家名字",
			Duration: 200,
		})
	}
	m.songIdx = 0
	one := ansi.Strip(m.songRow(0, 60))
	ten := ansi.Strip(m.songRow(9, 60))
	hundred := ansi.Strip(m.songRow(99, 60))
	if visWidth(one) != 60 || visWidth(ten) != 60 || visWidth(hundred) != 60 {
		t.Fatalf("行宽不符: %d/%d/%d", visWidth(one), visWidth(ten), visWidth(hundred))
	}
	if !strings.Contains(one, "  1") || !strings.Contains(ten, " 10") || !strings.Contains(hundred, "100") {
		t.Fatalf("序号未按 3 位对齐:\n%q\n%q\n%q", one, ten, hundred)
	}
	// 选中行滚动：相同宽度下，偏移后内容应变化，且行宽不变。
	m.marqueeOffset = 0
	before := ansi.Strip(m.songRow(0, 60))
	m.marqueeOffset = 7
	after := ansi.Strip(m.songRow(0, 60))
	if before == after {
		t.Fatal("选中长标题/艺术家应滚动")
	}
	if visWidth(after) != 60 {
		t.Fatalf("滚动后行宽不符: %d", visWidth(after))
	}
	if got := scrollText("短", 10, 3); got != "短" {
		t.Fatalf("短文本不应滚动: %q", got)
	}
}

func TestMarqueeSpeed(t *testing.T) {
	m := fixtureModel()
	m.view = viewLibrary
	m.songIdx = 0
	m.advanceMarquee() // 首次只初始化
	if m.marqueeOffset != 0 {
		t.Fatalf("初始化偏移应为 0: %d", m.marqueeOffset)
	}
	m.advanceMarquee()
	if m.marqueeOffset != 2 {
		t.Fatalf("每个 tick 应前进 2 列: %d", m.marqueeOffset)
	}
	m.advanceMarquee()
	if m.marqueeOffset != 4 {
		t.Fatalf("第二次应到 4 列: %d", m.marqueeOffset)
	}
}
