// Package config 负责数据目录与文件路径（默认 ~/.config/zero-beat）。
//
// 播放设置保存在 SQLite 的 settings 表里。这里也给出 keymap.toml、theme.toml、
// sync.toml 与 lyrics.toml 等本机配置文件的路径，自己不解析它们。
package config
