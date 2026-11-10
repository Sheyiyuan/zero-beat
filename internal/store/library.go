package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// LibraryFile 是文件夹歌单里一个音频文件的索引行。
//
// Path 是身份查找的第一优先项：用户改标签但路径不变时，仍复用同一个 song_id。
// Present 为 false 表示上次扫描没再看到这个文件（保留行与注释，文件回来能恢复）。
type LibraryFile struct {
	PlaylistID  string
	Path        string
	SongID      string
	Size        int64
	ModTime     int64
	ContentHash string
	Present     bool
}

// LibraryItem 是一次扫描产出的物化条目：歌曲元数据 + 文件索引。
type LibraryItem struct {
	Song        Song
	Path        string
	Size        int64
	ModTime     int64
	ContentHash string
}

// LibraryFiles 返回一个文件夹歌单的全部文件索引（含缺失文件）。
func (s *Store) LibraryFiles(playlistID string) ([]LibraryFile, error) {
	rows, err := s.db.Query(`
SELECT playlist_id, path, song_id, size, mtime, content_hash, present
FROM library_files WHERE playlist_id = ? ORDER BY path`, playlistID)
	if err != nil {
		return nil, fmt.Errorf("store: 读取文件索引: %w", err)
	}
	defer rows.Close()
	var out []LibraryFile
	for rows.Next() {
		var f LibraryFile
		if err := rows.Scan(&f.PlaylistID, &f.Path, &f.SongID, &f.Size, &f.ModTime, &f.ContentHash, &f.Present); err != nil {
			return nil, fmt.Errorf("store: 解析文件索引: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// LibraryFilesBySong 返回一首本地曲目对应的全部现存文件路径。
// 本地源的 Resolve 靠它逐个 os.Stat，取第一个仍然可用的文件。
func (s *Store) LibraryFilesBySong(songID string) ([]LibraryFile, error) {
	rows, err := s.db.Query(`
SELECT playlist_id, path, song_id, size, mtime, content_hash, present
FROM library_files WHERE song_id = ? AND present = 1 ORDER BY path`, songID)
	if err != nil {
		return nil, fmt.Errorf("store: 读取本地曲目路径: %w", err)
	}
	defer rows.Close()
	var out []LibraryFile
	for rows.Next() {
		var f LibraryFile
		if err := rows.Scan(&f.PlaylistID, &f.Path, &f.SongID, &f.Size, &f.ModTime, &f.ContentHash, &f.Present); err != nil {
			return nil, fmt.Errorf("store: 解析本地曲目路径: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// LibraryFilesBySongAll 返回一首曲目的全部文件行，包含已缺失的路径。
// 歌词查找需要它：音频文件暂时不在时，同目录的 .lrc 可能还在。
func (s *Store) LibraryFilesBySongAll(songID string) ([]LibraryFile, error) {
	rows, err := s.db.Query(`
SELECT playlist_id, path, song_id, size, mtime, content_hash, present
FROM library_files WHERE song_id = ? ORDER BY path`, songID)
	if err != nil {
		return nil, fmt.Errorf("store: 读取本地曲目路径: %w", err)
	}
	defer rows.Close()
	var out []LibraryFile
	for rows.Next() {
		var f LibraryFile
		if err := rows.Scan(&f.PlaylistID, &f.Path, &f.SongID, &f.Size, &f.ModTime, &f.ContentHash, &f.Present); err != nil {
			return nil, fmt.Errorf("store: 解析本地曲目路径: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// LibraryFile 按「歌单 + 路径」查一条文件索引；缺失时返回 ErrNotFound。
func (s *Store) LibraryFile(playlistID, path string) (LibraryFile, error) {
	var f LibraryFile
	err := s.db.QueryRow(`
SELECT playlist_id, path, song_id, size, mtime, content_hash, present
FROM library_files WHERE playlist_id = ? AND path = ?`, playlistID, filepath.Clean(path)).
		Scan(&f.PlaylistID, &f.Path, &f.SongID, &f.Size, &f.ModTime, &f.ContentHash, &f.Present)
	if errors.Is(err, sql.ErrNoRows) {
		return LibraryFile{}, ErrNotFound
	}
	if err != nil {
		return LibraryFile{}, fmt.Errorf("store: 读取文件索引: %w", err)
	}
	return f, nil
}

// SongByContentHash 按内容指纹找已经登记过的曲目，用于「文件移动/改名」后
// 复用身份。可以跨歌单命中：下载目录与本地库里的同一份文件应当共享曲目身份。
func (s *Store) SongByContentHash(hash string) (string, error) {
	if hash == "" {
		return "", ErrNotFound
	}
	var songID string
	err := s.db.QueryRow(`
SELECT f.song_id FROM library_files f JOIN songs s ON s.id = f.song_id
WHERE f.content_hash = ? LIMIT 1`, hash).Scan(&songID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: 按指纹查找曲目: %w", err)
	}
	return songID, nil
}

// LocalSongByMetaKey 按本地身份键找曲目（source=local 且 remote_id=key）。
func (s *Store) LocalSongByMetaKey(metaKey string) (Song, error) {
	if metaKey == "" {
		return Song{}, ErrNotFound
	}
	row := s.db.QueryRow(`
SELECT id, source, remote_id, bvid, page, cid, title, artist, album, duration, cover, added_at
FROM songs WHERE source = 'local' AND remote_id = ?`, metaKey)
	song, err := scanSong(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Song{}, ErrNotFound
	}
	return song, err
}

// LocalSongs 列出全部本地曲目，供歌词绑定里「选本机曲目」等场景使用。
func (s *Store) LocalSongs(limit int) ([]Song, error) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	rows, err := s.db.Query(`
SELECT id, source, remote_id, bvid, page, cid, title, artist, album, duration, cover, added_at
FROM songs WHERE source = 'local' ORDER BY lower(artist), lower(title), id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: 列出本地曲目: %w", err)
	}
	defer rows.Close()
	var out []Song
	for rows.Next() {
		song, err := scanSong(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, song)
	}
	return out, rows.Err()
}

// ReplaceLibraryItems 在单事务内替换文件夹歌单的物化内容与文件索引。
//
// 已存在的文件行被标记为 present=0 后再按本轮扫描结果逐条置 1，
// 这样「文件暂时不在」不会删掉歌曲行和注释，下次扫描回来还能接上。
func (s *Store) ReplaceLibraryItems(playlistID string, items []LibraryItem) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: 开启事务: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE library_files SET present = 0 WHERE playlist_id = ?`, playlistID); err != nil {
		return fmt.Errorf("store: 标记文件缺失: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM playlist_items WHERE playlist_id = ?`, playlistID); err != nil {
		return fmt.Errorf("store: 清空歌单: %w", err)
	}
	now := time.Now()
	seen := map[string]bool{}
	position := 0
	for _, item := range items {
		song := NormalizeSong(item.Song)
		if song.Source == "" || song.ExternalID() == "" {
			return fmt.Errorf("store: 歌曲缺少来源或外部 ID")
		}
		added := song.AddedAt
		if added.IsZero() {
			added = now
		}
		if _, err := tx.Exec(`
INSERT INTO songs (id, source, remote_id, bvid, page, cid, title, artist, album, duration, cover, added_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source, remote_id, page) DO UPDATE SET
	title = excluded.title, artist = excluded.artist, album = excluded.album,
	duration = CASE WHEN excluded.duration > 0 THEN excluded.duration ELSE songs.duration END,
	cover = CASE WHEN excluded.cover <> '' THEN excluded.cover ELSE songs.cover END,
	bvid = CASE WHEN excluded.bvid <> '' THEN excluded.bvid ELSE songs.bvid END,
	cid = CASE WHEN excluded.cid > 0 THEN excluded.cid ELSE songs.cid END`,
			song.ID, song.Source, song.RemoteID, song.BVID, song.Page, song.CID, song.Title,
			song.Artist, song.Album, song.Duration, song.Cover, added.Unix()); err != nil {
			return fmt.Errorf("store: 写入歌曲: %w", err)
		}
		// 同一份 meta 的多个文件共享一个 song_id；歌单条目只保留第一次出现的位置。
		if !seen[song.ID] {
			seen[song.ID] = true
			position++
			if _, err := tx.Exec(`
INSERT INTO playlist_items (playlist_id, song_id, position, added_at) VALUES (?, ?, ?, ?)`,
				playlistID, song.ID, position, added.Unix()); err != nil {
				return fmt.Errorf("store: 写入歌单项: %w", err)
			}
		}
		if _, err := tx.Exec(`
INSERT INTO library_files (playlist_id, path, song_id, size, mtime, content_hash, present)
VALUES (?, ?, ?, ?, ?, ?, 1)
ON CONFLICT(playlist_id, path) DO UPDATE SET
	song_id = excluded.song_id, size = excluded.size, mtime = excluded.mtime,
	content_hash = excluded.content_hash, present = 1`,
			playlistID, filepath.Clean(item.Path), song.ID, item.Size, item.ModTime, item.ContentHash); err != nil {
			return fmt.Errorf("store: 写入文件索引: %w", err)
		}
	}
	if _, err := tx.Exec(`
UPDATE playlists SET scan_status = 'ok', scan_error = '', scanned_at = ? WHERE id = ?`,
		now.Unix(), playlistID); err != nil {
		return fmt.Errorf("store: 更新扫描状态: %w", err)
	}
	return tx.Commit()
}

// UpdatePlaylistScan 只更新扫描状态，不动已物化的条目。
// 目录不可用时用它把歌单标成 missing/error，而不是清空内容。
func (s *Store) UpdatePlaylistScan(playlistID, status, errMsg string) error {
	_, err := s.db.Exec(`
UPDATE playlists SET scan_status = ?, scan_error = ?, scanned_at = ? WHERE id = ?`,
		status, errMsg, time.Now().Unix(), playlistID)
	if err != nil {
		return fmt.Errorf("store: 更新扫描状态: %w", err)
	}
	return nil
}

// SetPlaylistRoot 更换文件夹歌单的根目录；已有条目保持不变，等下次扫描替换。
func (s *Store) SetPlaylistRoot(playlistID, rootPath string) error {
	if !filepath.IsAbs(rootPath) {
		return fmt.Errorf("store: 文件夹路径必须是绝对路径")
	}
	_, err := s.db.Exec(`UPDATE playlists SET root_path = ?, scan_status = '', scan_error = '', scanned_at = 0
WHERE id = ? AND kind = 'folder'`, filepath.Clean(rootPath), playlistID)
	if err != nil {
		return fmt.Errorf("store: 更换文件夹: %w", err)
	}
	return nil
}

// DownloadByPath 按绝对路径查下载记录，用于把下载目录当文件夹歌单时
// 复用平台 song_id，避免同一文件出现「平台下载 + 本地曲目」两个身份。
func (s *Store) DownloadByPath(path string) (Download, bool, error) {
	row := s.db.QueryRow(`
SELECT song_id, path, size, codec, duration_ms, created_at FROM downloads
WHERE path = ? LIMIT 1`, filepath.Clean(path))
	d, err := scanDownload(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Download{}, false, nil
	}
	if err != nil {
		return Download{}, false, fmt.Errorf("store: 按路径查找下载记录: %w", err)
	}
	return d, true, nil
}

// SetSongDurationIfEmpty 只在时长仍为 0 时写入探测结果。
// 扫描阶段不逐文件 ffprobe，首次播放/seek 时补一次即可。
func (s *Store) SetSongDurationIfEmpty(songID string, duration int) error {
	if duration <= 0 {
		return nil
	}
	if _, err := s.db.Exec(`UPDATE songs SET duration = ? WHERE id = ? AND duration <= 0`, duration, songID); err != nil {
		return fmt.Errorf("store: 回写时长: %w", err)
	}
	return nil
}

// LocalOnlyCounts 返回不进同步仓库的歌单与注释数量，供同步页提示。
func (s *Store) LocalOnlyCounts() (playlists, annotations int, err error) {
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM playlists WHERE sync_excluded = 1`).Scan(&playlists); err != nil {
		return 0, 0, fmt.Errorf("store: 统计仅本机歌单: %w", err)
	}
	all, err := s.ListAnnotations()
	if err != nil {
		return playlists, 0, err
	}
	syncable, err := s.ListSyncAnnotations()
	if err != nil {
		return playlists, 0, err
	}
	// 差集就是「本地曲目上的注释」与「local: 孤儿注释」。
	annotations = len(all) - len(syncable)
	if annotations < 0 {
		annotations = 0
	}
	return playlists, annotations, nil
}
