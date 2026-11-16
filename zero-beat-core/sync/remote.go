package sync

import (
	"net/url"
	"path/filepath"
	"strings"
)

// 连接方式由仓库 URL 的 scheme 决定；sync.toml 不额外保存 auth 字段。
const (
	ModeHTTPS = "https"
	ModeSSH   = "ssh"
	ModeLocal = "local"
	ModeOther = "other"
)

// RemoteMode 判断仓库地址的连接方式。
// http(s):// → HTTPS；ssh:// 或 scp 风格 user@host:path → SSH；
// 本地绝对路径 / file:// → 本地仓库；其余（如 git://）归为 other。
func RemoteMode(remote string) string {
	r := strings.TrimSpace(remote)
	switch {
	case r == "":
		return ""
	case strings.HasPrefix(r, "http://"), strings.HasPrefix(r, "https://"):
		return ModeHTTPS
	case strings.HasPrefix(r, "ssh://"):
		return ModeSSH
	case isSCPLike(r):
		return ModeSSH
	case strings.HasPrefix(r, "file://"), filepath.IsAbs(r), strings.HasPrefix(r, "./"), strings.HasPrefix(r, "../"):
		return ModeLocal
	default:
		return ModeOther
	}
}

// isSCPLike 判断 git 的 scp 风格地址（git@host:path 或 host:path）。
// 排除 URL scheme（ssh://、https://）与 Windows 盘符。
func isSCPLike(s string) bool {
	colon := strings.Index(s, ":")
	if colon <= 0 {
		return false
	}
	if strings.Contains(s[:colon], "/") {
		return false
	}
	if strings.HasPrefix(s[colon+1:], "//") {
		return false
	}
	if colon == 1 && (s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z') {
		return false
	}
	return true
}

// splitSCP 把 [user@]host:path 拆成 host 与 path。
func splitSCP(s string) (host, path string, ok bool) {
	if at := strings.LastIndex(s, "@"); at >= 0 {
		s = s[at+1:]
	}
	colon := strings.Index(s, ":")
	if colon <= 0 {
		return "", "", false
	}
	host, path = s[:colon], s[colon+1:]
	if host == "" || path == "" || strings.HasPrefix(path, "//") {
		return "", "", false
	}
	return host, path, true
}

// ToggleRemoteScheme 在同一个仓库的 SSH / HTTPS 地址之间转换。
// 只在能无歧义推导时返回 ok；自定义端口、ssh config 别名等交给用户手填。
func ToggleRemoteScheme(remote string) (string, bool) {
	switch RemoteMode(remote) {
	case ModeHTTPS:
		return httpsToSSH(remote)
	case ModeSSH:
		return sshToHTTPS(remote)
	}
	return "", false
}

func httpsToSSH(remote string) (string, bool) {
	u, err := url.Parse(remote)
	if err != nil || u.Scheme != "https" {
		return "", false
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.TrimPrefix(u.EscapedPath(), "/")
	if host == "" || path == "" || strings.HasPrefix(path, "-") || strings.ContainsAny(path, " \t\n") {
		return "", false
	}
	return "git@" + host + ":" + path, true
}

func sshToHTTPS(remote string) (string, bool) {
	if strings.HasPrefix(remote, "ssh://") {
		u, err := url.Parse(remote)
		if err != nil || u.Hostname() == "" {
			return "", false
		}
		if u.Port() != "" && u.Port() != "22" {
			return "", false
		}
		path := strings.TrimPrefix(u.EscapedPath(), "/")
		if !plausibleRepoHost(u.Hostname()) || path == "" || strings.ContainsAny(path, " \t\n") {
			return "", false
		}
		return "https://" + strings.ToLower(u.Hostname()) + "/" + path, true
	}
	host, path, ok := splitSCP(remote)
	if !ok || !plausibleRepoHost(host) {
		return "", false
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "~") || strings.ContainsAny(path, " \t:") {
		return "", false
	}
	return "https://" + strings.ToLower(host) + "/" + path, true
}

// plausibleRepoHost 要求域名/IP 里有点，避免把 ssh config 的短别名
// （如 github-work）猜成 HTTPS 主机。
func plausibleRepoHost(host string) bool {
	return strings.Contains(host, ".")
}

// RemoteIdentity 把同一仓库的 SSH/HTTPS 写法归一成 host/path，
// 用于 sync-key 的 remote_hash：切换连接方式不应让本机密钥失效。
func RemoteIdentity(remote string) string {
	r := strings.TrimSpace(remote)
	if r == "" {
		return ""
	}
	switch RemoteMode(r) {
	case ModeHTTPS, ModeSSH:
		if strings.HasPrefix(r, "ssh://") || strings.HasPrefix(r, "http://") || strings.HasPrefix(r, "https://") {
			if u, err := url.Parse(r); err == nil && u.Hostname() != "" {
				path := normalizeRepoPath(u.EscapedPath())
				if path != "" {
					return strings.ToLower(u.Hostname()) + "/" + path
				}
			}
		}
		if host, path, ok := splitSCP(r); ok {
			path = normalizeRepoPath(path)
			if host != "" && path != "" {
				return strings.ToLower(host) + "/" + path
			}
		}
	case ModeLocal:
		p := r
		if strings.HasPrefix(p, "file://") {
			if u, err := url.Parse(p); err == nil {
				p = u.Path
			}
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		return filepath.Clean(p)
	}
	return r
}

func normalizeRepoPath(path string) string {
	path = strings.TrimPrefix(path, "~/")
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	return strings.TrimSuffix(path, "/")
}
