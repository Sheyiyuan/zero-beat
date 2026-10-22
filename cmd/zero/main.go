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
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/skip2/go-qrcode"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/bili"
	"github.com/sheyiyuan/zero-beat/internal/config"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/netease"
	"github.com/sheyiyuan/zero-beat/internal/source"
	bilibilisource "github.com/sheyiyuan/zero-beat/internal/source/bilibili"
	"github.com/sheyiyuan/zero-beat/internal/store"
	"github.com/sheyiyuan/zero-beat/internal/tui"
)

// version 由发布流水线通过 -ldflags "-X main.version=..." 注入。
var version = "0.4.1"

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
	tui.Version = version
	if *showVersion || (fs.NArg() == 1 && fs.Arg(0) == "version") {
		fmt.Printf("zero %s\n", version)
		return nil
	}

	switch cmd := fs.Arg(0); cmd {
	case "", "tui":
		return runTUI()
	case "play":
		if fs.NArg() < 2 {
			return errors.New("用法: zero play <音源链接或 ID>")
		}
		return runPlay(fs.Arg(1))
	case "login":
		name := ""
		if fs.NArg() >= 2 {
			name = fs.Arg(1)
		}
		return runLogin(name)
	case "demo":
		name := ""
		if fs.NArg() >= 2 {
			name = fs.Arg(1)
		}
		fmt.Println(tui.RenderDemo(name, 100, 30))
		return nil
	case "download":
		return runDownload(fs.Args()[1:])
	case "clean-playlists":
		return runCleanPlaylists()
	case "keymap":
		return runKeymap(fs.Args()[1:])
	case "theme":
		return runTheme(fs.Args()[1:])
	default:
		usage(fs)
		return fmt.Errorf("未知子命令 %q", cmd)
	}
}

func usage(fs *flag.FlagSet) {
	fmt.Fprintf(os.Stderr, `zero %s —— 跨平台音乐终端播放器

用法:
  zero                  启动终端界面
  zero play <链接或 ID>  直接播放（headless，便于验证音频链路）
  zero login [音源]     在终端里扫码登录并保存 cookie（默认第一个音源）
  zero demo [view]      打印一帧示例界面（library/search/queue/lyrics/help/login/clip/picker）
  zero download <目标...>  下载单曲（支持 -j 并发、Range 续传）
  zero clean-playlists  清空已导入的远端歌单，保留本地歌单
  zero theme [auto|dark|light]  查看/设置主题（auto 读 COLORFGBG）
  zero keymap --check   校验 ~/.config/zero-beat/keymap.toml
  zero keymap --init    生成默认快捷键配置
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

func loadSession(st *store.Store, src source.Source) {
	payload, err := st.LoadAccount(string(src.ID()))
	if err != nil {
		return
	}
	var cookies map[string]string
	if err := json.Unmarshal(payload, &cookies); err == nil {
		src.SetSession(cookies)
	}
}

func newSources(st *store.Store) *source.Registry {
	// 组合根：新增平台只需要在这里追加构造器，其余层通过 Registry 工作。
	list := []source.Source{
		bilibilisource.New(bili.New()),
		netease.New(),
	}
	for _, src := range list {
		loadSession(st, src)
	}
	return source.NewRegistry(list...)
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

	model := tui.New(newSources(st), st, engine)
	p := tea.NewProgram(model)
	_, err = p.Run()
	return err
}

func runKeymap(args []string) error {
	path, err := config.KeymapPath()
	if err != nil {
		return err
	}
	initFile, force := false, false
	for _, a := range args {
		switch a {
		case "--init", "init":
			initFile = true
		case "--force", "-f":
			force = true
		case "--check", "check":
		default:
			return fmt.Errorf("未知 keymap 参数 %q", a)
		}
	}
	if initFile {
		if _, err := os.Stat(path); err == nil && !force {
			return fmt.Errorf("%s 已存在；加 --force 覆盖", path)
		}
		if err := os.WriteFile(path, []byte(keymap.ExampleTOML()), 0o600); err != nil {
			return fmt.Errorf("写入快捷键配置: %w", err)
		}
		fmt.Println("已生成快捷键配置：" + path)
		return nil
	}
	if _, err := keymap.Load(path); err != nil {
		return err
	}
	fmt.Println("快捷键配置 OK：" + path)
	return nil
}

func runCleanPlaylists() error {
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.DeleteRemotePlaylists(); err != nil {
		return err
	}
	fmt.Println("已清空已导入的远端歌单；本地歌单保留。回到曲库按 f 重新导入。")
	return nil
}

func runTheme(args []string) error {
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	if len(args) == 0 {
		fmt.Printf("当前主题设置：%s\n", st.Setting("theme", "auto"))
		fmt.Println("可用值：auto / dark / light；auto 读取 COLORFGBG，识别不了时用 dark。")
		fmt.Println("临时覆盖：ZERO_THEME=light|dark|auto zero")
		return nil
	}

	v := strings.ToLower(strings.TrimSpace(args[0]))
	switch v {
	case "auto", "dark", "light":
	default:
		return errors.New("用法: zero theme [auto|dark|light]")
	}
	if err := st.SetSetting("theme", v); err != nil {
		return err
	}
	fmt.Printf("主题已设为 %s，重启终端界面生效。\n", v)
	return nil
}

// runPlay 是最小可用的 headless 播放器，用来验证解析 + 解码 + 输出链路。
func runPlay(input string) error {
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	sources := newSources(st)
	ref, matched, err := sources.ParseTarget(input, 0)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("无法识别播放目标 %q", input)
	}

	engine, err := audio.New()
	if err != nil {
		return err
	}
	defer engine.Close()

	ctx := context.Background()
	src := sources.Get(ref.Source)
	if src == nil {
		return fmt.Errorf("未知音源 %q", ref.Source)
	}
	track, stream, err := resolveDownload(ctx, src, ref, source.QualityAuto)
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
		DurationMS: stream.Duration.Milliseconds(),
		ExpiresAt:  stream.ExpiresAt,
		Quality:    stream.Quality,
	}); err != nil {
		return err
	}

	fmt.Printf("▶ %s — %s\n", track.Title, track.Artist)
	fmt.Printf("  音轨 %s（%s）· %s\n", stream.Quality, stream.Codec, src.Name())

	total := stream.Duration
	if total == 0 {
		total = time.Duration(track.Duration) * time.Second
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

func runLogin(name string) error {
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	sources := newSources(st)
	src := sources.GetByName(name)
	if src == nil && strings.TrimSpace(name) == "" {
		if all := sources.All(); len(all) > 0 {
			src = all[0]
		}
	}
	if src == nil {
		ids := make([]string, 0, len(sources.IDs()))
		for _, id := range sources.IDs() {
			ids = append(ids, string(id))
		}
		return fmt.Errorf("未知音源 %q（可选 %s）", name, strings.Join(ids, " / "))
	}
	ctx := context.Background()

	qr, err := src.StartQRLogin(ctx)
	if err != nil {
		return err
	}
	code, err := qrcode.New(qr.URL, qrcode.Medium)
	if err != nil {
		return err
	}
	fmt.Printf("用 %s 扫码登录：\n", src.Name())
	fmt.Println(code.ToSmallString(false))

	for {
		status, err := src.PollQRLogin(ctx, qr.Key)
		if err != nil {
			return err
		}
		fmt.Printf("\r状态：%s        ", status.Message)
		if status.LoggedIn {
			payload, err := json.Marshal(src.Session())
			if err != nil {
				return err
			}
			if err := st.SaveAccount(string(src.ID()), payload); err != nil {
				return err
			}
			fmt.Println("\n登录成功，cookie 已保存到本地")
			return nil
		}
		if status.Expired {
			return fmt.Errorf("二维码已失效，请重新运行 zero login %s", src.ID())
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
