package tui

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

type fakeSource struct {
	id         source.ID
	name       string
	user       source.User
	logged     bool
	search     source.SearchPage
	searchErr  error
	resolved   source.Resolved
	resolveErr error
	lyricLines []lyrics.Line
	lyricSrc   string
	lyricErr   error
	playlists  []source.Playlist
	tracks     []source.Track
	daily      []source.Track
	session    map[string]string
	qrStarts   int
	addedTo    string
	addedRef   source.Ref
	writeErr   error
}

func (f *fakeSource) ID() source.ID { return f.id }
func (f *fakeSource) Name() string  { return f.name }
func (f *fakeSource) Caps() source.Capability {
	caps := source.CapSearch | source.CapRemotePlaylists | source.CapOfficialLyrics | source.CapPlaylistWrite
	if len(f.daily) > 0 {
		caps |= source.CapDailyRecommendations
	}
	return caps
}
func (f *fakeSource) LoggedIn() bool    { return f.logged }
func (f *fakeSource) User() source.User { return f.user }
func (f *fakeSource) Account(context.Context) (source.User, error) {
	return f.user, nil
}
func (f *fakeSource) StartQRLogin(context.Context) (source.QRCode, error) {
	f.qrStarts++
	return source.QRCode{Key: "k-" + string(f.id), URL: "https://example/" + string(f.id)}, nil
}
func (f *fakeSource) PollQRLogin(context.Context, string) (source.LoginStatus, error) {
	return source.LoginStatus{LoggedIn: true, Message: "ok"}, nil
}
func (f *fakeSource) Search(context.Context, string, int) (source.SearchPage, error) {
	return f.search, f.searchErr
}
func (f *fakeSource) Resolve(context.Context, source.Ref, source.Quality) (source.Resolved, error) {
	return f.resolved, f.resolveErr
}
func (f *fakeSource) Lyrics(context.Context, source.Ref) ([]lyrics.Line, string, error) {
	return f.lyricLines, f.lyricSrc, f.lyricErr
}
func (f *fakeSource) RemotePlaylists(context.Context) ([]source.Playlist, error) {
	return f.playlists, nil
}
func (f *fakeSource) PlaylistTracks(context.Context, string) ([]source.Track, error) {
	return f.tracks, nil
}
func (f *fakeSource) DailyRecommendations(context.Context) ([]source.Track, error) {
	return f.daily, nil
}
func (f *fakeSource) AddToPlaylist(_ context.Context, remoteID string, ref source.Ref) error {
	f.addedTo = remoteID
	f.addedRef = ref
	return f.writeErr
}
func (f *fakeSource) Session() map[string]string   { return f.session }
func (f *fakeSource) SetSession(map[string]string) {}
func (f *fakeSource) Logout()                      { f.logged = false }

func twoFakes() (*fakeSource, *fakeSource, *source.Registry) {
	b := &fakeSource{
		id: source.Bilibili, name: "Bilibili", logged: true,
		user: source.User{Name: "B 用户", LoggedIn: true},
		search: source.SearchPage{Page: 1, Pages: 2, Candidates: []source.Candidate{
			{Track: source.Track{Source: source.Bilibili, ExternalID: "BV1", Page: 1, Title: "B1", Artist: "ba", Duration: 100}},
		}},
		playlists: []source.Playlist{{ID: "bili-fav-1", Source: source.Bilibili, RemoteID: "1", Title: "B 收藏", Count: 1}},
		tracks:    []source.Track{{Source: source.Bilibili, ExternalID: "BV1", Page: 1, Title: "B1", Artist: "ba", Duration: 100}},
	}
	n := &fakeSource{
		id: source.NetEase, name: "网易云", logged: true,
		user: source.User{Name: "云用户", LoggedIn: true},
		search: source.SearchPage{Page: 1, Pages: 3, Candidates: []source.Candidate{
			{Track: source.Track{Source: source.NetEase, ExternalID: "2", Page: 1, Title: "N1", Artist: "na", Album: "专辑", Duration: 200}},
		}},
		playlists: []source.Playlist{{ID: "netease-pl-2", Source: source.NetEase, RemoteID: "2", Title: "云歌单", Count: 1}},
		tracks:    []source.Track{{Source: source.NetEase, ExternalID: "2", Page: 1, Title: "N1", Artist: "na", Album: "专辑", Duration: 200}},
		daily:     []source.Track{{Source: source.NetEase, ExternalID: "9", Page: 1, Title: "日推", Artist: "na", Duration: 180}},
	}
	return b, n, source.NewRegistry(b, n)
}

func TestSearchMergesSourcesAndWarns(t *testing.T) {
	b, n, reg := twoFakes()
	m := fixtureModel()
	m.sources = reg
	m.searchKeyword = "x"
	m.searchSource = ""
	msg := m.searchCmd("x", 1)().(searchMsg)
	if len(msg.candidates) != 2 || msg.pages != 3 {
		t.Fatalf("合并搜索不符: %+v", msg)
	}
	if msg.err != nil || msg.warn != nil {
		t.Fatalf("两源都成功时不应有错误: err=%v warn=%v", msg.err, msg.warn)
	}
	if msg.candidates[0].Source != source.Bilibili || msg.candidates[1].Source != source.NetEase {
		t.Fatalf("候选顺序应按注册表: %+v", msg.candidates)
	}

	n.searchErr = context.DeadlineExceeded
	msg = m.searchCmd("x", 1)().(searchMsg)
	if len(msg.candidates) != 1 || msg.warn == nil || msg.err != nil {
		t.Fatalf("单源失败应保留结果并告警: %+v", msg)
	}

	b.searchErr = context.DeadlineExceeded
	msg = m.searchCmd("x", 1)().(searchMsg)
	if msg.err == nil || len(msg.candidates) != 0 {
		t.Fatalf("全部失败应报错: %+v", msg)
	}
}

func TestSearchSourceCycle(t *testing.T) {
	_, _, reg := twoFakes()
	m := fixtureModel()
	m.sources = reg
	if cmd := m.cycleSearchSource(); cmd != nil || m.searchSource != source.Bilibili {
		t.Fatalf("第一次应切到 B 站: %v %q", cmd, m.searchSource)
	}
	_ = m.cycleSearchSource()
	if m.searchSource != source.NetEase {
		t.Fatalf("第二次应切到网易云: %q", m.searchSource)
	}
	_ = m.cycleSearchSource()
	if m.searchSource != "" {
		t.Fatalf("第三次应回到全部: %q", m.searchSource)
	}
	m.searchKeyword = "x"
	if cmd := m.cycleSearchSource(); cmd == nil || !m.searchBusy {
		t.Fatal("有关键词时应自动重新搜索")
	}
}

func TestLoadUsersFromSources(t *testing.T) {
	_, _, reg := twoFakes()
	m := fixtureModel()
	m.sources = reg
	msg := m.loadUsersCmd()().(usersMsg)
	if len(msg.users) != 2 || msg.users[source.Bilibili].Name != "B 用户" || msg.users[source.NetEase].Name != "云用户" {
		t.Fatalf("用户加载不符: %+v", msg.users)
	}
}

func TestLoadPlaylistsAndSongsFromSources(t *testing.T) {
	_, _, reg := twoFakes()
	st, err := store.Open(filepath.Join(t.TempDir(), "multi.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	m := fixtureModel()
	m.sources = reg
	m.store = st
	msg := m.loadPlaylistsCmd()().(playlistsMsg)
	if msg.err != nil || msg.warn != nil {
		t.Fatalf("加载歌单失败: err=%v warn=%v", msg.err, msg.warn)
	}
	found := map[string]store.Playlist{}
	for _, p := range msg.items {
		found[p.ID] = p
	}
	if _, ok := found["netease-pl-2"]; ok {
		t.Fatalf("网易云歌单不应自动同步: %+v", msg.items)
	}
	if _, ok := found["bili-fav-1"]; ok {
		t.Fatalf("B 站收藏夹不应自动同步: %+v", msg.items)
	}
	daily, ok := found[dailyPlaylistID]
	if !ok {
		t.Fatalf("网易云日推入口缺失: %+v", msg.items)
	}
	dmsg := m.loadSongsCmdFor(daily, false)().(songsMsg)
	if dmsg.err != nil || len(dmsg.items) != 1 || dmsg.items[0].RemoteID != "9" {
		t.Fatalf("日推曲目不符: %+v", dmsg)
	}

	// B 站收藏夹按需添加：先拉候选，再确认加入本地。
	folders := m.loadRemoteFoldersCmd(source.Bilibili)().(foldersMsg)
	if folders.err != nil || len(folders.items) != 1 || folders.items[0].ID != "bili-fav-1" {
		t.Fatalf("B 站收藏夹候选不符: %+v", folders)
	}
	m.folderItems = folders.items
	m.folderIdx = 0
	_ = m.addSelectedFolder()
	items, err := m.store.ListPlaylists()
	if err != nil {
		t.Fatalf("列出歌单失败: %v", err)
	}
	var added store.Playlist
	for _, item := range items {
		if item.ID == "bili-fav-1" {
			added = item
		}
	}
	if added.Source != "bilibili" {
		t.Fatalf("按需添加的 B 站收藏夹来源不符: %+v", items)
	}

	p := added
	smsg := m.loadSongsCmdFor(p, false)().(songsMsg)
	if smsg.err != nil || len(smsg.items) != 1 {
		t.Fatalf("加载歌单曲目失败: %+v", smsg)
	}
	if smsg.items[0].Source != "bilibili" || smsg.items[0].RemoteID != "BV1" {
		t.Fatalf("曲目来源不符: %+v", smsg.items[0])
	}

	// 网易云歌单也按需导入。
	nf := m.loadRemoteFoldersCmd(source.NetEase)().(foldersMsg)
	if nf.err != nil || len(nf.items) != 1 || nf.items[0].ID != "netease-pl-2" {
		t.Fatalf("网易云歌单候选不符: %+v", nf)
	}
	m.folderItems = nf.items
	m.folderIdx = 0
	_ = m.addSelectedFolder()
	items, err = m.store.ListPlaylists()
	if err != nil {
		t.Fatalf("列出歌单失败: %v", err)
	}
	var npl store.Playlist
	for _, item := range items {
		if item.ID == "netease-pl-2" {
			npl = item
		}
	}
	nmsg := m.loadSongsCmdFor(npl, false)().(songsMsg)
	if nmsg.err != nil || len(nmsg.items) != 1 || nmsg.items[0].Source != "netease" {
		t.Fatalf("网易云歌单曲目不符: %+v", nmsg)
	}
}

func TestResolveDispatchesToSource(t *testing.T) {
	b, _, reg := twoFakes()
	b.resolved = source.Resolved{
		Track:  source.Track{Source: source.Bilibili, ExternalID: "BV1", Page: 1, CID: 88, Title: "解析标题", Artist: "解析作者", Duration: 123},
		Stream: source.Stream{URL: "https://cdn/stream.m4s", Codec: "mp4a.40.2", Duration: 123 * time.Second, Quality: "192k", ExpiresAt: time.Now().Add(time.Hour)},
	}
	m := fixtureModel()
	m.sources = reg
	m.store = nil
	song := store.Song{Source: "bilibili", RemoteID: "BV1", Page: 1, Title: "原标题"}
	msg := m.playSongCmdAt(song, 5*time.Second, 0)().(streamMsg)
	if msg.err != nil {
		t.Fatalf("解析失败: %v", msg.err)
	}
	if msg.song.Title != "解析标题" || msg.song.CID != 88 || msg.stream.URL == "" {
		t.Fatalf("解析结果未合并: %+v %+v", msg.song, msg.stream)
	}
	if msg.startAt != 5*time.Second {
		t.Fatalf("起始位置丢失: %v", msg.startAt)
	}
}

func TestLyricsFromSource(t *testing.T) {
	b, _, reg := twoFakes()
	b.lyricLines = []lyrics.Line{{StartMS: 1000, Text: "源歌词"}}
	b.lyricSrc = "B站字幕"
	m := fixtureModel()
	m.sources = reg
	m.lrclib = nil
	song := store.Song{Source: "bilibili", RemoteID: "BV1", Page: 1, CID: 88, Title: "B1"}
	msg := m.lyricsCmd(song)().(lyricMsg)
	if len(msg.lines) != 1 || msg.lines[0].Text != "源歌词" || msg.source != "B站字幕" {
		t.Fatalf("歌词未走音源: %+v", msg)
	}
}

func TestMixinRefreshesRemoteReference(t *testing.T) {
	_, n, reg := twoFakes()
	st, err := store.Open(filepath.Join(t.TempDir(), "mixin.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.EnsurePlaylist(store.Playlist{ID: "netease-pl-2", Source: "netease", Title: "云歌单", Kind: "remote", RemoteID: "2"}); err != nil {
		t.Fatalf("建远端歌单失败: %v", err)
	}
	if err := st.AddSongToPlaylist("netease-pl-2", store.Song{Source: "netease", RemoteID: "2", Page: 1, Title: "旧曲"}); err != nil {
		t.Fatalf("加歌失败: %v", err)
	}
	mix, err := st.CreateMixin("混合", []string{"netease-pl-2"})
	if err != nil {
		t.Fatalf("建混合歌单失败: %v", err)
	}
	m := fixtureModel()
	m.sources = reg
	m.store = st
	n.tracks = []source.Track{{Source: source.NetEase, ExternalID: "2", Page: 1, Title: "新曲", Duration: 200}}
	msg := m.loadSongsCmdFor(mix, false)().(songsMsg)
	if msg.err != nil || len(msg.items) != 1 || msg.items[0].Title != "新曲" {
		t.Fatalf("混合歌单应刷新引用: %+v", msg)
	}
}

func TestLoginQRCacheLazyAndExpiry(t *testing.T) {
	b, n, reg := twoFakes()
	b.logged, n.logged = false, false
	m := fixtureModel()
	m.sources = reg
	m.loginSource = source.Bilibili
	m.view = viewLogin

	cmd := m.ensureLoginQR()
	if cmd == nil {
		t.Fatal("首次应发起 B 站二维码请求")
	}
	msg := cmd().(qrMsg)
	if b.qrStarts != 1 {
		t.Fatalf("首次应拉取 B 站二维码: starts=%d", b.qrStarts)
	}
	mm, _ := m.Update(msg)
	m = mm.(*Model)
	if m.qr == nil || m.qrSource != source.Bilibili {
		t.Fatalf("二维码状态不符: %+v", m.qr)
	}
	if cmd := m.ensureLoginQR(); cmd != nil {
		t.Fatalf("有效缓存不应重复拉取")
	}

	// 切到网易云会单独拉一张并缓存；切回 B 站复用旧图。
	cmd = m.switchLoginSource(1)
	if cmd == nil || m.loginSource != source.NetEase {
		t.Fatalf("切源后应发起网易云二维码请求: login=%v cmd=%v", m.loginSource, cmd)
	}
	_ = cmd().(qrMsg)
	if n.qrStarts != 1 {
		t.Fatalf("切源后应拉取网易云二维码: starts=%d", n.qrStarts)
	}
	_ = m.switchLoginSource(-1)
	if b.qrStarts != 1 {
		t.Fatalf("切回 B 站应复用缓存，不应重新请求: starts=%d", b.qrStarts)
	}

	// 过期后重新拉取。
	m.qrCache[source.Bilibili].expiresAt = time.Now().Add(-time.Second)
	cmd = m.ensureLoginQR()
	if cmd == nil {
		t.Fatal("过期后应发起重新拉取")
	}
	_ = cmd().(qrMsg)
	if b.qrStarts != 2 {
		t.Fatalf("过期后应重新拉取: starts=%d", b.qrStarts)
	}
}

func TestLoginQRExpiredRefetches(t *testing.T) {
	b, n, reg := twoFakes()
	b.logged, n.logged = false, false
	_ = n
	m := fixtureModel()
	m.sources = reg
	m.loginSource = source.Bilibili
	m.view = viewLogin
	cmd := m.ensureLoginQR()
	if cmd == nil {
		t.Fatal("首次应发起二维码请求")
	}
	msg := cmd().(qrMsg)
	mm, _ := m.Update(msg)
	m = mm.(*Model)
	if b.qrStarts != 1 {
		t.Fatalf("首次请求次数不符: %d", b.qrStarts)
	}
	updated, cmd := m.Update(qrPollMsg{source: source.Bilibili, status: source.LoginStatus{Code: 800, Expired: true}})
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("失效后应自动重新拉取")
	}
	_ = cmd().(qrMsg)
	if b.qrStarts != 2 {
		t.Fatalf("失效后应重新请求: %d", b.qrStarts)
	}
}
