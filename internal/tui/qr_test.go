package tui

import (
	"strings"
	"testing"
)

func TestRenderQRHalfBlocks(t *testing.T) {
	art := renderQR("https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=8f3c1d2e4b5a69788796a5b4c3d2e1f0")
	lines := strings.Split(strings.TrimRight(art, "\n"), "\n")
	if len(lines) == 0 || len(lines) > 26 {
		t.Fatalf("B 站二维码行数应不超过 26，实际 %d", len(lines))
	}
	for _, line := range lines {
		if len([]rune(line)) > 60 {
			t.Fatalf("二维码宽度应不超过 60 列，实际 %d", len([]rune(line)))
		}
		for _, r := range line {
			if !strings.ContainsRune(" █▀▄", r) {
				t.Fatalf("二维码应只包含半块字符，发现 %q", r)
			}
		}
	}
}

func TestRenderQREmpty(t *testing.T) {
	if renderQR("  ") != "" {
		t.Fatal("空内容不应生成二维码")
	}
}

func TestQRHalfBlockOrientation(t *testing.T) {
	cases := []struct {
		top, bottom bool
		want        rune
	}{
		{true, true, ' '},
		{true, false, '▄'},
		{false, true, '▀'},
		{false, false, '█'},
	}
	for _, c := range cases {
		if got := qrHalfBlock(c.top, c.bottom); got != c.want {
			t.Fatalf("qrHalfBlock(%v,%v)=%q，期望 %q", c.top, c.bottom, got, c.want)
		}
	}
}
