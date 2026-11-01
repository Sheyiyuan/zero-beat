package sync

import "testing"

func TestRemoteMode(t *testing.T) {
	cases := []struct{ remote, want string }{
		{"", ""},
		{"https://github.com/me/zero.git", ModeHTTPS},
		{"http://host/x.git", ModeHTTPS},
		{"ssh://git@host/x.git", ModeSSH},
		{"git@github.com:me/zero.git", ModeSSH},
		{"github-work:me/zero.git", ModeSSH},
		{"/tmp/remote.git", ModeLocal},
		{"file:///tmp/remote.git", ModeLocal},
		{"./remote.git", ModeLocal},
		{"git://host/x.git", ModeOther},
	}
	for _, c := range cases {
		if got := RemoteMode(c.remote); got != c.want {
			t.Fatalf("RemoteMode(%q)=%q，期望 %q", c.remote, got, c.want)
		}
	}
}

func TestToggleRemoteScheme(t *testing.T) {
	ok := []struct{ from, to string }{
		{"https://github.com/me/zero.git", "git@github.com:me/zero.git"},
		{"git@github.com:me/zero.git", "https://github.com/me/zero.git"},
		{"https://gitlab.com/group/sub/repo.git", "git@gitlab.com:group/sub/repo.git"},
		{"git@gitlab.com:group/sub/repo.git", "https://gitlab.com/group/sub/repo.git"},
		{"ssh://git@codeberg.org/me/repo.git", "https://codeberg.org/me/repo.git"},
		{"ssh://git@host.example.com:22/me/repo.git", "https://host.example.com/me/repo.git"},
	}
	for _, c := range ok {
		got, converted := ToggleRemoteScheme(c.from)
		if !converted || got != c.to {
			t.Fatalf("ToggleRemoteScheme(%q)=%q,%v，期望 %q", c.from, got, converted, c.to)
		}
	}
	bad := []string{
		"",
		"/tmp/remote.git",
		"git://host/x.git",
		"http://host/x.git",            // 只自动转换 https
		"https://host:8443/x.git",      // 自定义端口无法推导
		"ssh://git@host:2222/x.git",    // 同上
		"git@host:~/x.git",             // 家目录写法
		"github-work:me/x.git",         // ssh config 别名
		"git@localhost:me/x.git",       // 短主机名不猜
		"https://user:pass@host/x.git", // 带 userinfo
	}
	for _, remote := range bad {
		if got, converted := ToggleRemoteScheme(remote); converted {
			t.Fatalf("ToggleRemoteScheme(%q) 不应转换，得到 %q", remote, got)
		}
	}
}

func TestRemoteIdentityMakesSchemesEqual(t *testing.T) {
	base := RemoteHash("https://github.com/me/zero.git")
	for _, remote := range []string{
		"git@github.com:me/zero.git",
		"ssh://git@github.com/me/zero.git",
		"ssh://git@github.com:22/me/zero.git",
		"https://github.com/me/zero",
		"https://GitHub.com/me/zero.git",
	} {
		if got := RemoteHash(remote); got != base {
			t.Fatalf("RemoteHash(%q) 应与 HTTPS 形式一致", remote)
		}
	}
	if RemoteHash("https://github.com/me/other.git") == base {
		t.Fatal("不同仓库不应得到相同 remote_hash")
	}
	if RemoteHash("/tmp/a") != RemoteHash("/tmp/a/") {
		t.Fatal("本地路径应归一化")
	}
}

func TestSanitizeRemoteKeepsSSHUser(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://user:token@host/x.git", "https://***@host/x.git"},
		{"https://token@host/x.git", "https://***@host/x.git"},
		{"ssh://git@host/x.git", "ssh://git@host/x.git"},
		{"git@host:me/x.git", "git@host:me/x.git"},
		{"fatal: unable to access 'https://u:p@host/x': 403", "fatal: unable to access 'https://***@host/x': 403"},
	}
	for _, c := range cases {
		if got := SanitizeRemote(c.in); got != c.want {
			t.Fatalf("SanitizeRemote(%q)=%q，期望 %q", c.in, got, c.want)
		}
	}
}
