// Package config 负责路径、目录创建与轻量配置。
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const appDirName = "zero-beat"

// DataDir 返回应用数据目录（默认 ~/.config/zero-beat）。
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("config: 无法确定配置目录: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("config: 创建数据目录: %w", err)
	}
	return dir, nil
}

// DatabasePath 返回数据库文件路径。
func DatabasePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "zero-beat.db"), nil
}

// KeymapPath 返回快捷键配置文件路径。
func KeymapPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "keymap.toml"), nil
}

// CacheDir 返回缓存目录，用于二维码图片等临时文件。
func CacheDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	cache := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return "", fmt.Errorf("config: 创建缓存目录: %w", err)
	}
	return cache, nil
}
