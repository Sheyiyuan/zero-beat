// Package local 把本机音频文件接成一种音源。
//
// 它刻意只实现「取流 + 本地歌词」：不搜索、不遍历远端歌单、不参与登录。
// 登录页、二维码轮换和账号摘要按 CapAccount 过滤，所以不会把本机当成一个
// 需要扫码的平台。
package local

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/library"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// Mark 是列表行里的短标记，与平台源的 Mark 语义一致。
const Mark = "本"

// Source 是本地音乐库音源。
type Source struct {
	store *store.Store
	cfg   lyrics.LocalConfig
	cache *dirCache
}

var (
	_ source.Source     = (*Source)(nil)
	_ source.LyricNamer = (*Source)(nil)
)

// New 构造本地音源；cfg 来自 lyrics.toml，零值时按默认值处理。
func New(st *store.Store, cfg lyrics.LocalConfig) *Source {
	if len(cfg.CandidateOrder) == 0 {
		cfg = lyrics.DefaultLocalConfig()
	}
	return &Source{store: st, cfg: cfg, cache: newDirCache()}
}

// ID 返回音源标识。
func (s *Source) ID() source.ID { return source.Local }

// Name 返回音源显示名。
func (s *Source) Name() string { return "本地音乐库" }

// Mark 返回列表短标记。
func (s *Source) Mark() string { return Mark }

// Caps 不置任何能力位：本地源不搜索、不写云端歌单、不需要登录。
// 本地曲目仍会被 `/` 的本地搜索查到，那走的是 songs 表而不是这个音源。
func (s *Source) Caps() source.Capability { return 0 }

// LoggedIn 恒为 true：本地文件不需要登录。
func (s *Source) LoggedIn() bool { return true }

// User 返回本机账号摘要。
func (s *Source) User() source.User {
	return source.User{ID: "local", Name: "本机", LoggedIn: true}
}

// Account 实现 source.Source；本地源没有可校验的登录态。
func (s *Source) Account(context.Context) (source.User, error) { return s.User(), nil }

// StartQRLogin 不会被调用（没有 CapAccount），仍返回明确错误便于排障。
func (s *Source) StartQRLogin(context.Context) (source.QRCode, error) {
	return source.QRCode{}, fmt.Errorf("本地源不需要登录")
}

// PollQRLogin 同 StartQRLogin。
func (s *Source) PollQRLogin(context.Context, string) (source.LoginStatus, error) {
	return source.LoginStatus{}, fmt.Errorf("本地源不需要登录")
}

// Search 返回空页；本地搜索走 songs 表。
func (s *Source) Search(context.Context, string, int) (source.SearchPage, error) {
	return source.SearchPage{}, nil
}

// RemotePlaylists 返回空：本地库没有远端歌单。
func (s *Source) RemotePlaylists(context.Context) ([]source.Playlist, error) { return nil, nil }

// PlaylistTracks 返回空：文件夹歌单由扫描物化，不走这里。
func (s *Source) PlaylistTracks(context.Context, string) ([]source.Track, error) {
	return nil, nil
}

// Session 返回空会话。
func (s *Source) Session() map[string]string { return nil }

// SetSession 空实现。
func (s *Source) SetSession(map[string]string) {}

// Logout 空实现：本地源没有登录态。
func (s *Source) Logout() {}

// LyricName 返回歌词来源展示名。
func (s *Source) LyricName() string { return "本地歌词" }

// Resolve 把本地曲目解析成本机文件路径。
//
// 同一份 meta 的多个文件共享一个 song_id，这里逐个 stat，取第一个仍然存在的；
// 都不在时报错，让调用方提示「本地文件缺失」。
func (s *Source) Resolve(ctx context.Context, ref source.Ref, quality source.Quality) (source.Resolved, error) {
	song, err := s.song(ref)
	if err != nil {
		return source.Resolved{}, err
	}
	path, err := s.songPath(song)
	if err != nil {
		return source.Resolved{}, err
	}
	duration := time.Duration(song.Duration) * time.Second
	if duration <= 0 {
		// 扫描阶段不逐文件 spawn ffprobe；第一次播放时补一次并回写。
		if seconds := probeDuration(ctx, path); seconds > 0 {
			duration = time.Duration(seconds) * time.Second
			if s.store != nil {
				_ = s.store.SetSongDurationIfEmpty(song.ID, seconds)
			}
			song.Duration = seconds
		}
	}
	return source.Resolved{
		Track: source.Track{
			Source:     source.Local,
			ExternalID: song.ExternalID(),
			Page:       song.Page,
			Title:      song.Title,
			Artist:     song.Artist,
			Album:      song.Album,
			Duration:   song.Duration,
			Cover:      song.Cover,
		},
		Stream: source.Stream{
			URL:      path,
			Codec:    library.CodecOf(path),
			Duration: duration,
			Quality:  "本地文件",
		},
	}, nil
}

// songPath 返回一首本地曲目当前可用的文件路径。
func (s *Source) songPath(song store.Song) (string, error) {
	if s.store == nil {
		return "", fmt.Errorf("本地源没有存储")
	}
	files, err := s.store.LibraryFilesBySong(song.ID)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		info, err := os.Stat(f.Path)
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		return f.Path, nil
	}
	return "", fmt.Errorf("本地文件不存在：%s", song.Title)
}

// song 按 Ref 取曲目行。
func (s *Source) song(ref source.Ref) (store.Song, error) {
	if s.store == nil {
		return store.Song{}, fmt.Errorf("本地源没有存储")
	}
	id := store.SongID(string(source.Local), ref.ExternalID, ref.Page)
	song, err := s.store.Song(id)
	if err != nil {
		return store.Song{}, fmt.Errorf("本地曲目不存在：%s", ref.ExternalID)
	}
	return song, nil
}

// probeDuration 用 ffprobe 取一次时长；取不到返回 0，不影响播放。
func probeDuration(ctx context.Context, path string) int {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, ffprobe,
		"-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		return 0
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return int(seconds)
}

// SongPath 供歌词绑定等场景取一首本地曲目的文件路径（取第一个存在的）。
func (s *Source) SongPath(songID string) (string, bool) {
	if s.store == nil || songID == "" {
		return "", false
	}
	song, err := s.store.Song(songID)
	if err != nil || song.Source != string(source.Local) {
		return "", false
	}
	path, err := s.songPath(song)
	if err != nil {
		return "", false
	}
	return path, true
}

// RootOf 返回一首本地曲目所在目录，供调节目里定位歌词。
func RootOf(path string) string { return filepath.Dir(path) }
