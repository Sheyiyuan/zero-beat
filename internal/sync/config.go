package sync

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config 是本地 sync.toml：只存连接方式与开关，不存 token 或口令。
type Config struct {
	Remote string
	Branch string
	Auto   bool
}

type configFile struct {
	Remote string `toml:"remote"`
	Branch string `toml:"branch"`
	Auto   *bool  `toml:"auto"`
}

// LoadConfig 读取 sync.toml；文件不存在时返回默认配置（自动同步开启）。
func LoadConfig(path string) (Config, error) {
	cfg := Config{Branch: "main", Auto: true}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("读取同步配置: %w", err)
	}
	var f configFile
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return cfg, fmt.Errorf("解析同步配置: %w", err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		return cfg, fmt.Errorf("同步配置包含未知字段: %s", u)
	}
	cfg.Remote = strings.TrimSpace(f.Remote)
	if b := strings.TrimSpace(f.Branch); b != "" {
		cfg.Branch = b
	}
	if f.Auto != nil {
		cfg.Auto = *f.Auto
	}
	return cfg, nil
}

// SaveConfig 以 0600 原子写入 sync.toml。
func SaveConfig(path string, cfg Config) error {
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	var b strings.Builder
	b.WriteString("# zero-beat 同步配置；不要在里面写 token 或口令。\n")
	b.WriteString("remote = " + strconv.Quote(cfg.Remote) + "\n")
	b.WriteString("branch = " + strconv.Quote(cfg.Branch) + "\n")
	b.WriteString("auto = " + strconv.FormatBool(cfg.Auto) + "\n")
	return writeFileAtomic(path, []byte(b.String()), 0o600)
}

// LoadToken 读取可选的本地 token 兜底文件；不存在时返回空字符串。
func LoadToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取本地 token: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if len(token) > 8192 {
		return "", fmt.Errorf("本地 token 过长")
	}
	return token, nil
}

// SaveToken 以 0600 写入 token 兜底文件；空字符串表示删除。
func SaveToken(path, token string) error {
	if strings.TrimSpace(token) == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("清除本地 token: %w", err)
		}
		return nil
	}
	return writeFileAtomic(path, []byte(strings.TrimSpace(token)+"\n"), 0o600)
}

// AskPassScript 是配合 GIT_ASKPASS 的 0700 脚本；token 只从 0600 文件读取，
// 不进入 argv，也不进入仓库配置。
const AskPassScript = `#!/bin/sh
case "$1" in
  *sername*) printf '%s\n' "${ZERO_SYNC_USERNAME:-x-access-token}" ;;
  *) cat "$ZERO_SYNC_TOKEN_FILE" ;;
esac
`

// WriteAskPass 写出 GIT_ASKPASS 脚本并返回路径。
func WriteAskPass(path string) (string, error) {
	if err := writeFileAtomic(path, []byte(AskPassScript), 0o700); err != nil {
		return "", err
	}
	return path, nil
}
