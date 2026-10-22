package player

import (
	"time"

	"github.com/sheyiyuan/zero-beat/internal/source"
)

// Prefetch 只缓存一首下一曲的解析结果；队列变化或过期即作废。
type Prefetch struct {
	id     string
	stream *source.Stream
}

// Set 记录一条预解析结果。
func (p *Prefetch) Set(id string, stream source.Stream) {
	p.id = id
	s := stream
	p.stream = &s
}

// Matches 判断预解析结果是否仍对指定曲目有效。
func (p *Prefetch) Matches(id string, now time.Time) bool {
	if p.stream == nil || p.id != id {
		return false
	}
	return p.stream.ExpiresAt.IsZero() || p.stream.ExpiresAt.After(now.Add(2*time.Minute))
}

// Take 取出预解析结果并清空；过期时返回 false。
func (p *Prefetch) Take(id string, now time.Time) (source.Stream, bool) {
	if !p.Matches(id, now) {
		p.Clear()
		return source.Stream{}, false
	}
	s := *p.stream
	p.Clear()
	return s, true
}

// Clear 丢弃预解析结果。
func (p *Prefetch) Clear() {
	p.id = ""
	p.stream = nil
}
