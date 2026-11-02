//go:build unix

package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
)

const socketName = "control.sock"

// SocketPath 返回数据目录下的控制端点路径。
func SocketPath(dir string) string {
	return filepath.Join(dir, socketName)
}

// Server 是 TUI 进程内的控制端点。
type Server struct {
	ln   net.Listener
	path string
	send func(tea.Msg)
	done chan struct{}
	once sync.Once
}

// Listen 在 dir 下监听控制端点；send 通常是 tea.Program.Send。
func Listen(dir string, send func(tea.Msg)) (*Server, error) {
	path := SocketPath(dir)
	// 崩溃或强杀会留下 socket 文件，监听前先清掉。
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("control: 监听控制端点: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("control: 设置控制端点权限: %w", err)
	}
	return &Server{ln: ln, path: path, send: send, done: make(chan struct{})}, nil
}

// Serve 接受控制连接；每个连接处理一个请求后断开。
func (s *Server) Serve() {
	if s == nil {
		return
	}
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	var wr wireRequest
	if err := json.NewDecoder(conn).Decode(&wr); err != nil {
		writeResponse(conn, Reply{Err: "请求格式错误: " + err.Error()})
		return
	}
	kind := Kind(wr.Kind)
	if !validKind(kind) {
		writeResponse(conn, Reply{Err: fmt.Sprintf("未知控制请求 %q", wr.Kind)})
		return
	}

	req := Request{
		Kind:  kind,
		Key:   wr.Key,
		Text:  wr.Text,
		Force: wr.Force,
		Reply: make(chan Reply, 1),
	}
	select {
	case <-s.done:
		writeResponse(conn, Reply{Err: "实例已退出"})
		return
	default:
	}

	// Program.Send 在 Run 开始前会阻塞，所以放到 goroutine 里。
	go s.send(req)

	var reply Reply
	select {
	case reply = <-req.Reply:
	case <-time.After(replyTimeout):
		reply.Err = "TUI 未响应（可能正在处理其它操作）"
	case <-s.done:
		reply.Err = "实例已退出"
	}
	writeResponse(conn, reply)
}

func writeResponse(conn net.Conn, reply Reply) {
	resp := Response{
		OK:    reply.Err == "",
		Error: reply.Err,
		Frame: reply.Frame,
		State: reply.State,
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

// Close 停止监听并删除端点文件。
func (s *Server) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.done)
		_ = s.ln.Close()
		_ = os.Remove(s.path)
	})
}

// Call 向运行中的实例发一个请求并等待回应。
func Call(dir string, req Request, timeout time.Duration) (Reply, error) {
	conn, err := net.DialTimeout("unix", SocketPath(dir), timeout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) ||
			errors.Is(err, syscall.ECONNREFUSED) {
			return Reply{}, ErrNotRunning
		}
		return Reply{}, fmt.Errorf("control: 连接实例: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	if err := json.NewEncoder(conn).Encode(req.wire()); err != nil {
		return Reply{}, fmt.Errorf("control: 发送请求: %w", err)
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Reply{}, fmt.Errorf("control: 读取回应: %w", err)
	}
	reply := Reply{Frame: resp.Frame, State: resp.State, Err: resp.Error}
	if !resp.OK && reply.Err == "" {
		reply.Err = "实例拒绝了请求"
	}
	return reply, nil
}
