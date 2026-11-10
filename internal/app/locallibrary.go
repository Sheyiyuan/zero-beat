package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sheyiyuan/zero-beat/internal/library"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// ExpandPath 把用户输入的路径补成绝对路径：支持 ~ 与相对路径。
// TUI 的输入框和 CLI 共用这一个入口，避免两边对 ~ 的处理不一致。
func ExpandPath(input string) (string, error) {
	path := strings.TrimSpace(input)
	if path == "" {
		return "", fmt.Errorf("路径不能为空")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("无法确定用户目录：%w", err)
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("无法解析路径：%w", err)
		}
		path = abs
	}
	return filepath.Clean(path), nil
}

// LibraryScan 是一次文件夹扫描的结果摘要，供界面提示。
type LibraryScan struct {
	Playlist store.Playlist
	Subdirs  int
	Scanned  int
	Added    int
	Reused   int
	Missing  int
	Warnings []string
}

// AddLocalFolder 把一个目录添加成只读文件夹歌单，并立即扫描一次。
//
// 路径校验在扫描前完成：用户输入错路径时不该留下一个空歌单。
func (s *Service) AddLocalFolder(path string) (store.Playlist, LibraryScan, error) {
	if s.store == nil {
		return store.Playlist{}, LibraryScan{}, fmt.Errorf("没有可用的存储")
	}
	abs, err := ExpandPath(path)
	if err != nil {
		return store.Playlist{}, LibraryScan{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return store.Playlist{}, LibraryScan{}, fmt.Errorf("文件夹不可用：%w", err)
	}
	if !info.IsDir() {
		return store.Playlist{}, LibraryScan{}, fmt.Errorf("不是文件夹：%s", abs)
	}
	playlist, err := s.store.CreateFolderPlaylist("", abs)
	if err != nil {
		return store.Playlist{}, LibraryScan{}, err
	}
	scan, err := s.RescanLibrary(playlist.ID)
	if err != nil {
		return playlist, scan, err
	}
	return scan.Playlist, scan, nil
}

// RescanLibrary 重新扫描一个文件夹歌单并替换其内容。
//
// 目录不可用时不覆盖已有条目，只更新 scan_status；文件级问题记 warning，
// 不中断整次扫描。
func (s *Service) RescanLibrary(playlistID string) (LibraryScan, error) {
	if s.store == nil {
		return LibraryScan{}, fmt.Errorf("没有可用的存储")
	}
	playlist, err := s.store.Playlist(playlistID)
	if err != nil {
		return LibraryScan{}, err
	}
	if playlist.Kind != store.PlaylistKindFolder {
		return LibraryScan{}, fmt.Errorf("不是文件夹歌单")
	}
	out := LibraryScan{Playlist: playlist}
	if playlist.RootPath == "" {
		_ = s.store.UpdatePlaylistScan(playlist.ID, "error", "没有设置文件夹路径")
		return out, fmt.Errorf("歌单没有设置文件夹路径")
	}
	if info, err := os.Stat(playlist.RootPath); err != nil || !info.IsDir() {
		// 目录暂时不可达（移动硬盘没挂载、U 盘拔了）时保留上次内容。
		_ = s.store.UpdatePlaylistScan(playlist.ID, "missing", "文件夹不可用")
		return out, fmt.Errorf("文件夹不可用：%s", playlist.RootPath)
	}
	result, err := library.Scan(playlist.RootPath)
	if err != nil {
		_ = s.store.UpdatePlaylistScan(playlist.ID, "error", err.Error())
		return out, err
	}
	out.Subdirs = result.Subdirs
	out.Scanned = len(result.Files)
	for _, w := range result.Warnings {
		out.Warnings = append(out.Warnings, w.Reason+"："+w.Path)
	}

	previous, err := s.store.LibraryFiles(playlist.ID)
	if err != nil {
		return out, err
	}
	wasPresent := map[string]bool{}
	for _, f := range previous {
		if f.Present {
			wasPresent[f.Path] = true
		}
	}

	items := make([]store.LibraryItem, 0, len(result.Files))
	hashes := map[string]map[string]bool{}
	now := map[string]bool{}
	for _, file := range result.Files {
		song, reused, warnings := s.identifyFile(playlist.ID, file)
		out.Warnings = append(out.Warnings, warnings...)
		if reused {
			out.Reused++
		} else {
			out.Added++
		}
		if hashes[song.ID] == nil {
			hashes[song.ID] = map[string]bool{}
		}
		if !hashes[song.ID][file.ContentHash] {
			hashes[song.ID][file.ContentHash] = true
			if len(hashes[song.ID]) > 1 {
				out.Warnings = append(out.Warnings,
					fmt.Sprintf("「%s」在同一目录有多个不同版本，已合并为一条", song.Title))
			}
		}
		now[file.Path] = true
		items = append(items, store.LibraryItem{
			Song:        song,
			Path:        file.Path,
			Size:        file.Size,
			ModTime:     file.ModTime,
			ContentHash: file.ContentHash,
		})
	}
	if err := s.store.ReplaceLibraryItems(playlist.ID, items); err != nil {
		_ = s.store.UpdatePlaylistScan(playlist.ID, "error", err.Error())
		return out, err
	}
	// 扫描结果只影响仅本机数据（文件夹歌单与本地曲目都不进同步），
	// 所以不 bump 同步版本号：否则每次打开歌单都会触发一轮无意义的同步。
	for path := range wasPresent {
		if !now[path] {
			out.Missing++
		}
	}
	if refreshed, err := s.store.Playlist(playlist.ID); err == nil {
		out.Playlist = refreshed
	}
	return out, nil
}

// identifyFile 按「路径 → 内容指纹 → 下载映射 → 元数据键 → 新建」定位曲目身份。
//
// 这个顺序保证了：改标签（路径不变）不换 ID；文件改名/移动（指纹不变）不换
// ID；下载目录里的文件继续用平台曲目身份，不会多出一份本地副本。
func (s *Service) identifyFile(playlistID string, file library.File) (store.Song, bool, []string) {
	var warnings []string
	if lf, err := s.store.LibraryFile(playlistID, file.Path); err == nil {
		if song, err := s.store.Song(lf.SongID); err == nil && s.platformSongStillApplies(song, file.Path) {
			return refreshLocalSong(song, file), true, nil
		}
	}
	if songID, err := s.store.SongByContentHash(file.ContentHash); err == nil {
		if song, err := s.store.Song(songID); err == nil && s.platformSongStillApplies(song, "") {
			return refreshLocalSong(song, file), true, nil
		}
	}
	if download, ok, err := s.store.DownloadByPath(file.Path); err == nil && ok {
		if song, err := s.store.Song(download.SongID); err == nil {
			return song, true, nil
		}
		warnings = append(warnings, "下载记录存在但歌曲已不存在，按本地曲目处理："+file.Path)
	}
	if song, err := s.store.LocalSongByMetaKey(file.MetaKey); err == nil {
		return refreshLocalSong(song, file), true, nil
	}
	return store.NormalizeSong(store.Song{
		Source:   "local",
		RemoteID: file.MetaKey,
		Page:     1,
		Title:    file.Title,
		Artist:   file.Artist,
		Album:    file.Album,
		Duration: file.Duration,
	}), false, warnings
}

// platformSongStillApplies 判断「这个平台曲目身份还能不能代表这个文件」。
//
// 下载目录里的文件靠 downloads 记录保持平台身份；下载记录被删掉后，同一个
// 文件就只是本地副本了，下次扫描应当退回本地身份，而不是继续挂着平台 ID。
// path 为空表示按内容指纹复用，只要求这首歌仍有下载记录。
func (s *Service) platformSongStillApplies(song store.Song, path string) bool {
	if song.Source == "local" {
		return true
	}
	if s.store == nil {
		return false
	}
	if path != "" {
		if download, ok, err := s.store.DownloadByPath(path); err == nil && ok {
			return download.SongID == song.ID
		}
	}
	if _, err := s.store.Download(song.ID); err == nil {
		return true
	}
	return false
}

// refreshLocalSong 用本次扫描到的元数据补齐本地曲目，但不覆盖用户已有信息：
// 标题为空才回填，时长只在原来缺失时补。
func refreshLocalSong(song store.Song, file library.File) store.Song {
	if song.Source != "local" {
		return song
	}
	if strings.TrimSpace(song.Title) == "" {
		song.Title = file.Title
	}
	if strings.TrimSpace(song.Artist) == "" {
		song.Artist = file.Artist
	}
	if strings.TrimSpace(song.Album) == "" {
		song.Album = file.Album
	}
	if song.Duration <= 0 {
		song.Duration = file.Duration
	}
	return song
}

// SetLibraryRoot 更换文件夹歌单的目录，并立刻重扫。
func (s *Service) SetLibraryRoot(playlistID, path string) (LibraryScan, error) {
	abs, err := ExpandPath(path)
	if err != nil {
		return LibraryScan{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return LibraryScan{}, fmt.Errorf("文件夹不可用：%w", err)
	}
	if !info.IsDir() {
		return LibraryScan{}, fmt.Errorf("不是文件夹：%s", abs)
	}
	if err := s.store.SetPlaylistRoot(playlistID, abs); err != nil {
		return LibraryScan{}, err
	}
	return s.RescanLibrary(playlistID)
}

// LocalSongs 返回曲库里的本地曲目，供歌词绑定选「本机曲目」。
func (s *Service) LocalSongs() ([]store.Song, error) {
	if s.store == nil {
		return nil, nil
	}
	return s.store.LocalSongs(5000)
}

// LocalOnlyCounts 返回仅本机歌单与本机注释的数量；同步页用它提示「换设备不会丢」。
func (s *Service) LocalOnlyCounts() (playlists, annotations int) {
	if s.store == nil {
		return 0, 0
	}
	playlists, annotations, _ = s.store.LocalOnlyCounts()
	return playlists, annotations
}
