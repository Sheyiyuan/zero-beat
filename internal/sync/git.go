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

// Git 是系统 git 的薄封装：只负责命令、环境与错误分类，不做业务判断。
// 工作克隆固定在受管目录，不是用户的代码仓库。
type Git struct {
	Dir       string
	Remote    string
	Branch    string
	AskPass   string // 可选的 GIT_ASKPASS 脚本路径
	TokenFile string // 配合 AskPass 的 0600 token 文件
	Timeout   time.Duration
}

// NewGit 构造 git 封装；空分支按 main 处理。
func NewGit(dir, remote, branch string) *Git {
	if branch == "" {
		branch = "main"
	}
	return &Git{Dir: dir, Remote: remote, Branch: branch, Timeout: 2 * time.Minute}
}

func (g *Git) env() []string {
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

func (g *Git) exec(ctx context.Context, input string, args ...string) (stdout, stderr []byte, err error) {
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
	return out.Bytes(), errBuf.Bytes(), err
}

// run 执行命令并把非零退出码翻译成分类错误。
func (g *Git) run(ctx context.Context, args ...string) (string, error) {
	out, stderr, err := g.exec(ctx, "", args...)
	if err == nil {
		return string(out), nil
	}
	return "", classifyGitError(strings.Join(args, " "), string(stderr), err)
}

// showFile 读取某个提交里的文件；路径不存在时返回 (nil, false, nil)。
func (g *Git) showFile(ctx context.Context, rev, path string) ([]byte, bool, error) {
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

// stageFile 读取冲突的三个版本之一（stage 1/2/3）；不存在时返回 (nil, false, nil)。
func (g *Git) stageFile(ctx context.Context, stage int, path string) ([]byte, bool, error) {
	spec := fmt.Sprintf(":%d:%s", stage, path)
	out, stderr, err := g.exec(ctx, "", "show", spec)
	if err == nil {
		return out, true, nil
	}
	msg := strings.ToLower(string(stderr))
	if strings.Contains(msg, "does not exist") || strings.Contains(msg, "exists on disk, but not in") ||
		strings.Contains(msg, "path not in the working tree") || strings.Contains(msg, "invalid object name") ||
		strings.Contains(msg, "is in the index, but not at stage") {
		return nil, false, nil
	}
	return nil, false, classifyGitError("show "+spec, string(stderr), err)
}

// EnsureRepo 创建或修正受管克隆，并把 origin 指向配置里的 URL。
func (g *Git) EnsureRepo(ctx context.Context) error {
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
func (g *Git) Fetch(ctx context.Context) error {
	_, err := g.run(ctx, "fetch", "--prune", "origin", g.Branch)
	return err
}

// RevParse 解析引用；不存在时返回 false。
func (g *Git) RevParse(ctx context.Context, rev string) (string, bool, error) {
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
func (g *Git) HasHead(ctx context.Context) bool {
	_, ok, _ := g.RevParse(ctx, "HEAD")
	return ok
}

// Head 返回本地提交号。
func (g *Git) Head(ctx context.Context) (string, error) {
	return g.run(ctx, "rev-parse", "HEAD")
}

// MergeHead 返回正在进行中的合并目标；没有合并时返回空字符串。
func (g *Git) MergeHead(ctx context.Context) string {
	rev, ok, _ := g.RevParse(ctx, "MERGE_HEAD")
	if !ok {
		return ""
	}
	return rev
}

// Status 返回 porcelain 状态（空字符串表示干净）。
func (g *Git) Status(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "status", "--porcelain")
	return strings.TrimSpace(out), err
}

// CommitAll 暂存全部改动并提交；没有改动时返回 false。
func (g *Git) CommitAll(ctx context.Context, message string) (bool, error) {
	status, err := g.Status(ctx)
	if err != nil {
		return false, err
	}
	if status == "" {
		return false, nil
	}
	if _, err := g.run(ctx, "add", "-A"); err != nil {
		return false, err
	}
	if _, err := g.run(ctx, "commit", "-m", message); err != nil {
		return false, err
	}
	return true, nil
}

// Merge 合并指定提交；冲突时返回冲突路径列表。
func (g *Git) Merge(ctx context.Context, rev string) ([]string, error) {
	_, stderr, err := g.exec(ctx, "", "merge", "--no-edit", rev)
	if err == nil {
		return nil, nil
	}
	paths, listErr := g.UnmergedPaths(ctx)
	if listErr == nil && len(paths) > 0 {
		return paths, nil
	}
	return nil, classifyGitError("merge", string(stderr), err)
}

// MergeNoCommit 重新制造同一次合并的冲突现场，不自动提交。
func (g *Git) MergeNoCommit(ctx context.Context, rev string) ([]string, error) {
	_, stderr, err := g.exec(ctx, "", "merge", "--no-commit", rev)
	if err == nil {
		return nil, nil
	}
	paths, listErr := g.UnmergedPaths(ctx)
	if listErr == nil && len(paths) > 0 {
		return paths, nil
	}
	return nil, classifyGitError("merge", string(stderr), err)
}

// UnmergedPaths 返回当前未合并的路径。
func (g *Git) UnmergedPaths(ctx context.Context) ([]string, error) {
	out, err := g.run(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

// CheckoutFrom 从指定提交取出单个文件到工作区与索引，用于让本侧直接采用远端版本。
func (g *Git) CheckoutFrom(ctx context.Context, rev, path string) error {
	_, err := g.run(ctx, "checkout", rev, "--", path)
	return err
}

// AddPath 只暂存指定路径的改动或删除，不会顺带解决其他冲突。
func (g *Git) AddPath(ctx context.Context, path string) error {
	_, err := g.run(ctx, "add", "-A", "--", path)
	return err
}

// AddAll 暂存工作区全部改动（冲突解决时用）。
func (g *Git) AddAll(ctx context.Context) error {
	_, err := g.run(ctx, "add", "-A")
	return err
}

// CommitMerge 提交进行中的合并，保留双方历史。
func (g *Git) CommitMerge(ctx context.Context, message string) error {
	_, err := g.run(ctx, "commit", "-m", message)
	return err
}

// AbortMerge 取消进行中的合并；没有合并时是空操作。
func (g *Git) AbortMerge(ctx context.Context) error {
	if g.MergeHead(ctx) == "" {
		return nil
	}
	_, err := g.run(ctx, "merge", "--abort")
	return err
}

// ResetHard 把工作区恢复到 HEAD；没有提交时忽略。
func (g *Git) ResetHard(ctx context.Context) error {
	if !g.HasHead(ctx) {
		return nil
	}
	_, err := g.run(ctx, "reset", "--hard", "HEAD")
	return err
}

// ResetHardTo 把当前分支重置到指定提交（仅用于新设备首次导入）。
func (g *Git) ResetHardTo(ctx context.Context, rev string) error {
	_, err := g.run(ctx, "reset", "--hard", rev)
	return err
}

// Clean 删除受管克隆里的未跟踪文件（导出前重建用）。
func (g *Git) Clean(ctx context.Context) error {
	_, err := g.run(ctx, "clean", "-fd")
	return err
}

// Push 推送当前 HEAD 到配置分支。
func (g *Git) Push(ctx context.Context) error {
	_, err := g.run(ctx, "push", "origin", "HEAD:refs/heads/"+g.Branch)
	return err
}

// LsTree 返回某个提交里的全部文件路径。
func (g *Git) LsTree(ctx context.Context, rev string) ([]string, error) {
	out, err := g.run(ctx, "ls-tree", "-r", "--name-only", rev)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// Recover 清理上次崩溃留下的合并与脏文件。
func (g *Git) Recover(ctx context.Context) error {
	if err := g.AbortMerge(ctx); err != nil {
		return err
	}
	if err := g.ResetHard(ctx); err != nil {
		return err
	}
	return g.Clean(ctx)
}

// classifyGitError 把 git 的 stderr 翻译成 §19 的错误分类。
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
	case errors.Is(err, context.DeadlineExceeded):
		return wrap(ErrNetwork)
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
