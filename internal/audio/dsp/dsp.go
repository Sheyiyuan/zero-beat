// Package dsp 提供与音源无关的音频处理原语。
//
// 音量补偿的语义沿用桌面版：dB 补偿叠加在用户音量之上，
// 超过 0 dBFS 时用软限幅避免削波。
package dsp

import "math"

// clipCeiling 是软限幅的输出上限（32767/32768）。
//
// 不用 ±1 是因为 float32 精度只有约 7 位十进制，tanh(-10) 转成 float32
// 会恰好舍入到 -1；留一点余量既避免 int16 转换溢出，也让「未削波」可判定。
const clipCeiling = 32767.0 / 32768.0

// DBToGain 把分贝转换为线性增益。
func DBToGain(db float64) float64 {
	return math.Pow(10, db/20)
}

// SoftClip 是 tanh 软限幅，用于补偿后的防削波。
// 输出范围是 [-clipCeiling, clipCeiling]，不会顶到满刻度。
func SoftClip(sample float32) float32 {
	v := float32(math.Tanh(float64(sample)))
	switch {
	case v > clipCeiling:
		return clipCeiling
	case v < -clipCeiling:
		return -clipCeiling
	default:
		return v
	}
}
