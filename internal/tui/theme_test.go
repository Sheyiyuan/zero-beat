package tui

import (
	"image/color"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

func TestParseTheme(t *testing.T) {
	cases := map[string]themeMode{
		"light": themeLight,
		"LIGHT": themeLight,
		"dark":  themeDark,
		"auto":  themeAuto,
		"":      themeAuto,
		"weird": themeAuto,
	}
	for in, want := range cases {
		if got := parseTheme(in); got != want {
			t.Fatalf("parseTheme(%q) = %v，期望 %v", in, got, want)
		}
	}
}

func TestDetectLightBackground(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"15;0", false},
		{"0;15", true},
		{"0;7", true},
		{"15;8", false},
		{"0;9", true},
		{"0;default", false},
		{"garbage", false},
	}
	for _, c := range cases {
		t.Setenv("COLORFGBG", c.value)
		if got := detectLightBackground(); got != c.want {
			t.Fatalf("COLORFGBG=%q: got %v，期望 %v", c.value, got, c.want)
		}
	}
}

func TestThemeModePalette(t *testing.T) {
	t.Setenv("COLORFGBG", "0;15")
	if got := themeAuto.palette(); got.text != lightPalette.text {
		t.Fatal("COLORFGBG 浅色背景时 auto 应选浅色")
	}
	t.Setenv("COLORFGBG", "15;0")
	if got := themeAuto.palette(); got.text != darkPalette.text {
		t.Fatal("COLORFGBG 深色背景时 auto 应选深色")
	}
	if themeLight.palette().text != lightPalette.text {
		t.Fatal("light 应选浅色")
	}
	if themeDark.palette().text != darkPalette.text {
		t.Fatal("dark 应选深色")
	}
}

func TestResolveThemeMode(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "theme.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	t.Setenv("ZERO_THEME", "")
	if got := resolveThemeMode(st); got != themeAuto {
		t.Fatalf("默认应为 auto，实际 %v", got)
	}
	if err := st.SetSetting("theme", "dark"); err != nil {
		t.Fatal(err)
	}
	if got := resolveThemeMode(st); got != themeDark {
		t.Fatalf("数据库设置 dark 未生效: %v", got)
	}
	t.Setenv("ZERO_THEME", "light")
	if got := resolveThemeMode(st); got != themeLight {
		t.Fatalf("ZERO_THEME 应覆盖数据库设置: %v", got)
	}
}

func TestLightPaletteContrast(t *testing.T) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	roles := map[string]color.Color{
		"text":   lightPalette.text,
		"muted":  lightPalette.muted,
		"dim":    lightPalette.dim,
		"accent": lightPalette.accent,
		"ok":     lightPalette.ok,
		"err":    lightPalette.err,
		"warn":   lightPalette.warn,
	}
	for name, c := range roles {
		if got := contrastRatio(c, white); got < 4.5 {
			t.Fatalf("浅色主题 %s 在白底上的对比度只有 %.2f，低于 4.5", name, got)
		}
	}
	if got := contrastRatio(lightPalette.selFg, lightPalette.selBg); got < 4.5 {
		t.Fatalf("浅色主题选中行对比度只有 %.2f", got)
	}
	if got := contrastRatio(lightPalette.playFg, lightPalette.playBg); got < 4.5 {
		t.Fatalf("浅色主题播放行对比度只有 %.2f", got)
	}
}

func contrastRatio(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	channel := func(v uint32) float64 {
		x := float64(v) / 65535
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

func TestLightThemeRender(t *testing.T) {
	old := curPalette
	applyTheme(lightPalette)
	t.Cleanup(func() { applyTheme(old) })

	m := fixtureModel()
	lines := assertFrame(t, m.View().Content, 100, 30)
	frame := strings.Join(lines, "\n")
	if !strings.Contains(frame, "\x1b[") {
		t.Skip("当前终端未启用颜色，跳过颜色断言")
	}
	if !strings.Contains(frame, "38;2;36;41;47") {
		t.Fatal("浅色主题正文应使用 #24292f")
	}
}
