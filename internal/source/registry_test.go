package source

import (
	"context"
	"testing"

	"github.com/sheyiyuan/zero-beat/internal/domain"
)

type fakeSource struct{ id string }

func (f fakeSource) ID() string       { return f.id }
func (f fakeSource) Caps() Capability { return CapSearch }

func (f fakeSource) Account(context.Context) (*domain.Account, error) {
	return &domain.Account{Source: f.id}, nil
}
func (f fakeSource) Login(context.Context, LoginInteraction) error { return nil }
func (f fakeSource) Logout(context.Context) error                  { return nil }

func (f fakeSource) Search(context.Context, string, int) ([]Candidate, error) { return nil, nil }
func (f fakeSource) Resolve(context.Context, domain.SourceRef, domain.Quality) (*Stream, error) {
	return nil, nil
}
func (f fakeSource) Lyrics(context.Context, domain.SourceRef) ([]LyricDocument, error) {
	return nil, nil
}
func (f fakeSource) RemotePlaylists(context.Context) ([]RemotePlaylist, error) { return nil, nil }
func (f fakeSource) SyncRemote(context.Context, string) (SyncReport, error)    { return SyncReport{}, nil }

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry(fakeSource{id: "bilibili"})
	if _, ok := r.Get("bilibili"); !ok {
		t.Fatal("注册表里应该能取到 bilibili")
	}
	if _, ok := r.Get("netease"); ok {
		t.Fatal("未注册的音源不应该取到")
	}
}

func TestRegistryRejectsDuplicate(t *testing.T) {
	r := NewRegistry(fakeSource{id: "netease"})
	if err := r.Register(fakeSource{id: "netease"}); err == nil {
		t.Fatal("重复注册应该返回错误")
	}
}

func TestRegistryAllIsSorted(t *testing.T) {
	r := NewRegistry(fakeSource{id: "netease"}, fakeSource{id: "bilibili"})
	all := r.All()
	if len(all) != 2 {
		t.Fatalf("期望 2 个音源，实际 %d", len(all))
	}
	if all[0].ID() != "bilibili" || all[1].ID() != "netease" {
		t.Fatalf("音源顺序应按 ID 排序，实际 %s, %s", all[0].ID(), all[1].ID())
	}
}

func TestCapabilityHas(t *testing.T) {
	caps := CapSearch | CapMultiPart
	if !caps.Has(CapMultiPart) {
		t.Fatal("应该包含 CapMultiPart")
	}
	if caps.Has(CapOfficialLyrics) {
		t.Fatal("不应该包含 CapOfficialLyrics")
	}
}
