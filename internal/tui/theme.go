package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// 配色承接 v1 的 #66ccff，暗色终端下对比度足够。
var (
	colAccent = lipgloss.Color("#66ccff")
	colText   = lipgloss.Color("#d7dae0")
	colMuted  = lipgloss.Color("#7b8394")
	colDim    = lipgloss.Color("#4b5263")
	colOK     = lipgloss.Color("#a6e3a1")
	colErr    = lipgloss.Color("#f38ba8")
	colWarn   = lipgloss.Color("#f9e2af")
	colSelBg  = lipgloss.Color("#66ccff")
	colSelFg  = lipgloss.Color("#10131a")
)

type styles struct {
	title    lipgloss.Style
	accent   lipgloss.Style
	text     lipgloss.Style
	muted    lipgloss.Style
	dim      lipgloss.Style
	selected lipgloss.Style
	ok       lipgloss.Style
	err      lipgloss.Style
	warn     lipgloss.Style
}

var st = newStyles()

func newStyles() styles {
	return styles{
		title:    lipgloss.NewStyle().Bold(true).Foreground(colAccent),
		accent:   lipgloss.NewStyle().Foreground(colAccent),
		text:     lipgloss.NewStyle().Foreground(colText),
		muted:    lipgloss.NewStyle().Foreground(colMuted),
		dim:      lipgloss.NewStyle().Foreground(colDim),
		selected: lipgloss.NewStyle().Background(colSelBg).Foreground(colSelFg).Bold(true),
		ok:       lipgloss.NewStyle().Foreground(colOK),
		err:      lipgloss.NewStyle().Foreground(colErr),
		warn:     lipgloss.NewStyle().Foreground(colWarn),
	}
}

// colorOrNil 让空值回落到终端默认色。
func colorOrNil(c color.Color) color.Color { return c }
