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
