package tui

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/sheyiyuan/zero-beat/internal/config"
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

func TestParsePalette(t *testing.T) {
	cases := map[string]paletteID{
		"zero-red":    paletteZeroRed,
		"ZERO-RED":    paletteZeroRed,
		"zero":        paletteZeroRed,
		"red":         paletteZeroRed,
		"佐洛红":         paletteZeroRed,
		"":            paletteZeroRed,
		"weird":       paletteZeroRed,
		"hanpai-blue": paletteHanpaiBlue,
		"HANPAI":      paletteHanpaiBlue,
		"hanpai":      paletteHanpaiBlue,
		"blue":        paletteHanpaiBlue,
		"半拍蓝":         paletteHanpaiBlue,
	}
	for in, want := range cases {
		if got := parsePalette(in); got != want {
			t.Fatalf("parsePalette(%q) = %v，期望 %v", in, got, want)
		}
	}
}

func TestValidPalette(t *testing.T) {
	for _, s := range []string{
		"zero-red", "zero", "red", "  ZERO  ", "佐洛红",
		"hanpai-blue", "blue", "hanpai", "半拍蓝",
	} {
		if !ValidPalette(s) {
			t.Fatalf("ValidPalette(%q) 应为 true", s)
		}
	}
	for _, s := range []string{"", "purple", "zero blue"} {
		if ValidPalette(s) {
			t.Fatalf("ValidPalette(%q) 应为 false", s)
		}
	}
}

func TestCanonicalPalette(t *testing.T) {
	if got := CanonicalPalette("blue"); got != PaletteHanpaiBlue {
		t.Fatalf("blue 应归一化成 %s，实际 %s", PaletteHanpaiBlue, got)
	}
	if got := CanonicalPalette("weird"); got != PaletteZeroRed {
		t.Fatalf("未知值应回落到 %s，实际 %s", PaletteZeroRed, got)
	}
}

func TestPaletteLabel(t *testing.T) {
	if got := PaletteLabel("zero-red"); got != "佐洛红 zero red" {
		t.Fatalf("默认配色标签不符: %q", got)
	}
	if got := PaletteLabel("hanpai-blue"); got != "半拍蓝 hanpai blue" {
		t.Fatalf("蓝色配色标签不符: %q", got)
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

func TestPaletteColors(t *testing.T) {
	t.Setenv("COLORFGBG", "0;15")
	if got := paletteZeroRed.colors(themeAuto); got.text != zeroLight.text {
		t.Fatal("COLORFGBG 浅色背景时 zero-red auto 应选浅色")
	}
	if got := paletteHanpaiBlue.colors(themeAuto); got.text != blueLight.text {
		t.Fatal("COLORFGBG 浅色背景时 hanpai-blue auto 应选浅色")
	}

	t.Setenv("COLORFGBG", "15;0")
	if got := paletteZeroRed.colors(themeAuto); got.text != zeroDark.text {
		t.Fatal("COLORFGBG 深色背景时 zero-red auto 应选深色")
	}
	if got := paletteZeroRed.colors(themeLight); got.text != zeroLight.text {
		t.Fatal("light 应选浅色")
	}
	if got := paletteZeroRed.colors(themeDark); got.text != zeroDark.text {
		t.Fatal("dark 应选深色")
	}
	if got := paletteHanpaiBlue.colors(themeDark); got.text != blueDark.text {
		t.Fatal("hanpai-blue 的深色变体不符")
	}
	if got := paletteHanpaiBlue.colors(themeLight); got.text != blueLight.text {
		t.Fatal("hanpai-blue 的浅色变体不符")
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
	if err := st.SetSetting(ThemeModeKey, "dark"); err != nil {
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

func TestResolveTheme(t *testing.T) {
	isolateConfig(t)
	st := openThemeStore(t)
	t.Setenv("ZERO_THEME", "")
	t.Setenv("ZERO_THEME_PALETTE", "")
	t.Setenv("COLORFGBG", "15;0")

	p, err := resolveTheme(st)
	if err != nil {
		t.Fatalf("默认主题应能解析: %v", err)
	}
	if p.accent != zeroDark.accent {
		t.Fatal("默认应使用佐洛红深色")
	}

	if err := st.SetSetting(ThemePaletteKey, PaletteHanpaiBlue); err != nil {
		t.Fatal(err)
	}
	p, err = resolveTheme(st)
	if err != nil {
		t.Fatalf("半拍蓝应能解析: %v", err)
	}
	if p.accent != blueDark.accent {
		t.Fatal("设置 hanpai-blue 后应使用半拍蓝深色")
	}

	if err := st.SetSetting(ThemeModeKey, "light"); err != nil {
		t.Fatal(err)
	}
	p, err = resolveTheme(st)
	if err != nil {
		t.Fatalf("浅色模式应能解析: %v", err)
	}
	if p.accent != blueLight.accent {
		t.Fatal("浅色模式应使用半拍蓝浅色")
	}

	t.Setenv("ZERO_THEME_PALETTE", "zero")
	p, err = resolveTheme(st)
	if err != nil {
		t.Fatalf("环境变量覆盖应能解析: %v", err)
	}
	if p.accent != zeroLight.accent {
		t.Fatal("ZERO_THEME_PALETTE 应覆盖数据库设置")
	}
}

func TestResolveThemeOverrides(t *testing.T) {
	dir := isolateConfig(t)
	st := openThemeStore(t)
	t.Setenv("ZERO_THEME", "")
	t.Setenv("ZERO_THEME_PALETTE", "")

	path := filepath.Join(dir, "theme.toml")
	body := "[dark]\naccent = \"#112233\"\n[light]\naccent = \"#445566\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("COLORFGBG", "15;0")
	p, err := resolveTheme(st)
	if err != nil {
		t.Fatalf("覆盖文件应能解析: %v", err)
	}
	if got := hexColor(p.accent); got != "#112233" {
		t.Fatalf("深色 accent 覆盖未生效: %s", got)
	}
	if p.text != zeroDark.text {
		t.Fatal("没覆盖的槽位应继续跟随内置配色")
	}

	t.Setenv("COLORFGBG", "0;15")
	p, err = resolveTheme(st)
	if err != nil {
		t.Fatalf("浅色覆盖应能解析: %v", err)
	}
	if got := hexColor(p.accent); got != "#445566" {
		t.Fatalf("浅色 accent 覆盖未生效: %s", got)
	}
}

func TestInvalidThemeFile(t *testing.T) {
	dir := isolateConfig(t)
	st := openThemeStore(t)
	path := filepath.Join(dir, "theme.toml")

	cases := []struct {
		name string
		body string
		want string
	}{
		{"未知槽位", "[dark]\nacent = \"#112233\"\n", "acent"},
		{"非法颜色", "[dark]\naccent = \"blue\"\n", "accent"},
		{"未知段", "[unknown]\naccent = \"#112233\"\n", "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(c.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := resolveTheme(st)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("应报包含 %q 的错误，实际 %v", c.want, err)
			}
		})
	}
}

func TestCheckThemeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "theme.toml")
	if _, err := CheckThemeFile(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("文件不存在时应返回 ErrNotExist，实际 %v", err)
	}

	body := "[dark]\naccent = \"#112233\"\n[light]\ntext = \"#ffffff\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := CheckThemeFile(path)
	if err != nil {
		t.Fatalf("校验应通过: %v", err)
	}
	if n != 2 {
		t.Fatalf("应统计到 2 项覆盖，实际 %d", n)
	}
}

func TestExampleThemeTOML(t *testing.T) {
	text := ExampleThemeTOML()
	for _, want := range []string{"[dark]", "[light]", "# accent = \"", "# mark_fg = \"", "zero-red|hanpai-blue"} {
		if !strings.Contains(text, want) {
			t.Fatalf("模板缺少 %q：\n%s", want, text)
		}
	}
	var f themeFile
	if _, err := toml.Decode(text, &f); err != nil {
		t.Fatalf("模板应是合法 TOML: %v", err)
	}
	if f.count() != 0 {
		t.Fatalf("模板默认应全部注释，实际 %d 项生效", f.count())
	}
}

func TestPaletteFields(t *testing.T) {
	for _, p := range []palette{zeroDark, zeroLight, blueDark, blueLight} {
		if got := len(p.fields()); got != 13 {
			t.Fatalf("palette 应有 13 个可覆盖槽位，实际 %d", got)
		}
		for _, f := range p.fields() {
			if _, ok := p.slot(f.name); !ok {
				t.Fatalf("槽位 %q 无法按名查找", f.name)
			}
		}
	}
}

func TestLightPaletteContrast(t *testing.T) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for name, p := range map[string]palette{
		"zero-red":    zeroLight,
		"hanpai-blue": blueLight,
	} {
		t.Run(name, func(t *testing.T) {
			assertPaletteContrast(t, p, white)
		})
	}
}

func TestZeroRedDarkContrast(t *testing.T) {
	darkBg := color.RGBA{R: 16, G: 16, B: 20, A: 255} // 约 #101014
	assertPaletteContrast(t, zeroDark, darkBg)
}

func assertPaletteContrast(t *testing.T, p palette, bg color.Color) {
	t.Helper()
	roles := map[string]color.Color{
		"text":   p.text,
		"muted":  p.muted,
		"dim":    p.dim,
		"accent": p.accent,
		"ok":     p.ok,
		"err":    p.err,
		"warn":   p.warn,
	}
	for name, c := range roles {
		if got := contrastRatio(c, bg); got < 4.5 {
			t.Fatalf("主题 %s 在背景上的对比度只有 %.2f，低于 4.5", name, got)
		}
	}
	if got := contrastRatio(p.selFg, p.selBg); got < 4.5 {
		t.Fatalf("选中行对比度只有 %.2f", got)
	}
	if got := contrastRatio(p.playFg, p.playBg); got < 4.5 {
		t.Fatalf("播放行对比度只有 %.2f", got)
	}
	if got := contrastRatio(p.markFg, p.markBg); got < 4.5 {
		t.Fatalf("多选标记对比度只有 %.2f", got)
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

func TestZeroRedLightThemeRender(t *testing.T) {
	old := curPalette
	applyTheme(zeroLight)
	t.Cleanup(func() { applyTheme(old) })

	m := fixtureModel()
	lines := assertFrame(t, m.View().Content, 100, 30)
	frame := strings.Join(lines, "\n")
	if !strings.Contains(frame, "\x1b[") {
		t.Skip("当前终端未启用颜色，跳过颜色断言")
	}
	if !strings.Contains(frame, "38;2;43;38;43") {
		t.Fatal("佐洛红浅色主题正文应使用 #2b262b")
	}
}

func TestHanpaiBlueLightThemeRender(t *testing.T) {
	old := curPalette
	applyTheme(blueLight)
	t.Cleanup(func() { applyTheme(old) })

	m := fixtureModel()
	lines := assertFrame(t, m.View().Content, 100, 30)
	frame := strings.Join(lines, "\n")
	if !strings.Contains(frame, "\x1b[") {
		t.Skip("当前终端未启用颜色，跳过颜色断言")
	}
	if !strings.Contains(frame, "38;2;36;41;47") {
		t.Fatal("半拍蓝浅色主题正文应使用 #24292f")
	}
}

// isolateConfig 把 HOME / XDG / AppData 指向临时目录，避免读到真实 theme.toml。
func isolateConfig(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("AppData", filepath.Join(root, "appdata"))
	dir, err := config.DataDir()
	if err != nil {
		t.Fatalf("创建临时数据目录失败: %v", err)
	}
	return dir
}

func openThemeStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "theme.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// fgSeq / bgSeq 把颜色写成渲染断言里的 ANSI truecolor 片段，
// 避免测试再把某一版配色的十六进制值写死。
func fgSeq(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

func bgSeq(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}
