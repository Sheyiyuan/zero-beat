package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/sheyiyuan/zero-beat/internal/audio/dsp"
)

func int16Bytes(samples ...int16) []byte {
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}
	return buf
}

func TestGainReaderAppliesGain(t *testing.T) {
	src := bytes.NewReader(int16Bytes(1000, -1000, 2000, -2000))
	g := newGainReader(src)
	g.setGain(2)

	out := make([]byte, 8)
	if _, err := g.Read(out); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	got := []int16{
		int16(binary.LittleEndian.Uint16(out[0:2])),
		int16(binary.LittleEndian.Uint16(out[2:4])),
		int16(binary.LittleEndian.Uint16(out[4:6])),
		int16(binary.LittleEndian.Uint16(out[6:8])),
	}
	// 软限幅会把线性放大压一点，但方向与量级应当正确。
	if got[0] <= 1900 || got[0] >= 2000 {
		t.Fatalf("2 倍增益后第一个样本 = %d，期望接近 2000", got[0])
	}
	if got[1] >= -1900 {
		t.Fatalf("负样本应同比放大，实际 %d", got[1])
	}
	if g.BytesRead() != 8 {
		t.Fatalf("BytesRead = %d，期望 8", g.BytesRead())
	}
}

func TestGainReaderSkipsWhenUnity(t *testing.T) {
	raw := int16Bytes(1234, -1234)
	g := newGainReader(bytes.NewReader(raw))
	out := make([]byte, len(raw))
	if _, err := g.Read(out); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if !bytes.Equal(raw, out) {
		t.Fatalf("增益为 1 时不应改写样本: %v != %v", out, raw)
	}
}

func TestGainReaderSoftClipsFullScale(t *testing.T) {
	src := bytes.NewReader(int16Bytes(math.MaxInt16, math.MinInt16))
	g := newGainReader(src)
	g.setGain(4)
	out := make([]byte, 4)
	if _, err := g.Read(out); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	for i := 0; i < 4; i += 2 {
		v := int16(binary.LittleEndian.Uint16(out[i : i+2]))
		if v >= math.MaxInt16-1 || v <= math.MinInt16+1 {
			t.Fatalf("软限幅后不应顶到满刻度，实际 %d", v)
		}
	}
}

func TestGainReaderMarksEOF(t *testing.T) {
	g := newGainReader(bytes.NewReader(nil))
	buf := make([]byte, 4)
	if _, err := g.Read(buf); err == nil {
		t.Fatal("空输入应返回 EOF")
	}
	if !g.eof.Load() {
		t.Fatal("应标记 eof")
	}
}

func TestHeadroomMath(t *testing.T) {
	// +6dB 是 1.995 倍，与 dsp 的实现保持一致。
	if got := dbToGain(6); math.Abs(got-dsp.DBToGain(6)) > 1e-9 {
		t.Fatalf("dbToGain 应复用 dsp: %v", got)
	}
}
