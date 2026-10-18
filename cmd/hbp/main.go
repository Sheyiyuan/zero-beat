// Command hbp 是新项目的入口占位实现。
//
// 目前只提供 --version 与子命令骨架，真正的 TUI、音频引擎与音源实现
// 按 ROADMAP.md 的阶段推进。项目名尚未确定，见 docs/NAMING.md。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

const (
	appName = "hbp"
	version = "0.0.1-dev"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet(appName, flag.ContinueOnError)
	fs.Usage = func() { printUsage(fs) }
	showVersion := fs.Bool("version", false, "打印版本信息并退出")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVersion || (fs.NArg() == 1 && fs.Arg(0) == "version") {
		fmt.Printf("%s %s\n", appName, version)
		return nil
	}

	if fs.NArg() == 0 {
		printUsage(fs)
		return nil
	}

	switch cmd := fs.Arg(0); cmd {
	case "tui":
		return errors.New("TUI 尚未实现（ROADMAP 的 P3）")
	case "play":
		return errors.New("headless 播放尚未实现（P2 的第一个交付物）")
	case "login":
		return errors.New("登录尚未实现（ROADMAP 的 P1）")
	default:
		printUsage(fs)
		return fmt.Errorf("未知子命令 %q", cmd)
	}
}

func printUsage(fs *flag.FlagSet) {
	fmt.Fprintf(os.Stderr, `%s %s —— 跨平台音乐终端播放器（脚手架阶段）

用法:
  %s [--version] <command> [args]

命令:
  tui             启动终端界面（未实现）
  play <ref>      直接播放，如 BV 号或歌曲链接（未实现）
  login <source>  登录指定音源，如 bilibili / netease（未实现）

当前仓库只有骨架、接口与文档，可播放的能力还在路线图里：
见 ROADMAP.md 与 docs/design/。

`, appName, version, appName)
	fs.PrintDefaults()
}
