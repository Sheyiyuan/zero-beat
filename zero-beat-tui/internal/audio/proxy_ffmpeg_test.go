package audio

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"
)

func testWAV(seconds int) []byte {
	const rate = 8000
	data := make([]byte, 44+seconds*rate*2)
	copy(data[0:], "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], rate)
	binary.LittleEndian.PutUint32(data[28:], rate*2)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(len(data)-44))
	return data
}

func TestHeaderProxyWorksWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg 未安装")
	}
	var gotCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(testWAV(1))
	}))
	defer upstream.Close()
	proxy, err := newHeaderProxy(upstream.URL, map[string]string{"Cookie": "MUSIC_U=secret"})
	if err != nil {
		t.Fatalf("启动代理失败: %v", err)
	}
	defer proxy.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-i", proxy.URL(), "-f", "null", "-")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg 通过代理解码失败: %v\n%s", err, out)
	}
	if gotCookie != "MUSIC_U=secret" {
		t.Fatalf("上游未收到 Cookie: %q", gotCookie)
	}
}
