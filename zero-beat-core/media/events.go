package media

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
		return "缓冲中"
	case StatePlaying:
		return "播放中"
	case StatePaused:
		return "已暂停"
	case StateEnded:
		return "已结束"
	case StateFailed:
		return "出错"
	default:
		return "空闲"
	}
}

// EventType 区分事件类型。
type EventType int

const (
	// EventStateChanged 播放状态变化。
	EventStateChanged EventType = iota
	// EventEnded 当前曲目播放完毕。
	EventEnded
	// EventFailed 播放失败，Err 里有原因。
	EventFailed
)

// Event 是引擎发往 UI 的事件。
type Event struct {
	Type  EventType
	State State
	Err   error
}
