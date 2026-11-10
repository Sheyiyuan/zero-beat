package library

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// probeDuration 从文件头/尾读出时长（秒），读不出来返回 0。
//
// 扫描阶段不 spawn ffprobe：大目录逐文件起进程太贵。常见容器（FLAC/MP4/MP3/
// OGG/WAV）的头里就有时长，剩下的格式留给首次播放时探测。
func probeDuration(path string) int {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		return flacDuration(path)
	case ".m4a":
		return mp4Duration(path)
	case ".mp3":
		return mp3Duration(path)
	case ".ogg", ".opus":
		return oggDuration(path)
	case ".wav":
		return wavDuration(path)
	}
	return 0
}

func openHead(path string) (*os.File, error) { return os.Open(path) }

// flacDuration 读 STREAMINFO：采样率 20 bit、总采样数 36 bit。
func flacDuration(path string) int {
	f, err := openHead(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	header := make([]byte, 4)
	if _, err := io.ReadFull(f, header); err != nil || string(header) != "fLaC" {
		return 0
	}
	// 第一个 metadata block 必须是 STREAMINFO（类型 0），长度固定 34 字节。
	block := make([]byte, 4)
	if _, err := io.ReadFull(f, block); err != nil {
		return 0
	}
	if block[0]&0x7f != 0 {
		return 0
	}
	data := make([]byte, 34)
	if _, err := io.ReadFull(f, data); err != nil {
		return 0
	}
	// 采样率：字节 10-12 的高 20 位；总采样数：低 36 位（字节 13 低 4 位起）。
	rate := uint32(data[10])<<12 | uint32(data[11])<<4 | uint32(data[12])>>4
	total := uint64(data[13]&0x0f)<<32 | uint64(data[14])<<24 | uint64(data[15])<<16 |
		uint64(data[16])<<8 | uint64(data[17])
	if rate == 0 || total == 0 {
		return 0
	}
	return int(total / uint64(rate))
}

// mp4Duration 读 moov/mvhd：duration / timescale。mvhd 可能在文件尾部，
// 所以只做一次全文件顺序扫描（限制在合理大小的头部与 moov 内）。
func mp4Duration(path string) int {
	f, err := openHead(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0
	}
	// mvhd 是 moov 的第一个子块；多数文件 moov 在前 8 MiB 内。
	limit := info.Size()
	if limit > 16<<20 {
		limit = 16 << 20
	}
	data := make([]byte, limit)
	n, _ := io.ReadFull(f, data)
	data = data[:n]
	idx := indexOf(data, []byte("mvhd"))
	if idx < 0 || idx+20 > len(data) {
		return 0
	}
	version := data[idx+4]
	if version == 1 {
		if idx+32 > len(data) {
			return 0
		}
		timescale := binary.BigEndian.Uint32(data[idx+24:])
		duration := binary.BigEndian.Uint64(data[idx+28:])
		if timescale == 0 {
			return 0
		}
		return int(duration / uint64(timescale))
	}
	timescale := binary.BigEndian.Uint32(data[idx+16:])
	duration := binary.BigEndian.Uint32(data[idx+20:])
	if timescale == 0 {
		return 0
	}
	return int(uint64(duration) / uint64(timescale))
}

func indexOf(data, sub []byte) int {
	for i := 0; i+len(sub) <= len(data); i++ {
		match := true
		for j := range sub {
			if data[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// mp3Duration 优先用 Xing/Info 头里的帧数，其次按 VBR 头或比特率估算。
func mp3Duration(path string) int {
	f, err := openHead(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	head := make([]byte, 128<<10)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	start := 0
	// 跳过 ID3v2 头（含 footer）。
	if len(head) > 10 && string(head[:3]) == "ID3" {
		size := int(head[6]&0x7f)<<21 | int(head[7]&0x7f)<<14 | int(head[8]&0x7f)<<7 | int(head[9]&0x7f)
		start = 10 + size
		if head[5]&0x10 != 0 {
			start += 10
		}
	}
	if start >= len(head) {
		return 0
	}
	frame := findMP3Frame(head[start:])
	if frame < 0 {
		return 0
	}
	frameData := head[start+frame:]
	// Xing/Info 标签在帧头之后，偏移量取决于 MPEG 版本与声道模式。
	for _, marker := range [][]byte{[]byte("Xing"), []byte("Info")} {
		if idx := indexOf(frameData[:minInt(len(frameData), 200)], marker); idx >= 0 {
			pos := idx + 4
			if pos+8 > len(frameData) {
				return 0
			}
			flags := binary.BigEndian.Uint32(frameData[pos:])
			pos += 4
			if flags&0x1 == 0 {
				return 0
			}
			frames := binary.BigEndian.Uint32(frameData[pos:])
			sampleRate := mp3SampleRate(frameData)
			if sampleRate == 0 {
				return 0
			}
			// MPEG-1 每帧 1152 采样，MPEG-2/2.5 为 576。
			samplesPerFrame := 1152
			if frameData[1]&0x18 == 0x10 || frameData[1]&0x18 == 0x00 {
				samplesPerFrame = 576
			}
			return int(uint64(frames) * uint64(samplesPerFrame) / uint64(sampleRate))
		}
	}
	return 0
}

// findMP3Frame 返回第一个同步字的偏移；跳过不合法帧头。
func findMP3Frame(data []byte) int {
	for i := 0; i+4 <= len(data); i++ {
		if data[i] != 0xff || data[i+1]&0xe0 != 0xe0 {
			continue
		}
		// 版本 01、层 00、比特率 0000、采样率 11 都是非法组合。
		if data[i+1]&0x18 == 0x08 || data[i+1]&0x06 == 0x00 ||
			data[i+2]&0xf0 == 0xf0 || data[i+2]&0x0c == 0x0c {
			continue
		}
		return i
	}
	return -1
}

// mp3SampleRate 根据帧头版本/采样率索引取采样率。
func mp3SampleRate(frame []byte) int {
	if len(frame) < 3 {
		return 0
	}
	var table [3][3]int
	switch frame[1] & 0x18 {
	case 0x18: // MPEG-1
		table = [3][3]int{{44100, 48000, 32000}}
	case 0x10: // MPEG-2
		table = [3][3]int{{22050, 24000, 16000}}
	case 0x00: // MPEG-2.5
		table = [3][3]int{{11025, 12000, 8000}}
	default:
		return 0
	}
	idx := int(frame[2]>>2) & 0x3
	if idx > 2 {
		return 0
	}
	return table[0][idx]
}

// oggDuration 读最后一个页面的 granule position：总采样数 / 采样率。
// 采样率从 ident header（第一个 Vorbis/Opus 包）里取。
func oggDuration(path string) int {
	f, err := openHead(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() < 64 {
		return 0
	}
	head := make([]byte, 64<<10)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	rate := oggSampleRate(head, filepath.Ext(path))
	tailSize := int64(64 << 10)
	if info.Size() < tailSize {
		tailSize = info.Size()
	}
	tail := make([]byte, tailSize)
	if _, err := f.ReadAt(tail, info.Size()-tailSize); err != nil {
		return 0
	}
	granule := lastOggGranule(tail)
	if granule == 0 || rate == 0 {
		return 0
	}
	return int(granule / uint64(rate))
}

// oggSampleRate 从第一个 ogg 页面的 ident 包读采样率。
func oggSampleRate(data []byte, ext string) int {
	if strings.EqualFold(ext, ".opus") {
		// Opus 固定 48 kHz，granule 按 48 kHz 计。
		return 48000
	}
	idx := indexOf(data, []byte("\x01vorbis"))
	if idx < 0 || idx+16 > len(data) {
		return 0
	}
	return int(binary.LittleEndian.Uint32(data[idx+12:]))
}

// lastOggGranule 在尾部数据里从后往前找最后一个完整页面的 granule。
func lastOggGranule(tail []byte) uint64 {
	for i := len(tail) - 27; i >= 0; i-- {
		if tail[i] != 'O' || tail[i+1] != 'g' || tail[i+2] != 'g' || tail[i+3] != 'S' {
			continue
		}
		granule := binary.LittleEndian.Uint64(tail[i+6:])
		if granule != ^uint64(0) {
			return granule
		}
	}
	return 0
}

// wavDuration 用 fmt 的字节率与 data 块大小计算时长。
func wavDuration(path string) int {
	f, err := openHead(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	head := make([]byte, 64<<10)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	if len(head) < 12 || string(head[:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return 0
	}
	var byteRate uint32
	var dataSize uint32
	for pos := 12; pos+8 <= len(head); {
		id := string(head[pos : pos+4])
		size := binary.LittleEndian.Uint32(head[pos+4:])
		body := pos + 8
		switch id {
		case "fmt ":
			if body+8 <= len(head) {
				byteRate = binary.LittleEndian.Uint32(head[body+8:])
			}
		case "data":
			dataSize = size
			if byteRate > 0 {
				return int(uint64(dataSize) / uint64(byteRate))
			}
			return 0
		}
		pos = body + int(size) + int(size&1)
	}
	return 0
}

// minInt 返回较小值；Go 1.21 的 min 只接受有序类型，这里显式一点更清楚。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
