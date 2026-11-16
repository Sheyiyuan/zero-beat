package dsp

import (
	"math"
	"testing"
)

func TestDBToGain(t *testing.T) {
	cases := []struct {
		db   float64
		want float64
	}{
		{0, 1},
		{6, 1.9952623149688795},
		{-6, 0.5011872336272722},
	}
	for _, c := range cases {
		got := DBToGain(c.db)
		if math.Abs(got-c.want) > 1e-9 {
			t.Fatalf("DBToGain(%v) = %v, 期望 %v", c.db, got, c.want)
		}
	}
}

func TestSoftClipBounds(t *testing.T) {
	if got := SoftClip(0); got != 0 {
		t.Fatalf("SoftClip(0) = %v, 期望 0", got)
	}
	for _, in := range []float32{-10, -1, 0.5, 1, 10} {
		if got := SoftClip(in); got <= -1 || got >= 1 {
			t.Fatalf("SoftClip(%v) = %v, 应落在 (-1,1)", in, got)
		}
	}
	// 单调递增，保证不产生失真台阶
	prev := SoftClip(-3)
	for x := -2.9; x <= 3; x += 0.1 {
		cur := SoftClip(float32(x))
		if cur <= prev {
			t.Fatalf("SoftClip 应单调递增，在 %v 处退化了", x)
		}
		prev = cur
	}
}

func TestSoftClipNeverReachesFullScale(t *testing.T) {
	// tanh 饱和后转 float32 会舍入到 ±1，必须靠上限兜住，
	// 否则换算成 int16 会溢出成反向的爆音。
	if got := SoftClip(50); got >= 1 {
		t.Fatalf("SoftClip(50) = %v，不应达到满刻度", got)
	}
	if got := SoftClip(-50); got <= -1 {
		t.Fatalf("SoftClip(-50) = %v，不应达到负满刻度", got)
	}
}
