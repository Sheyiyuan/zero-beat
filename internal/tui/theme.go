package tui

import (
	"fmt"
	"image/color"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sheyiyuan/zero-beat/internal/config"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// ThemeModeKey / ThemePaletteKey 是 settings 表里的键名，CLI 与 TUI 共用。
const (
	ThemeModeKey    = "theme"
	ThemePaletteKey = "theme_palette"
)

// 内置配色 ID；zero-red（佐洛红）是默认配色，hanpai-blue（半拍蓝）是 v0.6 及更早的蓝色。
const (
	PaletteZeroRed    = "zero-red"
	PaletteHanpaiBlue = "hanpai-blue"
)

// palette 是一套语义色板；深色/浅色只换颜色，不动布局与样式语义。
type palette struct {
	accent color.Color
	text   color.Color
	muted  color.Color
	dim    color.Color
	ok     color.Color
	err    color.Color
	warn   color.Color
	selBg  color.Color
	selFg  color.Color
	playBg color.Color
	playFg color.Color
	markBg color.Color
	markFg color.Color
}

// zeroDark 是默认配色的深色变体，颜色取自 assets/icons/appicon.svg：
// 光环红 #ff5555 / 浅红 #ff8080、耳内粉 #ecd2df、头发炭黑 #232324、
// 米白 #fdf1f3 与蝴蝶结上的琥珀 #e8ad4c。深色终端下整体提亮一档。
var zeroDark = palette{
	accent: lipgloss.Color("#ff8080"),
	text:   lipgloss.Color("#f4eaec"),
	muted:  lipgloss.Color("#b9a9b1"),
	dim:    lipgloss.Color("#8a7c84"),
	ok:     lipgloss.Color("#8fd0a0"),
	err:    lipgloss.Color("#ff5555"),
	warn:   lipgloss.Color("#e8ad4c"),
	selBg:  lipgloss.Color("#ff5555"),
	selFg:  lipgloss.Color("#1a1216"),
	playBg: lipgloss.Color("#332d33"),
	playFg: lipgloss.Color("#f4eaec"),
	markBg: lipgloss.Color("#4a2431"),
	markFg: lipgloss.Color("#ffb6c0"),
}

// zeroLight 是默认配色的浅色变体：把图标的红与粉压暗到白底可读，
// 正文用头发炭黑，播放行用图标背景的米白。
var zeroLight = palette{
	accent: lipgloss.Color("#b02a52"),
	text:   lipgloss.Color("#2b262b"),
	muted:  lipgloss.Color("#6b5f66"),
	dim:    lipgloss.Color("#7a6e75"),
	ok:     lipgloss.Color("#1a7f37"),
	err:    lipgloss.Color("#b42318"),
	warn:   lipgloss.Color("#8a5a00"),
	selBg:  lipgloss.Color("#c2182f"),
	selFg:  lipgloss.Color("#ffffff"),
	playBg: lipgloss.Color("#f3e9ec"),
	playFg: lipgloss.Color("#2b262b"),
	markBg: lipgloss.Color("#f7dbe2"),
	markFg: lipgloss.Color("#8c1c33"),
}

// blueDark / blueLight 是 v0.6 及更早的蓝色配色，保留为可选主题。
var blueDark = palette{
	accent: lipgloss.Color("#66ccff"),
	text:   lipgloss.Color("#d7dae0"),
	muted:  lipgloss.Color("#7b8394"),
	dim:    lipgloss.Color("#4b5263"),
	ok:     lipgloss.Color("#a6e3a1"),
	err:    lipgloss.Color("#f38ba8"),
	warn:   lipgloss.Color("#f9e2af"),
	selBg:  lipgloss.Color("#66ccff"),
	selFg:  lipgloss.Color("#10131a"),
	playBg: lipgloss.Color("#3a3f4a"),
	playFg: lipgloss.Color("#e6e9ef"),
	markBg: lipgloss.Color("#1f4059"),
	markFg: lipgloss.Color("#b9ecff"),
}

var blueLight = palette{
	accent: lipgloss.Color("#0969da"),
	text:   lipgloss.Color("#24292f"),
	muted:  lipgloss.Color("#57606a"),
	dim:    lipgloss.Color("#6e7781"),
	ok:     lipgloss.Color("#1a7f37"),
	err:    lipgloss.Color("#cf222e"),
	warn:   lipgloss.Color("#9a6700"),
	selBg:  lipgloss.Color("#0969da"),
	selFg:  lipgloss.Color("#ffffff"),
	playBg: lipgloss.Color("#eaeef2"),
	playFg: lipgloss.Color("#24292f"),
	markBg: lipgloss.Color("#dbeafe"),
	markFg: lipgloss.Color("#0b4a8f"),
}

var curPalette = zeroDark
var st = newStyles(zeroDark)

type styles struct {
	title    lipgloss.Style
	accent   lipgloss.Style
	text     lipgloss.Style
	muted    lipgloss.Style
	dim      lipgloss.Style
	selected lipgloss.Style
	playing  lipgloss.Style
	marked   lipgloss.Style
	ok       lipgloss.Style
	err      lipgloss.Style
	warn     lipgloss.Style
}

func newStyles(p palette) styles {
	return styles{
		title:    lipgloss.NewStyle().Bold(true).Foreground(p.accent),
		accent:   lipgloss.NewStyle().Foreground(p.accent),
		text:     lipgloss.NewStyle().Foreground(p.text),
		muted:    lipgloss.NewStyle().Foreground(p.muted),
		dim:      lipgloss.NewStyle().Foreground(p.dim),
		selected: lipgloss.NewStyle().Background(p.selBg).Foreground(p.selFg).Bold(true),
		playing:  lipgloss.NewStyle().Background(p.playBg).Foreground(p.playFg),
		marked:   lipgloss.NewStyle().Background(p.markBg).Foreground(p.markFg),
		ok:       lipgloss.NewStyle().Foreground(p.ok),
		err:      lipgloss.NewStyle().Foreground(p.err),
		warn:     lipgloss.NewStyle().Foreground(p.warn),
	}
}

type themeMode string

const (
	themeAuto  themeMode = "auto"
	themeDark  themeMode = "dark"
	themeLight themeMode = "light"
)

func parseTheme(s string) themeMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "light":
		return themeLight
	case "dark":
		return themeDark
	default:
		return themeAuto
	}
}

// resolved 把 auto 解析成具体的深色/浅色模式。
func (t themeMode) resolved() themeMode {
	switch t {
	case themeDark, themeLight:
		return t
	default:
		if detectLightBackground() {
			return themeLight
		}
		return themeDark
	}
}

// paletteID 是内置配色的稳定 ID。
type paletteID string

const (
	paletteZeroRed    paletteID = PaletteZeroRed
	paletteHanpaiBlue paletteID = PaletteHanpaiBlue
)

// parsePalette 解析设置/环境变量里的配色名；无法识别时回落到默认配色。
func parsePalette(s string) paletteID {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case PaletteHanpaiBlue, "hanpai", "hanpaiblue", "blue", "半拍蓝":
		return paletteHanpaiBlue
	default:
		return paletteZeroRed
	}
}

// ValidPalette 判断 CLI 传入的配色名是否受支持。
func ValidPalette(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case PaletteZeroRed, "zero", "red", "zerored", "佐洛红",
		PaletteHanpaiBlue, "hanpai", "hanpaiblue", "blue", "半拍蓝":
		return true
	}
	return false
}

// CanonicalPalette 把别名归一化成稳定的配色 ID，CLI 写回设置时使用。
func CanonicalPalette(s string) string {
	return string(parsePalette(s))
}

// PaletteLabel 返回配色的中英文名，供 CLI 与帮助文案展示。
func PaletteLabel(s string) string {
	if parsePalette(s) == paletteHanpaiBlue {
		return "半拍蓝 hanpai blue"
	}
	return "佐洛红 zero red"
}

// colors 返回指定模式下的配色；auto 会先按终端背景解析成深色或浅色。
func (n paletteID) colors(m themeMode) palette {
	light := m.resolved() == themeLight
	switch n {
	case paletteHanpaiBlue:
		if light {
			return blueLight
		}
		return blueDark
	default:
		if light {
			return zeroLight
		}
		return zeroDark
	}
}

// resolveThemeMode 的优先级：ZERO_THEME 环境变量 > 数据库设置 theme > auto。
func resolveThemeMode(st *store.Store) themeMode {
	if v := strings.TrimSpace(os.Getenv("ZERO_THEME")); v != "" {
		return parseTheme(v)
	}
	if st != nil {
		return parseTheme(st.Setting(ThemeModeKey, string(themeAuto)))
	}
	return themeAuto
}

// resolveThemePalette 的优先级：ZERO_THEME_PALETTE 环境变量 > 数据库设置 theme_palette > 零拍。
func resolveThemePalette(st *store.Store) paletteID {
	if v := strings.TrimSpace(os.Getenv("ZERO_THEME_PALETTE")); v != "" {
		return parsePalette(v)
	}
	if st != nil {
		return parsePalette(st.Setting(ThemePaletteKey, PaletteZeroRed))
	}
	return paletteZeroRed
}

// resolveTheme 组合模式、配色与 theme.toml 覆盖，返回最终色板。
//
// theme.toml 无效时仍然返回内置色板，错误交给调用方提示：一个颜色写错
// 不应该让界面起不来。
func resolveTheme(st *store.Store) (palette, error) {
	mode := resolveThemeMode(st)
	resolved := mode.resolved()
	p := resolveThemePalette(st).colors(resolved)
	path, err := config.ThemePath()
	if err != nil {
		return p, fmt.Errorf("定位主题配置: %w", err)
	}
	overrides, err := loadThemeOverrides(path)
	if err != nil {
		return p, err
	}
	if err := applyOverrides(&p, overrides.forMode(resolved)); err != nil {
		return p, err
	}
	return p, nil
}

// detectLightBackground 读 COLORFGBG（形如「15;0」或「0;15」）。
// 背景色号 7 及 9–15 视为浅色；没有该变量或解析不了时按深色处理。
func detectLightBackground() bool {
	v := strings.TrimSpace(os.Getenv("COLORFGBG"))
	if v == "" {
		return false
	}
	parts := strings.Split(v, ";")
	bg := strings.TrimSpace(parts[len(parts)-1])
	n, err := strconv.Atoi(bg)
	if err != nil {
		return false
	}
	return n == 7 || n >= 9
}

func applyTheme(p palette) {
	curPalette = p
	st = newStyles(p)
}
