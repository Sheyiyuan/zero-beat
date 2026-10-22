package source

import (
	"context"
	"testing"

	"github.com/sheyiyuan/zero-beat/internal/lyrics"
)

type stub struct {
	id      ID
	name    string
	mark    string
	aliases []string
}

func (s stub) ID() ID                                                     { return s.id }
func (s stub) Name() string                                               { return s.name }
func (s stub) Mark() string                                               { return s.mark }
func (s stub) Aliases() []string                                          { return s.aliases }
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
	r := NewRegistry(
		stub{id: Bilibili, name: "Bilibili", mark: "B", aliases: []string{"bili", "b站"}},
		nil,
		stub{id: NetEase, name: "网易云", mark: "云", aliases: []string{"ncm"}},
	)
	if len(r.All()) != 2 || r.Get(Bilibili) == nil || r.Get(NetEase) == nil {
		t.Fatalf("注册表内容不符: %+v", r.All())
	}
	if r.GetString("netease") == nil || r.GetString("") != nil {
		t.Fatal("字符串查找不符")
	}
	for _, name := range []string{"bilibili", "Bilibili", "bili", "b站", "netease", "网易云", "ncm"} {
		if r.GetByName(name) == nil {
			t.Fatalf("别名 %q 应能解析", name)
		}
	}
	if r.Name(NetEase) != "网易云" || r.Mark(NetEase) != "云" {
		t.Fatalf("展示信息不符: name=%q mark=%q", r.Name(NetEase), r.Mark(NetEase))
	}
	if r.Name("missing") != "missing" || r.Mark("missing") != "?" {
		t.Fatal("未知音源应有兜底展示")
	}
	ids := r.IDs()
	if len(ids) != 2 || ids[0] != Bilibili || ids[1] != NetEase {
		t.Fatalf("ID 顺序不符: %+v", ids)
	}
}

func TestKeysAndQuality(t *testing.T) {
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
