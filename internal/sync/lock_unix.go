//go:build unix

package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockHandle 是进程间同步锁；拿不到锁时直接跳过本轮。
type lockHandle struct{ file *os.File }

// AcquireLock 用 flock 加非阻塞排他锁；已被占用时返回 ErrBusy。
func AcquireLock(path string) (*lockHandle, error) {
	if err := ensureDir(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开同步锁: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, ErrBusy
		}
		return nil, fmt.Errorf("获取同步锁: %w", err)
	}
	return &lockHandle{file: f}, nil
}

// Release 释放同步锁。
func (l *lockHandle) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
	l.file = nil
}
