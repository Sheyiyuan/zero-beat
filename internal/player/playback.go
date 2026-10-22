package player

import (
	"context"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// PlayResult 是一次异步解析完成后的处理结果。
type PlayResult struct {
	Stale      bool
	Retry      bool
	Started    bool
	Song       store.Song
	Stream     source.Stream
	StartAt    time.Duration
	RetryCount int
	Err        error
}

// BeginPlay 登记一次新的播放请求，返回请求号和规范化后的曲目。
func (c *Controller) BeginPlay(song store.Song, startAt time.Duration, retry int) (int, store.Song) {
	c.state.Request++
	c.state.Awaiting = true
	c.state.Retry = retry
	song = store.NormalizeSong(song)
	c.state.Current = &song
	c.state.PlaybackState = audio.StateBuffering
	c.state.Position = startAt
	c.state.ResumeAt = 0
	c.state.Duration = time.Duration(song.Duration) * time.Second
	c.state.Prefetch.Clear()
	return c.state.Request, song
}

// CompletePlay 处理异步解析结果；过期请求会被丢弃。
func (c *Controller) CompletePlay(ctx context.Context, req int, song store.Song, stream source.Stream, startAt time.Duration, retry int, clip store.Clip, preview bool, err error) PlayResult {
	if req != c.state.Request {
		return PlayResult{Stale: true}
	}
	c.state.Retry = retry
	if err != nil {
		if retry < 1 {
			c.state.Retry = retry + 1
			c.state.Awaiting = true
			c.state.PlaybackState = audio.StateBuffering
			return PlayResult{Retry: true, Song: song, StartAt: startAt, RetryCount: c.state.Retry}
		}
		c.state.Awaiting = false
		if c.engine != nil {
			c.engine.Stop()
		}
		c.state.PlaybackState = audio.StateFailed
		return PlayResult{Err: err}
	}

	song = store.NormalizeSong(song)
	for i := range c.state.Queue {
		if store.NormalizeSong(c.state.Queue[i]).ID == song.ID {
			c.state.Queue[i] = song
			break
		}
	}
	c.state.Current = &song
	c.state.Awaiting = false
	c.state.PlaybackState = audio.StatePlaying
	c.state.Position = startAt
	c.state.Duration = stream.Duration
	if c.state.Duration == 0 && song.Duration > 0 {
		c.state.Duration = time.Duration(song.Duration) * time.Second
	}
	if c.state.Duration > 0 && c.state.Position > c.state.Duration {
		c.state.Position = c.state.Duration
	}
	c.state.ResumeAt = 0
	c.state.Prefetch.Clear()

	var clipStart, clipEnd int64
	if !preview && clip.Active() {
		clipStart, clipEnd = clip.StartMS, clip.EndMS
	}
	if c.engine != nil {
		durationMS := stream.Duration.Milliseconds()
		if durationMS == 0 && song.Duration > 0 {
			durationMS = int64(song.Duration) * 1000
		}
		audioStream := audio.Stream{
			URL:         stream.URL,
			Headers:     stream.Headers,
			Codec:       stream.Codec,
			DurationMS:  durationMS,
			ExpiresAt:   stream.ExpiresAt,
			Quality:     stream.Quality,
			ClipStartMS: clipStart,
			ClipEndMS:   clipEnd,
		}
		if err := c.engine.PlayAt(ctx, audioStream, startAt); err != nil {
			c.state.PlaybackState = audio.StateFailed
			return PlayResult{Err: err}
		}
	}
	c.Persist()
	return PlayResult{Started: true, Song: song, Stream: stream, StartAt: startAt}
}

// ToggleAction 是 Toggle 需要 TUI 执行的后续动作。
type ToggleAction int

const (
	ToggleNone ToggleAction = iota
	ToggleBusy
	TogglePaused
	ToggleResumed
	ToggleNeedPlay
)

// ToggleResult 是起播/暂停/继续的结果。
type ToggleResult struct {
	Action  ToggleAction
	Song    store.Song
	StartAt time.Duration
}

// Toggle 处理「起播 / 暂停 / 继续」。
func (c *Controller) Toggle() ToggleResult {
	if c.engine == nil {
		return ToggleResult{}
	}
	if c.state.Awaiting {
		return ToggleResult{Action: ToggleBusy}
	}
	switch c.engine.State() {
	case audio.StatePlaying:
		c.engine.Pause()
		c.state.PlaybackState = audio.StatePaused
		c.Persist()
		return ToggleResult{Action: TogglePaused}
	case audio.StatePaused:
		s := c.engine.CurrentStream()
		if s.URL != "" && c.state.Current != nil && !s.ExpiresAt.IsZero() && c.now().After(s.ExpiresAt) {
			c.state.PlaybackState = audio.StateBuffering
			return ToggleResult{Action: ToggleNeedPlay, Song: *c.state.Current, StartAt: c.state.Position}
		}
		if s.URL != "" {
			c.engine.Resume()
			c.state.PlaybackState = audio.StatePlaying
			c.Persist()
			return ToggleResult{Action: ToggleResumed}
		}
	case audio.StateEnded:
		if c.state.Current != nil {
			c.state.Position = 0
			c.state.ResumeAt = 0
			c.state.PlaybackState = audio.StateBuffering
			return ToggleResult{Action: ToggleNeedPlay, Song: *c.state.Current, StartAt: 0}
		}
	}
	if c.state.Current == nil {
		return ToggleResult{}
	}
	at := c.state.ResumeAt
	if at <= 0 {
		at = c.state.Position
	}
	c.state.PlaybackState = audio.StateBuffering
	return ToggleResult{Action: ToggleNeedPlay, Song: *c.state.Current, StartAt: at}
}

// SeekResult 是跳转结果。
type SeekResult struct {
	Action   ToggleAction // ToggleNeedPlay 表示需要重新解析
	Song     store.Song
	StartAt  time.Duration
	Clamped  bool
	NoEngine bool
	Err      error
}

// Seek 跳转 delta；clip 为当前片段，preview 为编辑器整首预览。
func (c *Controller) Seek(ctx context.Context, delta time.Duration, clip store.Clip, preview bool) SeekResult {
	if c.state.Current == nil {
		return SeekResult{}
	}
	if c.state.Awaiting {
		return SeekResult{Action: ToggleBusy}
	}
	target := c.state.Position + delta
	if c.engine != nil && c.engine.State() != audio.StateIdle && c.engine.CurrentStream().URL != "" {
		target = c.engine.Position() + delta
	}
	if target < 0 {
		target = 0
	}
	if c.state.Duration > 0 && target > c.state.Duration {
		target = c.state.Duration
	}
	var clamped bool
	if !preview && clip.Active() {
		start := time.Duration(clip.StartMS) * time.Millisecond
		end := c.state.Duration
		if clip.EndMS > 0 {
			end = time.Duration(clip.EndMS) * time.Millisecond
		}
		if target < start {
			target, clamped = start, true
		}
		if end > 0 && target >= end {
			target = end - time.Millisecond
			if target < start {
				target = start
			}
			clamped = true
		}
	}
	c.state.Position = target

	if c.engine == nil || c.engine.State() == audio.StateIdle || c.engine.CurrentStream().URL == "" {
		c.state.ResumeAt = target
		c.Persist()
		return SeekResult{Clamped: clamped, NoEngine: true}
	}
	stream := c.engine.CurrentStream()
	if !stream.ExpiresAt.IsZero() && c.now().Add(5*time.Minute).After(stream.ExpiresAt) {
		c.state.PlaybackState = audio.StateBuffering
		return SeekResult{Action: ToggleNeedPlay, Song: *c.state.Current, StartAt: target, Clamped: clamped}
	}
	wasPaused := c.engine.State() == audio.StatePaused
	if err := c.engine.Seek(ctx, target); err != nil {
		c.state.PlaybackState = audio.StateBuffering
		return SeekResult{Action: ToggleNeedPlay, Song: *c.state.Current, StartAt: target, Clamped: clamped, Err: err}
	}
	// 跳转不改变播放状态：暂停中仍保持暂停，播放中仍是播放，按钮与引擎一致。
	if wasPaused {
		c.engine.Pause()
		c.state.PlaybackState = audio.StatePaused
	} else {
		c.state.PlaybackState = audio.StatePlaying
	}
	c.state.ResumeAt = 0
	c.Persist()
	return SeekResult{Clamped: clamped}
}

// EventResult 是引擎事件的处理结果。
type EventResult struct {
	Retry   bool
	Ended   bool
	Song    store.Song
	StartAt time.Duration
	Err     error
}

// HandleEvent 更新播放状态；需要重试/切歌时由 TUI 发起后续命令。
func (c *Controller) HandleEvent(ev audio.Event) EventResult {
	// 切歌解析期间，旧流残留下来的失败/结束/停止事件不能打断新请求，
	// 也不能提前消耗重试次数（main 里这条守卫在 TUI，重构后下沉到这里）。
	if c.state.Awaiting &&
		(ev.Type == audio.EventFailed || ev.Type == audio.EventEnded || ev.State == audio.StateIdle) {
		return EventResult{}
	}
	switch ev.Type {
	case audio.EventFailed:
		if c.state.Current != nil && c.state.Retry < 1 {
			c.state.Retry++
			c.state.PlaybackState = audio.StateBuffering
			return EventResult{Retry: true, Song: *c.state.Current, StartAt: c.state.Position}
		}
		c.state.PlaybackState = audio.StateFailed
		return EventResult{Err: ev.Err}
	case audio.EventEnded:
		c.state.Retry = 0
		c.state.ResumeAt = 0
		c.state.PlaybackState = audio.StateEnded
		return EventResult{Ended: true}
	default:
		return EventResult{}
	}
}

// TickResult 是 250ms 心跳更新结果。
type TickResult struct {
	SleepPaused bool
	Persisted   bool
}

// Tick 同步引擎位置/时长、睡眠定时与周期落盘。
func (c *Controller) Tick(now time.Time) TickResult {
	var out TickResult
	if c.engine != nil && c.engine.State() != audio.StateIdle && !c.state.Awaiting {
		c.state.Position = c.engine.Position()
		if d := c.engine.Duration(); d > 0 {
			c.state.Duration = d
		}
	} else if c.state.Duration == 0 && c.state.Current != nil && c.state.Current.Duration > 0 {
		c.state.Duration = time.Duration(c.state.Current.Duration) * time.Second
	}
	if c.state.Duration > 0 && c.state.Position > c.state.Duration {
		c.state.Position = c.state.Duration
	}
	if !c.state.SleepUntil.IsZero() && !now.Before(c.state.SleepUntil) {
		c.state.SleepUntil = time.Time{}
		if c.engine != nil && c.engine.State() == audio.StatePlaying {
			c.engine.Pause()
			c.state.PlaybackState = audio.StatePaused
		}
		out.SleepPaused = true
	}
	if c.NeedsPersist() && (c.state.LastPersist.IsZero() || now.Sub(c.state.LastPersist) >= 5*time.Second) {
		c.Persist()
		out.Persisted = true
	}
	return out
}

// Stop 停止引擎并清空当前曲目。
func (c *Controller) Stop() {
	if c.engine != nil {
		c.engine.Stop()
	}
	c.state.Current = nil
	c.state.PlaybackState = audio.StateIdle
	c.state.Position = 0
	c.state.Duration = 0
	c.state.ResumeAt = 0
	c.state.Retry = 0
	c.state.Awaiting = false
}

// PlayResolved 直接播放已经解析好的流（预解析命中时用）。
func (c *Controller) PlayResolved(ctx context.Context, song store.Song, stream source.Stream, startAt time.Duration, clip store.Clip, preview bool) PlayResult {
	req, normalized := c.BeginPlay(song, startAt, 0)
	return c.CompletePlay(ctx, req, normalized, stream, startAt, 0, clip, preview, nil)
}

// EngineState 返回引擎当前状态；没有引擎时为 Idle。
func (c *Controller) EngineState() audio.State {
	if c.engine == nil {
		return audio.StateIdle
	}
	return c.engine.State()
}

// LivePosition 返回引擎实时位置；尚未起播时返回断点/保存位置。
func (c *Controller) LivePosition() time.Duration {
	if c.engine != nil && c.engine.State() != audio.StateIdle && !c.state.Awaiting {
		return c.engine.Position()
	}
	return c.state.Position
}

// PreviewAt 用当前直链做整首预览，清除片段区间。
func (c *Controller) PreviewAt(ctx context.Context, pos time.Duration, paused bool) (bool, error) {
	if c.engine == nil || c.state.Current == nil {
		return false, nil
	}
	stream := c.engine.CurrentStream()
	if stream.URL == "" {
		return false, nil
	}
	if !stream.ExpiresAt.IsZero() && c.now().Add(time.Minute).After(stream.ExpiresAt) {
		return false, nil
	}
	if pos < 0 {
		pos = 0
	}
	if c.state.Duration > 0 && pos > c.state.Duration {
		pos = c.state.Duration
	}
	stream.ClipStartMS = 0
	stream.ClipEndMS = 0
	if err := c.engine.PlayAt(ctx, stream, pos); err != nil {
		return false, err
	}
	c.state.Request++
	c.state.Awaiting = false
	c.state.Retry = 0
	c.state.ResumeAt = 0
	c.state.Position = pos
	if paused {
		c.engine.Pause()
		c.state.PlaybackState = audio.StatePaused
	} else {
		c.state.PlaybackState = audio.StatePlaying
	}
	return true, nil
}

// RestartWithClip 复用当前直链，按新片段重新起播。
func (c *Controller) RestartWithClip(ctx context.Context, clip store.Clip, pos time.Duration, paused bool) bool {
	if c.engine == nil {
		c.state.Position = pos
		if paused {
			c.state.ResumeAt = pos
		}
		return false
	}
	stream := c.engine.CurrentStream()
	if stream.URL == "" || (!stream.ExpiresAt.IsZero() && c.now().Add(time.Minute).After(stream.ExpiresAt)) {
		return false
	}
	stream.ClipStartMS = clip.StartMS
	stream.ClipEndMS = clip.EndMS
	if err := c.engine.PlayAt(ctx, stream, pos); err != nil {
		return false
	}
	c.state.Request++
	c.state.Awaiting = false
	c.state.Retry = 0
	c.state.ResumeAt = 0
	c.state.Position = pos
	if paused {
		c.engine.Pause()
		c.state.PlaybackState = audio.StatePaused
	} else {
		c.state.PlaybackState = audio.StatePlaying
	}
	c.Persist()
	return true
}
