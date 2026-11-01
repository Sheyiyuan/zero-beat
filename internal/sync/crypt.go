package sync

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

// 格式常量。改动任何一个都会让旧仓库无法读取，必须同步升 meta.v。
const (
	MetaVersion        = 1
	KeyFileVersion     = 1
	EnvelopeVersion    = 1
	SaltSize           = 16
	KeySize            = 32
	KeycheckSize       = 32
	MaxPassphraseLen   = 1024
	MinPassphraseRunes = 12
)

// KDFParams 是口令派生参数。只接受白名单，避免远端塞入超大 m 造成 DoS。
type KDFParams struct {
	Alg    string `json:"alg"`
	M      uint32 `json:"m"`
	T      uint32 `json:"t"`
	P      uint8  `json:"p"`
	KeyLen uint32 `json:"key_len"`
}

// DefaultKDF 是当前唯一允许的参数组合：Argon2id、64 MiB、3 轮、1 线程、32 字节。
func DefaultKDF() KDFParams {
	return KDFParams{Alg: "argon2id", M: 64 * 1024, T: 3, P: 1, KeyLen: KeySize}
}

// Validate 只接受文档里写死的参数组合。
func (p KDFParams) Validate() error {
	want := DefaultKDF()
	if p != want {
		return fmt.Errorf("%w: 不支持的 KDF 参数 %+v", ErrSchema, p)
	}
	return nil
}

// Derive 用 Argon2id 从口令与盐派生主密钥。
func (p KDFParams) Derive(passphrase string, salt []byte) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if len(passphrase) == 0 || len(passphrase) > MaxPassphraseLen {
		return nil, fmt.Errorf("同步口令长度无效")
	}
	if len(salt) != SaltSize {
		return nil, fmt.Errorf("同步盐长度应为 %d 字节", SaltSize)
	}
	key := argon2.IDKey([]byte(passphrase), salt, p.T, p.M, p.P, p.KeyLen)
	return key, nil
}

// Keycheck 计算用于校验口令的独立派生值：SHA-256(HKDF-SHA256(key, info))。
// 它不能反推主密钥，但任何校验值都允许离线猜口令，所以 KDF 成本必须足够高。
func Keycheck(key []byte) []byte {
	reader := hkdf.New(sha256.New, key, nil, []byte("zero-beat:keycheck:v1"))
	derived := make([]byte, 32)
	if _, err := io.ReadFull(reader, derived); err != nil {
		return nil
	}
	sum := sha256.Sum256(derived)
	return sum[:]
}

// GenerateSalt 返回新的随机盐。
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("生成同步盐: %w", err)
	}
	return salt, nil
}

// Envelope 是单个音源登录态的密文信封；nonce 与密文都可以公开。
type Envelope struct {
	V          int    `json:"v"`
	Source     string `json:"source"`
	KeyVersion int    `json:"key_version"`
	Nonce      string `json:"nonce"`
	Data       string `json:"data"`
}

// Credential 是密封前的明文；updated_at 用于跨设备 LWW。
type Credential struct {
	Source    string            `json:"source"`
	UpdatedAt int64             `json:"updated_at"`
	Device    string            `json:"device"`
	Session   map[string]string `json:"session"`
}

func credentialAAD(source string, keyVersion int) []byte {
	return []byte(fmt.Sprintf("zero-beat|credentials|v1|%s|%d", source, keyVersion))
}

// Seal 生成 JSON 信封并认证加密；每次调用都使用新的随机 nonce。
func Seal(key []byte, source string, keyVersion int, plaintext []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("同步密钥长度无效")
	}
	if source == "" {
		return nil, fmt.Errorf("登录态缺少音源")
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("初始化 XChaCha20-Poly1305: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("生成 nonce: %w", err)
	}
	sealed := aead.Seal(nil, nonce, plaintext, credentialAAD(source, keyVersion))
	envelope := Envelope{
		V:          EnvelopeVersion,
		Source:     source,
		KeyVersion: keyVersion,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Data:       base64.StdEncoding.EncodeToString(sealed),
	}
	return encodeJSON(envelope)
}

// Open 先认证后解析：认证失败只有「密钥不对」或「文件被篡改」两种解释。
func Open(key []byte, source string, keyVersion int, data []byte) ([]byte, error) {
	var envelope Envelope
	if err := decodeJSONStrict(data, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCredentialReject, err)
	}
	if envelope.V != EnvelopeVersion {
		return nil, fmt.Errorf("%w: 密文格式版本 %d", ErrSchema, envelope.V)
	}
	if envelope.Source != source {
		return nil, fmt.Errorf("%w: 密文音源不匹配", ErrCredentialReject)
	}
	if envelope.KeyVersion != keyVersion {
		return nil, fmt.Errorf("%w: 密文密钥版本 %d", ErrKeyMismatch, envelope.KeyVersion)
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil || len(nonce) != chacha20poly1305.NonceSizeX {
		return nil, fmt.Errorf("%w: nonce 无效", ErrCredentialReject)
	}
	sealed, err := base64.StdEncoding.DecodeString(envelope.Data)
	if err != nil {
		return nil, fmt.Errorf("%w: 密文无效", ErrCredentialReject)
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("初始化 XChaCha20-Poly1305: %w", err)
	}
	plain, err := aead.Open(nil, nonce, sealed, credentialAAD(source, keyVersion))
	if err != nil {
		return nil, ErrKeyMismatch
	}
	return plain, nil
}

// EncryptCredential 序列化并加密登录态。
func EncryptCredential(key []byte, source string, keyVersion int, cred Credential) ([]byte, error) {
	plain, err := encodeJSON(cred)
	if err != nil {
		return nil, err
	}
	return Seal(key, source, keyVersion, plain)
}

// DecryptCredential 解密并校验音源与字段。
func DecryptCredential(key []byte, source string, keyVersion int, data []byte) (Credential, error) {
	plain, err := Open(key, source, keyVersion, data)
	if err != nil {
		return Credential{}, err
	}
	var cred Credential
	if err := decodeJSONStrict(plain, &cred); err != nil {
		return Credential{}, fmt.Errorf("%w: %v", ErrCredentialReject, err)
	}
	if cred.Source != source {
		return Credential{}, fmt.Errorf("%w: 明文音源不匹配", ErrCredentialReject)
	}
	if cred.UpdatedAt <= 0 {
		return Credential{}, fmt.Errorf("%w: 缺少更新时间", ErrCredentialReject)
	}
	if cred.Session == nil {
		cred.Session = map[string]string{}
	}
	return cred, nil
}

// EnvelopeKeyVersion 读取信封声明的密钥版本；文件损坏时返回 ok=false。
func EnvelopeKeyVersion(data []byte) (int, bool) {
	var envelope Envelope
	if err := decodeJSONStrict(data, &envelope); err != nil {
		return 0, false
	}
	return envelope.KeyVersion, true
}

// KeycheckEqual 做常数时间比较。
func KeycheckEqual(a, b []byte) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare(a, b) == 1
}

// encodeJSON 是仓库内所有 JSON 的确定性编码：两个空格缩进、末尾一个换行。
func encodeJSON(v any) ([]byte, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func decodeJSONStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("JSON 末尾有多余内容")
	}
	return nil
}
