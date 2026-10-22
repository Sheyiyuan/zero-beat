package player

import (
	"testing"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/source"
)

func TestPrefetchTakeAndExpiry(t *testing.T) {
	var p Prefetch
	now := time.Now()
	p.Set("a", source.Stream{URL: "u1"})
	if !p.Matches("a", now) {
		t.Fatal("刚写入的预解析应有效")
	}
	if _, ok := p.Take("b", now); ok {
		t.Fatal("ID 不匹配时不应取出")
	}
	if p.Matches("a", now) {
		t.Fatal("Take 失败也应清空缓存")
	}

	p.Set("a", source.Stream{URL: "u2"})
	s, ok := p.Take("a", now)
	if !ok || s.URL != "u2" {
		t.Fatalf("取出结果不符: %+v ok=%v", s, ok)
	}

	p.Set("a", source.Stream{URL: "u3", ExpiresAt: now.Add(time.Minute)})
	if p.Matches("a", now) {
		t.Fatal("临近过期的预解析应判为无效")
	}
}
