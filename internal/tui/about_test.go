package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestEscOnLibraryOpensAboutPopup(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 100, 30

	updated, _ := m.Update(keyPress("", tea.KeyEscape))
	mm := updated.(*Model)
	if !mm.aboutPopup {
		t.Fatal("曲库首页按 esc 应打开关于弹窗")
	}
	body := ansi.Strip(mm.View().Content)
	for _, want := range []string{"关于", "版本", "AGPL-3.0", "github.com/Sheyiyuan/zero-beat"} {
		if !strings.Contains(body, want) {
			t.Fatalf("关于弹窗缺少 %q：\n%s", want, body)
		}
	}
	assertFrame(t, mm.View().Content, 100, 30)

	updated, _ = mm.Update(keyPress("", tea.KeyEscape))
	mm = updated.(*Model)
	if mm.aboutPopup {
		t.Fatal("再次按 esc 应关闭关于弹窗")
	}
	if mm.view != viewLibrary {
		t.Fatalf("关闭关于弹窗后应留在曲库，实际 %v", mm.view)
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
