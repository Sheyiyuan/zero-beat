package main

import (
	"errors"
	"fmt"

	"github.com/sheyiyuan/zero-beat/internal/desktop"
)

// runDesktop 管理 Linux 应用菜单入口：菜单里能不能显示图标取决于这个入口。
func runDesktop(args []string) error {
	if len(args) == 0 {
		return errors.New("用法: zero desktop install|uninstall")
	}
	switch args[0] {
	case "install":
		entry, err := desktop.Install()
		if err != nil {
			return err
		}
		fmt.Printf("已写入应用菜单入口：%s\n", entry.DesktopFile)
		fmt.Printf("已安装图标：%s\n", entry.IconFile)
		fmt.Println("打开应用菜单（GNOME 按 Super 后搜索 zero-beat）即可启动。")
		return nil
	case "uninstall":
		removed, err := desktop.Uninstall()
		if err != nil {
			return err
		}
		if len(removed) == 0 {
			fmt.Println("没有找到已安装的应用菜单入口")
			return nil
		}
		for _, path := range removed {
			fmt.Printf("已删除 %s\n", path)
		}
		return nil
	default:
		return fmt.Errorf("未知 desktop 子命令 %q（可选 install / uninstall）", args[0])
	}
}
