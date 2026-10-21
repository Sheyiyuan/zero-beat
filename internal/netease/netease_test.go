package netease

import (
	"testing"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/source"
)

func TestCapsIncludePlaylistWrite(t *testing.T) {
	c := New()
	if !c.Caps().Has(source.CapPlaylistWrite) || !c.Caps().Has(source.CapRemotePlaylists) {
		t.Fatalf("网易云能力位不完整: %v", c.Caps())
	}
}

func TestParseSearch(t *testing.T) {
	body := []byte(`{"code":200,"result":{"songCount":31,"songs":[
		{"id":1431292823,"name":"夜航星 (Night Voyager)","dt":303701,"fee":8,
		 "ar":[{"name":"蔡明希（不才）"},{"name":"三体宇宙"}],
		 "al":{"name":"我的三体之章北海传 原声音乐集","picUrl":"//p1/x.jpg"}}
	]}}`)
	page, err := parseSearch(body, 2, 30)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if page.Page != 2 || page.Pages != 2 || len(page.Candidates) != 1 {
		t.Fatalf("分页结果不符: %+v", page)
	}
	item := page.Candidates[0]
	if item.ExternalID != "1431292823" || item.Duration != 304 || item.Fee != 8 {
		t.Fatalf("条目字段不符: %+v", item)
	}
	if item.Artist != "蔡明希（不才） / 三体宇宙" || item.Album == "" || item.Cover != "https://p1/x.jpg" {
		t.Fatalf("艺术家/专辑/封面不符: %+v", item.Track)
	}
}

func TestParseResolved(t *testing.T) {
	body := []byte(`{"code":200,"data":[{
		"id":1431292823,"url":"http://cdn.example/song.mp3","br":320000,
		"code":200,"expi":1200,"type":"mp3","encodeType":"mp3","level":"exhigh","fee":8
	}]}`)
	res, err := parseResolved(body, "1431292823")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if res.Stream.URL != "http://cdn.example/song.mp3" || res.Stream.Quality != "320k" || res.Stream.Codec != "mp3" {
		t.Fatalf("流字段不符: %+v", res.Stream)
	}
	if time.Until(res.Stream.ExpiresAt) < 15*time.Minute {
		t.Fatalf("expi 未生效: %v", res.Stream.ExpiresAt)
	}

	if _, err := parseResolved([]byte(`{"code":200,"data":[{"id":1,"url":"","fee":1,"code":-110}]}`), "1"); err == nil {
		t.Fatal("空地址应报错")
	}
	if _, err := parseResolved([]byte(`{"code":200,"data":[]}`), "1"); err == nil {
		t.Fatal("空 data 应报错")
	}
	if _, err := parseResolved([]byte(`{"code":200,"data":[{"id":1,"url":"http://x","freeTrialInfo":{"start":0}}]}`), "1"); err == nil {
		t.Fatal("试听片段应报错")
	}
}

func TestParseLyrics(t *testing.T) {
	body := []byte(`{"code":200,"lrc":{"lyric":"[00:01.00]第一句\n[00:05.50]第二句"}}`)
	lines, err := parseLyrics(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(lines) != 2 || lines[1].StartMS != 5500 {
		t.Fatalf("歌词解析不符: %+v", lines)
	}
}

func TestParseLyricsTranslation(t *testing.T) {
	body := []byte(`{"code":200,
		"lrc":{"lyric":"[00:01.00]Hello\n[00:05.50]World\n[00:09.00]No translation"},
		"tlyric":{"lyric":"[00:01.00]你好\n[00:05.50]世界\n[00:20.00]时间对不上"}}`)
	lines, err := parseLyrics(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("歌词行数不符: %+v", lines)
	}
	if lines[0].Translation != "你好" || lines[1].Translation != "世界" {
		t.Fatalf("译文未按时间戳挂到原文: %+v", lines)
	}
	if lines[2].Translation != "" {
		t.Fatalf("时间戳对不上的译文不应强行匹配: %+v", lines[2])
	}
}

func TestParseLyricsSkipsPlaceholders(t *testing.T) {
	cases := map[string][]byte{
		"uncollected": []byte(`{"uncollected":true,"code":200,"lrc":{"lyric":"[00:00.00]暂无歌词"}}`),
		"nolyric":     []byte(`{"nolyric":true,"code":200,"lrc":{"lyric":"[00:00.00]暂无歌词"}}`),
		"placeholder": []byte(`{"code":200,"lrc":{"lyric":"[00:00.00]暂无歌词"}}`),
	}
	for name, body := range cases {
		lines, err := parseLyrics(body)
		if err != nil {
			t.Fatalf("%s: 解析失败: %v", name, err)
		}
		if len(lines) != 0 {
			t.Fatalf("%s: 占位歌词应视为没有歌词，实际 %+v", name, lines)
		}
	}

	// 纯音乐保留平台给的一句提示，避免去 LRCLIB 误配成别的版本。
	body := []byte(`{"pureMusic":true,"code":200,"lrc":{"lyric":"[00:00.00] 作曲 : 呱唧\n[00:05.00]纯音乐，请欣赏"}}`)
	lines, err := parseLyrics(body)
	if err != nil || len(lines) != 2 {
		t.Fatalf("纯音乐歌词解析失败: %+v %v", lines, err)
	}
}

func TestParseSongDetail(t *testing.T) {
	body := []byte(`{"code":200,"songs":[{"id":1,"name":"A","ar":[{"name":"歌手"}],"al":{"name":"专辑","picUrl":"//p"},"dt":223000}]}`)
	track, err := parseSongDetail(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if track.ExternalID != "1" || track.Title != "A" || track.Artist != "歌手" || track.Album != "专辑" || track.Duration != 223 || track.Cover != "https://p" {
		t.Fatalf("歌曲信息不符: %+v", track)
	}
	if _, err := parseSongDetail([]byte(`{"code":200,"songs":[]}`)); err == nil {
		t.Fatal("空 songs 应报错")
	}
}

func TestParsePlaylists(t *testing.T) {
	body := []byte(`{"code":200,"playlist":[
		{"id":123,"name":"我喜欢的音乐","trackCount":2},
		{"id":456,"name":"通勤","trackCount":1}
	]}`)
	lists, err := parsePlaylists(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(lists) != 2 || lists[0].ID != "netease-pl-123" || lists[0].Source != source.NetEase {
		t.Fatalf("歌单映射不符: %+v", lists)
	}
}

func TestParsePlaylistTracksSupportsBothShapes(t *testing.T) {
	resultShape := []byte(`{"code":200,"result":{"tracks":[
		{"id":1,"name":"A","duration":100000,"fee":0,"ar":[{"name":"歌手"}],"al":{"name":"专辑"}}
	]}}`)
	tracks, err := parsePlaylistTracks(resultShape)
	if err != nil || len(tracks) != 1 || tracks[0].Duration != 100 {
		t.Fatalf("result.tracks 解析失败: %+v %v", tracks, err)
	}
	playlistShape := []byte(`{"code":200,"playlist":{"tracks":[
		{"id":2,"name":"B","dt":223000,"artists":[{"name":"歌手2"}],"album":{"name":"专辑2"}}
	]}}`)
	tracks, err = parsePlaylistTracks(playlistShape)
	if err != nil || len(tracks) != 1 || tracks[0].Duration != 223 || tracks[0].Artist != "歌手2" {
		t.Fatalf("playlist.tracks 解析失败: %+v %v", tracks, err)
	}
}

func TestParseAccount(t *testing.T) {
	u, err := parseAccount([]byte(`{"code":200,"account":{"id":42},"profile":{"userId":42,"nickname":"云村用户","avatarUrl":"https://a"}}`))
	if err != nil || !u.LoggedIn || u.Name != "云村用户" || u.ID != "42" {
		t.Fatalf("账号解析不符: %+v %v", u, err)
	}
	u, err = parseAccount([]byte(`{"code":200,"account":null,"profile":null}`))
	if err != nil || u.LoggedIn {
		t.Fatalf("未登录应返回空用户: %+v %v", u, err)
	}
}

func TestLoginStatus(t *testing.T) {
	cases := map[int]struct {
		message string
		logged  bool
		expired bool
	}{
		800:  {"二维码已失效", false, true},
		801:  {"等待扫码", false, false},
		802:  {"已扫码，等待确认", false, false},
		803:  {"登录成功", true, false},
		8821: {"触发风控，请稍后再试", false, false},
	}
	for code, want := range cases {
		got := loginStatus(code, nil)
		if got.Message != want.message || got.LoggedIn != want.logged || got.Expired != want.expired {
			t.Fatalf("loginStatus(%d) = %+v，期望 %+v", code, got, want)
		}
	}
}

func TestQualityMapping(t *testing.T) {
	if levelFor(source.QualityAuto) != "exhigh" || levelFor(source.QualityLossless) != "lossless" {
		t.Fatal("音质档位映射不符")
	}
	if encodeTypeFor(source.QualityHigh) != "mp3" || encodeTypeFor(source.QualityLossless) != "flac" {
		t.Fatal("编码类型映射不符")
	}
	if qualityLabel("exhigh", 0) != "320k" || qualityLabel("lossless", 0) != "无损" {
		t.Fatal("音质标签不符")
	}
}

func TestSessionRoundTripAndLogout(t *testing.T) {
	c := New()
	c.SetSession(map[string]string{"MUSIC_U": "token-1", "__csrf": "csrf-1"})
	if !c.LoggedIn() {
		t.Fatal("MUSIC_U 应视为已登录")
	}
	session := c.Session()
	if session["MUSIC_U"] != "token-1" || session["__csrf"] != "csrf-1" {
		t.Fatalf("会话未恢复: %+v", session)
	}
	c.Logout()
	if c.LoggedIn() || c.Session()["MUSIC_U"] != "" {
		t.Fatalf("退出后应清空凭证: %+v", c.Session())
	}
}

func TestNormalizeURL(t *testing.T) {
	if normalizeURL("//p1/x.jpg") != "https://p1/x.jpg" || normalizeURL("http://p1/x.jpg") != "http://p1/x.jpg" {
		t.Fatal("URL 归一化不符")
	}
}

func TestParseDailySongs(t *testing.T) {
	body := []byte(`{"code":200,"data":{"dailySongs":[
		{"id":101,"name":"日推一","dt":180000,"fee":0,"ar":[{"name":"歌手"}],"al":{"name":"专辑"}},
		{"id":102,"name":"日推二","duration":200000,"fee":8,"artists":[{"name":"歌手2"}],"album":{"name":"专辑2"}}
	]}}`)
	tracks, err := parseDailySongs(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(tracks) != 2 || tracks[0].ExternalID != "101" || tracks[0].Duration != 180 || tracks[1].Duration != 200 {
		t.Fatalf("日推解析不符: %+v", tracks)
	}
	if tracks[1].Artist != "歌手2" || tracks[1].Album != "专辑2" {
		t.Fatalf("日推元数据不符: %+v", tracks[1])
	}
}

func TestParsePlaylistWriteResponse(t *testing.T) {
	if err := parsePlaylistWriteResponse([]byte(`{"code":200}`)); err != nil {
		t.Fatalf("code=200 应成功: %v", err)
	}
	if err := parsePlaylistWriteResponse([]byte(`{"code":0}`)); err != nil {
		t.Fatalf("code=0 应成功: %v", err)
	}
	if err := parsePlaylistWriteResponse([]byte(`{"code":502,"message":"歌单不存在"}`)); err == nil {
		t.Fatal("非成功 code 应报错")
	}
}

func TestEnsureCsrf(t *testing.T) {
	c := New()
	c.SetSession(map[string]string{"MUSIC_U": "test-session"})
	c.ensureCsrf()
	first := c.Session()["__csrf"]
	if first == "" {
		t.Fatal("缺少 __csrf 时应自动补一个")
	}
	c.ensureCsrf()
	if second := c.Session()["__csrf"]; second != first {
		t.Fatalf("已有 __csrf 时不应重新生成: %q -> %q", first, second)
	}
}
