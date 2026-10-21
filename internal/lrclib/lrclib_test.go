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
