//go:build linux

package sysmedia

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/sheyiyuan/zero-beat/internal/audio"
)

const (
	mprisPath        = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	mprisRootIface   = "org.mpris.MediaPlayer2"
	mprisPlayerIface = "org.mpris.MediaPlayer2.Player"
	propsIface       = "org.freedesktop.DBus.Properties"

	noTrack = dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack")
)

// mprisName 默认是标准名称；测试可用 ZERO_MPRIS_NAME 隔离，避免和本机
// 正在运行的 zero 抢名字。
func mprisName() string {
	if name := strings.TrimSpace(os.Getenv("ZERO_MPRIS_NAME")); name != "" {
		return name
	}
	return "org.mpris.MediaPlayer2.zero"
}

var playerPropertyNames = []string{
	"PlaybackStatus", "LoopStatus", "Rate", "Shuffle", "Metadata", "Volume",
	"Position", "MinimumRate", "MaximumRate", "CanGoNext", "CanGoPrevious",
	"CanPlay", "CanPause", "CanSeek", "CanControl",
}

type linuxService struct {
	conn     *dbus.Conn
	busName  string
	commands chan Command

	mu   sync.RWMutex
	snap Snapshot
	at   time.Time

	closed     atomic.Bool
	closedOnce sync.Once
}

// New 连接会话总线并导出 MPRIS 播放器。
func New() (Service, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("sysmedia: 连接会话总线: %w", err)
	}
	s := &linuxService{conn: conn, commands: make(chan Command, 8)}
	if err := s.export(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return s, nil
}

func (s *linuxService) export() error {
	// 先把对象和接口全部挂好，再申请总线名：GNOME Shell / mpris-proxy 会在
	// 名字出现的瞬间马上调 GetAll，晚一步导出就会返回 UnknownInterface。
	exports := []struct {
		iface   string
		obj     any
		mapping map[string]string
	}{
		{mprisRootIface, &root{s}, nil},
		// D-Bus 侧的方法名必须是 Seek，但 Go 方法不能叫 Seek：
		// 那会冒充 io.Seeker，go vet 的 stdmethods 会报签名不匹配。
		{mprisPlayerIface, &player{s}, map[string]string{"SeekOffset": "Seek"}},
		{propsIface, &props{s}, nil},
	}
	for _, e := range exports {
		if err := s.conn.ExportWithMap(e.obj, e.mapping, mprisPath, e.iface); err != nil {
			return fmt.Errorf("sysmedia: 导出 %s: %w", e.iface, err)
		}
	}
	node := mprisIntrospection()
	marker := introspect.NewIntrospectable(node)
	if err := s.conn.Export(marker, mprisPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return fmt.Errorf("sysmedia: 导出内省信息: %w", err)
	}

	name := mprisName()
	reply, err := s.conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("sysmedia: 申请 MPRIS 名称: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		// 不再退到 .instance<pid>：那会让系统面板同时看到两个「zero-beat」，
		// 控制其中一个、界面上却是另一个。宁可明确告知用户关掉旧实例。
		return fmt.Errorf("系统媒体名称 %s 已被另一个 zero-beat 实例占用；请先退出旧实例再启动", name)
	}
	s.busName = name
	return nil
}

func mprisIntrospection() *introspect.Node {
	rootProps := []introspect.Property{
		{Name: "Identity", Type: "s", Access: "read"},
		{Name: "DesktopEntry", Type: "s", Access: "read"},
		{Name: "SupportedUriSchemes", Type: "as", Access: "read"},
		{Name: "SupportedMimeTypes", Type: "as", Access: "read"},
		{Name: "CanQuit", Type: "b", Access: "read"},
		{Name: "CanRaise", Type: "b", Access: "read"},
		{Name: "HasTrackList", Type: "b", Access: "read"},
	}
	playerProps := []introspect.Property{
		{Name: "PlaybackStatus", Type: "s", Access: "read"},
		{Name: "LoopStatus", Type: "s", Access: "readwrite"},
		{Name: "Rate", Type: "d", Access: "read"},
		{Name: "Shuffle", Type: "b", Access: "readwrite"},
		{Name: "Metadata", Type: "a{sv}", Access: "read"},
		{Name: "Volume", Type: "d", Access: "readwrite"},
		{Name: "Position", Type: "x", Access: "read"},
		{Name: "MinimumRate", Type: "d", Access: "read"},
		{Name: "MaximumRate", Type: "d", Access: "read"},
		{Name: "CanGoNext", Type: "b", Access: "read"},
		{Name: "CanGoPrevious", Type: "b", Access: "read"},
		{Name: "CanPlay", Type: "b", Access: "read"},
		{Name: "CanPause", Type: "b", Access: "read"},
		{Name: "CanSeek", Type: "b", Access: "read"},
		{Name: "CanControl", Type: "b", Access: "read"},
	}
	return &introspect.Node{
		Name: string(mprisPath),
		Interfaces: []introspect.Interface{
			{
				Name:       mprisRootIface,
				Methods:    introspect.Methods(&root{}),
				Properties: rootProps,
			},
			{
				Name:       mprisPlayerIface,
				Methods:    introspect.Methods(&player{}),
				Properties: playerProps,
				Signals: []introspect.Signal{
					{Name: "Seeked", Args: []introspect.Arg{{Name: "Position", Type: "x", Direction: "out"}}},
				},
			},
			{
				Name:    propsIface,
				Methods: introspect.Methods(&props{}),
			},
		},
	}
}

// Publish 更新快照；只在用户能感知的字段变化时发 PropertiesChanged。
func (s *linuxService) Publish(snap Snapshot) {
	s.mu.Lock()
	old := s.snap
	s.snap = snap
	s.at = time.Now()
	s.mu.Unlock()

	if names := propertyChanges(old, snap); len(names) > 0 {
		s.emitProps(names...)
	}
	if old.TrackID != "" && old.TrackID == snap.TrackID {
		delta := snap.Position - old.Position
		if delta < 0 {
			delta = -delta
		}
		if delta > 2*time.Second {
			s.emitSeeked(snap.Position)
		}
	}
}

func propertyChanges(old, snap Snapshot) []string {
	var names []string
	if playbackStatus(old.State) != playbackStatus(snap.State) {
		names = append(names, "PlaybackStatus")
	}
	if metadataChanged(old, snap) {
		names = append(names, "Metadata")
	}
	if effectiveVolume(old) != effectiveVolume(snap) {
		names = append(names, "Volume")
	}
	if old.Shuffle != snap.Shuffle {
		names = append(names, "Shuffle")
	}
	if normalizeLoop(old.Loop) != normalizeLoop(snap.Loop) {
		names = append(names, "LoopStatus")
	}
	if old.CanNext != snap.CanNext {
		names = append(names, "CanGoNext")
	}
	if old.CanPrev != snap.CanPrev {
		names = append(names, "CanGoPrevious")
	}
	if canSeek(old) != canSeek(snap) {
		names = append(names, "CanSeek")
	}
	if (old.TrackID != "") != (snap.TrackID != "") {
		// GNOME Shell 只在 can-play 从 false 变 true 时把播放器加入媒体列表；
		// 启动时先发布了空快照，恢复队列后必须显式通知，否则永远不显示。
		names = append(names, "CanPlay", "CanPause")
	}
	return names
}

func (s *linuxService) emitSeeked(position time.Duration) {
	if s.closed.Load() {
		return
	}
	_ = s.conn.Emit(mprisPath, mprisPlayerIface+".Seeked", position.Microseconds())
}

func (s *linuxService) Commands() <-chan Command { return s.commands }

func (s *linuxService) Close() {
	s.closedOnce.Do(func() {
		s.closed.Store(true)
		if s.busName != "" {
			_, _ = s.conn.ReleaseName(s.busName)
		}
		_ = s.conn.Close()
	})
}

func (s *linuxService) enqueue(cmd Command) {
	if s.closed.Load() {
		return
	}
	select {
	case s.commands <- cmd:
	default:
	}
}

func (s *linuxService) emitProps(names ...string) {
	if s.closed.Load() {
		return
	}
	changed := map[string]dbus.Variant{}
	s.mu.RLock()
	for _, name := range names {
		if v, ok := s.playerPropertyLocked(name); ok {
			changed[name] = v
		}
	}
	s.mu.RUnlock()
	if len(changed) == 0 {
		return
	}
	_ = s.conn.Emit(mprisPath, propsIface+".PropertiesChanged", mprisPlayerIface, changed, []string{})
}

func (s *linuxService) playerPropertyLocked(name string) (dbus.Variant, bool) {
	switch name {
	case "PlaybackStatus":
		return dbus.MakeVariant(playbackStatus(s.snap.State)), true
	case "LoopStatus":
		return dbus.MakeVariant(string(normalizeLoop(s.snap.Loop))), true
	case "Rate":
		return dbus.MakeVariant(1.0), true
	case "Shuffle":
		return dbus.MakeVariant(s.snap.Shuffle), true
	case "Metadata":
		return dbus.MakeVariant(s.metadataLocked()), true
	case "Volume":
		return dbus.MakeVariant(effectiveVolume(s.snap)), true
	case "Position":
		return dbus.MakeVariant(s.positionLocked().Microseconds()), true
	case "MinimumRate":
		return dbus.MakeVariant(1.0), true
	case "MaximumRate":
		return dbus.MakeVariant(1.0), true
	case "CanGoNext":
		return dbus.MakeVariant(s.snap.CanNext), true
	case "CanGoPrevious":
		return dbus.MakeVariant(s.snap.CanPrev), true
	case "CanPlay":
		return dbus.MakeVariant(s.snap.TrackID != ""), true
	case "CanPause":
		return dbus.MakeVariant(s.snap.TrackID != ""), true
	case "CanSeek":
		return dbus.MakeVariant(canSeek(s.snap)), true
	case "CanControl":
		return dbus.MakeVariant(true), true
	default:
		return dbus.Variant{}, false
	}
}

func (s *linuxService) metadataLocked() map[string]dbus.Variant {
	track := trackPath(s.snap.TrackID)
	meta := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(track),
	}
	if s.snap.TrackID == "" {
		return meta
	}
	if s.snap.Title != "" {
		meta["xesam:title"] = dbus.MakeVariant(s.snap.Title)
	}
	if artists := artistList(s.snap.Artist); len(artists) > 0 {
		meta["xesam:artist"] = dbus.MakeVariant(artists)
	}
	if s.snap.Album != "" {
		meta["xesam:album"] = dbus.MakeVariant(s.snap.Album)
	}
	if s.snap.Duration > 0 {
		meta["mpris:length"] = dbus.MakeVariant(s.snap.Duration.Microseconds())
	}
	if s.snap.Artwork != "" {
		meta["mpris:artUrl"] = dbus.MakeVariant(fileURL(s.snap.Artwork))
	}
	return meta
}

func (s *linuxService) positionLocked() time.Duration {
	pos := s.snap.Position
	if s.snap.State == audio.StatePlaying && !s.at.IsZero() {
		pos += time.Since(s.at)
	}
	if s.snap.Duration > 0 && pos > s.snap.Duration {
		pos = s.snap.Duration
	}
	if pos < 0 {
		pos = 0
	}
	return pos
}

func (s *linuxService) rootVariants() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"Identity":            dbus.MakeVariant("zero-beat"),
		"DesktopEntry":        dbus.MakeVariant("zero"),
		"SupportedUriSchemes": dbus.MakeVariant([]string{}),
		"SupportedMimeTypes":  dbus.MakeVariant([]string{}),
		"CanQuit":             dbus.MakeVariant(false),
		"CanRaise":            dbus.MakeVariant(false),
		"HasTrackList":        dbus.MakeVariant(false),
	}
}

func (s *linuxService) allPlayerVariantsLocked() map[string]dbus.Variant {
	out := make(map[string]dbus.Variant, len(playerPropertyNames))
	for _, name := range playerPropertyNames {
		if v, ok := s.playerPropertyLocked(name); ok {
			out[name] = v
		}
	}
	return out
}

type root struct{ s *linuxService }

func (r *root) Raise() *dbus.Error { return nil }
func (r *root) Quit() *dbus.Error  { return nil }

type props struct{ s *linuxService }

func (p *props) Get(iface string, name string) (dbus.Variant, *dbus.Error) {
	p.s.mu.RLock()
	defer p.s.mu.RUnlock()
	switch iface {
	case mprisRootIface:
		if v, ok := p.s.rootVariants()[name]; ok {
			return v, nil
		}
	case mprisPlayerIface:
		if v, ok := p.s.playerPropertyLocked(name); ok {
			return v, nil
		}
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []any{name})
	default:
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", []any{iface})
	}
	return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []any{name})
}

func (p *props) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	p.s.mu.RLock()
	defer p.s.mu.RUnlock()
	switch iface {
	case mprisRootIface:
		return p.s.rootVariants(), nil
	case mprisPlayerIface:
		return p.s.allPlayerVariantsLocked(), nil
	default:
		return nil, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", []any{iface})
	}
}

func (p *props) Set(iface string, name string, value dbus.Variant) *dbus.Error {
	if iface != mprisPlayerIface {
		return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []any{name})
	}
	switch name {
	case "Volume":
		volume, ok := value.Value().(float64)
		if !ok {
			return dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []any{"Volume 需要 double"})
		}
		if volume < 0 {
			volume = 0
		}
		if volume > 1 {
			volume = 1
		}
		p.s.enqueue(Command{Kind: CommandSetVolume, Volume: volume})
		return nil
	case "Shuffle":
		shuffle, ok := value.Value().(bool)
		if !ok {
			return dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []any{"Shuffle 需要 boolean"})
		}
		p.s.enqueue(Command{Kind: CommandSetShuffle, Shuffle: shuffle})
		return nil
	case "LoopStatus":
		raw, ok := value.Value().(string)
		if !ok {
			return dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []any{"LoopStatus 需要 string"})
		}
		loop := LoopStatus(raw)
		if !loop.Valid() {
			return dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs",
				[]any{"LoopStatus 只支持 None / Track / Playlist"})
		}
		p.s.enqueue(Command{Kind: CommandSetLoopStatus, Loop: loop})
		return nil
	default:
		return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []any{name})
	}
}

type player struct{ s *linuxService }

func (p *player) Next() *dbus.Error {
	p.s.enqueue(Command{Kind: CommandNext})
	return nil
}

func (p *player) Previous() *dbus.Error {
	p.s.enqueue(Command{Kind: CommandPrevious})
	return nil
}

func (p *player) Pause() *dbus.Error {
	p.s.enqueue(Command{Kind: CommandPause})
	return nil
}

func (p *player) PlayPause() *dbus.Error {
	p.s.enqueue(Command{Kind: CommandToggle})
	return nil
}

func (p *player) Stop() *dbus.Error {
	p.s.enqueue(Command{Kind: CommandStop})
	return nil
}

func (p *player) Play() *dbus.Error {
	p.s.enqueue(Command{Kind: CommandPlay})
	return nil
}

// SeekOffset 对应 MPRIS 的 Seek(Offset) 方法；导出时映射回 D-Bus 名 Seek。
func (p *player) SeekOffset(offset int64) *dbus.Error {
	p.s.enqueue(Command{Kind: CommandSeekBy, Offset: time.Duration(offset) * time.Microsecond})
	return nil
}

func (p *player) SetPosition(trackID dbus.ObjectPath, position int64) *dbus.Error {
	p.s.mu.RLock()
	songID := p.s.snap.TrackID
	expected := trackPath(songID)
	p.s.mu.RUnlock()
	if songID == "" || trackID != expected {
		return nil
	}
	p.s.enqueue(Command{
		Kind:     CommandSeekTo,
		TrackID:  songID,
		Position: time.Duration(position) * time.Microsecond,
	})
	return nil
}

func normalizeLoop(l LoopStatus) LoopStatus {
	if l == "" {
		return LoopNone
	}
	return l
}

func playbackStatus(st audio.State) string {
	switch st {
	case audio.StatePlaying, audio.StateBuffering:
		return "Playing"
	case audio.StatePaused:
		return "Paused"
	default:
		return "Stopped"
	}
}

func effectiveVolume(s Snapshot) float64 {
	if s.Muted {
		return 0
	}
	return s.Volume
}

func canSeek(s Snapshot) bool {
	return s.TrackID != "" && s.Duration > 0
}

func metadataChanged(a, b Snapshot) bool {
	return a.TrackID != b.TrackID ||
		a.Title != b.Title ||
		a.Artist != b.Artist ||
		a.Album != b.Album ||
		a.Artwork != b.Artwork ||
		a.Duration != b.Duration
}

func artistList(artist string) []string {
	if artist == "" {
		return nil
	}
	parts := strings.Split(artist, " / ")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func trackPath(songID string) dbus.ObjectPath {
	if songID == "" {
		return noTrack
	}
	sum := sha1.Sum([]byte(songID))
	return dbus.ObjectPath("/org/zero_beat/track/" + hex.EncodeToString(sum[:]))
}
