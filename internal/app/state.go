package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// CreateMixin 新建一个混合歌单并写入引用。
func (s *Service) CreateMixin(title string, refIDs []string) (store.Playlist, error) {
	if s.store == nil {
		return store.Playlist{}, fmt.Errorf("没有可用的存储")
	}
	defer bumpLibraryRevision()
	return s.store.CreateMixin(title, refIDs)
}

// LastAddPlaylist 返回上次加入歌单的目标 ID。
func (s *Service) LastAddPlaylist() string {
	if s.store == nil {
		return ""
	}
	return s.store.Setting("last_add_playlist", "")
}

// SaveLastAddPlaylist 记住上次加入歌单的目标。
func (s *Service) SaveLastAddPlaylist(id string) {
	if s.store != nil {
		_ = s.store.SetSetting("last_add_playlist", id)
	}
}

// Clip 读取歌曲片段设置。
func (s *Service) Clip(songID string) (store.Clip, error) {
	if s.store == nil {
		return store.Clip{}, fmt.Errorf("没有可用的存储")
	}
	return s.store.Clip(songID)
}

// SaveClip 保存歌曲片段设置。
func (s *Service) SaveClip(songID string, clip store.Clip) error {
	if s.store == nil {
		return fmt.Errorf("没有可用的存储")
	}
	defer bumpLibraryRevision()
	return s.store.SetClip(songID, clip)
}

// SearchSongs 在本地曲库中搜索。
func (s *Service) SearchSongs(keyword string, limit int) ([]store.Song, error) {
	if s.store == nil {
		return nil, fmt.Errorf("没有可用的存储")
	}
	return s.store.SearchSongs(keyword, limit)
}

// UpsertSong 把歌曲写入/更新到本地库；没有存储时原样返回。
func (s *Service) UpsertSong(song store.Song) store.Song {
	song = store.NormalizeSong(song)
	if s.store == nil {
		return song
	}
	if stored, err := s.store.UpsertSong(song); err == nil {
		return stored
	}
	return song
}

// SaveQueue 持久化播放队列与进度。
func (s *Service) SaveQueue(songs []store.Song, index int, positionMS int64) error {
	if s.store == nil {
		return nil
	}
	ids := make([]string, 0, len(songs))
	for _, song := range songs {
		ids = append(ids, store.NormalizeSong(song).ID)
	}
	if positionMS < 0 {
		positionMS = 0
	}
	return s.store.SaveQueue(store.QueueState{IDs: ids, Index: index, Position: positionMS})
}

// SaveAccount 持久化某音源的登录态。
func (s *Service) SaveAccount(id source.ID, session map[string]string) error {
	if s.store == nil {
		return fmt.Errorf("没有可用的存储")
	}
	defer bumpLibraryRevision()
	payload, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return s.store.SaveAccount(string(id), payload)
}

// refreshMinAge 是两次主动续期之间的最小间隔。登录态刚存过就不再换发，
// 否则频繁重启会反复轮换 token，把同账号的其它客户端顶掉。
const refreshMinAge = 6 * time.Hour

// RefreshSessions 给支持续期的已登录音源换发登录态，并把新会话写回数据库。
//
// 续期失败不在这里清理账号：网络错误留给下一次重试，平台判定的过期仍由
// Users 统一处理，避免一次网络抖动就把歌单冻结。
func (s *Service) RefreshSessions(ctx context.Context) []source.ID {
	if s.sources == nil {
		return nil
	}
	var refreshed []source.ID
	for _, src := range s.sources.All() {
		if !src.LoggedIn() {
			continue
		}
		refresher, ok := src.(source.SessionRefresher)
		if !ok {
			continue
		}
		if s.sessionFreshEnough(src.ID()) {
			continue
		}
		if err := refresher.RefreshSession(ctx); err != nil {
			continue
		}
		if s.persistSession(src) {
			refreshed = append(refreshed, src.ID())
		}
	}
	return refreshed
}

// sessionFreshEnough 判断登录态是不是刚存过；续期本身会写库，
// 所以 updated_at 同时就是上一次续期的时间。
func (s *Service) sessionFreshEnough(id source.ID) bool {
	if s.store == nil {
		return false
	}
	_, updatedAt, err := s.store.LoadAccountAt(string(id))
	if err != nil || updatedAt <= 0 {
		return false
	}
	return time.Since(time.Unix(updatedAt, 0)) < refreshMinAge
}

// persistSession 只在会话真的变化时写库。账号校验每 15 分钟跑一次，如果
// 每次都写会反复刷新 updated_at、解冻歌单，还会让同步误以为库被改过。
func (s *Service) persistSession(src source.Source) bool {
	if s.store == nil || !src.LoggedIn() {
		return false
	}
	payload, err := json.Marshal(src.Session())
	if err != nil {
		return false
	}
	if old, err := s.store.LoadAccount(string(src.ID())); err == nil && bytes.Equal(old, payload) {
		return false
	}
	if err := s.store.SaveAccount(string(src.ID()), payload); err != nil {
		return false
	}
	return true
}

// Logout 清除某音源的登录态，并冻结它的远端歌单镜像。
// 镜像、缓存曲目与混合歌单引用都保留；重新登录后自动解冻并恢复刷新。
func (s *Service) Logout(id source.ID) {
	if s.sources != nil {
		if src := s.sources.Get(id); src != nil {
			src.Logout()
		}
	}
	if s.store == nil {
		return
	}
	defer bumpLibraryRevision()
	_ = s.store.DeleteAccount(string(id))
	_, _ = s.store.FreezeRemotePlaylistsBySource(string(id))
}
