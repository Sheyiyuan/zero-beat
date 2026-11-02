// Package control 提供运行中 TUI 的本地控制通道：读取当前帧与状态、注入按键。
//
// 通道只在数据目录里开一个 Unix socket；实例锁保证同一数据目录只有一个 TUI，
// 所以控制端不需要在多个候选实例里做选择。
package control

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	// ErrUnsupported 表示当前平台没有控制通道实现。
	ErrUnsupported = errors.New("control: 当前平台不支持控制通道")
	// ErrNotRunning 表示没有可连接的控制端点。
	ErrNotRunning = errors.New("control: 没有正在运行的 zero-beat 实例")
)

// Kind 是控制请求类型。
type Kind string

const (
	// KindFrame 只读取当前帧与状态。
	KindFrame Kind = "frame"
	// KindKey 注入一个按键，命名与 keymap.toml 一致。
	KindKey Kind = "key"
	// KindType 把一段文本逐字符送进当前输入态。
	KindType Kind = "type"
)

// Request 由控制端投递给 TUI 事件循环；Reply 必须带缓冲，避免服务端阻塞。
type Request struct {
	Kind  Kind
	Key   string
	Text  string
	Force bool
	Reply chan Reply
}

// Reply 是 TUI 对控制请求的回应。
type Reply struct {
	Frame string
	State json.RawMessage
	Err   string
}

// Response 是线上的响应体。
type Response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Frame string          `json:"frame,omitempty"`
	State json.RawMessage `json:"state,omitempty"`
}

type wireRequest struct {
	Kind  string `json:"kind"`
	Key   string `json:"key,omitempty"`
	Text  string `json:"text,omitempty"`
	Force bool   `json:"force,omitempty"`
}

func (r Request) wire() wireRequest {
	return wireRequest{Kind: string(r.Kind), Key: r.Key, Text: r.Text, Force: r.Force}
}

func validKind(k Kind) bool {
	switch k {
	case KindFrame, KindKey, KindType:
		return true
	}
	return false
}

// replyTimeout 是等待 TUI 事件循环回应的上限。
const replyTimeout = 3 * time.Second

// DefaultTimeout 是控制端调用端点的默认超时。
//
// 必须大于 replyTimeout：TUI 忙时服务端会等到 replyTimeout 才回一条「未响应」，
// 客户端若在同一时刻超时，用户只会看到底层的 i/o timeout，看不到这条提示。
const DefaultTimeout = replyTimeout + 2*time.Second
