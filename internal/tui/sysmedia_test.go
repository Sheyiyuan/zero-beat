package tui

import (
	"testing"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/audio"
	"github.com/sheyiyuan/zero-beat/internal/store"
	"github.com/sheyiyuan/zero-beat/internal/sysmedia"
)

type fakeSysmedia struct {
	published []sysmedia.Snapshot
	commands  chan sysmedia.Command
}

func (f *fakeSysmedia) Publish(s sysmedia.Snapshot) { f.published = append(f.published, s) }
func (f *fakeSysmedia) Commands() <-chan sysmedia.Command {
	return f.commands
}
func (f *fakeSysmedia) Close() {}

func TestPublishNowPlayingSnapshot(t *testing.T) {
	fake := &fakeSysmedia{commands: make(chan sysmedia.Command)}
	m := &Model{
		sys:           fake,
		current:       &store.Song{ID: "a", Title: "标题", Artist: "歌手", Album: "专辑", Duration: 200, Cover: "https://example.com/a.jpg"},
		state:         audio.StatePlaying,
		position:      10 * time.Second,
		duration:      200 * time.Second,
		volume:        0.5,
		mode:          modeShuffle,
		artworkSongID: "a",
		artwork:       "/tmp/a.jpg",
		queue:         []store.Song{{ID: "a"}, {ID: "b"}},
	}
	m.publishNowPlaying()

	if len(fake.published) != 1 {
		t.Fatalf("publish 次数 = %d", len(fake.published))
	}
	snap := fake.published[0]
	if snap.TrackID != "a" || snap.Title != "标题" || snap.Artwork != "/tmp/a.jpg" {
		t.Fatalf("快照 = %+v", snap)
	}
	if snap.Position != 10*time.Second || snap.Duration != 200*time.Second || snap.State != audio.StatePlaying {
		t.Fatalf("位置/时长/状态 = %+v", snap)
	}
	if !snap.CanNext || !snap.CanPrev || !snap.Shuffle {
		t.Fatalf("队列/模式标记 = %+v", snap)
	}
}

// TestPublishNowPlayingOrderSnapshot 表驱动验证四种模式投给面板的顺序状态。
func TestPublishNowPlayingOrderSnapshot(t *testing.T) {
	cases := []struct {
		name    string
		mode    playMode
		idx     int
		shuffle bool
		loop    sysmedia.LoopStatus
		canNext bool
		canPrev bool
	}{
		{"列表循环", modeListLoop, 1, false, sysmedia.LoopPlaylist, true, true},
		{"单曲循环", modeLoopOne, 1, false, sysmedia.LoopTrack, true, true},
		{"随机", modeShuffle, 1, true, sysmedia.LoopPlaylist, true, true},
		{"顺序中间", modeOrdered, 1, false, sysmedia.LoopNone, true, true},
		{"顺序队首", modeOrdered, 0, false, sysmedia.LoopNone, true, false},
		{"顺序队尾", modeOrdered, 2, false, sysmedia.LoopNone, false, true},
		{"单首队列", modeListLoop, 0, false, sysmedia.LoopPlaylist, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queue := []store.Song{{ID: "a"}, {ID: "b"}, {ID: "c"}}
			if tc.name == "单首队列" {
				queue = queue[:1]
			}
			fake := &fakeSysmedia{commands: make(chan sysmedia.Command, 8)}
			m := &Model{sys: fake, mode: tc.mode, queue: queue, queueIdx: tc.idx}
			m.publishNowPlaying()
			if len(fake.published) != 1 {
				t.Fatalf("publish 次数 = %d", len(fake.published))
			}
			snap := fake.published[0]
			if snap.Shuffle != tc.shuffle || snap.Loop != tc.loop ||
				snap.CanNext != tc.canNext || snap.CanPrev != tc.canPrev {
				t.Fatalf("顺序状态 = %+v，期望 shuffle=%v loop=%v next=%v prev=%v",
					snap, tc.shuffle, tc.loop, tc.canNext, tc.canPrev)
			}
		})
	}
}

// TestApplySysCommandMapsOrderProperties 表驱动验证面板写入按目标值映射，后写覆盖前写。
func TestApplySysCommandMapsOrderProperties(t *testing.T) {
	setShuffle := func(on bool) sysmedia.Command {
		return sysmedia.Command{Kind: sysmedia.CommandSetShuffle, Shuffle: on}
	}
	setLoop := func(l sysmedia.LoopStatus) sysmedia.Command {
		return sysmedia.Command{Kind: sysmedia.CommandSetLoopStatus, Loop: l}
	}
	cases := []struct {
		name    string
		initial playMode
		cmd     sysmedia.Command
		want    playMode
	}{
		{"循环 None → 顺序", modeListLoop, setLoop(sysmedia.LoopNone), modeOrdered},
		{"循环 Playlist → 列表循环", modeOrdered, setLoop(sysmedia.LoopPlaylist), modeListLoop},
		{"循环 Track → 单曲", modeListLoop, setLoop(sysmedia.LoopTrack), modeLoopOne},
		{"随机开 → 随机", modeLoopOne, setShuffle(true), modeShuffle},
		{"随机中写循环 → 退出随机", modeShuffle, setLoop(sysmedia.LoopNone), modeOrdered},
		{"关随机（旧会话）→ 列表循环", modeShuffle, setShuffle(false), modeListLoop},
		{"关非随机 → 不动作", modeLoopOne, setShuffle(false), modeLoopOne},
		{"非法循环值 → 不动作", modeLoopOne, setLoop("Repeat"), modeLoopOne},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{mode: tc.initial, queue: []store.Song{{ID: "a"}, {ID: "b"}}}
			_ = m.applySysCommand(tc.cmd)
			if m.mode != tc.want {
				t.Fatalf("模式 = %v，期望 %v", m.mode, tc.want)
			}
		})
	}
}

// TestShuffleOffRestoresModeBeforeShuffle 验证关闭随机回到进入随机前的模式，
// 而不是固定回列表循环。
func TestShuffleOffRestoresModeBeforeShuffle(t *testing.T) {
	m := &Model{mode: modeLoopOne, queue: []store.Song{{ID: "a"}, {ID: "b"}}}
	m.setMode(modeShuffle)
	if m.modeBeforeShuffle != modeLoopOne {
		t.Fatalf("进入随机应记录原模式，实际 %v", m.modeBeforeShuffle)
	}
	_ = m.applySysCommand(sysmedia.Command{Kind: sysmedia.CommandSetShuffle, Shuffle: false})
	if m.mode != modeLoopOne {
		t.Fatalf("关闭随机应恢复到单曲循环，实际 %v", m.mode)
	}
}

// TestSysCommandPublishesOrderState 验证面板命令落到模式后，立即把新状态发布出去。
func TestSysCommandPublishesOrderState(t *testing.T) {
	fake := &fakeSysmedia{commands: make(chan sysmedia.Command, 1)}
	m := &Model{
		sys:   fake,
		mode:  modeListLoop,
		queue: []store.Song{{ID: "a"}, {ID: "b"}},
	}
	updated, _ := m.Update(sysCommandMsg{cmd: sysmedia.Command{
		Kind: sysmedia.CommandSetLoopStatus,
		Loop: sysmedia.LoopTrack,
	}})
	mm := updated.(*Model)
	if mm.mode != modeLoopOne {
		t.Fatalf("面板写 Track 后模式 = %v，期望单曲", mm.mode)
	}
	if len(fake.published) == 0 {
		t.Fatal("处理面板命令后应发布快照")
	}
	last := fake.published[len(fake.published)-1]
	if last.Loop != sysmedia.LoopTrack || last.Shuffle {
		t.Fatalf("发布快照 = %+v", last)
	}
}

func TestSyncArtworkClearsOldAndLoadsOnce(t *testing.T) {
	fake := &fakeSysmedia{commands: make(chan sysmedia.Command)}
	m := &Model{
		sys:           fake,
		current:       &store.Song{ID: "b", Cover: "https://example.com/b.jpg"},
		artwork:       "/tmp/a.jpg",
		artworkSongID: "a",
	}
	cmd := m.syncArtwork()
	if cmd == nil {
		t.Fatal("新曲目应触发封面下载")
	}
	if m.artwork != "" || m.artworkSongID != "b" || m.coverLoadingID != "b" {
		t.Fatalf("新曲目状态 = %+v", m)
	}
	if again := m.syncArtwork(); again != nil {
		t.Fatal("下载中不应重复触发")
	}

	_, _ = m.Update(coverMsg{songID: "b", path: "/tmp/b.jpg"})
	if m.artwork != "/tmp/b.jpg" || m.coverLoadingID != "" {
		t.Fatalf("封面完成状态 = %+v", m)
	}
	if next := m.syncArtwork(); next != nil {
		t.Fatal("已有封面不应重复下载")
	}
}
