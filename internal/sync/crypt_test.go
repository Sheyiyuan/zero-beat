package sync

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testPassphrase() string { return "correct horse battery staple" }

func TestDeriveStable(t *testing.T) {
	salt, err := GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}
	kdf := DefaultKDF()
	a, err := kdf.Derive(testPassphrase(), salt)
	if err != nil {
		t.Fatal(err)
	}
	b, err := kdf.Derive(testPassphrase(), salt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("同一口令与盐应派生出相同密钥")
	}
	if len(a) != KeySize {
		t.Fatalf("密钥长度应为 %d，实际 %d", KeySize, len(a))
	}
	other, _ := GenerateSalt()
	c, _ := kdf.Derive(testPassphrase(), other)
	if bytes.Equal(a, c) {
		t.Fatal("不同盐应派生出不同密钥")
	}
}

func TestKDFParamsWhitelist(t *testing.T) {
	params := DefaultKDF()
	params.M = 1 << 30
	if err := params.Validate(); !errors.Is(err, ErrSchema) {
		t.Fatalf("超大 m 应被拒绝，实际 %v", err)
	}
	params = DefaultKDF()
	params.P = 2
	if err := params.Validate(); err == nil {
		t.Fatal("非白名单 p 应被拒绝")
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{7}, KeySize)
	plain := []byte(`{"source":"bilibili","updated_at":1,"session":{"SESSDATA":"x"}}`)
	first, err := Seal(key, "bilibili", 1, plain)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Seal(key, "bilibili", 1, plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("每次加密应使用新的 nonce，密文不应相同")
	}
	out, err := Open(key, "bilibili", 1, first)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, plain) {
		t.Fatalf("往返失败: %s", out)
	}
}

func TestOpenRejectsWrongKeyAndTamper(t *testing.T) {
	key := bytes.Repeat([]byte{1}, KeySize)
	data, err := Seal(key, "netease", 1, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	wrong := bytes.Repeat([]byte{2}, KeySize)
	if _, err := Open(wrong, "netease", 1, data); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("错误密钥应返回 ErrKeyMismatch，实际 %v", err)
	}
	// 改动密文最后一个字节（base64 解码后的 tag）。
	envelope, _ := decodeEnvelope(data)
	tampered, err := Seal(key, "netease", 1, []byte("other"))
	if err != nil {
		t.Fatal(err)
	}
	env2, _ := decodeEnvelope(tampered)
	envelope.Data = env2.Data
	raw, _ := encodeJSON(envelope)
	if _, err := Open(key, "netease", 1, raw); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("篡改密文应认证失败，实际 %v", err)
	}
	if _, err := Open(key, "bilibili", 1, data); err == nil {
		t.Fatal("AAD 绑定音源，换源应失败")
	}
	if _, err := Open(key, "netease", 2, data); err == nil {
		t.Fatal("AAD 绑定密钥版本，换版本应失败")
	}
}

func TestKeycheckIndependent(t *testing.T) {
	key := bytes.Repeat([]byte{9}, KeySize)
	sum := Keycheck(key)
	if len(sum) != KeycheckSize {
		t.Fatalf("keycheck 长度应为 %d", KeycheckSize)
	}
	if !bytes.Equal(sum, Keycheck(key)) {
		t.Fatal("keycheck 应稳定")
	}
	other := bytes.Repeat([]byte{8}, KeySize)
	if bytes.Equal(sum, Keycheck(other)) {
		t.Fatal("不同密钥的 keycheck 不应相同")
	}
}

func TestMetaLockUnlock(t *testing.T) {
	now := time.Unix(1761980000, 0)
	meta, key, err := NewMeta(testPassphrase(), now)
	if err != nil {
		t.Fatal(err)
	}
	if meta.V != MetaVersion || meta.KeyVersion != 1 {
		t.Fatalf("meta 版本不符: %+v", meta)
	}
	data, err := meta.Encode()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := DecodeMeta(data)
	if err != nil {
		t.Fatal(err)
	}
	unlocked, err := parsed.Unlock(testPassphrase())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unlocked.Key, key.Key) {
		t.Fatal("解出的密钥应与创建时一致")
	}
	if _, err := parsed.Unlock("wrong passphrase"); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("错误口令应返回 ErrKeyMismatch，实际 %v", err)
	}
	if _, _, err := NewMeta("short", now); err == nil {
		t.Fatal("过短口令应被拒绝")
	}
}

func TestMetaStrictDecode(t *testing.T) {
	now := time.Now()
	meta, _, err := NewMeta(testPassphrase(), now)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := meta.Encode()
	var expectFail = [][]byte{
		bytes.Replace(data, []byte(`"v": 1`), []byte(`"v": 2`), 1),
		[]byte(`{"v":1}`),
		bytes.Replace(data, []byte(`"salt": "`), []byte(`"extra": 1, "salt": "`), 1),
	}
	for i, bad := range expectFail {
		if _, err := DecodeMeta(bad); err == nil {
			t.Fatalf("第 %d 个非法 meta 应被拒绝", i)
		}
	}
}

func TestKeyFileRoundTripAndMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync-key")
	meta, key, err := NewMeta(testPassphrase(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_ = meta
	key.RemoteHash = RemoteHash("https://example.com/repo.git")
	if err := key.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("sync-key 权限应为 0600，实际 %o", info.Mode().Perm())
	}
	loaded, err := LoadKeyState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded.Key, key.Key) || loaded.RemoteHash != key.RemoteHash {
		t.Fatal("sync-key 往返不一致")
	}
}

// decodeEnvelope 解析信封；仅供测试构造篡改样例。
func decodeEnvelope(data []byte) (Envelope, error) {
	var envelope Envelope
	err := decodeJSONStrict(data, &envelope)
	return envelope, err
}
