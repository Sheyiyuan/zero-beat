package sysmedia

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/sheyiyuan/zero-beat/internal/audio"
)

func TestMPRISExportsStateAndCommands(t *testing.T) {
	if _, err := dbus.SessionBus(); err != nil {
		t.Skip("没有可用的会话总线")
	}
	// 测试用独立名称，避免和本机正在运行的 zero 抢标准 MPRIS 名。
	t.Setenv("ZERO_MPRIS_NAME", fmt.Sprintf("org.mpris.MediaPlayer2.zero.test%d", os.Getpid()))
	svc, err := New()
	if err != nil {
		t.Skipf("MPRIS 不可用: %v", err)
	}
	defer svc.Close()

	s := svc.(*linuxService)
	conn, err := dbus.SessionBus()
	if err != nil {
		t.Fatalf("连接会话总线: %v", err)
	}
	defer conn.Close()

	svc.Publish(Snapshot{
		TrackID:  "bilibili:BV1xx:1",
		Title:    "测试歌曲",
		Artist:   "甲 / 乙",
		Album:    "测试专辑",
		Artwork:  filepath.Join(t.TempDir(), "cover.jpg"),
		Duration: 3 * time.Minute,
		Position: 30 * time.Second,
		State:    audio.StatePaused,
		Volume:   0.5,
		CanNext:  true,
		CanPrev:  true,
	})

	obj := conn.Object(s.busName, mprisPath)
	var status string
	if err := obj.Call(propsIface+".Get", 0, mprisPlayerIface, "PlaybackStatus").Store(&status); err != nil {
		t.Fatalf("读取 PlaybackStatus: %v", err)
	}
	if status != "Paused" {
		t.Fatalf("PlaybackStatus = %q", status)
	}

	var meta map[string]dbus.Variant
	if err := obj.Call(propsIface+".Get", 0, mprisPlayerIface, "Metadata").Store(&meta); err != nil {
		t.Fatalf("读取 Metadata: %v", err)
	}
	if got := meta["xesam:title"].Value(); got != "测试歌曲" {
		t.Fatalf("xesam:title = %v", got)
	}
	artists, ok := meta["xesam:artist"].Value().([]string)
	if !ok || len(artists) != 2 || artists[0] != "甲" {
		t.Fatalf("xesam:artist = %#v", meta["xesam:artist"].Value())
	}
	if _, ok := meta["mpris:artUrl"]; !ok {
		t.Fatal("Metadata 缺少 mpris:artUrl")
	}
	if length, ok := meta["mpris:length"].Value().(int64); !ok || length != 3*time.Minute.Microseconds() {
		t.Fatalf("mpris:length = %#v", meta["mpris:length"].Value())
	}

	var position int64
	if err := obj.Call(propsIface+".Get", 0, mprisPlayerIface, "Position").Store(&position); err != nil {
		t.Fatalf("读取 Position: %v", err)
	}
	if position != 30*time.Second.Microseconds() {
		t.Fatalf("Position = %d", position)
	}

	var introspected string
	if err := obj.Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&introspected); err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	if want := mprisPlayerIface; !strings.Contains(introspected, want) {
		t.Fatalf("内省结果缺少 %q", want)
	}

	if err := obj.Call(mprisPlayerIface+".PlayPause", 0).Err; err != nil {
		t.Fatalf("PlayPause: %v", err)
	}
	select {
	case cmd := <-svc.Commands():
		if cmd.Kind != CommandToggle {
			t.Fatalf("命令 = %+v", cmd)
		}
	case <-time.After(time.Second):
		t.Fatal("没有收到 PlayPause 命令")
	}

	if err := obj.Call(mprisPlayerIface+".SetPosition", 0, trackPath("bilibili:BV1xx:1"), int64(90*time.Second.Microseconds())).Err; err != nil {
		t.Fatalf("SetPosition: %v", err)
	}
	select {
	case cmd := <-svc.Commands():
		if cmd.Kind != CommandSeekTo || cmd.TrackID != "bilibili:BV1xx:1" || cmd.Position != 90*time.Second {
			t.Fatalf("SeekTo 命令 = %+v", cmd)
		}
	case <-time.After(time.Second):
		t.Fatal("没有收到 SetPosition 命令")
	}

	if err := obj.Call(propsIface+".Set", 0, mprisPlayerIface, "LoopStatus", dbus.MakeVariant("Track")).Err; err != nil {
		t.Fatalf("写入 LoopStatus: %v", err)
	}
	select {
	case cmd := <-svc.Commands():
		if cmd.Kind != CommandSetLoopStatus || cmd.Loop != LoopTrack {
			t.Fatalf("LoopStatus 命令 = %+v", cmd)
		}
	case <-time.After(time.Second):
		t.Fatal("没有收到 LoopStatus 写入命令")
	}
	var loop string
	if err := obj.Call(propsIface+".Get", 0, mprisPlayerIface, "LoopStatus").Store(&loop); err != nil {
		t.Fatalf("读取 LoopStatus: %v", err)
	}
	if loop != string(LoopNone) {
		t.Fatalf("写入只入队命令，快照未更新时 LoopStatus 应仍是 %q，实际 %q", LoopNone, loop)
	}
	if err := obj.Call(propsIface+".Set", 0, mprisPlayerIface, "LoopStatus", dbus.MakeVariant("loop")).Err; err == nil {
		t.Fatal("非法 LoopStatus 值应被拒绝")
	}
}

func TestPropertiesSetEnqueuesOrderCommands(t *testing.T) {
	s := &linuxService{commands: make(chan Command, 8)}
	p := &props{s: s}

	cases := []struct {
		name  string
		prop  string
		value dbus.Variant
		want  Command
	}{
		{"随机开启", "Shuffle", dbus.MakeVariant(true), Command{Kind: CommandSetShuffle, Shuffle: true}},
		{"随机关闭", "Shuffle", dbus.MakeVariant(false), Command{Kind: CommandSetShuffle}},
		{"播完即停", "LoopStatus", dbus.MakeVariant("None"), Command{Kind: CommandSetLoopStatus, Loop: LoopNone}},
		{"单曲循环", "LoopStatus", dbus.MakeVariant("Track"), Command{Kind: CommandSetLoopStatus, Loop: LoopTrack}},
		{"列表循环", "LoopStatus", dbus.MakeVariant("Playlist"), Command{Kind: CommandSetLoopStatus, Loop: LoopPlaylist}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := p.Set(mprisPlayerIface, tc.prop, tc.value); err != nil {
				t.Fatalf("写入 %s 应成功: %v", tc.prop, err)
			}
			select {
			case got := <-s.commands:
				if got != tc.want {
					t.Fatalf("命令 = %+v，期望 %+v", got, tc.want)
				}
			default:
				t.Fatal("没有入队命令")
			}
		})
	}
}

func TestPropertiesSetRejectsBadValues(t *testing.T) {
	s := &linuxService{commands: make(chan Command, 8)}
	p := &props{s: s}

	rejected := []struct {
		name  string
		value dbus.Variant
	}{
		{"Shuffle", dbus.MakeVariant("true")},
		{"LoopStatus", dbus.MakeVariant("track")},
		{"LoopStatus", dbus.MakeVariant("Repeat")},
		{"LoopStatus", dbus.MakeVariant(2)},
	}
	for _, tc := range rejected {
		err := p.Set(mprisPlayerIface, tc.name, tc.value)
		if err == nil || !strings.Contains(err.Name, "InvalidArgs") {
			t.Fatalf("%s=%v 应返回 InvalidArgs，实际 %v", tc.name, tc.value, err)
		}
	}
	for _, name := range []string{"Metadata", "PlaybackStatus", "Position"} {
		err := p.Set(mprisPlayerIface, name, dbus.MakeVariant("x"))
		if err == nil || !strings.Contains(err.Name, "PropertyReadOnly") {
			t.Fatalf("%s 应保持只读，实际 %v", name, err)
		}
	}
	select {
	case cmd := <-s.commands:
		t.Fatalf("非法写入不应入队命令: %+v", cmd)
	default:
	}
}

func TestPropertyChangesCoversShuffleAndLoop(t *testing.T) {
	old := Snapshot{TrackID: "a", Shuffle: true, Loop: LoopPlaylist}
	next := Snapshot{TrackID: "a", Shuffle: false, Loop: LoopNone}
	names := propertyChanges(old, next)
	for _, want := range []string{"Shuffle", "LoopStatus"} {
		if !slices.Contains(names, want) {
			t.Fatalf("缺少属性 %q，实际 %v", want, names)
		}
	}

	// 空快照与显式 None 是同一个状态，不应产生抖动。
	if names := propertyChanges(Snapshot{}, Snapshot{Loop: LoopNone}); len(names) != 0 {
		t.Fatalf("空 Loop 与 None 应等价，实际 %v", names)
	}
}

func TestIntrospectionMarksOrderPropertiesWritable(t *testing.T) {
	access := map[string]string{}
	for _, iface := range mprisIntrospection().Interfaces {
		if iface.Name != mprisPlayerIface {
			continue
		}
		for _, prop := range iface.Properties {
			access[prop.Name] = prop.Access
		}
	}
	if len(access) == 0 {
		t.Fatal("内省结果缺少 Player 接口")
	}
	for _, name := range []string{"Shuffle", "LoopStatus", "Volume"} {
		if access[name] != "readwrite" {
			t.Fatalf("%s access = %q，期望 readwrite", name, access[name])
		}
	}
	for _, name := range []string{"Metadata", "PlaybackStatus", "Position"} {
		if access[name] != "read" {
			t.Fatalf("%s 应保持只读，实际 %q", name, access[name])
		}
	}
}

func TestMPRISNameConflictIsExplicit(t *testing.T) {
	if _, err := dbus.SessionBus(); err != nil {
		t.Skip("没有可用的会话总线")
	}
	t.Setenv("ZERO_MPRIS_NAME", fmt.Sprintf("org.mpris.MediaPlayer2.zero.conflict%d", os.Getpid()))
	first, err := New()
	if err != nil {
		t.Skipf("MPRIS 不可用: %v", err)
	}
	defer first.Close()
	if _, err := New(); err == nil {
		t.Fatal("第二个实例应明确报名称冲突，而不是退到 .instance")
	}
}

func TestPropertyChangesOnTrackAppear(t *testing.T) {
	old := Snapshot{State: audio.StateIdle}
	next := Snapshot{TrackID: "a", Title: "标题", State: audio.StatePaused, Duration: time.Minute}
	names := propertyChanges(old, next)
	if len(names) == 0 {
		t.Fatal("从空状态进入曲目应产生 PropertiesChanged")
	}
	for _, want := range []string{"PlaybackStatus", "Metadata", "CanPlay", "CanPause", "CanSeek"} {
		if !slices.Contains(names, want) {
			t.Fatalf("缺少属性 %q，实际 %v", want, names)
		}
	}
}

func TestPropertyChangesIgnoresPositionOnly(t *testing.T) {
	old := Snapshot{TrackID: "a", State: audio.StatePlaying, Duration: time.Minute, Position: 10 * time.Second}
	next := old
	next.Position = 11 * time.Second
	if names := propertyChanges(old, next); len(names) != 0 {
		t.Fatalf("纯进度变化不应触发属性信号: %v", names)
	}
}

func TestTrackPathIsObjectPath(t *testing.T) {
	for _, id := range []string{"", "bilibili:BV1xx:1", "netease:123:1"} {
		path := trackPath(id)
		if len(path) == 0 || path[0] != '/' {
			t.Fatalf("trackPath(%q) = %q", id, path)
		}
		if !path.IsValid() {
			t.Fatalf("ObjectPath 非法: %q", path)
		}
	}
}
