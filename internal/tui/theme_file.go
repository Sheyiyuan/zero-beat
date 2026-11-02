package tui

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/BurntSushi/toml"
)

// themeFile 是 ~/.config/zero-beat/theme.toml 的结构：按深/浅模式覆盖颜色槽位。
type themeFile struct {
	Dark  map[string]string `toml:"dark"`
	Light map[string]string `toml:"light"`
}

// forMode 返回该模式对应的覆盖；auto 调用前应先解析成 dark/light。
func (f themeFile) forMode(m themeMode) map[string]string {
	if m == themeLight {
		return f.Light
	}
	return f.Dark
}

func (f themeFile) count() int {
	return len(f.Dark) + len(f.Light)
}

// readThemeFile 读取并校验 theme.toml；文件不存在时返回 os.ErrNotExist。
func readThemeFile(path string) (themeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return themeFile{}, fmt.Errorf("读取主题配置 %s: %w", path, err)
	}
	var f themeFile
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return themeFile{}, fmt.Errorf("解析主题配置 %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return themeFile{}, fmt.Errorf("主题配置 %s 里有未知项：%s", path, strings.Join(keys, ", "))
	}
	for _, section := range []struct {
		name string
		m    map[string]string
	}{{"dark", f.Dark}, {"light", f.Light}} {
		if err := validateOverrides(section.name, section.m); err != nil {
			return themeFile{}, fmt.Errorf("主题配置 %s: %w", path, err)
		}
	}
	return f, nil
}

// loadThemeOverrides 读取覆盖；没有 theme.toml 时返回空覆盖。
func loadThemeOverrides(path string) (themeFile, error) {
	f, err := readThemeFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return themeFile{}, nil
	}
	return f, err
}

// CheckThemeFile 校验 theme.toml，返回覆盖的颜色项数。
func CheckThemeFile(path string) (int, error) {
	f, err := readThemeFile(path)
	if err != nil {
		return 0, err
	}
	return f.count(), nil
}

// ExampleThemeTOML 生成带注释的 theme.toml 模板。
//
// 所有项默认注释掉，避免生成后把当前配色整份钉死；用户只取消注释
// 想改的那几项，其余继续跟随所选配色。
func ExampleThemeTOML() string {
	var b strings.Builder
	b.WriteString("# zero-beat 自定义主题颜色\n")
	b.WriteString("# 只取消注释想覆盖的项；没写的沿用所选配色。\n")
	b.WriteString("# 配色用 zero theme palette zero-red|hanpai-blue 选择。\n")
	b.WriteString("# 注释里的色值是佐洛红的默认值，仅作参考。\n")
	b.WriteString("# 颜色写法：#rgb 或 #rrggbb。改完重启终端界面生效。\n")
	b.WriteString("# 槽位：accent text muted dim ok err warn sel_bg sel_fg play_bg play_fg mark_bg mark_fg\n\n")
	for _, section := range []struct {
		name string
		p    palette
	}{{"dark", zeroDark}, {"light", zeroLight}} {
		fmt.Fprintf(&b, "[%s]\n", section.name)
		for _, slot := range section.p.fields() {
			fmt.Fprintf(&b, "# %s = %q\n", slot.name, hexColor(*slot.value))
		}
		b.WriteString("\n")
	}
	return b.String()
}

type paletteField struct {
	name  string
	value *color.Color
}

// fields 列出可被 theme.toml 覆盖的槽位。新增颜色时只改这一处，
// applyOverrides 与 ExampleThemeTOML 会一起生效。
func (p *palette) fields() []paletteField {
	return []paletteField{
		{"accent", &p.accent},
		{"text", &p.text},
		{"muted", &p.muted},
		{"dim", &p.dim},
		{"ok", &p.ok},
		{"err", &p.err},
		{"warn", &p.warn},
		{"sel_bg", &p.selBg},
		{"sel_fg", &p.selFg},
		{"play_bg", &p.playBg},
		{"play_fg", &p.playFg},
		{"mark_bg", &p.markBg},
		{"mark_fg", &p.markFg},
	}
}

func (p *palette) slot(name string) (*color.Color, bool) {
	for _, f := range p.fields() {
		if f.name == name {
			return f.value, true
		}
	}
	return nil, false
}

func validateOverrides(section string, overrides map[string]string) error {
	names := sortedKeys(overrides)
	var p palette
	for _, name := range names {
		if _, ok := p.slot(name); !ok {
			return fmt.Errorf("[%s] 未知颜色项 %q", section, name)
		}
		if _, err := parseHexColor(overrides[name]); err != nil {
			return fmt.Errorf("[%s] %s: %w", section, name, err)
		}
	}
	return nil
}

func applyOverrides(p *palette, overrides map[string]string) error {
	for _, name := range sortedKeys(overrides) {
		dst, ok := p.slot(name)
		if !ok {
			return fmt.Errorf("未知颜色项 %q", name)
		}
		c, err := parseHexColor(overrides[name])
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		*dst = c
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseHexColor 只接受 #rgb / #rrggbb，保证写错时能明确报错而不是静默变色。
func parseHexColor(raw string) (color.Color, error) {
	s := strings.TrimSpace(raw)
	if (len(s) != 4 && len(s) != 7) || !strings.HasPrefix(s, "#") {
		return nil, fmt.Errorf("颜色 %q 应为 #rgb 或 #rrggbb", raw)
	}
	for _, r := range s[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return nil, fmt.Errorf("颜色 %q 含非十六进制字符", raw)
		}
	}
	return lipgloss.Color(s), nil
}

// hexColor 把 color.Color 写成 #rrggbb，供模板与调试输出使用。
func hexColor(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
}
