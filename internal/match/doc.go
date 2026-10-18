// Package match 负责跨音源匹配：判断两个平台的条目是不是"同一首歌"。
//
// 计划：
//   - 归一化标题、歌手、时长，生成 norm key；
//   - 评分模型：标题 0.5 + 歌手 0.3 + 时长 0.2；
//   - 高于阈值自动绑定，中间区间进人工确认队列。
//
// 纯函数实现，必须可单测。详见 docs/design/02-multi-source-plan.md 第 3 节。
package match
