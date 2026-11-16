//go:build windows

package sync

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// A kernel lock is released by Windows on process death; no stale timeout can
// admit another writer while an existing operation is still alive.
type lockHandle struct {
	file       *os.File
	overlapped windows.Overlapped
}

func AcquireLock(path string) (*lockHandle, error) {
	if err := ensureDir(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	l := &lockHandle{file: f}
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &l.overlapped)
	if err != nil {
		f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return l, nil
}
func (l *lockHandle) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &l.overlapped)
	_ = l.file.Close()
	l.file = nil
}
