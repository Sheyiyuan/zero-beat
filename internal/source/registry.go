package source

import (
	"fmt"
	"sort"
	"sync"
)

// Registry 保存已注册的音源。
type Registry struct {
	mu      sync.RWMutex
	sources map[string]Source
}

// NewRegistry 创建一个注册表，可传入初始音源。
func NewRegistry(sources ...Source) *Registry {
	r := &Registry{sources: make(map[string]Source, len(sources))}
	for _, s := range sources {
		_ = r.Register(s)
	}
	return r
}

// Register 注册一个音源，重复的 ID 会返回错误。
func (r *Registry) Register(s Source) error {
	if s == nil {
		return fmt.Errorf("source: 不能注册 nil 音源")
	}
	id := s.ID()
	if id == "" {
		return fmt.Errorf("source: 音源 ID 不能为空")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sources[id]; exists {
		return fmt.Errorf("source: 音源 %q 已注册", id)
	}
	r.sources[id] = s
	return nil
}

// Get 按 ID 取音源。
func (r *Registry) Get(id string) (Source, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sources[id]
	return s, ok
}

// All 返回全部音源，按 ID 排序以保证 UI 顺序稳定。
func (r *Registry) All() []Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Source, 0, len(r.sources))
	for _, s := range r.sources {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}
