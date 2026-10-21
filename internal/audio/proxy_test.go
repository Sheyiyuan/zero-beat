package audio

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHeaderProxyInjectsHeadersAndRange(t *testing.T) {
	var gotCookie, gotReferer, gotRange string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotReferer = r.Header.Get("Referer")
		gotRange = r.Header.Get("Range")
		w.Header().Set("Content-Range", "bytes 10-13/100")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "data")
	}))
	defer upstream.Close()

	proxy, err := newHeaderProxy(upstream.URL, map[string]string{
		"Cookie":  "MUSIC_U=secret-cookie",
		"Referer": "https://music.163.com/",
	})
	if err != nil {
		t.Fatalf("启动代理失败: %v", err)
	}
	defer proxy.Close()
	if strings.Contains(proxy.URL(), "secret-cookie") || strings.Contains(proxy.URL(), upstream.URL) {
		t.Fatalf("代理 URL 不应包含敏感 header 或上游地址: %s", proxy.URL())
	}

	req, _ := http.NewRequest(http.MethodGet, proxy.URL(), nil)
	req.Header.Set("Range", "bytes=10-")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求代理失败: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusPartialContent || string(body) != "data" {
		t.Fatalf("代理响应不符: status=%d body=%q", resp.StatusCode, body)
	}
	if gotCookie != "MUSIC_U=secret-cookie" || gotReferer != "https://music.163.com/" || gotRange != "bytes=10-" {
		t.Fatalf("上游 header 不符: cookie=%q referer=%q range=%q", gotCookie, gotReferer, gotRange)
	}
}

func TestHeaderProxyDoesNotLeakCookieAcrossHosts(t *testing.T) {
	var cookie, referer string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie = r.Header.Get("Cookie")
		referer = r.Header.Get("Referer")
		_, _ = io.WriteString(w, "ok")
	}))
	defer other.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer redirect.Close()

	proxy, err := newHeaderProxy(redirect.URL, map[string]string{
		"Cookie":  "MUSIC_U=secret-cookie",
		"Referer": "https://music.163.com/",
	})
	if err != nil {
		t.Fatalf("启动代理失败: %v", err)
	}
	defer proxy.Close()
	resp, err := http.Get(proxy.URL())
	if err != nil {
		t.Fatalf("请求代理失败: %v", err)
	}
	resp.Body.Close()
	if cookie != "" {
		t.Fatalf("跨域重定向不应携带 Cookie: %q", cookie)
	}
	if referer != "https://music.163.com/" {
		t.Fatalf("跨域重定向应保留 Referer: %q", referer)
	}
}
