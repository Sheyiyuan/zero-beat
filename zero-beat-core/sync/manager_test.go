package sync

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func waitSyncEvent(t *testing.T, m *Manager, want EventKind, timeout time.Duration) Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-m.Events():
			if ev.Kind == want {
				return ev
			}
		case <-deadline:
			t.Fatalf("等待事件 %v 超时", want)
			return Event{}
		}
	}
}

func TestManagerDebouncesChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	remote := setupBareRemote(t)
	dev := newTestDevice(t, "A")
	if _, err := dev.client.Init(context.Background(), remote, "main", true, "manager test passphrase"); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(dev.client, ManagerOptions{
		Debounce: 120 * time.Millisecond,
		Backoff:  []time.Duration{10 * time.Millisecond},
	})
	mgr.Start()
	defer mgr.Close()

	// 启动时自动拉取一轮。
	waitSyncEvent(t, mgr, EventApplied, 10*time.Second)

	mgr.NotifyChange()
	time.Sleep(20 * time.Millisecond)
	mgr.NotifyChange()
	waitSyncEvent(t, mgr, EventApplied, 10*time.Second)

	// 两次变更应合并成一轮：等待超过 debounce 后不应再冒出第二轮的 Started。
	select {
	case ev := <-mgr.Events():
		if ev.Kind == EventStarted {
			t.Fatalf("debounce 应合并请求，不应再触发第二轮: %+v", ev)
		}
	case <-time.After(250 * time.Millisecond):
	}
}

func TestManagerManualSync(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	remote := setupBareRemote(t)
	dev := newTestDevice(t, "A")
	if _, err := dev.client.Init(context.Background(), remote, "main", true, "manager manual passphrase"); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(dev.client, ManagerOptions{})
	mgr.Start()
	defer mgr.Close()

	waitSyncEvent(t, mgr, EventApplied, 10*time.Second)
	mgr.SyncNow()
	ev := waitSyncEvent(t, mgr, EventApplied, 10*time.Second)
	if ev.Err != nil {
		t.Fatalf("手动同步失败: %v", ev.Err)
	}
}
