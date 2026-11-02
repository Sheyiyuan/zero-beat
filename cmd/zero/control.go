package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sheyiyuan/zero-beat/internal/config"
	"github.com/sheyiyuan/zero-beat/internal/control"
)

const controlUsage = `用法:
  zero frame [--ansi] [--json] [--delay 200ms] [--stable 2]  读取运行中实例的当前帧
  zero +<按键> [--force] [--json]                            向实例注入按键（键名同 keymap.toml）
  zero +type <文本>                                          向当前输入态灌入文本

控制通道只在本机数据目录生效；没有运行中的实例时会直接报错。
--force 允许执行删除、同步这类默认被拒的按键。`

// printControlReply 输出控制通道的回应：默认是去掉颜色后的纯文本帧。
func printControlReply(reply control.Reply, wantJSON, wantANSI bool) error {
	if wantJSON {
		frame := plainFrame(reply.Frame)
		if wantANSI {
			frame = reply.Frame
		}
		payload := struct {
			State json.RawMessage `json:"state"`
			Frame string          `json:"frame"`
		}{State: reply.State, Frame: frame}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("control: 编码 JSON: %w", err)
		}
		_, err = fmt.Fprintln(os.Stdout, string(data))
		return err
	}
	if wantANSI {
		_, err := fmt.Fprintln(os.Stdout, reply.Frame)
		return err
	}
	_, err := fmt.Fprintln(os.Stdout, plainFrame(reply.Frame))
	return err
}

// plainFrame 去掉 ANSI 样式并裁掉行尾空白，便于人和 agent 阅读。
func plainFrame(frame string) string {
	lines := strings.Split(ansi.Strip(frame), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func runFrame(args []string) error {
	fs := flag.NewFlagSet("zero frame", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	wantANSI := fs.Bool("ansi", false, "保留 ANSI 颜色输出原始帧")
	wantJSON := fs.Bool("json", false, "输出 JSON（含状态快照）")
	delay := fs.Duration("delay", 0, "取帧前的等待，例如 200ms")
	stable := fs.Int("stable", 1, "连续 N 帧不变才算稳定")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir, err := config.DataDir()
	if err != nil {
		return err
	}
	if *delay > 0 {
		time.Sleep(*delay)
	}

	need := *stable
	if need < 1 {
		need = 1
	}
	var reply control.Reply
	same := 0
	var last string
	deadline := time.Now().Add(5 * time.Second)
	for {
		reply, err = callControl(dir, control.Request{Kind: control.KindFrame})
		if err != nil {
			return err
		}
		if reply.Frame == last {
			same++
		} else {
			same = 0
			last = reply.Frame
		}
		if same >= need-1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return printControlReply(reply, *wantJSON, *wantANSI)
}

func runControl(args []string) error {
	if len(args) == 0 {
		return errors.New(controlUsage)
	}
	name := strings.TrimPrefix(args[0], "+")
	if name == "" {
		return errors.New(controlUsage)
	}
	dir, err := config.DataDir()
	if err != nil {
		return err
	}

	if name == "type" {
		wantJSON := false
		var parts []string
		for _, a := range args[1:] {
			if a == "--json" {
				wantJSON = true
				continue
			}
			parts = append(parts, a)
		}
		text := strings.Join(parts, " ")
		if text == "" {
			return errors.New("用法: zero +type <文本>")
		}
		reply, err := callControl(dir, control.Request{Kind: control.KindType, Text: text})
		if err != nil {
			return err
		}
		return printControlReply(reply, wantJSON, false)
	}

	fs := flag.NewFlagSet("zero +"+name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	force := fs.Bool("force", false, "允许执行删除、同步等有数据风险的按键")
	wantJSON := fs.Bool("json", false, "输出 JSON（含状态快照）")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("多余参数 %q", fs.Arg(0))
	}

	reply, err := callControl(dir, control.Request{Kind: control.KindKey, Key: name, Force: *force})
	if err != nil {
		return err
	}
	return printControlReply(reply, *wantJSON, false)
}

// callControl 把控制通道的错误统一成普通 error。
func callControl(dir string, req control.Request) (control.Reply, error) {
	reply, err := control.Call(dir, req, control.DefaultTimeout)
	if err != nil {
		return reply, err
	}
	if reply.Err != "" {
		return reply, errors.New(reply.Err)
	}
	return reply, nil
}
