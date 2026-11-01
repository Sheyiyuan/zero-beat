package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeRepoFile(t *testing.T, repo, path string, data []byte) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestImportRejectsSymlinkedPlaylist(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, PlaylistsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "outside.json")
	if err := os.WriteFile(target, []byte(`{"id":"local-1","title":"不该被读","kind":"local","created_at":1,"items":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(repo, PlaylistsDir, "local-1.json")); err != nil {
		t.Skip("当前环境不支持符号链接")
	}
	st := openTestStore(t)
	res, err := ImportSnapshot(ImportInput{Store: st, Dir: repo, KeepLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("符号链接应产生警告")
	}
	if _, err := st.Playlist("local-1"); err == nil {
		t.Fatal("不应通过符号链接导入歌单")
	}
}

func TestImportDeleteAnnotation(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	songID := "bilibili:BV1:1"
	writeRepoFile(t, repo, AnnotationPath(songID), []byte(`{"song_id":"bilibili:BV1:1","lyric_offset_ms":-500}`))
	st := openTestStore(t)
	if _, err := ImportSnapshot(ImportInput{Store: st, Dir: repo}); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListAnnotations()
	if err != nil || len(list) != 1 || list[0].LyricOffsetMS != -500 {
		t.Fatalf("注释应导入: %+v err=%v", list, err)
	}
	if err := os.Remove(filepath.Join(repo, filepath.FromSlash(AnnotationPath(songID)))); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportSnapshot(ImportInput{Store: st, Dir: repo}); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListAnnotations()
	if len(list) != 0 {
		t.Fatalf("远端删除注释后本地也应删除: %+v", list)
	}
}

func TestImportKeymapValidates(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	local := filepath.Join(dir, "keymap.toml")
	if err := os.WriteFile(local, []byte("[keys]\nquit = [\"Q\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, KeymapFile, []byte("[keys]\nquit = [\"Z\"]\n"))
	st := openTestStore(t)
	res, err := ImportSnapshot(ImportInput{Store: st, Dir: repo, KeymapPath: local})
	if err != nil {
		t.Fatal(err)
	}
	if !res.KeymapUpdate {
		t.Fatalf("有效 keymap 应导入: %+v", res)
	}
	data, _ := os.ReadFile(local)
	if !strings.Contains(string(data), `"Z"`) {
		t.Fatalf("本地 keymap 应被远端覆盖: %s", data)
	}
	// 非法配置不能覆盖本地。
	writeRepoFile(t, repo, KeymapFile, []byte("[keys]\nnot_an_action = [\"x\"]\n"))
	res, err = ImportSnapshot(ImportInput{Store: st, Dir: repo, KeymapPath: local})
	if err != nil {
		t.Fatal(err)
	}
	if res.KeymapUpdate || len(res.Warnings) == 0 {
		t.Fatalf("非法 keymap 应跳过并警告: %+v", res)
	}
	data, _ = os.ReadFile(local)
	if !strings.Contains(string(data), `"Z"`) {
		t.Fatalf("非法远端不应覆盖本地: %s", data)
	}
}

func TestCredentialLWWByUpdatedAt(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	st := openTestStore(t)
	client := NewClient(Options{Store: st, Dir: dir})
	meta, key, err := NewMeta(testPassphrase(), time.Unix(1761980000, 0))
	if err != nil {
		t.Fatal(err)
	}
	_ = meta
	if err := st.SaveAccountAt("bilibili", []byte(`{"SESSDATA":"local"}`), 100); err != nil {
		t.Fatal(err)
	}
	writeAccount := func(updated int64, token string) {
		cred := Credential{Source: "bilibili", UpdatedAt: updated, Session: map[string]string{"SESSDATA": token}}
		sealed, err := EncryptCredential(key.Key, "bilibili", key.KeyVersion, cred)
		if err != nil {
			t.Fatal(err)
		}
		writeRepoFile(t, repo, AccountPath("bilibili"), sealed)
	}

	// 时间戳相同但内容不同：保留本地并提示。
	writeAccount(100, "remote")
	applied, warnings, err := client.importAccountsFromTree(key, key.KeyVersion)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 || len(warnings) == 0 {
		t.Fatalf("同时间戳不同内容应保留本地并提示: applied=%v warnings=%v", applied, warnings)
	}
	payload, _, _ := st.LoadAccountAt("bilibili")
	if string(payload) != `{"SESSDATA":"local"}` {
		t.Fatalf("本地登录态不应被覆盖: %s", payload)
	}

	// 远端更新：导入。
	writeAccount(200, "remote")
	applied, _, err = client.importAccountsFromTree(key, key.KeyVersion)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0] != "bilibili" {
		t.Fatalf("远端较新应导入: %v", applied)
	}
	payload, updated, _ := st.LoadAccountAt("bilibili")
	if string(payload) != `{"SESSDATA":"remote"}` || updated != 200 {
		t.Fatalf("导入后应为远端内容且保留时间戳: %s %d", payload, updated)
	}
}

func TestCredentialOldKeyVersionSkipped(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	st := openTestStore(t)
	client := NewClient(Options{Store: st, Dir: dir})
	_, key, err := NewMeta(testPassphrase(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cred := Credential{Source: "netease", UpdatedAt: 5, Session: map[string]string{"MUSIC_U": "x"}}
	sealed, err := EncryptCredential(key.Key, "netease", 1, cred)
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, AccountPath("netease"), sealed)
	// 当前密钥已是版本 2：v1 密文应跳过，而不是报密钥错误。
	keyV2 := *key
	keyV2.KeyVersion = 2
	applied, warnings, err := client.importAccountsFromTree(&keyV2, 2)
	if err != nil {
		t.Fatalf("旧版本密文不应导致失败: %v", err)
	}
	if len(applied) != 0 || len(warnings) == 0 {
		t.Fatalf("旧版本密文应跳过并提示: applied=%v warnings=%v", applied, warnings)
	}
}
