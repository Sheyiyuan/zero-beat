package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/sheyiyuan/zero-beat-core/keymap"
)

// Backend selects transport and object storage; merge policy belongs to Client.
type Backend string

const (
	BackendAuto  Backend = "auto"
	BackendCLI   Backend = "cli"
	BackendGoGit Backend = "gogit"
)

func ValidateBackend(b Backend) error {
	switch b {
	case "", BackendAuto, BackendCLI, BackendGoGit:
		return nil
	}
	return fmt.Errorf("未知同步后端 %q", b)
}
func selectBackend(override, configured Backend) (Backend, Backend, error) {
	requested := override
	if requested == "" {
		requested = Backend(os.Getenv("ZERO_SYNC_BACKEND"))
	}
	if requested == "" {
		requested = configured
	}
	if requested == "" {
		requested = BackendAuto
	}
	if err := ValidateBackend(requested); err != nil {
		return requested, "", err
	}
	if requested != BackendAuto {
		return requested, requested, nil
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "android" {
		return requested, BackendGoGit, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "git", "--version").Run(); err == nil {
		return requested, BackendCLI, nil
	}
	return requested, BackendGoGit, nil
}

// repositoryBackend has no merge or conflict-index operations. All revisions
// passed by the shared merge layer are immutable commit IDs.
type repositoryBackend interface {
	EnsureRepo(context.Context) error
	Fetch(context.Context) error
	RevParse(context.Context, string) (string, bool, error)
	HasHead(context.Context) bool
	Head(context.Context) (string, error)
	Status(context.Context) (string, error)
	showFile(context.Context, string, string) ([]byte, bool, error)
	LsTree(context.Context, string) ([]string, error)
	CheckoutFrom(context.Context, string, string) error
	AddPath(context.Context, string) error
	AddAll(context.Context) error
	ResetHard(context.Context) error
	ResetHardTo(context.Context, string) error
	Clean(context.Context) error
	Push(context.Context) error
	MergeBases(context.Context, string, string) ([]string, error)
	WriteCommit(context.Context, []string, string, time.Time) (string, error)
	UpdateHead(context.Context, string, string) error
}

// Git holds one backend for an entire operation and shared three-way state.
type Git struct {
	BeforeUpdate func(old, next string) error
	repositoryBackend
	Dir                 string
	Backend             Backend
	Now                 func() time.Time
	base, local, remote string
	conflicts           map[string]bool
}

func NewGit(dir, remote, branch string) *Git {
	if branch == "" {
		branch = "main"
	}
	return &Git{repositoryBackend: &cliGit{Dir: dir, Remote: remote, Branch: branch, Timeout: 2 * time.Minute}, Dir: dir, Backend: BackendCLI}
}
func (g *Git) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}
func (g *Git) CommitAll(ctx context.Context, message string) (bool, error) {
	status, err := g.Status(ctx)
	if err != nil || status == "" {
		return false, err
	}
	old, _, err := g.RevParse(ctx, "HEAD")
	if err != nil {
		return false, err
	}
	var parents []string
	if old != "" {
		parents = []string{old}
	}
	next, err := g.WriteCommit(ctx, parents, message, g.now())
	if err != nil {
		return false, err
	}
	if err = g.UpdateHead(ctx, old, next); err != nil {
		return false, err
	}
	return true, g.ResetHard(ctx)
}
func (g *Git) MergeHead(context.Context) string { return g.remote }
func (g *Git) Merge(ctx context.Context, rev string) ([]string, error) {
	return g.merge(ctx, rev, true)
}
func (g *Git) MergeNoCommit(ctx context.Context, rev string) ([]string, error) {
	return g.merge(ctx, rev, false)
}
func (g *Git) merge(ctx context.Context, rev string, commit bool) ([]string, error) {
	local, err := g.Head(ctx)
	if err != nil {
		return nil, err
	}
	remote, ok, err := g.RevParse(ctx, rev)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrCorruptRepo
	}
	g.local = local
	g.remote = remote
	g.conflicts = map[string]bool{}
	if local == remote {
		g.remote = ""
		return nil, nil
	}
	bases, err := g.MergeBases(ctx, local, remote)
	if err != nil {
		return nil, err
	}
	if len(bases) != 1 {
		return nil, fmt.Errorf("%w: 需要唯一共同祖先，找到 %d 个", ErrUnsupportedHistory, len(bases))
	}
	g.base = bases[0]
	if g.base == remote {
		g.remote = ""
		return nil, nil
	}
	if g.base == local {
		if err = g.UpdateHead(ctx, local, remote); err != nil {
			return nil, err
		}
		g.remote = ""
		return nil, g.ResetHard(ctx)
	}
	base, err := g.readTree(ctx, g.base)
	if err != nil {
		return nil, err
	}
	left, err := g.readTree(ctx, local)
	if err != nil {
		return nil, err
	}
	right, err := g.readTree(ctx, remote)
	if err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	for _, tree := range []map[string][]byte{base, left, right} {
		for p := range tree {
			paths[p] = true
		}
	}
	for p := range paths {
		b, bok := base[p]
		l, lok := left[p]
		r, rok := right[p]
		chosen, exists := l, lok
		switch {
		case sameFile(l, lok, r, rok):
		case sameFile(b, bok, l, lok):
			chosen, exists = r, rok
		case sameFile(b, bok, r, rok):
		default:
			g.conflicts[p] = true
		}
		if err := g.writeFile(p, chosen, exists); err != nil {
			return nil, err
		}
	}
	conflicts, _ := g.UnmergedPaths(ctx)
	if len(conflicts) == 0 && commit {
		err = g.CommitMerge(ctx, "zero-beat sync merge")
	}
	return conflicts, err
}
func sameFile(a []byte, aok bool, b []byte, bok bool) bool { return aok == bok && bytes.Equal(a, b) }
func (g *Git) readTree(ctx context.Context, rev string) (map[string][]byte, error) {
	files, err := g.LsTree(ctx, rev)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, p := range files {
		if _, err := cleanRepoPath(p); err != nil {
			return nil, err
		}
		b, ok, e := g.showFile(ctx, rev, p)
		if e != nil {
			return nil, e
		}
		if ok {
			if len(b) > maxRepoFileSize {
				return nil, ErrCorruptRepo
			}
			out[p] = b
		}
	}
	return out, nil
}
func (g *Git) writeFile(path string, data []byte, exists bool) error {
	clean, err := cleanRepoPath(path)
	if err != nil {
		return err
	}
	full := filepath.Join(g.Dir, filepath.FromSlash(clean))
	if !exists {
		return removeIfExists(full)
	}
	return writeFileAtomic(full, data, 0o600)
}
func (g *Git) conflictFile(ctx context.Context, side, path string) ([]byte, bool, error) {
	rev := g.base
	switch side {
	case "local":
		rev = g.local
	case "remote":
		rev = g.remote
	case "base":
	default:
		return nil, false, ErrCorruptRepo
	}
	if rev == "" {
		return nil, false, nil
	}
	return g.showFile(ctx, rev, path)
}
func (g *Git) UnmergedPaths(context.Context) ([]string, error) {
	var paths []string
	for p := range g.conflicts {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}
func (g *Git) AddPath(ctx context.Context, path string) error {
	if err := g.repositoryBackend.AddPath(ctx, path); err != nil {
		return err
	}
	delete(g.conflicts, path)
	return nil
}
func (g *Git) CommitMerge(ctx context.Context, message string) error {
	if g.remote == "" {
		return nil
	}
	if len(g.conflicts) > 0 {
		return ErrConflict
	}
	next, err := g.WriteCommit(ctx, []string{g.local, g.remote}, message, g.now())
	if err != nil {
		return err
	}
	if err = g.UpdateHead(ctx, g.local, next); err != nil {
		return err
	}
	g.remote = ""
	return g.ResetHard(ctx)
}
func (g *Git) AbortMerge(ctx context.Context) error {
	if g.local != "" {
		head, err := g.Head(ctx)
		if err != nil {
			return err
		}
		if head != g.local {
			return nil
		}
	}
	g.remote = ""
	g.conflicts = nil
	return g.ResetHard(ctx)
}
func (g *Git) Recover(ctx context.Context) error {
	// Old clients may leave a MERGE_HEAD. No new transaction relies on it.
	if err := g.ResetHard(ctx); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(g.Dir, ".git", "MERGE_HEAD")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return g.Clean(ctx)
}

var ErrUnsupportedHistory = errors.New("不支持的同步历史")
var ErrStaleConflict = errors.New("冲突输入已变化，请放弃旧选择并重新同步")
var ErrLegacyConflict = errors.New("旧版冲突事务仅支持查看或归档，请放弃后重新同步")

// validateTree checks all managed entities before any import/export can drop fields.
func (c *Client) validateTree(ctx context.Context, g *Git, rev string, key *KeyState) error {
	if rev == "" {
		return nil
	}
	tree, err := g.readTree(ctx, rev)
	if err != nil {
		return err
	}
	for p, data := range tree {
		switch {
		case p == MetaFile:
			_, err = DecodeMeta(data)
		case strings.HasPrefix(p, PlaylistsDir+"/"):
			_, err = DecodePlaylistFile(data)
		case strings.HasPrefix(p, AnnotationsDir+"/"):
			var a AnnotationFile
			err = decodeJSONStrict(data, &a)
		case p == KeymapFile:
			err = validateKeymapBytes(data)
		case accountSourceFromPath(p) != "":
			var envelope Envelope
			err = decodeJSONStrict(data, &envelope)
			if err == nil && envelope.V != EnvelopeVersion {
				err = fmt.Errorf("%w: 密文格式版本 %d", ErrSchema, envelope.V)
			}
			if err == nil && key != nil {
				_, err = DecryptCredential(key.Key, accountSourceFromPath(p), key.KeyVersion, data)
				if errors.Is(err, ErrKeyMismatch) {
					err = nil
				}
			}
		}
		if err != nil {
			return fmt.Errorf("%w: %s: %w", ErrCorruptRepo, p, err)
		}
	}
	return nil
}

func validateKeymapBytes(data []byte) error { _, _, err := keymap.Parse(data); return err }

func (g *Git) UpdateHead(ctx context.Context, old, next string) error {
	if g.BeforeUpdate != nil {
		if err := g.BeforeUpdate(old, next); err != nil {
			return err
		}
	}
	return g.repositoryBackend.UpdateHead(ctx, old, next)
}
