package keymap

import (
	"os"
	"path/filepath"
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
	conflict := filepath.Join(dir, "conflict.toml")
	_ = os.WriteFile(conflict, []byte(`[keys]
next = ["p"]
`), 0o600)
	if _, err := Load(conflict); err == nil {
		t.Fatal("next 与默认 mode 冲突应报错")
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
