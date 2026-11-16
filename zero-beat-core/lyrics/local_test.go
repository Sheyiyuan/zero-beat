package lyrics

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// mustUTF16 按指定字节序编码字符串（不含 BOM），用例里自己拼 BOM 以覆盖两种顺序。
func mustUTF16(t *testing.T, s string, order binary.ByteOrder) []byte {
	t.Helper()
	units := utf16.Encode([]rune(s))
	buf := make([]byte, 2*len(units))
	for i, u := range units {
		order.PutUint16(buf[i*2:], u)
	}
	return buf
}

// mustGB18030 用 x/text 现编 GB18030 字节，避免在测试里硬编码一长串十六进制。
func mustGB18030(t *testing.T, s string) []byte {
	t.Helper()
	b, _, err := transform.Bytes(simplifiedchinese.GB18030.NewEncoder(), []byte(s))
	if err != nil {
		t.Fatalf("构造 GB18030 测试数据失败: %v", err)
	}
	return b
}

func TestDecodeText(t *testing.T) {
	text := "[ti:测试]\n[00:01.00]第一行\r\n[00:02.00]第二行\r[00:03.00]第三行"
	want := "[ti:测试]\n[00:01.00]第一行\n[00:02.00]第二行\n[00:03.00]第三行"

	cases := []struct {
		name string
		data []byte
	}{
		{"UTF-8 BOM", append([]byte{0xEF, 0xBB, 0xBF}, []byte(text)...)},
		{"UTF-16 LE BOM", append([]byte{0xFF, 0xFE}, mustUTF16(t, text, binary.LittleEndian)...)},
		{"UTF-16 BE BOM", append([]byte{0xFE, 0xFF}, mustUTF16(t, text, binary.BigEndian)...)},
		{"GB18030", mustGB18030(t, text)},
		{"UTF-8", []byte(text)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := DecodeText(c.data)
			if err != nil {
				t.Fatalf("DecodeText 返回错误: %v", err)
			}
			if got != want {
				t.Fatalf("解码结果不对:\n got=%q\nwant=%q", got, want)
			}
		})
	}
}

func TestDecodeTextStripsNUL(t *testing.T) {
	got, err := DecodeText([]byte("\x00[00:01.00]第一行\x00\r\n\x00[00:02.00]第二行\x00"))
	if err != nil {
		t.Fatalf("DecodeText 返回错误: %v", err)
	}
	if strings.ContainsRune(got, '\x00') {
		t.Fatalf("NUL 未被清除: %q", got)
	}
	if got != "[00:01.00]第一行\n[00:02.00]第二行" {
		t.Fatalf("清洗结果不对: %q", got)
	}
}

func TestDecodeTextBadGB18030(t *testing.T) {
	// 这串字节既不是合法 UTF-8，也不是合法 GB18030（0xFF 不是合法首字节）。
	_, err := DecodeText([]byte{0x81, 0x20, 0xFF, 0x00})
	if err == nil {
		t.Fatal("非法字节应返回错误")
	}
	if !strings.Contains(err.Error(), "无法解码歌词文件") {
		t.Fatalf("错误信息不对: %v", err)
	}
}

func TestParseLocalLRCTimestamps(t *testing.T) {
	text := strings.Join([]string{
		"[00:01.20]第一行",
		"[00:03.500]第二行",
		"[00:05]第三行",
		"[02:00.00][01:30.00]重复行",
		"[1234:56.78]超长分钟",
	}, "\n")
	lines := ParseLocalLRC(text)
	want := []Line{
		{StartMS: 1200, Text: "第一行"},
		{StartMS: 3500, Text: "第二行"},
		{StartMS: 5000, Text: "第三行"},
		{StartMS: 90000, Text: "重复行"},
		{StartMS: 120000, Text: "重复行"},
		{StartMS: 74096780, Text: "超长分钟"},
	}
	if len(lines) != len(want) {
		t.Fatalf("期望 %d 行，实际 %d：%+v", len(want), len(lines), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("第 %d 行不对:\n got=%+v\nwant=%+v", i, lines[i], want[i])
		}
	}
}

func TestParseLocalLRCOffset(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []int
	}{
		{
			name: "正值延后",
			text: "[offset:+500]\n[00:01.00]第一行\n[00:02.00]第二行",
			want: []int{1500, 2500},
		},
		{
			name: "负值提前",
			text: "[00:01.00]第一行\n[offset:-500]\n[00:02.00]第二行",
			want: []int{500, 1500},
		},
		{
			name: "无符号与空格",
			text: "[offset: 250]\n[00:01.00]第一行",
			want: []int{1250},
		},
		{
			name: "写坏的标签忽略",
			text: "[offset:abc]\n[00:01.00]第一行\n[offset:]\n[offset:+/-]\n[00:02.00]第二行",
			want: []int{1000, 2000},
		},
		{
			name: "多个 offset 以最后一个为准",
			text: "[offset:+500]\n[00:01.00]第一行\n[offset:-200]",
			want: []int{800},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines := ParseLocalLRC(c.text)
			if len(lines) != len(c.want) {
				t.Fatalf("期望 %d 行，实际 %d：%+v", len(c.want), len(lines), lines)
			}
			for i, w := range c.want {
				if lines[i].StartMS != w {
					t.Fatalf("第 %d 行时间戳 = %d，期望 %d（%+v）", i, lines[i].StartMS, w, lines)
				}
			}
		})
	}
}

func TestParseLocalLRCEnhancedTags(t *testing.T) {
	text := "[00:10.00]<00:10.00>Hel<00:10.50>lo <00:11.00>world\n" +
		"[00:12.00]<00:12.00>\n" +
		"[00:13.00]  <00:13.00>只有标签  "
	lines := ParseLocalLRC(text)
	if len(lines) != 2 {
		t.Fatalf("期望 2 行（只有逐字标签的行应丢弃），实际 %d：%+v", len(lines), lines)
	}
	if lines[0].StartMS != 10000 || lines[0].Text != "Hello world" {
		t.Fatalf("逐字标签未清理干净: %+v", lines[0])
	}
	if lines[1].StartMS != 13000 || lines[1].Text != "只有标签" {
		t.Fatalf("首尾空格未收紧: %+v", lines[1])
	}
}

func TestParseLocalLRCMeta(t *testing.T) {
	text := strings.Join([]string{
		"[ti:  夜曲  ]",
		"[ar: 周杰伦 ]",
		"[al:十一月的萧邦]",
		"[by:someone]",
		"[length:04:00]",
		"[chorus]",
		"[00:01.00]第一行",
		"[00:02.00]第二行",
	}, "\n")

	lines := ParseLocalLRC(text)
	if len(lines) != 2 {
		t.Fatalf("元数据不应进入正文，实际 %d 行：%+v", len(lines), lines)
	}
	if lines[0].Text != "第一行" || lines[1].Text != "第二行" {
		t.Fatalf("正文解析错误: %+v", lines)
	}

	title, artist := ParseLocalLRCMeta(text)
	if title != "夜曲" {
		t.Fatalf("ti = %q，期望 夜曲", title)
	}
	if artist != "周杰伦" {
		t.Fatalf("ar = %q，期望 周杰伦", artist)
	}

	exLines, exTitle, exArtist := ParseLocalLRCEx(text)
	if exTitle != title || exArtist != artist || len(exLines) != len(lines) {
		t.Fatalf("ParseLocalLRCEx 与独立调用不一致: %+v %q %q", exLines, exTitle, exArtist)
	}
}

func TestParseLocalLRCBadTimestamps(t *testing.T) {
	text := strings.Join([]string{
		"[00:xx]坏行",
		"[99:99.9999]小数位过长",
		"[abc]没有时间",
		"[]空标签",
		"[99999999999999999999:00]分钟溢出",
		"[00:01.00]好行",
		"[12:34]好行2",
	}, "\n")
	lines := ParseLocalLRC(text)
	if len(lines) != 2 {
		t.Fatalf("期望只保留 2 行，实际 %d：%+v", len(lines), lines)
	}
	if lines[0].StartMS != 1000 || lines[0].Text != "好行" {
		t.Fatalf("第一行不对: %+v", lines[0])
	}
	if lines[1].StartMS != 754000 || lines[1].Text != "好行2" {
		t.Fatalf("第二行不对: %+v", lines[1])
	}
}

func TestMergeSameTimestamp(t *testing.T) {
	cases := []struct {
		name string
		in   []Line
		want []Line
	}{
		{
			name: "译文在后",
			in: []Line{
				{StartMS: 0, Text: "原文"},
				{StartMS: 0, Text: "译文"},
			},
			want: []Line{{StartMS: 0, Text: "原文", Translation: "译文"}},
		},
		{
			name: "译文在前",
			in: []Line{
				{StartMS: 0, Text: "译文"},
				{StartMS: 0, Text: "原文"},
			},
			want: []Line{{StartMS: 0, Text: "译文", Translation: "原文"}},
		},
		{
			name: "重复原文不当译文",
			in: []Line{
				{StartMS: 1000, Text: "同一句"},
				{StartMS: 1000, Text: "同一句"},
				{StartMS: 1000, Text: "译文"},
				{StartMS: 1000, Text: "第二句译文"},
			},
			want: []Line{{StartMS: 1000, Text: "同一句", Translation: "译文"}},
		},
		{
			name: "乱序输入按时间排序且唯一时间戳原样保留",
			in: []Line{
				{StartMS: 2000, Text: "后"},
				{StartMS: 1000, Text: "前"},
				{StartMS: 2000, Text: "后译"},
				{StartMS: 3000, Text: "独行", Translation: "已有译文"},
			},
			want: []Line{
				{StartMS: 1000, Text: "前"},
				{StartMS: 2000, Text: "后", Translation: "后译"},
				{StartMS: 3000, Text: "独行", Translation: "已有译文"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MergeSameTimestamp(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("期望 %d 行，实际 %d：%+v", len(c.want), len(got), got)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("第 %d 行不对:\n got=%+v\nwant=%+v", i, got[i], c.want[i])
				}
			}
		})
	}
	if got := MergeSameTimestamp(nil); got != nil {
		t.Fatalf("空输入应返回 nil，实际 %+v", got)
	}
}

func TestMergeSidecar(t *testing.T) {
	primary := []Line{
		{StartMS: 1000, Text: "a", Translation: "旧译"},
		{StartMS: 2000, Text: "b"},
		{StartMS: 3000, Text: "c", Translation: "保留"},
	}
	sidecar := []Line{
		{StartMS: 1000, Text: "A 译"},
		{StartMS: 2000, Text: "B 译"},
		{StartMS: 4000, Text: "多出来的"},
	}
	got := MergeSidecar(primary, sidecar)
	if len(got) != 3 {
		t.Fatalf("只应以 primary 为骨架，实际 %+v", got)
	}
	want := []Line{
		{StartMS: 1000, Text: "a", Translation: "A 译"},
		{StartMS: 2000, Text: "b", Translation: "B 译"},
		{StartMS: 3000, Text: "c", Translation: "保留"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 行不对:\n got=%+v\nwant=%+v", i, got[i], want[i])
		}
	}
	// 纯函数：不能改到入参，否则播放列表里缓存的原始歌词会被污染。
	if primary[0].Translation != "旧译" {
		t.Fatalf("MergeSidecar 不应修改 primary: %+v", primary[0])
	}
	if got := MergeSidecar(nil, sidecar); got != nil {
		t.Fatalf("空 primary 应返回 nil，实际 %+v", got)
	}
}

func TestLoadLocalFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("写测试文件失败: %v", err)
		}
		return p
	}

	t.Run("正常文件", func(t *testing.T) {
		p := write("ok.lrc", "[ti:歌名]\n[00:01.00]第一行\n[00:02.00]第二行\n")
		lines, err := LoadLocalFile(p, 0)
		if err != nil {
			t.Fatalf("LoadLocalFile 返回错误: %v", err)
		}
		if len(lines) != 2 || lines[0].StartMS != 1000 || lines[1].Text != "第二行" {
			t.Fatalf("解析结果不对: %+v", lines)
		}
	})

	t.Run("GB18030 文件", func(t *testing.T) {
		p := filepath.Join(dir, "gb.lrc")
		if err := os.WriteFile(p, mustGB18030(t, "[00:01.00]中文歌词"), 0o600); err != nil {
			t.Fatalf("写测试文件失败: %v", err)
		}
		lines, err := LoadLocalFile(p, 0)
		if err != nil {
			t.Fatalf("LoadLocalFile 返回错误: %v", err)
		}
		if len(lines) != 1 || lines[0].Text != "中文歌词" {
			t.Fatalf("GB18030 解析结果不对: %+v", lines)
		}
	})

	t.Run("空文件", func(t *testing.T) {
		p := write("empty.lrc", "")
		if _, err := LoadLocalFile(p, 0); err == nil || !strings.Contains(err.Error(), "歌词文件为空") {
			t.Fatalf("空文件错误信息不对: %v", err)
		}
	})

	t.Run("只有空白", func(t *testing.T) {
		p := write("blank.lrc", " \n\t\r\n  ")
		if _, err := LoadLocalFile(p, 0); err == nil || !strings.Contains(err.Error(), "歌词文件为空") {
			t.Fatalf("空白文件错误信息不对: %v", err)
		}
	})

	t.Run("只有元数据", func(t *testing.T) {
		p := write("meta.lrc", "[ti:只有标题]\n[ar:歌手]\n")
		if _, err := LoadLocalFile(p, 0); err == nil {
			t.Fatal("没有正文的歌词文件应报错")
		}
	})

	t.Run("超过大小限制", func(t *testing.T) {
		p := write("big.lrc", "[00:01.00]第一行\n[00:02.00]第二行\n")
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat 失败: %v", err)
		}
		if _, err := LoadLocalFile(p, info.Size()-1); err == nil || !strings.Contains(err.Error(), "大小限制") {
			t.Fatalf("超限错误信息不对: %v", err)
		}
		if _, err := LoadLocalFile(p, info.Size()); err != nil {
			t.Fatalf("刚好等于上限时不应报错: %v", err)
		}
	})

	t.Run("文件不存在", func(t *testing.T) {
		if _, err := LoadLocalFile(filepath.Join(dir, "nope.lrc"), 0); err == nil {
			t.Fatal("不存在的文件应报错")
		}
	})
}
