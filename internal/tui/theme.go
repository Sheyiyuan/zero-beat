package tui

import (
	"image/color"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sheyiyuan/zero-beat/internal/store"
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
}

// 配色承接 v1 的 #66ccff，暗色终端下对比度足够。
var darkPalette = palette{
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
}

// 浅色终端用的配套色板；正文/强调色在白底上至少 4.5:1。
var lightPalette = palette{
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
}

var curPalette = darkPalette
var st = newStyles(darkPalette)

type styles struct {
	title    lipgloss.Style
	accent   lipgloss.Style
	text     lipgloss.Style
	muted    lipgloss.Style
	dim      lipgloss.Style
	selected lipgloss.Style
	playing  lipgloss.Style
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

func (t themeMode) palette() palette {
	switch t {
	case themeLight:
		return lightPalette
	case themeDark:
		return darkPalette
	default:
		if detectLightBackground() {
			return lightPalette
		}
		return darkPalette
	}
}

// resolveThemeMode 的优先级：ZERO_THEME 环境变量 > 数据库设置 theme > auto。
func resolveThemeMode(st *store.Store) themeMode {
	if v := strings.TrimSpace(os.Getenv("ZERO_THEME")); v != "" {
		return parseTheme(v)
	}
	if st != nil {
		return parseTheme(st.Setting("theme", string(themeAuto)))
	}
	return themeAuto
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
