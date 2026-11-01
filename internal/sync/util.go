package sync

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

// 仓库布局见 docs/sync-design.md §5。
const (
	MetaFile          = "meta.json"
	GitattributesFile = ".gitattributes"
	KeymapFile        = "keymap.toml"
	PlaylistsDir      = "playlists"
	AnnotationsDir    = "annotations"
	AccountsDir       = "accounts"
)

// maxRepoFileSize 限制从仓库读取的单个文件，避免恶意仓库塞爆内存。
const maxRepoFileSize = 4 << 20

func joinPath(dir, name string) string { return filepath.Join(dir, name) }

// encodeBase64 统一用标准 base64，与 meta.json / sync-key 的字段一致。
func encodeBase64(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

// writeFileAtomic 先写同目录临时文件再改名，避免半截文件。
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建目录: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("设置文件权限: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入临时文件: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("刷新临时文件: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("替换文件: %w", err)
	}
	return nil
}

// readRepoFile 读取受管克隆里的文件；不存在时返回 os.ErrNotExist。
// 只接受普通文件：拒绝符号链接与设备文件，防止恶意仓库把读取引到工作区外。
func readRepoFile(dir, name string) ([]byte, error) {
	path := joinPath(dir, name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s 不是普通文件", ErrCorruptRepo, name)
	}
	if info.Size() > maxRepoFileSize {
		return nil, fmt.Errorf("%w: %s 过大", ErrCorruptRepo, name)
	}
	return os.ReadFile(path)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func ensureDir(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err != nil {
		return fmt.Errorf("创建目录 %s: %w", path, err)
	}
	return nil
}
