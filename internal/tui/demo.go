package tui

import (
	"time"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/bili"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// RenderDemo 用示例数据渲染一帧界面，供 `zero demo` 与文档截图使用。
//
// 它走的是真实的 View 代码路径，所以截图与运行时完全一致。
func RenderDemo(name string, width, height int) string {
	songs := []store.Song{
		{ID: "d1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "【官方 MV】Never Gonna Give You Up", Artist: "Rick Astley", Duration: 213},
		{ID: "d2", Source: "bilibili", BVID: "BV2", Page: 1, Title: "夜航星", Artist: "不才", Duration: 287},
		{ID: "d3", Source: "bilibili", BVID: "BV3", Page: 1, Title: "春庭雪", Artist: "等什么君", Duration: 192},
		{ID: "d4", Source: "bilibili", BVID: "BV4", Page: 1, Title: "漠河舞厅", Artist: "柳爽", Duration: 237},
		{ID: "d5", Source: "bilibili", BVID: "BV5", Page: 1, Title: "海底", Artist: "一支榴莲", Duration: 246},
		{ID: "d6", Source: "bilibili", BVID: "BV6", Page: 1, Title: "像风一样", Artist: "薛之谦", Duration: 232},
		{ID: "d7", Source: "bilibili", BVID: "BV7", Page: 1, Title: "光年之外", Artist: "G.E.M. 邓紫棋", Duration: 235},
	}

	m := &Model{
		width:    width,
		height:   height,
		view:     viewLibrary,
		volume:   0.76,
		compDB:   2,
		mode:     modeSequence,
		state:    audio.StatePlaying,
		position: 84 * time.Second,
		duration: 287 * time.Second,
		user:     bili.User{Name: "Sheyiyuan", LoggedIn: true},
	}
	m.playlists = []store.Playlist{
		{ID: "f1", Title: "我喜欢的音乐", Kind: "remote", Count: 128},
		{ID: "f2", Title: "通勤路上", Kind: "remote", Count: 42},
		{ID: "local-default", Title: "本地歌单", Kind: "local", Count: 7},
	}
	m.songs = songs
	m.queue = append([]store.Song(nil), songs...)
	m.current = &songs[1]
	m.songIdx = 1
	m.queueIdx = 1
	m.queueCursor = 1

	m.searchKeyword = "周杰伦"
	m.searchResults = []bili.SearchItem{
		{BVID: "BV1xx411c7mD", Title: "稻香", Author: "周杰伦", Duration: 223, Play: 4839201},
		{BVID: "BV1fx411N7bU", Title: "晴天", Author: "周杰伦", Duration: 269, Play: 3921044},
		{BVID: "BV1Ks411J7bH", Title: "夜曲", Author: "周杰伦", Duration: 226, Play: 2817233},
		{BVID: "BV1Ws411i7Rk", Title: "以父之名", Author: "周杰伦", Duration: 341, Play: 1502881},
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
	m.demoQuality = "192k"

	switch name {
	case "search":
		m.view = viewSearch
	case "queue":
		m.view = viewQueue
	case "lyrics":
		m.view = viewLyrics
	case "help":
		m.view = viewHelp
	case "login":
		m.view = viewLogin
		m.qrStatus = "用 B 站 App 扫码登录"
		m.setQRCode("https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=8f3c1d2e4b5a69788796a5b4c3d2e1f0&source=main-fe-header")
	default:
		m.view = viewLibrary
	}
	return m.View().Content
}
