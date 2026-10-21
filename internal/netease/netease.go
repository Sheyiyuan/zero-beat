// Package netease 是网易云音乐客户端，实现 source.Source。
//
// v0.3 起改用 go-musicfox/netease-music（MIT）作为协议层：它覆盖 weapi/eapi、
// 二维码登录所需的 chainId、cloudsearch 与 song_url_v1 的 8 档音质。
// 我们只保留自己的 Source 适配、错误分类和元数据映射，避免 TUI 依赖第三方类型。
package netease

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	neaservice "github.com/go-musicfox/netease-music/service"
	neautil "github.com/go-musicfox/netease-music/util"

	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
)

const (
	musicHost = "https://music.163.com"
	defaultUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

var (
	configureOnce sync.Once
	musicURL, _   = url.Parse(musicHost)
)

// APIError 是网易云返回的业务错误。
type APIError struct {
	Code    int
	Message string
	Path    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("netease: %s 返回 code=%d %s", e.Path, e.Code, e.Message)
}

// Client 是网易云客户端。协议细节由 go-musicfox/netease-music 处理。
var _ source.DailyRecommender = (*Client)(nil)
var _ source.PlaylistWriter = (*Client)(nil)

type Client struct {
	// opMu 串行化协议层调用：go-musicfox/netease-music 使用全局 cookie jar，
	// 并发请求与 SetSession/Logout 同时发生会读写同一份全局状态。
	opMu sync.Mutex

	mu      sync.RWMutex
	jar     http.CookieJar
	cookies map[string]string
	user    source.User
}

// New 创建客户端并初始化协议层。
func New() *Client {
	configure()
	c := &Client{cookies: map[string]string{}}
	c.SetSession(nil)
	return c
}

func configure() {
	configureOnce.Do(func() {
		// 不启用 UNM（版权绕过）与音效解锁，只做正常登录用户能播的内容。
		neautil.UNMSwitch = false
		neautil.ForceBestQuality = false
		neautil.EnableLocalVip = false
		neautil.UnlockSoundEffects = false
		neautil.Sources = nil
		neautil.UNMProxyURL = ""
		neautil.HTTPClientTimeout = 20 * time.Second
		// 协议层在非 200 时会直接 log.Printf；进 TUI 会污染整屏，统一丢弃。
		log.SetOutput(io.Discard)
	})
}

// ID 实现 source.Source。
func (c *Client) ID() source.ID { return source.NetEase }

// Name 实现 source.Source。
func (c *Client) Name() string { return "网易云" }

// Caps 实现 source.Source。
func (c *Client) Caps() source.Capability {
	return source.CapSearch | source.CapRemotePlaylists | source.CapOfficialLyrics |
		source.CapQualitySelect | source.CapLossless | source.CapDailyRecommendations |
		source.CapPlaylistWrite
}

// LoggedIn 实现 source.Source。
func (c *Client) LoggedIn() bool {
	return c.Session()["MUSIC_U"] != ""
}

// User 实现 source.Source。
func (c *Client) User() source.User {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.user
}

func (c *Client) setUser(u source.User) {
	c.mu.Lock()
	c.user = u
	c.mu.Unlock()
}

// Session 实现 source.Source；从协议层 cookie jar 读，保证扫码后拿到 MUSIC_U。
func (c *Client) Session() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.jar != nil {
		fresh := map[string]string{}
		for _, ck := range c.jar.Cookies(musicURL) {
			if ck.Name != "" && ck.Value != "" {
				fresh[ck.Name] = ck.Value
			}
		}
		c.cookies = fresh
	}
	out := make(map[string]string, len(c.cookies))
	for k, v := range c.cookies {
		out[k] = v
	}
	return out
}

// SetSession 实现 source.Source。
func (c *Client) SetSession(cookies map[string]string) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	jar, _ := cookiejar.New(nil)
	if len(cookies) > 0 {
		neautil.AddCookiesToJar(jar, cookies, musicHost)
	}
	if neautil.CheckSDeviceId(jar) == "" {
		jar.SetCookies(musicURL, []*http.Cookie{{
			Name: "sDeviceId", Value: neautil.GenerateSDeviceId(), Domain: musicURL.Hostname(), Path: "/",
		}})
	}
	neautil.SetGlobalCookieJar(jar)

	c.mu.Lock()
	c.jar = jar
	c.cookies = make(map[string]string, len(cookies))
	for k, v := range cookies {
		if k != "" && v != "" {
			c.cookies[k] = v
		}
	}
	c.user = source.User{}
	c.mu.Unlock()
}

// Logout 实现 source.Source。
func (c *Client) Logout() {
	c.SetSession(nil)
}

func (c *Client) cookieHeader() string {
	session := c.Session()
	if len(session) == 0 {
		return ""
	}
	keys := make([]string, 0, len(session))
	for k := range session {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+session[k])
	}
	return strings.Join(parts, "; ")
}

// Account 实现 source.Source。
func (c *Client) Account(ctx context.Context) (source.User, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.accountLocked(ctx)
}

func (c *Client) accountLocked(ctx context.Context) (source.User, error) {
	if err := ctx.Err(); err != nil {
		return source.User{}, err
	}
	svc := &neaservice.UserAccountService{}
	code, body := svc.AccountInfo()
	if code != 200 {
		c.setUser(source.User{})
		return source.User{}, &APIError{Code: int(code), Message: "账号信息不可用", Path: "nuser/account/get"}
	}
	u, err := parseAccount(body)
	if err != nil {
		return source.User{}, err
	}
	c.setUser(u)
	return u, nil
}

// StartQRLogin 实现 source.Source。协议层会生成新版登录需要的 chainId。
func (c *Client) StartQRLogin(ctx context.Context) (source.QRCode, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return source.QRCode{}, err
	}
	// unikey 请求不能带登录 cookie：旧 MUSIC_U/风控 cookie 可能让网易云返回 462。
	// 与 musicfox 一致：用一个只有 sDeviceId 的临时 jar 申请 key，申请完恢复原 jar
	// 继续轮询，这样登录成功后的 MUSIC_U 仍写回当前会话。
	originalJar := neautil.GetGlobalCookieJar()
	freshJar, _ := cookiejar.New(nil)
	if freshJar != nil {
		neautil.AddCookiesToJar(freshJar, map[string]string{"sDeviceId": neautil.GenerateSDeviceId()}, musicHost)
		neautil.SetGlobalCookieJar(freshJar)
		defer neautil.SetGlobalCookieJar(originalJar)
	}

	svc := &neaservice.LoginQRService{}
	code, _, _, err := svc.GetKey()
	if err != nil {
		return source.QRCode{}, fmt.Errorf("netease: 申请二维码: %w", err)
	}
	if code != 200 || svc.UniKey == "" {
		return source.QRCode{}, &APIError{Code: int(code), Message: "二维码生成失败", Path: "login/qrcode/unikey"}
	}
	// chainId 仍用原会话的设备指纹；unikey 请求本身不带旧 cookie。
	chainID := neautil.GenerateChainID(originalJar)
	qrURL := "https://music.163.com/login?codekey=" + svc.UniKey + "&chainId=" + chainID
	return source.QRCode{Key: svc.UniKey, URL: qrURL}, nil
}

// PollQRLogin 实现 source.Source。
func (c *Client) PollQRLogin(ctx context.Context, key string) (source.LoginStatus, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if key == "" {
		return source.LoginStatus{Code: 800, Message: "二维码已失效", Expired: true}, nil
	}
	if err := ctx.Err(); err != nil {
		return source.LoginStatus{}, err
	}
	svc := &neaservice.LoginQRService{UniKey: key}
	code, body, err := svc.CheckQR()
	if err != nil {
		return source.LoginStatus{}, fmt.Errorf("netease: 轮询二维码: %w", err)
	}
	return loginStatus(int(code), body), nil
}

func loginStatus(code int, body []byte) source.LoginStatus {
	status := source.LoginStatus{Code: code}
	switch code {
	case 800:
		status.Message = "二维码已失效"
		status.Expired = true
	case 801:
		status.Message = "等待扫码"
	case 802:
		status.Message = "已扫码，等待确认"
	case 803:
		status.Message = "登录成功"
		status.LoggedIn = true
	case 8821:
		status.Message = "触发风控，请稍后再试"
	default:
		status.Message = "状态码 " + strconv.Itoa(code)
	}
	if status.Message == "" {
		var data struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &data) == nil && data.Message != "" {
			status.Message = data.Message
		}
	}
	return status
}

type neArtist struct {
	Name string `json:"name"`
}

type neAlbum struct {
	Name   string `json:"name"`
	PicURL string `json:"picUrl"`
}

type neSong struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	Artists  []neArtist `json:"artists"`
	Ar       []neArtist `json:"ar"`
	Album    neAlbum    `json:"album"`
	Al       neAlbum    `json:"al"`
	Duration int64      `json:"duration"` // 毫秒
	DT       int64      `json:"dt"`       // cloudsearch 用 dt 表示毫秒时长
	Fee      int        `json:"fee"`
}

func (s neSong) artist() string {
	list := s.Ar
	if len(list) == 0 {
		list = s.Artists
	}
	names := make([]string, 0, len(list))
	for _, a := range list {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	return strings.Join(names, " / ")
}

func (s neSong) album() neAlbum {
	if s.Al.Name != "" || s.Al.PicURL != "" {
		return s.Al
	}
	return s.Album
}

func (s neSong) track() source.Track {
	ms := s.Duration
	if ms == 0 {
		ms = s.DT
	}
	return source.Track{
		Source:     source.NetEase,
		ExternalID: strconv.FormatInt(s.ID, 10),
		Page:       1,
		Title:      strings.TrimSpace(s.Name),
		Artist:     s.artist(),
		Album:      s.album().Name,
		Duration:   int((ms + 500) / 1000),
		Cover:      normalizeURL(s.album().PicURL),
	}
}

func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}

// Search 实现 source.Source。
func (c *Client) Search(ctx context.Context, keyword string, page int) (source.SearchPage, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if strings.TrimSpace(keyword) == "" {
		return source.SearchPage{Page: 1}, nil
	}
	if page <= 0 {
		page = 1
	}
	if err := ctx.Err(); err != nil {
		return source.SearchPage{}, err
	}
	const limit = 30
	svc := &neaservice.SearchService{
		S:      keyword,
		Type:   "1",
		Limit:  strconv.Itoa(limit),
		Offset: strconv.Itoa((page - 1) * limit),
	}
	code, body := svc.Search()
	if code != 200 {
		return source.SearchPage{}, &APIError{Code: int(code), Message: "搜索失败", Path: "cloudsearch"}
	}
	return parseSearch(body, page, limit)
}

func parseSearch(body []byte, page, limit int) (source.SearchPage, error) {
	var data struct {
		Code   int `json:"code"`
		Result struct {
			SongCount int      `json:"songCount"`
			Songs     []neSong `json:"songs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return source.SearchPage{}, fmt.Errorf("netease: 解析搜索响应: %w", err)
	}
	if data.Code != 0 && data.Code != 200 {
		return source.SearchPage{}, &APIError{Code: data.Code, Message: "搜索失败", Path: "cloudsearch"}
	}
	out := source.SearchPage{Page: page}
	for _, s := range data.Result.Songs {
		if s.ID == 0 {
			continue
		}
		out.Candidates = append(out.Candidates, source.Candidate{
			Track: s.track(),
			Fee:   s.Fee,
		})
	}
	out.Pages = (data.Result.SongCount + limit - 1) / limit
	if out.Pages <= 0 && len(out.Candidates) > 0 {
		out.Pages = 1
	}
	return out, nil
}

// Resolve 实现 source.Source。
func (c *Client) Resolve(ctx context.Context, ref source.Ref, quality source.Quality) (source.Resolved, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	id, err := strconv.ParseInt(ref.ExternalID, 10, 64)
	if err != nil {
		return source.Resolved{}, fmt.Errorf("netease: 歌曲 ID 无效: %s", ref.ExternalID)
	}
	if err := ctx.Err(); err != nil {
		return source.Resolved{}, err
	}
	svc := &neaservice.SongUrlV1Service{
		ID:         strconv.FormatInt(id, 10),
		Level:      levelFor(quality),
		SkipUNM:    true,
		EncodeType: encodeTypeFor(quality),
	}
	code, body, err := svc.SongUrl()
	if err != nil {
		return source.Resolved{}, fmt.Errorf("netease: 取流: %w", err)
	}
	if code != 200 {
		return source.Resolved{}, &APIError{Code: int(code), Message: "取流失败", Path: "song/enhance/player/url/v1"}
	}
	resolved, err := parseResolved(body, ref.ExternalID)
	if err != nil {
		return source.Resolved{}, err
	}
	if resolved.Stream.URL == "" {
		return source.Resolved{}, fmt.Errorf("netease: 该曲目无版权或需要会员")
	}
	resolved.Stream.Headers = map[string]string{
		"Referer":    musicHost + "/",
		"User-Agent": defaultUA,
	}
	if cookie := c.cookieHeader(); cookie != "" {
		resolved.Stream.Headers["Cookie"] = cookie
	}
	return resolved, nil
}

// Track 读取单曲元数据，供下载等非 TUI 场景使用。
func (c *Client) Track(ctx context.Context, ref source.Ref) (source.Track, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return source.Track{}, err
	}
	id, err := strconv.ParseInt(ref.ExternalID, 10, 64)
	if err != nil {
		return source.Track{}, fmt.Errorf("netease: 歌曲 ID 无效: %s", ref.ExternalID)
	}
	svc := &neaservice.SongDetailService{Ids: strconv.FormatInt(id, 10)}
	code, body := svc.SongDetail()
	if code != 200 {
		return source.Track{}, &APIError{Code: int(code), Message: "歌曲信息不可用", Path: "song/detail"}
	}
	return parseSongDetail(body)
}

func parseSongDetail(body []byte) (source.Track, error) {
	var data struct {
		Code  int      `json:"code"`
		Songs []neSong `json:"songs"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return source.Track{}, fmt.Errorf("netease: 解析歌曲信息: %w", err)
	}
	if data.Code != 0 && data.Code != 200 {
		return source.Track{}, &APIError{Code: data.Code, Message: "歌曲信息不可用", Path: "song/detail"}
	}
	if len(data.Songs) == 0 {
		return source.Track{}, fmt.Errorf("netease: 歌曲不存在")
	}
	return data.Songs[0].track(), nil
}

func levelFor(q source.Quality) neaservice.SongQualityLevel {
	if q == source.QualityLossless {
		return neaservice.Lossless
	}
	return neaservice.Exhigh
}

func encodeTypeFor(q source.Quality) string {
	if q == source.QualityLossless {
		return "flac"
	}
	return "mp3"
}

func parseResolved(body []byte, externalID string) (source.Resolved, error) {
	var data struct {
		Code int `json:"code"`
		Data []struct {
			ID            int64  `json:"id"`
			URL           string `json:"url"`
			Br            int    `json:"br"`
			Code          int    `json:"code"`
			Expi          int    `json:"expi"`
			Type          string `json:"type"`
			EncodeType    string `json:"encodeType"`
			Level         string `json:"level"`
			Fee           int    `json:"fee"`
			FreeTrialInfo any    `json:"freeTrialInfo"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return source.Resolved{}, fmt.Errorf("netease: 解析取流响应: %w", err)
	}
	if data.Code != 0 && data.Code != 200 {
		return source.Resolved{}, &APIError{Code: data.Code, Message: "取流失败", Path: "song/enhance/player/url/v1"}
	}
	if len(data.Data) == 0 {
		return source.Resolved{}, &APIError{Code: data.Code, Message: "没有返回音轨", Path: "song/enhance/player/url/v1"}
	}
	track := data.Data[0]
	if track.FreeTrialInfo != nil {
		return source.Resolved{}, fmt.Errorf("netease: 该曲目仅提供试听片段")
	}
	if strings.TrimSpace(track.URL) == "" {
		return source.Resolved{}, fmt.Errorf("netease: 该曲目无版权或需要会员")
	}
	expires := time.Now().Add(20 * time.Minute)
	if track.Expi > 0 {
		expires = time.Now().Add(time.Duration(track.Expi) * time.Second)
	}
	codec := track.Type
	if codec == "" {
		codec = track.EncodeType
	}
	return source.Resolved{
		Track: source.Track{
			Source:     source.NetEase,
			ExternalID: externalID,
			Page:       1,
		},
		Stream: source.Stream{
			URL:       normalizeURL(track.URL),
			Codec:     codec,
			ExpiresAt: expires,
			Quality:   qualityLabel(track.Level, track.Br),
		},
	}, nil
}

func qualityLabel(level string, br int) string {
	switch level {
	case "standard":
		return "128k"
	case "higher":
		return "192k"
	case "exhigh":
		return "320k"
	case "lossless":
		return "无损"
	case "hires":
		return "Hi-Res"
	case "jyeffect", "sky":
		return "沉浸音效"
	case "jymaster":
		return "母带"
	}
	if br > 0 {
		return fmt.Sprintf("%dk", br/1000)
	}
	return "未知音质"
}

// Lyrics 实现 source.Source。
func (c *Client) Lyrics(ctx context.Context, ref source.Ref) ([]lyrics.Line, string, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	svc := &neaservice.LyricService{ID: ref.ExternalID}
	code, body := svc.Lyric()
	if code != 200 {
		return nil, "", &APIError{Code: int(code), Message: "歌词不可用", Path: "song/lyric"}
	}
	lines, err := parseLyrics(body)
	if err != nil || len(lines) == 0 {
		return nil, "", err
	}
	return lines, "网易云歌词", nil
}

func parseLyrics(body []byte) ([]lyrics.Line, error) {
	var data struct {
		Code        int  `json:"code"`
		Uncollected bool `json:"uncollected"`
		NoLyric     bool `json:"nolyric"`
		PureMusic   bool `json:"pureMusic"`
		LRC         struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
		TLyric struct {
			Lyric string `json:"lyric"`
		} `json:"tlyric"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("netease: 解析歌词响应: %w", err)
	}
	if data.Code != 0 && data.Code != 200 {
		return nil, &APIError{Code: data.Code, Message: "歌词不可用", Path: "song/lyric"}
	}
	// 无版权 / 未收录的曲目会带着 uncollected 返回一行「暂无歌词」占位，
	// 必须当作没有歌词，才能继续走 LRCLIB 兜底。
	if data.Uncollected || data.NoLyric {
		return nil, nil
	}
	lines := lyrics.ParseLRC(data.LRC.Lyric)
	if !data.PureMusic && len(lines) == 1 && isPlaceholderLyric(lines[0].Text) {
		return nil, nil
	}
	mergeLyricTranslation(lines, lyrics.ParseLRC(data.TLyric.Lyric))
	return lines, nil
}

// mergeLyricTranslation 按起始时间把 tlyric 挂到原文行上。
// 时间戳对不上（或该行没有译文）时留空，不强行错位匹配。
func mergeLyricTranslation(lines, translated []lyrics.Line) {
	if len(lines) == 0 || len(translated) == 0 {
		return
	}
	byStart := make(map[int]string, len(translated))
	for _, line := range translated {
		if text := strings.TrimSpace(line.Text); text != "" {
			byStart[line.StartMS] = text
		}
	}
	for i := range lines {
		if text, ok := byStart[lines[i].StartMS]; ok {
			lines[i].Translation = text
		}
	}
}

// isPlaceholderLyric 判断网易云用一行文案表示「这首歌没有歌词」的情况。
func isPlaceholderLyric(text string) bool {
	switch strings.TrimSpace(text) {
	case "暂无歌词", "暂无歌词。", "暂无歌词~":
		return true
	}
	return false
}

// DailyRecommendations 实现 source.DailyRecommender。
func (c *Client) DailyRecommendations(ctx context.Context) ([]source.Track, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if !c.LoggedIn() {
		return nil, fmt.Errorf("netease: 登录后才能获取每日推荐")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	svc := &neaservice.RecommendSongsService{}
	code, body := svc.RecommendSongs()
	if code != 200 {
		return nil, &APIError{Code: int(code), Message: "每日推荐不可用", Path: "discovery/recommend/songs"}
	}
	return parseDailySongs(body)
}

func parseDailySongs(body []byte) ([]source.Track, error) {
	var data struct {
		Code int `json:"code"`
		Data struct {
			DailySongs []neSong `json:"dailySongs"`
			Songs      []neSong `json:"songs"`
		} `json:"data"`
		DailySongs []neSong `json:"dailySongs"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("netease: 解析每日推荐: %w", err)
	}
	if data.Code != 0 && data.Code != 200 {
		return nil, &APIError{Code: data.Code, Message: "每日推荐不可用", Path: "discovery/recommend/songs"}
	}
	raw := data.Data.DailySongs
	if len(raw) == 0 {
		raw = data.Data.Songs
	}
	if len(raw) == 0 {
		raw = data.DailySongs
	}
	out := make([]source.Track, 0, len(raw))
	for _, s := range raw {
		if s.ID == 0 {
			continue
		}
		out = append(out, s.track())
	}
	return out, nil
}

// RemotePlaylists 实现 source.Source。
func (c *Client) RemotePlaylists(ctx context.Context) ([]source.Playlist, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	u := c.User()
	if !u.LoggedIn || u.ID == "" {
		account, err := c.accountLocked(ctx)
		if err != nil {
			return nil, err
		}
		if !account.LoggedIn {
			return nil, nil
		}
		u = account
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	svc := &neaservice.UserPlaylistService{Uid: u.ID, Limit: "1000", Offset: "0"}
	code, body := svc.UserPlaylist()
	if code != 200 {
		return nil, &APIError{Code: int(code), Message: "歌单列表不可用", Path: "user/playlist"}
	}
	return parsePlaylists(body)
}

func parsePlaylists(body []byte) ([]source.Playlist, error) {
	var data struct {
		Code     int `json:"code"`
		Playlist []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			TrackCount int    `json:"trackCount"`
		} `json:"playlist"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("netease: 解析歌单响应: %w", err)
	}
	if data.Code != 0 && data.Code != 200 {
		return nil, &APIError{Code: data.Code, Message: "歌单列表不可用", Path: "user/playlist"}
	}
	out := make([]source.Playlist, 0, len(data.Playlist))
	for _, p := range data.Playlist {
		if p.ID == 0 {
			continue
		}
		out = append(out, source.Playlist{
			ID:       "netease-pl-" + strconv.FormatInt(p.ID, 10),
			Source:   source.NetEase,
			RemoteID: strconv.FormatInt(p.ID, 10),
			Title:    p.Name,
			Count:    p.TrackCount,
		})
	}
	return out, nil
}

// PlaylistTracks 实现 source.Source。
func (c *Client) PlaylistTracks(ctx context.Context, remoteID string) ([]source.Track, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	id, err := strconv.ParseInt(remoteID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("netease: 歌单 ID 无效: %s", remoteID)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	svc := &neaservice.PlaylistDetailService{Id: strconv.FormatInt(id, 10), S: "8"}
	code, body := svc.PlaylistDetail()
	if code != 200 {
		return nil, &APIError{Code: int(code), Message: "歌单不可用", Path: "playlist/detail"}
	}
	return parsePlaylistTracks(body)
}

// AddToPlaylist 实现 source.PlaylistWriter：把一首歌加入当前账号的网易云歌单。
func (c *Client) AddToPlaylist(ctx context.Context, remoteID string, ref source.Ref) error {
	if ref.Source != source.NetEase {
		return fmt.Errorf("netease: 不能把 %s 的歌曲加入网易云歌单", ref.Source)
	}
	if ref.ExternalID == "" {
		return fmt.Errorf("netease: 歌曲 ID 为空")
	}
	if !c.LoggedIn() {
		return fmt.Errorf("netease: 请先登录后再写入云端歌单")
	}
	c.ensureCsrf()
	id, err := strconv.ParseInt(remoteID, 10, 64)
	if err != nil {
		return fmt.Errorf("netease: 歌单 ID 无效: %s", remoteID)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	trackIDs, err := json.Marshal([]string{ref.ExternalID})
	if err != nil {
		return fmt.Errorf("netease: 编码歌曲 ID: %w", err)
	}
	// 新接口是 playlist/manipulate/tracks；旧的 playlist/track/add 在部分账号上会返回 401。
	options := &neautil.Options{
		Crypto:  "weapi",
		Cookies: []*http.Cookie{{Name: "os", Value: "pc"}},
	}
	code, body, _ := neautil.CreateRequest("POST",
		"https://music.163.com/api/playlist/manipulate/tracks",
		map[string]string{
			"op":       "add",
			"pid":      strconv.FormatInt(id, 10),
			"trackIds": string(trackIDs),
			"imme":     "true",
		}, options)
	if code != 200 {
		if int(code) == 401 {
			return fmt.Errorf("netease: 加入歌单被拒（401）：登录态/CSRF 失效，或目标歌单不属于当前账号；请重新登录或换一个自己创建的歌单")
		}
		return &APIError{Code: int(code), Message: "加入歌单失败", Path: "playlist/manipulate/tracks"}
	}
	if err := parsePlaylistWriteResponse(body); err != nil {
		if apiErr, ok := err.(*APIError); ok && apiErr.Code == 401 {
			return fmt.Errorf("netease: 加入歌单被拒（401）：登录态/CSRF 失效，或目标歌单不属于当前账号；请重新登录或换一个自己创建的歌单")
		}
		return err
	}
	return nil
}

// ensureCsrf 确保 jar 里有 __csrf。部分登录路径只保存 MUSIC_U，
// 写歌单时没有这个 cookie 会被网易云以 401 拒绝。
func (c *Client) ensureCsrf() {
	if c.Session()["__csrf"] != "" {
		return
	}
	c.mu.Lock()
	jar := c.jar
	c.mu.Unlock()
	if jar == nil {
		return
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return
	}
	token := hex.EncodeToString(buf)
	jar.SetCookies(musicURL, []*http.Cookie{{Name: "__csrf", Value: token, Path: "/"}})
	c.mu.Lock()
	c.cookies["__csrf"] = token
	c.mu.Unlock()
}

func parsePlaylistWriteResponse(body []byte) error {
	var data struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return fmt.Errorf("netease: 解析加歌响应: %w", err)
	}
	if data.Code != 0 && data.Code != 200 {
		return &APIError{Code: data.Code, Message: data.Message, Path: "playlist/manipulate/tracks"}
	}
	return nil
}

func parsePlaylistTracks(body []byte) ([]source.Track, error) {
	var data struct {
		Code   int `json:"code"`
		Result struct {
			Tracks []neSong `json:"tracks"`
		} `json:"result"`
		Playlist struct {
			Tracks []neSong `json:"tracks"`
		} `json:"playlist"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("netease: 解析歌单详情: %w", err)
	}
	if data.Code != 0 && data.Code != 200 {
		return nil, &APIError{Code: data.Code, Message: "歌单不可用", Path: "playlist/detail"}
	}
	raw := data.Result.Tracks
	if len(raw) == 0 {
		raw = data.Playlist.Tracks // v3 /playlist/detail 把 tracks 放在 playlist 下
	}
	out := make([]source.Track, 0, len(raw))
	for _, s := range raw {
		if s.ID == 0 {
			continue
		}
		out = append(out, s.track())
	}
	return out, nil
}

func parseAccount(body []byte) (source.User, error) {
	var data struct {
		Code    int `json:"code"`
		Account *struct {
			ID int64 `json:"id"`
		} `json:"account"`
		Profile *struct {
			UserID    int64  `json:"userId"`
			Nickname  string `json:"nickname"`
			AvatarURL string `json:"avatarUrl"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return source.User{}, fmt.Errorf("netease: 解析账号响应: %w", err)
	}
	if data.Profile == nil {
		return source.User{}, nil
	}
	id := data.Profile.UserID
	if id == 0 && data.Account != nil {
		id = data.Account.ID
	}
	return source.User{
		ID:       strconv.FormatInt(id, 10),
		Name:     data.Profile.Nickname,
		Avatar:   data.Profile.AvatarURL,
		LoggedIn: id != 0,
	}, nil
}
