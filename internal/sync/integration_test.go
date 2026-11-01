package sync

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

type testDevice struct {
	name   string
	dir    string
	store  *store.Store
	client *Client
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"LC_ALL=C",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newTestDevice(t *testing.T, name string) *testDevice {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "zero-beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	opts := Options{
		Store:      st,
		Dir:        filepath.Join(dir, "sync"),
		ConfigPath: filepath.Join(dir, "sync.toml"),
		KeyPath:    filepath.Join(dir, "sync-key"),
		TokenPath:  filepath.Join(dir, "sync-token"),
		KeymapPath: filepath.Join(dir, "keymap.toml"),
	}
	return &testDevice{name: name, dir: dir, store: st, client: NewClient(opts)}
}

func setupBareRemote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "", "init", "--bare", "-b", "main", dir)
	return dir
}

func TestSyncTwoDevices(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	ctx := context.Background()
	remote := setupBareRemote(t)
	a := newTestDevice(t, "A")
	b := newTestDevice(t, "B")
	pass := "a sufficiently long passphrase"

	local, err := a.store.CreatePlaylist("通勤")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.AddSongToPlaylist(local.ID, store.Song{Source: "bilibili", RemoteID: "BV1", BVID: "BV1", Page: 1, Title: "曲名", Duration: 200}); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SaveAccountAt("bilibili", []byte(`{"SESSDATA":"aaa"}`), 1761980001); err != nil {
		t.Fatal(err)
	}

	out, err := a.client.Init(ctx, remote, "main", true, pass)
	if err != nil {
		t.Fatalf("A 初始化失败: %v", err)
	}
	if out.Joined {
		t.Fatal("A 应新建仓库而不是加入")
	}
	if !out.Synced.Pushed {
		t.Fatal("A 首次同步应推送成功")
	}

	joined, err := b.client.Init(ctx, remote, "main", true, pass)
	if err != nil {
		t.Fatalf("B 加入失败: %v", err)
	}
	if !joined.Joined {
		t.Fatal("B 应识别为加入已有仓库")
	}
	playlists, err := b.store.ListPlaylists()
	if err != nil {
		t.Fatal(err)
	}
	if len(playlists) != 1 || playlists[0].Title != "通勤" {
		t.Fatalf("B 应导入 A 的歌单，实际 %+v", playlists)
	}
	payload, _, err := b.store.LoadAccountAt("bilibili")
	if err != nil || string(payload) != `{"SESSDATA":"aaa"}` {
		t.Fatalf("B 应恢复登录态，实际 %s err=%v", payload, err)
	}

	// A 增加曲目后推送，B 拉取。
	if err := a.store.AddSongToPlaylist(local.ID, store.Song{Source: "netease", RemoteID: "42", Page: 1, Title: "第二首", Duration: 100}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.client.Sync(ctx); err != nil {
		t.Fatalf("A 同步失败: %v", err)
	}
	if _, err := b.client.Sync(ctx); err != nil {
		t.Fatalf("B 同步失败: %v", err)
	}
	snap, err := b.store.PlaylistSnapshot(local.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Songs) != 2 {
		t.Fatalf("B 应拉到两首歌，实际 %+v", snap.Songs)
	}

	// 无改动重复同步不应产生提交。
	again, err := b.client.Sync(ctx)
	if err != nil {
		t.Fatalf("B 再次同步失败: %v", err)
	}
	if again.Committed {
		t.Fatal("没有改动时不应产生提交")
	}

	// 同一歌单两端都改：进入冲突，按实体选择远程。
	if err := a.store.RenamePlaylist(local.ID, "A 改名"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.client.Sync(ctx); err != nil {
		t.Fatalf("A 改名同步失败: %v", err)
	}
	if err := b.store.RenamePlaylist(local.ID, "B 改名"); err != nil {
		t.Fatal(err)
	}
	_, err = b.client.Sync(ctx)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("应返回冲突，实际 %v", err)
	}
	index, err := b.client.Conflicts()
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entities) != 1 || index.Entities[0].Entity != "playlist:"+local.ID {
		t.Fatalf("冲突实体不符: %+v", index.Entities)
	}
	choices := map[string]Choice{index.Entities[0].Entity: KeepRemote}
	if _, err := b.client.Resolve(ctx, index.TxID, choices); err != nil {
		t.Fatalf("解决冲突失败: %v", err)
	}
	p, err := b.store.Playlist(local.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "A 改名" {
		t.Fatalf("选择远程后标题应为 A 改名，实际 %q", p.Title)
	}
	if _, err := b.client.Sync(ctx); err != nil {
		t.Fatalf("解决后同步失败: %v", err)
	}
	// A 拉到解决后的结果。
	if _, err := a.client.Sync(ctx); err != nil {
		t.Fatalf("A 拉取失败: %v", err)
	}
	if p, _ := a.store.Playlist(local.ID); p.Title != "A 改名" {
		t.Fatalf("A 应保持 A 改名，实际 %q", p.Title)
	}

	// 换口令：A 轮换后，B 的旧密钥应被识别为不可用，重新输入新口令后恢复。
	if _, err := a.client.SetPassphrase(ctx, "a brand new passphrase"); err != nil {
		t.Fatalf("A 换口令失败: %v", err)
	}
	res, err := b.client.Sync(ctx)
	if err != nil {
		t.Fatalf("B 用旧密钥同步不应整体失败: %v", err)
	}
	if !errors.Is(res.KeyIssue, ErrKeyMismatch) {
		t.Fatalf("B 应报告密钥不匹配，实际 %v", res.KeyIssue)
	}
	rejoined, err := b.client.Init(ctx, remote, "main", true, "a brand new passphrase")
	if err != nil {
		t.Fatalf("B 用新口令加入失败: %v", err)
	}
	if !rejoined.Joined {
		t.Fatal("应识别为加入已有仓库")
	}
	status := b.client.Status()
	if status.KeyVersion != 2 || status.KeyIssue != "" {
		t.Fatalf("B 应缓存新版本密钥，实际 %+v", status)
	}
}

func TestSyncConflictKeepBoth(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	ctx := context.Background()
	remote := setupBareRemote(t)
	a := newTestDevice(t, "A")
	b := newTestDevice(t, "B")
	pass := "another sufficiently long passphrase"

	local, _ := a.store.CreatePlaylist("共享")
	if _, err := a.client.Init(ctx, remote, "main", true, pass); err != nil {
		t.Fatal(err)
	}
	if _, err := b.client.Init(ctx, remote, "main", true, pass); err != nil {
		t.Fatal(err)
	}
	// 两端各自加不同的歌，然后同时改名触发冲突。
	if err := a.store.AddSongToPlaylist(local.ID, store.Song{Source: "bilibili", RemoteID: "A1", BVID: "A1", Page: 1, Title: "A 的歌"}); err != nil {
		t.Fatal(err)
	}
	if err := a.store.RenamePlaylist(local.ID, "A 版"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.client.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.store.AddSongToPlaylist(local.ID, store.Song{Source: "bilibili", RemoteID: "B1", BVID: "B1", Page: 1, Title: "B 的歌"}); err != nil {
		t.Fatal(err)
	}
	if err := b.store.RenamePlaylist(local.ID, "B 版"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.client.Sync(ctx); !errors.Is(err, ErrConflict) {
		t.Fatalf("应冲突，实际 %v", err)
	}
	index, err := b.client.Conflicts()
	if err != nil {
		t.Fatal(err)
	}
	entity := index.Entities[0]
	if !entity.SupportsBoth() {
		t.Fatalf("歌单冲突应支持保留两者: %+v", entity)
	}
	choices := map[string]Choice{entity.Entity: KeepBoth}
	if _, err := b.client.Resolve(ctx, index.TxID, choices); err != nil {
		t.Fatalf("保留两者失败: %v", err)
	}
	playlists, err := b.store.ListPlaylists()
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, p := range playlists {
		titles = append(titles, p.Title)
	}
	joined := strings.Join(titles, "|")
	if !strings.Contains(joined, "B 版") || !strings.Contains(joined, "A 版（副本）") {
		t.Fatalf("应同时保留本地与远端副本，实际 %v", titles)
	}
}

func TestSyncPushRejectedRetries(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	// 用不可写的本地裸仓库模拟推送失败：应返回分类错误而不是 force push。
	ctx := context.Background()
	remote := setupBareRemote(t)
	a := newTestDevice(t, "A")
	local, _ := a.store.CreatePlaylist("x")
	_ = local
	if _, err := a.client.Init(ctx, remote, "main", true, "yet another long passphrase"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(remote, 0o500); err != nil {
		t.Skip("无法修改仓库权限")
	}
	t.Cleanup(func() { _ = os.Chmod(remote, 0o700) })
	p, _ := a.store.CreatePlaylist("新歌单")
	_ = p
	_, err := a.client.Sync(ctx)
	if err == nil {
		t.Skip("当前环境仍可写入只读目录，跳过")
	}
	if !errors.Is(err, ErrPushRejected) && !errors.Is(err, ErrAuth) && !errors.Is(err, ErrNetwork) {
		t.Fatalf("推送失败应分类，实际 %v", err)
	}
}

func TestJoinKeepsLocalPlaylists(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	ctx := context.Background()
	remote := setupBareRemote(t)
	a := newTestDevice(t, "A")
	b := newTestDevice(t, "B")
	pass := "join keeps local passphrase"

	if _, err := a.store.CreatePlaylist("A 的歌单"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.client.Init(ctx, remote, "main", true, pass); err != nil {
		t.Fatal(err)
	}
	// B 在启用同步前已经用过一段时间。
	if _, err := b.store.CreatePlaylist("B 的旧歌单"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.client.Init(ctx, remote, "main", true, pass); err != nil {
		t.Fatalf("B 加入失败: %v", err)
	}
	playlists, err := b.store.ListPlaylists()
	if err != nil {
		t.Fatal(err)
	}
	if len(playlists) != 2 {
		t.Fatalf("加入后应保留本地歌单并导入远端，实际 %+v", playlists)
	}
	if _, err := a.client.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	playlists, err = a.store.ListPlaylists()
	if err != nil {
		t.Fatal(err)
	}
	if len(playlists) != 2 {
		t.Fatalf("A 应拉到 B 的旧歌单，实际 %+v", playlists)
	}
}

func TestAccountConflictUsesUpdatedAt(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	ctx := context.Background()
	remote := setupBareRemote(t)
	a := newTestDevice(t, "A")
	b := newTestDevice(t, "B")
	pass := "account conflict passphrase"
	if err := a.store.SaveAccountAt("bilibili", []byte(`{"SESSDATA":"base"}`), 100); err != nil {
		t.Fatal(err)
	}
	if _, err := a.client.Init(ctx, remote, "main", true, pass); err != nil {
		t.Fatal(err)
	}
	if _, err := b.client.Init(ctx, remote, "main", true, pass); err != nil {
		t.Fatal(err)
	}
	// 远端先写 200，本地随后写 300：导出时两边都改过同一个密文文件，
	// 应在合并阶段按 updated_at 自动选 300，不进入冲突页。
	if err := a.store.SaveAccountAt("bilibili", []byte(`{"SESSDATA":"from-a"}`), 200); err != nil {
		t.Fatal(err)
	}
	if _, err := a.client.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.store.SaveAccountAt("bilibili", []byte(`{"SESSDATA":"from-b"}`), 300); err != nil {
		t.Fatal(err)
	}
	res, err := b.client.Sync(ctx)
	if err != nil {
		t.Fatalf("登录态冲突不应中断同步: %v", err)
	}
	if errors.Is(res.KeyIssue, ErrConflict) {
		t.Fatal("登录态冲突不应进入冲突页")
	}
	payload, updated, err := b.store.LoadAccountAt("bilibili")
	if err != nil || string(payload) != `{"SESSDATA":"from-b"}` || updated != 300 {
		t.Fatalf("本地较新应保留本地: %s %d err=%v", payload, updated, err)
	}
	if _, err := a.client.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	payload, updated, _ = a.store.LoadAccountAt("bilibili")
	if string(payload) != `{"SESSDATA":"from-b"}` || updated != 300 {
		t.Fatalf("A 应拉到 B 的较新登录态: %s %d", payload, updated)
	}
}

func TestKeymapConflictResolveRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	ctx := context.Background()
	remote := setupBareRemote(t)
	a := newTestDevice(t, "A")
	b := newTestDevice(t, "B")
	pass := "keymap conflict passphrase"
	writeKeymap := func(dev *testDevice, key string) {
		t.Helper()
		content := "[keys]\nquit = [\"" + key + "\"]\n"
		if err := os.WriteFile(dev.client.opts.KeymapPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeKeymap(a, "c")
	if _, err := a.client.Init(ctx, remote, "main", true, pass); err != nil {
		t.Fatal(err)
	}
	if _, err := b.client.Init(ctx, remote, "main", true, pass); err != nil {
		t.Fatal(err)
	}
	// B 加入时导入了 A 的键位。
	data, err := os.ReadFile(b.client.opts.KeymapPath)
	if err != nil || !strings.Contains(string(data), `"c"`) {
		t.Fatalf("B 应导入 A 的键位: %q err=%v", data, err)
	}
	writeKeymap(a, "z")
	if _, err := a.client.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	writeKeymap(b, "x")
	if _, err := b.client.Sync(ctx); !errors.Is(err, ErrConflict) {
		t.Fatalf("键位两端都改应冲突，实际 %v", err)
	}
	index, err := b.client.Conflicts()
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entities) != 1 || index.Entities[0].Kind != KindKeymap {
		t.Fatalf("应只有键位冲突: %+v", index.Entities)
	}
	if _, err := b.client.Resolve(ctx, index.TxID, map[string]Choice{index.Entities[0].Entity: KeepRemote}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(b.client.opts.KeymapPath)
	if err != nil || !strings.Contains(string(data), `"z"`) {
		t.Fatalf("选择远程后本地键位应更新: %q err=%v", data, err)
	}
}

func TestResolveReplaysAccountConflict(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	ctx := context.Background()
	remote := setupBareRemote(t)
	a := newTestDevice(t, "A")
	b := newTestDevice(t, "B")
	pass := "mixed conflict passphrase"
	local, err := a.store.CreatePlaylist("共享")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.SaveAccountAt("bilibili", []byte(`{"SESSDATA":"base"}`), 100); err != nil {
		t.Fatal(err)
	}
	if _, err := a.client.Init(ctx, remote, "main", true, pass); err != nil {
		t.Fatal(err)
	}
	if _, err := b.client.Init(ctx, remote, "main", true, pass); err != nil {
		t.Fatal(err)
	}
	// A 同时改歌单与登录态（200），B 同时改歌单与登录态（300）。
	if err := a.store.RenamePlaylist(local.ID, "A 版"); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SaveAccountAt("bilibili", []byte(`{"SESSDATA":"from-a"}`), 200); err != nil {
		t.Fatal(err)
	}
	if _, err := a.client.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.store.RenamePlaylist(local.ID, "B 版"); err != nil {
		t.Fatal(err)
	}
	if err := b.store.SaveAccountAt("bilibili", []byte(`{"SESSDATA":"from-b"}`), 300); err != nil {
		t.Fatal(err)
	}
	if _, err := b.client.Sync(ctx); !errors.Is(err, ErrConflict) {
		t.Fatalf("歌单冲突应进入冲突页，实际 %v", err)
	}
	index, err := b.client.Conflicts()
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entities) != 1 || index.Entities[0].Kind != KindPlaylist {
		t.Fatalf("凭据冲突应被自动处理，只剩歌单冲突: %+v", index.Entities)
	}
	if _, err := b.client.Resolve(ctx, index.TxID, map[string]Choice{index.Entities[0].Entity: KeepRemote}); err != nil {
		t.Fatalf("解决冲突失败: %v", err)
	}
	if p, _ := b.store.Playlist(local.ID); p.Title != "A 版" {
		t.Fatalf("应保留远端歌单标题，实际 %q", p.Title)
	}
	payload, updated, _ := b.store.LoadAccountAt("bilibili")
	if string(payload) != `{"SESSDATA":"from-b"}` || updated != 300 {
		t.Fatalf("重放合并时应保留较新的本地登录态: %s %d", payload, updated)
	}
}

func TestDeleteVersusModifyConflict(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	ctx := context.Background()
	remote := setupBareRemote(t)
	a := newTestDevice(t, "A")
	b := newTestDevice(t, "B")
	pass := "delete modify passphrase"
	local, err := a.store.CreatePlaylist("会被删除")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.client.Init(ctx, remote, "main", true, pass); err != nil {
		t.Fatal(err)
	}
	if _, err := b.client.Init(ctx, remote, "main", true, pass); err != nil {
		t.Fatal(err)
	}
	// A 删除；B 改名。
	if err := a.store.DeletePlaylist(local.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.client.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.store.RenamePlaylist(local.ID, "B 改过"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.client.Sync(ctx); !errors.Is(err, ErrConflict) {
		t.Fatalf("删除 vs 修改应冲突，实际 %v", err)
	}
	index, err := b.client.Conflicts()
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entities) != 1 {
		t.Fatalf("应有一项冲突: %+v", index.Entities)
	}
	entity := index.Entities[0]
	if !entity.DeletedRemote || entity.Remote {
		t.Fatalf("远端应为删除状态: %+v", entity)
	}
	if entity.SupportsBoth() {
		t.Fatal("一侧删除时不应提供保留两者")
	}
	if _, err := b.client.Resolve(ctx, index.TxID, map[string]Choice{entity.Entity: KeepRemote}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.store.Playlist(local.ID); err == nil {
		t.Fatal("选择远程删除后本地歌单应被移除")
	}
	archived, _ := filepath.Glob(filepath.Join(b.dir, "sync", "archive", "*", "deleted-playlists", "*.json"))
	if len(archived) != 1 {
		t.Fatalf("删除前应归档本地版本，实际 %v", archived)
	}
}
