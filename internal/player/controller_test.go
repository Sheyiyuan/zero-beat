package player

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

type fakeEngine struct {
	state    audio.State
	stream   audio.Stream
	position time.Duration
	duration time.Duration
	volume   float64
	comp     float64
	events   chan audio.Event
	paused   bool
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{state: audio.StateIdle, events: make(chan audio.Event)}
}
func (f *fakeEngine) Events() <-chan audio.Event                      { return f.events }
func (f *fakeEngine) State() audio.State                              { return f.state }
func (f *fakeEngine) SetVolume(v float64)                             { f.volume = v }
func (f *fakeEngine) SetCompensationDB(v float64)                     { f.comp = v }
func (f *fakeEngine) Pause()                                          { f.state = audio.StatePaused }
func (f *fakeEngine) Resume()                                         { f.state = audio.StatePlaying }
func (f *fakeEngine) Stop()                                           { f.state = audio.StateIdle; f.stream = audio.Stream{} }
func (f *fakeEngine) Position() time.Duration                         { return f.position }
func (f *fakeEngine) Duration() time.Duration                         { return f.duration }
func (f *fakeEngine) CurrentStream() audio.Stream                     { return f.stream }
func (f *fakeEngine) Seek(_ context.Context, pos time.Duration) error { f.position = pos; return nil }
func (f *fakeEngine) PlayAt(_ context.Context, s audio.Stream, at time.Duration) error {
	f.state = audio.StatePlaying
	f.stream = s
	f.position = at
	f.duration = time.Duration(s.DurationMS) * time.Millisecond
	return nil
}

type fakeSaver struct {
	queue    []store.Song
	index    int
	position int64
	calls    int
}

func (f *fakeSaver) SaveQueue(songs []store.Song, index int, positionMS int64) error {
	f.queue = append([]store.Song(nil), songs...)
	f.index = index
	f.position = positionMS
	f.calls++
	return nil
}

func queueSongs(ids ...string) []store.Song {
	out := make([]store.Song, 0, len(ids))
	for _, id := range ids {
		out = append(out, store.Song{ID: id, Source: "fake", RemoteID: id, Page: 1, Title: id, Duration: 10})
	}
	return out
}

func TestControllerQueueAndMode(t *testing.T) {
	eng := newFakeEngine()
	saver := &fakeSaver{}
	c := New(eng, saver)
	c.SetShuffleFunc(reverse)
	c.LoadQueue(queueSongs("a", "b", "c"), 1)

	if got, idx, ok := c.Next(); !ok || got.ID != "c" || idx != 2 {
		t.Fatalf("Next 结果不符: %+v idx=%d ok=%v", got, idx, ok)
	}
	if got, idx, _ := c.Prev(); got.ID != "b" || idx != 1 {
		t.Fatalf("Prev 结果不符: %+v idx=%d", got, idx)
	}

	c.SetMode(ModeShuffle)
	ids := []string{}
	for _, s := range c.Queue() {
		ids = append(ids, s.ID)
	}
	if len(ids) != 3 || !contains(ids, "a") || c.Mode() != ModeShuffle {
		t.Fatalf("随机模式队列不符: %v", ids)
	}
	c.SetMode(ModeSequence)
	if got := c.Queue(); got[0].ID != "a" || got[1].ID != "b" || got[2].ID != "c" {
		t.Fatalf("退出随机后顺序不符: %+v", got)
	}
}

func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestControllerCompletePlayAndTick(t *testing.T) {
	eng := newFakeEngine()
	saver := &fakeSaver{}
	c := New(eng, saver)
	now := time.Unix(1000, 0)
	c.SetClock(func() time.Time { return now })
	c.LoadQueue(queueSongs("a", "b"), 0)
	song := c.Queue()[0]
	req, normalized := c.BeginPlay(song, 2*time.Second, 0)
	res := c.CompletePlay(context.Background(), req, normalized,
		source.Stream{URL: "https://x/a.mp3", Duration: 10 * time.Second, Quality: "mp3"}, 2*time.Second, 0, store.Clip{}, false, nil)
	cur, _ := c.Current()
	if !res.Started || eng.state != audio.StatePlaying || cur.ID != "a" {
		t.Fatalf("起播结果不符: %+v eng=%v", res, eng.state)
	}
	if c.Position() != 2*time.Second || c.Duration() != 10*time.Second {
		t.Fatalf("位置/时长不符: %v/%v", c.Position(), c.Duration())
	}

	eng.position = 7 * time.Second
	now = now.Add(6 * time.Second)
	tr := c.Tick(now)
	if c.Position() != 7*time.Second {
		t.Fatalf("Tick 未同步位置: %v", c.Position())
	}
	if saver.calls == 0 || tr.Persisted != true {
		t.Fatalf("应周期性落盘: calls=%d result=%+v", saver.calls, tr)
	}
	if stale := c.CompletePlay(context.Background(), req+1, normalized, source.Stream{}, 0, 0, store.Clip{}, false, nil); !stale.Stale {
		t.Fatal("过期请求应被丢弃")
	}
}

func TestControllerRetryAndEnded(t *testing.T) {
	c := New(nil, nil)
	c.LoadQueue(queueSongs("a", "b"), 0)
	current := c.Queue()[0]
	c.state.Current = &current
	c.state.PlaybackState = audio.StatePlaying

	retry := c.HandleEvent(audio.Event{Type: audio.EventFailed})
	if !retry.Retry || c.state.Retry != 1 {
		t.Fatalf("首次失败应重试: %+v", retry)
	}
	final := c.HandleEvent(audio.Event{Type: audio.EventFailed, Err: errors.New("boom")})
	if final.Err == nil || c.state.PlaybackState != audio.StateFailed {
		t.Fatalf("二次失败应进入失败态: %+v", final)
	}

	c.state.Retry = 0
	c.state.PlaybackState = audio.StatePlaying
	ended := c.HandleEvent(audio.Event{Type: audio.EventEnded})
	if !ended.Ended || c.state.PlaybackState != audio.StateEnded {
		t.Fatalf("结束事件不符: %+v", ended)
	}
}

func TestControllerSeekAndPreview(t *testing.T) {
	eng := newFakeEngine()
	saver := &fakeSaver{}
	c := New(eng, saver)
	c.LoadQueue(queueSongs("a", "b"), 0)
	req, song := c.BeginPlay(c.Queue()[0], 0, 0)
	if res := c.CompletePlay(context.Background(), req, song,
		source.Stream{URL: "https://x/a.mp3", Duration: 10 * time.Second}, 0, 0, store.Clip{}, false, nil); !res.Started {
		t.Fatalf("起播失败: %+v", res)
	}
	seek := c.Seek(context.Background(), 3*time.Second, store.Clip{}, false)
	if seek.Action != ToggleNone || c.Position() != 3*time.Second || eng.position != 3*time.Second {
		t.Fatalf("Seek 结果不符: res=%+v pos=%v eng=%v", seek, c.Position(), eng.position)
	}
	if ok, err := c.PreviewAt(context.Background(), 4*time.Second, true); !ok || err != nil {
		t.Fatalf("预览失败: ok=%v err=%v", ok, err)
	}
	if eng.stream.ClipStartMS != 0 || eng.stream.ClipEndMS != 0 || c.EngineState() != audio.StatePaused {
		t.Fatalf("预览应清除片段并暂停: %+v state=%v", eng.stream, c.EngineState())
	}
	if ok := c.RestartWithClip(context.Background(), store.Clip{StartMS: 1000, EndMS: 5000}, time.Second, false); !ok {
		t.Fatal("按新片段重播失败")
	}
	if eng.stream.ClipStartMS != 1000 || eng.stream.ClipEndMS != 5000 || c.EngineState() != audio.StatePlaying {
		t.Fatalf("片段重播不符: %+v state=%v", eng.stream, c.EngineState())
	}
}

// TestHandleEventIgnoredWhileAwaiting 验证切歌解析期间旧流的失败/结束事件
// 被忽略：不切歌、不消耗重试次数；解析完成后事件恢复正常处理。
func TestHandleEventIgnoredWhileAwaiting(t *testing.T) {
	eng := newFakeEngine()
	c := New(eng, &fakeSaver{})
	song := queueSongs("a")[0]
	c.LoadQueue(queueSongs("a", "b"), 0)
	c.BeginPlay(song, 0, 0)
	if !c.state.Awaiting {
		t.Fatal("BeginPlay 后应处于等待解析状态")
	}

	if res := c.HandleEvent(audio.Event{Type: audio.EventEnded}); res.Ended || res.Retry || res.Err != nil {
		t.Fatalf("缓冲态收到 Ended 应忽略，实际 %+v", res)
	}
	if c.state.PlaybackState != audio.StateBuffering {
		t.Fatalf("缓冲态不应被改成 %v", c.state.PlaybackState)
	}
	res := c.HandleEvent(audio.Event{Type: audio.EventFailed, Err: errors.New("旧流失败")})
	if res.Retry || res.Err != nil {
		t.Fatalf("缓冲态收到 Failed 应忽略，实际 %+v", res)
	}
	if c.state.Retry != 0 {
		t.Fatalf("不应提前消耗重试次数，实际 %d", c.state.Retry)
	}
	if got := c.EngineState(); got != audio.StateIdle {
		t.Fatalf("忽略事件时不应停引擎，实际 %v", got)
	}

	// 解析完成后（非缓冲态）事件照常处理。
	c.state.Awaiting = false
	if res := c.HandleEvent(audio.Event{Type: audio.EventEnded}); !res.Ended {
		t.Fatalf("非缓冲态的 Ended 应照常处理，实际 %+v", res)
	}
}
