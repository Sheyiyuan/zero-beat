// Package player 收纳与界面无关的播放队列算法。
//
// 队列状态本身仍由 TUI 持有（当前曲目、游标、随机基准），这里只处理
// 「插入/移动/移除后索引怎么变」这类容易写错的纯逻辑，便于单测。
package player

import (
	"math/rand"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

// InsertNext 把 song 插到当前曲目之后；已存在时先移走旧位置，避免重复。
// hasCurrent 为 false 时插到队首。
func InsertNext(queue []store.Song, queueIdx, cursor int, hasCurrent bool, song store.Song) ([]store.Song, int, int) {
	insertAt := 0
	if hasCurrent {
		insertAt = queueIdx + 1
	}
	if insertAt > len(queue) {
		insertAt = len(queue)
	}
	for i := 0; i < len(queue); i++ {
		if queue[i].ID != song.ID {
			continue
		}
		if i < insertAt {
			insertAt--
		}
		if i < queueIdx {
			queueIdx--
		}
		queue = append(queue[:i], queue[i+1:]...)
		break
	}
	if insertAt < 0 {
		insertAt = 0
	}
	if insertAt > len(queue) {
		insertAt = len(queue)
	}
	queue = append(queue, store.Song{})
	copy(queue[insertAt+1:], queue[insertAt:])
	queue[insertAt] = song
	cursor = insertAt
	if queueIdx < 0 {
		queueIdx = 0
	}
	if queueIdx >= len(queue) {
		queueIdx = len(queue) - 1
	}
	return queue, queueIdx, cursor
}

// Move 交换游标与相邻条目；当前播放索引跟随交换。
func Move(queue []store.Song, queueIdx, cursor, delta int) ([]store.Song, int, int) {
	i := cursor
	j := cursor + delta
	if i < 0 || i >= len(queue) || j < 0 || j >= len(queue) {
		return queue, queueIdx, cursor
	}
	queue[i], queue[j] = queue[j], queue[i]
	if queueIdx == i {
		queueIdx = j
	} else if queueIdx == j {
		queueIdx = i
	}
	return queue, queueIdx, j
}

// MoveToNext 把游标条目移动到当前曲目之后。
func MoveToNext(queue []store.Song, queueIdx, cursor int, hasCurrent bool) ([]store.Song, int, int) {
	if cursor < 0 || cursor >= len(queue) {
		return queue, queueIdx, cursor
	}
	item := queue[cursor]
	queue = append(queue[:cursor], queue[cursor+1:]...)

	insertAt := 0
	if hasCurrent {
		if cursor < queueIdx {
			queueIdx--
		}
		insertAt = queueIdx + 1
	}
	if insertAt > len(queue) {
		insertAt = len(queue)
	}
	queue = append(queue, store.Song{})
	copy(queue[insertAt+1:], queue[insertAt:])
	queue[insertAt] = item
	cursor = insertAt
	if queueIdx >= len(queue) {
		queueIdx = len(queue) - 1
		if queueIdx < 0 {
			queueIdx = 0
		}
	}
	return queue, queueIdx, cursor
}

// Remove 删除游标条目。removingCurrent 表示删掉了正在播放的曲目；
// 此时返回的新索引指向原位置的下一首（越界时回到队尾）。
func Remove(queue []store.Song, queueIdx, cursor int) (out []store.Song, idx, cur int, removed store.Song, removingCurrent bool) {
	if cursor < 0 || cursor >= len(queue) {
		return queue, queueIdx, cursor, store.Song{}, false
	}
	removed = queue[cursor]
	removingCurrent = cursor == queueIdx
	out = append(queue[:cursor], queue[cursor+1:]...)
	if removingCurrent {
		if len(out) == 0 {
			return out, 0, 0, removed, true
		}
		idx = cursor
		if idx >= len(out) {
			idx = len(out) - 1
		}
		return out, idx, idx, removed, true
	}
	idx = queueIdx
	if cursor < idx {
		idx--
	}
	if idx >= len(out) {
		idx = len(out) - 1
	}
	if idx < 0 {
		idx = 0
	}
	cur = cursor
	if cur >= len(out) {
		cur = len(out) - 1
	}
	if cur < 0 {
		cur = 0
	}
	return out, idx, cur, removed, false
}

// Clear 清空待播队列；有当前曲目时保留它作为唯一条目。
func Clear(current *store.Song) ([]store.Song, int, int) {
	if current == nil {
		return nil, 0, 0
	}
	return []store.Song{*current}, 0, 0
}

// Index 返回歌曲在队列中的位置，找不到返回 -1。
func Index(queue []store.Song, id string) int {
	if id == "" {
		return -1
	}
	for i := range queue {
		if queue[i].ID == id {
			return i
		}
	}
	return -1
}

// Shuffle 就地洗牌，并让当前歌曲保持在新的索引上。
// shuffleFn 仅用于测试注入确定性洗牌；为 nil 时使用 math/rand。
func Shuffle(queue []store.Song, currentID string, index int, shuffleFn func([]store.Song)) ([]store.Song, int, int) {
	if len(queue) < 2 {
		if index < 0 {
			index = 0
		}
		if index >= len(queue) {
			index = len(queue) - 1
		}
		if index < 0 {
			index = 0
		}
		return queue, index, index
	}
	if shuffleFn != nil {
		shuffleFn(queue)
	} else {
		rand.Shuffle(len(queue), func(i, j int) {
			queue[i], queue[j] = queue[j], queue[i]
		})
	}
	if idx := Index(queue, currentID); idx >= 0 {
		index = idx
	}
	if index < 0 {
		index = 0
	}
	if index >= len(queue) {
		index = len(queue) - 1
	}
	return queue, index, index
}

// Restore 按洗牌前的顺序恢复队列；洗牌期间新增的歌追加到末尾。
func Restore(shuffled, base []store.Song, currentID string, fallbackIdx int) ([]store.Song, int, int) {
	if len(base) == 0 {
		return shuffled, fallbackIdx, fallbackIdx
	}
	counts := make(map[string]int, len(shuffled))
	for _, song := range shuffled {
		counts[store.NormalizeSong(song).ID]++
	}
	restored := make([]store.Song, 0, len(shuffled))
	appendSong := func(song store.Song) {
		song = store.NormalizeSong(song)
		if counts[song.ID] <= 0 {
			return
		}
		restored = append(restored, song)
		counts[song.ID]--
	}
	for _, song := range base {
		appendSong(song)
	}
	for _, song := range shuffled {
		appendSong(song)
	}
	idx := Index(restored, currentID)
	if idx < 0 {
		idx = fallbackIdx
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(restored) {
		idx = len(restored) - 1
	}
	if idx < 0 {
		idx = 0
	}
	return restored, idx, idx
}

// NextIndex 返回下一首索引，末尾回到开头。
func NextIndex(index, length int) int {
	if length <= 0 {
		return 0
	}
	next := index + 1
	if next >= length {
		next = 0
	}
	return next
}

// PrevIndex 返回上一首索引，开头回到末尾。
func PrevIndex(index, length int) int {
	if length <= 0 {
		return 0
	}
	prev := index - 1
	if prev < 0 {
		prev = length - 1
	}
	return prev
}
