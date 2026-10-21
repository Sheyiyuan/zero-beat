package source

import (
	"context"
	"testing"

	"github.com/sheyiyuan/zero-beat/internal/lyrics"
)

type stub struct{ id ID }

func (s stub) ID() ID                                                     { return s.id }
func (s stub) Name() string                                               { return s.id.Label() }
func (s stub) Caps() Capability                                           { return CapSearch }
func (s stub) LoggedIn() bool                                             { return false }
func (s stub) User() User                                                 { return User{} }
func (s stub) Account(context.Context) (User, error)                      { return User{}, nil }
func (s stub) StartQRLogin(context.Context) (QRCode, error)               { return QRCode{}, nil }
func (s stub) PollQRLogin(context.Context, string) (LoginStatus, error)   { return LoginStatus{}, nil }
func (s stub) Search(context.Context, string, int) (SearchPage, error)    { return SearchPage{}, nil }
func (s stub) Resolve(context.Context, Ref, Quality) (Resolved, error)    { return Resolved{}, nil }
func (s stub) Lyrics(context.Context, Ref) ([]lyrics.Line, string, error) { return nil, "", nil }
func (s stub) RemotePlaylists(context.Context) ([]Playlist, error)        { return nil, nil }
func (s stub) PlaylistTracks(context.Context, string) ([]Track, error)    { return nil, nil }
func (s stub) Session() map[string]string                                 { return nil }
func (s stub) SetSession(map[string]string)                               {}
func (s stub) Logout()                                                    {}

func TestRegistry(t *testing.T) {
	r := NewRegistry(stub{Bilibili}, nil, stub{NetEase})
	if len(r.All()) != 2 || r.Get(Bilibili) == nil || r.Get(NetEase) == nil {
		t.Fatalf("注册表内容不符: %+v", r.All())
	}
	if r.GetString("netease") == nil || r.GetString("") != nil {
		t.Fatal("字符串查找不符")
	}
	ids := r.IDs()
	if len(ids) != 2 || ids[0] != Bilibili || ids[1] != NetEase {
		t.Fatalf("ID 顺序不符: %+v", ids)
	}
}

func TestLabelsAndKeys(t *testing.T) {
	if Bilibili.Label() != "Bilibili" || NetEase.Label() != "网易云" || Bilibili.Mark() != "B" || NetEase.Mark() != "云" {
		t.Fatal("音源名称/标记不符")
	}
	ref := Ref{Source: NetEase, ExternalID: "1431292823", Page: 0}
	if ref.Key() != "netease:1431292823:1" {
		t.Fatalf("Ref.Key = %q", ref.Key())
	}
	if QualityLossless.Label() != "无损" || ParseQuality(9) != QualityAuto {
		t.Fatal("音质档位不符")
	}
	if !CapQualitySelect.Has(CapQualitySelect) || CapSearch.Has(CapLossless) {
		t.Fatal("能力位判断不符")
	}
}
