package tui

import (
	"context"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// RenderDemo 用示例数据渲染一帧界面，供 `zero demo` 与文档截图使用。
//
// 它走的是真实的 View 代码路径，所以截图与运行时完全一致。
func RenderDemo(name string, width, height int) string {
	applyTheme(resolveThemeMode(nil).palette())
	songs := []store.Song{
		{ID: "d1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "【官方 MV】Never Gonna Give You Up", Artist: "Rick Astley", Duration: 213},
		{ID: "d2", Source: "bilibili", BVID: "BV2", Page: 1, Title: "夜航星", Artist: "不才", Duration: 287},
		{ID: "d3", Source: "bilibili", BVID: "BV3", Page: 1, Title: "春庭雪", Artist: "等什么君", Duration: 192},
		{ID: "d4", Source: "bilibili", BVID: "BV4", Page: 1, Title: "漠河舞厅", Artist: "柳爽", Duration: 237},
		{ID: "d5", Source: "bilibili", BVID: "BV5", Page: 1, Title: "海底", Artist: "一支榴莲", Duration: 246},
		{ID: "d6", Source: "bilibili", BVID: "BV6", Page: 1, Title: "像风一样", Artist: "薛之谦", Duration: 232},
		{ID: "d7", Source: "bilibili", BVID: "BV7", Page: 1, Title: "光年之外", Artist: "G.E.M. 邓紫棋", Duration: 235},
		{ID: "netease:1431292823:1", Source: "netease", RemoteID: "1431292823", Page: 1, Title: "夜航星 (Night Voyager)", Artist: "蔡明希（不才） / 三体宇宙", Album: "我的三体之章北海传 原声音乐集", Duration: 304},
	}

	m := &Model{
		sources: source.NewRegistry(
			demoSource{id: source.Bilibili, name: "Bilibili", mark: "B", lyric: "B 站字幕", login: "B 站 App"},
			demoSource{id: source.NetEase, name: "网易云", mark: "云", lyric: "网易云歌词", login: "网易云音乐 App"},
		),
		width:       width,
		height:      height,
		view:        viewLibrary,
		volume:      0.76,
		compDB:      2,
		mode:        modeSequence,
		state:       audio.StatePlaying,
		position:    84 * time.Second,
		duration:    287 * time.Second,
		loginSource: source.Bilibili,
		users: map[source.ID]source.User{
			source.Bilibili: {Name: "Sheyiyuan", LoggedIn: true},
			source.NetEase:  {Name: "云村用户", LoggedIn: true},
		},
	}
	m.playlists = []store.Playlist{
		{ID: app.DailyPlaylistID(source.NetEase), Source: "netease", Title: "每日推荐", Kind: "remote", Count: 30},
		{ID: "bili-fav-1", Source: "bilibili", Title: "我喜欢的音乐", Kind: "remote", Count: 128},
		{ID: "netease-pl-9", Source: "netease", Title: "云村收藏", Kind: "remote", Count: 42},
		{ID: "local-default", Title: "本地歌单", Kind: "local", Count: 7},
	}
	m.songs = songs
	m.queue = append([]store.Song(nil), songs...)
	m.current = &songs[1]
	m.songIdx = 1
	m.queueIdx = 1
	m.queueCursor = 1

	m.downloadItems = []downloadItem{
		{entry: store.DownloadEntry{Download: store.Download{SongID: songs[1].ID, Path: "/home/user/Music/zero-beat/不才 - 夜航星.flac", Size: 38 * 1024 * 1024, Codec: "flac"}, Song: songs[1]}},
		{entry: store.DownloadEntry{Download: store.Download{SongID: songs[2].ID, Path: "/home/user/Music/zero-beat/等什么君 - 春庭雪.m4a", Size: 9 * 1024 * 1024, Codec: "aac"}, Song: songs[2]}},
		{entry: store.DownloadEntry{Download: store.Download{SongID: songs[3].ID, Path: "/home/user/Music/zero-beat/柳爽 - 漠河舞厅.m4a", Size: 10 * 1024 * 1024, Codec: "aac"}, Song: songs[3]}, missing: true},
	}

	m.searchKeyword = "周杰伦"
	m.searchPage = 1
	m.searchPages = 5
	m.searchResults = []source.Candidate{
		{Track: source.Track{Source: source.Bilibili, ExternalID: "BV1xx411c7mD", Page: 1, Title: "稻香", Artist: "周杰伦", Duration: 223}, Play: 4839201},
		{Track: source.Track{Source: source.NetEase, ExternalID: "1858094998", Page: 1, Title: "稻香", Artist: "周杰伦", Album: "魔杰座", Duration: 223}},
		{Track: source.Track{Source: source.Bilibili, ExternalID: "BV1fx411N7bU", Page: 1, Title: "晴天", Artist: "周杰伦", Duration: 269}, Play: 3921044},
		{Track: source.Track{Source: source.NetEase, ExternalID: "186016", Page: 1, Title: "晴天", Artist: "周杰伦", Album: "叶惠美", Duration: 269}},
	}
	m.lyricLines = []lyrics.Line{
		{StartMS: 0, Text: "夜航星"},
		{StartMS: 6000, Text: "作词：不才"},
		{StartMS: 11000, Text: "作曲：不才"},
		{StartMS: 18000, Text: "穿过黑夜的航线"},
		{StartMS: 24000, Text: "把星光都点燃"},
		{StartMS: 31000, Text: "我在无人的甲板上"},
		{StartMS: 38000, Text: "数着遥远的灯盏"},
		{StartMS: 46000, Text: "如果风会记得"},
		{StartMS: 52000, Text: "我曾来过这里"},
		{StartMS: 60000, Text: "就让这首歌替我说"},
		{StartMS: 68000, Text: "别害怕 天快亮了"},
		{StartMS: 76000, Text: "夜航星 请带我回家"},
	}
	m.lyricIdx = 10
	m.lyricLoaded = true
	m.demoQuality = "192k"

	switch name {
	case "search":
		m.view = viewSearch
		m.searchMode = searchSource
	case "queue":
		m.view = viewQueue
	case "lyrics":
		m.view = viewLyrics
		m.lyricSource = "B站字幕"
	case "help":
		m.view = viewHelp
	case "login":
		m.view = viewLogin
		m.loginSource = source.NetEase
		m.qrSource = source.NetEase
		m.qrStatus = "等待扫码"
		m.setQRCode("https://music.163.com/login?codekey=8f3c1d2e4b5a69788796a5b4c3d2e1f0")
	case "clip":
		m.setActiveClip(m.current.ID, store.Clip{StartMS: 30000, EndMS: 120000})
		m.position = 84 * time.Second
		m.clip = &clipState{songID: m.current.ID, startMS: 30000, endMS: 120000}
	case "downloads":
		m.view = viewDownloads
		m.downloadIdx = 1
	case "picker":
		m.picker = &targetPickerState{
			mode: targetPickerAddSong,
			song: *m.current,
			items: []playlistTarget{
				{ID: "local-default", Title: "本地歌单", Kind: "local", Count: 7},
				{ID: "mix-1", Title: "通勤合集", Kind: "mixin", Count: 38},
				{ID: "bili-fav-1", Title: "我喜欢的音乐", Kind: "remote", Source: "bilibili", RemoteID: "1", Count: 128, Cloud: true},
				{Title: "＋ 新建本地歌单并加入", Create: true},
			},
		}
	default:
		m.view = viewLibrary
	}
	return m.View().Content
}

// demoSource 只提供展示信息，保证脚本截图不依赖真实网络客户端。
type demoSource struct {
	id    source.ID
	name  string
	mark  string
	lyric string
	login string
}

func (d demoSource) ID() source.ID           { return d.id }
func (d demoSource) Name() string            { return d.name }
func (d demoSource) Mark() string            { return d.mark }
func (d demoSource) LyricName() string       { return d.lyric }
func (d demoSource) LoginAppName() string    { return d.login }
func (d demoSource) Caps() source.Capability { return 0 }
func (d demoSource) LoggedIn() bool          { return true }
func (d demoSource) User() source.User       { return source.User{Name: d.name, LoggedIn: true} }
func (d demoSource) Account(context.Context) (source.User, error) {
	return d.User(), nil
}
func (d demoSource) StartQRLogin(context.Context) (source.QRCode, error) {
	return source.QRCode{}, nil
}
func (d demoSource) PollQRLogin(context.Context, string) (source.LoginStatus, error) {
	return source.LoginStatus{}, nil
}
func (d demoSource) Search(context.Context, string, int) (source.SearchPage, error) {
	return source.SearchPage{}, nil
}
func (d demoSource) Resolve(context.Context, source.Ref, source.Quality) (source.Resolved, error) {
	return source.Resolved{}, nil
}
func (d demoSource) Lyrics(context.Context, source.Ref) ([]lyrics.Line, string, error) {
	return nil, d.lyric, nil
}
func (d demoSource) RemotePlaylists(context.Context) ([]source.Playlist, error) {
	return nil, nil
}
func (d demoSource) PlaylistTracks(context.Context, string) ([]source.Track, error) {
	return nil, nil
}
func (d demoSource) Session() map[string]string   { return nil }
func (d demoSource) SetSession(map[string]string) {}
func (d demoSource) Logout()                      {}
