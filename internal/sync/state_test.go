package sync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.toml")
	if err := SaveConfig(path, Config{Remote: "https://example.com/repo.git", Branch: "main", Auto: false}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("sync.toml 权限应为 0600，实际 %o", info.Mode().Perm())
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Remote != "https://example.com/repo.git" || cfg.Branch != "main" || cfg.Auto {
		t.Fatalf("配置往返不符: %+v", cfg)
	}
	// 不存在的文件按默认值：自动同步开启。
	cfg, err = LoadConfig(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Auto || cfg.Branch != "main" || cfg.Remote != "" {
		t.Fatalf("默认配置不符: %+v", cfg)
	}
	// 未知字段必须拒绝，避免拼错字段名后静默失效。
	if err := os.WriteFile(path, []byte("remote = \"x\"\nbranchh = \"main\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("未知字段应被拒绝")
	}
}

func TestValidateRemoteAndBranch(t *testing.T) {
	badRemotes := []string{"", "-u", "https://x\nrm", "a\x00b"}
	for _, remote := range badRemotes {
		if err := ValidateRemote(remote); err == nil {
			t.Fatalf("应拒绝仓库地址 %q", remote)
		}
	}
	if err := ValidateRemote("git@github.com:me/zero.git"); err != nil {
		t.Fatal(err)
	}
	badBranches := []string{"", "-x", "a..b", "a b", "a/", "x.lock", "a;b"}
	for _, branch := range badBranches {
		if err := ValidateBranch(branch); err == nil {
			t.Fatalf("应拒绝分支 %q", branch)
		}
	}
	for _, branch := range []string{"main", "feature/sync", "release-1"} {
		if err := ValidateBranch(branch); err != nil {
			t.Fatalf("应接受分支 %q: %v", branch, err)
		}
	}
}

func TestStateDeviceStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.Device == "" {
		t.Fatal("应生成设备 ID")
	}
	state.LastSyncAt = time.Now().Unix()
	state.ConflictTx = "tx-1"
	if err := SaveState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Device != state.Device || loaded.ConflictTx != "tx-1" {
		t.Fatalf("状态往返不符: %+v", loaded)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state.json 权限应为 0600，实际 %o", info.Mode().Perm())
	}
}

func TestLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	first, err := AcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(path); !errors.Is(err, ErrBusy) {
		t.Fatalf("第二个锁应返回 ErrBusy，实际 %v", err)
	}
	first.Release()
	second, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("释放后应能重新加锁: %v", err)
	}
	second.Release()
}

func TestAuthLabel(t *testing.T) {
	cases := []struct {
		remote string
		token  bool
		want   string
	}{
		{"git@github.com:me/x.git", false, "SSH"},
		{"ssh://git@host/x.git", false, "SSH"},
		{"https://host/x.git", true, "HTTPS（本地 token）"},
		{"https://host/x.git", false, "HTTPS（系统凭据）"},
		{"/tmp/remote.git", false, "本地仓库"},
	}
	for _, c := range cases {
		if got := AuthLabel(c.remote, c.token); got != c.want {
			t.Fatalf("AuthLabel(%q,%v)=%q，期望 %q", c.remote, c.token, got, c.want)
		}
	}
}
