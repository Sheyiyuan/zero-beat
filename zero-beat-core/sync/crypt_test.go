package sync

import (
	"bytes"
	"encoding/hex"
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
		bytes.Replace(data, []byte(`"v": 2`), []byte(`"v": 3`), 1),
		[]byte(`{"v":1}`),
		bytes.Replace(data, []byte(`"salt": "`), []byte(`"extra": 1, "salt": "`), 1),
	}
	for i, bad := range expectFail {
		if _, err := DecodeMeta(bad); err == nil {
			t.Fatalf("第 %d 个非法 meta 应被拒绝", i)
		}
	}
	// v1 仓库仍然可读：升级发生在同步客户端，不在解码器里。
	v1 := bytes.Replace(data, []byte(`"v": 2`), []byte(`"v": 1`), 1)
	if _, err := DecodeMeta(v1); err != nil {
		t.Fatalf("v1 meta 应该仍可读取: %v", err)
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

// TestDeriveGolden 锁定 Argon2id 的已知答案，防止参数或实现悄悄变化。
//
// 期望值由 PHC 参考实现的 C 版 argon2id_hash_raw(3, 65536, 1, ...) 独立算出；
// macOS 与 Linux 使用同一份纯 Go x/crypto/argon2 与同一组参数（盐来自仓库
// meta.json），所以两端派生出的密钥只取决于口令和盐。
func TestDeriveGolden(t *testing.T) {
	salt := make([]byte, SaltSize)
	for i := range salt {
		salt[i] = byte(i)
	}
	key, err := DefaultKDF().Derive("test-passphrase-123", salt)
	if err != nil {
		t.Fatalf("派生密钥失败: %v", err)
	}
	const wantKey = "edad53e4321066e363cb0daa95aa5bec7e217ddeed1cb9a6f47c0eebe7d2c476"
	if got := hex.EncodeToString(key); got != wantKey {
		t.Fatalf("Argon2id 结果不符:\n got %s\nwant %s", got, wantKey)
	}
	// keycheck 期望值由独立实现核对：HKDF-SHA256 + SHA-256。
	const wantCheck = "347494f554af44d9c216b7a9198ab9cf3e3da411349e774b491d322810489083"
	if got := hex.EncodeToString(Keycheck(key)); got != wantCheck {
		t.Fatalf("keycheck 不符:\n got %s\nwant %s", got, wantCheck)
	}
}

// TestRemoteHashGolden 验证同一仓库的 SSH/HTTPS 写法得到同一个哈希，
// 且结果与独立的 SHA-256 计算一致（host 归一化为小写）。
func TestRemoteHashGolden(t *testing.T) {
	const want = "e1ee845dd0a63fd1d20eff11e887196c4e21434aa6a9e3860e2d00f34693e94a"
	for _, remote := range []string{
		"git@github.com:Sheyiyuan/zero-beat.git",
		"https://github.com/Sheyiyuan/zero-beat",
		"https://github.com/Sheyiyuan/zero-beat.git/",
	} {
		if got := RemoteHash(remote); got != want {
			t.Fatalf("RemoteHash(%q) = %s，期望 %s", remote, got, want)
		}
	}
}
