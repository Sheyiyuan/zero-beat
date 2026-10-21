// Package keymap 定义可配置的按键动作，并把 keymap.toml 合并到内置默认值。
package keymap

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/BurntSushi/toml"
)

// Action 是稳定的动作 ID；配置文件只使用这些 ID，不直接写功能代码。
type Action string

const (
	Quit             Action = "quit"
	HelpPopup        Action = "help_popup"
	HelpPage         Action = "help_page"
	Login            Action = "login"
	LocalSearch      Action = "local_search"
	Search           Action = "search"
	QueueView        Action = "queue_view"
	PlayPause        Action = "play_pause"
	Next             Action = "next"
	Previous         Action = "previous"
	SeekBack1        Action = "seek_back_1"
	SeekBack5        Action = "seek_back_5"
	SeekFwd5         Action = "seek_forward_5"
	SeekFwd10        Action = "seek_forward_10"
	VolumeDown       Action = "volume_down"
	VolumeUp         Action = "volume_up"
	CompDown         Action = "comp_down"
	CompUp           Action = "comp_up"
	Mode             Action = "mode"
	Mute             Action = "mute"
	Quality          Action = "quality"
	Sleep            Action = "sleep"
	OffsetDown       Action = "offset_down"
	OffsetUp         Action = "offset_up"
	OffsetReset      Action = "offset_reset"
	Up               Action = "up"
	Down             Action = "down"
	Left             Action = "left"
	Right            Action = "right"
	Top              Action = "top"
	Bottom           Action = "bottom"
	PageUp           Action = "page_up"
	PageDown         Action = "page_down"
	PlaySelected     Action = "play_selected"
	Download         Action = "download"
	DownloadSelected Action = "download_selected"
	AddPlaylist      Action = "add_playlist"
	MergePlaylist    Action = "merge_playlist"
	Enqueue          Action = "enqueue"
	PlayNext         Action = "play_next"
	Remove           Action = "remove"
	Delete           Action = "delete"
	Rename           Action = "rename"
	New              Action = "new"
	Folders          Action = "folders"
	MixEdit          Action = "mix_edit"
	EditClip         Action = "edit_clip"
	ClipSetStart     Action = "clip_set_start"
	ClipSetEnd       Action = "clip_set_end"
	ClipClear        Action = "clip_clear"
	Unlock           Action = "unlock"
	Refresh          Action = "refresh"
	QueueUp          Action = "queue_up"
	QueueDown        Action = "queue_down"
	Source           Action = "source"
	LoginImage       Action = "login_image"
	Logout           Action = "logout"
	Back             Action = "back"
)

// Defaults 是内置键位；配置文件按动作覆盖，不写出的动作继续使用这里。
var Defaults = map[Action][]string{
	Quit:             {"q", "Q"},
	HelpPopup:        {"H"},
	HelpPage:         {},
	Login:            {"L"},
	LocalSearch:      {"/"},
	Search:           {"?"},
	QueueView:        {"c"},
	PlayPause:        {"space"},
	Next:             {"]"},
	Previous:         {"["},
	SeekBack1:        {"x"},
	SeekBack5:        {"X"},
	SeekFwd5:         {"v"},
	SeekFwd10:        {"V"},
	VolumeDown:       {"-"},
	VolumeUp:         {"+", "="},
	CompDown:         {","},
	CompUp:           {"."},
	Mode:             {"p"},
	Mute:             {"m"},
	Quality:          {"y"},
	Sleep:            {"t"},
	OffsetDown:       {"<"},
	OffsetUp:         {">"},
	OffsetReset:      {"0"},
	Up:               {"up", "k"},
	Down:             {"down", "j"},
	Left:             {"left", "h"},
	Right:            {"right", "l"},
	Top:              {"g"},
	Bottom:           {"G"},
	PageUp:           {"ctrl+u", "pgup"},
	PageDown:         {"ctrl+d", "pgdown"},
	PlaySelected:     {"n"},
	Download:         {"w"},
	DownloadSelected: {"W"},
	AddPlaylist:      {"a"},
	MergePlaylist:    {"S"},
	Enqueue:          {"E", "A"},
	PlayNext:         {"e"},
	Remove:           {"d", "\\"},
	Delete:           {"D"},
	Rename:           {"R"},
	New:              {"N"},
	Folders:          {"f"},
	MixEdit:          {"M"},
	EditClip:         {"C"},
	ClipSetStart:     {"{"},
	ClipSetEnd:       {"}"},
	ClipClear:        {"ctrl+r"},
	Unlock:           {"U"},
	Refresh:          {"r"},
	QueueUp:          {"K"},
	QueueDown:        {"J"},
	Source:           {"s"},
	LoginImage:       {"o"},
	Logout:           {"ctrl+w"},
	Back:             {"b"},
}

var actionSet = func() map[Action]bool {
	m := make(map[Action]bool, len(Defaults))
	for a := range Defaults {
		m[a] = true
	}
	return m
}()

type binding struct {
	display string
	code    rune
	text    string
	mods    tea.KeyMod
}

// Keymap 是编译后的动作绑定。
type Keymap struct {
	labels   map[Action][]string
	bindings map[Action][]binding
}

// Default 返回内置键位。
func Default() *Keymap {
	km, err := build(Defaults)
	if err != nil {
		panic("keymap: 默认键位冲突: " + err.Error())
	}
	return km
}

// Load 读取 TOML 配置；文件不存在时返回默认键位。
func Load(path string) (*Keymap, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取快捷键配置: %w", err)
	}
	var doc struct {
		Keys map[string][]string `toml:"keys"`
	}
	md, err := toml.Decode(string(raw), &doc)
	if err != nil {
		return nil, fmt.Errorf("解析快捷键配置: %w", err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		return nil, fmt.Errorf("快捷键配置包含未知字段: %s", u)
	}
	merged := make(map[Action][]string, len(Defaults))
	for a, keys := range Defaults {
		merged[a] = append([]string(nil), keys...)
	}
	for name, keys := range doc.Keys {
		a := Action(name)
		if !actionSet[a] {
			return nil, fmt.Errorf("未知动作 %q", name)
		}
		merged[a] = append([]string(nil), keys...)
	}
	return build(merged)
}

func build(actions map[Action][]string) (*Keymap, error) {
	km := &Keymap{
		labels:   make(map[Action][]string, len(actions)),
		bindings: make(map[Action][]binding, len(actions)),
	}
	used := map[string]Action{}
	for a, keys := range actions {
		seen := map[string]bool{}
		for _, raw := range keys {
			b, err := parseBinding(raw)
			if err != nil {
				return nil, fmt.Errorf("动作 %s 的按键 %q: %w", a, raw, err)
			}
			if seen[b.display] {
				continue
			}
			seen[b.display] = true
			if prev, ok := used[b.display]; ok && prev != a {
				return nil, fmt.Errorf("按键 %q 同时绑定到 %s 和 %s", b.display, prev, a)
			}
			used[b.display] = a
			km.labels[a] = append(km.labels[a], b.display)
			km.bindings[a] = append(km.bindings[a], b)
		}
	}
	return km, nil
}

// Keys 返回动作当前生效的按键展示值。
func (k *Keymap) Keys(a Action) []string {
	if k == nil {
		return nil
	}
	return append([]string(nil), k.labels[a]...)
}

// Label 返回形如「j / down」的展示字符串。
func (k *Keymap) Label(a Action) string {
	keys := k.Keys(a)
	return strings.Join(keys, " / ")
}

// Match 判断当前按键是否命中动作。
func (k *Keymap) Match(a Action, key tea.Key) bool {
	if k == nil {
		return false
	}
	for _, b := range k.bindings[a] {
		if b.match(key) {
			return true
		}
	}
	return false
}

func (b binding) match(key tea.Key) bool {
	if b.mods != 0 {
		if key.Mod&b.mods != b.mods {
			return false
		}
	} else if key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) != 0 {
		return false
	}
	// 终端里 Shift+C 通常是 Code='c'、Text="C"；如果先按 Code 匹配，
	// 小写 c 的动作会抢走上层 C 的键位。可打印字符一律以 Text 为准。
	if b.text != "" && key.Text != "" {
		return key.Text == b.text
	}
	if b.code != 0 {
		return key.Code == b.code
	}
	return key.Text == b.text
}

func parseBinding(raw string) (binding, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return binding{}, errors.New("按键不能为空")
	}
	if raw == " " {
		return binding{display: "space", code: tea.KeySpace, text: " "}, nil
	}
	// 「+」本身是一个键，不是修饰符分隔符。
	if raw == "+" {
		return binding{display: "+", code: '+', text: "+"}, nil
	}
	parts := strings.Split(raw, "+")
	base := parts[len(parts)-1]
	var mods tea.KeyMod
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "ctrl", "control":
			mods |= tea.ModCtrl
		case "alt", "meta":
			mods |= tea.ModAlt
		case "shift":
			mods |= tea.ModShift
		case "super", "cmd":
			mods |= tea.ModSuper
		default:
			return binding{}, fmt.Errorf("未知修饰键 %q", p)
		}
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return binding{}, errors.New("缺少基础按键")
	}
	display := base
	if mods&tea.ModCtrl != 0 {
		display = "ctrl+" + display
	}
	if mods&tea.ModAlt != 0 {
		display = "alt+" + display
	}
	if mods&tea.ModShift != 0 {
		display = "shift+" + display
	}
	if mods&tea.ModSuper != 0 {
		display = "super+" + display
	}
	if code, ok := namedKey(base); ok {
		if mods != 0 && code == tea.KeySpace {
			return binding{display: display, code: code, text: " ", mods: mods}, nil
		}
		if mods == 0 {
			switch code {
			case tea.KeyUp:
				display = "↑"
			case tea.KeyDown:
				display = "↓"
			case tea.KeyLeft:
				display = "←"
			case tea.KeyRight:
				display = "→"
			case tea.KeyPgUp:
				display = "PgUp"
			case tea.KeyPgDown:
				display = "PgDn"
			}
		}
		return binding{display: display, code: code, mods: mods}, nil
	}
	r := []rune(base)
	if len(r) != 1 {
		return binding{}, fmt.Errorf("只支持单键或命名键，收到 %q", base)
	}
	return binding{display: display, code: r[0], text: string(r[0]), mods: mods}, nil
}

func namedKey(s string) (rune, bool) {
	switch strings.ToLower(s) {
	case "space":
		return tea.KeySpace, true
	case "enter", "return":
		return tea.KeyEnter, true
	case "tab":
		return tea.KeyTab, true
	case "esc", "escape":
		return tea.KeyEscape, true
	case "backspace":
		return tea.KeyBackspace, true
	case "delete", "del":
		return tea.KeyDelete, true
	case "up":
		return tea.KeyUp, true
	case "down":
		return tea.KeyDown, true
	case "left":
		return tea.KeyLeft, true
	case "right":
		return tea.KeyRight, true
	case "pgup", "pageup":
		return tea.KeyPgUp, true
	case "pgdown", "pagedown":
		return tea.KeyPgDown, true
	case "home":
		return tea.KeyHome, true
	case "end":
		return tea.KeyEnd, true
	}
	return 0, false
}

// ExampleTOML 生成包含全部默认动作的示例配置。
func ExampleTOML() string {
	names := make([]string, 0, len(Defaults))
	for a := range Defaults {
		names = append(names, string(a))
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("# zero-beat 快捷键配置\n")
	b.WriteString("# 只写想改的动作；未写出的动作使用内置默认值。\n")
	b.WriteString("# 空数组表示禁用该动作。\n\n[keys]\n")
	for _, name := range names {
		b.WriteString(name)
		b.WriteString(" = [")
		for i, k := range Defaults[Action(name)] {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(k))
		}
		b.WriteString("]\n")
	}
	return b.String()
}
