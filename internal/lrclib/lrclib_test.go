package lrclib

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCleanTitle(t *testing.T) {
	cases := map[string]string{
		"【官方 MV】Never Gonna Give You Up": "Never Gonna Give You Up",
		"夜航星 (Live)":                     "夜航星",
		"夜航星 官方MV":                       "夜航星",
		"夜航星【4K 修复版】":                    "夜航星",
		"Live Forever":                   "Live Forever",
		"  《夜航星》  ":                      "夜航星",
		"周杰伦 - 稻香 官方 MV":                 "周杰伦 - 稻香",
		"":                               "",
	}
	for in, want := range cases {
		if got := CleanTitle(in); got != want {
			t.Fatalf("CleanTitle(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestLookupChoosesSyncedClosestDuration(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"trackName": "夜航星", "artistName": "不才", "duration": 999,
				"syncedLyrics": "[00:99.00]错误版本",
			},
			{
				"trackName": "夜航星", "artistName": "不才", "duration": 287.2,
				"syncedLyrics": "[00:01.00]第一句\n[00:05.50]第二句",
			},
			{
				"trackName": "夜航星", "artistName": "不才", "duration": 287,
				"plainLyrics": "没有时间轴",
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	res, ok, err := c.Lookup(context.Background(), Track{
		Title: "夜航星【官方MV】", Artist: "不才", Duration: 287 * time.Second,
	})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if !ok {
		t.Fatal("应命中带时间轴的歌词")
	}
	if res.SyncedLyrics == "" || res.Duration != 287*time.Second+200*time.Millisecond {
		t.Fatalf("选中的结果不符: %+v", res)
	}
	if gotQuery == "" {
		t.Fatal("请求应带查询参数")
	}
}

func TestLookupEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	if _, ok, err := c.Lookup(context.Background(), Track{Title: "不存在"}); err != nil || ok {
		t.Fatalf("空结果应返回 ok=false, err=nil，实际 ok=%v err=%v", ok, err)
	}
}

func TestLookupHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	if _, _, err := c.Lookup(context.Background(), Track{Title: "夜航星"}); err == nil {
		t.Fatal("非 200 应报错")
	}
}

func TestLookupCopiesID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": 4242, "trackName": "夜航星", "artistName": "不才", "duration": 287,
			"syncedLyrics": "[00:01.00]第一句",
		}})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	res, ok, err := c.Lookup(context.Background(), Track{Title: "夜航星", Duration: 287 * time.Second})
	if err != nil || !ok {
		t.Fatalf("查询失败: ok=%v err=%v", ok, err)
	}
	if res.ID != 4242 {
		t.Fatalf("choose 应把 id 拷进 Result，实际 %d", res.ID)
	}
}

func TestSearchMapsFieldsAndOrder(t *testing.T) {
	var gotPath, gotQuery, gotAccept, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("q")
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id": 2, "trackName": "夜航星", "artistName": "不才", "duration": 287.2,
				"syncedLyrics": "[00:01.00]第一句", "plainLyrics": "第一句", "instrumental": false,
			},
			{
				"id": 11, "trackName": "夜航星 (Live)", "artistName": "不才", "duration": 300,
				"syncedLyrics": "", "plainLyrics": "纯文本", "instrumental": false,
			},
			{
				"id": 7, "trackName": "夜航星 (伴奏)", "artistName": "", "duration": 0,
				"instrumental": true,
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	got, err := c.Search(context.Background(), "  夜航星 Live & 4K=1  ")
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if gotPath != "/api/search" {
		t.Fatalf("请求路径 = %q，期望 /api/search", gotPath)
	}
	// q 必须原样（只去首尾空白）经过 URL 编码后由 Query 解回。
	if gotQuery != "夜航星 Live & 4K=1" {
		t.Fatalf("q = %q，期望 %q", gotQuery, "夜航星 Live & 4K=1")
	}
	if gotAccept != "application/json" {
		t.Fatalf("Accept = %q", gotAccept)
	}
	if gotUA != c.UserAgent || gotUA == "" {
		t.Fatalf("User-Agent = %q，期望 %q", gotUA, c.UserAgent)
	}
	if len(got) != 3 {
		t.Fatalf("结果条数 = %d，期望 3", len(got))
	}
	want := []struct {
		id       int64
		track    string
		artist   string
		duration time.Duration
		synced   string
		plain    string
		instr    bool
	}{
		{2, "夜航星", "不才", 287*time.Second + 200*time.Millisecond, "[00:01.00]第一句", "第一句", false},
		{11, "夜航星 (Live)", "不才", 300 * time.Second, "", "纯文本", false},
		{7, "夜航星 (伴奏)", "", 0, "", "", true},
	}
	for i, w := range want {
		r := got[i]
		if r.ID != w.id || r.TrackName != w.track || r.ArtistName != w.artist ||
			r.Duration != w.duration || r.SyncedLyrics != w.synced ||
			r.PlainLyrics != w.plain || r.Instrumental != w.instr {
			t.Fatalf("第 %d 条 = %+v，期望 %+v", i, r, w)
		}
	}
}

func TestSearchEmptyQueryMakesNoRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("空查询不应发请求，却收到 %s", r.URL)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	for _, q := range []string{"", "   ", "\n\t "} {
		got, err := c.Search(context.Background(), q)
		if err != nil || got != nil {
			t.Fatalf("Search(%q) = %v, %v，期望 nil, nil", q, got, err)
		}
	}
}

func TestSearchEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	got, err := c.Search(context.Background(), "不存在")
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("期望空结果，实际 %+v", got)
	}
}

func TestSearchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	if _, err := c.Search(context.Background(), "夜航星"); err == nil {
		t.Fatal("非 200 应报错")
	}
}

func TestGetByID(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 4242, "trackName": "夜航星", "artistName": "不才", "duration": 287.2,
			"syncedLyrics": "[00:01.00]第一句", "plainLyrics": "第一句", "instrumental": false,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	res, ok, err := c.Get(context.Background(), 4242)
	if err != nil || !ok {
		t.Fatalf("Get 失败: ok=%v err=%v", ok, err)
	}
	if gotPath != "/api/get/4242" {
		t.Fatalf("请求路径 = %q，期望 /api/get/4242", gotPath)
	}
	if res.ID != 4242 || res.TrackName != "夜航星" || res.ArtistName != "不才" ||
		res.Duration != 287*time.Second+200*time.Millisecond ||
		res.SyncedLyrics != "[00:01.00]第一句" || res.PlainLyrics != "第一句" {
		t.Fatalf("结果不符: %+v", res)
	}
}

func TestGetNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":404,"name":"TrackNotFound","message":"Track not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	res, ok, err := c.Get(context.Background(), 99)
	if err != nil {
		t.Fatalf("404 不应报错，实际 %v", err)
	}
	if ok || res != (Result{}) {
		t.Fatalf("404 应返回零值且 ok=false，实际 %+v ok=%v", res, ok)
	}
}

func TestGetServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	if _, _, err := c.Get(context.Background(), 1); err == nil {
		t.Fatal("500 应报错")
	}
}

func TestGetInvalidIDMakesNoRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("非法 id 不应发请求，却收到 %s", r.URL)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	for _, id := range []int64{0, -1} {
		res, ok, err := c.Get(context.Background(), id)
		if err != nil || ok || res != (Result{}) {
			t.Fatalf("Get(%d) = %+v, %v, %v，期望零值, false, nil", id, res, ok, err)
		}
	}
}
