package audio

import (
	"encoding/binary"
	"io"
	"math"
	"sync/atomic"

	"github.com/sheyiyuan/zero-beat/internal/audio/dsp"
)

// gainReader 在 PCM（交错 s16le）上应用音量与补偿，并做软限幅。
//
// 播放引擎把 ffmpeg 的 stdout 包一层，就能在不重新解码的前提下实时改音量。
type gainReader struct {
	src io.Reader

	gainBits atomic.Uint64 // math.Float64bits
	read     atomic.Int64  // 已读字节数
	eof      atomic.Bool
}

func newGainReader(src io.Reader) *gainReader {
	g := &gainReader{src: src}
	g.setGain(1)
	return g
}

func (g *gainReader) setGain(v float64) {
	if v < 0 {
		v = 0
	}
	g.gainBits.Store(math.Float64bits(v))
}

func (g *gainReader) currentGain() float64 {
	return math.Float64frombits(g.gainBits.Load())
}

// BytesRead 返回已交给声卡的 PCM 字节数。
func (g *gainReader) BytesRead() int64 { return g.read.Load() }

func (g *gainReader) Read(p []byte) (int, error) {
	n, err := g.src.Read(p)
	if n > 0 {
		g.apply(p[:n])
		g.read.Add(int64(n))
	}
	if err == io.EOF {
		g.eof.Store(true)
	}
	return n, err
}

// apply 就地放大样本；增益为 1 时完全跳过，避免无谓开销。
func (g *gainReader) apply(buf []byte) {
	gain := g.currentGain()
	if math.Abs(gain-1) < 1e-4 {
		return
	}
	for i := 0; i+1 < len(buf); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(buf[i : i+2]))
		scaled := float64(sample) / 32768.0 * gain
		out := dsp.SoftClip(float32(scaled))
		v := int16(out * 32768.0)
		binary.LittleEndian.PutUint16(buf[i:i+2], uint16(v))
	}
}
