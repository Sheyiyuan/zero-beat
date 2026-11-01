package cover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func TestFetchCachesByURL(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if got := r.Header.Get("Referer"); got != "https://example.com/" {
			t.Errorf("Referer = %q", got)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-bytes"))
	}))
	defer server.Close()

	cache := New(t.TempDir())
	first, err := cache.Fetch(context.Background(), server.URL+"/cover.jpg", map[string]string{"Referer": "https://example.com/"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if info, err := os.Stat(first); err != nil || info.Size() == 0 {
		t.Fatalf("封面没有落盘: %v", err)
	}

	second, err := cache.Fetch(context.Background(), server.URL+"/cover.jpg", nil)
	if err != nil {
		t.Fatalf("第二次 Fetch: %v", err)
	}
	if second != first {
		t.Fatalf("命中缓存应返回同一路径: %q != %q", second, first)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("命中缓存后不应重复下载，请求数 = %d", got)
	}
}

func TestFetchRejectsNonImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer server.Close()

	if _, err := New(t.TempDir()).Fetch(context.Background(), server.URL, nil); err == nil {
		t.Fatal("非图片响应应返回错误")
	}
}
