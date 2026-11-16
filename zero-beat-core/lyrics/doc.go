// Package lyrics 负责歌词文本解析与时间轴定位。
//
// 输入有两类：标准 LRC 文本（LRCLIB）和 B 站字幕（已带起止时间）。
// 偏移与高亮由时间轴函数统一处理，音源侧只提供原文。
package lyrics
