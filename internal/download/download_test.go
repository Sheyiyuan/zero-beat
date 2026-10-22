package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sheyiyuan/zero-beat/internal/source"
)

func TestExt(t *testing.T) {
	cases := []struct {
		codec string
		want  string
	}{
		{"mp4a.40.2", ".m4a"},
		{"flac", ".flac"},
		{"mp3", ".mp3"},
		{"", ".m4a"},
	}
	for _, c := range cases {
		if got := Ext(c.codec); got != c.want {
			t.Fatalf("Ext(%q) = %q，期望 %q", c.codec, got, c.want)
		}
	}
}

func TestFilename(t *testing.T) {
	track := source.Track{Source: source.Bilibili, ExternalID: "BV1", Title: "A/B", Artist: "UP"}
	if got := Filename(track); got != "UP - A_B" {
		t.Fatalf("Filename = %q", got)
	}
	if got := Filename(source.Track{Source: source.NetEase, ExternalID: "1"}); got != "netease-1" {
		t.Fatalf("空标题回落文件名 = %q", got)
	}
	if got := Filename(source.Track{Title: strings.Repeat("歌", 200)}); len([]rune(got)) != 120 {
		t.Fatalf("超长文件名应截到 120 字符，实际 %d", len([]rune(got)))
	}
}

func TestPartPath(t *testing.T) {
	track := source.Track{Source: source.NetEase, ExternalID: "2701880637"}
	got := PartPath("/music/song.flac", track)
	want := "/music/song.flac.netease-2701880637.part"
	if got != want {
		t.Fatalf("PartPath = %q，期望 %q", got, want)
	}
}

func TestRunExists(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(out, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), source.Track{}, source.Stream{URL: "http://example.invalid"}, out, false, nil)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("已存在时应返回 ErrExists，实际 %v", err)
	}
}

func TestDownloadToPartWritesContentAndHeaders(t *testing.T) {
	data := []byte("fake audio bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://example.com/" {
			http.Error(w, "missing referer", http.StatusForbidden)
			return
		}
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "x.m4a.part")
	err := downloadToPart(context.Background(), path, source.Stream{
		URL:     srv.URL,
		Headers: map[string]string{"Referer": "https://example.com/"},
	}, nil)
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("写入内容不符: %q", got)
	}
}

func TestDownloadToPartResumesWithRange(t *testing.T) {
	data := []byte("0123456789abcdef")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := 0
		if rh := r.Header.Get("Range"); rh != "" {
			_, _ = fmt.Sscanf(rh, "bytes=%d-", &start)
			if start >= len(data) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(data[start:])
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "resume.part")
	if err := os.WriteFile(path, data[:5], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := downloadToPart(context.Background(), path, source.Stream{URL: srv.URL}, nil); err != nil {
		t.Fatalf("续传失败: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("续传后内容不符: %q", got)
	}
}

func TestDownloadToPartRestartsWhenRangeIgnored(t *testing.T) {
	data := []byte("full content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "restart.part")
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := downloadToPart(context.Background(), path, source.Stream{URL: srv.URL}, nil); err != nil {
		t.Fatalf("重启下载失败: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("忽略 Range 时不应保留旧内容: %q", got)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:               "0 B",
		1023:            "1023 B",
		1024:            "1.0 KiB",
		5 * 1024 * 1024: "5.0 MiB",
	}
	for in, want := range cases {
		if got := HumanBytes(in); got != want {
			t.Fatalf("HumanBytes(%d) = %q，期望 %q", in, got, want)
		}
	}
}
