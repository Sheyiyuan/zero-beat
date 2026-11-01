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
