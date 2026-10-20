// Package store 负责持久化：歌单、歌曲、播放队列、设置与登录态。
//
// 设计约束见 docs/adr/0004-track-and-tracksource.md：
// 只存平台标识与元数据，播放直链不落库（会过期）。
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，免 cgo
)

// ErrNotFound 表示目标记录不存在。
var ErrNotFound = errors.New("store: 记录不存在")

// Song 是一首歌的持久化形态。
type Song struct {
	ID       string
	Source   string
	BVID     string
	Page     int
	CID      int64
	Title    string
	Artist   string
	Duration int // 秒
	Cover    string
	AddedAt  time.Time
}

// Playlist 是一个歌单，Kind 为 "remote"（B 站收藏夹镜像）或 "local"。
type Playlist struct {
	ID       string
	Title    string
	Kind     string
	RemoteID string
	Count    int
}

// Store 是 SQLite 封装。
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS songs (
	id        TEXT PRIMARY KEY,
	source    TEXT NOT NULL,
	bvid      TEXT NOT NULL,
	page      INTEGER NOT NULL DEFAULT 1,
	cid       INTEGER NOT NULL DEFAULT 0,
	title     TEXT NOT NULL,
	artist    TEXT NOT NULL DEFAULT '',
	duration  INTEGER NOT NULL DEFAULT 0,
	cover     TEXT NOT NULL DEFAULT '',
	added_at  INTEGER NOT NULL,
	UNIQUE(source, bvid, page)
);
CREATE TABLE IF NOT EXISTS playlists (
	id        TEXT PRIMARY KEY,
	title     TEXT NOT NULL,
	kind      TEXT NOT NULL DEFAULT 'local',
	remote_id TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS playlist_items (
	playlist_id TEXT NOT NULL,
	song_id     TEXT NOT NULL,
	position    INTEGER NOT NULL,
	added_at    INTEGER NOT NULL,
	PRIMARY KEY (playlist_id, song_id)
);
CREATE TABLE IF NOT EXISTS queue (
	id          INTEGER PRIMARY KEY CHECK (id = 1),
	data        TEXT NOT NULL,
	position    INTEGER NOT NULL DEFAULT 0,
	queue_index INTEGER NOT NULL DEFAULT 0,
	updated     INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS accounts (
	source     TEXT PRIMARY KEY,
	payload    TEXT NOT NULL,
	updated_at INTEGER NOT NULL
);
`

// Open 打开（或创建）数据库并建表。
func Open(path string) (*Store, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: 打开数据库: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: 建表: %w", err)
	}
	return &Store{db: db}, nil
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

// SongID 生成稳定的歌曲 ID：同一平台同一 BV 同一分 P 视为同一首。
func SongID(source, bvid string, page int) string {
	return fmt.Sprintf("%s:%s:%d", source, bvid, page)
}

// UpsertSong 写入或更新一首歌，返回落库后的记录。
func (s *Store) UpsertSong(song Song) (Song, error) {
	if song.Source == "" || song.BVID == "" {
		return Song{}, fmt.Errorf("store: 歌曲缺少来源或 BV 号")
	}
	if song.Page <= 0 {
		song.Page = 1
	}
	if song.ID == "" {
		song.ID = SongID(song.Source, song.BVID, song.Page)
	}
	if song.AddedAt.IsZero() {
		song.AddedAt = time.Now()
	}
	_, err := s.db.Exec(`
INSERT INTO songs (id, source, bvid, page, cid, title, artist, duration, cover, added_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source, bvid, page) DO UPDATE SET
	title = excluded.title,
	artist = excluded.artist,
	duration = excluded.duration,
	cover = excluded.cover,
	cid = CASE WHEN excluded.cid > 0 THEN excluded.cid ELSE songs.cid END`,
		song.ID, song.Source, song.BVID, song.Page, song.CID, song.Title, song.Artist,
		song.Duration, song.Cover, song.AddedAt.Unix())
	if err != nil {
		return Song{}, fmt.Errorf("store: 写入歌曲: %w", err)
	}
	return s.Song(song.ID)
}

// Song 按 ID 取一首歌。
func (s *Store) Song(id string) (Song, error) {
	row := s.db.QueryRow(`SELECT id, source, bvid, page, cid, title, artist, duration, cover, added_at FROM songs WHERE id = ?`, id)
	song, err := scanSong(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Song{}, ErrNotFound
	}
	return song, err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanSong(r rowScanner) (Song, error) {
	var (
		song  Song
		added int64
	)
	err := r.Scan(&song.ID, &song.Source, &song.BVID, &song.Page, &song.CID,
		&song.Title, &song.Artist, &song.Duration, &song.Cover, &added)
	if err != nil {
		return Song{}, err
	}
	song.AddedAt = time.Unix(added, 0)
	return song, nil
}

// SongsByIDs 按给定顺序取歌，缺失的 ID 会被跳过。
func (s *Store) SongsByIDs(ids []string) ([]Song, error) {
	out := make([]Song, 0, len(ids))
	for _, id := range ids {
		song, err := s.Song(id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, song)
	}
	return out, nil
}

// EnsurePlaylist 确保歌单存在（按 ID 幂等）。
func (s *Store) EnsurePlaylist(p Playlist) (Playlist, error) {
	if p.ID == "" {
		return Playlist{}, fmt.Errorf("store: 歌单缺少 ID")
	}
	if p.Kind == "" {
		p.Kind = "local"
	}
	_, err := s.db.Exec(`
INSERT INTO playlists (id, title, kind, remote_id, created_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET title = excluded.title, kind = excluded.kind, remote_id = excluded.remote_id`,
		p.ID, p.Title, p.Kind, p.RemoteID, time.Now().Unix())
	if err != nil {
		return Playlist{}, fmt.Errorf("store: 写入歌单: %w", err)
	}
	return s.Playlist(p.ID)
}

// Playlist 按 ID 取歌单（带曲目数）。
func (s *Store) Playlist(id string) (Playlist, error) {
	row := s.db.QueryRow(`
SELECT p.id, p.title, p.kind, p.remote_id,
       (SELECT COUNT(*) FROM playlist_items i WHERE i.playlist_id = p.id)
FROM playlists p WHERE p.id = ?`, id)
	var p Playlist
	err := row.Scan(&p.ID, &p.Title, &p.Kind, &p.RemoteID, &p.Count)
	if errors.Is(err, sql.ErrNoRows) {
		return Playlist{}, ErrNotFound
	}
	if err != nil {
		return Playlist{}, fmt.Errorf("store: 读歌单: %w", err)
	}
	return p, nil
}

// ListPlaylists 列出全部歌单，远端在前、本地在后。
func (s *Store) ListPlaylists() ([]Playlist, error) {
	rows, err := s.db.Query(`
SELECT p.id, p.title, p.kind, p.remote_id,
       (SELECT COUNT(*) FROM playlist_items i WHERE i.playlist_id = p.id)
FROM playlists p
ORDER BY CASE p.kind WHEN 'remote' THEN 0 ELSE 1 END, p.created_at`)
	if err != nil {
		return nil, fmt.Errorf("store: 列出歌单: %w", err)
	}
	defer rows.Close()
	var out []Playlist
	for rows.Next() {
		var p Playlist
		if err := rows.Scan(&p.ID, &p.Title, &p.Kind, &p.RemoteID, &p.Count); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AddSongToPlaylist 把歌曲追加到歌单末尾（重复加入会移到末尾）。
func (s *Store) AddSongToPlaylist(playlistID string, song Song) error {
	stored, err := s.UpsertSong(song)
	if err != nil {
		return err
	}
	var maxPos sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(position) FROM playlist_items WHERE playlist_id = ?`, playlistID).Scan(&maxPos); err != nil {
		return fmt.Errorf("store: 读取歌单位置: %w", err)
	}
	pos := int(maxPos.Int64) + 1
	_, err = s.db.Exec(`
INSERT INTO playlist_items (playlist_id, song_id, position, added_at) VALUES (?, ?, ?, ?)
ON CONFLICT(playlist_id, song_id) DO UPDATE SET position = excluded.position`,
		playlistID, stored.ID, pos, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("store: 加入歌单: %w", err)
	}
	return nil
}

// RemoveSongFromPlaylist 从歌单移除一首歌。
func (s *Store) RemoveSongFromPlaylist(playlistID, songID string) error {
	_, err := s.db.Exec(`DELETE FROM playlist_items WHERE playlist_id = ? AND song_id = ?`, playlistID, songID)
	if err != nil {
		return fmt.Errorf("store: 移除歌曲: %w", err)
	}
	return nil
}

// PlaylistSongs 按顺序返回歌单内的歌曲。
func (s *Store) PlaylistSongs(playlistID string) ([]Song, error) {
	rows, err := s.db.Query(`
SELECT s.id, s.source, s.bvid, s.page, s.cid, s.title, s.artist, s.duration, s.cover, s.added_at
FROM playlist_items i JOIN songs s ON s.id = i.song_id
WHERE i.playlist_id = ? ORDER BY i.position`, playlistID)
	if err != nil {
		return nil, fmt.Errorf("store: 读歌单歌曲: %w", err)
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

// ReplacePlaylistSongs 用一个完整列表替换歌单内容（用于远端收藏夹镜像同步）。
func (s *Store) ReplacePlaylistSongs(playlistID string, songs []Song) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: 开启事务: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM playlist_items WHERE playlist_id = ?`, playlistID); err != nil {
		return fmt.Errorf("store: 清空歌单: %w", err)
	}
	for i, song := range songs {
		if song.Page <= 0 {
			song.Page = 1
		}
		if song.ID == "" {
			song.ID = SongID(song.Source, song.BVID, song.Page)
		}
		added := song.AddedAt
		if added.IsZero() {
			added = time.Now()
		}
		if _, err := tx.Exec(`
INSERT INTO songs (id, source, bvid, page, cid, title, artist, duration, cover, added_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source, bvid, page) DO UPDATE SET
	title = excluded.title, artist = excluded.artist, duration = excluded.duration,
	cover = excluded.cover, cid = CASE WHEN excluded.cid > 0 THEN excluded.cid ELSE songs.cid END`,
			song.ID, song.Source, song.BVID, song.Page, song.CID, song.Title, song.Artist,
			song.Duration, song.Cover, added.Unix()); err != nil {
			return fmt.Errorf("store: 写入歌曲: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO playlist_items (playlist_id, song_id, position, added_at) VALUES (?, ?, ?, ?)`,
			playlistID, song.ID, i+1, added.Unix()); err != nil {
			return fmt.Errorf("store: 写入歌单项: %w", err)
		}
	}
	return tx.Commit()
}

// QueueState 是播放队列的持久化状态。
type QueueState struct {
	IDs      []string
	Index    int
	Position int64 // 毫秒
}

// SaveQueue 保存队列。
func (s *Store) SaveQueue(q QueueState) error {
	data, err := json.Marshal(q.IDs)
	if err != nil {
		return fmt.Errorf("store: 序列化队列: %w", err)
	}
	_, err = s.db.Exec(`
INSERT INTO queue (id, data, position, queue_index, updated) VALUES (1, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET data = excluded.data, position = excluded.position,
	queue_index = excluded.queue_index, updated = excluded.updated`,
		string(data), q.Position, q.Index, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("store: 保存队列: %w", err)
	}
	return nil
}

// LoadQueue 读取队列；没有记录时返回空队列。
func (s *Store) LoadQueue() (QueueState, error) {
	var (
		raw   string
		state QueueState
	)
	err := s.db.QueryRow(`SELECT data, position, queue_index FROM queue WHERE id = 1`).Scan(&raw, &state.Position, &state.Index)
	if errors.Is(err, sql.ErrNoRows) {
		return QueueState{}, nil
	}
	if err != nil {
		return QueueState{}, fmt.Errorf("store: 读取队列: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), &state.IDs); err != nil {
		return QueueState{}, fmt.Errorf("store: 解析队列: %w", err)
	}
	return state, nil
}

// Setting 读取设置项，缺失时返回 fallback。
func (s *Store) Setting(key, fallback string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		return fallback
	}
	return v
}

// SetSetting 写入设置项。
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("store: 写入设置: %w", err)
	}
	return nil
}

// SaveAccount 保存音源登录态（JSON）。
func (s *Store) SaveAccount(source string, payload []byte) error {
	_, err := s.db.Exec(`INSERT INTO accounts (source, payload, updated_at) VALUES (?, ?, ?)
ON CONFLICT(source) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at`,
		source, string(payload), time.Now().Unix())
	if err != nil {
		return fmt.Errorf("store: 保存登录态: %w", err)
	}
	return nil
}

// LoadAccount 读取音源登录态。
func (s *Store) LoadAccount(source string) ([]byte, error) {
	var payload string
	err := s.db.QueryRow(`SELECT payload FROM accounts WHERE source = ?`, source).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: 读取登录态: %w", err)
	}
	return []byte(payload), nil
}

// DeleteAccount 清除音源登录态。
func (s *Store) DeleteAccount(source string) error {
	if _, err := s.db.Exec(`DELETE FROM accounts WHERE source = ?`, source); err != nil {
		return fmt.Errorf("store: 清除登录态: %w", err)
	}
	return nil
}
