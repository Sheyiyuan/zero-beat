//go:build linux

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sheyiyuan/zero-beat/assets/icons"
)

// appID 同时用作 .desktop 文件名与图标主题里的图标名。
const appID = "zero-beat"

// Install 写入应用菜单入口和图标，返回写入的文件位置。
//
// Exec 指向当前正在运行的可执行文件；Terminal=true 让桌面环境自己拉起终端，
// 避免依赖某个具体的终端模拟器。
func Install() (Entry, error) {
	execPath, err := os.Executable()
	if err != nil {
		return Entry{}, fmt.Errorf("desktop: 定位可执行文件: %w", err)
	}
	if real, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = real
	}

	appsDir := filepath.Join(dataHome(), "applications")
	iconDir := filepath.Join(dataHome(), "icons", "hicolor", "256x256", "apps")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return Entry{}, fmt.Errorf("desktop: 创建应用目录: %w", err)
	}
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		return Entry{}, fmt.Errorf("desktop: 创建图标目录: %w", err)
	}

	iconPath := filepath.Join(iconDir, appID+".png")
	if err := os.WriteFile(iconPath, icons.AppIconPNG, 0o644); err != nil {
		return Entry{}, fmt.Errorf("desktop: 写入图标: %w", err)
	}
	desktopPath := filepath.Join(appsDir, appID+".desktop")
	if err := os.WriteFile(desktopPath, []byte(desktopContent(execPath)), 0o644); err != nil {
		return Entry{}, fmt.Errorf("desktop: 写入入口: %w", err)
	}

	refreshCaches()
	return Entry{DesktopFile: desktopPath, IconFile: iconPath}, nil
}

// Uninstall 删除应用菜单入口与图标，返回实际删除的文件位置。
func Uninstall() ([]string, error) {
	var removed []string
	for _, path := range []string{desktopFile(), iconFile()} {
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, fmt.Errorf("desktop: 删除 %s: %w", path, err)
		}
		removed = append(removed, path)
	}
	if len(removed) > 0 {
		refreshCaches()
	}
	return removed, nil
}

func desktopFile() string {
	return filepath.Join(dataHome(), "applications", appID+".desktop")
}

func iconFile() string {
	return filepath.Join(dataHome(), "icons", "hicolor", "256x256", "apps", appID+".png")
}

func desktopContent(execPath string) string {
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Version=1.0\n")
	b.WriteString("Name=" + appID + "\n")
	b.WriteString("Name[zh_CN]=零拍\n")
	b.WriteString("GenericName=Music Player\n")
	b.WriteString("GenericName[zh_CN]=终端音乐播放器\n")
	b.WriteString("Comment=Play Bilibili and NetEase Cloud Music in a terminal\n")
	b.WriteString("Comment[zh_CN]=在终端里播放 B 站与网易云音乐\n")
	fmt.Fprintf(&b, "Exec=%s\n", quoteExec(execPath))
	b.WriteString("Icon=" + appID + "\n")
	b.WriteString("Terminal=true\n")
	b.WriteString("Categories=AudioVideo;Audio;Player;\n")
	b.WriteString("Keywords=music;bilibili;netease;tui;player;\n")
	b.WriteString("StartupNotify=false\n")
	return b.String()
}

// quoteExec 按 Desktop Entry 规范给含保留字符的路径加双引号并转义。
func quoteExec(path string) string {
	// % 是 Exec 的字段码前缀，即使在引号内也要写成 %%。
	path = strings.ReplaceAll(path, "%", "%%")
	if !strings.ContainsAny(path, " \t\n\"'\\><~|&;$*?()`") {
		return path
	}
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", "\\$")
	return `"` + replacer.Replace(path) + `"`
}

func dataHome() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".local", "share")
	}
	return filepath.Join(home, ".local", "share")
}

// refreshCaches 刷新菜单与图标缓存；命令缺失或失败都不影响入口本身可用。
func refreshCaches() {
	if path, err := exec.LookPath("update-desktop-database"); err == nil {
		_ = exec.Command(path, filepath.Join(dataHome(), "applications")).Run()
	}
	hicolor := filepath.Join(dataHome(), "icons", "hicolor")
	if _, err := os.Stat(hicolor); err != nil {
		return
	}
	if path, err := exec.LookPath("gtk-update-icon-cache"); err == nil {
		_ = exec.Command(path, "-f", "-t", hicolor).Run()
	}
}
