package app

import (
	"fmt"

	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// CreatePlaylist 新建一个本地歌单。
func (s *Service) CreatePlaylist(title string) (store.Playlist, error) {
	if s.store == nil {
		return store.Playlist{}, fmt.Errorf("没有可用的存储")
	}
	return s.store.CreatePlaylist(title)
}

// CreatePlaylistWithSong 新建本地歌单并把一首歌放进去。
func (s *Service) CreatePlaylistWithSong(title string, song store.Song) (store.Playlist, error) {
	p, err := s.CreatePlaylist(title)
	if err != nil {
		return store.Playlist{}, err
	}
	if err := s.AddSongToPlaylist(p.ID, song); err != nil {
		return store.Playlist{}, err
	}
	return p, nil
}

// RenamePlaylist 重命名歌单。
func (s *Service) RenamePlaylist(id, title string) error {
	if s.store == nil {
		return fmt.Errorf("没有可用的存储")
	}
	return s.store.RenamePlaylist(id, title)
}

// DeletePlaylist 删除歌单及其条目。
func (s *Service) DeletePlaylist(id string) error {
	if s.store == nil {
		return fmt.Errorf("没有可用的存储")
	}
	return s.store.DeletePlaylist(id)
}

// UnlockPlaylist 把同步歌单解锁为本地歌单，保留曲目并停止远端同步。
func (s *Service) UnlockPlaylist(id string) (store.Playlist, error) {
	if s.store == nil {
		return store.Playlist{}, fmt.Errorf("没有可用的存储")
	}
	return s.store.UnlockPlaylist(id)
}

// AddSongToPlaylist 把歌曲写入本地/混合歌单。
func (s *Service) AddSongToPlaylist(id string, song store.Song) error {
	if s.store == nil {
		return fmt.Errorf("没有可用的存储")
	}
	return s.store.AddSongToPlaylist(id, store.NormalizeSong(song))
}

// RemoveSongFromPlaylist 从歌单移除歌曲；远端歌单只写 exclusion。
func (s *Service) RemoveSongFromPlaylist(id, songID string) error {
	if s.store == nil {
		return fmt.Errorf("没有可用的存储")
	}
	return s.store.RemoveSongFromPlaylist(id, songID)
}

// ImportRemotePlaylist 把远端收藏夹/歌单加入本地镜像。
func (s *Service) ImportRemotePlaylist(p source.Playlist) (store.Playlist, error) {
	if s.store == nil {
		return store.Playlist{}, fmt.Errorf("没有可用的存储")
	}
	return s.store.EnsurePlaylist(store.Playlist{
		ID:       p.ID,
		Source:   string(p.Source),
		Title:    p.Title,
		Kind:     "remote",
		RemoteID: p.RemoteID,
	})
}

// MixinRefs 返回混合歌单引用的歌单列表。
func (s *Service) MixinRefs(id string) ([]store.Playlist, error) {
	if s.store == nil {
		return nil, fmt.Errorf("没有可用的存储")
	}
	return s.store.MixinRefs(id)
}

// SetMixinRefs 覆盖混合歌单的引用列表。
func (s *Service) SetMixinRefs(id string, refIDs []string) error {
	if s.store == nil {
		return fmt.Errorf("没有可用的存储")
	}
	return s.store.SetMixinRefs(id, refIDs)
}

// AddMixinRef 追加一个引用；已存在时返回 added=false。
func (s *Service) AddMixinRef(id, refID string) (bool, error) {
	if s.store == nil {
		return false, fmt.Errorf("没有可用的存储")
	}
	return s.store.AddMixinRef(id, refID)
}
