package instance

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireIsExclusive(t *testing.T) {
	dir := t.TempDir()
	first, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("第二个实例应返回 ErrAlreadyRunning，实际 %v", err)
	}
	first.Release()
	second, err := Acquire(dir)
	if err != nil {
		t.Fatalf("释放后应能重新加锁: %v", err)
	}
	second.Release()
}

func TestLockFilePermAndPID(t *testing.T) {
	dir := t.TempDir()
	lock, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	path := filepath.Join(dir, "instance.lock")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("锁文件权限应为 0600，实际 %o", perm)
	}
	if pid := readHolder(path); pid == "" {
		t.Fatal("锁文件应写入当前 PID")
	}
}
