package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	apiHost      = "https://api.bilibili.com"
	passportHost = "https://passport.bilibili.com"
	defaultUA    = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

// APIError 是 B 站接口返回的业务错误。
type APIError struct {
	Code    int
	Message string
	Path    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bilibili: %s 返回 code=%d %s", e.Path, e.Code, e.Message)
}

// IsNotLoggedIn 判断错误是否为未登录。
func IsNotLoggedIn(err error) bool {
	var ae *APIError
	if ok := asAPIError(err, &ae); ok {
		return ae.Code == -101 || ae.Code == -400
	}
	return false
}

func asAPIError(err error, target **APIError) bool {
	for err != nil {
		if ae, ok := err.(*APIError); ok {
			*target = ae
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// Client 是 B 站 API 客户端。
type Client struct {
	http *http.Client

	mu      sync.RWMutex
	cookies map[string]string
	user    User

	wbiMu   sync.Mutex
	wbiKey  string
	wbiTime time.Time
}

// New 创建客户端。
func New() *Client {
	return &Client{
		http:    &http.Client{Timeout: 20 * time.Second},
		cookies: map[string]string{},
	}
}

// SetCookies 覆盖全部 cookie。
func (c *Client) SetCookies(cookies map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cookies = make(map[string]string, len(cookies))
	for k, v := range cookies {
		c.cookies[k] = v
	}
	c.wbiMu.Lock()
	c.wbiKey = ""
	c.wbiMu.Unlock()
}

// Cookies 返回当前 cookie 的副本。
func (c *Client) Cookies() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]string, len(c.cookies))
	for k, v := range c.cookies {
		out[k] = v
	}
	return out
}

// LoggedIn 判断是否持有登录态。
func (c *Client) LoggedIn() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cookies["SESSDATA"] != ""
}

// User 返回缓存的用户信息。
func (c *Client) User() User {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.user
}

func (c *Client) setUser(u User) {
	c.mu.Lock()
	c.user = u
	c.mu.Unlock()
}

func (c *Client) cookieHeader() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.cookies) == 0 {
		return ""
	}
	keys := make([]string, 0, len(c.cookies))
	for k := range c.cookies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+c.cookies[k])
	}
	return strings.Join(parts, "; ")
}

func (c *Client) captureCookies(resp *http.Response) {
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	changed := false
	for _, ck := range cookies {
		if ck.Name == "" || ck.Value == "" {
			continue
		}
		if c.cookies[ck.Name] != ck.Value {
			c.cookies[ck.Name] = ck.Value
			changed = true
		}
	}
	if changed {
		c.wbiMu.Lock()
		c.wbiKey = ""
		c.wbiMu.Unlock()
	}
}

func (c *Client) newRequest(ctx context.Context, rawURL string, referer string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("bilibili: 构造请求: %w", err)
	}
	req.Header.Set("User-Agent", defaultUA)
	req.Header.Set("Referer", referer)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if cookie := c.cookieHeader(); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	return req, nil
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// rawEnvelope 取回接口原始信封，业务错误码由调用方判断。
func (c *Client) rawEnvelope(ctx context.Context, rawURL, referer string) (envelope, error) {
	req, err := c.newRequest(ctx, rawURL, referer)
	if err != nil {
		return envelope{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return envelope{}, fmt.Errorf("bilibili: 请求失败: %w", err)
	}
	defer resp.Body.Close()
	c.captureCookies(resp)

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return envelope{}, fmt.Errorf("bilibili: 读取响应: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return envelope{}, &APIError{Code: resp.StatusCode, Message: http.StatusText(resp.StatusCode), Path: rawURL}
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return envelope{}, fmt.Errorf("bilibili: 解析响应: %w", err)
	}
	return env, nil
}

func (c *Client) getJSON(ctx context.Context, rawURL, referer string, out any) error {
	env, err := c.rawEnvelope(ctx, rawURL, referer)
	if err != nil {
		return err
	}
	if env.Code != 0 {
		return &APIError{Code: env.Code, Message: env.Message, Path: rawURL}
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("bilibili: 解析 data: %w", err)
	}
	return nil
}

func withQuery(base string, params url.Values) string {
	if len(params) == 0 {
		return base
	}
	return base + "?" + params.Encode()
}

func videoReferer(bvid string) string {
	if bvid == "" {
		return "https://www.bilibili.com"
	}
	return "https://www.bilibili.com/video/" + bvid
}
