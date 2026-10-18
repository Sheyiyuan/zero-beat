// Package dsp 提供与音源无关的音频处理原语。
//
// 音量补偿的语义沿用桌面版：dB 补偿叠加在用户音量之上，
// 超过 0 dBFS 时用软限幅避免削波。
package dsp

import "math"

// MaxGain 是合并后的增益上限，防止误配置把耳朵送走。
const MaxGain = 4.0 // +12 dB

// clipCeiling 是软限幅的输出上限（32767/32768）。
//
// 不用 ±1 是因为 float32 精度只有约 7 位十进制，tanh(-10) 转成 float32
// 会恰好舍入到 -1；留一点余量既避免 int16 转换溢出，也让"未削波"可判定。
const clipCeiling = 32767.0 / 32768.0

// DBToGain 把分贝转换为线性增益。
func DBToGain(db float64) float64 {
	return math.Pow(10, db/20)
}

// Gain 合并用户音量与补偿，并把结果夹在 [0, MaxGain]。
func Gain(volume, compensationDB float64) float64 {
	switch {
	case volume < 0:
		volume = 0
	case volume > 1:
		volume = 1
	}
	g := volume * DBToGain(compensationDB)
	if g > MaxGain {
		return MaxGain
	}
	return g
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

// FadeGain 返回切歌淡入淡出时第 i 个采样的系数。
// total <= 0 或越界时返回 1（不淡入）。
func FadeGain(i, total int) float32 {
	if total <= 0 || i >= total {
		return 1
	}
	if i <= 0 {
		return 0
	}
	return float32(i) / float32(total)
}
