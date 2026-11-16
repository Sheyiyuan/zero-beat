package bili

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Nav 拉取登录用户信息；未登录不算错误，返回 LoggedIn=false。
func (c *Client) Nav(ctx context.Context) (User, error) {
	env, err := c.rawEnvelope(ctx, apiHost+"/x/web-interface/nav", "https://www.bilibili.com")
	if err != nil {
		return User{}, err
	}
	var data struct {
		IsLogin bool   `json:"isLogin"`
		Mid     int64  `json:"mid"`
		Uname   string `json:"uname"`
		Face    string `json:"face"`
	}
	if len(env.Data) > 0 {
		if err := jsonUnmarshal(env.Data, &data); err != nil {
			return User{}, fmt.Errorf("bilibili: 解析用户信息: %w", err)
		}
	}
	user := User{Mid: data.Mid, Name: data.Uname, Face: data.Face, LoggedIn: data.IsLogin && env.Code == 0}
	c.setUser(user)
	return user, nil
}

// Search 搜索视频，返回结果与总页数。
func (c *Client) Search(ctx context.Context, keyword string, page int) ([]SearchItem, int, error) {
	if strings.TrimSpace(keyword) == "" {
		return nil, 0, nil
	}
	if page <= 0 {
		page = 1
	}
	params := url.Values{
		"search_type": {"video"},
		"keyword":     {keyword},
		"page":        {strconv.Itoa(page)},
		"page_size":   {"30"},
	}
	signed, err := c.signedQuery(params)
	if err != nil {
		return nil, 0, err
	}
	items, pages, err := c.doSearch(ctx, signed)
	if err != nil {
		if isRiskControl(err) {
			// 签名可能失效，退回不带签名的请求重试一次。
			plain := url.Values{
				"search_type": {"video"},
				"keyword":     {keyword},
				"page":        {strconv.Itoa(page)},
				"page_size":   {"30"},
			}
			return c.doSearch(ctx, plain)
		}
		return nil, 0, err
	}
	return items, pages, nil
}

func (c *Client) doSearch(ctx context.Context, params url.Values) ([]SearchItem, int, error) {
	var data struct {
		NumPages int `json:"numPages"`
		Result   []struct {
			Type     string `json:"type"`
			BVID     string `json:"bvid"`
			Title    string `json:"title"`
			Author   string `json:"author"`
			Pic      string `json:"pic"`
			Duration string `json:"duration"`
			Play     int64  `json:"play"`
		} `json:"result"`
	}
	endpoint := withQuery(apiHost+"/x/web-interface/wbi/search/type", params)
	if err := c.getJSON(ctx, endpoint, "https://search.bilibili.com", &data); err != nil {
		return nil, 0, err
	}
	items := make([]SearchItem, 0, len(data.Result))
	for _, r := range data.Result {
		if r.Type != "" && r.Type != "video" {
			continue
		}
		if r.BVID == "" {
			continue
		}
		items = append(items, SearchItem{
			BVID:     r.BVID,
			Title:    stripEm(r.Title),
			Author:   stripEm(r.Author),
			Cover:    normalizeURL(r.Pic),
			Duration: parseClock(r.Duration),
			Play:     r.Play,
		})
	}
	pages := data.NumPages
	if pages <= 0 && len(items) > 0 {
		pages = 1
	}
	return items, pages, nil
}

// VideoInfo 拉取视频与分 P 信息。
func (c *Client) VideoInfo(ctx context.Context, bvid string) (Video, error) {
	var data struct {
		Aid      int64  `json:"aid"`
		BVID     string `json:"bvid"`
		Title    string `json:"title"`
		Pic      string `json:"pic"`
		Duration int64  `json:"duration"`
		Owner    struct {
			Name string `json:"name"`
		} `json:"owner"`
		Pages []struct {
			CID      int64  `json:"cid"`
			Page     int    `json:"page"`
			Part     string `json:"part"`
			Duration int64  `json:"duration"`
		} `json:"pages"`
	}
	if err := c.getJSON(ctx, apiHost+"/x/web-interface/view?bvid="+url.QueryEscape(bvid), videoReferer(bvid), &data); err != nil {
		return Video{}, err
	}
	v := Video{
		Aid:      data.Aid,
		BVID:     data.BVID,
		Title:    data.Title,
		Cover:    normalizeURL(data.Pic),
		Owner:    data.Owner.Name,
		Duration: data.Duration,
	}
	for _, p := range data.Pages {
		v.Pages = append(v.Pages, VideoPage{CID: p.CID, Page: p.Page, Part: p.Part, Duration: p.Duration})
	}
	if len(v.Pages) == 0 {
		return Video{}, fmt.Errorf("bilibili: 视频 %s 没有分 P 信息", bvid)
	}
	return v, nil
}

type dashAudio struct {
	ID        int      `json:"id"`
	BaseURL   string   `json:"baseUrl"`
	BackupURL []string `json:"backup_url"`
	Bandwidth int64    `json:"bandwidth"`
	Codecs    string   `json:"codecs"`
	MimeType  string   `json:"mimeType"`
}

type dashData struct {
	Duration int64       `json:"duration"`
	Audio    []dashAudio `json:"audio"`
	Flac     *struct {
		Display bool       `json:"display"`
		Audio   *dashAudio `json:"audio"`
	} `json:"flac"`
	Dolby *struct {
		Audio []dashAudio `json:"audio"`
	} `json:"dolby"`
}

// PlayURL 解析指定分 P 的可播放音频流。
func (c *Client) PlayURL(ctx context.Context, bvid string, cid int64, quality Quality) (Stream, error) {
	if cid == 0 {
		info, err := c.VideoInfo(ctx, bvid)
		if err != nil {
			return Stream{}, err
		}
		cid = info.Pages[0].CID
	}
	params := url.Values{
		"bvid":  {bvid},
		"cid":   {strconv.FormatInt(cid, 10)},
		"fnval": {"4048"},
		"fnver": {"0"},
		"fourk": {"1"},
	}
	var data struct {
		Dash dashData `json:"dash"`
	}
	if err := c.getJSON(ctx, withQuery(apiHost+"/x/player/playurl", params), videoReferer(bvid), &data); err != nil {
		return Stream{}, err
	}
	track, err := pickAudio(data.Dash, quality)
	if err != nil {
		return Stream{}, err
	}
	streamURL := track.URL
	if streamURL == "" && len(track.BackupURL) > 0 {
		streamURL = track.BackupURL[0]
	}
	if streamURL == "" {
		return Stream{}, fmt.Errorf("bilibili: 未拿到可播放地址")
	}
	durationMS := data.Dash.Duration * 1000
	return Stream{
		URL:        streamURL,
		Headers:    map[string]string{"Referer": videoReferer(bvid), "User-Agent": defaultUA},
		Codec:      track.Codecs,
		DurationMS: durationMS,
		ExpiresAt:  streamExpiry(streamURL, time.Now()),
		Quality:    track.Quality,
		BVID:       bvid,
		CID:        cid,
	}, nil
}

// streamExpiry 从 CDN 地址里的 deadline/expires 读出真实过期时间；读不到时保守按 2 小时算。
func streamExpiry(rawURL string, now time.Time) time.Time {
	fallback := now.Add(2 * time.Hour)
	u, err := url.Parse(rawURL)
	if err != nil {
		return fallback
	}
	q := u.Query()
	for _, key := range []string{"deadline", "expires"} {
		v := q.Get(key)
		if v == "" {
			continue
		}
		sec, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			continue
		}
		t := time.Unix(sec, 0)
		if t.After(now.Add(time.Minute)) && t.Before(now.Add(12*time.Hour)) {
			return t
		}
	}
	return fallback
}

// pickAudio 选择音轨：
//   - 自动：有 FLAC 就取 FLAC，否则取带宽最大的 AAC-LC；
//   - AAC 优先：忽略 FLAC，取带宽最大的 AAC-LC；
//   - 无损：只接受 FLAC，没有则明确报错，避免「以为听的是无损」。
func pickAudio(dash dashData, quality Quality) (AudioTrack, error) {
	flac := flacTrack(dash)
	if quality == QualityAuto && flac != nil {
		return *flac, nil
	}
	if quality == QualityLossless {
		if flac == nil {
			return AudioTrack{}, fmt.Errorf("bilibili: 该视频没有可用的无损音轨")
		}
		return *flac, nil
	}

	candidates := dash.Audio
	if len(candidates) == 0 {
		if flac != nil {
			return *flac, nil
		}
		return AudioTrack{}, fmt.Errorf("bilibili: 没有可用音轨")
	}

	// 第一轮只要 AAC-LC（mp4a.40.2），它是最稳的可解码编码。
	var best *dashAudio
	for i := range candidates {
		a := candidates[i]
		if a.Codecs != "mp4a.40.2" || a.BaseURL == "" {
			continue
		}
		if best == nil || a.Bandwidth > best.Bandwidth {
			best = &candidates[i]
		}
	}
	if best == nil {
		for i := range candidates {
			a := candidates[i]
			if a.BaseURL == "" {
				continue
			}
			if best == nil || a.Bandwidth > best.Bandwidth {
				best = &candidates[i]
			}
		}
	}
	if best == nil {
		return AudioTrack{}, fmt.Errorf("bilibili: 所有音轨都没有播放地址")
	}
	return AudioTrack{
		ID:        best.ID,
		URL:       best.BaseURL,
		BackupURL: best.BackupURL,
		Bandwidth: best.Bandwidth,
		Codecs:    best.Codecs,
		MimeType:  best.MimeType,
		Quality:   labelForTrack(best.ID, best.Codecs),
	}, nil
}

func flacTrack(dash dashData) *AudioTrack {
	if dash.Flac == nil || dash.Flac.Audio == nil || dash.Flac.Audio.BaseURL == "" {
		return nil
	}
	a := dash.Flac.Audio
	return &AudioTrack{
		ID: a.ID, URL: a.BaseURL, BackupURL: a.BackupURL, Bandwidth: a.Bandwidth,
		Codecs: a.Codecs, MimeType: a.MimeType, Quality: "FLAC",
	}
}

func labelForTrack(id int, codecs string) string {
	switch id {
	case 30280:
		return "192k"
	case 30232:
		return "132k"
	case 30216:
		if codecs == "mp4a.40.5" {
			return "64k HE-AAC"
		}
		return "64k"
	default:
		return codecs
	}
}
