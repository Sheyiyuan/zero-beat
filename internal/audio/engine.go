// Package audio 实现播放引擎：ffmpeg 解码 + oto 输出 + 自研增益链。
//
// 选型理由见 docs/adr/0003-audio-decoder-abstraction.md：先用外部解码器把
// 编解码矩阵（AAC-LC / HE-AAC / FLAC / EC-3）全部覆盖，把纯 Go 解码留给后续。
package audio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"

	"github.com/sheyiyuan/zero-beat/internal/audio/dsp"
)

const (
	sampleRate   = 48000
	channelCount = 2

	// bytesPerSecond 是 s16le 立体声 48kHz 的字节速率。
	bytesPerSecond = sampleRate * channelCount * 2

	eventBuffer = 16
)

// State 是播放状态。
type State int

const (
	StateIdle State = iota
	StateBuffering
	StatePlaying
	StatePaused
	StateEnded
	StateFailed
)

func (s State) String() string {
	switch s {
	case StateBuffering:
		return "缓冲中"
	case StatePlaying:
		return "播放中"
	case StatePaused:
		return "已暂停"
	case StateEnded:
		return "已结束"
	case StateFailed:
		return "出错"
	default:
		return "空闲"
	}
}

// EventType 区分事件类型。
type EventType int

const (
	// EventStateChanged 播放状态变化。
	EventStateChanged EventType = iota
	// EventEnded 当前曲目播放完毕。
	EventEnded
	// EventFailed 播放失败，Err 里有原因。
	EventFailed
)

// Event 是引擎发往 UI 的事件。
type Event struct {
	Type  EventType
	State State
	Err   error
}

// Engine 是播放引擎。
type Engine struct {
	ctx    *oto.Context
	events chan Event

	ffmpeg string

	mu       sync.Mutex
	state    State
	volume   float64
	compDB   float64
	stream   Stream
	cmd      *exec.Cmd
	proxy    *headerProxy
	player   *oto.Player
	gain     *gainReader
	seekBase time.Duration
	gen      int
	closed   bool
}

// New 创建引擎并初始化音频设备（PulseAudio / WASAPI / CoreAudio）。
func New() (*Engine, error) {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:      sampleRate,
		ChannelCount:    channelCount,
		Format:          oto.FormatSignedInt16LE,
		BufferSize:      80 * time.Millisecond,
		ApplicationName: "zero-beat",
	})
	if err != nil {
		return nil, fmt.Errorf("audio: 初始化音频设备: %w", err)
	}
	<-ready
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("audio: 音频设备不可用: %w", err)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, errors.New("audio: 未找到 ffmpeg，请先安装（Debian/Ubuntu: sudo apt install ffmpeg）")
	}
	return &Engine{
		ctx:    ctx,
		events: make(chan Event, eventBuffer),
		ffmpeg: ffmpeg,
		volume: 0.8,
		state:  StateIdle,
	}, nil
}

// Events 返回事件通道。
func (e *Engine) Events() <-chan Event { return e.events }

// State 返回当前状态。
func (e *Engine) State() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

// SetVolume 调整用户音量。
func (e *Engine) SetVolume(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	e.mu.Lock()
	e.volume = v
	gain := e.gain
	comp := e.compDB
	vol := e.volume
	e.mu.Unlock()
	if gain != nil {
		gain.setGain(vol * dbToGain(comp))
	}
}

// SetCompensationDB 调整音量补偿（分贝）。
func (e *Engine) SetCompensationDB(db float64) {
	if db < -20 {
		db = -20
	}
	if db > 12 {
		db = 12
	}
	e.mu.Lock()
	e.compDB = db
	gain := e.gain
	vol := e.volume
	e.mu.Unlock()
	if gain != nil {
		gain.setGain(vol * dbToGain(db))
	}
}

func dbToGain(db float64) float64 {
	return dsp.DBToGain(db)
}

// Play 开始播放一个流；会先停掉当前曲目。
func (e *Engine) Play(ctx context.Context, s Stream) error {
	return e.PlayAt(ctx, s, 0)
}

// PlayAt 从指定位置开始播放；用于断点恢复，避免「先起播再 seek」的双重启动。
func (e *Engine) PlayAt(ctx context.Context, s Stream, at time.Duration) error {
	if s.URL == "" {
		return errors.New("audio: 播放地址为空")
	}
	if at < 0 {
		at = 0
	}
	if duration := time.Duration(s.DurationMS) * time.Millisecond; duration > 0 && at > duration {
		at = duration
	}
	at = s.clampPosition(at, false)
	e.stopLocked()

	e.mu.Lock()
	e.stream = s
	e.seekBase = at
	e.mu.Unlock()

	return e.startLocked(ctx, s, at)
}

// startLocked 启动 ffmpeg 与播放器；调用方需保证没有正在播放的实例。
func (e *Engine) startLocked(ctx context.Context, s Stream, seek time.Duration) error {
	streamURL := s.URL
	var proxy *headerProxy
	if len(s.Headers) > 0 {
		var err error
		proxy, err = newHeaderProxy(s.URL, s.Headers)
		if err != nil {
			return err
		}
		streamURL = proxy.URL()
	}

	args := ffmpegArgs(streamURL, seek, s.clipEnd())

	cmd := exec.CommandContext(ctx, e.ffmpeg, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		if proxy != nil {
			_ = proxy.Close()
		}
		return fmt.Errorf("audio: 获取解码输出: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		if proxy != nil {
			_ = proxy.Close()
		}
		return fmt.Errorf("audio: 获取解码日志: %w", err)
	}
	if err := cmd.Start(); err != nil {
		if proxy != nil {
			_ = proxy.Close()
		}
		return fmt.Errorf("audio: 启动 ffmpeg: %w", err)
	}

	gain := newGainReader(stdout)
	player := e.ctx.NewPlayer(gain)
	player.SetVolume(1)
	player.Play()

	e.mu.Lock()
	e.gen++
	gen := e.gen
	e.cmd = cmd
	e.proxy = proxy
	e.player = player
	e.gain = gain
	e.seekBase = seek
	e.state = StatePlaying
	volume := e.volume
	comp := e.compDB
	e.mu.Unlock()

	gain.setGain(volume * dbToGain(comp))
	e.emit(Event{Type: EventStateChanged, State: StatePlaying})

	go e.collectStderr(stderr, gen)
	go e.watch(cmd, player, gen)
	return nil
}

func (e *Engine) collectStderr(r io.Reader, gen int) {
	scanner := bufio.NewScanner(r)
	var last bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		last.Reset()
		last.WriteString(line)
	}
	msg := strings.TrimSpace(last.String())
	if msg == "" {
		return
	}
	e.mu.Lock()
	if e.gen != gen {
		e.mu.Unlock()
		return
	}
	failed := e.state == StatePlaying || e.state == StatePaused
	if failed {
		e.state = StateFailed
	}
	e.mu.Unlock()
	if failed {
		e.emit(Event{Type: EventFailed, State: StateFailed, Err: errors.New("audio: " + msg)})
	}
}

// watch 等待 ffmpeg 退出并给出结束/失败事件。
func (e *Engine) watch(cmd *exec.Cmd, player *oto.Player, gen int) {
	err := cmd.Wait()

	e.mu.Lock()
	if e.gen != gen {
		e.mu.Unlock()
		return // 已经被新的播放替换
	}
	state := e.state
	proxy := e.proxy
	e.proxy = nil
	e.mu.Unlock()
	if proxy != nil {
		_ = proxy.Close()
	}

	// 让 oto 把缓冲里的数据放完。
	time.Sleep(120 * time.Millisecond)
	_ = player.Close()

	if state == StateFailed {
		return // stderr 已报告过
	}
	if err != nil {
		e.mu.Lock()
		if e.gen != gen {
			e.mu.Unlock()
			return
		}
		e.state = StateFailed
		e.mu.Unlock()
		e.emit(Event{Type: EventFailed, State: StateFailed, Err: fmt.Errorf("audio: ffmpeg 退出: %w", err)})
		return
	}

	e.mu.Lock()
	if e.gen != gen {
		e.mu.Unlock()
		return
	}
	e.state = StateEnded
	e.mu.Unlock()
	e.emit(Event{Type: EventEnded, State: StateEnded})
}

// Pause 暂停播放（同时停止读取，避免继续下载）。
func (e *Engine) Pause() {
	e.mu.Lock()
	player := e.player
	if e.state != StatePlaying {
		e.mu.Unlock()
		return
	}
	e.state = StatePaused
	e.mu.Unlock()
	if player != nil {
		player.PauseAndStopReading()
	}
	e.emit(Event{Type: EventStateChanged, State: StatePaused})
}

// Resume 恢复播放。
func (e *Engine) Resume() {
	e.mu.Lock()
	player := e.player
	if e.state != StatePaused {
		e.mu.Unlock()
		return
	}
	e.state = StatePlaying
	e.mu.Unlock()
	if player != nil {
		player.Play()
	}
	e.emit(Event{Type: EventStateChanged, State: StatePlaying})
}

// Stop 停止播放并释放资源。
func (e *Engine) Stop() {
	e.stopLocked()
	e.mu.Lock()
	e.state = StateIdle
	e.stream = Stream{}
	e.seekBase = 0
	e.mu.Unlock()
	e.emit(Event{Type: EventStateChanged, State: StateIdle})
}

func (e *Engine) stopLocked() {
	e.mu.Lock()
	player := e.player
	cmd := e.cmd
	proxy := e.proxy
	gain := e.gain
	e.player = nil
	e.cmd = nil
	e.proxy = nil
	e.gain = nil
	e.gen++
	e.mu.Unlock()

	if player != nil {
		_ = player.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if proxy != nil {
		_ = proxy.Close()
	}
	_ = gain
}

// Seek 跳转到指定位置（重启 ffmpeg，定位到目标时间）。
func (e *Engine) Seek(ctx context.Context, pos time.Duration) error {
	if pos < 0 {
		pos = 0
	}
	e.mu.Lock()
	s := e.stream
	duration := time.Duration(s.DurationMS) * time.Millisecond
	e.mu.Unlock()
	if s.URL == "" {
		return errors.New("audio: 还没有正在播放的曲目")
	}
	if duration > 0 && pos > duration {
		pos = duration
	}
	pos = s.clampPosition(pos, true)
	e.stopLocked()
	return e.startLocked(ctx, s, pos)
}

// Position 返回当前播放位置。
func (e *Engine) Position() time.Duration {
	e.mu.Lock()
	gain := e.gain
	base := e.seekBase
	e.mu.Unlock()
	if gain == nil {
		return base
	}
	return base + time.Duration(gain.BytesRead()*int64(time.Second)/bytesPerSecond)
}

// Duration 返回当前曲目总时长。
func (e *Engine) Duration() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return time.Duration(e.stream.DurationMS) * time.Millisecond
}

// CurrentStream 返回当前流的副本。
func (e *Engine) CurrentStream() Stream {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stream
}

// Close 停止播放并释放设备；可重复调用。
func (e *Engine) Close() error {
	e.stopLocked()
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.state = StateIdle
	// 持锁关闭，保证 emit 不会向已关闭的 channel 发送而 panic。
	close(e.events)
	e.mu.Unlock()
	return nil
}

func (e *Engine) emit(ev Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	select {
	case e.events <- ev:
	default:
	}
}

// ffmpegArgs 拼装解码参数；clipEnd > 0 时用 -t 把输出限制到区间末尾。
func ffmpegArgs(streamURL string, seek, clipEnd time.Duration) []string {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
	}
	if seek > 0 {
		args = append(args, "-ss", formatSeconds(seek))
	}
	args = append(args, "-i", streamURL)
	if clipEnd > seek {
		args = append(args, "-t", formatSeconds(clipEnd-seek))
	}
	args = append(args,
		"-vn", "-f", "s16le", "-ac", strconv.Itoa(channelCount), "-ar", strconv.Itoa(sampleRate),
		"pipe:1",
	)
	return args
}

func formatSeconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 3, 64)
}
