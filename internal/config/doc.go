// Package config 负责加载与保存 TOML 配置。
//
// 与 v1 的差异：v1 把播放设置塞进 PlayerSetting.config（map[string]any），
// 这里改为强类型 TOML，便于校验与迁移。
package config
