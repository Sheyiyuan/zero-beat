// Package instance 保证同一个数据目录只运行一个 TUI 播放实例。
//
// 没有这个锁时，第二个 zero 会注册成独立的 MPRIS 播放器（名称冲突时退到
// .instance<pid>），系统媒体面板可能控制旧实例，看起来像「两个播放器」。
package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrAlreadyRunning 表示同一数据目录已有 TUI 在运行。
var ErrAlreadyRunning = errors.New("另一个 zero-beat 已在运行")

// Lock 是进程级实例锁；进程退出（包括崩溃）时由内核自动释放。
type Lock struct {
	file *os.File
	path string
}

// Acquire 在 dir 下拿 TUI 实例锁；已有实例时返回包装了 ErrAlreadyRunning 的错误。
func Acquire(dir string) (*Lock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("instance: 创建数据目录: %w", err)
	}
	return acquire(filepath.Join(dir, "instance.lock"))
}

// Release 释放实例锁。
func (l *Lock) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = unlock(l.file)
	_ = l.file.Close()
	l.file = nil
}

// writeHolder 把当前 PID 写进锁文件，方便第二个实例提示「谁在运行」。
func writeHolder(f *os.File) {
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	_ = f.Sync()
}

// readHolder 读取锁文件里的 PID；失败时返回空字符串。
func readHolder(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	pid := strings.TrimSpace(string(data))
	if _, err := strconv.Atoi(pid); err != nil {
		return ""
	}
	return pid
}
