//go:build !unix

package instance

import (
	"fmt"
	"os"
	"time"
)

func acquire(path string) (*Lock, error) {
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			writeHolder(f)
			return &Lock{file: f, path: path}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("instance: 创建锁文件: %w", err)
		}
		info, statErr := os.Stat(path)
		if statErr == nil && time.Since(info.ModTime()) > 10*time.Minute {
			_ = os.Remove(path)
			continue
		}
		if pid := readHolder(path); pid != "" {
			return nil, fmt.Errorf("%w（PID %s）；请先退出旧实例", ErrAlreadyRunning, pid)
		}
		return nil, ErrAlreadyRunning
	}
	return nil, ErrAlreadyRunning
}

func unlock(f *os.File) error {
	return os.Remove(f.Name())
}
