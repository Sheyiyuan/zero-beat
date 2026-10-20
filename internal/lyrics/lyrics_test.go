package lyrics

import "testing"

func TestParseLRC(t *testing.T) {
	text := `[ti:测试]
[00:01.20]第一行
[00:03.500]第二行
[01:02.00][01:05.00]重复行
`
	lines := ParseLRC(text)
	if len(lines) != 4 {
		t.Fatalf("期望 4 行，实际 %d：%+v", len(lines), lines)
	}
	if lines[0].StartMS != 1200 || lines[0].Text != "第一行" {
		t.Fatalf("第一行解析错误: %+v", lines[0])
	}
	if lines[1].StartMS != 3500 {
		t.Fatalf("毫秒解析错误: %+v", lines[1])
	}
	if lines[2].StartMS != 62000 || lines[3].StartMS != 65000 {
		t.Fatalf("重复时间标签解析错误: %+v", lines[2:])
	}
}

func TestActiveIndex(t *testing.T) {
	lines := []Line{{0, "a"}, {1000, "b"}, {2500, "c"}}
	cases := []struct{ pos, want int }{
		{-1, -1}, {0, 0}, {999, 0}, {1000, 1}, {2499, 1}, {2500, 2}, {99999, 2},
	}
	for _, c := range cases {
		if got := ActiveIndex(lines, c.pos); got != c.want {
			t.Fatalf("ActiveIndex(%d) = %d，期望 %d", c.pos, got, c.want)
		}
	}
	if got := ActiveIndex(nil, 100); got != -1 {
		t.Fatalf("空歌词应返回 -1，实际 %d", got)
	}
}

func TestCloneOffset(t *testing.T) {
	lines := []Line{{1000, "a"}}
	shifted := Clone(lines, 500)
	if shifted[0].StartMS != 1500 {
		t.Fatalf("偏移未生效: %+v", shifted[0])
	}
	if lines[0].StartMS != 1000 {
		t.Fatal("Clone 不应改动原切片")
	}
}
