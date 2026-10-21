// Package lrclib 从 LRCLIB 拉取带时间轴的歌词，作为 B 站字幕缺失时的兜底。
//
// LRCLIB 是开放的歌词库（https://lrclib.net），接口不需要登录。这里只做两件事：
// 把视频标题清洗成歌名、在候选结果里挑与视频时长最接近的一条。
package lrclib

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultBaseURL = "https://lrclib.net"

// Track 是待查询的曲目信息。Artist 可能是 B 站 UP 主而不是歌手，只作为加分项。
type Track struct {
	Title    string
	Artist   string
	Duration time.Duration
}

// Result 是一条歌词候选。
type Result struct {
	TrackName    string
	ArtistName   string
	Duration     time.Duration
	SyncedLyrics string
	PlainLyrics  string
	Instrumental bool
}

// Client 是 LRCLIB 客户端。
type Client struct {
	BaseURL   string
	HTTP      *http.Client
	UserAgent string
}

// New 创建使用默认地址的客户端。
func New() *Client {
	return &Client{
		BaseURL:   defaultBaseURL,
		HTTP:      &http.Client{Timeout: 10 * time.Second},
		UserAgent: "zero-beat/0.2 (https://github.com/Sheyiyuan/zero-beat)",
	}
}

// NewWithBaseURL 用于测试或自建实例。
func NewWithBaseURL(base string) *Client {
	c := New()
	c.BaseURL = strings.TrimRight(base, "/")
	return c
}

type apiItem struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	SyncedLyrics string  `json:"syncedLyrics"`
	PlainLyrics  string  `json:"plainLyrics"`
	Instrumental bool    `json:"instrumental"`
}

// Lookup 查询歌词。第二个返回值表示是否找到带时间轴的歌词。
func (c *Client) Lookup(ctx context.Context, track Track) (Result, bool, error) {
	title := CleanTitle(track.Title)
	if len([]rune(title)) < 2 {
		// 整段标题都在括号里时，清洗会把它删空；退回原标题再试一次。
		title = strings.TrimSpace(track.Title)
	}
	if title == "" {
		return Result{}, false, nil
	}
	if c.BaseURL == "" {
		c.BaseURL = defaultBaseURL
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 10 * time.Second}
	}

	items, err := c.search(ctx, title, track.Artist)
	if err != nil {
		return Result{}, false, err
	}
	if len(items) == 0 && track.Artist != "" {
		// B 站 UP 主经常不是歌手，带 artist_name 查不到时退一步只按歌名查。
		items, err = c.search(ctx, title, "")
		if err != nil {
			return Result{}, false, err
		}
	}
	best, ok := choose(items, title, track)
	if !ok {
		return Result{}, false, nil
	}
	return best, true, nil
}

func (c *Client) search(ctx context.Context, title, artist string) ([]apiItem, error) {
	q := url.Values{}
	q.Set("track_name", title)
	if artist != "" {
		q.Set("artist_name", artist)
	}
	endpoint := c.BaseURL + "/api/search?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("lrclib: 构造请求: %w", err)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lrclib: 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lrclib: 接口返回 %s", resp.Status)
	}
	var items []apiItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("lrclib: 解析响应: %w", err)
	}
	return items, nil
}

// choose 在候选里挑一条：必须有 syncedLyrics，时长越接近越好，歌名/歌手匹配加分。
func choose(items []apiItem, title string, track Track) (Result, bool) {
	wantTitle := normalize(title)
	wantArtist := normalize(track.Artist)
	bestScore := -1
	var best Result
	for _, it := range items {
		if it.Instrumental || strings.TrimSpace(it.SyncedLyrics) == "" {
			continue
		}
		score := 0
		gotTitle := normalize(it.TrackName)
		switch {
		case gotTitle == wantTitle:
			score += 30
		case strings.Contains(gotTitle, wantTitle) || strings.Contains(wantTitle, gotTitle):
			score += 15
		}
		if wantArtist != "" && normalize(it.ArtistName) == wantArtist {
			score += 20
		}
		if track.Duration > 0 && it.Duration > 0 {
			diff := time.Duration(it.Duration*float64(time.Second)) - track.Duration
			if diff < 0 {
				diff = -diff
			}
			switch {
			case diff <= 2*time.Second:
				score += 100
			case diff <= 5*time.Second:
				score += 50
			case diff <= 15*time.Second:
				score += 10
			}
		}
		if score > bestScore {
			bestScore = score
			best = Result{
				TrackName:    it.TrackName,
				ArtistName:   it.ArtistName,
				Duration:     time.Duration(it.Duration * float64(time.Second)),
				SyncedLyrics: it.SyncedLyrics,
				PlainLyrics:  it.PlainLyrics,
				Instrumental: it.Instrumental,
			}
		}
	}
	return best, bestScore >= 0
}

var (
	bracketTag = regexp.MustCompile(`[【\[（(][^】\]）)]*[】\]）)]`)
	suffixTag  = regexp.MustCompile(`(?i)\s*[-–—]?\s*(官方\s*MV|高清\s*MV|MV|M/V|4K|8K|1080P|720P|完整版|现场版|live\s*版|歌词版|动态歌词|字幕版|中文字幕|纯享|附歌词)\s*$`)
)

// CleanTitle 把视频标题清洗成更适合歌词库检索的歌名。
func CleanTitle(title string) string {
	s := strings.TrimSpace(title)
	if s == "" {
		return ""
	}
	s = bracketTag.ReplaceAllString(s, " ")
	// 只去尾部噪声词，避免把「Live Forever」这类正常歌名削掉。
	for suffixTag.MatchString(s) {
		s = suffixTag.ReplaceAllString(s, "")
	}
	s = strings.ReplaceAll(s, "｜", " ")
	s = strings.ReplaceAll(s, "|", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.Trim(s, " -—–·.,，。!！?？\"'\u201c\u201d\u2018\u2019「」『』《》")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '-' || r == '_' || r == '·' || r == '—' || r == '–' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
