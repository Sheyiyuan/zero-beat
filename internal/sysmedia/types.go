// Package sysmedia 把播放状态投喂给系统媒体面板，并把面板/媒体键的操作
// 转回播放器。平台实现放在同包的 build tag 文件里，TUI 只依赖这里的契约。
package sysmedia

import (
	"time"

	"github.com/sheyiyuan/zero-beat/internal/audio"
)

// LoopStatus 是 MPRIS 的循环语义，与播放模式一一对应。
type LoopStatus string

const (
	LoopNone     LoopStatus = "None"     // 播完即停
	LoopTrack    LoopStatus = "Track"    // 单曲循环
	LoopPlaylist LoopStatus = "Playlist" // 列表循环
)

// Valid 判断是否为 MPRIS 接受的三个值之一；大小写敏感。
func (l LoopStatus) Valid() bool {
	switch l {
	case LoopNone, LoopTrack, LoopPlaylist:
		return true
	default:
		return false
	}
}

// Snapshot 是推给系统面板的播放状态快照。Position 由调用方每 250ms 更新一次，
// 平台实现可结合本地时钟做插值，避免高频写系统接口。
type Snapshot struct {
	TrackID  string
	Title    string
	Artist   string
	Album    string
	Artwork  string // 本地封面文件路径；空表示没有封面
	Duration time.Duration
	Position time.Duration
	State    audio.State
	Volume   float64
	Muted    bool
	Shuffle  bool
	Loop     LoopStatus
	CanNext  bool
	CanPrev  bool
}

// CommandKind 是系统面板发回播放器的操作类型。
type CommandKind int

const (
	CommandToggle CommandKind = iota
	CommandPlay
	CommandPause
	CommandNext
	CommandPrevious
	CommandStop
	CommandSeekTo
	CommandSeekBy
	CommandSetVolume
	CommandSetShuffle
	CommandSetLoopStatus
)

// Command 是系统面板发来的操作。
type Command struct {
	Kind     CommandKind
	TrackID  string        // SeekTo 用；用于丢弃过期的跳转请求
	Offset   time.Duration // SeekBy 用
	Position time.Duration // SeekTo 用
	Volume   float64       // SetVolume 用
	Shuffle  bool          // SetShuffle 用
	Loop     LoopStatus    // SetLoopStatus 用
}

// Service 是系统媒体集成。
type Service interface {
	Publish(Snapshot)
	Commands() <-chan Command
	Close()
}
