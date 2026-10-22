package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestEscOnLibraryKeepsLibraryWithoutAbout 验证曲库首页按 esc 只保持/返回曲库，
// 不再意外弹出关于弹窗（关于入口在页头 zero 与命令面板）。
// 「关于 popup 内容 / 关闭」的断言见 view_test.go 的 TestPaletteAboutOpensPopup。
func TestEscOnLibraryKeepsLibraryWithoutAbout(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 100, 30

	updated, _ := m.Update(keyPress("", tea.KeyEscape))
	mm := updated.(*Model)
	if mm.view != viewLibrary {
		t.Fatalf("曲库首页按 esc 应留在曲库，实际 %v", mm.view)
	}
	if mm.aboutPopup {
		t.Fatal("曲库首页按 esc 不应打开关于弹窗")
	}
	assertFrame(t, mm.View().Content, 100, 30)

	updated, _ = mm.Update(keyPress("", tea.KeyEscape))
	mm = updated.(*Model)
	if mm.aboutPopup || mm.view != viewLibrary {
		t.Fatalf("重复 esc 仍应是无操作：about=%v view=%v", mm.aboutPopup, mm.view)
	}
}

func TestEscOutsideLibraryReturnsHomeWithoutAbout(t *testing.T) {
	m := fixtureModel()
	m.view = viewQueue

	updated, _ := m.Update(keyPress("", tea.KeyEscape))
	mm := updated.(*Model)
	if mm.view != viewLibrary {
		t.Fatalf("esc 应返回曲库，实际 %v", mm.view)
	}
	if mm.aboutPopup {
		t.Fatal("非曲库视图按 esc 不应打开关于弹窗")
	}
}

func TestAboutPopupRendersNarrow(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 40, 12
	m.aboutPopup = true

	assertFrame(t, m.View().Content, 40, 12)
	body := ansi.Strip(m.View().Content)
	if !strings.Contains(body, "esc / enter 关闭") {
		t.Fatalf("窄屏关于弹窗缺少关闭提示：\n%s", body)
	}
}
