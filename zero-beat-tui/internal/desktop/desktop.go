// Package desktop 把 zero-beat 注册进 Linux 桌面应用菜单。
//
// TUI 自己在终端里运行，无法设置窗口图标；系统菜单里的图标来自 .desktop
// 入口与 hicolor 图标目录。Install 写入这两个文件，Uninstall 删除它们。
package desktop

import "errors"

// ErrUnsupported 表示当前平台没有应用菜单注册能力。
var ErrUnsupported = errors.New("desktop: 当前平台不支持应用菜单注册（仅 Linux）")

// Entry 记录一次安装写入的文件位置。
type Entry struct {
	DesktopFile string
	IconFile    string
}
