//go:build !linux

package desktop

// Install 在非 Linux 平台不可用。
func Install() (Entry, error) { return Entry{}, ErrUnsupported }

// Uninstall 在非 Linux 平台不可用。
func Uninstall() ([]string, error) { return nil, ErrUnsupported }
