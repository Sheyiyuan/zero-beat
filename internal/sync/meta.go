package sync

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

// Meta 是仓库里的 meta.json：全局盐、KDF 参数、密钥版本与口令校验值。
type Meta struct {
	V          int       `json:"v"`
	KeyVersion int       `json:"key_version"`
	KDF        KDFParams `json:"kdf"`
	Salt       string    `json:"salt"`
	Keycheck   string    `json:"keycheck"`
	CreatedAt  int64     `json:"created_at"`
}

// NewMeta 生成新的 meta.json 与对应的本地密钥状态（只用于空仓库初始化）。
func NewMeta(passphrase string, now time.Time) (Meta, *KeyState, error) {
	if len([]rune(passphrase)) < MinPassphraseRunes {
		return Meta{}, nil, fmt.Errorf("同步口令至少 %d 个字符", MinPassphraseRunes)
	}
	salt, err := GenerateSalt()
	if err != nil {
		return Meta{}, nil, err
	}
	kdf := DefaultKDF()
	key, err := kdf.Derive(passphrase, salt)
	if err != nil {
		return Meta{}, nil, err
	}
	m := Meta{
		V:          MetaVersion,
		KeyVersion: 1,
		KDF:        kdf,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Keycheck:   base64.StdEncoding.EncodeToString(Keycheck(key)),
		CreatedAt:  now.Unix(),
	}
	ks := &KeyState{
		Version:    1,
		KeyVersion: 1,
		KDF:        kdf,
		Salt:       salt,
		Key:        key,
	}
	return m, ks, nil
}

// DecodeMeta 严格解析 meta.json：版本、字段、盐与校验值长度都不猜。
func DecodeMeta(data []byte) (Meta, error) {
	var m Meta
	if err := decodeJSONStrict(data, &m); err != nil {
		return Meta{}, fmt.Errorf("%w: 解析 meta.json: %v", ErrCorruptRepo, err)
	}
	// 旧客户端读到这里只认自己写出的版本；v2 仓库对 v1 客户端必须报 schema
	// 错误而不是尝试降级，否则会重新导出并删掉仅本机数据。
	if m.V < MetaVersionMin || m.V > MetaVersion {
		return Meta{}, fmt.Errorf("%w: meta.v=%d，请升级 zero-beat", ErrSchema, m.V)
	}
	if m.KeyVersion < 1 {
		return Meta{}, fmt.Errorf("%w: key_version 无效", ErrCorruptRepo)
	}
	if err := m.KDF.Validate(); err != nil {
		return Meta{}, err
	}
	if m.CreatedAt <= 0 {
		return Meta{}, fmt.Errorf("%w: created_at 无效", ErrCorruptRepo)
	}
	salt, err := base64.StdEncoding.DecodeString(m.Salt)
	if err != nil || len(salt) != SaltSize {
		return Meta{}, fmt.Errorf("%w: salt 无效", ErrCorruptRepo)
	}
	check, err := base64.StdEncoding.DecodeString(m.Keycheck)
	if err != nil || len(check) != KeycheckSize {
		return Meta{}, fmt.Errorf("%w: keycheck 无效", ErrCorruptRepo)
	}
	return m, nil
}

// Encode 序列化 meta.json。
func (m Meta) Encode() ([]byte, error) { return encodeJSON(m) }

// SaltBytes 返回解码后的盐。
func (m Meta) SaltBytes() ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(m.Salt)
	if err != nil || len(salt) != SaltSize {
		return nil, fmt.Errorf("%w: salt 无效", ErrCorruptRepo)
	}
	return salt, nil
}

// KeycheckBytes 返回解码后的校验值。
func (m Meta) KeycheckBytes() ([]byte, error) {
	check, err := base64.StdEncoding.DecodeString(m.Keycheck)
	if err != nil || len(check) != KeycheckSize {
		return nil, fmt.Errorf("%w: keycheck 无效", ErrCorruptRepo)
	}
	return check, nil
}

// Unlock 用口令派生密钥并校验 keycheck；失败返回 ErrKeyMismatch。
func (m Meta) Unlock(passphrase string) (*KeyState, error) {
	salt, err := m.SaltBytes()
	if err != nil {
		return nil, err
	}
	want, err := m.KeycheckBytes()
	if err != nil {
		return nil, err
	}
	key, err := m.KDF.Derive(passphrase, salt)
	if err != nil {
		return nil, err
	}
	if !KeycheckEqual(Keycheck(key), want) {
		return nil, ErrKeyMismatch
	}
	return &KeyState{
		Version:    KeyFileVersion,
		KeyVersion: m.KeyVersion,
		KDF:        m.KDF,
		Salt:       salt,
		Key:        key,
	}, nil
}

// KeyState 是本地缓存的派生密钥；口令本身不落盘。
type KeyState struct {
	Version    int
	KeyVersion int
	KDF        KDFParams
	Salt       []byte
	Key        []byte
	RemoteHash string
}

type keyFile struct {
	V          int       `json:"v"`
	KeyVersion int       `json:"key_version"`
	KDF        KDFParams `json:"kdf"`
	Salt       string    `json:"salt"`
	Key        string    `json:"key"`
	RemoteHash string    `json:"remote_hash"`
}

// LoadKeyState 读取本地 sync-key；文件不存在时返回 (nil, nil)。
func LoadKeyState(path string) (*KeyState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取同步密钥: %w", err)
	}
	if len(data) > 64*1024 {
		return nil, fmt.Errorf("%w: sync-key 过大", ErrCorruptRepo)
	}
	var f keyFile
	if err := decodeJSONStrict(data, &f); err != nil {
		return nil, fmt.Errorf("解析同步密钥: %w", err)
	}
	if f.V != KeyFileVersion {
		return nil, fmt.Errorf("同步密钥版本 %d 不支持", f.V)
	}
	if f.KeyVersion < 1 {
		return nil, fmt.Errorf("同步密钥版本号无效")
	}
	if err := f.KDF.Validate(); err != nil {
		return nil, err
	}
	salt, err := base64.StdEncoding.DecodeString(f.Salt)
	if err != nil || len(salt) != SaltSize {
		return nil, fmt.Errorf("同步密钥的盐无效")
	}
	key, err := base64.StdEncoding.DecodeString(f.Key)
	if err != nil || len(key) != KeySize {
		return nil, fmt.Errorf("同步密钥无效")
	}
	return &KeyState{
		Version:    f.V,
		KeyVersion: f.KeyVersion,
		KDF:        f.KDF,
		Salt:       salt,
		Key:        key,
		RemoteHash: f.RemoteHash,
	}, nil
}

// Save 以 0600 原子写入本地密钥文件。
func (k *KeyState) Save(path string) error {
	f := keyFile{
		V:          KeyFileVersion,
		KeyVersion: k.KeyVersion,
		KDF:        k.KDF,
		Salt:       base64.StdEncoding.EncodeToString(k.Salt),
		Key:        base64.StdEncoding.EncodeToString(k.Key),
		RemoteHash: k.RemoteHash,
	}
	data, err := encodeJSON(f)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

// RemoteHash 用于发现「换了仓库但沿用旧密钥」；哈希归一化后的仓库身份，
// 同一仓库的 SSH/HTTPS 写法得到相同结果，切换连接方式不会让密钥失效。
func RemoteHash(remote string) string {
	sum := sha256.Sum256([]byte(RemoteIdentity(remote)))
	return hex.EncodeToString(sum[:])
}

// LoadMetaFile 读取并解析仓库里的 meta.json。
func LoadMetaFile(dir string) (Meta, error) {
	data, err := os.ReadFile(joinPath(dir, MetaFile))
	if err != nil {
		return Meta{}, err
	}
	return DecodeMeta(data)
}
