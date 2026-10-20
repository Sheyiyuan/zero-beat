package bili

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// FavoriteFolders 列出当前账号创建的收藏夹。
func (c *Client) FavoriteFolders(ctx context.Context) ([]Folder, error) {
	user := c.User()
	if !user.LoggedIn {
		nav, err := c.Nav(ctx)
		if err != nil {
			return nil, err
		}
		if !nav.LoggedIn {
			return nil, fmt.Errorf("bilibili: 需要登录后才能读取收藏夹")
		}
		user = nav
	}
	params := url.Values{"up_mid": {strconv.FormatInt(user.Mid, 10)}, "web_location": {"333.1387"}}
	var data struct {
		List []struct {
			ID         int64  `json:"id"`
			Title      string `json:"title"`
			MediaCount int    `json:"media_count"`
		} `json:"list"`
	}
	if err := c.getJSON(ctx, withQuery(apiHost+"/x/v3/fav/folder/created/list-all", params), "https://www.bilibili.com", &data); err != nil {
		return nil, err
	}
	out := make([]Folder, 0, len(data.List))
	for _, f := range data.List {
		out = append(out, Folder{MediaID: f.ID, Title: f.Title, Count: f.MediaCount})
	}
	return out, nil
}

// FavoritePage 读取收藏夹的一页内容。
func (c *Client) FavoritePage(ctx context.Context, mediaID int64, page int) ([]FavItem, bool, error) {
	if page <= 0 {
		page = 1
	}
	params := url.Values{
		"media_id": {strconv.FormatInt(mediaID, 10)},
		"pn":       {strconv.Itoa(page)},
		"ps":       {"20"},
		"platform": {"web"},
		"order":    {"mtime"},
	}
	var data struct {
		HasMore bool `json:"has_more"`
		Medias  []struct {
			BVID     string `json:"bvid"`
			Title    string `json:"title"`
			Cover    string `json:"cover"`
			Duration int    `json:"duration"`
			Upper    struct {
				Name string `json:"name"`
			} `json:"upper"`
		} `json:"medias"`
	}
	if err := c.getJSON(ctx, withQuery(apiHost+"/x/v3/fav/resource/list", params), "https://www.bilibili.com", &data); err != nil {
		return nil, false, err
	}
	items := make([]FavItem, 0, len(data.Medias))
	for _, m := range data.Medias {
		if m.BVID == "" {
			continue // 失效视频
		}
		items = append(items, FavItem{
			BVID:     m.BVID,
			Title:    m.Title,
			Cover:    normalizeURL(m.Cover),
			Artist:   m.Upper.Name,
			Duration: m.Duration,
		})
	}
	return items, data.HasMore, nil
}

// AllFavorites 翻页读取整个收藏夹（最多 limit 条，limit<=0 表示不限）。
func (c *Client) AllFavorites(ctx context.Context, mediaID int64, limit int) ([]FavItem, error) {
	var out []FavItem
	for page := 1; page <= 50; page++ {
		items, more, err := c.FavoritePage(ctx, mediaID, page)
		if err != nil {
			return out, err
		}
		out = append(out, items...)
		if !more || len(items) == 0 {
			break
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
