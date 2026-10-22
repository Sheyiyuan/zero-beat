package app

import (
	"context"
	"path/filepath"

	"github.com/sheyiyuan/zero-beat/internal/download"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// DownloadResult 是界面层需要的下载结果。
type DownloadResult struct {
	Song store.Song
	Path string
	Size int64
	Err  error
}

// DownloadSong 解析音频并落盘到默认下载目录；progress 可为 nil。
func (s *Service) DownloadSong(ctx context.Context, song store.Song, quality source.Quality, progress func(done, total int64)) DownloadResult {
	song = store.NormalizeSong(song)
	track, stream, err := s.Stream(ctx, song, quality)
	if err != nil {
		return DownloadResult{Song: song, Err: err}
	}
	merged := MergeResolved(song, track)
	t := source.Track{
		Source:     source.ID(merged.Source),
		ExternalID: merged.ExternalID(),
		Page:       merged.Page,
		Title:      merged.Title,
		Artist:     merged.Artist,
		Album:      merged.Album,
		Duration:   merged.Duration,
		Cover:      merged.Cover,
	}
	outPath := filepath.Join(download.DefaultDir(), download.Filename(t)+download.Ext(stream.Codec))
	var onProgress func(download.Progress)
	if progress != nil {
		onProgress = func(p download.Progress) { progress(p.Done, p.Total) }
	}
	res, err := download.Run(ctx, t, stream, outPath, false, onProgress)
	if err != nil {
		path := outPath
		if res.Path != "" {
			path = res.Path
		}
		return DownloadResult{Song: merged, Path: path, Size: res.Size, Err: err}
	}
	return DownloadResult{Song: merged, Path: res.Path, Size: res.Size}
}
