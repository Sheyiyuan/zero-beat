package lyrics

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// decodeFailMsg 只给用户看，不必区分具体是哪种编码坏了；调用方也只需要知道“这个文件用不了”。
const decodeFailMsg = "lyrics: 无法解码歌词文件"

var (
	// 分钟位宽不设上限：部分播放器导出的长音频歌词会出现 [1234:56.78] 这类时间戳。
	localTimeTag = regexp.MustCompile(`\[(\d+):(\d{1,2})(?:[.:](\d{1,3}))?\]`)

	// 增强 LRC 的逐字时间标签只负责卡位；留在正文里会显示成 <00:12.00> 这样的乱码。
	localWordTag = regexp.MustCompile(`<\d+:\d{1,2}(?:[.:]\d{1,3})?>`)

	// 方括号里剩下的标签（ti/ar/al/by/offset/chorus…）都不是歌词正文，统一按元数据丢掉。
	localAnyTag = regexp.MustCompile(`\[[^\]]*\]`)

	// 键名大小写混用的情况存在（[TI:]/[Ar:]），统一按小写比较。
	localMetaTag = regexp.MustCompile(`(?i)\[([a-z]+):([^\]]*)\]`)

	// 只接受带符号或纯数字的 offset；写坏的标签按不存在处理，不能让它污染整份歌词。
	localOffsetTag = regexp.MustCompile(`(?i)\[offset:\s*([+-]?\d+)\s*\]`)
)

// DecodeText 把本地歌词文件的原始字节转成 UTF-8 文本。
//
// 歌词文件来源杂乱（Windows 记事本、旧版播放器导出），没法假定统一编码：
// 先按 BOM 判定，再按 UTF-8 校验，最后才退回 GB18030。
func DecodeText(data []byte) (string, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	// UTF-16 的 BOM 必须先判断：它的字节几乎必然不是合法 UTF-8，先跑 utf8.Valid 会误判成 GB18030。
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return normalizeText(decodeUTF16(data[2:], binary.LittleEndian)), nil
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return normalizeText(decodeUTF16(data[2:], binary.BigEndian)), nil
	}
	if utf8.Valid(data) {
		return normalizeText(string(data)), nil
	}
	decoded, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), data)
	// GB18030 解码器遇到非法字节不会返回错误，只会吐 U+FFFD，所以失败要自己判。
	if err != nil || bytes.ContainsRune(decoded, utf8.RuneError) {
		return "", errors.New(decodeFailMsg)
	}
	return normalizeText(string(decoded)), nil
}

// LoadLocalFile 读取并解析本地 .lrc 文件。
//
// maxBytes 给调用方一个自我保护的上限，避免用户误选超大文本文件把界面卡住；
// 传 0 或负数表示不限制。
func LoadLocalFile(path string, maxBytes int64) ([]Line, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lyrics: 读取歌词文件失败: %w", err)
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("lyrics: 歌词文件超过大小限制 %d 字节", maxBytes)
	}
	text, err := DecodeText(data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("lyrics: 歌词文件为空")
	}
	lines := ParseLocalLRC(text)
	if len(lines) == 0 {
		// 只剩 [ti:]/[ar:] 之类元数据的文件同样算空，交给上层提示用户换文件。
		return nil, errors.New("lyrics: 歌词文件没有可解析的歌词行")
	}
	return lines, nil
}

// ParseLocalLRC 只取正文行，需要 [ti:]/[ar:] 时用 ParseLocalLRCEx。
func ParseLocalLRC(text string) []Line {
	lines, _, _ := ParseLocalLRCEx(text)
	return lines
}

// ParseLocalLRCMeta 读取 [ti:] 与 [ar:]，值已去掉首尾空格。
func ParseLocalLRCMeta(text string) (title, artist string) {
	_, title, artist = ParseLocalLRCEx(text)
	return title, artist
}

// ParseLocalLRCEx 一次解析出正文、标题与歌手，供文件浏览与播放两条路径共用。
//
// 分成 Ex 版本是为了让预览面板只跑一遍解析：既拿元数据，又能在真正加载时复用同一份逻辑。
func ParseLocalLRCEx(text string) (lines []Line, title, artist string) {
	var offsetMS int
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// 同一文件出现多个 offset 时以最后一个为准：导出工具通常把最终值追加在末尾。
		if tags := localOffsetTag.FindAllStringSubmatch(line, -1); len(tags) > 0 {
			if v, err := strconv.Atoi(tags[len(tags)-1][1]); err == nil {
				offsetMS = v
			}
		}
		// 首个 [ti:]/[ar:] 才是文件级信息，后面重复的通常是某个工具追加的副本。
		for _, m := range localMetaTag.FindAllStringSubmatch(line, -1) {
			switch strings.ToLower(m[1]) {
			case "ti":
				if title == "" {
					title = strings.TrimSpace(m[2])
				}
			case "ar":
				if artist == "" {
					artist = strings.TrimSpace(m[2])
				}
			}
		}
		stamps := localTimeTag.FindAllStringSubmatch(line, -1)
		if len(stamps) == 0 {
			continue
		}
		// 先摘时间戳再摘逐字标签和方括号标签，最后剩下的才算这一行的歌词文本。
		body := localTimeTag.ReplaceAllString(line, "")
		body = localWordTag.ReplaceAllString(body, "")
		body = localAnyTag.ReplaceAllString(body, "")
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		for _, m := range stamps {
			ms, ok := localTimestampMS(m)
			if !ok {
				continue
			}
			lines = append(lines, Line{StartMS: ms, Text: body})
		}
	}
	// 正值表示歌词整体延后出现，所以直接加；播放器里“歌词偏移 +N”的语义与 LRC 的 offset 相反，
	// 用户的个人偏移由上层再用 Clone 叠加，这里只负责还原文件自带的偏移。
	if offsetMS != 0 {
		for i := range lines {
			lines[i].StartMS += offsetMS
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].StartMS < lines[j].StartMS })
	return lines, title, artist
}

// MergeSameTimestamp 把同一时间戳的多行收成一行：首行当原文，后面第一句不同的文本当译文。
//
// 双语歌词文件两种顺序都有（原文在前或译文在前），谁排第一谁就是原文，
// 这样无论导出顺序如何，面板都能按“原文 + 译文”的结构展示。
func MergeSameTimestamp(lines []Line) []Line {
	if len(lines) == 0 {
		return nil
	}
	out := make([]Line, 0, len(lines))
	at := make(map[int]int, len(lines)) // StartMS → out 下标，省掉单独的分组切片
	for _, l := range lines {
		if i, ok := at[l.StartMS]; ok {
			main := &out[i]
			// 不少文件把同一句原文复制两份；它既不能当译文，也不该重复出现。
			if l.Text == "" || l.Text == main.Text {
				continue
			}
			// 已经有译文说明更早的行先到（或输入本身就带了译文），后来的不覆盖。
			if main.Translation == "" {
				main.Translation = l.Text
			}
			continue
		}
		at[l.StartMS] = len(out)
		out = append(out, l)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartMS < out[j].StartMS })
	return out
}

// MergeSidecar 用同名译文文件补译文，时间戳相同即视为同一句。
//
// 只以 primary 为骨架：译文文件里多出来的时间戳无法判断该挂在哪句上，
// 强行按“最接近时间”配对会在间奏处错位，所以直接忽略。
func MergeSidecar(primary, sidecar []Line) []Line {
	if len(primary) == 0 {
		return nil
	}
	// 译文文件也可能带双语，同一时间戳取第一条非空文本；空文本不能用来清掉已有译文。
	byTime := make(map[int]string, len(sidecar))
	for _, l := range sidecar {
		if l.Text == "" {
			continue
		}
		if _, ok := byTime[l.StartMS]; !ok {
			byTime[l.StartMS] = l.Text
		}
	}
	out := make([]Line, len(primary))
	copy(out, primary)
	for i := range out {
		if t, ok := byTime[out[i].StartMS]; ok {
			// 专门的译文文件比歌词里自带的 Translation 更可信，允许覆盖。
			out[i].Translation = t
		}
	}
	return out
}

// decodeUTF16 把去掉 BOM 的 UTF-16 字节解码成字符串。
//
// 末尾被截断的半个码元直接丢弃，否则最后一个字符会变成替换符，用户会以为文件坏了。
func decodeUTF16(data []byte, order binary.ByteOrder) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = order.Uint16(data[i*2:])
	}
	return string(utf16.Decode(units))
}

// normalizeText 统一换行并清掉 NUL：解析器只按 \n 切行，
// 而 Windows 导出与 UTF-16 文件常见 \r\n、单独的 \r 以及填充的 \x00。
func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// localTimestampMS 把时间戳各段换算成毫秒；数字溢出或非法时返回 false，让调用方丢掉这个时间戳。
func localTimestampMS(m []string) (int, bool) {
	min, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	sec, err := strconv.Atoi(m[2])
	if err != nil {
		return 0, false
	}
	frac := m[3]
	if frac == "" {
		return (min*60 + sec) * 1000, true
	}
	// LRC 的小数位按 1/100 秒解释（"5" 表示 500ms），补齐到三位再算。
	switch len(frac) {
	case 1:
		frac += "00"
	case 2:
		frac += "0"
	}
	ms, err := strconv.Atoi(frac)
	if err != nil {
		return 0, false
	}
	return (min*60+sec)*1000 + ms, true
}
