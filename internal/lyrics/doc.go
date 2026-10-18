// Package lyrics 负责歌词解析、匹配与多源聚合。
//
// LRC 解析、编码识别、模糊匹配从 v1 的 internal/lyrics 搬迁，
// 音源侧只提供原文（官方 LRC / B 站字幕），解析与偏移统一在这里做。
package lyrics
