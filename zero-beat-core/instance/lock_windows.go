//go:build windows

package instance

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// lockOffset 把排他锁加在文件第 4096 字节之后，而不是文件头。
// Windows 的字节区间锁是强制的：锁住头部后，第二个实例连 PID 都读不出来。
const lockOffset = 4096

func acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("instance: 打开锁文件: %w", err)
	}
	ol := &windows.Overlapped{Offset: lockOffset}
	err = windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if err != nil {
		f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
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
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{Offset: lockOffset})
}
