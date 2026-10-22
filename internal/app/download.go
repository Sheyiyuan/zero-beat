package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

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

// DownloadSong 解析音频并落盘到默认下载目录；force 为 true 时覆盖已有文件，
// progress 可为 nil。
func (s *Service) DownloadSong(ctx context.Context, song store.Song, quality source.Quality, force bool, progress func(done, total int64)) DownloadResult {
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
	duration := stream.Duration
	if duration == 0 && merged.Duration > 0 {
		duration = time.Duration(merged.Duration) * time.Second
	}
	res, err := download.Run(ctx, t, stream, outPath, force, onProgress)
	if err != nil {
		path := outPath
		if res.Path != "" {
			path = res.Path
		}
		if errors.Is(err, download.ErrExists) && path != "" {
			_ = s.RecordDownload(merged, path, res.Size, stream.Codec, duration)
		}
		return DownloadResult{Song: merged, Path: path, Size: res.Size, Err: err}
	}
	_ = s.RecordDownload(merged, res.Path, res.Size, stream.Codec, duration)
	return DownloadResult{Song: merged, Path: res.Path, Size: res.Size}
}

// RecordDownload 把成功落盘（或已存在）的文件登记到下载索引。
//
// 只写索引，不碰文件；文件大小缺失时会现场 stat 补齐。
func (s *Service) RecordDownload(song store.Song, path string, size int64, codec string, duration time.Duration) error {
	if s.store == nil || path == "" {
		return nil
	}
	song = store.NormalizeSong(song)
	stored, err := s.store.UpsertSong(song)
	if err != nil {
		return err
	}
	if abs, aerr := filepath.Abs(path); aerr == nil {
		path = abs
	}
	if size <= 0 {
		if info, serr := os.Stat(path); serr == nil {
			size = info.Size()
		}
	}
	durationMS := duration.Milliseconds()
	if durationMS == 0 && stored.Duration > 0 {
		durationMS = int64(stored.Duration) * 1000
	}
	return s.store.UpsertDownload(store.Download{
		SongID:     stored.ID,
		Path:       path,
		Size:       size,
		Codec:      codec,
		DurationMS: durationMS,
	})
}
