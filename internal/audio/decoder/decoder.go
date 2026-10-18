// Package decoder 定义解码后端抽象。
//
// 计划两套实现：
//
//   - native：纯 Go（MP3 / FLAC / AAC-LC），覆盖绝大多数曲目，无外部依赖；
//   - mpv：外部进程兜底，覆盖 HE-AAC、EC-3 等 native 解不了的编码。
//
// 选轨时应优先选择 native 可解的编码，把外部依赖留作兜底。
// 详见 docs/adr/0003-audio-decoder-abstraction.md。
package decoder

import (
	"context"
	"io"

	"github.com/sheyiyuan/zero-beat/internal/domain"
	"github.com/sheyiyuan/zero-beat/internal/source"
)

// Format 是解码后的 PCM 格式，统一为交错 s16le。
type Format struct {
	SampleRate int
	Channels   int
}

// Decoder 把 Stream 解成 PCM 流。
type Decoder interface {
	// Name 用于日志与状态展示，如 "native"、"mpv"。
	Name() string
	// Supports 判断该后端能否解码指定编码。
	Supports(codec domain.Codec) bool
	// Open 打开流；返回的 ReadCloser 输出 pcm，Close 后应释放全部资源。
	Open(ctx context.Context, s *source.Stream) (io.ReadCloser, Format, error)
}
