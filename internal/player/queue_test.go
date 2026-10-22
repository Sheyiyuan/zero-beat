package player

import (
	"testing"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

func songs(ids ...string) []store.Song {
	out := make([]store.Song, 0, len(ids))
	for _, id := range ids {
		out = append(out, store.Song{ID: id, Source: "fake", RemoteID: id, Page: 1, Title: id})
	}
	return out
}

func ids(queue []store.Song) []string {
	out := make([]string, len(queue))
	for i, s := range queue {
		out[i] = s.ID
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestInsertNextMovesExistingAndKeepsCurrent(t *testing.T) {
	queue := songs("a", "b", "c")
	out, idx, cursor := InsertNext(queue, 0, 0, true, songs("c")[0])
	if !equal(ids(out), []string{"a", "c", "b"}) || idx != 0 || cursor != 1 {
		t.Fatalf("插入已存在条目结果不符: %v idx=%d cursor=%d", ids(out), idx, cursor)
	}

	out, idx, cursor = InsertNext(songs("a", "b", "c"), 1, 2, true, songs("x")[0])
	if !equal(ids(out), []string{"a", "b", "x", "c"}) || idx != 1 || cursor != 2 {
		t.Fatalf("插入新条目结果不符: %v idx=%d cursor=%d", ids(out), idx, cursor)
	}
}

func TestMoveFollowsCurrent(t *testing.T) {
	out, idx, cursor := Move(songs("a", "b", "c"), 1, 1, 1)
	if !equal(ids(out), []string{"a", "c", "b"}) || idx != 2 || cursor != 2 {
		t.Fatalf("移动当前条目结果不符: %v idx=%d cursor=%d", ids(out), idx, cursor)
	}
	out, idx, cursor = Move(songs("a", "b", "c"), 1, 2, -1)
	if !equal(ids(out), []string{"a", "c", "b"}) || idx != 2 || cursor != 1 {
		t.Fatalf("移动其它条目结果不符: %v idx=%d cursor=%d", ids(out), idx, cursor)
	}
}

func TestMoveToNext(t *testing.T) {
	out, idx, cursor := MoveToNext(songs("a", "b", "c"), 0, 2, true)
	if !equal(ids(out), []string{"a", "c", "b"}) || idx != 0 || cursor != 1 {
		t.Fatalf("移动到下一首结果不符: %v idx=%d cursor=%d", ids(out), idx, cursor)
	}
}

func TestRemoveCurrentAndOther(t *testing.T) {
	out, idx, cursor, removed, current := Remove(songs("a", "b", "c"), 1, 1)
	if !current || removed.ID != "b" || !equal(ids(out), []string{"a", "c"}) || idx != 1 || cursor != 1 {
		t.Fatalf("删除当前结果不符: %v idx=%d cursor=%d removed=%s current=%v", ids(out), idx, cursor, removed.ID, current)
	}

	out, idx, cursor, removed, current = Remove(songs("a", "b", "c"), 2, 0)
	if current || removed.ID != "a" || !equal(ids(out), []string{"b", "c"}) || idx != 1 || cursor != 0 {
		t.Fatalf("删除其它条目结果不符: %v idx=%d cursor=%d removed=%s current=%v", ids(out), idx, cursor, removed.ID, current)
	}

	out, idx, cursor, _, current = Remove(songs("a"), 0, 0)
	if !current || len(out) != 0 || idx != 0 || cursor != 0 {
		t.Fatalf("删除最后一条结果不符: %v idx=%d cursor=%d", ids(out), idx, cursor)
	}
}

func TestClearKeepsCurrent(t *testing.T) {
	current := songs("a")[0]
	out, idx, cursor := Clear(&current)
	if !equal(ids(out), []string{"a"}) || idx != 0 || cursor != 0 {
		t.Fatalf("清空队列结果不符: %v idx=%d cursor=%d", ids(out), idx, cursor)
	}
	out, _, _ = Clear(nil)
	if len(out) != 0 {
		t.Fatalf("没有当前曲目时应清空: %v", ids(out))
	}
}

func reverse(queue []store.Song) {
	for i, j := 0, len(queue)-1; i < j; i, j = i+1, j-1 {
		queue[i], queue[j] = queue[j], queue[i]
	}
}

func TestShuffleKeepsCurrent(t *testing.T) {
	queue := songs("a", "b", "c")
	out, idx, cursor := Shuffle(queue, "b", 1, reverse)
	if !equal(ids(out), []string{"c", "b", "a"}) || idx != 1 || cursor != 1 {
		t.Fatalf("洗牌后当前曲目应保持: %v idx=%d cursor=%d", ids(out), idx, cursor)
	}
}

func TestRestoreKeepsNewSongs(t *testing.T) {
	base := songs("a", "b", "c")
	shuffled := songs("c", "a", "x", "b")
	out, idx, cursor := Restore(shuffled, base, "a", 0)
	if !equal(ids(out), []string{"a", "b", "c", "x"}) || idx != 0 || cursor != 0 {
		t.Fatalf("恢复顺序或新增曲目不符: %v idx=%d cursor=%d", ids(out), idx, cursor)
	}
}

func TestNextPrevIndex(t *testing.T) {
	if got := NextIndex(2, 3); got != 0 {
		t.Fatalf("NextIndex 末尾应回到 0: %d", got)
	}
	if got := PrevIndex(0, 3); got != 2 {
		t.Fatalf("PrevIndex 开头应回到末尾: %d", got)
	}
	if got := NextIndex(0, 0); got != 0 {
		t.Fatalf("空队列索引应为 0: %d", got)
	}
}
