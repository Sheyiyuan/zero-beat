package keymap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func key(text string, code rune) tea.Key {
	return tea.Key{Text: text, Code: code}
}

func TestDefaultBindings(t *testing.T) {
	km := Default()
	if !km.Match(Next, key("]", ']')) || km.Match(Next, key("n", 'n')) {
		t.Fatal("默认 next 应为 ]")
	}
	if !km.Match(PageUp, tea.Key{Code: 'u', Mod: tea.ModCtrl}) {
		t.Fatal("默认 page_up 应包含 ctrl+u")
	}
	if got := km.Label(Quit); got != "q / Q" {
		t.Fatalf("quit 标签不符: %q", got)
	}
	// 歌词：单键 T 为主，ctrl+l 备选。
	if !km.Match(LyricsView, key("T", 'T')) {
		t.Fatal("歌词应支持单键 T")
	}
	shiftT := tea.Key{Code: 't', Text: "T"} // 真实终端里的 Shift+T
	if !km.Match(LyricsView, shiftT) {
		t.Fatal("真实 Shift+T 事件应命中歌词")
	}
	if !km.Match(LyricsView, tea.Key{Code: 'l', Mod: tea.ModCtrl}) {
		t.Fatal("歌词应保留 ctrl+l 备选")
	}
}

func TestLoadOverrideAndDisable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keymap.toml")
	if err := os.WriteFile(path, []byte(`[keys]
next = ["z"]
previous = []
`), 0o600); err != nil {
		t.Fatal(err)
	}
	km, err := Load(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if !km.Match(Next, key("z", 'z')) || km.Match(Next, key("]", ']')) {
		t.Fatal("next 覆盖未生效")
	}
	if len(km.Keys(Previous)) != 0 {
		t.Fatalf("空数组应禁用动作: %v", km.Keys(Previous))
	}
}

func TestLoadRejectsConflictAndUnknown(t *testing.T) {
	dir := t.TempDir()
	// 两份用户自定义动作抢同一个键：整份拒绝（方案 B）。
	conflict := filepath.Join(dir, "conflict.toml")
	_ = os.WriteFile(conflict, []byte(`[keys]
next = ["p"]
mode = ["p"]
`), 0o600)
	if _, err := Load(conflict); err == nil {
		t.Fatal("两个自定义动作抢同一个键应报错")
	}
	unknown := filepath.Join(dir, "unknown.toml")
	_ = os.WriteFile(unknown, []byte(`[keys]
not_an_action = ["x"]
`), 0o600)
	if _, err := Load(unknown); err == nil {
		t.Fatal("未知动作应报错")
	}
}

func TestExampleTOMLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keymap.toml")
	if err := os.WriteFile(path, []byte(ExampleTOML()), 0o600); err != nil {
		t.Fatal(err)
	}
	km, err := Load(path)
	if err != nil {
		t.Fatalf("示例配置应可加载: %v", err)
	}
	if !km.Match(Next, key("]", ']')) || !km.Match(LoginImage, key("o", 'o')) {
		t.Fatal("示例配置键位不符")
	}
}

func TestUpperCaseDoesNotMatchLowerCaseAction(t *testing.T) {
	km := Default()
	// 真实终端里的 Shift+C：Code 是小写 c，Text 是大写 C。
	upperC := tea.Key{Code: 'c', Text: "C"}
	if km.Match(QueueView, upperC) {
		t.Fatal("大写 C 不应触发小写 c 的队列动作")
	}
	if !km.Match(EditClip, upperC) {
		t.Fatal("大写 C 应触发片段编辑")
	}
	lowerC := tea.Key{Code: 'c', Text: "c"}
	if !km.Match(QueueView, lowerC) || km.Match(EditClip, lowerC) {
		t.Fatal("小写 c 应触发队列，不应触发片段编辑")
	}
	if km.Match(Previous, tea.Key{Code: '[', Text: "{"}) || !km.Match(ClipSetStart, tea.Key{Code: '[', Text: "{"}) {
		t.Fatal("Shift+[ 应触发设起点，不应触发上一首")
	}
}

// TestUserOverrideWinsOverDefault 验证方案 B：用户绑定优先，
// 被抢走按键的默认绑定禁用并产生一条 warning，而不是整份回退。
func TestUserOverrideWinsOverDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keymap.toml")
	if err := os.WriteFile(path, []byte(`[keys]
next = ["p"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	km, warnings, err := LoadWithWarnings(path)
	if err != nil {
		t.Fatalf("用户覆盖与默认冲突不应整份失败: %v", err)
	}
	if !km.Match(Next, key("p", 'p')) {
		t.Fatal("用户绑定 next=p 应生效")
	}
	if km.Match(Mode, key("p", 'p')) {
		t.Fatal("被抢走按键的默认 mode=p 应被禁用")
	}
	if len(km.Keys(Mode)) != 0 {
		t.Fatalf("mode 的 p 应被移除，剩余 %v", km.Keys(Mode))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "mode") || !strings.Contains(warnings[0], "next") {
		t.Fatalf("应产生一条指向 mode 与 next 的降级提示，实际 %v", warnings)
	}
}

// TestOldInitConfigStillLoads 验证 v0.4.1 用 keymap --init 生成的旧配置
// 在新默认键位（恢复 j/k/h/l、歌词改 ctrl+l）下仍能整份加载。
func TestOldInitConfigStillLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.toml")
	old := `[keys]
up = ["up", "k"]
down = ["down", "j"]
left = ["left", "h"]
right = ["right", "l"]
logout = ["ctrl+w"]
play_pause = ["space"]
`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	km, warnings, err := LoadWithWarnings(path)
	if err != nil {
		t.Fatalf("旧配置应可加载: %v", err)
	}
	if !km.Match(Right, key("l", 'l')) {
		t.Fatal("旧配置 right=l 应保留")
	}
	if !km.Match(LyricsView, tea.Key{Code: 'l', Mod: tea.ModCtrl}) {
		t.Fatal("歌词应使用新的默认 ctrl+l")
	}
	if len(warnings) != 0 {
		t.Fatalf("本用例不应有降级提示: %v", warnings)
	}
}

// TestOldConfigLogoutConflictsWithNewDownloadKey 验证旧配置把 logout 绑在 W、
// 且没写 download_selected 时，用户绑定优先、默认 W 下载被禁用并提示。
func TestOldConfigLogoutConflictsWithNewDownloadKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-logout.toml")
	if err := os.WriteFile(path, []byte(`[keys]
logout = ["W"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	km, warnings, err := LoadWithWarnings(path)
	if err != nil {
		t.Fatalf("旧 logout=W 不应让整份配置失败: %v", err)
	}
	if !km.Match(Logout, key("W", 'W')) {
		t.Fatal("用户 logout=W 应生效")
	}
	if km.Match(DownloadSelected, key("W", 'W')) {
		t.Fatal("被占用的默认 download_selected=W 应禁用")
	}
	if len(warnings) == 0 {
		t.Fatal("应提示 download_selected 的默认键被占用")
	}
}
