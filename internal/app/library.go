package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// defaultPlaylistMarker 标记默认本地歌单已经创建过；删掉后不再自动重建。
const defaultPlaylistMarker = "ui.default_playlist_created"

// EnsureDefaultPlaylist 只在首次启动时补一个默认本地歌单。
// 之后用户删掉它也不会「删了又回来」；已有任何本地歌单时也不会再补。
func EnsureDefaultPlaylist(st *store.Store) error {
	if st == nil {
		return nil
	}
	if st.Setting(defaultPlaylistMarker, "") == "1" {
		return nil
	}
	if _, err := st.Playlist("local-default"); err == nil {
		return st.SetSetting(defaultPlaylistMarker, "1")
	}
	items, err := st.ListPlaylists()
	if err != nil {
		return err
	}
	for _, p := range items {
		if p.Kind == "local" {
			return st.SetSetting(defaultPlaylistMarker, "1")
		}
	}
	if _, err := st.EnsurePlaylist(store.Playlist{ID: "local-default", Title: "本地歌单", Kind: "local"}); err != nil {
		return err
	}
	return st.SetSetting(defaultPlaylistMarker, "1")
}

// DailyPlaylistID 返回某音源「每日推荐」入口的稳定 ID。
func DailyPlaylistID(id source.ID) string { return string(id) + "-daily" }

// IsDailyPlaylist 判断歌单是否是某音源的每日推荐入口。
func IsDailyPlaylist(p store.Playlist) bool {
	return p.Kind == "remote" &&
		p.RemoteID == "daily" &&
		p.Source != "" &&
		p.ID == DailyPlaylistID(source.ID(p.Source))
}

// Playlists 确保默认歌单与各音源的每日推荐入口存在，再返回全部歌单。
func (s *Service) Playlists(ctx context.Context) (items []store.Playlist, warn, err error) {
	if s.store == nil {
		return nil, nil, fmt.Errorf("没有可用的存储")
	}
	if e := EnsureDefaultPlaylist(s.store); e != nil {
		warn = e
	}
	if s.sources != nil {
		for _, src := range s.sources.All() {
			if !src.LoggedIn() || !src.Caps().Has(source.CapDailyRecommendations) {
				continue
			}
			if _, ok := src.(source.DailyRecommender); !ok {
				continue
			}
			if _, e := s.store.EnsurePlaylist(store.Playlist{
				ID:       DailyPlaylistID(src.ID()),
				Source:   string(src.ID()),
				Title:    "每日推荐",
				Kind:     "remote",
				RemoteID: "daily",
			}); e != nil && warn == nil {
				warn = e
			}
		}
	}
	items, err = s.store.ListPlaylists()
	return items, warn, err
}

// LocalPlaylists 只读本地库，不触碰远端接口。
func (s *Service) LocalPlaylists() (items []store.Playlist, warn, err error) {
	if s.store == nil {
		return nil, nil, fmt.Errorf("没有可用的存储")
	}
	if e := EnsureDefaultPlaylist(s.store); e != nil {
		warn = e
	}
	items, err = s.store.ListPlaylists()
	return items, warn, err
}

// RemoteFolders 拉取一个音源的远端收藏夹/歌单，过滤掉已经加入本地的。
func (s *Service) RemoteFolders(ctx context.Context, id source.ID) ([]source.Playlist, error) {
	src := s.sources.Get(id)
	if src == nil {
		return nil, fmt.Errorf("音源 %q 不可用", id)
	}
	if !src.LoggedIn() {
		return nil, fmt.Errorf("请先登录 %s", src.Name())
	}
	lists, err := src.RemotePlaylists(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s 歌单读取失败：%w", src.Name(), err)
	}
	added := map[string]bool{}
	if s.store != nil {
		if existing, lerr := s.store.ListPlaylists(); lerr == nil {
			for _, p := range existing {
				added[p.ID] = true
			}
		}
	}
	out := make([]source.Playlist, 0, len(lists))
	for _, p := range lists {
		if !added[p.ID] {
			out = append(out, p)
		}
	}
	return out, nil
}

// PlaylistSongs 读取一个歌单的曲目；远端歌单按需刷新并写回本地缓存。
func (s *Service) PlaylistSongs(ctx context.Context, p store.Playlist, force bool) (items []store.Song, warn, err error) {
	if s.store == nil {
		return nil, nil, fmt.Errorf("没有可用的存储")
	}
	if p.Kind == "mixin" {
		warn = s.refreshMixinRefs(ctx, p)
		items, err = s.store.PlaylistSongs(p.ID)
		return items, warn, err
	}

	isDaily := IsDailyPlaylist(p)
	needRemote := p.Kind == "remote" && (force || p.Count == 0 || isDaily)
	if needRemote {
		srcID := strings.TrimSpace(p.Source)
		if srcID == "" {
			warn = fmt.Errorf("歌单 %s 缺少音源信息", p.Title)
		} else if src := s.sources.GetString(srcID); src == nil {
			warn = fmt.Errorf("歌单 %s 的音源 %q 不可用", p.Title, srcID)
		} else {
			var tracks []source.Track
			var rerr error
			supported := true
			if isDaily {
				rec, ok := src.(source.DailyRecommender)
				if !ok {
					warn = fmt.Errorf("%s 不支持每日推荐", src.Name())
					supported = false
				} else {
					tracks, rerr = rec.DailyRecommendations(ctx)
				}
			} else {
				tracks, rerr = src.PlaylistTracks(ctx, p.RemoteID)
			}
			switch {
			case !supported:
			case rerr != nil:
				warn = fmt.Errorf("%s 歌单读取失败：%w", src.Name(), rerr)
			default:
				songs := make([]store.Song, 0, len(tracks))
				for _, t := range tracks {
					songs = append(songs, SongFromTrack(t))
				}
				if rerr := s.store.ReplacePlaylistSongs(p.ID, songs); rerr != nil {
					warn = rerr
				}
			}
		}
	}
	items, err = s.store.PlaylistSongs(p.ID)
	return items, warn, err
}

// refreshMixinRefs 在打开混合歌单时刷新其中的远端引用；失败时保留缓存并返回警告。
func (s *Service) refreshMixinRefs(ctx context.Context, p store.Playlist) error {
	if s.sources == nil {
		return nil
	}
	refs, err := s.store.MixinRefs(p.ID)
	if err != nil {
		return err
	}
	var warn error
	for _, ref := range refs {
		if ref.Kind != "remote" {
			continue
		}
		src := s.sources.GetString(ref.Source)
		if src == nil || !src.LoggedIn() {
			continue
		}
		var tracks []source.Track
		if IsDailyPlaylist(ref) {
			rec, ok := src.(source.DailyRecommender)
			if !ok {
				continue
			}
			tracks, err = rec.DailyRecommendations(ctx)
		} else {
			tracks, err = src.PlaylistTracks(ctx, ref.RemoteID)
		}
		if err != nil {
			if warn == nil {
				warn = fmt.Errorf("%s 同步失败：%w", ref.Title, err)
			}
			continue
		}
		songs := make([]store.Song, 0, len(tracks))
		for _, t := range tracks {
			songs = append(songs, SongFromTrack(t))
		}
		if err := s.store.ReplacePlaylistSongs(ref.ID, songs); err != nil && warn == nil {
			warn = err
		}
	}
	return warn
}

// AddToCloudPlaylist 把一首歌写入同源云端歌单。
func (s *Service) AddToCloudPlaylist(ctx context.Context, song store.Song, remoteID string) error {
	song = store.NormalizeSong(song)
	src := s.sources.GetString(song.Source)
	if src == nil {
		return fmt.Errorf("未知音源 %s", song.Source)
	}
	writer, ok := src.(source.PlaylistWriter)
	if !ok || !src.Caps().Has(source.CapPlaylistWrite) {
		return fmt.Errorf("%s 不支持写入云端歌单", src.Name())
	}
	return writer.AddToPlaylist(ctx, remoteID, RefFromSong(song))
}

// CloudPlaylists 返回某音源可用于写入的云端歌单；每日推荐是动态列表，排除。
func (s *Service) CloudPlaylists(ctx context.Context, id source.ID) ([]source.Playlist, error) {
	src := s.sources.Get(id)
	if src == nil {
		return nil, fmt.Errorf("音源不可用")
	}
	lists, err := src.RemotePlaylists(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]source.Playlist, 0, len(lists))
	for _, p := range lists {
		if p.RemoteID == "" || p.RemoteID == "daily" {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}
