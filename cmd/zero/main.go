// Command zero 是 zero-beat 的入口。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/skip2/go-qrcode"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/bili"
	"github.com/sheyiyuan/zero-beat/internal/config"
	"github.com/sheyiyuan/zero-beat/internal/store"
	"github.com/sheyiyuan/zero-beat/internal/tui"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "zero: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("zero", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	showVersion := fs.Bool("version", false, "打印版本并退出")
	fs.Usage = func() { usage(fs) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion || (fs.NArg() == 1 && fs.Arg(0) == "version") {
		fmt.Printf("zero %s\n", version)
		return nil
	}

	switch cmd := fs.Arg(0); cmd {
	case "", "tui":
		return runTUI()
	case "play":
		if fs.NArg() < 2 {
			return errors.New("用法: zero play <BV号或视频链接>")
		}
		return runPlay(fs.Arg(1))
	case "login":
		return runLogin()
	case "demo":
		name := ""
		if fs.NArg() >= 2 {
			name = fs.Arg(1)
		}
		fmt.Println(tui.RenderDemo(name, 100, 30))
		return nil
	default:
		usage(fs)
		return fmt.Errorf("未知子命令 %q", cmd)
	}
}

func usage(fs *flag.FlagSet) {
	fmt.Fprintf(os.Stderr, `zero %s —— 跨平台音乐终端播放器（B 站）

用法:
  zero                  启动终端界面
  zero play <BV号/链接>  直接播放（headless，便于验证音频链路）
  zero login            在终端里扫码登录并保存 cookie
  zero demo [view]      打印一帧示例界面（library/search/queue/lyrics/help）
  zero --version        打印版本

数据目录: ~/.config/zero-beat/（数据库与登录态）
依赖: ffmpeg（解码）、PulseAudio/CoreAudio/WASAPI（输出）

`, version)
	fs.PrintDefaults()
}

func openStore() (*store.Store, error) {
	path, err := config.DatabasePath()
	if err != nil {
		return nil, err
	}
	return store.Open(path)
}

func newClient(st *store.Store) *bili.Client {
	client := bili.New()
	if payload, err := st.LoadAccount("bilibili"); err == nil {
		var cookies map[string]string
		if err := json.Unmarshal(payload, &cookies); err == nil {
			client.SetCookies(cookies)
		}
	}
	return client
}

func runTUI() error {
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	engine, err := audio.New()
	if err != nil {
		return err
	}
	defer engine.Close()

	model := tui.New(newClient(st), st, engine)
	p := tea.NewProgram(model)
	_, err = p.Run()
	return err
}

// runPlay 是最小可用的 headless 播放器，用来验证解析 + 解码 + 输出链路。
func runPlay(input string) error {
	bvid, err := bili.ParseBVID(input)
	if err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	engine, err := audio.New()
	if err != nil {
		return err
	}
	defer engine.Close()

	ctx := context.Background()
	client := newClient(st)

	info, err := client.VideoInfo(ctx, bvid)
	if err != nil {
		return err
	}
	page := info.Pages[0]
	stream, err := client.PlayURL(ctx, bvid, page.CID, bili.QualityAuto)
	if err != nil {
		return err
	}

	failures := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		for ev := range engine.Events() {
			switch ev.Type {
			case audio.EventEnded:
				close(done)
				return
			case audio.EventFailed:
				if ev.Err != nil {
					failures <- ev.Err
				}
				return
			}
		}
	}()

	if err := engine.Play(ctx, audio.Stream{
		URL:        stream.URL,
		Headers:    stream.Headers,
		Codec:      stream.Codec,
		DurationMS: stream.DurationMS,
		ExpiresAt:  stream.ExpiresAt,
		Quality:    stream.Quality,
	}); err != nil {
		return err
	}

	fmt.Printf("▶ %s — %s\n", info.Title, info.Owner)
	fmt.Printf("  音轨 %s（%s）· %s\n", stream.Quality, stream.Codec, page.Part)

	total := time.Duration(stream.DurationMS) * time.Millisecond
	if total == 0 {
		total = time.Duration(page.Duration) * time.Second
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sig:
			fmt.Println("\n已停止")
			return nil
		case <-done:
			fmt.Println("\n播放结束")
			return nil
		case err := <-failures:
			return err
		case <-ticker.C:
			fmt.Printf("\r%s / %s", formatDur(engine.Position()), formatDur(total))
		}
	}
}

func runLogin() error {
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	client := newClient(st)
	ctx := context.Background()

	qr, err := client.StartQRLogin(ctx)
	if err != nil {
		return err
	}
	code, err := qrcode.New(qr.URL, qrcode.Medium)
	if err != nil {
		return err
	}
	fmt.Println("用 B 站 App 扫码登录：")
	fmt.Println(code.ToSmallString(false))

	for {
		status, err := client.PollQRLogin(ctx, qr.Key)
		if err != nil {
			return err
		}
		fmt.Printf("\r状态：%s        ", status.Message)
		if status.LoggedIn {
			payload, err := json.Marshal(client.Cookies())
			if err != nil {
				return err
			}
			if err := st.SaveAccount("bilibili", payload); err != nil {
				return err
			}
			fmt.Println("\n登录成功，cookie 已保存到本地")
			return nil
		}
		if status.Code == 86038 {
			return errors.New("二维码已失效，请重新运行 zero login")
		}
		time.Sleep(2 * time.Second)
	}
}

func formatDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Round(time.Second) / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
