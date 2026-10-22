package tui

import (
	"testing"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

// TestPlaylistPlayInShuffleStartsRandom 验证随机模式下播放歌单会先整体洗牌，
// 起播的第一首不是固定的歌单第一首。
func TestPlaylistPlayInShuffleStartsRandom(t *testing.T) {
	m := fixtureModel()
	m.mode = modeShuffle
	m.shuffleFn = func(queue []store.Song) {
		for i, j := 0, len(queue)-1; i < j; i, j = i+1, j-1 {
			queue[i], queue[j] = queue[j], queue[i]
		}
	}
	m.playlistIdx = 0
	m.songIdx = 0

	if cmd := m.playPlaylistFromStart(); cmd == nil {
		t.Fatal("播放歌单应返回命令")
	}
	want := m.songs[len(m.songs)-1].ID
	if m.current == nil || m.current.ID != want {
		t.Fatalf("随机起播第一首应为洗牌后的首项 %s，实际 %+v", want, m.current)
	}
	if m.queueIdx != 0 || len(m.queue) != len(m.songs) {
		t.Fatalf("队列应为整单洗牌：idx=%d len=%d", m.queueIdx, len(m.queue))
	}
	if got := m.queue[0].ID; got != want {
		t.Fatalf("队列首项应为洗牌后的曲目 %s，实际 %s", want, got)
	}
	if m.playingPlaylistID != m.playlists[0].ID {
		t.Fatalf("应记录正在播放的来源歌单，实际 %q", m.playingPlaylistID)
	}
}

// TestExplicitSongInShuffleKeepsSelected 随机模式下显式点歌仍播选中的那首。
func TestExplicitSongInShuffleKeepsSelected(t *testing.T) {
	m := fixtureModel()
	m.mode = modeShuffle
	m.shuffleFn = func(queue []store.Song) {
		for i, j := 0, len(queue)-1; i < j; i, j = i+1, j-1 {
			queue[i], queue[j] = queue[j], queue[i]
		}
	}
	m.playlistIdx = 0
	m.songIdx = 0
	want := m.songs[0].ID

	_ = m.playPlaylistIndex(0)
	if m.current == nil || m.current.ID != want {
		t.Fatalf("显式点歌应播选中的 %s，实际 %+v", want, m.current)
	}
	if m.queue[m.queueIdx].ID != want {
		t.Fatalf("当前索引应指向选中曲目，实际 %s", m.queue[m.queueIdx].ID)
	}
}
