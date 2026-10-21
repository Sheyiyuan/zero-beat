// Package bilibili 把 B 站客户端适配成统一的 source.Source。
package bilibili

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/bili"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
)

// Source 是 B 站音源。
var _ source.PlaylistWriter = (*Source)(nil)

type Source struct {
	client *bili.Client
}

// New 包装一个 B 站客户端。
func New(client *bili.Client) *Source {
	return &Source{client: client}
}

// ID 实现 source.Source。
func (s *Source) ID() source.ID { return source.Bilibili }

// Name 实现 source.Source。
func (s *Source) Name() string { return "Bilibili" }

// Caps 实现 source.Source。
func (s *Source) Caps() source.Capability {
	return source.CapSearch | source.CapMultiPart | source.CapRemotePlaylists |
		source.CapOfficialLyrics | source.CapQualitySelect | source.CapLossless |
		source.CapPlaylistWrite
}

// LoggedIn 实现 source.Source。
func (s *Source) LoggedIn() bool {
	return s != nil && s.client != nil && s.client.LoggedIn()
}

// User 实现 source.Source。
func (s *Source) User() source.User {
	if s == nil || s.client == nil {
		return source.User{}
	}
	u := s.client.User()
	return source.User{
		ID:       strconv.FormatInt(u.Mid, 10),
		Name:     u.Name,
		Avatar:   u.Face,
		LoggedIn: u.LoggedIn,
	}
}

// Account 实现 source.Source。
func (s *Source) Account(ctx context.Context) (source.User, error) {
	u, err := s.client.Nav(ctx)
	if err != nil {
		return source.User{}, err
	}
	return source.User{
		ID:       strconv.FormatInt(u.Mid, 10),
		Name:     u.Name,
		Avatar:   u.Face,
		LoggedIn: u.LoggedIn,
	}, nil
}

// StartQRLogin 实现 source.Source。
func (s *Source) StartQRLogin(ctx context.Context) (source.QRCode, error) {
	qr, err := s.client.StartQRLogin(ctx)
	if err != nil {
		return source.QRCode{}, err
	}
	return source.QRCode{Key: qr.Key, URL: qr.URL}, nil
}

// PollQRLogin 实现 source.Source。
func (s *Source) PollQRLogin(ctx context.Context, key string) (source.LoginStatus, error) {
	st, err := s.client.PollQRLogin(ctx, key)
	if err != nil {
		return source.LoginStatus{}, err
	}
	return source.LoginStatus{
		Code:     st.Code,
		Message:  st.Message,
		LoggedIn: st.LoggedIn,
		Expired:  st.Code == 86038,
	}, nil
}

// Search 实现 source.Source。
func (s *Source) Search(ctx context.Context, keyword string, page int) (source.SearchPage, error) {
	items, pages, err := s.client.Search(ctx, keyword, page)
	if err != nil {
		return source.SearchPage{}, err
	}
	out := source.SearchPage{Page: page, Pages: pages}
	for _, it := range items {
		out.Candidates = append(out.Candidates, source.Candidate{
			Track: source.Track{
				Source:     source.Bilibili,
				ExternalID: it.BVID,
				Page:       1,
				Title:      it.Title,
				Artist:     it.Author,
				Duration:   it.Duration,
				Cover:      it.Cover,
			},
			Play: it.Play,
		})
	}
	return out, nil
}

// Resolve 实现 source.Source。
func (s *Source) Resolve(ctx context.Context, ref source.Ref, quality source.Quality) (source.Resolved, error) {
	page := ref.Page
	if page <= 0 {
		page = 1
	}
	track := source.Track{
		Source:     source.Bilibili,
		ExternalID: ref.ExternalID,
		Page:       page,
		CID:        ref.CID,
	}
	cid := ref.CID
	if cid == 0 {
		info, err := s.client.VideoInfo(ctx, ref.ExternalID)
		if err != nil {
			return source.Resolved{}, err
		}
		chosen := bili.VideoPage{}
		for _, p := range info.Pages {
			if p.Page == page {
				chosen = p
				break
			}
		}
		if chosen.CID == 0 && len(info.Pages) > 0 {
			chosen = info.Pages[0]
			track.Page = chosen.Page
		}
		if chosen.CID == 0 {
			return source.Resolved{}, fmt.Errorf("bilibili: 视频 %s 没有可播放分 P", ref.ExternalID)
		}
		cid = chosen.CID
		track.CID = cid
		track.Title = info.Title
		if chosen.Page > 1 && chosen.Part != "" {
			track.Title = info.Title + " · " + chosen.Part
		}
		track.Artist = info.Owner
		track.Cover = info.Cover
		if chosen.Duration > 0 {
			track.Duration = int(chosen.Duration)
		} else {
			track.Duration = int(info.Duration)
		}
	}
	stream, err := s.client.PlayURL(ctx, ref.ExternalID, cid, mapQuality(quality))
	if err != nil {
		return source.Resolved{}, err
	}
	if track.Duration == 0 && stream.DurationMS > 0 {
		track.Duration = int((stream.DurationMS + 500) / 1000)
	}
	return source.Resolved{
		Track: track,
		Stream: source.Stream{
			URL:       stream.URL,
			Headers:   stream.Headers,
			Codec:     stream.Codec,
			Duration:  timeMS(stream.DurationMS),
			ExpiresAt: stream.ExpiresAt,
			Quality:   stream.Quality,
		},
	}, nil
}

// Lyrics 实现 source.Source。
func (s *Source) Lyrics(ctx context.Context, ref source.Ref) ([]lyrics.Line, string, error) {
	if ref.CID == 0 {
		return nil, "", nil
	}
	subs, err := s.client.Subtitles(ctx, ref.ExternalID, ref.CID)
	if err != nil {
		return nil, "", err
	}
	out := make([]lyrics.Line, 0, len(subs))
	for _, l := range subs {
		if l.Text == "" {
			continue
		}
		out = append(out, lyrics.Line{StartMS: l.StartMS, Text: l.Text})
	}
	if len(out) == 0 {
		return nil, "", nil
	}
	return out, "B站字幕", nil
}

// RemotePlaylists 实现 source.Source。
func (s *Source) RemotePlaylists(ctx context.Context) ([]source.Playlist, error) {
	folders, err := s.client.FavoriteFolders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]source.Playlist, 0, len(folders))
	for _, f := range folders {
		remoteID := strconv.FormatInt(f.MediaID, 10)
		out = append(out, source.Playlist{
			ID:       "bili-fav-" + remoteID,
			Source:   source.Bilibili,
			RemoteID: remoteID,
			Title:    f.Title,
			Count:    f.Count,
		})
	}
	return out, nil
}

// PlaylistTracks 实现 source.Source。
func (s *Source) PlaylistTracks(ctx context.Context, remoteID string) ([]source.Track, error) {
	mediaID, err := strconv.ParseInt(remoteID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("bilibili: 收藏夹 ID 无效: %s", remoteID)
	}
	favs, err := s.client.AllFavorites(ctx, mediaID, 300)
	if err != nil {
		return nil, err
	}
	out := make([]source.Track, 0, len(favs))
	for _, f := range favs {
		out = append(out, source.Track{
			Source:     source.Bilibili,
			ExternalID: f.BVID,
			Page:       1,
			Title:      f.Title,
			Artist:     f.Artist,
			Duration:   f.Duration,
			Cover:      f.Cover,
		})
	}
	return out, nil
}

// Session 实现 source.Source。
func (s *Source) Session() map[string]string { return s.client.Cookies() }

// SetSession 实现 source.Source。
func (s *Source) SetSession(cookies map[string]string) { s.client.SetCookies(cookies) }

// Logout 实现 source.Source。
func (s *Source) Logout() { s.client.Logout() }

// AddToPlaylist 实现 source.PlaylistWriter；B 站只有收藏夹这一种云端歌单。
func (s *Source) AddToPlaylist(ctx context.Context, remoteID string, ref source.Ref) error {
	if ref.Source != source.Bilibili {
		return fmt.Errorf("bilibili: 不能把 %s 的歌曲加入 B 站收藏夹", ref.Source)
	}
	mediaID, err := strconv.ParseInt(remoteID, 10, 64)
	if err != nil {
		return fmt.Errorf("bilibili: 收藏夹 ID 无效: %s", remoteID)
	}
	return s.client.AddFavorite(ctx, mediaID, ref.ExternalID)
}

func mapQuality(q source.Quality) bili.Quality {
	switch q {
	case source.QualityHigh:
		return bili.QualityHigh
	case source.QualityLossless:
		return bili.QualityLossless
	default:
		return bili.QualityAuto
	}
}

func timeMS(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }
