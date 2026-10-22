package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sheyiyuan/zero-beat/internal/bili"
	"github.com/sheyiyuan/zero-beat/internal/netease"
	"github.com/sheyiyuan/zero-beat/internal/source"
	bilibilisource "github.com/sheyiyuan/zero-beat/internal/source/bilibili"
)

func TestParseDownloadArgs(t *testing.T) {
	o, err := parseDownloadArgs([]string{"BV1GJ411x7h7", "186016", "-o", "dl/", "-q", "high", "-p", "2", "-j", "3", "-f"})
	if err != nil {
		t.Fatalf("解析参数失败: %v", err)
	}
	if len(o.targets) != 2 || o.targets[0] != "BV1GJ411x7h7" || o.targets[1] != "186016" {
		t.Fatalf("目标解析不符: %+v", o.targets)
	}
	if o.out != "dl/" || o.quality != "high" || o.page != 2 || o.jobs != 3 || !o.force {
		t.Fatalf("参数不符: %+v", o)
	}

	o, err = parseDownloadArgs([]string{"-q=lossless", "-j=4", "186016"})
	if err != nil || o.quality != "lossless" || o.jobs != 4 || o.targets[0] != "186016" {
		t.Fatalf("等号参数解析失败: %+v %v", o, err)
	}
	if _, err := parseDownloadArgs(nil); err == nil {
		t.Fatal("缺少目标应报错")
	}
	if _, err := parseDownloadArgs([]string{"-x"}); err == nil {
		t.Fatal("未知参数应报错")
	}
	if _, err := parseDownloadArgs([]string{"-j", "0", "186016"}); err == nil {
		t.Fatal("-j 0 应报错")
	}
	if _, err := parseDownloadArgs([]string{"-j", "9", "186016"}); err == nil {
		t.Fatal("-j 9 应报错")
	}
}

func TestParseDownloadTarget(t *testing.T) {
	reg := source.NewRegistry(bilibilisource.New(bili.New()), netease.New())

	ref, ok, err := reg.ParseTarget("BV1GJ411x7h7", 0)
	if err != nil || !ok || ref.Source != source.Bilibili || ref.ExternalID != "BV1GJ411x7h7" || ref.Page != 1 {
		t.Fatalf("BV 号解析失败: %+v %v ok=%v", ref, err, ok)
	}

	ref, ok, err = reg.ParseTarget("https://www.bilibili.com/video/BV1GJ411x7h7?p=2", 0)
	if err != nil || !ok || ref.Page != 2 {
		t.Fatalf("B 站链接分 P 解析失败: %+v %v ok=%v", ref, err, ok)
	}

	ref, ok, err = reg.ParseTarget("186016", 0)
	if err != nil || !ok || ref.Source != source.NetEase || ref.ExternalID != "186016" {
		t.Fatalf("网易云 ID 解析失败: %+v %v ok=%v", ref, err, ok)
	}

	ref, ok, err = reg.ParseTarget("https://music.163.com/song?id=186016", 0)
	if err != nil || !ok || ref.Source != source.NetEase || ref.ExternalID != "186016" {
		t.Fatalf("网易云链接解析失败: %+v %v ok=%v", ref, err, ok)
	}

	ref, ok, err = reg.ParseTarget("BV1GJ411x7h7", 3)
	if err != nil || !ok || ref.Page != 3 {
		t.Fatalf("--page 覆盖失败: %+v %v ok=%v", ref, err, ok)
	}

	if _, ok, _ := reg.ParseTarget("not-a-target", 0); ok {
		t.Fatal("无法识别的目标不应被匹配")
	}
}

func TestParseDownloadQuality(t *testing.T) {
	cases := map[string]source.Quality{
		"":         source.QualityAuto,
		"auto":     source.QualityAuto,
		"HIGH":     source.QualityHigh,
		"lossless": source.QualityLossless,
	}
	for in, want := range cases {
		got, err := parseDownloadQuality(in)
		if err != nil || got != want {
			t.Fatalf("parseDownloadQuality(%q) = %v, %v；期望 %v", in, got, err, want)
		}
	}
	if _, err := parseDownloadQuality("ultra"); err == nil {
		t.Fatal("未知音质应报错")
	}
}

func TestResolveDownloadOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	track := source.Track{Source: source.Bilibili, ExternalID: "BV1", Title: "A", Artist: "B"}

	got, err := resolveDownloadOutput("", track, ".m4a")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "Music", "zero-beat", "B - A.m4a")
	if got != want {
		t.Fatalf("默认输出路径不符: %q，期望 %q", got, want)
	}

	dir := filepath.Join(t.TempDir(), "dl")
	got, err = resolveDownloadOutput(dir, track, ".m4a")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "B - A.m4a") {
		t.Fatalf("目录输出路径不符: %q", got)
	}

	file := filepath.Join(t.TempDir(), "custom.flac")
	got, err = resolveDownloadOutput(file, track, ".flac")
	if err != nil {
		t.Fatal(err)
	}
	if got != file {
		t.Fatalf("文件输出路径不符: %q", got)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("输出目录应被创建: %v", err)
	}
}

func TestDedupeDownloadPaths(t *testing.T) {
	dir := t.TempDir()
	track := source.Track{Source: source.NetEase, ExternalID: "1"}
	jobs := []*downloadJob{
		{track: track, outPath: filepath.Join(dir, "same.mp3")},
		{track: track, outPath: filepath.Join(dir, "same.mp3")},
	}
	dedupeDownloadPaths(jobs)
	if jobs[0].outPath == jobs[1].outPath {
		t.Fatalf("重名输出应被区分: %q", jobs[0].outPath)
	}
}
