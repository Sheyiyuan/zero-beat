//go:build !unix

package control

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Server 在非 Unix 平台没有实现；保留空壳让调用方无需分支。
type Server struct{}

// Listen 在非 Unix 平台返回 ErrUnsupported。
func Listen(dir string, send func(tea.Msg)) (*Server, error) {
	return nil, ErrUnsupported
}

// Serve 是空实现。
func (s *Server) Serve() {}

// Close 是空实现。
func (s *Server) Close() {}

// Call 在非 Unix 平台返回 ErrUnsupported。
func Call(dir string, req Request, timeout time.Duration) (Reply, error) {
	return Reply{}, ErrUnsupported
}
