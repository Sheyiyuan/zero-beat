package sync

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Trigger 说明一次同步请求的来源。
type Trigger int

const (
	TriggerStartup Trigger = iota
	TriggerChange
	TriggerManual
	TriggerRetry
)

// EventKind 是同步事件类型。
type EventKind int

const (
	EventStatus EventKind = iota
	EventStarted
	EventApplied
	EventConflict
	EventKeyIssue
	EventError
)

// Event 由 Manager 发往 TUI；Status 总是最新快照。
type Event struct {
	Kind   EventKind
	At     time.Time
	Status Status
	Result Result
	Err    error
}

// ManagerOptions 控制自动同步节奏；零值使用文档默认值。
type ManagerOptions struct {
	Debounce time.Duration
	Backoff  []time.Duration
	Now      func() time.Time
}

func (o ManagerOptions) debounce() time.Duration {
	if o.Debounce > 0 {
		return o.Debounce
	}
	return 30 * time.Second
}

func (o ManagerOptions) backoff() []time.Duration {
	if len(o.Backoff) > 0 {
		return o.Backoff
	}
	return []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
}

// Manager 负责单飞、debounce、退避与事件分发；实际同步交给 Client。
type Manager struct {
	client    *Client
	opts      ManagerOptions
	events    chan Event
	requests  chan Trigger
	done      chan struct{}
	started   bool
	closeOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewManager 构造自动同步管理器（尚未启动）。
func NewManager(client *Client, opts ManagerOptions) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		client:   client,
		opts:     opts,
		events:   make(chan Event, 32),
		requests: make(chan Trigger, 16),
		done:     make(chan struct{}),
		ctx:      ctx,
		cancel:   cancel,
	}
	return m
}

// Events 返回事件通道；TUI 每处理一个事件后要重新挂起等待。
func (m *Manager) Events() <-chan Event { return m.events }

// Client 暴露底层客户端，供同步页执行初始化、改口令、解决冲突等长操作。
func (m *Manager) Client() *Client { return m.client }

// Status 返回最新状态快照。
func (m *Manager) Status() Status { return m.client.Status() }

// Start 启动后台循环；重复调用无副作用。
func (m *Manager) Start() {
	if m.started {
		return
	}
	m.started = true
	go m.loop()
	if status := m.client.Status(); status.Configured && status.Auto {
		m.Request(TriggerStartup)
	}
}

// Close 停止后台循环；已排队的同步会被取消。
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		m.cancel()
		close(m.done)
	})
}

// Request 投递一次同步请求；忙时合并。
func (m *Manager) Request(trigger Trigger) {
	select {
	case m.requests <- trigger:
	default:
		// 队列已满说明已有待处理请求，再丢一次也不会丢失「需要同步」的语义。
	}
}

// NotifyChange 在歌单/键位/登录态变更后调用：debounce 30 秒后推送。
func (m *Manager) NotifyChange() { m.Request(TriggerChange) }

// SyncNow 立即同步（用户点「立即同步」或启动拉取）。
func (m *Manager) SyncNow() { m.Request(TriggerManual) }

// Retry 手动重试；与 SyncNow 等价，保留给状态机语义。
func (m *Manager) Retry() { m.Request(TriggerManual) }

func (m *Manager) loop() {
	debounce := m.opts.debounce()
	backoff := m.opts.backoff()
	var (
		timer      *time.Timer
		timerC     <-chan time.Time
		backoffIdx int
		pending    bool
		frozen     bool // 冲突未解决时冻结自动同步
	)
	stopTimer := func() {
		if timer != nil {
			timer.Stop()
			timer = nil
			timerC = nil
		}
	}
	schedule := func(d time.Duration) {
		stopTimer()
		timer = time.NewTimer(d)
		timerC = timer.C
	}
	defer stopTimer()

	m.emit(Event{Kind: EventStatus, Status: m.client.Status()})

	for {
		select {
		case <-m.done:
			return
		case trigger := <-m.requests:
			if trigger == TriggerChange {
				if frozen {
					if m.client.Status().ConflictTx != "" {
						continue
					}
					frozen = false
				}
				pending = true
				schedule(debounce)
				continue
			}
			// 手动重试允许在冲突冻结后重新检查一次状态。
			frozen = false
			pending = false
			stopTimer()
			backoffIdx, frozen = m.runOnce(backoffIdx, backoff)
		case <-timerC:
			timer = nil
			timerC = nil
			if pending {
				pending = false
				backoffIdx, frozen = m.runOnce(backoffIdx, backoff)
			}
		}
	}
}

// runOnce 执行一轮同步并发出事件；返回下一次退避序号与冲突冻结状态。
func (m *Manager) runOnce(backoffIdx int, backoff []time.Duration) (int, bool) {
	m.emit(Event{Kind: EventStarted, Status: m.client.Status()})
	result, err := m.client.Sync(m.ctx)
	status := m.client.Status()
	switch {
	case err == nil:
		event := Event{Kind: EventApplied, Status: status, Result: result}
		if result.KeyIssue != nil {
			event.Kind = EventKeyIssue
		}
		m.emit(event)
		return 0, false
	case errors.Is(err, ErrConflict):
		m.emit(Event{Kind: EventConflict, Status: status, Err: err})
		return 0, true
	case errors.Is(err, ErrBusy):
		m.emit(Event{Kind: EventStatus, Status: status})
		return backoffIdx, false
	case errors.Is(err, ErrRevision):
		// 用户刚改过库：稍后重试，不进入长退避。
		m.emit(Event{Kind: EventStatus, Status: status})
		go func() {
			select {
			case <-time.After(time.Second):
				m.Request(TriggerRetry)
			case <-m.done:
			}
		}()
		return backoffIdx, false
	default:
		m.emit(Event{Kind: EventError, Status: status, Err: err})
		if !status.Auto {
			return backoffIdx, false
		}
		if backoffIdx >= len(backoff) {
			return backoffIdx, false
		}
		delay := backoff[backoffIdx]
		next := backoffIdx + 1
		go func() {
			select {
			case <-time.After(delay):
				m.Request(TriggerRetry)
			case <-m.done:
			}
		}()
		return next, false
	}
}

func (m *Manager) emit(event Event) {
	if event.At.IsZero() {
		event.At = time.Now()
	}
	select {
	case m.events <- event:
	case <-m.done:
	}
}
