package bili

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// mixinKeyEncTab 是 B 站 WBI 签名的固定置换表。
var mixinKeyEncTab = []int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49,
	33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13, 37, 48, 7, 16, 24, 55, 40,
	61, 26, 17, 0, 1, 60, 51, 30, 4, 22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11,
	36, 20, 34, 44, 52,
}

// mixinKey 由 wbi_img 的两个文件名拼接置换得到。
func mixinKey(imgURL, subURL string) string {
	imgKey := strings.TrimSuffix(path.Base(imgURL), path.Ext(imgURL))
	subKey := strings.TrimSuffix(path.Base(subURL), path.Ext(subURL))
	raw := imgKey + subKey
	if len(raw) < len(mixinKeyEncTab) {
		return ""
	}
	var b strings.Builder
	for _, i := range mixinKeyEncTab {
		if i >= 0 && i < len(raw) {
			b.WriteByte(raw[i])
		}
	}
	key := b.String()
	if len(key) > 32 {
		key = key[:32]
	}
	return key
}

// signParams 计算 w_rid 并写回参数（wts 由调用方或此处补齐）。
func signParams(params url.Values, key string, now time.Time) url.Values {
	if params == nil {
		params = url.Values{}
	}
	params.Set("wts", strconv.FormatInt(now.Unix(), 10))

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var query strings.Builder
	for _, k := range keys {
		v := filterValue(params.Get(k))
		if query.Len() > 0 {
			query.WriteString("&")
		}
		query.WriteString(url.QueryEscape(k))
		query.WriteString("=")
		query.WriteString(url.QueryEscape(v))
	}
	sum := md5.Sum([]byte(query.String() + key))
	params.Set("w_rid", hex.EncodeToString(sum[:]))
	return params
}

// filterValue 去掉 B 站签名要求过滤的字符。
func filterValue(v string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '!', '\'', '(', ')', '*':
			return -1
		}
		return r
	}, v)
}

// wbiImg 是 nav 接口里的签名素材。
type wbiImg struct {
	ImgURL string `json:"img_url"`
	SubURL string `json:"sub_url"`
}

// signedQuery 取（或刷新）mixin key 后返回已签名参数。
func (c *Client) signedQuery(params url.Values) (url.Values, error) {
	key, err := c.currentMixinKey()
	if err != nil {
		return nil, err
	}
	return signParams(params, key, time.Now()), nil
}

func (c *Client) currentMixinKey() (string, error) {
	c.wbiMu.Lock()
	defer c.wbiMu.Unlock()
	if c.wbiKey != "" && time.Since(c.wbiTime) < 6*time.Hour {
		return c.wbiKey, nil
	}
	env, err := c.rawEnvelope(context.Background(), apiHost+"/x/web-interface/nav", "https://www.bilibili.com")
	if err != nil {
		return "", fmt.Errorf("bilibili: 获取 WBI 签名素材: %w", err)
	}
	// 未登录时该接口返回 -101，但 data 里仍然带 wbi_img，必须照常解析。
	var nav struct {
		WbiImg wbiImg `json:"wbi_img"`
	}
	if len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, &nav); err != nil {
			return "", fmt.Errorf("bilibili: 解析 WBI 素材: %w", err)
		}
	}
	key := mixinKey(nav.WbiImg.ImgURL, nav.WbiImg.SubURL)
	if key == "" {
		return "", fmt.Errorf("bilibili: WBI 签名素材无效")
	}
	c.wbiKey = key
	c.wbiTime = time.Now()
	return key, nil
}
