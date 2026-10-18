// Package audio 定义播放引擎抽象。
//
// 引擎只认 source.Stream（URL + Headers + Codec + 过期时间），
// 因此新增音源不需要改引擎；新增编码只需要新增解码后端。
package audio

import (
	"context"
	"errors"
	"time"

	"github.com/sheyiyuan/hbp/internal/source"
)

// ErrNotSupported 表示当前解码后端无法处理该编码。
var ErrNotSupported = errors.New("audio: 当前解码后端不支持该编码")

// State 是播放状态。
type State int

const (
	StateIdle State = iota
	StateBuffering
	StatePlaying
	StatePaused
	StateEnded
	StateFailed
)

func (s State) String() string {
	switch s {
	case StateBuffering:
		return "buffering"
	case StatePlaying:
		return "playing"
	case StatePaused:
		return "paused"
	case StateEnded:
		return "ended"
	case StateFailed:
		return "failed"
	default:
		return "idle"
	}
}

// EventType 区分引擎事件。
type EventType int

const (
	EventStateChanged EventType = iota
	EventPosition
	EventEnded
	EventFailed
)

// Event 是引擎发往 UI 的唯一通道。
//
// UI 侧必须在 goroutine 里把它转成 tea.Msg，不要在回调里直接改状态。
type Event struct {
	Type     EventType
	State    State
	Position time.Duration
	Err      error
}

// Engine 是播放引擎。
type Engine interface {
	Play(ctx context.Context, s *source.Stream) error
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Stop(ctx context.Context) error
	Seek(ctx context.Context, pos time.Duration) error

	// SetVolume 设置用户音量，范围 0..1。
	SetVolume(volume float64) error
	// SetCompensationDB 设置音量补偿（分贝），叠加在用户音量之上。
	SetCompensationDB(db float64) error

	Position() time.Duration
	State() State
	// Events 返回事件通道；引擎关闭时通道被关闭。
	Events() <-chan Event
	Close() error
}
