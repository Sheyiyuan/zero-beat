package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/store"
	syncpkg "github.com/sheyiyuan/zero-beat/internal/sync"
)

var stdinReader = bufio.NewReader(os.Stdin)

// readNewPassphrase 用于新建仓库：校验长度并二次确认，失败时就地重来，
// 避免用户输入半天后在 Init 里才看到「口令太短」。
func readNewPassphrase() (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		pass, err := readPassphrase("同步口令: ")
		if err != nil {
			return "", err
		}
		if len([]rune(pass)) < syncpkg.MinPassphraseRunes {
			fmt.Fprintf(os.Stderr, "口令至少需要 %d 个字符，请重新输入。\n", syncpkg.MinPassphraseRunes)
			continue
		}
		confirm, err := readPassphrase("再次输入口令: ")
		if err != nil {
			return "", err
		}
		if pass != confirm {
			fmt.Fprintln(os.Stderr, "两次输入的口令不一致，请重新输入。")
			continue
		}
		return pass, nil
	}
	return "", fmt.Errorf("口令输入失败次数过多")
}

// readPassphrase 从 stdin 读口令：终端下不回显，管道下读一行（便于自动化）。
func readPassphrase(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		data, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	line, err := stdinReader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// openSync 打开数据库并构造同步客户端；调用方负责 Close 返回的 store。
func openSync() (*syncpkg.Client, *store.Store, error) {
	st, err := openStore()
	if err != nil {
		return nil, nil, err
	}
	opts, err := syncpkg.DefaultOptions(st, app.LibraryRevision)
	if err != nil {
		_ = st.Close()
		return nil, nil, err
	}
	return syncpkg.NewClient(opts), st, nil
}

func runSync(args []string) error {
	if len(args) == 0 {
		syncUsage()
		return fmt.Errorf("缺少 sync 子命令")
	}
	switch args[0] {
	case "init":
		return runSyncInit(args[1:])
	case "status":
		return runSyncStatus()
	case "pull", "push":
		return runSyncOnce(args[0])
	case "conflicts":
		return runSyncConflicts()
	case "resolve":
		return runSyncResolve(args[1:])
	case "key":
		return runSyncKey(args[1:])
	case "archive":
		return runSyncArchive()
	default:
		syncUsage()
		return fmt.Errorf("未知 sync 子命令 %q", args[0])
	}
}

func syncUsage() {
	fmt.Fprint(os.Stderr, `zero sync —— 歌单、快捷键与加密登录态的 git 同步

用法:
  zero sync init --remote <url> [--branch main] [--auto=false] [--repo <path>]
  zero sync status
  zero sync pull | push
  zero sync conflicts
  zero sync resolve <tx> [<entity>] --keep local|remote|both
  zero sync resolve <tx> --all local|remote
  zero sync resolve <tx> --discard
  zero sync key rotate|set|forget
  zero sync archive

口令从 stdin 读取（终端下不回显），不使用命令行参数。仓库需要自己创建。
`)
}

func runSyncInit(args []string) error {
	fs := flag.NewFlagSet("sync init", flag.ContinueOnError)
	remote := fs.String("remote", "", "仓库 URL（HTTPS 或 SSH）")
	branch := fs.String("branch", "main", "分支名")
	auto := fs.Bool("auto", true, "自动同步（启动拉取、变更后推送）")
	repo := fs.String("repo", "", "本地仓库路径（测试用，等价于 --remote）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	url := strings.TrimSpace(*remote)
	if *repo != "" {
		url = *repo
	}
	if url == "" {
		return fmt.Errorf("需要 --remote <url> 或 --repo <path>")
	}
	client, st, err := openSync()
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	if err := client.Configure(url, *branch, *auto); err != nil {
		return err
	}
	hasMeta, err := client.Probe(ctx)
	if err != nil {
		return err
	}
	if hasMeta {
		fmt.Fprintln(os.Stderr, "检测到远端已有同步数据，将加入现有仓库。")
	}
	var pass string
	if hasMeta {
		pass, err = readPassphrase("同步口令: ")
		if err != nil {
			return err
		}
	} else {
		pass, err = readNewPassphrase()
		if err != nil {
			return err
		}
	}
	out, err := client.Init(ctx, url, *branch, *auto, pass)
	if err != nil {
		return err
	}
	if out.Joined {
		fmt.Println("已加入现有同步仓库。")
	} else {
		fmt.Println("已创建同步仓库。")
	}
	if out.ReusedKey {
		fmt.Println("已复用本机已有密钥（口令未更改）。")
	}
	printSyncResult(out.Synced)
	return nil
}

func printSyncResult(res syncpkg.Result) {
	fmt.Printf("歌单 %d 个，注释 %d 个，登录态 %d 个", res.Playlists, res.Annotations, res.Accounts)
	if res.Committed {
		fmt.Print("，已提交")
	}
	if res.Pushed {
		fmt.Print("，已推送")
	}
	fmt.Println()
	if len(res.Deleted) > 0 {
		fmt.Printf("已删除本地歌单 %d 个（已归档）\n", len(res.Deleted))
	}
	for _, warning := range res.Warnings {
		fmt.Fprintln(os.Stderr, "提示："+warning)
	}
}

func runSyncStatus() error {
	client, st, err := openSync()
	if err != nil {
		return err
	}
	defer st.Close()
	status := client.Status()
	if !status.Configured {
		fmt.Println("同步未配置。运行 zero sync init --remote <url> 开始。")
		return nil
	}
	fmt.Printf("仓库: %s\n分支: %s\n认证: %s\n自动同步: %s\n",
		status.Remote, status.Branch, status.Auth, boolLabel(status.Auto))
	if status.KeyCached {
		fmt.Printf("本机密钥: 已保存（版本 %d）\n", status.KeyVersion)
		if !status.KeyMatchesRepo {
			fmt.Println("密钥状态: 当前密钥属于另一个仓库，请重新初始化/加入")
		}
	} else {
		fmt.Println("本机密钥: 未保存")
	}
	if status.KeyIssue != "" {
		fmt.Println("密钥状态: " + status.KeyIssue)
	}
	if status.LastSyncAt.IsZero() {
		fmt.Println("上次同步: 从未")
	} else {
		fmt.Printf("上次同步: %s（%s）\n", status.LastSyncAt.Format("2006-01-02 15:04:05"), status.Summary())
	}
	if status.Conflicts > 0 {
		fmt.Printf("待处理冲突: %d（事务 %s）\n", status.Conflicts, status.ConflictTx)
	}
	for _, warning := range status.Warnings {
		fmt.Println("提示: " + warning)
	}
	return nil
}

func boolLabel(v bool) string {
	if v {
		return "开启"
	}
	return "关闭"
}

func runSyncOnce(verb string) error {
	client, st, err := openSync()
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	res, err := client.Sync(ctx)
	if err != nil {
		return syncError(err)
	}
	if verb == "pull" {
		fmt.Println("拉取完成。")
	} else {
		fmt.Println("同步完成。")
	}
	printSyncResult(res)
	return nil
}

func syncError(err error) error {
	switch {
	case errors.Is(err, syncpkg.ErrConflict):
		return fmt.Errorf("%w，运行 zero sync conflicts 查看", err)
	default:
		return err
	}
}

func runSyncConflicts() error {
	client, st, err := openSync()
	if err != nil {
		return err
	}
	defer st.Close()
	index, err := client.Conflicts()
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("没有待处理的冲突。")
			return nil
		}
		return err
	}
	fmt.Printf("冲突事务 %s（%d 项）\n", index.TxID, len(index.Entities))
	choices, _ := client.PendingChoices(index.TxID)
	for _, entity := range index.Entities {
		choice := choices[entity.Entity]
		label := "未选择"
		if choice != "" {
			label = string(choice)
		}
		fmt.Printf("- %s\n  %s\n  实体: %s · 当前选择: %s\n",
			entity.Entity, syncpkg.SummarizeConflict(entity), entity.Path, label)
	}
	return nil
}

type resolveArgs struct {
	txid    string
	entity  string
	keep    string
	all     string
	discard bool
}

// parseResolveArgs 手写参数解析：标准 flag 包在第一个位置参数后停止，
// 而设计里的用法是 `zero sync resolve <tx> <entity> --keep local`。
func parseResolveArgs(args []string) (resolveArgs, error) {
	var out resolveArgs
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--discard":
			out.discard = true
		case arg == "--keep" || arg == "-keep":
			if i+1 >= len(args) {
				return out, fmt.Errorf("--keep 需要 local|remote|both")
			}
			i++
			out.keep = args[i]
		case strings.HasPrefix(arg, "--keep="):
			out.keep = strings.TrimPrefix(arg, "--keep=")
		case arg == "--all" || arg == "-all":
			if i+1 >= len(args) {
				return out, fmt.Errorf("--all 需要 local|remote")
			}
			i++
			out.all = args[i]
		case strings.HasPrefix(arg, "--all="):
			out.all = strings.TrimPrefix(arg, "--all=")
		case strings.HasPrefix(arg, "-"):
			return out, fmt.Errorf("未知参数 %q", arg)
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) == 0 {
		return out, fmt.Errorf("用法: zero sync resolve <tx> [<entity>] --keep local|remote|both")
	}
	out.txid = positional[0]
	if len(positional) > 1 {
		out.entity = positional[1]
	}
	if len(positional) > 2 {
		return out, fmt.Errorf("多余的参数：%s", strings.Join(positional[2:], " "))
	}
	return out, nil
}

func runSyncResolve(args []string) error {
	parsed, err := parseResolveArgs(args)
	if err != nil {
		return err
	}
	client, st, err := openSync()
	if err != nil {
		return err
	}
	defer st.Close()
	if parsed.discard {
		if err := client.DiscardConflict(context.Background(), parsed.txid); err != nil {
			return err
		}
		fmt.Println("已放弃本轮冲突；下次同步会重新拉取。")
		return nil
	}
	index, err := client.ConflictIndex(parsed.txid)
	if err != nil {
		return err
	}
	if parsed.all != "" {
		choice := syncpkg.Choice(parsed.all)
		for _, entity := range index.Entities {
			if err := client.ResolveEntity(parsed.txid, entity.Entity, choice); err != nil {
				return err
			}
		}
	} else {
		if parsed.keep == "" {
			return fmt.Errorf("需要 --keep local|remote|both")
		}
		if parsed.entity == "" {
			return fmt.Errorf("需要实体名，或用 --all")
		}
		if err := client.ResolveEntity(parsed.txid, parsed.entity, syncpkg.Choice(parsed.keep)); err != nil {
			return err
		}
	}
	choices, err := client.PendingChoices(parsed.txid)
	if err != nil {
		return err
	}
	missing := 0
	for _, entity := range index.Entities {
		if _, ok := choices[entity.Entity]; !ok {
			missing++
		}
	}
	if missing > 0 {
		fmt.Printf("还差 %d 项未选择。\n", missing)
		return nil
	}
	res, err := client.Resolve(context.Background(), parsed.txid, choices)
	if err != nil {
		return err
	}
	fmt.Println("冲突已解决并推送。")
	printSyncResult(res)
	return nil
}

func runSyncKey(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: zero sync key rotate|set|forget")
	}
	client, st, err := openSync()
	if err != nil {
		return err
	}
	defer st.Close()
	switch args[0] {
	case "rotate", "set":
		pass, err := readPassphrase("新同步口令: ")
		if err != nil {
			return err
		}
		confirm, err := readPassphrase("再次输入: ")
		if err != nil {
			return err
		}
		if pass != confirm {
			return fmt.Errorf("两次输入的口令不一致")
		}
		res, err := client.SetPassphrase(context.Background(), pass)
		if err != nil {
			return err
		}
		fmt.Println("口令已更换；其他设备需要重新输入新口令。")
		printSyncResult(res)
		return nil
	case "forget":
		if err := client.ForgetKey(); err != nil {
			return err
		}
		fmt.Println("已删除本机同步密钥；远端密文与登录态未改动。")
		return nil
	default:
		return fmt.Errorf("未知 key 子命令 %q", args[0])
	}
}

func runSyncArchive() error {
	client, st, err := openSync()
	if err != nil {
		return err
	}
	defer st.Close()
	fmt.Println(client.ArchiveDir())
	return nil
}
