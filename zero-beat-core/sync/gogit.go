package sync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	sshagent "github.com/xanzy/ssh-agent"
)

// go-git defaults its file transport to external git-upload-pack. Use its
// embedded server so local repositories also work with no Git executable.
func init() { client.InstallProtocol("file", embeddedFileTransport{}) }

type goGit struct {
	dir, remote, branch, token, sshKey, knownHosts string
	timeout                                        time.Duration
	repo                                           *git.Repository
	fetched                                        string
	agentConn                                      net.Conn
}

func (g *goGit) EnsureRepo(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ensureDir(g.dir, 0o700); err != nil {
		return err
	}
	r, err := git.PlainOpen(g.dir)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		r, err = git.PlainInitWithOptions(g.dir, &git.PlainInitOptions{InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName(g.branch)}})
	}
	if err != nil {
		return mapGoGitError(err)
	}
	g.repo = r
	cfg, err := r.Config()
	if err != nil {
		return err
	}
	cfg.Remotes["origin"] = &gitconfig.RemoteConfig{Name: "origin", URLs: []string{g.remote}, Fetch: []gitconfig.RefSpec{gitconfig.RefSpec("+refs/heads/" + g.branch + ":refs/remotes/origin/" + g.branch)}}
	return r.SetConfig(cfg)
}
func (g *goGit) auth() (transport.AuthMethod, error) {
	ep, err := transport.NewEndpoint(g.remote)
	if err != nil {
		return nil, err
	}
	switch ep.Protocol {
	case "https", "http":
		if g.token != "" {
			return &githttp.BasicAuth{Username: "git", Password: g.token}, nil
		}
		return nil, nil
	case "file":
		return nil, nil
	case "ssh":
		user := ep.User
		if user == "" {
			user = "git"
		}
		var files []string
		if g.knownHosts != "" {
			files = []string{g.knownHosts}
		}
		callback, err := gitssh.NewKnownHostsCallback(files...)
		if err != nil {
			return nil, fmt.Errorf("%w: known_hosts: %v", ErrAuth, err)
		}
		if g.sshKey != "" {
			a, err := gitssh.NewPublicKeysFromFile(user, g.sshKey, "")
			if err != nil {
				return nil, fmt.Errorf("%w: SSH key: %v", ErrAuth, err)
			}
			a.HostKeyCallback = callback
			return a, nil
		}
		if sshagent.Available() {
			agent, conn, err := sshagent.New()
			if err == nil {
				g.agentConn = conn
				a := &gitssh.PublicKeysCallback{User: user, Callback: agent.Signers}
				a.HostKeyCallback = callback
				return a, nil
			}
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		for _, name := range []string{"id_ed25519", "id_rsa"} {
			a, e := gitssh.NewPublicKeysFromFile(user, filepath.Join(home, ".ssh", name), "")
			if e == nil {
				a.HostKeyCallback = callback
				return a, nil
			}
		}
		return nil, fmt.Errorf("%w: go-git 需要 SSH agent 或 ssh_key；不执行 GIT_SSH_COMMAND", ErrAuth)
	default:
		return nil, fmt.Errorf("%w: go-git 不支持协议 %s", ErrGit, ep.Protocol)
	}
}
func (g *goGit) closeAuth() {
	if g.agentConn != nil {
		_ = g.agentConn.Close()
		g.agentConn = nil
	}
}
func (g *goGit) operation(ctx context.Context) (context.Context, context.CancelFunc) {
	if g.timeout > 0 {
		return context.WithTimeout(ctx, g.timeout)
	}
	return context.WithCancel(ctx)
}
func (g *goGit) Fetch(ctx context.Context) error {
	ctx, cancel := g.operation(ctx)
	defer cancel()
	auth, err := g.auth()
	defer g.closeAuth()
	if err != nil {
		return err
	}
	err = g.repo.FetchContext(ctx, &git.FetchOptions{RemoteName: "origin", Auth: auth, RefSpecs: []gitconfig.RefSpec{gitconfig.RefSpec("+refs/heads/" + g.branch + ":refs/remotes/origin/" + g.branch)}, Tags: git.NoTags})
	if errors.Is(err, transport.ErrEmptyRemoteRepository) || errors.Is(err, git.NoMatchingRefSpecError{}) {
		g.fetched = ""
		return errEmptyRemote
	}
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return mapGoGitError(err)
	}
	ref, err := g.repo.Reference(plumbing.ReferenceName("refs/remotes/origin/"+g.branch), true)
	if err != nil {
		return mapGoGitError(err)
	}
	g.fetched = ref.Hash().String()
	// Conventional FETCH_HEAD permits switching to CLI on the next operation.
	return writeFileAtomic(filepath.Join(g.dir, ".git", "FETCH_HEAD"), []byte(g.fetched+"\t\tbranch '"+g.branch+"' of "+g.remote+"\n"), 0o600)
}
func (g *goGit) RevParse(ctx context.Context, rev string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if rev == "FETCH_HEAD" {
		return g.fetched, g.fetched != "", nil
	}
	h, err := g.repo.ResolveRevision(plumbing.Revision(rev))
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, mapGoGitError(err)
	}
	return h.String(), true, nil
}
func (g *goGit) Head(ctx context.Context) (string, error) {
	h, ok, e := g.RevParse(ctx, "HEAD")
	if e == nil && !ok {
		e = plumbing.ErrReferenceNotFound
	}
	return h, e
}
func (g *goGit) HasHead(ctx context.Context) bool { _, ok, _ := g.RevParse(ctx, "HEAD"); return ok }
func (g *goGit) Status(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	wt, err := g.repo.Worktree()
	if err != nil {
		return "", err
	}
	s, err := wt.Status()
	return s.String(), err
}
func (g *goGit) tree(ctx context.Context, rev string) (*object.Tree, error) {
	h, ok, err := g.RevParse(ctx, rev)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, plumbing.ErrReferenceNotFound
	}
	c, err := g.repo.CommitObject(plumbing.NewHash(h))
	if err != nil {
		return nil, err
	}
	return c.Tree()
}
func (g *goGit) showFile(ctx context.Context, rev, path string) ([]byte, bool, error) {
	tree, err := g.tree(ctx, rev)
	if err != nil {
		return nil, false, err
	}
	f, err := tree.File(path)
	if errors.Is(err, object.ErrFileNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if f.Mode != filemode.Regular && f.Mode != filemode.Executable {
		return nil, false, ErrCorruptRepo
	}
	if f.Size > maxRepoFileSize {
		return nil, false, ErrCorruptRepo
	}
	r, err := f.Reader()
	if err != nil {
		return nil, false, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, maxRepoFileSize+1))
	return data, true, err
}
func (g *goGit) LsTree(ctx context.Context, rev string) ([]string, error) {
	tree, err := g.tree(ctx, rev)
	if err != nil {
		return nil, err
	}
	var paths []string
	var walk func(*object.Tree, string, int) error
	walk = func(tree *object.Tree, prefix string, depth int) error {
		if depth > 128 {
			return ErrCorruptRepo
		}
		for _, entry := range tree.Entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := prefix + entry.Name
			if _, err := cleanRepoPath(name); err != nil {
				return err
			}
			switch entry.Mode {
			case filemode.Dir:
				child, err := g.repo.TreeObject(entry.Hash)
				if err != nil {
					return err
				}
				if err := walk(child, name+"/", depth+1); err != nil {
					return err
				}
			case filemode.Regular, filemode.Executable:
				paths = append(paths, name)
			default:
				return ErrCorruptRepo
			}
		}
		return nil
	}
	if err := walk(tree, "", 0); err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}
func (g *goGit) CheckoutFrom(ctx context.Context, rev, path string) error {
	data, ok, err := g.showFile(ctx, rev, path)
	if err != nil {
		return err
	}
	return (&Git{Dir: g.dir}).writeFile(path, data, ok)
}
func (g *goGit) AddPath(ctx context.Context, path string) error { return ctx.Err() }
func (g *goGit) AddAll(ctx context.Context) error               { return ctx.Err() }
func (g *goGit) ResetHard(ctx context.Context) error {
	if !g.HasHead(ctx) {
		return ctx.Err()
	}
	h, err := g.Head(ctx)
	if err != nil {
		return err
	}
	return g.ResetHardTo(ctx, h)
}
func (g *goGit) ResetHardTo(ctx context.Context, rev string) error {
	h, ok, err := g.RevParse(ctx, rev)
	if err != nil {
		return err
	}
	if !ok {
		return ErrCorruptRepo
	}
	old, exists, err := g.RevParse(ctx, "HEAD")
	if err != nil {
		return err
	}
	if !exists || old != h {
		if err := g.UpdateHead(ctx, old, h); err != nil {
			return err
		}
	}
	wt, err := g.repo.Worktree()
	if err != nil {
		return err
	}
	return wt.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: plumbing.NewHash(h)})
}
func (g *goGit) Clean(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wt, err := g.repo.Worktree()
	if err != nil {
		return err
	}
	return wt.Clean(&git.CleanOptions{Dir: true})
}
func (g *goGit) Push(ctx context.Context) error {
	ctx, cancel := g.operation(ctx)
	defer cancel()
	auth, err := g.auth()
	defer g.closeAuth()
	if err != nil {
		return err
	}
	name, err := g.headRef()
	if err != nil {
		return err
	}
	err = g.repo.PushContext(ctx, &git.PushOptions{RemoteName: "origin", Auth: auth, RefSpecs: []gitconfig.RefSpec{gitconfig.RefSpec(name.String() + ":refs/heads/" + g.branch)}})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return mapGoGitError(err)
}
func (g *goGit) MergeBases(ctx context.Context, left, right string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a, err := g.repo.CommitObject(plumbing.NewHash(left))
	if err != nil {
		return nil, err
	}
	b, err := g.repo.CommitObject(plumbing.NewHash(right))
	if err != nil {
		return nil, err
	}
	bases, err := a.MergeBase(b)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(bases))
	for i, b := range bases {
		out[i] = b.Hash.String()
	}
	return out, ctx.Err()
}

// WriteCommit writes objects only. Updating HEAD is a separate compare-and-swap.
func (g *goGit) WriteCommit(ctx context.Context, parents []string, message string, now time.Time) (string, error) {
	tree, err := g.writeTree(ctx, g.dir)
	if err != nil {
		return "", err
	}
	c := &object.Commit{Author: object.Signature{Name: "zero-beat", Email: "zero-beat@localhost", When: now}, TreeHash: tree, Message: strings.TrimRight(message, "\n") + "\n"}
	c.Committer = c.Author
	for _, p := range parents {
		c.ParentHashes = append(c.ParentHashes, plumbing.NewHash(p))
	}
	obj := g.repo.Storer.NewEncodedObject()
	if err = c.Encode(obj); err != nil {
		return "", err
	}
	h, err := g.repo.Storer.SetEncodedObject(obj)
	return h.String(), err
}
func (g *goGit) writeTree(ctx context.Context, dir string) (plumbing.Hash, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	tree := &object.Tree{}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return plumbing.ZeroHash, err
		}
		if e.Name() == ".git" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		mode := filemode.Regular
		var hash plumbing.Hash
		if e.IsDir() {
			mode = filemode.Dir
			hash, err = g.writeTree(ctx, path)
			// Git's index never records empty directories. Match CLI trees even
			// after deleting the last entity in a managed directory.
			if err == nil && hash == plumbing.NewHash("4b825dc642cb6eb9a060e54bf8d69288fbee4904") {
				continue
			}
		} else {
			info, e2 := e.Info()
			if e2 != nil {
				return plumbing.ZeroHash, e2
			}
			if !info.Mode().IsRegular() || info.Size() > maxRepoFileSize {
				return plumbing.ZeroHash, ErrCorruptRepo
			}
			if info.Mode()&0o111 != 0 {
				mode = filemode.Executable
			}
			data, e2 := os.ReadFile(path)
			if e2 != nil {
				return plumbing.ZeroHash, e2
			}
			obj := g.repo.Storer.NewEncodedObject()
			obj.SetType(plumbing.BlobObject)
			obj.SetSize(int64(len(data)))
			w, e2 := obj.Writer()
			if e2 != nil {
				return plumbing.ZeroHash, e2
			}
			_, err = w.Write(data)
			closeErr := w.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				hash, err = g.repo.Storer.SetEncodedObject(obj)
			}
		}
		if err != nil {
			return plumbing.ZeroHash, err
		}
		tree.Entries = append(tree.Entries, object.TreeEntry{Name: e.Name(), Mode: mode, Hash: hash})
	}
	sort.Sort(object.TreeEntrySorter(tree.Entries))
	obj := g.repo.Storer.NewEncodedObject()
	if err = tree.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return g.repo.Storer.SetEncodedObject(obj)
}
func (g *goGit) headRef() (plumbing.ReferenceName, error) {
	head, err := g.repo.Reference(plumbing.HEAD, false)
	if err != nil {
		return "", err
	}
	if head.Type() != plumbing.SymbolicReference {
		return "", fmt.Errorf("%w: 受管仓库 HEAD 必须指向本地分支", ErrCorruptRepo)
	}
	return head.Target(), nil
}
func (g *goGit) UpdateHead(ctx context.Context, old, next string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name, err := g.headRef()
	if err != nil {
		return err
	}
	current, err := g.repo.Reference(name, false)
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		if old != "" {
			return ErrRevision
		}
		current = nil
	} else if err != nil {
		return err
	} else if current.Hash().String() != old {
		return ErrRevision
	}
	return g.repo.Storer.CheckAndSetReference(plumbing.NewHashReference(name, plumbing.NewHash(next)), current)
}
func mapGoGitError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, transport.ErrAuthenticationRequired) || errors.Is(err, transport.ErrAuthorizationFailed) || errors.Is(err, transport.ErrRepositoryNotFound) {
		return fmt.Errorf("%w: %v", ErrAuth, err)
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	if errors.Is(err, git.ErrForceNeeded) || strings.Contains(err.Error(), "non-fast-forward") || strings.Contains(err.Error(), "failed to update ref") {
		return fmt.Errorf("%w: %v", ErrPushRejected, err)
	}
	if strings.Contains(err.Error(), "ssh:") || strings.Contains(err.Error(), "knownhosts:") {
		return fmt.Errorf("%w: %v", ErrAuth, err)
	}
	return fmt.Errorf("%w: %v", ErrGit, err)
}
