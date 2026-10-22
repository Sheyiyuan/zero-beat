package app

import (
	"strconv"

	"github.com/sheyiyuan/zero-beat/internal/source"
)

// Preferences 是跨会话保存的播放偏好。
type Preferences struct {
	Volume          float64
	CompDB          float64
	Quality         source.Quality
	Mode            int
	PlayingPlaylist string
}

// Preferences 从 settings 表读取播放偏好，并为缺失项提供默认值。
func (s *Service) Preferences() Preferences {
	prefs := Preferences{Volume: 0.8}
	if s.store == nil {
		return prefs
	}
	if v, err := strconv.ParseFloat(s.store.Setting("volume", "0.8"), 64); err == nil {
		prefs.Volume = v
	}
	if v, err := strconv.ParseFloat(s.store.Setting("comp_db", "0"), 64); err == nil {
		prefs.CompDB = v
	}
	if v, err := strconv.Atoi(s.store.Setting("mode", "0")); err == nil && v >= 0 && v <= 2 {
		prefs.Mode = v
	}
	if v, err := strconv.Atoi(s.store.Setting("quality", "0")); err == nil {
		prefs.Quality = source.ParseQuality(v)
	}
	prefs.PlayingPlaylist = s.store.Setting("playing_playlist", "")
	return prefs
}

// SaveVolume 持久化音量。
func (s *Service) SaveVolume(v float64) {
	if s.store != nil {
		_ = s.store.SetSetting("volume", strconv.FormatFloat(v, 'f', 2, 64))
	}
}

// SaveCompDB 持久化响度补偿。
func (s *Service) SaveCompDB(v float64) {
	if s.store != nil {
		_ = s.store.SetSetting("comp_db", strconv.FormatFloat(v, 'f', 1, 64))
	}
}

// SaveQuality 持久化音质偏好。
func (s *Service) SaveQuality(q source.Quality) {
	if s.store != nil {
		_ = s.store.SetSetting("quality", strconv.Itoa(int(q)))
	}
}

// SaveMode 持久化播放模式。
func (s *Service) SaveMode(mode int) {
	if s.store != nil {
		_ = s.store.SetSetting("mode", strconv.Itoa(mode))
	}
}

// SavePlayingPlaylist 持久化当前正在播放的歌单。
func (s *Service) SavePlayingPlaylist(id string) {
	if s.store != nil {
		_ = s.store.SetSetting("playing_playlist", id)
	}
}
