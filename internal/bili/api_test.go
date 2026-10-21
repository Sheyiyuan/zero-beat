package bili

import (
	"testing"
	"time"
)

func sampleDash() dashData {
	var d dashData
	d.Duration = 213
	d.Audio = []dashAudio{
		{ID: 30216, BaseURL: "https://cdn/64.m4s", Bandwidth: 43962, Codecs: "mp4a.40.5", MimeType: "audio/mp4"},
		{ID: 30232, BaseURL: "https://cdn/132.m4s", Bandwidth: 102931, Codecs: "mp4a.40.2", MimeType: "audio/mp4"},
		{ID: 30280, BaseURL: "https://cdn/192.m4s", Bandwidth: 203786, Codecs: "mp4a.40.2", MimeType: "audio/mp4"},
	}
	return d
}

func TestPickAudioPrefersHighestAACLC(t *testing.T) {
	track, err := pickAudio(sampleDash(), QualityAuto)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if track.ID != 30280 {
		t.Fatalf("应选 192k 的 30280，实际 %d", track.ID)
	}
	if track.Quality != "192k" {
		t.Fatalf("档位标签应为 192k，实际 %q", track.Quality)
	}
}

func TestPickAudioPrefersFLACWhenAvailable(t *testing.T) {
	d := sampleDash()
	d.Flac = &struct {
		Display bool       `json:"display"`
		Audio   *dashAudio `json:"audio"`
	}{Display: true, Audio: &dashAudio{ID: 32000, BaseURL: "https://cdn/hires.flac", Bandwidth: 1400000, Codecs: "fLaC"}}
	track, err := pickAudio(d, QualityAuto)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if track.Quality != "FLAC" || track.URL != "https://cdn/hires.flac" {
		t.Fatalf("应优先 FLAC，实际 %+v", track)
	}
}

func TestPickAudioFallsBackToHEAAC(t *testing.T) {
	d := dashData{Audio: []dashAudio{
		{ID: 30216, BaseURL: "https://cdn/64.m4s", Bandwidth: 43962, Codecs: "mp4a.40.5"},
	}}
	track, err := pickAudio(d, QualityAuto)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if track.ID != 30216 || track.Quality != "64k HE-AAC" {
		t.Fatalf("应回退到 HE-AAC，实际 %+v", track)
	}
}

func TestPickAudioErrorsWithoutTracks(t *testing.T) {
	if _, err := pickAudio(dashData{}, QualityAuto); err == nil {
		t.Fatal("没有音轨时应报错")
	}
}

func TestParseBVID(t *testing.T) {
	cases := map[string]string{
		"BV1GJ411x7h7": "BV1GJ411x7h7",
		"https://www.bilibili.com/video/BV1GJ411x7h7":      "BV1GJ411x7h7",
		"https://www.bilibili.com/video/BV1GJ411x7h7/?p=2": "BV1GJ411x7h7",
		"  BV1GJ411x7h7  ": "BV1GJ411x7h7",
	}
	for input, want := range cases {
		got, err := ParseBVID(input)
		if err != nil {
			t.Fatalf("%q 解析失败: %v", input, err)
		}
		if got != want {
			t.Fatalf("%q -> %q，期望 %q", input, got, want)
		}
	}
	if _, err := ParseBVID("https://example.com/nothing"); err == nil {
		t.Fatal("非法输入应报错")
	}
}

func TestParseClock(t *testing.T) {
	cases := map[string]int{
		"3:45":    225,
		"03:45":   225,
		"1:02:03": 3723,
		"":        0,
		"bad":     0,
	}
	for in, want := range cases {
		if got := parseClock(in); got != want {
			t.Fatalf("parseClock(%q) = %d，期望 %d", in, got, want)
		}
	}
}

func TestStripEm(t *testing.T) {
	in := `<em class="keyword">周杰伦</em> 稻香 &amp; 现场`
	want := `周杰伦 稻香 & 现场`
	if got := stripEm(in); got != want {
		t.Fatalf("stripEm = %q，期望 %q", got, want)
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"//i0.hdslb.com/a.jpg":      "https://i0.hdslb.com/a.jpg",
		"http://i0.hdslb.com/a.jpg": "https://i0.hdslb.com/a.jpg",
		"https://x/y":               "https://x/y",
	}
	for in, want := range cases {
		if got := normalizeURL(in); got != want {
			t.Fatalf("normalizeURL(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func flacDash() dashData {
	d := sampleDash()
	d.Flac = &struct {
		Display bool       `json:"display"`
		Audio   *dashAudio `json:"audio"`
	}{Display: true, Audio: &dashAudio{ID: 32000, BaseURL: "https://cdn/hires.flac", Bandwidth: 1400000, Codecs: "fLaC"}}
	return d
}

func TestPickAudioQualityHighSkipsFLAC(t *testing.T) {
	track, err := pickAudio(flacDash(), QualityHigh)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if track.ID != 30280 || track.Quality != "192k" {
		t.Fatalf("AAC 优先应跳过 FLAC，实际 %+v", track)
	}
}

func TestPickAudioLosslessRequiresFLAC(t *testing.T) {
	if _, err := pickAudio(sampleDash(), QualityLossless); err == nil {
		t.Fatal("没有 FLAC 时无损模式应报错")
	}
	track, err := pickAudio(flacDash(), QualityLossless)
	if err != nil {
		t.Fatalf("有无损轨时应成功: %v", err)
	}
	if track.Quality != "FLAC" || track.URL != "https://cdn/hires.flac" {
		t.Fatalf("应选 FLAC，实际 %+v", track)
	}
}

func TestStreamExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	want := now.Add(30 * time.Minute)
	got := streamExpiry("https://cdn/x.m4s?deadline=1700001800&upsig=x", now)
	if !got.Equal(want) {
		t.Fatalf("应读取 deadline，得到 %v 期望 %v", got, want)
	}
	got = streamExpiry("https://cdn/x.m4s?expires=1700001800", now)
	if !got.Equal(want) {
		t.Fatalf("应读取 expires，得到 %v 期望 %v", got, want)
	}
	got = streamExpiry("https://cdn/x.m4s", now)
	if !got.Equal(now.Add(2 * time.Hour)) {
		t.Fatalf("无参数应回退 2 小时，得到 %v", got)
	}
	// 明显不合理的过期时间（过去或太远）应忽略。
	got = streamExpiry("https://cdn/x.m4s?deadline=1", now)
	if !got.Equal(now.Add(2 * time.Hour)) {
		t.Fatalf("过去时间应回退，得到 %v", got)
	}
}
