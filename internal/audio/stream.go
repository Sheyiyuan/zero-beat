package audio

import "time"

// Stream 是播放引擎唯一的输入：一个带请求头的音频地址。
//
// Headers 用于 B 站这类需要 Referer 的 CDN；ExpiresAt 用于提示上层何时续签。
type Stream struct {
	URL        string
	Headers    map[string]string
	Codec      string
	DurationMS int64
	ExpiresAt  time.Time
	Quality    string
}
