// Package resolve 决定"这一首要从哪个平台、以什么音质播放"。
//
// 计划：
//   - 优先取用户指定的 preferred source，失败后按顺序回退；
//   - 预解析下一首，避免切歌等待；
//   - URL 过期前主动续签；
//   - 全部失败时把 track_sources.available 置 0，并交给 UI 决定是否跳过。
package resolve
