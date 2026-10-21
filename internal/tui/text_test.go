package tui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sheyiyuan/zero-beat/internal/lyrics"
)

func TestGradientTextKeepsContentAndWidth(t *testing.T) {
	base := color.RGBA{R: 20, G: 30, B: 40, A: 255}
	highlight := color.RGBA{R: 102, G: 204, B: 255, A: 255}
	for _, progress := range []float64{-1, 0, 0.2, 0.5, 1, 2} {
		for _, text := range []string{"夜航星 Night Voyager", "a", ""} {
			got := gradientText(text, progress, base, highlight)
			if ansi.Strip(got) != text {
				t.Fatalf("progress=%.1f 文本被改动: %q -> %q", progress, text, ansi.Strip(got))
			}
			if visWidth(got) != visWidth(text) {
				t.Fatalf("progress=%.1f 宽度不符: %d/%d", progress, visWidth(got), visWidth(text))
			}
		}
	}
}

func TestGradientTextColorsDependOnProgress(t *testing.T) {
	base := color.RGBA{R: 1, G: 2, B: 3, A: 255}
	highlight := color.RGBA{R: 250, G: 251, B: 252, A: 255}
	start := gradientText("abc", 0, base, highlight)
	mid := gradientText("abc", 0.5, base, highlight)
	end := gradientText("abc", 1, base, highlight)
	if !strings.Contains(start, "\x1b[") {
		t.Skip("当前终端未启用颜色，跳过颜色断言")
	}
	if !strings.Contains(start, "38;2;1;2;3") {
		t.Fatalf("进度 0 应为未高亮色：%q", start)
	}
	if !strings.Contains(end, "38;2;250;251;252") {
		t.Fatalf("进度 1 应为高亮色：%q", end)
	}
	if !strings.Contains(mid, "38;2;125;126;127") {
		t.Fatalf("进度 0.5 应为中间色：%q", mid)
	}
	if strings.Count(mid, "38;2;") != 1 {
		t.Fatalf("整行应只有一个颜色：%q", mid)
	}
	if start == end {
		t.Fatal("不同进度应产生不同颜色")
	}
}

func TestWrapText(t *testing.T) {
	text := "r 重新获取 · s 切换音源 · o 二维码图片 · W 退出当前源"
	lines := wrapText(text, 24)
	if len(lines) < 2 {
		t.Fatalf("长提示应换行，实际 %d 行: %+v", len(lines), lines)
	}
	for _, line := range lines {
		if visWidth(line) > 24 {
			t.Fatalf("换行后仍超宽: %q (%d)", line, visWidth(line))
		}
	}
	joined := strings.Join(lines, " ")
	for _, want := range []string{"重新获取", "切换音源", "二维码图片", "退出当前源"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("换行后丢了 %q: %+v", want, lines)
		}
	}

	cjk := "这是一个没有空格的超长中文句子"
	lines = wrapText(cjk, 10)
	for _, line := range lines {
		if visWidth(line) > 10 {
			t.Fatalf("中文换行超宽: %q (%d)", line, visWidth(line))
		}
	}
	if got := strings.Join(lines, ""); got != cjk {
		t.Fatalf("中文换行改动了内容: %q", got)
	}
	if wrapText("", 10) != nil {
		t.Fatal("空字符串不应产生行")
	}
}

func TestBlendColor(t *testing.T) {
	black := color.RGBA{A: 255}
	orange := color.RGBA{R: 200, G: 100, B: 50, A: 255}
	got, ok := blendColor(black, orange, 0.5).(color.RGBA)
	if !ok {
		t.Fatalf("混色应返回 color.RGBA，实际 %T", blendColor(black, orange, 0.5))
	}
	if got.R != 100 || got.G != 50 || got.B != 25 || got.A != 255 {
		t.Fatalf("中点混色不符: %+v", got)
	}
	if blendColor(black, orange, 0) != color.Color(black) {
		t.Fatal("t=0 应返回起始色")
	}
	if blendColor(black, orange, 1) != color.Color(orange) {
		t.Fatal("t=1 应返回目标色")
	}
}

func TestLyricProgress(t *testing.T) {
	lines := []lyrics.Line{
		{StartMS: 1000, Text: "a"},
		{StartMS: 3000, Text: "b"},
		{StartMS: 5000, Text: "c"},
	}
	cases := []struct {
		name       string
		idx, posMS int
		durationMS int
		want       float64
	}{
		{"未开始", -1, 0, 9000, 0},
		{"第一行开头", 0, 1000, 9000, 0},
		{"第一行中点", 0, 2000, 9000, 0.5},
		{"第一行结束", 0, 3000, 9000, 1},
		{"第二行中点", 1, 4000, 9000, 0.5},
		{"最后一行用时长", 2, 7000, 9000, 0.5},
		{"最后一行超过时长", 2, 10000, 9000, 1},
		{"时长未知", 2, 7000, 0, 1},
		{"越界", 99, 7000, 9000, 0},
	}
	for _, c := range cases {
		got := lyricProgress(lines, c.idx, c.posMS, c.durationMS)
		if got != c.want {
			t.Fatalf("%s: got %v，期望 %v", c.name, got, c.want)
		}
	}
}
