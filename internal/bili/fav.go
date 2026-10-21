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

// AddFavorite 把一个视频加入收藏夹。收藏夹接口用 aid 而不是 BV 号，
// 所以先查 VideoInfo 拿到 aid；csrf 必须带登录 cookie 里的 bili_jct。
func (c *Client) AddFavorite(ctx context.Context, mediaID int64, bvid string) error {
	if mediaID <= 0 {
		return fmt.Errorf("bilibili: 收藏夹 ID 无效")
	}
	cookies := c.Cookies()
	csrf := cookies["bili_jct"]
	if csrf == "" {
		return fmt.Errorf("bilibili: 登录态缺少 bili_jct，请重新登录")
	}
	info, err := c.VideoInfo(ctx, bvid)
	if err != nil {
		return err
	}
	if info.Aid <= 0 {
		return fmt.Errorf("bilibili: 视频 %s 没有 aid，无法收藏", bvid)
	}
	form := favoriteDealForm(info.Aid, mediaID, csrf)
	err = c.postFavoriteDeal(ctx, form)
	if err == nil {
		return nil
	}
	var apiErr *APIError
	if asAPIError(err, &apiErr) {
		switch apiErr.Code {
		case 11201:
			return nil // 已经收藏过，按成功处理
		case 401, 403, 412:
			return fmt.Errorf("bilibili: 收藏夹写入被拒（%d）：登录态失效或触发风控，请重新登录/稍后再试", apiErr.Code)
		case -101:
			return fmt.Errorf("bilibili: 登录态已失效，请重新登录后再收藏")
		case -111:
			return fmt.Errorf("bilibili: CSRF 校验失败（bili_jct 已失效），请重新登录")
		}
	}
	return err
}

// postFavoriteDeal 先走双端 medialist 接口，失败再回退旧 web 接口。
func (c *Client) postFavoriteDeal(ctx context.Context, form url.Values) error {
	primary := apiHost + "/medialist/gateway/coll/resource/deal"
	if err := c.postForm(ctx, primary, "https://www.bilibili.com", form, nil); err == nil {
		return nil
	} else if apiErr, ok := err.(*APIError); ok && apiErr.Code == 11201 {
		return err
	}
	return c.postForm(ctx, apiHost+"/x/v3/fav/resource/deal", "https://www.bilibili.com", form, nil)
}

// favoriteDealForm 抽出请求字段，便于单测；收藏视频的 type 固定为 2。
func favoriteDealForm(aid, mediaID int64, csrf string) url.Values {
	return url.Values{
		"rid":           {strconv.FormatInt(aid, 10)},
		"type":          {"2"},
		"add_media_ids": {strconv.FormatInt(mediaID, 10)},
		"del_media_ids": {""},
		"csrf":          {csrf},
		"platform":      {"web"},
	}
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
