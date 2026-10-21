// Package config 负责数据目录与文件路径（默认 ~/.config/zero-beat）。
//
// 播放设置保存在 SQLite 的 settings 表里，这里不解析 TOML，只保证目录存在且权限为 0700。
package config
