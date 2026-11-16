package bili

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMixinKeyIsPermutationOfKeys(t *testing.T) {
	img := "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png"
	sub := "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png"
	key := mixinKey(img, sub)
	if len(key) != 32 {
		t.Fatalf("mixin key 长度应为 32，实际 %d (%q)", len(key), key)
	}
	raw := "7cd084941338484aae1ad9425b84077c4932caff0ff746eab6f01bf08b70ac45"
	for _, r := range key {
		if !strings.ContainsRune(raw, r) {
			t.Fatalf("key 里出现了原始字符集之外的字符 %q", r)
		}
	}
	// 确定性：同一输入必须得到同一结果
	if again := mixinKey(img, sub); again != key {
		t.Fatal("mixinKey 必须确定性")
	}
}

func TestMixinKeyRejectsShortInput(t *testing.T) {
	if got := mixinKey("https://x/a.png", "https://x/b.png"); got != "" {
		t.Fatalf("素材过短时应返回空，实际 %q", got)
	}
}

func TestSignParams(t *testing.T) {
	params := url.Values{
		"keyword":     {"周杰伦 (Live)"},
		"search_type": {"video"},
	}
	key := "0123456789abcdef0123456789abcdef"
	now := time.Unix(1700000000, 0)
	signed := signParams(params, key, now)

	if signed.Get("wts") != "1700000000" {
		t.Fatalf("wts 未写入: %q", signed.Get("wts"))
	}
	if got := signed.Get("w_rid"); len(got) != 32 {
		t.Fatalf("w_rid 应为 32 位 md5，实际 %q", got)
	}
	// 同样的参数必须得到同样的签名
	again := signParams(url.Values{
		"keyword":     {"周杰伦 (Live)"},
		"search_type": {"video"},
	}, key, now)
	if again.Get("w_rid") != signed.Get("w_rid") {
		t.Fatal("签名必须确定性")
	}
}

func TestFilterValue(t *testing.T) {
	if got := filterValue("a!b'c(d)e*f"); got != "abcdef" {
		t.Fatalf("过滤结果错误: %q", got)
	}
}
