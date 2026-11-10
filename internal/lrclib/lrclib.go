// Package lrclib 从 LRCLIB 拉取带时间轴的歌词，作为 B 站字幕缺失时的兜底。
//
// LRCLIB 是开放的歌词库（https://lrclib.net），接口不需要登录。这里做三件事：
// 把视频标题清洗成歌名、在候选结果里挑与视频时长最接近的一条，以及给「选一个
// 歌词源」的界面提供自由文本搜索和按 id 取回。
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
	// ID 是 LRCLIB 内部的记录 id，用于之后按 id 精确取回同一条歌词，
	// 也让「选一个歌词源」这类 UI 有稳定的标识可以做 key。
	ID           int64
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
	ID           int64   `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	SyncedLyrics string  `json:"syncedLyrics"`
	PlainLyrics  string  `json:"plainLyrics"`
	Instrumental bool    `json:"instrumental"`
}

// result 把接口条目转成对外结果。Search 与 choose 共用，避免以后加字段时漏拷。
func (it apiItem) result() Result {
	return Result{
		ID:           it.ID,
		TrackName:    it.TrackName,
		ArtistName:   it.ArtistName,
		Duration:     time.Duration(it.Duration * float64(time.Second)),
		SyncedLyrics: it.SyncedLyrics,
		PlainLyrics:  it.PlainLyrics,
		Instrumental: it.Instrumental,
	}
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
	c.normalize()

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

// normalize 补齐零值，调用方直接构造 Client{} 或只填 BaseURL 也能用。
// 放在需要发请求的地方调用，保持原先 Lookup 在空标题时不碰默认值的语义。
func (c *Client) normalize() {
	if c.BaseURL == "" {
		c.BaseURL = defaultBaseURL
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
}

// fetch 发一次 GET，统一带 User-Agent / Accept。状态码交给调用方判断，
// 因为 Get 需要把 404 当成「没找到」而不是错误。
func (c *Client) fetch(ctx context.Context, endpoint string) (*http.Response, error) {
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
	return resp, nil
}

// getJSON 请求 pathAndQuery 并把 200 的响应体解析到 out。
func (c *Client) getJSON(ctx context.Context, pathAndQuery string, out any) error {
	resp, err := c.fetch(ctx, c.BaseURL+pathAndQuery)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("lrclib: 接口返回 %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("lrclib: 解析响应: %w", err)
	}
	return nil
}

func (c *Client) search(ctx context.Context, title, artist string) ([]apiItem, error) {
	q := url.Values{}
	q.Set("track_name", title)
	if artist != "" {
		q.Set("artist_name", artist)
	}
	var items []apiItem
	if err := c.getJSON(ctx, "/api/search?"+q.Encode(), &items); err != nil {
		return nil, err
	}
	return items, nil
}

// Search 用自由文本查 LRCLIB，返回全部候选，顺序与接口返回一致。
// 给「选一个歌词源」的界面用：调用方拿到多条结果自己展示、再由用户决定。
func (c *Client) Search(ctx context.Context, q string) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	c.normalize()

	params := url.Values{}
	params.Set("q", q)
	var items []apiItem
	if err := c.getJSON(ctx, "/api/search?"+params.Encode(), &items); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(items))
	for _, it := range items {
		out = append(out, it.result())
	}
	return out, nil
}

// Get 按 id 精确取回一条歌词。第二个返回值为 false 表示 LRCLIB 没有这条记录
// （例如记录已被删除），此时不算错误。
func (c *Client) Get(ctx context.Context, id int64) (Result, bool, error) {
	if id <= 0 {
		return Result{}, false, nil
	}
	c.normalize()

	resp, err := c.fetch(ctx, fmt.Sprintf("%s/api/get/%d", c.BaseURL, id))
	if err != nil {
		return Result{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Result{}, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, false, fmt.Errorf("lrclib: 接口返回 %s", resp.Status)
	}
	var it apiItem
	if err := json.NewDecoder(resp.Body).Decode(&it); err != nil {
		return Result{}, false, fmt.Errorf("lrclib: 解析响应: %w", err)
	}
	return it.result(), true, nil
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
			best = it.result()
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
