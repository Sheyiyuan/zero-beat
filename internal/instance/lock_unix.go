//go:build unix

package instance

import (
	"fmt"
	"os"
	"syscall"
)

func acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("instance: 打开锁文件: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			if pid := readHolder(path); pid != "" {
				return nil, fmt.Errorf("%w（PID %s）；请先退出旧实例", ErrAlreadyRunning, pid)
			}
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("instance: 获取锁: %w", err)
	}
	writeHolder(f)
	return &Lock{file: f, path: path}, nil
}

func unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
