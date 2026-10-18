// Package source 定义多平台音源抽象。
//
// 新增一个平台只需要实现 Source 接口，不需要改播放引擎与 UI：
// 播放引擎只消费 Stream，UI 只消费 Capability。
//
// 详见 docs/adr/0003-audio-decoder-abstraction.md 与
// docs/adr/0005-single-source-first.md。
package source

import (
	"context"
	"time"

	"github.com/sheyiyuan/hbp/internal/domain"
)

// Capability 描述音源支持的能力，UI 依据它决定展示哪些控件，
// 而不是硬编码平台名。
type Capability uint32

const (
	// CapSearch 支持远程搜索。
	CapSearch Capability = 1 << iota
	// CapMultiPart 支持分 P / 分集。
	CapMultiPart
	// CapRemotePlaylistSync 支持远端歌单或收藏夹同步。
	CapRemotePlaylistSync
	// CapOfficialLyrics 提供官方歌词。
	CapOfficialLyrics
	// CapQualitySelect 支持选择音质档位。
	CapQualitySelect
)

// Has 判断能力位是否包含 flag。
func (c Capability) Has(flag Capability) bool {
	return c&flag != 0
}

// Candidate 是搜索结果中的一个条目。
type Candidate struct {
	Ref        domain.SourceRef
	Title      string
	Artist     string
	DurationMS int64
	CoverURL   string
	Extra      map[string]string
}

// Stream 是 Source 与音频引擎之间唯一的契约。
//
// 带上 Headers 与 Codec 是刻意的：B 站要求 Referer、网易云要求 cookie，
// 这些差异不该泄漏到播放引擎里；Codec 决定用哪个解码后端。
type Stream struct {
	URL        string
	Headers    map[string]string
	Codec      domain.Codec
	DurationMS int64
	ExpiresAt  time.Time
	Size       int64 // 总字节数，-1 表示未知（B 站不支持 HEAD）
}

// LyricDocument 是音源提供的一份歌词原文。
type LyricDocument struct {
	Source      string // 音源标识
	SourceLabel string // 展示用来源名，如 "网易云官方" / "B 站字幕"
	Format      string // "lrc" 或 "plain"
	RawText     string
	Confidence  float64
}

// RemotePlaylist 是音源侧的远端歌单或收藏夹。
type RemotePlaylist struct {
	ExternalID string
	Title      string
	TrackCount int
	Syncable   bool
}

// SyncReport 是一次远端同步的结果摘要。
type SyncReport struct {
	Added   int
	Removed int
	Skipped int
}

// Prompt 是需要用户输入的一项信息（手机号、验证码等）。
type Prompt struct {
	Kind    string // "phone" / "code" / "password"
	Message string
	Secret  bool
}

// LoginInteraction 由 UI 层实现，音源通过它把二维码与输入交给前端。
//
// 这样登录流程就可以被 TUI 渲染，而不是像 v1 那样写死在 service 里。
type LoginInteraction interface {
	// QRCode 把二维码内容交给 UI 渲染；UI 返回 nil 后音源继续轮询状态。
	QRCode(ctx context.Context, payload string) error
	// Prompt 让用户输入一项信息。
	Prompt(ctx context.Context, p Prompt) (string, error)
	// Status 展示一句进度文案（"等待扫码"、"登录成功"等）。
	Status(ctx context.Context, message string) error
}

// Source 是一个平台音源的完整抽象。
type Source interface {
	// ID 是稳定的音源标识，用于落库与配置，不可随意更改。
	ID() string
	// Caps 返回该音源支持的能力。
	Caps() Capability

	Account(ctx context.Context) (*domain.Account, error)
	Login(ctx context.Context, ui LoginInteraction) error
	Logout(ctx context.Context) error

	Search(ctx context.Context, query string, limit int) ([]Candidate, error)
	// Resolve 解析出可直接播放的 Stream；过期时间由音源负责填写。
	Resolve(ctx context.Context, ref domain.SourceRef, q domain.Quality) (*Stream, error)
	Lyrics(ctx context.Context, ref domain.SourceRef) ([]LyricDocument, error)

	RemotePlaylists(ctx context.Context) ([]RemotePlaylist, error)
	SyncRemote(ctx context.Context, externalID string) (SyncReport, error)
}
