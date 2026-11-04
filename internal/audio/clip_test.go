package audio

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func argValue(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func TestFFmpegArgsWithClip(t *testing.T) {
	args := ffmpegArgs("http://stream", 30*time.Second, 90*time.Second, false)
	if got := argValue(args, "-ss"); got != "30.000" {
		t.Fatalf("-ss = %q，期望 30.000: %v", got, args)
	}
	if got := argValue(args, "-t"); got != "60.000" {
		t.Fatalf("-t = %q，期望 60.000: %v", got, args)
	}
}

func TestFFmpegArgsWithoutEnd(t *testing.T) {
	args := ffmpegArgs("http://stream", 30*time.Second, 0, false)
	if got := argValue(args, "-t"); got != "" {
		t.Fatalf("没有区间终点时不应有 -t: %v", args)
	}
	if got := argValue(args, "-af"); got != "" {
		t.Fatalf("关闭自动响度均衡时不应有 -af: %v", args)
	}
}

func TestFFmpegArgsWithAutoGain(t *testing.T) {
	args := ffmpegArgs("http://stream", 0, 0, true)
	if got := argValue(args, "-af"); got != loudnormFilter {
		t.Fatalf("-af = %q，期望 %q: %v", got, loudnormFilter, args)
	}
}

func TestStreamClampPosition(t *testing.T) {
	s := Stream{ClipStartMS: 30000, ClipEndMS: 60000}
	if got := s.clampPosition(10*time.Second, false); got != 30*time.Second {
		t.Fatalf("起点前应回到片段起点，实际 %v", got)
	}
	if got := s.clampPosition(70*time.Second, false); got != 30*time.Second {
		t.Fatalf("PlayAt 越过后应回到起点，实际 %v", got)
	}
	if got := s.clampPosition(70*time.Second, true); got != 60*time.Second-time.Millisecond {
		t.Fatalf("Seek 越过后应停在终点前，实际 %v", got)
	}
}

func TestStreamClipBoundsIgnoresInvalidRange(t *testing.T) {
	s := Stream{ClipStartMS: 60000, ClipEndMS: 30000}
	start, end := s.clipBounds()
	if start != 60*time.Second || end != 0 {
		t.Fatalf("非法区间应只保留起点: start=%v end=%v", start, end)
	}
}

func TestFFmpegClipProducesExactDuration(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg 未安装")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "in.wav")
	if err := os.WriteFile(input, testWAV(5), 0o644); err != nil {
		t.Fatal(err)
	}
	args := ffmpegArgs(input, 1*time.Second, 3*time.Second, false)
	out, err := exec.Command(ffmpeg, args...).Output()
	if err != nil {
		t.Fatalf("ffmpeg 失败: %v", err)
	}
	want := 2 * 48000 * 2 * 2 // 2 秒 s16le 立体声 48kHz
	if len(out) != want {
		t.Fatalf("片段输出 %d 字节，期望 %d", len(out), want)
	}
}

func TestFFmpegAutoGainProducesAudio(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg 未安装")
	}
	if !detectLoudnorm(ffmpeg) {
		t.Skip("ffmpeg 未提供 loudnorm 滤镜")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "in.wav")
	if err := os.WriteFile(input, testWAV(3), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(ffmpeg, ffmpegArgs(input, 0, 0, true)...).Output()
	if err != nil {
		t.Fatalf("带 loudnorm 的 ffmpeg 失败: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("自动响度均衡输出为空")
	}
}
