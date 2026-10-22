// Package downloader 管理下载任务队列、并发度与进度事件，不依赖 Bubble Tea。
package downloader

import (
	"context"
	"sync"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

// State 是任务状态。
type State int

const (
	Queued State = iota
	Running
)

// Job 是一次下载的参数，在入队（界面协程）时快照，
// 后台 goroutine 只读它，不再回读界面状态。
type Job struct {
	Quality int  // 音质档位；downloader 不依赖 source 包，按整数透传
	Force   bool // 忽略已有文件（补全 0 字节/损坏文件）
}

// Task 是任务快照。
type Task struct {
	ID    int
	Song  store.Song
	Title string
	Job   Job
	State State
	Done  int64
	Total int64
	Path  string
	Err   error
}

// Event 是任务进度/完成事件。
type Event struct {
	ID       int
	Done     int64
	Total    int64
	Path     string
	Size     int64
	Err      error
	Finished bool
}

// Runner 执行一次下载；由 app.Service 提供。
type Runner func(ctx context.Context, song store.Song, job Job, progress func(done, total int64)) (path string, size int64, err error)

type task struct {
	Task
	cancel context.CancelFunc
}

// Manager 负责下载队列与并发调度。
type Manager struct {
	mu      sync.Mutex
	max     int
	run     Runner
	tasks   []*task
	bySong  map[string]int
	seq     int
	running int
	events  chan Event
}

// New 构造下载管理器。
func New(max int, run Runner) *Manager {
	if max < 1 {
		max = 1
	}
	return &Manager{max: max, run: run, bySong: map[string]int{}, events: make(chan Event, 256)}
}

// Events 返回事件流。
func (m *Manager) Events() <-chan Event { return m.events }

// Add 加入一首歌；同一首歌重复加入时返回已有 ID 和 false。
func (m *Manager) Add(song store.Song, title string, job Job) (int, bool) {
	song = store.NormalizeSong(song)
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.bySong[song.ID]; ok {
		return id, false
	}
	m.seq++
	t := &task{Task: Task{ID: m.seq, Song: song, Title: title, Job: job, State: Queued}}
	m.tasks = append(m.tasks, t)
	m.bySong[song.ID] = t.ID
	return t.ID, true
}

// Has 判断歌曲是否已在任务列表。
func (m *Manager) Has(songID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.bySong[songID]
	return ok
}

// Tasks 返回任务快照。
func (m *Manager) Tasks() []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, t.Task)
	}
	return out
}

// Start 启动排队任务直到并发上限。
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startLocked()
}

func (m *Manager) startLocked() {
	for m.running < m.max {
		var next *task
		for _, t := range m.tasks {
			if t.State == Queued {
				next = t
				break
			}
		}
		if next == nil {
			return
		}
		next.State = Running
		m.running++
		ctx, cancel := context.WithCancel(context.Background())
		next.cancel = cancel
		go m.runTask(ctx, next)
	}
}

func (m *Manager) runTask(ctx context.Context, t *task) {
	last := time.Time{}
	progress := func(done, total int64) {
		now := time.Now()
		if !last.IsZero() && now.Sub(last) < 200*time.Millisecond {
			return
		}
		last = now
		m.mu.Lock()
		t.Done, t.Total = done, total
		m.mu.Unlock()
		m.emit(Event{ID: t.ID, Done: done, Total: total}, false)
	}
	path, size, err := m.run(ctx, t.Song, t.Job, progress)
	m.mu.Lock()
	t.Path = path
	t.Err = err
	m.running--
	delete(m.bySong, t.Song.ID)
	m.mu.Unlock()
	m.emit(Event{ID: t.ID, Path: path, Size: size, Err: err, Finished: true}, true)
	m.mu.Lock()
	m.startLocked()
	m.mu.Unlock()
}

func (m *Manager) emit(ev Event, must bool) {
	if must {
		m.events <- ev
		return
	}
	select {
	case m.events <- ev:
	default:
	}
}

// Remove 移除一个任务（完成事件处理完后由界面调用）。
func (m *Manager) Remove(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.tasks {
		if t.ID != id {
			continue
		}
		if t.cancel != nil {
			t.cancel()
		}
		delete(m.bySong, t.Song.ID)
		m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
		return
	}
}

// CancelAll 取消所有排队/运行中的任务。
func (m *Manager) CancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t.cancel != nil {
			t.cancel()
		}
	}
}

// Len 返回任务数。
func (m *Manager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tasks)
}

// SetProgress 更新任务进度；主要用于界面恢复或测试注入。
func (m *Manager) SetProgress(id int, done, total int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t.ID == id {
			t.Done, t.Total = done, total
			return
		}
	}
}
