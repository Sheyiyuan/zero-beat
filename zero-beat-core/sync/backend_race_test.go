package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type competingPushBackend struct {
	repositoryBackend
	beforePush func() error
	pushes     int
}

func (b *competingPushBackend) Fetch(ctx context.Context) error {
	if err := b.repositoryBackend.Fetch(ctx); err != nil {
		return fmt.Errorf("candidate fetch after %d pushes: %w", b.pushes, err)
	}
	return nil
}

func (b *competingPushBackend) Push(ctx context.Context) error {
	b.pushes++
	if err := b.beforePush(); err != nil {
		return fmt.Errorf("competing device: %w", err)
	}
	if err := b.repositoryBackend.Push(ctx); err != nil {
		return fmt.Errorf("candidate push: %w", err)
	}
	return nil
}

// The competitor advances the real remote after our fetch and before our push.
// This exercises server rejection, shared retries, and the three-round bound.
func TestConcurrentRemotePushRetries(t *testing.T) {
	for _, backend := range []Backend{BackendCLI, BackendGoGit} {
		for _, competeEveryTime := range []bool{false, true} {
			name := string(backend) + "/once"
			if competeEveryTime {
				name = string(backend) + "/exhausted"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				if backend == BackendGoGit {
					t.Setenv("PATH", t.TempDir())
				}
				remote := embeddedRemote(t)
				a, b := newTestDevice(t, "a"), newTestDevice(t, "b")
				for _, d := range []*testDevice{a, b} {
					d.client.opts.Backend = backend
					if _, err := d.client.Init(ctx, remote, "main", true, "a sufficiently long passphrase"); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := a.store.CreatePlaylist("local edit"); err != nil {
					t.Fatal(err)
				}
				cfg, err := LoadConfig(a.client.opts.ConfigPath)
				if err != nil {
					t.Fatal(err)
				}
				key, err := LoadKeyState(a.client.opts.KeyPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := a.client.acquire(ctx); err != nil {
					t.Fatal(err)
				}
				g, err := a.client.prepareGit(ctx, cfg, "")
				if err != nil {
					a.client.release()
					t.Fatal(err)
				}
				competing := &competingPushBackend{repositoryBackend: g.repositoryBackend}
				competing.beforePush = func() error {
					if !competeEveryTime && competing.pushes > 1 {
						return nil
					}
					if _, err := b.store.CreatePlaylist("remote edit"); err != nil {
						return err
					}
					_, err := b.client.Sync(ctx)
					return err
				}
				g.repositoryBackend = competing
				_, err = a.client.syncWithLock(ctx, cfg, key, g, syncRunOptions{})
				a.client.release()
				wantPushes, wantPlaylists := 2, 2
				if competeEveryTime {
					wantPushes, wantPlaylists = 3, 4
					if !errors.Is(err, ErrPushRejected) {
						t.Fatalf("expected bounded push rejection, got %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if competing.pushes != wantPushes {
					t.Fatalf("push attempts: %d, want %d", competing.pushes, wantPushes)
				}
				// Once the competitor stops, a normal retry converges without losing edits.
				if _, err := a.client.Sync(ctx); err != nil {
					t.Fatal(err)
				}
				if _, err := b.client.Sync(ctx); err != nil {
					t.Fatal(err)
				}
				items, err := b.store.ListPlaylistSnapshots("local")
				if err != nil || len(items) != wantPlaylists {
					t.Fatalf("lost edits: count=%d err=%v", len(items), err)
				}
			})
		}
	}
}

func TestBackendTreeOmitsEmptyDirectories(t *testing.T) {
	var expected plumbing.Hash
	for _, backend := range []Backend{BackendCLI, BackendGoGit} {
		t.Run(string(backend), func(t *testing.T) {
			ctx := context.Background()
			g := testBackend(t, backend)
			if err := os.MkdirAll(filepath.Join(g.Dir, "playlists", "empty"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := g.writeFile("empty-file", nil, true); err != nil {
				t.Fatal(err)
			}
			h, err := g.WriteCommit(ctx, nil, "empty file, no empty directories", g.now())
			if err != nil {
				t.Fatal(err)
			}
			r, err := git.PlainOpen(g.Dir)
			if err != nil {
				t.Fatal(err)
			}
			c, err := r.CommitObject(plumbing.NewHash(h))
			if err != nil {
				t.Fatal(err)
			}
			if expected != plumbing.ZeroHash && expected != c.TreeHash {
				t.Fatalf("CLI and go-git trees differ: %s / %s", expected, c.TreeHash)
			}
			expected = c.TreeHash
			data, exists, err := g.showFile(ctx, h, "empty-file")
			if err != nil || !exists || len(data) != 0 {
				t.Fatal("empty file confused with deletion", err)
			}
		})
	}
}

func TestBackendChangingTargetBranch(t *testing.T) {
	for _, backend := range []Backend{BackendCLI, BackendGoGit} {
		t.Run(string(backend), func(t *testing.T) {
			ctx := context.Background()
			remote := embeddedRemote(t)
			a := newTestDevice(t, "a")
			a.client.opts.Backend = backend
			if _, err := a.client.Init(ctx, remote, "main", true, "a sufficiently long passphrase"); err != nil {
				t.Fatal(err)
			}
			if err := a.client.Configure(remote, "another", true); err != nil {
				t.Fatal(err)
			}
			if _, err := a.store.CreatePlaylist("on another branch"); err != nil {
				t.Fatal(err)
			}
			if _, err := a.client.Sync(ctx); err != nil {
				t.Fatal(err)
			}
			r, err := git.PlainOpen(remote)
			if err != nil {
				t.Fatal(err)
			}
			main, err := r.Reference(plumbing.NewBranchReferenceName("main"), true)
			if err != nil {
				t.Fatal(err)
			}
			other, err := r.Reference(plumbing.NewBranchReferenceName("another"), true)
			if err != nil || main.Hash() == other.Hash() {
				t.Fatal("target branch did not advance independently", err)
			}
		})
	}
}

func TestBackendRejectsUnsupportedTreeEntries(t *testing.T) {
	for _, backend := range []Backend{BackendCLI, BackendGoGit} {
		t.Run(string(backend), func(t *testing.T) {
			g := testBackend(t, backend)
			r, err := git.PlainOpen(g.Dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, mode := range []filemode.FileMode{filemode.Symlink, filemode.Submodule, filemode.Dir} {
				tree := object.Tree{Entries: []object.TreeEntry{{Name: "bad", Mode: mode, Hash: plumbing.ZeroHash}}}
				obj := r.Storer.NewEncodedObject()
				if err := tree.Encode(obj); err != nil {
					t.Fatal(err)
				}
				h, err := r.Storer.SetEncodedObject(obj)
				if err != nil {
					t.Fatal(err)
				}
				commit := object.Commit{TreeHash: h, Author: object.Signature{Name: "test", When: g.now()}, Message: "invalid tree"}
				commit.Committer = commit.Author
				obj = r.Storer.NewEncodedObject()
				if err := commit.Encode(obj); err != nil {
					t.Fatal(err)
				}
				h, err = r.Storer.SetEncodedObject(obj)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := g.readTree(context.Background(), h.String()); err == nil {
					t.Fatalf("accepted unsupported or missing tree entry mode %s", mode)
				}
			}
		})
	}
}
