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
	// ClipStartMS / ClipEndMS 是本地播放区间；ClipEndMS 为 0 表示不限制结尾。
	// 位置与时长仍按整首歌的绝对时间计算，区间只限制起播点和解码长度。
	ClipStartMS int64
	ClipEndMS   int64
}

// clipBounds 返回区间的绝对起止时间；end 为 0 表示不限制。
func (s Stream) clipBounds() (start, end time.Duration) {
	start = time.Duration(s.ClipStartMS) * time.Millisecond
	end = time.Duration(s.ClipEndMS) * time.Millisecond
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if end > 0 && end <= start {
		end = 0
	}
	return start, end
}

func (s Stream) clipEnd() time.Duration {
	_, end := s.clipBounds()
	return end
}

// clampPosition 把位置夹到区间内；atEnd 为 true 时允许停在终点前 1ms，
// 避免 Seek 到终点后启动一个长度为 0 的 ffmpeg。
func (s Stream) clampPosition(pos time.Duration, atEnd bool) time.Duration {
	start, end := s.clipBounds()
	if pos < start {
		return start
	}
	if end > 0 && pos >= end {
		if atEnd {
			if end-start > time.Millisecond {
				return end - time.Millisecond
			}
			return start
		}
		return start
	}
	return pos
}
