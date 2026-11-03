package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sheyiyuan/zero-beat/internal/control"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// sendControl 走一遍真实的 Update 路径，返回回应和更新后的模型。
func sendControl(t *testing.T, m *Model, req control.Request) (control.Reply, *Model) {
	t.Helper()
	req.Reply = make(chan control.Reply, 1)
	next, _ := m.Update(req)
	mm, ok := next.(*Model)
	if !ok {
		t.Fatalf("Update 应返回 *Model，实际 %T", next)
	}
	select {
	case reply := <-req.Reply:
		return reply, mm
	default:
		t.Fatal("控制请求没有收到回应")
		return control.Reply{}, mm
	}
}

func TestControlFrameAndState(t *testing.T) {
	m := fixtureModel()
	m.keys = keymap.Default()

	reply, _ := sendControl(t, m, control.Request{Kind: control.KindFrame})
	if reply.Err != "" {
		t.Fatalf("取帧不应失败: %s", reply.Err)
	}
	if !strings.Contains(reply.Frame, "曲库") {
		t.Fatalf("帧里应包含当前页面标题，实际：%q", reply.Frame)
	}

	var state controlState
	if err := json.Unmarshal(reply.State, &state); err != nil {
		t.Fatalf("状态快照应能解析成 JSON: %v", err)
	}
	if state.View != "曲库" || state.Width != 100 || state.Height != 30 {
		t.Fatalf("快照字段不符: %+v", state)
	}
	if state.Selected == nil || state.Selected.Title != "Never Gonna Give You Up" {
		t.Fatalf("快照应包含选中曲目: %+v", state.Selected)
	}
	if state.Queue != 2 || state.Mode != "列表循环" {
		t.Fatalf("队列/模式不符: %+v", state)
	}
}

func TestControlInjectsKey(t *testing.T) {
	m := fixtureModel()
	m.keys = keymap.Default()

	reply, next := sendControl(t, m, control.Request{Kind: control.KindKey, Key: "down"})
	if reply.Err != "" {
		t.Fatalf("注入 down 不应失败: %s", reply.Err)
	}
	if next.songIdx != 1 {
		t.Fatalf("down 应把选中项移到第二首，实际 %d", next.songIdx)
	}
}

func TestControlDeniesDestructiveKey(t *testing.T) {
	m := fixtureModel()
	m.keys = keymap.Default()

	reply, _ := sendControl(t, m, control.Request{Kind: control.KindKey, Key: "D"})
	if reply.Err == "" {
		t.Fatal("D 默认应被拒绝")
	}
	if !strings.Contains(reply.Err, string(keymap.Delete)) {
		t.Fatalf("错误信息应点名受限动作，实际：%s", reply.Err)
	}
	if !strings.Contains(reply.Err, "--force") {
		t.Fatalf("错误信息应提示 --force，实际：%s", reply.Err)
	}
}

func TestControlDeniesHardcodedQuit(t *testing.T) {
	m := fixtureModel()
	m.keys = keymap.Default()

	reply, _ := sendControl(t, m, control.Request{Kind: control.KindKey, Key: "ctrl+c"})
	if reply.Err == "" {
		t.Fatal("ctrl+c 是硬编码退出键，默认应被拒绝")
	}
	if !strings.Contains(reply.Err, string(keymap.Quit)) {
		t.Fatalf("错误信息应点名受限动作，实际：%s", reply.Err)
	}
}

func TestControlDeniesRemappedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keymap.toml")
	if err := os.WriteFile(path, []byte("[keys]\ndelete = [\"ctrl+x\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	km, _, err := keymap.LoadWithWarnings(path)
	if err != nil {
		t.Fatal(err)
	}

	m := fixtureModel()
	m.keys = km

	reply, _ := sendControl(t, m, control.Request{Kind: control.KindKey, Key: "ctrl+x"})
	if !strings.Contains(reply.Err, string(keymap.Delete)) {
		t.Fatalf("改绑后的 ctrl+x 仍应被拒绝，实际：%s", reply.Err)
	}

	// delete 已经不在 D 上，注入 D 不该再被拦。
	reply, _ = sendControl(t, m, control.Request{Kind: control.KindKey, Key: "D"})
	if reply.Err != "" {
		t.Fatalf("D 不再触发 delete，不应拒绝：%s", reply.Err)
	}
}

func TestControlTypesTextIntoPrompt(t *testing.T) {
	m := fixtureModel()
	m.keys = keymap.Default()
	m.prompt = promptNewPlaylist

	reply, next := sendControl(t, m, control.Request{Kind: control.KindType, Text: "新歌单"})
	if reply.Err != "" {
		t.Fatalf("注入文本不应失败: %s", reply.Err)
	}
	if next.promptInput != "新歌单" {
		t.Fatalf("输入缓冲应为「新歌单」，实际 %q", next.promptInput)
	}
}

func TestControlTypeRequiresInputFocus(t *testing.T) {
	m := fixtureModel()
	m.keys = keymap.Default()

	reply, next := sendControl(t, m, control.Request{Kind: control.KindType, Text: "q"})
	if reply.Err == "" {
		t.Fatal("没有输入态时 +type 应被拒绝")
	}
	if next.view != viewLibrary {
		t.Fatalf("+type 不应改变视图，实际 %v", viewNames[next.view])
	}
}

func TestControlFrameSearchSelection(t *testing.T) {
	m := fixtureModel()
	m.keys = keymap.Default()
	m.view = viewSearch
	m.searchMode = searchLocalAll
	m.localResults = append([]store.Song(nil), m.songs...)
	m.searchIdx = 1

	reply, _ := sendControl(t, m, control.Request{Kind: control.KindFrame})
	if reply.Err != "" {
		t.Fatalf("取帧不应失败: %s", reply.Err)
	}
	var state controlState
	if err := json.Unmarshal(reply.State, &state); err != nil {
		t.Fatalf("状态快照应能解析成 JSON: %v", err)
	}
	if state.View != "本地搜索" {
		t.Fatalf("搜索页视图名不符: %q", state.View)
	}
	if state.Selected == nil || state.Selected.Title != "夜航星" {
		t.Fatalf("应报告搜索焦点项: %+v", state.Selected)
	}
}

func TestControlRejectsUnknownKind(t *testing.T) {
	m := fixtureModel()
	m.keys = keymap.Default()

	reply, _ := sendControl(t, m, control.Request{Kind: control.Kind("bogus")})
	if reply.Err == "" {
		t.Fatal("未知请求类型应报错")
	}
}
