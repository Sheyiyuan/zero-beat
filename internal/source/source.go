// Package source 定义播放器的多音源契约。
//
// 第一版只有一个 B 站实现时故意没有抽接口（见 ADR-0009）。现在第二个音源
// 网易云落地，接口由两个真实实现共同约束：登录、搜索、取流、歌词、远端歌单。
package source

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/lyrics"
)

// ID 是音源的稳定标识，同时会写进歌曲的 source 字段。
type ID string

const (
	Bilibili ID = "bilibili"
	NetEase  ID = "netease"
)

// Capability 是音源能力位，UI 只按能力显隐，不按平台名分支。
type Capability uint32

const (
	CapSearch Capability = 1 << iota
	CapMultiPart
	CapRemotePlaylists
	CapOfficialLyrics
	CapQualitySelect
	CapLossless
	// CapDailyRecommendations 表示音源提供「每日推荐」列表。
	CapDailyRecommendations
	// CapPlaylistWrite 表示可以把歌曲写入该音源的云端歌单。
	CapPlaylistWrite
)

// Has 判断是否具备某项能力。
func (c Capability) Has(want Capability) bool { return c&want != 0 }

// Quality 是跨音源的音质偏好。
type Quality int

const (
	QualityAuto Quality = iota
	QualityHigh
	QualityLossless
)

// Label 返回界面用的档位名。
func (q Quality) Label() string {
	switch q {
	case QualityHigh:
		return "高音质"
	case QualityLossless:
		return "无损"
	default:
		return "自动"
	}
}

// ParseQuality 把设置里的整数夹到合法范围。
func ParseQuality(v int) Quality {
	if v < 0 || v > 2 {
		return QualityAuto
	}
	return Quality(v)
}

// Ref 定位一首歌在某个音源里的版本。
type Ref struct {
	Source     ID
	ExternalID string // 音源内的稳定 ID
	Page       int
	CID        int64 // 平台扩展字段；B 站分 P 为 cid，其他音源为 0
}

// Key 返回稳定 ID，格式与 store.SongID 保持一致。
func (r Ref) Key() string {
	page := r.Page
	if page <= 0 {
		page = 1
	}
	return fmt.Sprintf("%s:%s:%d", r.Source, r.ExternalID, page)
}

// Track 是音源返回的曲目元数据。
type Track struct {
	Source     ID
	ExternalID string
	Page       int
	CID        int64
	Title      string
	Artist     string
	Album      string
	Duration   int // 秒
	Cover      string
}

// Ref 返回曲目的定位信息。
func (t Track) Ref() Ref {
	return Ref{Source: t.Source, ExternalID: t.ExternalID, Page: t.Page, CID: t.CID}
}

// Key 返回稳定 ID。
func (t Track) Key() string { return t.Ref().Key() }

// Candidate 是搜索结果条目。
type Candidate struct {
	Track
	Play int64 // 播放量，未知为 0
	VIP  bool  // 平台标记为会员/付费才能完整播放
}

// SearchPage 是一页搜索结果。
type SearchPage struct {
	Candidates []Candidate
	Page       int
	Pages      int
}

// DailyRecommender 是可选能力：支持每日推荐的音源实现它。
// UI 只在 CapDailyRecommendations 置位时调用，不要求所有音源都实现。
type DailyRecommender interface {
	DailyRecommendations(ctx context.Context) ([]Track, error)
}

// PlaylistWriter 是可选能力：把一首歌写入该音源的云端歌单。
// 目标歌单必须属于同一音源；歌曲平台由 ref.Source 约束。
type PlaylistWriter interface {
	AddToPlaylist(ctx context.Context, remoteID string, ref Ref) error
}

// Aliased 是可选能力：音源声明命令行里可接受的别名（ID 与 Name 自动包含）。
type Aliased interface {
	Aliases() []string
}

// TargetParser 是可选能力：把用户输入的链接/编号解析成本音源的 Ref。
// 返回 ok=false 表示输入不属于该音源，注册表会继续尝试下一个。
type TargetParser interface {
	ParseTarget(input string, pageOverride int) (Ref, bool, error)
}

// LyricNamer 是可选能力：歌词来源在界面上的展示名，如「B 站字幕」。
type LyricNamer interface {
	LyricName() string
}

// LoginNamer 是可选能力：扫码登录页里提示使用的 App 名称。
type LoginNamer interface {
	LoginAppName() string
}

// Stream 是音源交给播放引擎的最终音频地址。
type Stream struct {
	URL       string
	Headers   map[string]string
	Codec     string
	Duration  time.Duration
	ExpiresAt time.Time
	Quality   string
}

// Resolved 是取流结果，Track 会回写队列以补齐 CID/标题等元数据。
type Resolved struct {
	Track  Track
	Stream Stream
}

// Playlist 是音源侧的远端歌单/收藏夹镜像。
type Playlist struct {
	ID       string // store 里的歌单 ID
	Source   ID
	RemoteID string
	Title    string
	Count    int
}

// User 是音源账号。
type User struct {
	ID       string
	Name     string
	Avatar   string
	LoggedIn bool
}

// QRCode 是一次扫码登录会话。
type QRCode struct {
	Key string
	URL string
}

// LoginStatus 是二维码轮询结果。
type LoginStatus struct {
	Code     int
	Message  string
	LoggedIn bool
	Expired  bool
}

// Source 是播放器需要音源提供的全部能力。
//
// 登录流程由 UI 驱动：Source 只负责申请二维码和轮询状态，二维码渲染留在 TUI。
type Source interface {
	ID() ID
	Name() string
	// Mark 是列表行里用的极短标记，属于平台展示信息，不再由核心包穷举。
	Mark() string
	Caps() Capability
	LoggedIn() bool
	User() User
	Account(ctx context.Context) (User, error)
	StartQRLogin(ctx context.Context) (QRCode, error)
	PollQRLogin(ctx context.Context, key string) (LoginStatus, error)
	Search(ctx context.Context, keyword string, page int) (SearchPage, error)
	Resolve(ctx context.Context, ref Ref, quality Quality) (Resolved, error)
	Lyrics(ctx context.Context, ref Ref) ([]lyrics.Line, string, error)
	RemotePlaylists(ctx context.Context) ([]Playlist, error)
	PlaylistTracks(ctx context.Context, remoteID string) ([]Track, error)
	Session() map[string]string
	SetSession(map[string]string)
	Logout()
}

// Registry 保存按 ID 索引的音源，顺序决定界面展示顺序。
type Registry struct {
	order   []Source
	byID    map[ID]Source
	aliases map[string]ID
}

// NewRegistry 创建注册表；nil 音源会被忽略。
func NewRegistry(sources ...Source) *Registry {
	r := &Registry{byID: map[ID]Source{}, aliases: map[string]ID{}}
	for _, s := range sources {
		if s == nil {
			continue
		}
		id := s.ID()
		r.order = append(r.order, s)
		r.byID[id] = s
		r.bindAlias(string(id), id)
		r.bindAlias(s.Name(), id)
		if a, ok := s.(Aliased); ok {
			for _, alias := range a.Aliases() {
				r.bindAlias(alias, id)
			}
		}
	}
	return r
}

func (r *Registry) bindAlias(alias string, id ID) {
	if r == nil {
		return
	}
	key := normalizeName(alias)
	if key == "" {
		return
	}
	r.aliases[key] = id
}

func normalizeName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// All 按注册顺序返回全部音源。
func (r *Registry) All() []Source {
	if r == nil {
		return nil
	}
	out := make([]Source, len(r.order))
	copy(out, r.order)
	return out
}

// Get 按 ID 查找音源。
func (r *Registry) Get(id ID) Source {
	if r == nil {
		return nil
	}
	return r.byID[id]
}

// GetString 按字符串查找音源，便于从数据库字段直接取。
func (r *Registry) GetString(id string) Source {
	return r.Get(ID(strings.TrimSpace(id)))
}

// GetByName 按 ID、显示名或平台别名查找音源。
func (r *Registry) GetByName(name string) Source {
	if r == nil {
		return nil
	}
	if id, ok := r.aliases[normalizeName(name)]; ok {
		return r.Get(id)
	}
	return r.GetString(name)
}

// Name 返回音源显示名；未注册时退回原始 ID。
func (r *Registry) Name(id ID) string {
	if src := r.Get(id); src != nil {
		return src.Name()
	}
	return string(id)
}

// Mark 返回音源短标记；未注册时返回 ?。
func (r *Registry) Mark(id ID) string {
	if src := r.Get(id); src != nil && src.Mark() != "" {
		return src.Mark()
	}
	return "?"
}

// LyricName 返回歌词来源展示名。
func (r *Registry) LyricName(id ID) string {
	src := r.Get(id)
	if src == nil {
		return "平台歌词"
	}
	if n, ok := src.(LyricNamer); ok && strings.TrimSpace(n.LyricName()) != "" {
		return n.LyricName()
	}
	return src.Name() + "歌词"
}

// LoginAppName 返回扫码登录页提示的 App 名称。
func (r *Registry) LoginAppName(id ID) string {
	src := r.Get(id)
	if src == nil {
		return "手机 App"
	}
	if n, ok := src.(LoginNamer); ok && strings.TrimSpace(n.LoginAppName()) != "" {
		return n.LoginAppName()
	}
	return src.Name() + " App"
}

// ParseTarget 让各音源依次尝试解析用户输入的链接/编号。
func (r *Registry) ParseTarget(input string, pageOverride int) (Ref, bool, error) {
	if r == nil {
		return Ref{}, false, nil
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return Ref{}, false, nil
	}
	for _, src := range r.order {
		p, ok := src.(TargetParser)
		if !ok {
			continue
		}
		ref, matched, err := p.ParseTarget(input, pageOverride)
		if !matched {
			continue
		}
		if err != nil {
			return Ref{}, true, err
		}
		return ref, true, nil
	}
	return Ref{}, false, nil
}

// IDs 返回全部音源 ID。
func (r *Registry) IDs() []ID {
	if r == nil {
		return nil
	}
	out := make([]ID, 0, len(r.order))
	for _, s := range r.order {
		out = append(out, s.ID())
	}
	return out
}
