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
	defer bumpLibraryRevision()
	return s.store.CreatePlaylist(title)
}

// CreateLocalOnlyPlaylist 新建「仅本机」歌单：内容不进同步仓库。
func (s *Service) CreateLocalOnlyPlaylist(title string) (store.Playlist, error) {
	if s.store == nil {
		return store.Playlist{}, fmt.Errorf("没有可用的存储")
	}
	defer bumpLibraryRevision()
	return s.store.CreatePlaylistEx(title, true)
}

// CreateLocalOnlyPlaylistWithSongs 新建仅本机歌单并批量放入歌曲。
func (s *Service) CreateLocalOnlyPlaylistWithSongs(title string, songs []store.Song) (store.Playlist, error) {
	p, err := s.CreateLocalOnlyPlaylist(title)
	if err != nil {
		return store.Playlist{}, err
	}
	if _, err := s.AddSongsToPlaylist(p.ID, songs); err != nil {
		return store.Playlist{}, err
	}
	return p, nil
}

// CreatePlaylistWithSong 新建本地歌单并把一首歌放进去。
func (s *Service) CreatePlaylistWithSong(title string, song store.Song) (store.Playlist, error) {
	return s.CreatePlaylistWithSongs(title, []store.Song{song})
}

// CreatePlaylistWithSongs 新建本地歌单并批量放入歌曲，保持传入顺序。
func (s *Service) CreatePlaylistWithSongs(title string, songs []store.Song) (store.Playlist, error) {
	p, err := s.CreatePlaylist(title)
	if err != nil {
		return store.Playlist{}, err
	}
	if _, err := s.AddSongsToPlaylist(p.ID, songs); err != nil {
		return store.Playlist{}, err
	}
	return p, nil
}

// RenamePlaylist 重命名歌单。
func (s *Service) RenamePlaylist(id, title string) error {
	if s.store == nil {
		return fmt.Errorf("没有可用的存储")
	}
	defer bumpLibraryRevision()
	return s.store.RenamePlaylist(id, title)
}

// DeletePlaylist 删除歌单及其条目。
func (s *Service) DeletePlaylist(id string) error {
	if s.store == nil {
		return fmt.Errorf("没有可用的存储")
	}
	defer bumpLibraryRevision()
	return s.store.DeletePlaylist(id)
}

// DeletePlaylistCompletely 删除歌单及其本地条目；同步歌单会先解除同步再删除本地副本，
// 平台上的远端歌单不受影响。每日推荐入口由系统维护，不允许删除。
func (s *Service) DeletePlaylistCompletely(id string) error {
	if s.store == nil {
		return fmt.Errorf("没有可用的存储")
	}
	defer bumpLibraryRevision()
	p, err := s.store.Playlist(id)
	if err != nil {
		return err
	}
	if p.Kind == "remote" {
		if IsDailyPlaylist(p) {
			return fmt.Errorf("每日推荐入口不能删除")
		}
		// 先确认没有被混合歌单引用，避免解锁成功但删除失败留下半套状态。
		referenced, err := s.store.IsReferenced(id)
		if err != nil {
			return err
		}
		if referenced {
			return fmt.Errorf("歌单被其他混合歌单引用，先移除引用")
		}
		if _, err := s.store.UnlockPlaylist(id); err != nil {
			return err
		}
	}
	return s.store.DeletePlaylist(id)
}

// UnlockPlaylist 把同步歌单解锁为本地歌单，保留曲目并停止远端同步。
func (s *Service) UnlockPlaylist(id string) (store.Playlist, error) {
	if s.store == nil {
		return store.Playlist{}, fmt.Errorf("没有可用的存储")
	}
	defer bumpLibraryRevision()
	return s.store.UnlockPlaylist(id)
}

// AddSongToPlaylist 把歌曲写入本地/混合歌单。
func (s *Service) AddSongToPlaylist(id string, song store.Song) error {
	if s.store == nil {
		return fmt.Errorf("没有可用的存储")
	}
	defer bumpLibraryRevision()
	return s.store.AddSongToPlaylist(id, store.NormalizeSong(song))
}

// AddSongsToPlaylist 按顺序把多首歌写入本地/混合歌单，返回成功写入的数量。
// 遇到错误立即停止，已写入的歌曲保留（与逐首手动加入的语义一致）。
func (s *Service) AddSongsToPlaylist(id string, songs []store.Song) (int, error) {
	added := 0
	for _, song := range songs {
		if err := s.AddSongToPlaylist(id, song); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// RemoveSongFromPlaylist 从歌单移除歌曲；远端歌单只写 exclusion。
func (s *Service) RemoveSongFromPlaylist(id, songID string) error {
	if s.store == nil {
		return fmt.Errorf("没有可用的存储")
	}
	defer bumpLibraryRevision()
	return s.store.RemoveSongFromPlaylist(id, songID)
}

// ImportRemotePlaylist 把远端收藏夹/歌单加入本地镜像。
func (s *Service) ImportRemotePlaylist(p source.Playlist) (store.Playlist, error) {
	if s.store == nil {
		return store.Playlist{}, fmt.Errorf("没有可用的存储")
	}
	defer bumpLibraryRevision()
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
	defer bumpLibraryRevision()
	return s.store.SetMixinRefs(id, refIDs)
}

// AddMixinRef 追加一个引用；已存在时返回 added=false。
func (s *Service) AddMixinRef(id, refID string) (bool, error) {
	if s.store == nil {
		return false, fmt.Errorf("没有可用的存储")
	}
	defer bumpLibraryRevision()
	return s.store.AddMixinRef(id, refID)
}
