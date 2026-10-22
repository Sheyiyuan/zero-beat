package player

import (
	"context"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// Mode 是播放模式。
type Mode int

const (
	ModeSequence Mode = iota
	ModeLoopOne
	ModeShuffle
)

// String 返回界面用的模式名。
func (m Mode) String() string {
	switch m {
	case ModeLoopOne:
		return "单曲"
	case ModeShuffle:
		return "随机"
	default:
		return "顺序"
	}
}

// QueueSaver 持久化队列；由 app.Service 实现。
type QueueSaver interface {
	SaveQueue(songs []store.Song, index int, positionMS int64) error
}

// Engine 是播放器依赖的音频输出能力，*audio.Engine 实现。
type Engine interface {
	Events() <-chan audio.Event
	State() audio.State
	SetVolume(float64)
	SetCompensationDB(float64)
	PlayAt(ctx context.Context, s audio.Stream, at time.Duration) error
	Pause()
	Resume()
	Stop()
	Seek(ctx context.Context, pos time.Duration) error
	Position() time.Duration
	Duration() time.Duration
	CurrentStream() audio.Stream
}

// State 是可由 TUI 推送/拉取的播放状态快照。
type State struct {
	Queue          []store.Song
	Index          int
	Current        *store.Song
	PlaybackState  audio.State
	Position       time.Duration
	Duration       time.Duration
	Mode           Mode
	Volume         float64
	CompDB         float64
	Muted          bool
	ResumeAt       time.Duration
	Retry          int
	Awaiting       bool
	Request        int
	Prefetch       Prefetch
	ShuffleBase    []store.Song
	ShuffleBaseIdx int
	SleepUntil     time.Time
	SleepSlot      int
	Quality        source.Quality
	LastPersist    time.Time
}

// Controller 是播放器业务状态机。它不依赖 Bubble Tea，也不做渲染。
type Controller struct {
	engine    Engine
	saver     QueueSaver
	now       func() time.Time
	shuffleFn func([]store.Song)

	state State
}

// New 构造播放器控制器。
func New(engine Engine, saver QueueSaver) *Controller {
	return &Controller{engine: engine, saver: saver, now: time.Now}
}

// SetClock 测试注入时钟。
func (c *Controller) SetClock(now func() time.Time) { c.now = now }

// SetShuffleFunc 测试注入确定性洗牌。
func (c *Controller) SetShuffleFunc(fn func([]store.Song)) { c.shuffleFn = fn }

// Load 用 TUI 当前镜像覆盖控制器状态。
func (c *Controller) Load(s State) { c.state = s }

// Export 导出状态给 TUI 镜像。
func (c *Controller) Export() State { return c.state }

// Queue 返回队列。
func (c *Controller) Queue() []store.Song { return c.state.Queue }

// Index 返回当前索引。
func (c *Controller) Index() int { return c.state.Index }

// Current 返回当前曲目。
func (c *Controller) Current() (store.Song, bool) {
	if c.state.Current == nil {
		return store.Song{}, false
	}
	return *c.state.Current, true
}

// Mode 返回播放模式。
func (c *Controller) Mode() Mode { return c.state.Mode }

// Quality 返回音质偏好。
func (c *Controller) Quality() source.Quality { return c.state.Quality }

// Volume 返回音量。
func (c *Controller) Volume() float64 { return c.state.Volume }

// CompDB 返回响度补偿。
func (c *Controller) CompDB() float64 { return c.state.CompDB }

// Muted 返回是否静音。
func (c *Controller) Muted() bool { return c.state.Muted }

// Position 返回播放位置。
func (c *Controller) Position() time.Duration { return c.state.Position }

// Duration 返回当前曲目时长。
func (c *Controller) Duration() time.Duration { return c.state.Duration }

// Awaiting 返回是否正在等待解析。
func (c *Controller) Awaiting() bool { return c.state.Awaiting }

// SleepUntil 返回睡眠定时结束时间。
func (c *Controller) SleepUntil() time.Time { return c.state.SleepUntil }

// LoadQueue 用新队列替换，随机模式下保存原始顺序并洗牌。
func (c *Controller) LoadQueue(queue []store.Song, idx int) {
	normalized := make([]store.Song, len(queue))
	for i := range queue {
		normalized[i] = store.NormalizeSong(queue[i])
	}
	c.state.ShuffleBase = nil
	c.state.Queue = normalized
	if len(normalized) == 0 {
		c.state.Index = 0
		c.state.Prefetch.Clear()
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(normalized) {
		idx = len(normalized) - 1
	}
	c.state.Index = idx
	targetID := normalized[idx].ID
	if c.state.Mode == ModeShuffle {
		c.state.ShuffleBase = append([]store.Song(nil), normalized...)
		c.state.ShuffleBaseIdx = idx
		c.shuffleKeeping(targetID)
	}
	c.state.Prefetch.Clear()
}

// LoadQueueRandom 用新队列替换并整体洗牌，索引归零——用于「随机播放歌单」，
// 第一首也随机（LoadQueue 会保留指定曲目在原位，那是给显式点歌用的）。
// 仅在随机模式下调用；返回 false 表示队列为空。
func (c *Controller) LoadQueueRandom(queue []store.Song) bool {
	if len(queue) == 0 {
		c.LoadQueue(nil, 0)
		return false
	}
	normalized := make([]store.Song, len(queue))
	for i := range queue {
		normalized[i] = store.NormalizeSong(queue[i])
	}
	c.state.Queue = append([]store.Song(nil), normalized...)
	c.state.Index = 0
	c.state.ShuffleBase = append([]store.Song(nil), normalized...)
	c.state.ShuffleBaseIdx = 0
	c.state.Prefetch.Clear()
	// keepID 为空串：Shuffle 找不到要保留的曲目，整队一起洗。
	c.shuffleKeeping("")
	c.state.Index = 0
	return true
}

// Enqueue 追加到队列末尾，返回新索引。
func (c *Controller) Enqueue(song store.Song) int {
	song = store.NormalizeSong(song)
	c.state.Queue = append(c.state.Queue, song)
	if len(c.state.Queue) == 1 && c.state.Current == nil {
		c.state.Index = 0
	}
	c.state.Prefetch.Clear()
	return len(c.state.Queue) - 1
}

// InsertNext 把歌曲插到当前曲目之后，返回插入位置。
func (c *Controller) InsertNext(song store.Song) int {
	song = store.NormalizeSong(song)
	queue, idx, cursor := InsertNext(c.state.Queue, c.state.Index, c.state.Index, c.state.Current != nil, song)
	c.state.Queue, c.state.Index = queue, idx
	c.state.Prefetch.Clear()
	return cursor
}

// MoveItem 移动队列条目，返回新游标。
func (c *Controller) MoveItem(cursor, delta int) int {
	queue, idx, next := Move(c.state.Queue, c.state.Index, cursor, delta)
	c.state.Queue, c.state.Index = queue, idx
	c.state.Prefetch.Clear()
	return next
}

// MoveToNext 把游标条目移到当前曲目之后，返回新游标。
func (c *Controller) MoveToNext(cursor int) int {
	queue, idx, next := MoveToNext(c.state.Queue, c.state.Index, cursor, c.state.Current != nil)
	c.state.Queue, c.state.Index, cursor = queue, idx, next
	c.state.Prefetch.Clear()
	return cursor
}

// RemoveResult 是删除队列条目的结果。
type RemoveResult struct {
	Removed         store.Song
	RemovingCurrent bool
	Empty           bool
}

// RemoveItem 删除游标条目，返回结果和新索引；调用方负责播放决策。
func (c *Controller) RemoveItem(cursor int) (RemoveResult, int) {
	queue, idx, cur, removed, removingCurrent := Remove(c.state.Queue, c.state.Index, cursor)
	if len(queue) == len(c.state.Queue) {
		return RemoveResult{}, cursor
	}
	c.state.Queue, c.state.Index = queue, idx
	c.state.Prefetch.Clear()
	return RemoveResult{Removed: removed, RemovingCurrent: removingCurrent, Empty: len(queue) == 0}, cur
}

// Clear 清空待播队列，保留当前曲目。
func (c *Controller) Clear() {
	c.state.Queue, c.state.Index, _ = Clear(c.state.Current)
	c.state.Prefetch.Clear()
}

// SetMode 切换播放模式；返回是否发生洗牌/恢复。
func (c *Controller) SetMode(next Mode) (shuffled, restored bool) {
	wasShuffle := c.state.Mode == ModeShuffle
	hadBase := len(c.state.ShuffleBase) > 0
	if wasShuffle {
		c.restoreShuffle()
	}
	c.state.Mode = next
	if next == ModeShuffle {
		c.startShuffle()
		shuffled = len(c.state.Queue) > 1
	}
	if wasShuffle && next != ModeShuffle && hadBase {
		restored = true
	}
	c.state.Prefetch.Clear()
	return shuffled, restored
}

func (c *Controller) startShuffle() {
	if len(c.state.Queue) < 2 {
		c.state.ShuffleBase = nil
		return
	}
	c.state.ShuffleBase = append([]store.Song(nil), c.state.Queue...)
	c.state.ShuffleBaseIdx = c.state.Index
	c.shuffleKeeping(c.currentID())
}

func (c *Controller) restoreShuffle() {
	if len(c.state.ShuffleBase) == 0 {
		return
	}
	queue, idx, _ := Restore(c.state.Queue, c.state.ShuffleBase, c.currentID(), c.state.ShuffleBaseIdx)
	c.state.Queue, c.state.Index = queue, idx
	c.state.ShuffleBase = nil
}

func (c *Controller) shuffleKeeping(id string) {
	var fn func([]store.Song)
	if c.shuffleFn != nil {
		fn = c.shuffleFn
	}
	queue, idx, _ := Shuffle(c.state.Queue, id, c.state.Index, fn)
	c.state.Queue, c.state.Index = queue, idx
}

func (c *Controller) currentID() string {
	if c.state.Current == nil {
		return ""
	}
	return c.state.Current.ID
}

// Next 前进一首；LoopOne 时仍返回当前曲目。返回曲目、索引和是否有效。
func (c *Controller) Next() (store.Song, int, bool) {
	if len(c.state.Queue) == 0 {
		return store.Song{}, 0, false
	}
	if c.state.Mode == ModeLoopOne && c.state.Current != nil {
		return *c.state.Current, c.state.Index, true
	}
	next := NextIndex(c.state.Index, len(c.state.Queue))
	c.state.Index = next
	return c.state.Queue[next], next, true
}

// Prev 返回上一首。
func (c *Controller) Prev() (store.Song, int, bool) {
	if len(c.state.Queue) == 0 {
		return store.Song{}, 0, false
	}
	prev := PrevIndex(c.state.Index, len(c.state.Queue))
	c.state.Index = prev
	c.state.Prefetch.Clear()
	return c.state.Queue[prev], prev, true
}

// NextSongID 返回顺序模式下将要播放的曲目 ID。
func (c *Controller) NextSongID() string {
	if c.state.Mode != ModeSequence || len(c.state.Queue) < 2 {
		return ""
	}
	next := NextIndex(c.state.Index, len(c.state.Queue))
	if next == c.state.Index {
		return ""
	}
	return store.NormalizeSong(c.state.Queue[next]).ID
}

// Prefetch 返回预解析缓存。
func (c *Controller) PrefetchCache() *Prefetch { return &c.state.Prefetch }

// Persist 保存队列与进度。
func (c *Controller) Persist() {
	if c.saver == nil {
		return
	}
	position := c.state.Position.Milliseconds()
	if position < 0 {
		position = 0
	}
	_ = c.saver.SaveQueue(c.state.Queue, c.state.Index, position)
	c.state.LastPersist = c.now()
}

// NeedsPersist 判断当前状态是否需要落盘。
func (c *Controller) NeedsPersist() bool {
	if c.state.Current == nil {
		return false
	}
	return c.state.PlaybackState == audio.StatePlaying ||
		c.state.PlaybackState == audio.StatePaused ||
		c.state.ResumeAt > 0
}

// SetQuality 设置音质偏好。
func (c *Controller) SetQuality(q source.Quality) { c.state.Quality = q }

// CycleQuality 循环音质偏好。
func (c *Controller) CycleQuality() source.Quality {
	c.state.Quality = source.Quality((int(c.state.Quality) + 1) % 3)
	c.state.Prefetch.Clear()
	return c.state.Quality
}

// SetVolume 设置音量并应用到引擎。
func (c *Controller) SetVolume(v float64) {
	c.state.Volume = v
	c.state.Muted = false
	if c.engine != nil {
		c.engine.SetVolume(v)
	}
}

// ToggleMute 切换静音。
func (c *Controller) ToggleMute() bool {
	c.state.Muted = !c.state.Muted
	if c.engine != nil {
		if c.state.Muted {
			c.engine.SetVolume(0)
		} else {
			c.engine.SetVolume(c.state.Volume)
		}
	}
	return c.state.Muted
}

// SetCompDB 设置响度补偿并应用到引擎。
func (c *Controller) SetCompDB(v float64) {
	if v < -20 {
		v = -20
	}
	if v > 12 {
		v = 12
	}
	c.state.CompDB = v
	if c.engine != nil {
		c.engine.SetCompensationDB(v)
	}
}

// CycleSleep 循环睡眠定时，返回新的结束时间。
func (c *Controller) CycleSleep() time.Time {
	slots := []time.Duration{0, 15 * time.Minute, 30 * time.Minute, 60 * time.Minute, 120 * time.Minute}
	c.state.SleepSlot = (c.state.SleepSlot + 1) % len(slots)
	d := slots[c.state.SleepSlot]
	if d == 0 {
		c.state.SleepUntil = time.Time{}
		return c.state.SleepUntil
	}
	c.state.SleepUntil = c.now().Add(d)
	return c.state.SleepUntil
}

// SetIndex 设置当前队列索引（界面选择队列条目时用）。
func (c *Controller) SetIndex(idx int) {
	if idx < 0 {
		idx = 0
	}
	if idx >= len(c.state.Queue) {
		idx = len(c.state.Queue) - 1
	}
	if idx < 0 {
		idx = 0
	}
	c.state.Index = idx
}

// RestoreSession 恢复上次的队列、当前曲目与断点，不启动引擎。
func (c *Controller) RestoreSession(queue []store.Song, idx int, current *store.Song, position time.Duration) {
	c.LoadQueue(queue, idx)
	c.state.Current = current
	c.state.PlaybackState = audio.StatePaused
	c.state.Position = position
	c.state.ResumeAt = position
	c.state.Awaiting = false
	c.state.Retry = 0
	if current != nil {
		c.state.Duration = time.Duration(current.Duration) * time.Second
	}
}

// SetPosition 更新视图/断点位置，不重启引擎。
func (c *Controller) SetPosition(pos time.Duration) { c.state.Position = pos }

// SetBuffering 标记正在等待新的解析。
func (c *Controller) SetBuffering(pos time.Duration) {
	c.state.Position = pos
	c.state.PlaybackState = audio.StateBuffering
}

// SetResume 设置恢复会话的断点位置。
func (c *Controller) SetResume(pos time.Duration) { c.state.ResumeAt = pos }

// CurrentQuality 返回引擎当前流的音质标签。
func (c *Controller) CurrentQuality() string {
	if c.engine == nil {
		return ""
	}
	return c.engine.CurrentStream().Quality
}
