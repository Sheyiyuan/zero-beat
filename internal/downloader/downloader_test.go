package downloader

import (
	"context"
	"testing"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

func song(id string) store.Song {
	return store.Song{ID: "fake:" + id + ":1", Source: "fake", RemoteID: id, Page: 1, Title: id}
}

func TestManagerAddsDedupesAndRemoves(t *testing.T) {
	mgr := New(2, func(context.Context, store.Song, Job, func(int64, int64)) (string, int64, error) {
		return "", 0, nil
	})
	id, ok := mgr.Add(song("a"), "A", Job{})
	if !ok || id == 0 {
		t.Fatalf("首次加入失败: id=%d ok=%v", id, ok)
	}
	if _, ok := mgr.Add(song("a"), "A", Job{}); ok {
		t.Fatal("重复歌曲不应再次加入")
	}
	if !mgr.Has("fake:a:1") || mgr.Len() != 1 {
		t.Fatalf("任务状态不符: len=%d", mgr.Len())
	}
	mgr.Remove(id)
	if mgr.Len() != 0 || mgr.Has("fake:a:1") {
		t.Fatal("移除后任务应消失")
	}
}

func TestManagerRunsAndEmitsFinished(t *testing.T) {
	mgr := New(1, func(ctx context.Context, s store.Song, _ Job, progress func(int64, int64)) (string, int64, error) {
		progress(5, 5)
		return "/tmp/" + s.Title, 5, nil
	})
	id, _ := mgr.Add(song("a"), "A", Job{})
	mgr.Start()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-mgr.Events():
			if !ev.Finished {
				continue
			}
			if ev.ID != id || ev.Path != "/tmp/a" || ev.Size != 5 || ev.Err != nil {
				t.Fatalf("完成事件不符: %+v", ev)
			}
			goto done
		case <-deadline:
			t.Fatal("等待完成事件超时")
		}
	}
done:
	mgr.Remove(id)
	if mgr.Len() != 0 {
		t.Fatalf("完成后应可移除: %d", mgr.Len())
	}
}

func TestManagerConcurrencyLimitAndCancel(t *testing.T) {
	started := make(chan struct{}, 3)
	block := make(chan struct{})
	mgr := New(2, func(ctx context.Context, _ store.Song, _ Job, _ func(int64, int64)) (string, int64, error) {
		started <- struct{}{}
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-block:
			return "p", 1, nil
		}
	})
	for _, id := range []string{"a", "b", "c"} {
		mgr.Add(song(id), id, Job{})
	}
	mgr.Start()
	<-started
	<-started
	select {
	case <-started:
		t.Fatal("并发数应限制为 2")
	case <-time.After(100 * time.Millisecond):
	}
	running, queued := 0, 0
	for _, task := range mgr.Tasks() {
		if task.State == Running {
			running++
		} else {
			queued++
		}
	}
	if running != 2 || queued != 1 {
		t.Fatalf("并发状态不符: running=%d queued=%d", running, queued)
	}
	mgr.CancelAll()
	close(block)
}

// TestManagerKeepsJobSnapshot 验证任务在入队时快照下载参数（音质/强制重下），
// 后台 goroutine 只读快照，不依赖界面状态。
func TestManagerKeepsJobSnapshot(t *testing.T) {
	mgr := New(1, func(context.Context, store.Song, Job, func(int64, int64)) (string, int64, error) {
		return "", 0, nil
	})
	_, ok := mgr.Add(song("a"), "A", Job{Quality: 2, Force: true})
	if !ok {
		t.Fatal("入队失败")
	}
	task := mgr.Tasks()[0]
	if task.Job.Quality != 2 || !task.Job.Force {
		t.Fatalf("任务应保留下载参数快照，实际 %+v", task.Job)
	}
	mgr.CancelAll()
}
