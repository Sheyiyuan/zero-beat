//go:build linux

package desktop

import (
	"os"
	"strings"
	"testing"
)

func TestQuoteExec(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"简单路径", "/home/syy/.local/bin/zero", "/home/syy/.local/bin/zero"},
		{"空格", "/opt/zero beat/zero", `"/opt/zero beat/zero"`},
		{"双引号", `/tmp/a"b`, `"/tmp/a\"b"`},
		{"反斜杠", `/tmp/a\b`, `"/tmp/a\\b"`},
		{"美元符号", "/tmp/$x", `"/tmp/\$x"`},
		{"百分号", "/tmp/100%/zero", "/tmp/100%%/zero"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := quoteExec(c.path); got != c.want {
				t.Fatalf("quoteExec(%q) = %q，应为 %q", c.path, got, c.want)
			}
		})
	}
}

func TestDesktopContent(t *testing.T) {
	got := desktopContent("/opt/zero beat/zero")
	for _, want := range []string{
		"[Desktop Entry]\n",
		"Exec=\"/opt/zero beat/zero\"\n",
		"Icon=zero-beat\n",
		"Terminal=true\n",
		"Name[zh_CN]=零拍\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("入口内容缺少 %q：\n%s", want, got)
		}
	}
}

func TestInstallUninstallRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", home)

	entry, err := Install()
	if err != nil {
		t.Fatalf("安装应成功: %v", err)
	}
	if !strings.HasPrefix(entry.DesktopFile, home) {
		t.Fatalf("入口应写进 XDG_DATA_HOME，实际 %s", entry.DesktopFile)
	}
	if _, err := os.Stat(entry.DesktopFile); err != nil {
		t.Fatalf("入口文件应存在: %v", err)
	}
	if _, err := os.Stat(entry.IconFile); err != nil {
		t.Fatalf("图标文件应存在: %v", err)
	}

	removed, err := Uninstall()
	if err != nil {
		t.Fatalf("卸载应成功: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("应删除入口与图标两个文件，实际 %v", removed)
	}
	if _, err := os.Stat(entry.DesktopFile); !os.IsNotExist(err) {
		t.Fatalf("卸载后入口应不存在: %v", err)
	}
}
