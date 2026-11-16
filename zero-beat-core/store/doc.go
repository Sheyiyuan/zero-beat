// Package store 负责 SQLite 持久化：歌曲、歌单、队列、设置与登录态。
//
// 设计约束：
//   - 播放直链不落库，只存平台标识与元数据（直链约 2 小时 / 20 分钟过期）；
//   - 远端歌单/收藏夹是只读镜像，本地歌单可增删改；
//   - 不使用 ORM：schema 与 SQL 都在 store.go，驱动为纯 Go 的 modernc.org/sqlite。
package store
