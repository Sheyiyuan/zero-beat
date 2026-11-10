package lyrics

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// LocalConfig 是本地歌词的识别配置，来自 ~/.config/zero-beat/lyrics.toml。
//
// 它只在本机生效，不参与同步：候选顺序与翻译合成都跟本地目录结构有关。
type LocalConfig struct {
	// CandidateOrder 是自动选择时的匹配优先级，越靠前越优。
	CandidateOrder []string
	// Translation 是翻译的处理方式：both / same-timestamp / sidecar。
	Translation string
	// MaxSizeKB 是单个歌词文件的大小上限。
	MaxSizeKB int
}

// 翻译取值。
const (
	TranslationBoth          = "both"
	TranslationSameTimestamp = "same-timestamp"
	TranslationSidecar       = "sidecar"
)

// 候选匹配类型，与 candidate_order 的取值一一对应。
const (
	MatchStem        = "stem"
	MatchArtistTitle = "artist-title"
	MatchTitleArtist = "title-artist"
	MatchTitle       = "title"
	MatchTag         = "tag"
)

var defaultCandidateOrder = []string{MatchStem, MatchArtistTitle, MatchTitleArtist, MatchTitle, MatchTag}

// DefaultLocalConfig 返回内置默认值；配置缺失或无效时也用它。
func DefaultLocalConfig() LocalConfig {
	return LocalConfig{
		CandidateOrder: append([]string(nil), defaultCandidateOrder...),
		Translation:    TranslationBoth,
		MaxSizeKB:      2048,
	}
}

// MaxBytes 返回歌词文件大小上限（字节）。
func (c LocalConfig) MaxBytes() int64 {
	kb := c.MaxSizeKB
	if kb <= 0 {
		kb = DefaultLocalConfig().MaxSizeKB
	}
	return int64(kb) << 10
}

// Rank 返回匹配类型的优先级；不在配置里的类型排在最后。
func (c LocalConfig) Rank(kind string) int {
	for i, k := range c.CandidateOrder {
		if k == kind {
			return i
		}
	}
	return len(c.CandidateOrder) + 1
}

// WantsSameTimestamp 判断是否需要合并同时间戳双语行。
func (c LocalConfig) WantsSameTimestamp() bool {
	return c.Translation == TranslationBoth || c.Translation == TranslationSameTimestamp
}

// WantsSidecar 判断是否需要合并独立翻译文件。
func (c LocalConfig) WantsSidecar() bool {
	return c.Translation == TranslationBoth || c.Translation == TranslationSidecar
}

// tomlFile 是 lyrics.toml 的磁盘形态。
type tomlFile struct {
	Local struct {
		CandidateOrder []string `toml:"candidate_order"`
		Translation    string   `toml:"translation"`
		MaxSizeKB      int      `toml:"max_size_kb"`
	} `toml:"local"`
}

// LoadLocalConfig 读取并校验配置。文件不存在时返回默认值且不告警；
// 内容无效时返回默认值并给出中文警告，绝不因为配置问题影响播放。
func LoadLocalConfig(path string) (LocalConfig, []string) {
	cfg := DefaultLocalConfig()
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, []string{"读取 lyrics.toml 失败，已使用默认配置"}
	}
	var file tomlFile
	if err := toml.Unmarshal(data, &file); err != nil {
		return cfg, []string{"lyrics.toml 格式无效，已使用默认配置"}
	}
	var warnings []string
	if len(file.Local.CandidateOrder) > 0 {
		order, bad := normalizeOrder(file.Local.CandidateOrder)
		if len(bad) > 0 {
			warnings = append(warnings, "lyrics.toml 候选顺序含未知类型："+strings.Join(bad, "、"))
		}
		if len(order) > 0 {
			cfg.CandidateOrder = order
		}
	}
	if v := strings.TrimSpace(file.Local.Translation); v != "" {
		switch v {
		case TranslationBoth, TranslationSameTimestamp, TranslationSidecar:
			cfg.Translation = v
		default:
			warnings = append(warnings, "lyrics.toml 的 translation 值无效，已使用 both")
		}
	}
	if v := file.Local.MaxSizeKB; v > 0 {
		// 上限再夹一次，避免误配把整个文件读进内存。
		if v > 8192 {
			cfg.MaxSizeKB = 8192
			warnings = append(warnings, "lyrics.toml 的 max_size_kb 过大，已收敛到 8192")
		} else {
			cfg.MaxSizeKB = v
		}
	}
	return cfg, warnings
}

// normalizeOrder 去掉未知类型与重复项，保留有效顺序。
func normalizeOrder(order []string) (kept, bad []string) {
	seen := map[string]bool{}
	for _, raw := range order {
		v := strings.ToLower(strings.TrimSpace(raw))
		switch v {
		case MatchStem, MatchArtistTitle, MatchTitleArtist, MatchTitle, MatchTag:
			if !seen[v] {
				seen[v] = true
				kept = append(kept, v)
			}
		default:
			if v != "" {
				bad = append(bad, raw)
			}
		}
	}
	return kept, bad
}

// ValidateLocalConfig 供测试和排障使用：确认配置最终落在合法取值上。
func ValidateLocalConfig(c LocalConfig) error {
	if len(c.CandidateOrder) == 0 {
		return fmt.Errorf("lyrics: 候选顺序不能为空")
	}
	switch c.Translation {
	case TranslationBoth, TranslationSameTimestamp, TranslationSidecar:
	default:
		return fmt.Errorf("lyrics: 未知翻译方式 %q", c.Translation)
	}
	return nil
}
