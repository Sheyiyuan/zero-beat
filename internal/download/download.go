// Package download 把音源返回的音频流落盘：支持 Range 续传、ffmpeg 无损封装和标签。
package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/source"
)

// ErrExists 表示目标文件已经存在，且没有要求覆盖。
var ErrExists = errors.New("download: 文件已存在")

// Progress 是下载进度；Total 为 0 表示服务端没有给出总长度。
type Progress struct {
	Done  int64
	Total int64
}

// Result 描述一次成功下载。
type Result struct {
	Path    string
	Size    int64
	Resumed bool
}

// DefaultDir 返回 TUI/CLI 默认下载目录：~/Music/zero-beat。
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "downloads"
	}
	return filepath.Join(home, "Music", "zero-beat")
}

// Ext 根据编码选择容器扩展名；未知编码按 m4a 兜底，不再按平台猜测。
func Ext(codec string) string {
	c := strings.ToLower(codec)
	switch {
	case strings.Contains(c, "flac"):
		return ".flac"
	case strings.Contains(c, "mp3"):
		return ".mp3"
	case strings.Contains(c, "aac"), strings.Contains(c, "mp4a"),
		strings.Contains(c, "ec-3"), strings.Contains(c, "opus"):
		return ".m4a"
	}
	return ".m4a"
}

// Filename 返回「歌手 - 标题」形式的文件名（不带扩展名）。
func Filename(track source.Track) string {
	title := strings.TrimSpace(track.Title)
	artist := strings.TrimSpace(track.Artist)
	name := title
	if artist != "" && title != "" {
		name = artist + " - " + title
	} else if title == "" {
		name = string(track.Source) + "-" + track.ExternalID
	}
	if strings.TrimSpace(name) == "" || name == "-" {
		name = track.ExternalID
	}
	return safeFilename(name)
}

// PartPath 返回稳定续传文件名；带来源和曲目 ID，避免不同曲目混用半成品。
func PartPath(outPath string, track source.Track) string {
	tag := safeFilename(string(track.Source) + "-" + track.ExternalID)
	return outPath + "." + tag + ".part"
}

// Run 下载一首歌到 outPath。
// force 为 true 时会删除已有目标和 .part，从零开始；否则已有目标返回 ErrExists。
func Run(ctx context.Context, track source.Track, stream source.Stream, outPath string, force bool, onProgress func(Progress)) (Result, error) {
	if outPath == "" {
		return Result{}, errors.New("download: 输出路径为空")
	}
	if !force {
		if _, err := os.Stat(outPath); err == nil {
			return Result{Path: outPath}, ErrExists
		}
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return Result{}, fmt.Errorf("创建下载目录: %w", err)
	}

	part := PartPath(outPath, track)
	if force {
		_ = os.Remove(outPath)
		_ = os.Remove(part)
	}
	resumed := false
	if info, err := os.Stat(part); err == nil && info.Size() > 0 {
		resumed = true
	}

	for attempt := 0; attempt < 2; attempt++ {
		if err := downloadToPart(ctx, part, stream, onProgress); err != nil {
			return Result{Path: outPath, Resumed: resumed}, err
		}
		if err := ffmpegRemux(ctx, part, outPath, track); err != nil {
			if attempt == 0 {
				_ = os.Remove(part)
				resumed = false
				continue
			}
			return Result{Path: outPath, Resumed: resumed}, err
		}
		_ = os.Remove(part)
		size := int64(0)
		if fi, err := os.Stat(outPath); err == nil {
			size = fi.Size()
		}
		return Result{Path: outPath, Size: size, Resumed: resumed}, nil
	}
	return Result{Path: outPath, Resumed: resumed}, errors.New("download: 下载失败")
}

func downloadToPart(ctx context.Context, path string, stream source.Stream, onProgress func(Progress)) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开临时文件: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("读取临时文件: %w", err)
	}
	offset := info.Size()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, stream.URL, nil)
	if err != nil {
		return fmt.Errorf("构造下载请求: %w", err)
	}
	for k, v := range stream.Headers {
		req.Header.Set(k, v)
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	start := offset
	switch resp.StatusCode {
	case http.StatusPartialContent:
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return fmt.Errorf("定位临时文件: %w", err)
		}
	case http.StatusOK:
		if offset > 0 {
			if err := f.Truncate(0); err != nil {
				return fmt.Errorf("重置临时文件: %w", err)
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("定位临时文件: %w", err)
			}
			start = 0
		}
	case http.StatusRequestedRangeNotSatisfiable:
		if offset > 0 {
			return nil // 本地已经完整，交给 ffmpeg 校验/封装
		}
		return fmt.Errorf("下载失败: HTTP %s", resp.Status)
	default:
		return fmt.Errorf("下载失败: HTTP %s", resp.Status)
	}

	total := contentTotal(resp, start)
	report(onProgress, Progress{Done: start, Total: total})
	pw := &progressCallback{onProgress: onProgress, done: start, total: total}
	if _, err := io.CopyBuffer(io.MultiWriter(f, pw), resp.Body, make([]byte, 64*1024)); err != nil {
		return fmt.Errorf("下载中断: %w", err)
	}
	report(onProgress, Progress{Done: pw.done, Total: total})
	return nil
}

func report(onProgress func(Progress), p Progress) {
	if onProgress != nil {
		onProgress(p)
	}
}

type progressCallback struct {
	onProgress func(Progress)
	done       int64
	total      int64
}

func (p *progressCallback) Write(b []byte) (int, error) {
	n := len(b)
	p.done += int64(n)
	report(p.onProgress, Progress{Done: p.done, Total: p.total})
	return n, nil
}

func contentTotal(resp *http.Response, start int64) int64 {
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if i := strings.LastIndex(cr, "/"); i >= 0 {
			if n, err := strconv.ParseInt(strings.TrimSpace(cr[i+1:]), 10, 64); err == nil {
				return n
			}
		}
	}
	if resp.ContentLength >= 0 {
		return start + resp.ContentLength
	}
	return 0
}

func ffmpegRemux(ctx context.Context, in, out string, track source.Track) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("找不到 ffmpeg，无法封装音频: %w", err)
	}
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", in}
	for _, kv := range [][2]string{
		{"title", track.Title},
		{"artist", track.Artist},
		{"album", track.Album},
	} {
		if v := strings.TrimSpace(kv[1]); v != "" {
			args = append(args, "-metadata", kv[0]+"="+v)
		}
	}
	args = append(args, "-vn", "-c", "copy", out)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(out)
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("封装音频失败: %w: %s", err, msg)
		}
		return fmt.Errorf("封装音频失败: %w", err)
	}
	return nil
}

func safeFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
	s = strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_",
	).Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, ". ")
	if s == "" {
		s = "untitled"
	}
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120])
	}
	return s
}

// HumanBytes 把字节数格式化成 TUI/CLI 共用的短文本。
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// FormatDuration 把时长格式化成 m:ss / h:mm:ss。
func FormatDuration(d time.Duration) string {
	total := int(d.Round(time.Second) / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
