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

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/bili"
	"github.com/sheyiyuan/zero-beat/internal/config"
	"github.com/sheyiyuan/zero-beat/internal/control"
	"github.com/sheyiyuan/zero-beat/internal/instance"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/netease"
	"github.com/sheyiyuan/zero-beat/internal/source"
	bilibilisource "github.com/sheyiyuan/zero-beat/internal/source/bilibili"
	localsource "github.com/sheyiyuan/zero-beat/internal/source/local"
	"github.com/sheyiyuan/zero-beat/internal/store"
	syncpkg "github.com/sheyiyuan/zero-beat/internal/sync"
	"github.com/sheyiyuan/zero-beat/internal/tui"
)

// version 由发布流水线通过 -ldflags "-X main.version=..." 注入。
var version = "0.7.0"

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
	case "library":
		return runLibrary(fs.Args()[1:])
	case "lyrics":
		return runLyrics(fs.Args()[1:])
	case "clean-playlists":
		return runCleanPlaylists()
	case "keymap":
		return runKeymap(fs.Args()[1:])
	case "theme":
		return runTheme(fs.Args()[1:])
	case "desktop":
		return runDesktop(fs.Args()[1:])
	case "sync":
		return runSync(fs.Args()[1:])
	case "frame":
		return runFrame(fs.Args()[1:])
	default:
		if strings.HasPrefix(cmd, "+") {
			return runControl(fs.Args())
		}
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
  zero demo [view]      打印一帧示例界面（library/search/queue/lyrics/lyricbind/help/login/clip/picker/menu/about/downloads/selection/sync/conflicts）
  zero download <目标...>  下载单曲（支持 -j 并发、Range 续传）
  zero library <子命令>  本地文件夹歌单：add/list/scan/remove
  zero lyrics <子命令>   歌词来源：show/bind/unbind
  zero clean-playlists  清空已导入的远端歌单，保留本地歌单
  zero theme [auto|dark|light]  查看/设置深色/浅色模式
  zero theme palette zero-red|hanpai-blue  选择配色（佐洛红 / 半拍蓝）
  zero theme --init|--check     生成/校验自定义颜色 theme.toml
  zero desktop install  注册 Linux 应用菜单入口（含图标，卸载用 uninstall）
  zero keymap --check   校验 ~/.config/zero-beat/keymap.toml
  zero keymap --init    生成默认快捷键配置
  zero sync <子命令>    歌单/键位/加密登录态的 git 同步（zero sync 查看用法）
  zero frame [选项]     读取运行中实例的当前帧（--ansi / --json / --delay / --stable）
  zero +<按键>          向运行中实例注入按键，例如 zero +space、zero +ctrl+a
  zero +type <文本>     向当前输入态灌入文本
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

// loadLyricsConfig 读取本地歌词配置；配置问题只提示，不影响启动。
func loadLyricsConfig() lyrics.LocalConfig {
	path, err := config.LyricsPath()
	if err != nil {
		return lyrics.DefaultLocalConfig()
	}
	cfg, warnings := lyrics.LoadLocalConfig(path)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "提示："+w)
	}
	return cfg
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
	lyricsCfg := loadLyricsConfig()
	list := []source.Source{
		bilibilisource.New(bili.New()),
		netease.New(),
		// 本地源放在最后：它没有 CapAccount/CapSearch，不会出现在登录页与搜索范围内。
		localsource.New(st, lyricsCfg),
	}
	for _, src := range list {
		loadSession(st, src)
	}
	return source.NewRegistry(list...)
}

func runTUI() error {
	dir, err := config.DataDir()
	if err != nil {
		return err
	}
	lock, err := instance.Acquire(dir)
	if err != nil {
		return err
	}
	defer lock.Release()

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

	var manager *syncpkg.Manager
	if opts, err := syncpkg.DefaultOptions(st, app.LibraryRevision); err == nil {
		manager = syncpkg.NewManager(syncpkg.NewClient(opts), syncpkg.ManagerOptions{})
	}
	model := tui.New(newSources(st), st, engine, manager)
	p := tea.NewProgram(model)
	// 控制通道是开发期辅助能力，起不来不影响播放。
	if srv, err := control.Listen(dir, p.Send); err == nil {
		defer srv.Close()
		go srv.Serve()
	}
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
	_, warnings, err := keymap.LoadWithWarnings(path)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Println("提示：" + w)
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

	path, err := config.ThemePath()
	if err != nil {
		return err
	}

	initFile, force, check := false, false, false
	var pos []string
	for _, a := range args {
		switch a {
		case "--init", "init":
			initFile = true
		case "--check", "check":
			check = true
		case "--force", "-f":
			force = true
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("未知 theme 参数 %q", a)
			}
			pos = append(pos, a)
		}
	}
	if initFile && check {
		return errors.New("--init 与 --check 不能同时使用")
	}

	switch {
	case initFile:
		if _, err := os.Stat(path); err == nil && !force {
			return fmt.Errorf("%s 已存在；加 --force 覆盖", path)
		}
		if err := os.WriteFile(path, []byte(tui.ExampleThemeTOML()), 0o600); err != nil {
			return fmt.Errorf("写入主题配置: %w", err)
		}
		fmt.Println("已生成主题配置：" + path)
		fmt.Println("取消注释想覆盖的颜色，重启终端界面生效。")
		return nil
	case check:
		n, err := tui.CheckThemeFile(path)
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println("没有 theme.toml，使用内置配色。")
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Printf("theme.toml 有效，覆盖 %d 项颜色。\n", n)
		return nil
	case len(pos) == 0:
		printThemeSummary(st, path)
		return nil
	}

	if pos[0] == "palette" {
		if len(pos) == 1 {
			name := st.Setting(tui.ThemePaletteKey, tui.PaletteZeroRed)
			fmt.Printf("当前配色：%s（%s）\n", name, tui.PaletteLabel(name))
			fmt.Println("可选：")
			fmt.Printf("  %-12s %s（默认）\n", tui.PaletteZeroRed, tui.PaletteLabel(tui.PaletteZeroRed))
			fmt.Printf("  %-12s %s\n", tui.PaletteHanpaiBlue, tui.PaletteLabel(tui.PaletteHanpaiBlue))
			return nil
		}
		if len(pos) > 2 {
			return errors.New("用法: zero theme palette zero-red|hanpai-blue")
		}
		if !tui.ValidPalette(pos[1]) {
			return fmt.Errorf("未知配色 %q；可选 zero-red（佐洛红）/ hanpai-blue（半拍蓝）", pos[1])
		}
		name := tui.CanonicalPalette(pos[1])
		if err := st.SetSetting(tui.ThemePaletteKey, name); err != nil {
			return err
		}
		fmt.Printf("配色已设为 %s（%s），重启终端界面生效。\n", name, tui.PaletteLabel(name))
		return nil
	}

	if len(pos) == 1 {
		v := strings.ToLower(strings.TrimSpace(pos[0]))
		switch v {
		case "auto", "dark", "light":
		default:
			return fmt.Errorf("未知主题模式 %q（可选 auto / dark / light）；设置配色用 zero theme palette zero-red|hanpai-blue", pos[0])
		}
		if err := st.SetSetting(tui.ThemeModeKey, v); err != nil {
			return err
		}
		fmt.Printf("主题模式已设为 %s，重启终端界面生效。\n", v)
		return nil
	}
	return errors.New("用法: zero theme [auto|dark|light] | palette zero-red|hanpai-blue | --init | --check")
}

// printThemeSummary 展示当前设置、自定义文件状态与常用命令。
func printThemeSummary(st *store.Store, path string) {
	mode := st.Setting(tui.ThemeModeKey, "auto")
	name := st.Setting(tui.ThemePaletteKey, tui.PaletteZeroRed)
	fmt.Printf("主题模式：%s（auto / dark / light）\n", mode)
	fmt.Printf("配色：%s（%s）\n", name, tui.PaletteLabel(name))
	if v := strings.TrimSpace(os.Getenv("ZERO_THEME")); v != "" {
		fmt.Printf("环境覆盖：ZERO_THEME=%s\n", v)
	}
	if v := strings.TrimSpace(os.Getenv("ZERO_THEME_PALETTE")); v != "" {
		fmt.Printf("环境覆盖：ZERO_THEME_PALETTE=%s\n", v)
	}
	if _, err := os.Stat(path); err == nil {
		if n, err := tui.CheckThemeFile(path); err != nil {
			fmt.Println("自定义颜色：" + path + "（无效：" + err.Error() + "）")
		} else {
			fmt.Printf("自定义颜色：%s（覆盖 %d 项）\n", path, n)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		fmt.Println("自定义颜色：未创建（zero theme --init 生成）")
	} else {
		fmt.Println("自定义颜色：读取失败：" + err.Error())
	}
	fmt.Println("设置模式：zero theme auto|dark|light")
	fmt.Println("设置配色：zero theme palette zero-red|hanpai-blue")
	fmt.Println("生成 / 校验：zero theme --init | zero theme --check")
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
		// 只有需要登录的音源才出现在命令行登录入口；本地源没有登录流程。
		if all := sources.AccountSources(); len(all) > 0 {
			src = all[0]
		}
	}
	if src == nil || !src.Caps().Has(source.CapAccount) {
		ids := make([]string, 0)
		for _, id := range sources.AccountIDs() {
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
