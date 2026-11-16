package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// errEmptyRemote 表示远端还没有目标分支（首次推送前的正常状态）。
var errEmptyRemote = errors.New("远端分支为空")

// cliGit 是系统 git 的薄封装：只负责命令、环境与错误分类，不做业务判断。
type cliGit struct {
	Dir       string
	Remote    string
	Branch    string
	AskPass   string // 可选的 GIT_ASKPASS 脚本路径
	TokenFile string // 配合 AskPass 的 0600 token 文件
	Timeout   time.Duration
}

func (g *cliGit) env() []string {
	// 受管仓库使用固定身份提交，避免依赖用户的全局 git 配置。
	env := append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
		"GIT_AUTHOR_NAME=zero-beat",
		"GIT_AUTHOR_EMAIL=zero-beat@localhost",
		"GIT_COMMITTER_NAME=zero-beat",
		"GIT_COMMITTER_EMAIL=zero-beat@localhost",
	)
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		env = append(env, "GIT_SSH_COMMAND=ssh -oBatchMode=yes")
	}
	if g.AskPass != "" {
		env = append(env, "GIT_ASKPASS="+g.AskPass)
		if g.TokenFile != "" {
			env = append(env, "ZERO_SYNC_TOKEN_FILE="+g.TokenFile)
		}
	}
	return env
}

func (g *cliGit) exec(ctx context.Context, input string, args ...string) (stdout, stderr []byte, err error) {
	if g.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.Dir
	cmd.Env = g.env()
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err = cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return out.Bytes(), errBuf.Bytes(), err
}

// run 执行命令并把非零退出码翻译成分类错误。
func (g *cliGit) run(ctx context.Context, args ...string) (string, error) {
	out, stderr, err := g.exec(ctx, "", args...)
	if err == nil {
		return string(out), nil
	}
	return "", classifyGitError(strings.Join(args, " "), string(stderr), err)
}

// showFile 读取某个提交里的文件；路径不存在时返回 (nil, false, nil)。
func (g *cliGit) showFile(ctx context.Context, rev, path string) ([]byte, bool, error) {
	out, stderr, err := g.exec(ctx, "", "show", rev+":"+path)
	if err == nil {
		return out, true, nil
	}
	msg := strings.ToLower(string(stderr))
	if strings.Contains(msg, "does not exist") || strings.Contains(msg, "exists on disk, but not in") ||
		strings.Contains(msg, "unknown revision") || strings.Contains(msg, "invalid object name") {
		return nil, false, nil
	}
	return nil, false, classifyGitError("show "+path, string(stderr), err)
}

func (g *cliGit) EnsureRepo(ctx context.Context) error {
	if _, err := exec.LookPath("git"); err != nil {
		return ErrGitMissing
	}
	if err := ensureDir(g.Dir, 0o700); err != nil {
		return err
	}
	if !fileExists(filepath.Join(g.Dir, ".git")) {
		if _, err := g.run(ctx, "init", "-b", g.Branch); err != nil {
			// 老版本 git 没有 `init -b`：回退到 init + symbolic-ref。
			if _, err2 := g.run(ctx, "init"); err2 != nil {
				return err
			}
			if _, err2 := g.run(ctx, "symbolic-ref", "HEAD", "refs/heads/"+g.Branch); err2 != nil {
				return err2
			}
		}
	}
	if _, err := g.run(ctx, "remote", "get-url", "origin"); err != nil {
		if _, err := g.run(ctx, "remote", "add", "origin", g.Remote); err != nil {
			return err
		}
	} else if _, err := g.run(ctx, "remote", "set-url", "origin", g.Remote); err != nil {
		return err
	}
	return nil
}

// Fetch 拉取配置分支；远端还没有该分支时返回 errEmptyRemote。
func (g *cliGit) Fetch(ctx context.Context) error {
	_, err := g.run(ctx, "fetch", "--prune", "origin", g.Branch)
	return err
}

// RevParse 解析引用；不存在时返回 false。
func (g *cliGit) RevParse(ctx context.Context, rev string) (string, bool, error) {
	out, stderr, err := g.exec(ctx, "", "rev-parse", "-q", "--verify", rev)
	if err == nil {
		return strings.TrimSpace(string(out)), true, nil
	}
	if len(stderr) == 0 || strings.Contains(strings.ToLower(string(stderr)), "needed a single revision") {
		return "", false, nil
	}
	return "", false, classifyGitError("rev-parse "+rev, string(stderr), err)
}

// HasHead 判断本地是否已有提交。
func (g *cliGit) HasHead(ctx context.Context) bool {
	_, ok, _ := g.RevParse(ctx, "HEAD")
	return ok
}

// Head 返回本地提交号。
func (g *cliGit) Head(ctx context.Context) (string, error) {
	h, err := g.run(ctx, "rev-parse", "HEAD")
	return strings.TrimSpace(h), err
}

func (g *cliGit) Status(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "status", "--porcelain")
	return strings.TrimSpace(out), err
}

func (g *cliGit) CheckoutFrom(ctx context.Context, rev, path string) error {
	_, err := g.run(ctx, "checkout", rev, "--", path)
	return err
}

// AddPath 只暂存指定路径的改动或删除，不会顺带解决其他冲突。
func (g *cliGit) AddPath(ctx context.Context, path string) error {
	_, err := g.run(ctx, "add", "-A", "--", path)
	return err
}

// AddAll 暂存工作区全部改动（冲突解决时用）。
func (g *cliGit) AddAll(ctx context.Context) error {
	_, err := g.run(ctx, "add", "-A")
	return err
}

func (g *cliGit) ResetHard(ctx context.Context) error {
	if !g.HasHead(ctx) {
		return nil
	}
	_, err := g.run(ctx, "reset", "--hard", "HEAD")
	return err
}

// ResetHardTo 把当前分支重置到指定提交（仅用于新设备首次导入）。
func (g *cliGit) ResetHardTo(ctx context.Context, rev string) error {
	_, err := g.run(ctx, "reset", "--hard", rev)
	return err
}

// Clean 删除受管克隆里的未跟踪文件（导出前重建用）。
func (g *cliGit) Clean(ctx context.Context) error {
	_, err := g.run(ctx, "clean", "-fd")
	return err
}

// Push 推送当前 HEAD 到配置分支。
func (g *cliGit) Push(ctx context.Context) error {
	_, err := g.run(ctx, "push", "origin", "HEAD:refs/heads/"+g.Branch)
	return err
}

// LsTree 返回某个提交里的全部文件路径。
func (g *cliGit) LsTree(ctx context.Context, rev string) ([]string, error) {
	out, err := g.run(ctx, "ls-tree", "-r", "-z", rev)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, record := range strings.Split(out, "\x00") {
		if record == "" {
			continue
		}
		fields, path, ok := strings.Cut(record, "\t")
		if !ok {
			return nil, ErrCorruptRepo
		}
		meta := strings.Fields(fields)
		if len(meta) != 3 || meta[1] != "blob" || (meta[0] != "100644" && meta[0] != "100755") {
			return nil, ErrCorruptRepo
		}
		files = append(files, path)
	}
	return files, nil
}

func classifyGitError(op, stderr string, err error) error {
	msg := strings.ToLower(stderr)
	var detail string
	if line := firstLine(stderr); line != "" {
		detail = line
	} else if err != nil {
		detail = err.Error()
	}
	wrap := func(kind error) error {
		if detail == "" {
			return kind
		}
		return fmt.Errorf("%w（%s）", kind, detail)
	}

	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, exec.ErrNotFound):
		return ErrGitMissing
	case strings.Contains(msg, "couldn't find remote ref"), strings.Contains(msg, "could not find remote ref"):
		return errEmptyRemote
	case strings.Contains(msg, "authentication failed"),
		strings.Contains(msg, "could not read username"),
		strings.Contains(msg, "terminal prompts disabled"),
		strings.Contains(msg, "permission denied (publickey"),
		strings.Contains(msg, "invalid username or password"),
		strings.Contains(msg, "support for password authentication"),
		strings.Contains(msg, "returned error: 401"),
		strings.Contains(msg, "returned error: 403"),
		strings.Contains(msg, "access denied"),
		strings.Contains(msg, "repository not found"):
		return wrap(ErrAuth)
	case strings.Contains(msg, "could not resolve host"),
		strings.Contains(msg, "connection timed out"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "network is unreachable"),
		strings.Contains(msg, "failed to connect"),
		strings.Contains(msg, "early eof"),
		strings.Contains(msg, "rpc failed"),
		strings.Contains(msg, "operation timed out"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "no route to host"),
		strings.Contains(msg, "tls handshake"):
		return wrap(ErrNetwork)
	case strings.Contains(msg, "non-fast-forward"),
		strings.Contains(msg, "fetch first"),
		strings.Contains(msg, "rejected"),
		strings.Contains(msg, "failed to push some refs"):
		return wrap(ErrPushRejected)
	case strings.Contains(msg, "would be overwritten"),
		strings.Contains(msg, "local changes"),
		strings.Contains(msg, "please commit your changes"):
		return wrap(ErrDirtyWorktree)
	}
	if detail == "" {
		return fmt.Errorf("%w: git %s 失败", ErrGit, op)
	}
	return fmt.Errorf("%w: git %s: %s", ErrGit, op, detail)
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func (g *cliGit) MergeBases(ctx context.Context, local, remote string) ([]string, error) {
	out, stderr, err := g.exec(ctx, "", "merge-base", "--all", local, remote)
	if err != nil && len(stderr) > 0 {
		return nil, classifyGitError("merge-base", string(stderr), err)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return strings.Fields(string(out)), nil
}
func (g *cliGit) WriteCommit(ctx context.Context, parents []string, message string, now time.Time) (string, error) {
	if err := g.AddAll(ctx); err != nil {
		return "", err
	}
	tree, err := g.run(ctx, "write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", strings.TrimSpace(tree)}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	// Fixed author/time are shared with the embedded backend.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.Dir
	cmd.Env = append(g.env(), "GIT_AUTHOR_DATE="+now.Format(time.RFC3339), "GIT_COMMITTER_DATE="+now.Format(time.RFC3339))
	cmd.Stdin = strings.NewReader(message + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", classifyGitError("commit-tree", string(out), err)
	}
	return strings.TrimSpace(string(out)), nil
}
func (g *cliGit) UpdateHead(ctx context.Context, old, next string) error {
	if old == "" {
		old = strings.Repeat("0", 40)
	}
	_, err := g.run(ctx, "update-ref", "HEAD", next, old)
	return err
}
