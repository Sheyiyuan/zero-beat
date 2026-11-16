package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

func embeddedRemote(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "remote.git")
	_, err := git.PlainInitWithOptions(path, &git.PlainInitOptions{Bare: true, InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")}})
	if err != nil {
		t.Fatal(err)
	}
	return path
}
func TestEmbeddedSyncWithoutGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ZERO_SYNC_BACKEND", "auto")
	ctx := context.Background()
	remote := embeddedRemote(t)
	a := newTestDevice(t, "a")
	b := newTestDevice(t, "b")
	p, err := a.store.CreatePlaylist("original")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.client.Init(ctx, remote, "main", true, "a sufficiently long passphrase"); err != nil {
		t.Fatal(err)
	}
	if a.client.Status().ActualBackend != BackendGoGit {
		t.Fatal(a.client.Status())
	}
	if _, err = b.client.Init(ctx, remote, "main", true, "a sufficiently long passphrase"); err != nil {
		t.Fatal(err)
	}
	if err = a.store.RenamePlaylist(p.ID, "A"); err != nil {
		t.Fatal(err)
	}
	if err = b.store.RenamePlaylist(p.ID, "B"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.client.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = b.client.Sync(ctx); !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	index, err := b.client.Conflicts()
	if err != nil {
		t.Fatal(err)
	}
	choices := map[string]Choice{}
	for _, e := range index.Entities {
		choices[e.Entity] = KeepBoth
	}
	if _, err = b.client.Resolve(ctx, index.TxID, choices); err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainOpen(remote)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if len(commit.ParentHashes) != 2 || commit.ParentHashes[0].String() != index.LocalHead || commit.ParentHashes[1].String() != index.MergeHead {
		t.Fatalf("incorrect parents: %v", commit.ParentHashes)
	}
	if _, err = b.client.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	again, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	if again.Hash() != head.Hash() {
		t.Fatal("second sync created another commit")
	}
}
func TestBackendSelection(t *testing.T) {
	t.Setenv("ZERO_SYNC_BACKEND", "gogit")
	requested, actual, err := selectBackend(BackendCLI, BackendGoGit)
	if err != nil || requested != BackendCLI || actual != BackendCLI {
		t.Fatal(requested, actual, err)
	}
	_, actual, err = selectBackend("", BackendCLI)
	if err != nil || actual != BackendGoGit {
		t.Fatal(actual, err)
	}
	if _, _, err = selectBackend("bad", ""); err == nil {
		t.Fatal("invalid backend accepted")
	}
}
func TestWritePreflightPreservesUnknownFields(t *testing.T) {
	for _, backend := range []Backend{BackendCLI, BackendGoGit} {
		t.Run(string(backend), func(t *testing.T) {
			t.Setenv("ZERO_SYNC_BACKEND", string(backend))
			ctx := context.Background()
			remote := embeddedRemote(t)
			a := newTestDevice(t, "a")
			p, err := a.store.CreatePlaylist("safe")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = a.client.Init(ctx, remote, "main", true, "a sufficiently long passphrase"); err != nil {
				t.Fatal(err)
			}
			g := NewGit(a.client.repoPath(), remote, "main")
			if err = g.EnsureRepo(ctx); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(g.Dir, PlaylistPath(p.ID))
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			raw = append([]byte(`{"future_field":true,`), raw[1:]...)
			if err = os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err = g.CommitAll(ctx, "future schema"); err != nil {
				t.Fatal(err)
			}
			before, _ := g.Head(ctx)
			if _, err = a.client.Sync(ctx); !errors.Is(err, ErrCorruptRepo) {
				t.Fatalf("got %v", err)
			}
			after, _ := g.Head(ctx)
			if before != after {
				t.Fatal("unknown field was rewritten")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(raw) {
				t.Fatal("unknown field was lost", err)
			}
		})
	}
}
func TestManagedSchemaPreflightWithoutKey(t *testing.T) {
	for _, backend := range []Backend{BackendCLI, BackendGoGit} {
		t.Run(string(backend), func(t *testing.T) {
			g := testBackend(t, backend)
			ctx := context.Background()
			cases := map[string]string{
				MetaFile:                      `{"v":3}`,
				KeymapFile:                    "future_option = true\n",
				AnnotationsDir + "/song.json": `{"song_id":"song","future_field":true}`,
				AccountPath("bilibili"):       `{"v":1,"source":"bilibili","future_field":true}`,
			}
			for path, content := range cases {
				if err := g.writeFile(path, []byte(content), true); err != nil {
					t.Fatal(err)
				}
				h, err := g.WriteCommit(ctx, nil, "future schema", g.now())
				if err != nil {
					t.Fatal(err)
				}
				if err := (&Client{}).validateTree(ctx, g, h, nil); !errors.Is(err, ErrCorruptRepo) {
					t.Fatalf("%s: future schema accepted without a key: %v", path, err)
				}
				if err := g.writeFile(path, nil, false); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestConflictRejectsChangedInputs(t *testing.T) {
	t.Setenv("ZERO_SYNC_BACKEND", "gogit")
	ctx := context.Background()
	remote := embeddedRemote(t)
	a := newTestDevice(t, "a")
	b := newTestDevice(t, "b")
	p, _ := a.store.CreatePlaylist("base")
	for _, d := range []*testDevice{a, b} {
		if _, err := d.client.Init(ctx, remote, "main", true, "a sufficiently long passphrase"); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.store.RenamePlaylist(p.ID, "remote"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.client.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.store.RenamePlaylist(p.ID, "local"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.client.Sync(ctx); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	idx, err := b.client.Conflicts()
	if err != nil {
		t.Fatal(err)
	}
	choices := map[string]Choice{}
	for _, e := range idx.Entities {
		choices[e.Entity] = KeepRemote
	}
	if err = b.store.RenamePlaylist(p.ID, "later edit"); err != nil {
		t.Fatal(err)
	}
	if _, err = b.client.Resolve(ctx, idx.TxID, choices); !errors.Is(err, ErrStaleConflict) {
		t.Fatalf("got %v", err)
	}
	idx, err = b.client.Conflicts()
	if err != nil {
		t.Fatal(err)
	}
	idx.Version = 0
	if err = saveConflictIndex(b.client.opts.Dir, idx); err != nil {
		t.Fatal(err)
	}
	if _, err = b.client.Resolve(ctx, idx.TxID, choices); !errors.Is(err, ErrLegacyConflict) {
		t.Fatal(err)
	}
	if err = b.client.DiscardConflict(ctx, idx.TxID); err != nil {
		t.Fatal(err)
	}
}
func TestSyncLockCoversInitialization(t *testing.T) {
	a := newTestDevice(t, "a")
	lock, err := AcquireLock(filepath.Join(a.client.opts.Dir, "lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if _, err = a.client.Init(context.Background(), embeddedRemote(t), "main", true, "a sufficiently long passphrase"); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if _, err = os.Stat(a.client.repoPath()); !os.IsNotExist(err) {
		t.Fatal("repository created before lock")
	}
}

func testBackend(t *testing.T, backend Backend) *Git {
	t.Helper()
	dir := t.TempDir()
	g := NewGit(dir, embeddedRemote(t), "main")
	g.Backend = backend
	if backend == BackendGoGit {
		g.repositoryBackend = &goGit{dir: dir, remote: embeddedRemote(t), branch: "main"}
	}
	if err := g.EnsureRepo(context.Background()); err != nil {
		t.Fatal(err)
	}
	return g
}
func TestBackendMergeTreesAndHistory(t *testing.T) {
	var expected map[string][]byte
	for _, backend := range []Backend{BackendCLI, BackendGoGit} {
		t.Run(string(backend), func(t *testing.T) {
			ctx := context.Background()
			g := testBackend(t, backend)
			write := func(name, value string) {
				t.Helper()
				if err := g.writeFile(name, []byte(value), true); err != nil {
					t.Fatal(err)
				}
			}
			commit := func(parents []string, msg string) string {
				t.Helper()
				h, err := g.WriteCommit(ctx, parents, msg, g.now())
				if err != nil {
					t.Fatal(err)
				}
				return h
			}
			write("entity", "base")
			base := commit(nil, "base")
			if err := g.UpdateHead(ctx, "", base); err != nil {
				t.Fatal(err)
			}
			write("left", "one")
			left := commit([]string{base}, "left")
			if err := g.ResetHardTo(ctx, base); err != nil {
				t.Fatal(err)
			}
			if err := g.Clean(ctx); err != nil {
				t.Fatal(err)
			}
			write("right", "two")
			right := commit([]string{base}, "right")
			if err := g.ResetHardTo(ctx, left); err != nil {
				t.Fatal(err)
			}
			if err := g.Clean(ctx); err != nil {
				t.Fatal(err)
			}
			if paths, err := g.Merge(ctx, right); err != nil || len(paths) > 0 {
				t.Fatal(paths, err)
			}
			head, err := g.Head(ctx)
			if err != nil {
				t.Fatal(err)
			}
			tree, err := g.readTree(ctx, head)
			if err != nil {
				t.Fatal(err)
			}
			if string(tree["left"]) != "one" || string(tree["right"]) != "two" {
				t.Fatal(tree)
			}
			if expected == nil {
				expected = tree
			} else if !reflect.DeepEqual(expected, tree) {
				t.Fatalf("different trees: %v / %v", expected, tree)
			}
			// Two independently created merge commits have two equally good bases.
			one := commit([]string{left, right}, "merge one")
			two := commit([]string{right, left}, "merge two")
			if err = g.ResetHardTo(ctx, one); err != nil {
				t.Fatal(err)
			}
			if _, err = g.Merge(ctx, two); !errors.Is(err, ErrUnsupportedHistory) {
				t.Fatalf("multiple bases: %v", err)
			}
			unrelated := commit(nil, "unrelated")
			if _, err = g.Merge(ctx, unrelated); !errors.Is(err, ErrUnsupportedHistory) {
				t.Fatalf("unrelated: %v", err)
			}
		})
	}
}
func TestBackendSwitchSameClone(t *testing.T) {
	ctx := context.Background()
	remote := embeddedRemote(t)
	a := newTestDevice(t, "a")
	a.client.opts.Backend = BackendCLI
	if _, err := a.client.Init(ctx, remote, "main", true, "a sufficiently long passphrase"); err != nil {
		t.Fatal(err)
	}
	var previous plumbing.Hash
	for _, backend := range []Backend{BackendGoGit, BackendCLI, BackendGoGit} {
		a.client.opts.Backend = backend
		if _, err := a.client.Sync(ctx); err != nil {
			t.Fatal(backend, err)
		}
		repo, err := git.PlainOpen(remote)
		if err != nil {
			t.Fatal(err)
		}
		head, err := repo.Head()
		if err != nil {
			t.Fatal(err)
		}
		if previous != plumbing.ZeroHash && head.Hash() != previous {
			t.Fatal("switching backend created a commit")
		}
		previous = head.Hash()
	}
}
func TestRecoverBeforeImportKeepsLocalAndRemoteEdits(t *testing.T) {
	for _, backend := range []Backend{BackendCLI, BackendGoGit} {
		t.Run(string(backend), func(t *testing.T) {
			ctx := context.Background()
			remote := embeddedRemote(t)
			a := newTestDevice(t, "a")
			b := newTestDevice(t, "b")
			a.client.opts.Backend = backend
			b.client.opts.Backend = backend
			p, _ := a.store.CreatePlaylist("base")
			for _, d := range []*testDevice{a, b} {
				if _, err := d.client.Init(ctx, remote, "main", true, "a sufficiently long passphrase"); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.store.RenamePlaylist(p.ID, "remote edit"); err != nil {
				t.Fatal(err)
			}
			if _, err := a.client.Sync(ctx); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(b.client.opts.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			g, err := b.client.prepareGit(ctx, cfg, "")
			if err != nil {
				t.Fatal(err)
			}
			if err = g.Fetch(ctx); err != nil {
				t.Fatal(err)
			}
			before, err := g.Head(ctx)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := b.client.inputDigest()
			if err != nil {
				t.Fatal(err)
			}
			tx := transaction{Phase: "merging", Before: before, Input: digest, Backend: backend}
			if err = b.client.saveTransaction(tx); err != nil {
				t.Fatal(err)
			}
			g.BeforeUpdate = func(old, next string) error { tx.Candidate = next; return b.client.saveTransaction(tx) }
			if _, err = g.Merge(ctx, "FETCH_HEAD"); err != nil {
				t.Fatal(err)
			}
			// Simulate death after the ref update but before the DB import, and an edit
			// by the user before restarting sync.
			if _, err = b.store.CreatePlaylist("new local edit"); err != nil {
				t.Fatal(err)
			}
			b.client = NewClient(b.client.opts)
			if _, err = b.client.Sync(ctx); err != nil {
				t.Fatal(err)
			}
			snapshots, err := b.store.ListPlaylistSnapshots("local", "mixin")
			if err != nil {
				t.Fatal(err)
			}
			titles := map[string]bool{}
			for _, p := range snapshots {
				titles[p.Title] = true
			}
			if !titles["remote edit"] || !titles["new local edit"] {
				t.Fatal(titles)
			}
		})
	}
}
func TestBackendRefCAS(t *testing.T) {
	for _, backend := range []Backend{BackendCLI, BackendGoGit} {
		t.Run(string(backend), func(t *testing.T) {
			g := testBackend(t, backend)
			ctx := context.Background()
			if err := g.writeFile("file", []byte("x"), true); err != nil {
				t.Fatal(err)
			}
			first, err := g.WriteCommit(ctx, nil, "first", g.now())
			if err != nil {
				t.Fatal(err)
			}
			if err = g.UpdateHead(ctx, "", first); err != nil {
				t.Fatal(err)
			}
			second, err := g.WriteCommit(ctx, []string{first}, "second", g.now())
			if err != nil {
				t.Fatal(err)
			}
			if err = g.UpdateHead(ctx, "", second); err == nil {
				t.Fatal("overwrote an existing head with an absent-head expectation")
			}
			head, err := g.Head(ctx)
			if err != nil || head != first {
				t.Fatal(head, err)
			}
		})
	}
}

func TestRotationAndInterruptedRecovery(t *testing.T) {
	for _, backend := range []Backend{BackendCLI, BackendGoGit} {
		t.Run(string(backend), func(t *testing.T) {
			ctx := context.Background()
			remote := embeddedRemote(t)
			a := newTestDevice(t, "a")
			a.client.opts.Backend = backend
			if err := a.store.SaveAccountAt("bilibili", []byte(`{"SESSDATA":"secret"}`), 100); err != nil {
				t.Fatal(err)
			}
			if _, err := a.client.Init(ctx, remote, "main", true, "the original long passphrase"); err != nil {
				t.Fatal(err)
			}
			if _, err := a.client.SetPassphrase(ctx, "the replacement long passphrase"); err != nil {
				t.Fatal(err)
			}
			b := newTestDevice(t, "b")
			b.client.opts.Backend = backend
			if _, err := b.client.Init(ctx, remote, "main", true, "the replacement long passphrase"); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(a.client.opts.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			g, err := a.client.prepareGit(ctx, cfg, "")
			if err != nil {
				t.Fatal(err)
			}
			before, err := g.Head(ctx)
			if err != nil {
				t.Fatal(err)
			}
			oldKey, err := os.ReadFile(a.client.opts.KeyPath)
			if err != nil {
				t.Fatal(err)
			}
			oldMeta, err := os.ReadFile(filepath.Join(g.Dir, MetaFile))
			if err != nil {
				t.Fatal(err)
			}
			meta, key, err := NewMeta("another interrupted passphrase", a.client.now())
			if err != nil {
				t.Fatal(err)
			}
			key.RemoteHash = RemoteHash(remote)
			key.KeyVersion = 3
			meta.KeyVersion = 3
			nextMeta, err := meta.Encode()
			if err != nil {
				t.Fatal(err)
			}
			nextKey, err := key.encode()
			if err != nil {
				t.Fatal(err)
			}
			tx := rotationJournal{Before: before, RemoteHash: RemoteHash(remote), OldKey: oldKey, OldMeta: oldMeta, NewKey: nextKey, NewMeta: nextMeta}
			if err = a.client.saveRotation(tx); err != nil {
				t.Fatal(err)
			}
			if err = key.Save(a.client.opts.KeyPath); err != nil {
				t.Fatal(err)
			}
			if err = g.writeFile(MetaFile, nextMeta, true); err != nil {
				t.Fatal(err)
			}
			if _, err = g.CommitAll(ctx, "interrupted rotation"); err != nil {
				t.Fatal(err)
			}
			if _, err = a.client.Sync(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(a.client.opts.KeyPath)
			if err != nil || string(got) != string(oldKey) {
				t.Fatal("old key not restored", err)
			}
			if _, err = os.Stat(a.client.rotationPath()); !os.IsNotExist(err) {
				t.Fatal("rotation journal retained", err)
			}
		})
	}
}

func TestInterruptedJoinCannotExportEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	remote := embeddedRemote(t)
	a := newTestDevice(t, "a")
	a.client.opts.Backend = BackendGoGit
	if _, err := a.store.CreatePlaylist("remote playlist"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.client.Init(ctx, remote, "main", true, "a sufficiently long passphrase"); err != nil {
		t.Fatal(err)
	}
	b := newTestDevice(t, "b")
	b.client.opts.Backend = BackendGoGit
	if err := b.client.Configure(remote, "main", true); err != nil {
		t.Fatal(err)
	}
	key, err := LoadKeyState(a.client.opts.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = key.Save(b.client.opts.KeyPath); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(b.client.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	g, err := b.client.prepareGit(ctx, cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = g.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	candidate, _, err := g.RevParse(ctx, "FETCH_HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err = b.client.saveTransaction(transaction{Phase: "joining", Candidate: candidate, Backend: BackendGoGit}); err != nil {
		t.Fatal(err)
	}
	if err = g.ResetHardTo(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	// Death here leaves HEAD populated but the database empty.
	if _, err = b.client.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	playlists, err := b.store.ListPlaylistSnapshots("local")
	if err != nil {
		t.Fatal(err)
	}
	if len(playlists) != 1 || playlists[0].Title != "remote playlist" {
		t.Fatal(playlists)
	}
	repo, err := git.PlainOpen(remote)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	if head.Hash().String() != candidate {
		t.Fatal("join rewrote remote content")
	}
}
