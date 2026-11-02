//go:build unix

package control

import (
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestCallRoundTrip(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	srv, err := Listen(dir, func(msg tea.Msg) {
		req, ok := msg.(Request)
		if !ok {
			return
		}
		calls.Add(1)
		switch req.Kind {
		case KindKey:
			if req.Key == "" {
				req.Reply <- Reply{Err: "缺少按键"}
				return
			}
		}
		req.Reply <- Reply{Frame: "frame:" + req.Key, State: json.RawMessage(`{"view":"曲库"}`)}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go srv.Serve()

	reply, err := Call(dir, Request{Kind: KindFrame}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Err != "" || reply.Frame != "frame:" {
		t.Fatalf("帧回应不符: %+v", reply)
	}
	if got := string(reply.State); got != `{"view":"曲库"}` {
		t.Fatalf("状态透传不符: %s", got)
	}

	keyReply, err := Call(dir, Request{Kind: KindKey, Key: "down"}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if keyReply.Frame != "frame:down" {
		t.Fatalf("按键请求应带键名: %+v", keyReply)
	}
	if calls.Load() != 2 {
		t.Fatalf("应处理 2 个请求，实际 %d", calls.Load())
	}
}

func TestCallRejectsUnknownKind(t *testing.T) {
	dir := t.TempDir()
	srv, err := Listen(dir, func(tea.Msg) {})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go srv.Serve()

	reply, err := Call(dir, Request{Kind: Kind("bogus")}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Err == "" {
		t.Fatal("未知请求类型应被拒绝")
	}
}

func TestCallWithoutInstance(t *testing.T) {
	dir := t.TempDir()
	if _, err := Call(dir, Request{Kind: KindFrame}, time.Second); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("没有实例时应返回 ErrNotRunning，实际 %v", err)
	}
}

func TestDefaultTimeoutExceedsReplyTimeout(t *testing.T) {
	if DefaultTimeout <= replyTimeout {
		t.Fatalf("客户端超时 %v 必须大于服务端回应上限 %v", DefaultTimeout, replyTimeout)
	}
}

func TestListenCleansStaleSocketAndPerms(t *testing.T) {
	dir := t.TempDir()
	path := SocketPath(dir)
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, err := Listen(dir, func(tea.Msg) {})
	if err != nil {
		t.Fatalf("残留 socket 文件应被清理: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		srv.Close()
		t.Fatalf("socket 权限应为 0600，实际 %o", perm)
	}
	srv.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Close 应删除 socket: %v", err)
	}
}
