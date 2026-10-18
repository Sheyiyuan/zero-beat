// Package domain 定义与平台无关的领域模型。
//
// 三条设计约束：
//
//  1. Track 是用户视角的一首歌，不携带任何平台字段；
//  2. TrackSource 是这首歌在某个平台上的具体版本（BV 号、网易云 song id）；
//  3. 播放直链不落库，只在播放时解析，因为它会过期。
//
// 详见 docs/adr/0004-track-and-tracksource.md。
package domain

import "time"

// ID 是领域实体的本地标识，与平台标识无关。
type ID string

// Quality 是跨平台统一的音质档位，各音源自行映射到平台内的档位。
type Quality int

const (
	QualityAuto Quality = iota
	QualityStandard
	QualityHigh
	QualityLossless
)

func (q Quality) String() string {
	switch q {
	case QualityStandard:
		return "standard"
	case QualityHigh:
		return "high"
	case QualityLossless:
		return "lossless"
	default:
		return "auto"
	}
}

// Codec 是音频编码，决定解码后端的选择。
type Codec int

const (
	CodecUnknown Codec = iota
	CodecAACLC
	CodecHEAAC
	CodecFLAC
	CodecMP3
	CodecEC3
)

func (c Codec) String() string {
	switch c {
	case CodecAACLC:
		return "aac-lc"
	case CodecHEAAC:
		return "he-aac"
	case CodecFLAC:
		return "flac"
	case CodecMP3:
		return "mp3"
	case CodecEC3:
		return "ec-3"
	default:
		return "unknown"
	}
}

// DecodableNative 表示纯 Go 解码链能否处理该编码。
// 返回 false 的编码需要外部解码后端（mpv/ffmpeg）。
func (c Codec) DecodableNative() bool {
	switch c {
	case CodecAACLC, CodecFLAC, CodecMP3:
		return true
	default:
		return false
	}
}

// SourceRef 指向某个音源上的一条具体内容。
type SourceRef struct {
	Source     string // 音源标识，如 "bilibili"、"netease"
	ExternalID string // 平台内标识，如 BV 号、歌曲 id
	Page       int    // 分 P 序号，从 1 开始；非分 P 内容为 0
}

// IsZero 判断引用是否为空。
func (r SourceRef) IsZero() bool {
	return r.Source == "" || r.ExternalID == ""
}

// Track 是用户视角的一首歌。
type Track struct {
	ID         ID
	Title      string
	Artist     string
	Album      string
	DurationMS int64
	CoverURL   string
	CoverLocal string
	NormKey    string // 归一化匹配键，用于跨源匹配
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// TrackSource 是 Track 在一个平台上的版本。
type TrackSource struct {
	ID        ID
	TrackID   ID
	Ref       SourceRef
	Quality   Quality
	Available bool
	Preferred bool
	Extra     map[string]string
	CheckedAt time.Time
}

// Account 是某个音源的登录态。
type Account struct {
	Source    string
	UserID    string
	Nickname  string
	AvatarURL string
	ExpiresAt time.Time
}

// Playlist 是本地歌单，可混装任意音源的 Track。
type Playlist struct {
	ID        ID
	Title     string
	Items     []PlaylistItem
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PlaylistItem 是歌单中的一项，只引用 Track，不关心它来自哪个平台。
type PlaylistItem struct {
	ID       ID
	TrackID  ID
	Position int
	AddedAt  time.Time
}
