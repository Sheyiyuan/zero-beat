// Package store 负责持久化：SQLite 表结构、迁移与查询。
//
// 计划：
//   - 表结构见 docs/design/02-multi-source-plan.md 第 3 节；
//   - 提供 v1（half-beat.db）到新模型的导入器，导入前自动备份；
//   - 播放直链不落库，只存平台标识与可用性。
//
// TODO(P0): 接入 gorm 或 sqlc，落地 tracks / track_sources / playlists 三张核心表。
package store
