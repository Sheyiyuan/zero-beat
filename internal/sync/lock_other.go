//go:build !unix

package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// lockHandle 是进程间同步锁；非 Unix 平台用独占创建标记文件实现。
type lockHandle struct{ path string }

// AcquireLock 创建独占锁文件；已存在且未过期时返回 ErrBusy。
// 崩溃后残留的锁在 10 分钟后视为过期，允许下一轮同步接管。
func AcquireLock(path string) (*lockHandle, error) {
	if err := ensureDir(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return &lockHandle{path: path}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("创建同步锁: %w", err)
		}
		info, statErr := os.Stat(path)
		if statErr == nil && time.Since(info.ModTime()) > 10*time.Minute {
			_ = os.Remove(path)
			continue
		}
		return nil, ErrBusy
	}
	return nil, ErrBusy
}

// Release 删除锁文件。
func (l *lockHandle) Release() {
	if l == nil || l.path == "" {
		return
	}
	_ = os.Remove(l.path)
	l.path = ""
}
