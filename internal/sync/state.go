package sync

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// State 是本地同步状态（不进仓库）。
type State struct {
	Device             string   `json:"device"`
	LastSyncAt         int64    `json:"last_sync_at,omitempty"`
	LastResult         string   `json:"last_result,omitempty"`
	LastError          string   `json:"last_error,omitempty"`
	MergeHead          string   `json:"merge_head,omitempty"`
	ConflictTx         string   `json:"conflict_tx,omitempty"`
	KeyIssue           string   `json:"key_issue,omitempty"`
	CredentialWarnings []string `json:"credential_warnings,omitempty"`
}

// DeviceID 生成本机随机标识；提交信息只用它，不含歌单名与主机名。
func DeviceID() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// LoadState 读取 state.json；不存在时返回带新设备 ID 的空状态。
func LoadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return State{Device: DeviceID()}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("读取同步状态: %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("解析同步状态: %w", err)
	}
	if st.Device == "" {
		st.Device = DeviceID()
	}
	return st, nil
}

// SaveState 以 0600 原子写入 state.json。
func SaveState(path string, st State) error {
	if st.Device == "" {
		st.Device = DeviceID()
	}
	data, err := encodeJSON(st)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

// Status 是给 UI/CLI 看的同步概况，不包含任何密文或口令。
type Status struct {
	Configured     bool
	Remote         string
	Branch         string
	Auth           string
	Auto           bool
	KeyCached      bool
	KeyMatchesRepo bool
	KeyVersion     int
	LastSyncAt     time.Time
	LastResult     string
	LastError      string
	KeyIssue       string
	ConflictTx     string
	Conflicts      int
	Warnings       []string
}

// Summary 返回状态行的一行文案。
func (s Status) Summary() string {
	switch {
	case !s.Configured:
		return "同步未配置"
	case s.Conflicts > 0 || s.ConflictTx != "":
		if s.Conflicts > 0 {
			return fmt.Sprintf("同步冲突 %d", s.Conflicts)
		}
		return "同步冲突"
	case s.LastError != "":
		return "同步失败：" + shortError(s.LastError)
	case s.KeyIssue != "":
		return "同步 密钥不可用"
	case s.LastSyncAt.IsZero():
		return "同步未运行"
	case !s.KeyCached:
		return "同步 密钥不可用"
	case !s.KeyMatchesRepo:
		return "同步 密钥属于另一个仓库"
	default:
		return "同步 " + s.LastSyncAt.Format("15:04") + " 成功"
	}
}

func shortError(msg string) string {
	msg = strings.TrimSpace(strings.SplitN(msg, "（", 2)[0])
	if len([]rune(msg)) > 18 {
		return string([]rune(msg)[:18]) + "…"
	}
	return msg
}

// AuthLabel 根据 URL 与本地 token 判断认证方式（只用于显示）。
func AuthLabel(remote string, token bool) string {
	switch RemoteMode(remote) {
	case ModeSSH:
		return "SSH"
	case ModeHTTPS:
		if token {
			return "HTTPS（本地 token）"
		}
		return "HTTPS（系统凭据）"
	case ModeLocal:
		return "本地仓库"
	case "":
		return ""
	}
	return "其他"
}

// archivePath 返回归档目录，按秒级时间分目录，避免覆盖。
func archivePath(dir string, now time.Time) string {
	base := filepath.Join(dir, "archive", now.Format("20060102-150405"))
	candidate := base
	for i := 1; fileExists(candidate); i++ {
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return candidate
}
