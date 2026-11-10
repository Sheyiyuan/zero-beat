package store

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 歌词绑定与 offset 的 settings key 前缀。
const (
	lyricBindKey      = "lyric_bind:"
	lyricBindLocalKey = "lyric_bind_local:"
	lyricOffsetKey    = "lyric_offset:"
)

// BindingScope 表示当前实际生效的歌词来源层级。
//
// 它描述的是「这一版歌词从哪来」，而不是「是否存在绑定」：绑定失效降级到
// 自动时是 auto，offset 也必须写回 auto，不能写进已经失效的绑定。
type BindingScope string

const (
	BindingAuto     BindingScope = "auto"
	BindingPortable BindingScope = "portable"
	BindingLocal    BindingScope = "local"
)

// LyricBinding 是用户为一首歌显式指定的歌词来源。
//
// 只存引用和展示信息，不存歌词正文：正文每次按引用重新取，平台更新后自然生效。
// Kind 为 source / lrclib 时是可跨设备复现的 portable 绑定；local_file /
// local_song 指向本机，永不参与同步。
type LyricBinding struct {
	Kind       string `json:"kind"` // source | lrclib | local_file | local_song
	Source     string `json:"source,omitempty"`
	ExternalID string `json:"external_id,omitempty"`
	Page       int    `json:"page,omitempty"`
	CID        int64  `json:"cid,omitempty"`
	LRCLIBID   int64  `json:"lrclib_id,omitempty"`
	Path       string `json:"path,omitempty"`
	SongID     string `json:"song_id,omitempty"`
	Title      string `json:"title,omitempty"`
	Artist     string `json:"artist,omitempty"`
	Duration   int    `json:"duration,omitempty"`
	OffsetMS   int    `json:"offset_ms,omitempty"`
	BoundAt    int64  `json:"bound_at,omitempty"`
}

// BindingKind 是绑定类型的枚举值。
const (
	BindingKindSource    = "source"
	BindingKindLRCLIB    = "lrclib"
	BindingKindLocalFile = "local_file"
	BindingKindLocalSong = "local_song"
)

// Portable 判断绑定是否可以跨设备复现。
func (b LyricBinding) Portable() bool {
	return b.Kind == BindingKindSource || b.Kind == BindingKindLRCLIB
}

// Local 判断绑定是否指本机资源。
func (b LyricBinding) Local() bool {
	return b.Kind == BindingKindLocalFile || b.Kind == BindingKindLocalSong
}

// Empty 判断绑定是否为空。
func (b LyricBinding) Empty() bool { return strings.TrimSpace(b.Kind) == "" }

// Validate 校验绑定字段与类型一致；绑定前必须已经能取到带时间轴的歌词。
func (b LyricBinding) Validate() error {
	switch b.Kind {
	case BindingKindSource:
		if strings.TrimSpace(b.Source) == "" || strings.TrimSpace(b.ExternalID) == "" {
			return fmt.Errorf("store: 平台歌词绑定缺少音源或外部 ID")
		}
	case BindingKindLRCLIB:
		if b.LRCLIBID <= 0 {
			return fmt.Errorf("store: LRCLIB 绑定缺少记录 ID")
		}
	case BindingKindLocalFile:
		if strings.TrimSpace(b.Path) == "" {
			return fmt.Errorf("store: 本地歌词绑定缺少文件路径")
		}
	case BindingKindLocalSong:
		if strings.TrimSpace(b.SongID) == "" {
			return fmt.Errorf("store: 本机曲目绑定缺少歌曲 ID")
		}
	default:
		return fmt.Errorf("store: 未知歌词绑定类型 %q", b.Kind)
	}
	return nil
}

// LyricBinding 返回实际生效的绑定：本机绑定优先于可同步绑定。
// 存储里的 JSON 无法解析时按「没有绑定」处理，歌词回退到自动来源。
func (s *Store) LyricBinding(songID string) (LyricBinding, BindingScope, error) {
	songID = strings.TrimSpace(songID)
	if songID == "" {
		return LyricBinding{}, BindingAuto, nil
	}
	if b, ok := s.readLyricBinding(lyricBindLocalKey + songID); ok {
		return b, BindingLocal, nil
	}
	if b, ok := s.readLyricBinding(lyricBindKey + songID); ok {
		return b, BindingPortable, nil
	}
	return LyricBinding{}, BindingAuto, nil
}

// PortableLyricBinding 只读可同步的绑定，忽略本机绑定。
func (s *Store) PortableLyricBinding(songID string) (LyricBinding, bool) {
	return s.readLyricBinding(lyricBindKey + strings.TrimSpace(songID))
}

// LocalLyricBinding 只读本机绑定。
func (s *Store) LocalLyricBinding(songID string) (LyricBinding, bool) {
	return s.readLyricBinding(lyricBindLocalKey + strings.TrimSpace(songID))
}

func (s *Store) readLyricBinding(key string) (LyricBinding, bool) {
	raw := strings.TrimSpace(s.Setting(key, ""))
	if raw == "" {
		return LyricBinding{}, false
	}
	var b LyricBinding
	if err := json.Unmarshal([]byte(raw), &b); err != nil || b.Empty() {
		return LyricBinding{}, false
	}
	return b, true
}

// SaveLyricBinding 按绑定类型写到 portable 或 local key，永不混写。
// 一首歌只有一个生效绑定，新绑定覆盖同类型的旧绑定。
func (s *Store) SaveLyricBinding(songID string, b LyricBinding) error {
	songID = strings.TrimSpace(songID)
	if songID == "" {
		return fmt.Errorf("store: 歌词绑定缺少歌曲 ID")
	}
	if err := b.Validate(); err != nil {
		return err
	}
	if b.BoundAt <= 0 {
		b.BoundAt = time.Now().Unix()
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("store: 编码歌词绑定: %w", err)
	}
	key := lyricBindKey
	if b.Local() {
		key = lyricBindLocalKey
	}
	return s.SetSetting(key+songID, string(raw))
}

// ClearLyricBinding 解除一首歌的全部绑定（portable 与 local 都清）。
func (s *Store) ClearLyricBinding(songID string) error {
	songID = strings.TrimSpace(songID)
	if songID == "" {
		return nil
	}
	if _, err := s.db.Exec(`DELETE FROM settings WHERE key = ? OR key = ?`,
		lyricBindKey+songID, lyricBindLocalKey+songID); err != nil {
		return fmt.Errorf("store: 解除歌词绑定: %w", err)
	}
	return nil
}

// LyricOffset 读取某一层来源自己的 offset。
// portable/local 从绑定 JSON 里取，auto 用独立的 lyric_offset key。
func (s *Store) LyricOffset(songID string, scope BindingScope) int {
	songID = strings.TrimSpace(songID)
	if songID == "" {
		return 0
	}
	switch scope {
	case BindingLocal:
		if b, ok := s.LocalLyricBinding(songID); ok {
			return b.OffsetMS
		}
	case BindingPortable:
		if b, ok := s.PortableLyricBinding(songID); ok {
			return b.OffsetMS
		}
	default:
		if v, err := s.SettingInt(lyricOffsetKey+songID, 0); err == nil {
			return v
		}
	}
	return 0
}

// SetLyricOffset 把 offset 写进「当前实际来源」。
//
// offset 属于当前歌词来源，不属于歌曲：换源就换 offset。传 auto 时写独立 key；
// portable/local 只更新对应绑定的 offset_ms，绑定不存在时退回 auto，避免把
// 失效绑定的 offset 写脏。
func (s *Store) SetLyricOffset(songID string, scope BindingScope, offsetMS int) error {
	songID = strings.TrimSpace(songID)
	if songID == "" {
		return fmt.Errorf("store: 歌词偏移缺少歌曲 ID")
	}
	switch scope {
	case BindingLocal, BindingPortable:
		var (
			b  LyricBinding
			ok bool
		)
		if scope == BindingLocal {
			b, ok = s.LocalLyricBinding(songID)
		} else {
			b, ok = s.PortableLyricBinding(songID)
		}
		if ok {
			b.OffsetMS = offsetMS
			raw, err := json.Marshal(b)
			if err != nil {
				return fmt.Errorf("store: 编码歌词绑定: %w", err)
			}
			key := lyricBindKey
			if scope == BindingLocal {
				key = lyricBindLocalKey
			}
			return s.SetSetting(key+songID, string(raw))
		}
	}
	return s.SetAutoLyricOffset(songID, offsetMS)
}

// SetAutoLyricOffset 写自动歌词的 offset；0 等价于清除，保持 settings 干净。
func (s *Store) SetAutoLyricOffset(songID string, offsetMS int) error {
	songID = strings.TrimSpace(songID)
	if songID == "" {
		return nil
	}
	if offsetMS == 0 {
		if _, err := s.db.Exec(`DELETE FROM settings WHERE key = ?`, lyricOffsetKey+songID); err != nil {
			return fmt.Errorf("store: 清除歌词偏移: %w", err)
		}
		return nil
	}
	return s.SetSetting(lyricOffsetKey+songID, strconv.Itoa(offsetMS))
}

// localSongIDSet 返回曲库里 source=local 的歌曲 ID 集合。
// 注释导出必须跳过它们：本地曲目本身不在同步仓库里，附属数据过去也没有意义。
func (s *Store) localSongIDSet() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT id FROM songs WHERE source = 'local'`)
	if err != nil {
		return nil, fmt.Errorf("store: 读取本地曲目: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
