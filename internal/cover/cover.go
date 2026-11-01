// Package cover 下载并缓存歌曲封面，供系统媒体面板读取。
//
// 封面只服务系统集成，不参与终端渲染；缓存按封面 URL 哈希命名，
// 同一 URL 只下载一次，总量超限时按最近使用时间淘汰。
package cover

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/config"
)

const (
	// DefaultMaxBytes 是封面缓存上限；封面通常只有几十 KB，足够存上千张。
	DefaultMaxBytes  = 64 << 20
	maxDownloadBytes = 8 << 20
)

var imageExts = map[string]string{
	".jpg":  ".jpg",
	".jpeg": ".jpg",
	".png":  ".png",
	".webp": ".webp",
	".gif":  ".gif",
}

// Cache 是磁盘封面缓存。
type Cache struct {
	dir      string
	client   *http.Client
	maxBytes int64
}

// New 在 dir 下创建封面缓存。
func New(dir string) *Cache {
	return &Cache{
		dir:      dir,
		client:   &http.Client{Timeout: 15 * time.Second},
		maxBytes: DefaultMaxBytes,
	}
}

var (
	defaultMu    sync.Mutex
	defaultCache *Cache
)

// Fetch 用默认缓存下载封面。
func Fetch(ctx context.Context, rawURL string, headers map[string]string) (string, error) {
	dir, err := config.CacheDir()
	if err != nil {
		return "", err
	}
	defaultMu.Lock()
	if defaultCache == nil {
		defaultCache = New(filepath.Join(dir, "covers"))
	}
	cache := defaultCache
	defaultMu.Unlock()
	return cache.Fetch(ctx, rawURL, headers)
}

// Fetch 下载并缓存封面，返回本地文件路径。
func (c *Cache) Fetch(ctx context.Context, rawURL string, headers map[string]string) (string, error) {
	if rawURL == "" {
		return "", errors.New("cover: 封面地址为空")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("cover: 无效封面地址 %q", rawURL)
	}
	if !strings.HasPrefix(u.Scheme, "http") {
		return "", fmt.Errorf("cover: 不支持的封面协议 %q", u.Scheme)
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return "", fmt.Errorf("cover: 创建缓存目录: %w", err)
	}

	hash := hashURL(rawURL)
	if path := c.cachedPath(hash); path != "" {
		// 命中缓存只刷新访问时间，失败不影响使用。
		_ = os.Chtimes(path, time.Now(), time.Now())
		return path, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("cover: 构造请求: %w", err)
	}
	req.Header.Set("User-Agent", "zero-beat")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cover: 下载封面: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cover: 下载封面失败: HTTP %d", resp.StatusCode)
	}

	ext := extFromURL(u)
	if ext == "" {
		ext = extFromContentType(resp.Header.Get("Content-Type"))
	}
	if ext == "" {
		return "", errors.New("cover: 响应不是可识别的图片格式")
	}

	tmp, err := os.CreateTemp(c.dir, "download-*"+ext)
	if err != nil {
		return "", fmt.Errorf("cover: 创建临时文件: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, maxDownloadBytes)); err != nil {
		cleanup()
		return "", fmt.Errorf("cover: 写入封面: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("cover: 关闭封面文件: %w", err)
	}

	finalPath := filepath.Join(c.dir, hash+ext)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("cover: 落盘封面: %w", err)
	}
	c.trim()
	return finalPath, nil
}

func (c *Cache) cachedPath(hash string) string {
	matches, err := filepath.Glob(filepath.Join(c.dir, hash+".*"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	for _, path := range matches {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return path
		}
	}
	return ""
}

// trim 按修改时间淘汰旧封面，保证缓存不超过上限。
func (c *Cache) trim() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type item struct {
		path string
		size int64
		at   time.Time
	}
	items := make([]item, 0, len(entries))
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), "download-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		items = append(items, item{path: filepath.Join(c.dir, entry.Name()), size: info.Size(), at: info.ModTime()})
		total += info.Size()
	}
	if total <= c.maxBytes {
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].at.Before(items[j].at) })
	for _, it := range items {
		if total <= c.maxBytes {
			break
		}
		if err := os.Remove(it.path); err == nil {
			total -= it.size
		}
	}
}

func hashURL(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:])
}

func extFromURL(u *url.URL) string {
	ext := strings.ToLower(filepath.Ext(u.Path))
	return imageExts[ext]
}

func extFromContentType(ct string) string {
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return ""
	}
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ""
	}
}
