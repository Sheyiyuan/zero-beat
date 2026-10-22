package app

import (
	"encoding/json"
	"fmt"

	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// CreateMixin 新建一个混合歌单并写入引用。
func (s *Service) CreateMixin(title string, refIDs []string) (store.Playlist, error) {
	if s.store == nil {
		return store.Playlist{}, fmt.Errorf("没有可用的存储")
	}
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
	return s.store.SetClip(songID, clip)
}

// SaveLyricOffset 保存单曲歌词偏移。
func (s *Service) SaveLyricOffset(songID string, offsetMS int) {
	if s.store != nil {
		_ = s.store.SetSetting("lyric_offset:"+songID, fmt.Sprint(offsetMS))
	}
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
	payload, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return s.store.SaveAccount(string(id), payload)
}

// DeleteAccount 删除某音源的登录态与远端歌单镜像。
func (s *Service) DeleteAccount(id source.ID) {
	if s.store == nil {
		return
	}
	_ = s.store.DeleteAccount(string(id))
	_ = s.store.DeleteRemotePlaylistsBySource(string(id))
}
