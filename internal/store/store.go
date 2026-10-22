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
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，免 cgo
)

// ErrNotFound 表示目标记录不存在。
var ErrNotFound = errors.New("store: 记录不存在")

// Song 是一首歌的持久化形态。
//
// RemoteID 是音源内的稳定 ID（B 站 BV 号 / 网易云 song id）；BVID 是 v0.1 遗留的
// B 站专用字段，读写时保持兼容，新代码统一用 ExternalID()。
type Song struct {
	ID       string
	Source   string
	RemoteID string
	BVID     string
	Page     int
	CID      int64
	Title    string
	Artist   string
	Album    string
	Duration int // 秒
	Cover    string
	AddedAt  time.Time
}

// ExternalID 返回音源内的稳定 ID。
func (s Song) ExternalID() string {
	if s.RemoteID != "" {
		return s.RemoteID
	}
	return s.BVID
}

// NormalizeSong 补齐派生字段。它不猜音源：Source 由调用方或数据库负责，
// 避免在通用层里写死某个平台。
func NormalizeSong(song Song) Song {
	if song.Page <= 0 {
		song.Page = 1
	}
	if song.RemoteID == "" {
		song.RemoteID = song.BVID
	}
	if song.ID == "" {
		song.ID = SongID(song.Source, song.ExternalID(), song.Page)
	}
	return song
}

// Download 是一首已下载歌曲的落盘记录。
type Download struct {
	SongID     string
	Path       string
	Size       int64
	Codec      string
	DurationMS int64
	CreatedAt  time.Time
}

// DownloadEntry 是下载记录和歌曲元数据的组合；Song 为零值时表示歌曲表里没有对应行。
type DownloadEntry struct {
	Download Download
	Song     Song
}

// Playlist 是一个歌单，Kind 为「remote」（某音源的远端歌单/收藏夹镜像）或「local」。
type Playlist struct {
	ID       string
	Source   string
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
	remote_id TEXT NOT NULL DEFAULT '',
	bvid      TEXT NOT NULL DEFAULT '',
	page      INTEGER NOT NULL DEFAULT 1,
	cid       INTEGER NOT NULL DEFAULT 0,
	title     TEXT NOT NULL,
	artist    TEXT NOT NULL DEFAULT '',
	album     TEXT NOT NULL DEFAULT '',
	duration  INTEGER NOT NULL DEFAULT 0,
	cover     TEXT NOT NULL DEFAULT '',
	added_at  INTEGER NOT NULL,
	UNIQUE(source, remote_id, page)
);
CREATE TABLE IF NOT EXISTS playlists (
	id        TEXT PRIMARY KEY,
	source    TEXT NOT NULL DEFAULT '',
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
CREATE TABLE IF NOT EXISTS playlist_refs (
	playlist_id TEXT NOT NULL,
	ref_id      TEXT NOT NULL,
	position    INTEGER NOT NULL,
	PRIMARY KEY (playlist_id, ref_id)
);
CREATE TABLE IF NOT EXISTS playlist_excludes (
	playlist_id TEXT NOT NULL,
	song_id     TEXT NOT NULL,
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
CREATE TABLE IF NOT EXISTS downloads (
	song_id     TEXT PRIMARY KEY,
	path        TEXT NOT NULL,
	size        INTEGER NOT NULL DEFAULT 0,
	codec       TEXT NOT NULL DEFAULT '',
	duration_ms INTEGER NOT NULL DEFAULT 0,
	created_at  INTEGER NOT NULL
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
	st := &Store{db: db}
	if err := st.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

// migrate 把 v0.1 的 B 站单源表升级到多源结构；可重复执行。
func (s *Store) migrate() error {
	hasRemote, err := s.hasColumn("songs", "remote_id")
	if err != nil {
		return err
	}
	hasAlbum, err := s.hasColumn("songs", "album")
	if err != nil {
		return err
	}
	if !hasRemote || !hasAlbum {
		if err := s.rebuildSongs(hasRemote, hasAlbum); err != nil {
			return err
		}
	}
	hasPlaylistSource, err := s.hasColumn("playlists", "source")
	if err != nil {
		return err
	}
	if !hasPlaylistSource {
		if _, err := s.db.Exec(`ALTER TABLE playlists ADD COLUMN source TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("store: 升级歌单表: %w", err)
		}
	}
	if _, err := s.db.Exec(`UPDATE songs SET remote_id = bvid WHERE remote_id = '' AND bvid <> ''`); err != nil {
		return fmt.Errorf("store: 回填歌曲 ID: %w", err)
	}
	if _, err := s.db.Exec(`UPDATE playlists SET source = 'bilibili' WHERE source = '' AND id LIKE 'bili-fav-%'`); err != nil {
		return fmt.Errorf("store: 回填 B 站歌单来源: %w", err)
	}
	if _, err := s.db.Exec(`UPDATE playlists SET source = 'netease' WHERE source = '' AND id LIKE 'netease-%'`); err != nil {
		return fmt.Errorf("store: 回填网易云歌单来源: %w", err)
	}
	return nil
}

func (s *Store) hasColumn(table, column string) (bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, fmt.Errorf("store: 读取表结构: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notNull int
			dflt    any
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return false, fmt.Errorf("store: 解析表结构: %w", err)
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) rebuildSongs(hasRemote, hasAlbum bool) error {
	remoteExpr := `CASE WHEN bvid <> '' THEN bvid ELSE '' END`
	if hasRemote {
		remoteExpr = `CASE WHEN remote_id <> '' THEN remote_id ELSE bvid END`
	}
	albumExpr := `''`
	if hasAlbum {
		albumExpr = `album`
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: 开启迁移事务: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DROP TABLE IF EXISTS songs_migrating`); err != nil {
		return fmt.Errorf("store: 清理迁移临时表: %w", err)
	}
	if _, err := tx.Exec(`
CREATE TABLE songs_migrating (
	id        TEXT PRIMARY KEY,
	source    TEXT NOT NULL,
	remote_id TEXT NOT NULL DEFAULT '',
	bvid      TEXT NOT NULL DEFAULT '',
	page      INTEGER NOT NULL DEFAULT 1,
	cid       INTEGER NOT NULL DEFAULT 0,
	title     TEXT NOT NULL,
	artist    TEXT NOT NULL DEFAULT '',
	album     TEXT NOT NULL DEFAULT '',
	duration  INTEGER NOT NULL DEFAULT 0,
	cover     TEXT NOT NULL DEFAULT '',
	added_at  INTEGER NOT NULL,
	UNIQUE(source, remote_id, page)
)`); err != nil {
		return fmt.Errorf("store: 创建新歌曲表: %w", err)
	}
	if _, err := tx.Exec(`
INSERT INTO songs_migrating (id, source, remote_id, bvid, page, cid, title, artist, album, duration, cover, added_at)
SELECT id, source, ` + remoteExpr + `, bvid, page, cid, title, artist, ` + albumExpr + `, duration, cover, added_at FROM songs`); err != nil {
		return fmt.Errorf("store: 迁移歌曲数据: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE songs`); err != nil {
		return fmt.Errorf("store: 删除旧歌曲表: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE songs_migrating RENAME TO songs`); err != nil {
		return fmt.Errorf("store: 重命名歌曲表: %w", err)
	}
	return tx.Commit()
}

// SongID 生成稳定的歌曲 ID：同一平台同一外部 ID 同一分 P 视为同一首。
func SongID(source, externalID string, page int) string {
	if page <= 0 {
		page = 1
	}
	return fmt.Sprintf("%s:%s:%d", source, externalID, page)
}

// UpsertSong 写入或更新一首歌，返回落库后的记录。
func (s *Store) UpsertSong(song Song) (Song, error) {
	song = NormalizeSong(song)
	if song.Source == "" || song.ExternalID() == "" {
		return Song{}, fmt.Errorf("store: 歌曲缺少来源或外部 ID")
	}
	if song.AddedAt.IsZero() {
		song.AddedAt = time.Now()
	}
	_, err := s.db.Exec(`
INSERT INTO songs (id, source, remote_id, bvid, page, cid, title, artist, album, duration, cover, added_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source, remote_id, page) DO UPDATE SET
	title = excluded.title,
	artist = excluded.artist,
	album = excluded.album,
	duration = excluded.duration,
	cover = excluded.cover,
	bvid = CASE WHEN excluded.bvid <> '' THEN excluded.bvid ELSE songs.bvid END,
	cid = CASE WHEN excluded.cid > 0 THEN excluded.cid ELSE songs.cid END`,
		song.ID, song.Source, song.RemoteID, song.BVID, song.Page, song.CID, song.Title,
		song.Artist, song.Album, song.Duration, song.Cover, song.AddedAt.Unix())
	if err != nil {
		return Song{}, fmt.Errorf("store: 写入歌曲: %w", err)
	}
	return s.Song(song.ID)
}

// Song 按 ID 取一首歌。
func (s *Store) Song(id string) (Song, error) {
	row := s.db.QueryRow(`SELECT id, source, remote_id, bvid, page, cid, title, artist, album, duration, cover, added_at FROM songs WHERE id = ?`, id)
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
	err := r.Scan(&song.ID, &song.Source, &song.RemoteID, &song.BVID, &song.Page, &song.CID,
		&song.Title, &song.Artist, &song.Album, &song.Duration, &song.Cover, &added)
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

// UpsertDownload 写入或更新一首歌的下载记录；重复下载保留原 created_at。
func (s *Store) UpsertDownload(d Download) error {
	d.SongID = strings.TrimSpace(d.SongID)
	d.Path = strings.TrimSpace(d.Path)
	if d.SongID == "" || d.Path == "" {
		return fmt.Errorf("store: 下载记录缺少歌曲 ID 或路径")
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now()
	}
	_, err := s.db.Exec(`
INSERT INTO downloads (song_id, path, size, codec, duration_ms, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(song_id) DO UPDATE SET
	path = excluded.path,
	size = excluded.size,
	codec = excluded.codec,
	duration_ms = excluded.duration_ms`,
		d.SongID, d.Path, d.Size, d.Codec, d.DurationMS, d.CreatedAt.Unix())
	if err != nil {
		return fmt.Errorf("store: 写入下载记录: %w", err)
	}
	return nil
}

// Download 按歌曲 ID 取一条下载记录。
func (s *Store) Download(songID string) (Download, error) {
	row := s.db.QueryRow(`SELECT song_id, path, size, codec, duration_ms, created_at FROM downloads WHERE song_id = ?`, songID)
	d, err := scanDownload(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Download{}, ErrNotFound
	}
	return d, err
}

func scanDownload(r rowScanner) (Download, error) {
	var (
		d       Download
		created int64
	)
	err := r.Scan(&d.SongID, &d.Path, &d.Size, &d.Codec, &d.DurationMS, &created)
	if err != nil {
		return Download{}, err
	}
	d.CreatedAt = time.Unix(created, 0)
	return d, nil
}

// Downloads 返回全部下载记录，最近下载的排在前面。
func (s *Store) Downloads() ([]Download, error) {
	rows, err := s.db.Query(`SELECT song_id, path, size, codec, duration_ms, created_at FROM downloads ORDER BY created_at DESC, song_id`)
	if err != nil {
		return nil, fmt.Errorf("store: 读取下载记录: %w", err)
	}
	defer rows.Close()
	out := make([]Download, 0)
	for rows.Next() {
		d, err := scanDownload(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 解析下载记录: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: 遍历下载记录: %w", err)
	}
	return out, nil
}

// DownloadEntries 返回下载记录及对应歌曲元数据；歌曲缺失时 Song 为零值。
func (s *Store) DownloadEntries() ([]DownloadEntry, error) {
	downloads, err := s.Downloads()
	if err != nil {
		return nil, err
	}
	out := make([]DownloadEntry, 0, len(downloads))
	for _, d := range downloads {
		song, err := s.Song(d.SongID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		out = append(out, DownloadEntry{Download: d, Song: song})
	}
	return out, nil
}

// DeleteDownload 删除一首歌的下载记录，不删文件。
func (s *Store) DeleteDownload(songID string) error {
	if _, err := s.db.Exec(`DELETE FROM downloads WHERE song_id = ?`, songID); err != nil {
		return fmt.Errorf("store: 删除下载记录: %w", err)
	}
	return nil
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
INSERT INTO playlists (id, source, title, kind, remote_id, created_at) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET source = excluded.source, title = excluded.title,
	kind = excluded.kind, remote_id = excluded.remote_id`,
		p.ID, p.Source, p.Title, p.Kind, p.RemoteID, time.Now().Unix())
	if err != nil {
		return Playlist{}, fmt.Errorf("store: 写入歌单: %w", err)
	}
	return s.Playlist(p.ID)
}

// CreatePlaylist 新建一个本地歌单。
func (s *Store) CreatePlaylist(title string) (Playlist, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Playlist{}, fmt.Errorf("store: 歌单名不能为空")
	}
	id := fmt.Sprintf("local-%d", time.Now().UnixNano())
	if _, err := s.db.Exec(`
INSERT INTO playlists (id, source, title, kind, remote_id, created_at) VALUES (?, '', ?, 'local', '', ?)`,
		id, title, time.Now().Unix()); err != nil {
		return Playlist{}, fmt.Errorf("store: 新建歌单: %w", err)
	}
	return s.Playlist(id)
}

// RenamePlaylist 重命名本地/混合歌单；远端收藏夹镜像不允许改名。
func (s *Store) RenamePlaylist(id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("store: 歌单名不能为空")
	}
	p, err := s.Playlist(id)
	if err != nil {
		return err
	}
	if p.Kind != "local" && p.Kind != "mixin" {
		return fmt.Errorf("store: 只有本地或混合歌单可以重命名")
	}
	if _, err := s.db.Exec(`UPDATE playlists SET title = ? WHERE id = ?`, title, id); err != nil {
		return fmt.Errorf("store: 重命名歌单: %w", err)
	}
	return nil
}

// DeletePlaylist 删除本地/混合歌单及其条目；远端歌单不可删除。
func (s *Store) DeletePlaylist(id string) error {
	p, err := s.Playlist(id)
	if err != nil {
		return err
	}
	if p.Kind != "local" && p.Kind != "mixin" {
		return fmt.Errorf("store: 远端歌单不能删除")
	}
	referenced, err := s.IsReferenced(id)
	if err != nil {
		return err
	}
	if referenced {
		return fmt.Errorf("store: 歌单被其他混合歌单引用，先移除引用")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: 开启事务: %w", err)
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM playlist_items WHERE playlist_id = ?`,
		`DELETE FROM playlist_refs WHERE playlist_id = ?`,
		`DELETE FROM playlist_excludes WHERE playlist_id = ?`,
		`DELETE FROM playlists WHERE id = ?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return fmt.Errorf("store: 删除歌单: %w", err)
		}
	}
	return tx.Commit()
}

// IsReferenced 判断歌单是否被其他混合歌单引用。
func (s *Store) IsReferenced(id string) (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM playlist_refs WHERE ref_id = ?`, id).Scan(&n); err != nil {
		return false, fmt.Errorf("store: 读取歌单引用: %w", err)
	}
	return n > 0, nil
}

// UnlockPlaylist 把同步歌单解锁为本地歌单：保留 ID 与全部曲目，断开远端同步。
func (s *Store) UnlockPlaylist(id string) (Playlist, error) {
	p, err := s.Playlist(id)
	if err != nil {
		return Playlist{}, err
	}
	if p.Kind != "remote" {
		return Playlist{}, fmt.Errorf("store: 只有同步歌单可以解锁")
	}
	if _, err := s.db.Exec(`UPDATE playlists SET kind = 'local', source = '', remote_id = '' WHERE id = ?`, id); err != nil {
		return Playlist{}, fmt.Errorf("store: 解锁歌单: %w", err)
	}
	return s.Playlist(id)
}

// CreateMixin 新建一个混合歌单；refIDs 按引用顺序保存。
func (s *Store) CreateMixin(title string, refIDs []string) (Playlist, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Playlist{}, fmt.Errorf("store: 歌单名不能为空")
	}
	refs, err := s.validateMixinRefs(refIDs)
	if err != nil {
		return Playlist{}, err
	}
	id := fmt.Sprintf("mix-%d", time.Now().UnixNano())
	tx, err := s.db.Begin()
	if err != nil {
		return Playlist{}, fmt.Errorf("store: 开启事务: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
INSERT INTO playlists (id, source, title, kind, remote_id, created_at) VALUES (?, '', ?, 'mixin', '', ?)`,
		id, title, time.Now().Unix()); err != nil {
		return Playlist{}, fmt.Errorf("store: 新建混合歌单: %w", err)
	}
	for i, ref := range refs {
		if _, err := tx.Exec(`INSERT INTO playlist_refs (playlist_id, ref_id, position) VALUES (?, ?, ?)`, id, ref.ID, i); err != nil {
			return Playlist{}, fmt.Errorf("store: 写入歌单引用: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Playlist{}, err
	}
	return s.Playlist(id)
}

// SetMixinRefs 按给定顺序替换混合歌单的引用列表。
func (s *Store) SetMixinRefs(playlistID string, refIDs []string) error {
	p, err := s.Playlist(playlistID)
	if err != nil {
		return err
	}
	if p.Kind != "mixin" {
		return fmt.Errorf("store: 不是混合歌单")
	}
	refs, err := s.validateMixinRefs(refIDs)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: 开启事务: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM playlist_refs WHERE playlist_id = ?`, playlistID); err != nil {
		return fmt.Errorf("store: 清空歌单引用: %w", err)
	}
	for i, ref := range refs {
		if _, err := tx.Exec(`INSERT INTO playlist_refs (playlist_id, ref_id, position) VALUES (?, ?, ?)`, playlistID, ref.ID, i); err != nil {
			return fmt.Errorf("store: 写入歌单引用: %w", err)
		}
	}
	return tx.Commit()
}

// AddMixinRef 往混合歌单追加一个引用；已存在时返回 false，不重复写入。
// 源歌单不能是另一个混合歌单，保持「引用不嵌套」的约束。
func (s *Store) AddMixinRef(playlistID, refID string) (bool, error) {
	playlistID = strings.TrimSpace(playlistID)
	refID = strings.TrimSpace(refID)
	if playlistID == "" || refID == "" {
		return false, fmt.Errorf("store: 歌单 ID 不能为空")
	}
	if playlistID == refID {
		return false, fmt.Errorf("store: 歌单不能引用自己")
	}
	target, err := s.Playlist(playlistID)
	if err != nil {
		return false, err
	}
	if target.Kind != "mixin" {
		return false, fmt.Errorf("store: 只有混合歌单可以添加引用")
	}
	ref, err := s.Playlist(refID)
	if err != nil {
		return false, err
	}
	if ref.Kind == "mixin" {
		return false, fmt.Errorf("store: 混合歌单不能再引用混合歌单")
	}
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM playlist_refs WHERE playlist_id = ? AND ref_id = ?`, playlistID, refID).Scan(&exists); err != nil {
		return false, fmt.Errorf("store: 检查歌单引用: %w", err)
	}
	if exists > 0 {
		return false, nil
	}
	var maxPos sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(position) FROM playlist_refs WHERE playlist_id = ?`, playlistID).Scan(&maxPos); err != nil {
		return false, fmt.Errorf("store: 读取引用位置: %w", err)
	}
	if _, err := s.db.Exec(`INSERT INTO playlist_refs (playlist_id, ref_id, position) VALUES (?, ?, ?)`,
		playlistID, refID, int(maxPos.Int64)+1); err != nil {
		return false, fmt.Errorf("store: 追加歌单引用: %w", err)
	}
	return true, nil
}

func (s *Store) validateMixinRefs(refIDs []string) ([]Playlist, error) {
	if len(refIDs) == 0 {
		return nil, fmt.Errorf("store: 混合歌单至少引用一个歌单")
	}
	seen := map[string]bool{}
	refs := make([]Playlist, 0, len(refIDs))
	for _, id := range refIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		p, err := s.Playlist(id)
		if err != nil {
			return nil, fmt.Errorf("store: 读取引用歌单 %q: %w", id, err)
		}
		if p.Kind == "mixin" {
			return nil, fmt.Errorf("store: 混合歌单不能再引用混合歌单")
		}
		refs = append(refs, p)
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("store: 混合歌单至少引用一个歌单")
	}
	return refs, nil
}

// MixinRefs 按顺序返回混合歌单引用的歌单。
func (s *Store) MixinRefs(playlistID string) ([]Playlist, error) {
	rows, err := s.db.Query(`
SELECT p.id, p.source, p.title, p.kind, p.remote_id,
       (SELECT COUNT(*) FROM playlist_items i WHERE i.playlist_id = p.id)
FROM playlist_refs r JOIN playlists p ON p.id = r.ref_id
WHERE r.playlist_id = ? ORDER BY r.position`, playlistID)
	if err != nil {
		return nil, fmt.Errorf("store: 读取歌单引用: %w", err)
	}
	defer rows.Close()
	var out []Playlist
	for rows.Next() {
		var p Playlist
		if err := rows.Scan(&p.ID, &p.Source, &p.Title, &p.Kind, &p.RemoteID, &p.Count); err != nil {
			return nil, err
		}
		if p.Kind == "mixin" {
			return nil, fmt.Errorf("store: 混合歌单存在嵌套引用")
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Playlist 按 ID 取歌单（带曲目数；混合歌单为展开后的去重数量）。
func (s *Store) Playlist(id string) (Playlist, error) {
	row := s.db.QueryRow(`
SELECT p.id, p.source, p.title, p.kind, p.remote_id,
       (SELECT COUNT(*) FROM playlist_items i WHERE i.playlist_id = p.id)
FROM playlists p WHERE p.id = ?`, id)
	var p Playlist
	err := row.Scan(&p.ID, &p.Source, &p.Title, &p.Kind, &p.RemoteID, &p.Count)
	if errors.Is(err, sql.ErrNoRows) {
		return Playlist{}, ErrNotFound
	}
	if err != nil {
		return Playlist{}, fmt.Errorf("store: 读歌单: %w", err)
	}
	if p.Kind == "mixin" {
		songs, err := s.mixinSongs(p)
		if err != nil {
			return Playlist{}, err
		}
		p.Count = len(songs)
	}
	return p, nil
}

// ListPlaylists 列出全部歌单，远端在前、混合次之、本地最后。
func (s *Store) ListPlaylists() ([]Playlist, error) {
	rows, err := s.db.Query(`
SELECT p.id, p.source, p.title, p.kind, p.remote_id,
       (SELECT COUNT(*) FROM playlist_items i WHERE i.playlist_id = p.id)
FROM playlists p
ORDER BY CASE p.kind WHEN 'remote' THEN 0 WHEN 'mixin' THEN 1 ELSE 2 END, p.created_at`)
	if err != nil {
		return nil, fmt.Errorf("store: 列出歌单: %w", err)
	}
	var out []Playlist
	for rows.Next() {
		var p Playlist
		if err := rows.Scan(&p.ID, &p.Source, &p.Title, &p.Kind, &p.RemoteID, &p.Count); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// 单连接 SQLite：先读完列表并关闭 rows，再计算混合歌单的展开数量。
	for i := range out {
		if out[i].Kind != "mixin" {
			continue
		}
		songs, err := s.mixinSongs(out[i])
		if err != nil {
			return nil, err
		}
		out[i].Count = len(songs)
	}
	return out, nil
}

// AddSongToPlaylist 把歌曲追加到歌单末尾（重复加入会移到末尾）。
func (s *Store) AddSongToPlaylist(playlistID string, song Song) error {
	p, err := s.Playlist(playlistID)
	if err != nil {
		return err
	}
	stored, err := s.UpsertSong(song)
	if err != nil {
		return err
	}
	if p.Kind == "mixin" {
		if _, err := s.db.Exec(`DELETE FROM playlist_excludes WHERE playlist_id = ? AND song_id = ?`, playlistID, stored.ID); err != nil {
			return fmt.Errorf("store: 取消歌单隐藏: %w", err)
		}
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

// RemoveSongFromPlaylist 从歌单移除一首歌；混合歌单中的继承曲目用 exclusion 隐藏，不修改源歌单。
func (s *Store) RemoveSongFromPlaylist(playlistID, songID string) error {
	p, err := s.Playlist(playlistID)
	if err != nil {
		return err
	}
	if p.Kind != "mixin" {
		if _, err := s.db.Exec(`DELETE FROM playlist_items WHERE playlist_id = ? AND song_id = ?`, playlistID, songID); err != nil {
			return fmt.Errorf("store: 移除歌曲: %w", err)
		}
		return nil
	}
	var inherited int
	if err := s.db.QueryRow(`
SELECT COUNT(*) FROM playlist_items i
WHERE i.song_id = ? AND i.playlist_id IN (SELECT ref_id FROM playlist_refs WHERE playlist_id = ?)`,
		songID, playlistID).Scan(&inherited); err != nil {
		return fmt.Errorf("store: 检查继承曲目: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM playlist_items WHERE playlist_id = ? AND song_id = ?`, playlistID, songID); err != nil {
		return fmt.Errorf("store: 移除歌曲: %w", err)
	}
	if inherited > 0 {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO playlist_excludes (playlist_id, song_id) VALUES (?, ?)`, playlistID, songID); err != nil {
			return fmt.Errorf("store: 隐藏继承曲目: %w", err)
		}
	}
	return nil
}

// PlaylistSongs 按顺序返回歌单内的歌曲；混合歌单实时展开引用。
func (s *Store) PlaylistSongs(playlistID string) ([]Song, error) {
	p, err := s.Playlist(playlistID)
	if err != nil {
		return nil, err
	}
	return s.playlistSongsFor(p)
}

func (s *Store) playlistSongsFor(p Playlist) ([]Song, error) {
	if p.Kind == "mixin" {
		return s.mixinSongs(p)
	}
	return s.rawPlaylistSongs(p.ID)
}

func (s *Store) rawPlaylistSongs(playlistID string) ([]Song, error) {
	rows, err := s.db.Query(`
SELECT s.id, s.source, s.remote_id, s.bvid, s.page, s.cid, s.title, s.artist, s.album, s.duration, s.cover, s.added_at
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

func (s *Store) mixinSongs(p Playlist) ([]Song, error) {
	refs, err := s.MixinRefs(p.ID)
	if err != nil {
		return nil, err
	}
	excluded := map[string]bool{}
	rows, err := s.db.Query(`SELECT song_id FROM playlist_excludes WHERE playlist_id = ?`, p.ID)
	if err != nil {
		return nil, fmt.Errorf("store: 读取歌单隐藏项: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		excluded[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	out := make([]Song, 0)
	for _, ref := range refs {
		songs, err := s.playlistSongsFor(ref)
		if err != nil {
			return nil, err
		}
		for _, song := range songs {
			if seen[song.ID] || excluded[song.ID] {
				continue
			}
			seen[song.ID] = true
			out = append(out, song)
		}
	}
	own, err := s.rawPlaylistSongs(p.ID)
	if err != nil {
		return nil, err
	}
	for _, song := range own {
		if seen[song.ID] {
			continue
		}
		seen[song.ID] = true
		out = append(out, song)
	}
	return out, nil
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
		song = NormalizeSong(song)
		if song.Source == "" || song.ExternalID() == "" {
			return fmt.Errorf("store: 歌曲缺少来源或外部 ID")
		}
		added := song.AddedAt
		if added.IsZero() {
			added = time.Now()
		}
		if _, err := tx.Exec(`
INSERT INTO songs (id, source, remote_id, bvid, page, cid, title, artist, album, duration, cover, added_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source, remote_id, page) DO UPDATE SET
	title = excluded.title, artist = excluded.artist, album = excluded.album,
	duration = excluded.duration, cover = excluded.cover,
	bvid = CASE WHEN excluded.bvid <> '' THEN excluded.bvid ELSE songs.bvid END,
	cid = CASE WHEN excluded.cid > 0 THEN excluded.cid ELSE songs.cid END`,
			song.ID, song.Source, song.RemoteID, song.BVID, song.Page, song.CID, song.Title,
			song.Artist, song.Album, song.Duration, song.Cover, added.Unix()); err != nil {
			return fmt.Errorf("store: 写入歌曲: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO playlist_items (playlist_id, song_id, position, added_at) VALUES (?, ?, ?, ?)`,
			playlistID, song.ID, i+1, added.Unix()); err != nil {
			return fmt.Errorf("store: 写入歌单项: %w", err)
		}
	}
	return tx.Commit()
}

// SearchSongs 在本地曲库中按标题/歌手/专辑做子串搜索。
func (s *Store) SearchSongs(query string, limit int) ([]Song, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.db.Query(`
SELECT id, source, remote_id, bvid, page, cid, title, artist, album, duration, cover, added_at
FROM songs
WHERE instr(lower(title), ?) > 0 OR instr(lower(artist), ?) > 0 OR instr(lower(album), ?) > 0
ORDER BY lower(title), lower(artist)
LIMIT ?`, query, query, query, limit)
	if err != nil {
		return nil, fmt.Errorf("store: 搜索本地歌曲: %w", err)
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

// SettingInt 读取整数设置项；缺失或格式错误时返回 fallback。
func (s *Store) SettingInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(s.Setting(key, ""))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback, fmt.Errorf("store: 设置 %s 不是整数: %w", key, err)
	}
	return n, nil
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

// Clip 是一首歌的播放区间，单位毫秒；EndMS 为 0 表示播放到结尾。
type Clip struct {
	StartMS int64 `json:"start_ms"`
	EndMS   int64 `json:"end_ms"`
}

// Active 判断区间是否真的限制了播放范围。
func (c Clip) Active() bool { return c.StartMS > 0 || c.EndMS > 0 }

// Clip 读取一首歌的播放区间；没有设置时返回零值。
func (s *Store) Clip(songID string) (Clip, error) {
	songID = strings.TrimSpace(songID)
	if songID == "" {
		return Clip{}, nil
	}
	raw := s.Setting("clip:"+songID, "")
	if raw == "" {
		return Clip{}, nil
	}
	var c Clip
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Clip{}, fmt.Errorf("store: 解析播放区间: %w", err)
	}
	return c.normalized(), nil
}

// SetClip 写入播放区间；start/end 都为 0 时等价于清除。
func (s *Store) SetClip(songID string, c Clip) error {
	songID = strings.TrimSpace(songID)
	if songID == "" {
		return fmt.Errorf("store: 歌曲 ID 不能为空")
	}
	c = c.normalized()
	if c.StartMS < 0 {
		return fmt.Errorf("store: 片段起点不能为负")
	}
	if c.EndMS > 0 && c.EndMS <= c.StartMS {
		return fmt.Errorf("store: 片段终点必须大于起点")
	}
	if !c.Active() {
		return s.ClearClip(songID)
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("store: 编码播放区间: %w", err)
	}
	return s.SetSetting("clip:"+songID, string(raw))
}

// ClearClip 删除一首歌的播放区间。
func (s *Store) ClearClip(songID string) error {
	songID = strings.TrimSpace(songID)
	if songID == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM settings WHERE key = ?`, "clip:"+songID)
	if err != nil {
		return fmt.Errorf("store: 清除播放区间: %w", err)
	}
	return nil
}

func (c Clip) normalized() Clip {
	if c.StartMS < 0 {
		c.StartMS = 0
	}
	if c.EndMS < 0 {
		c.EndMS = 0
	}
	return c
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

// DeleteRemotePlaylists 删除所有远端收藏夹镜像及其条目（退出登录时用）。
func (s *Store) DeleteRemotePlaylists() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: 开启事务: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM playlist_refs WHERE ref_id IN (SELECT id FROM playlists WHERE kind = 'remote')`); err != nil {
		return fmt.Errorf("store: 删除远端歌单引用: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM playlist_excludes WHERE playlist_id IN (SELECT id FROM playlists WHERE kind = 'remote')`); err != nil {
		return fmt.Errorf("store: 删除远端歌单隐藏项: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM playlist_items WHERE playlist_id IN (SELECT id FROM playlists WHERE kind = 'remote')`); err != nil {
		return fmt.Errorf("store: 删除远端歌单条目: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM playlists WHERE kind = 'remote'`); err != nil {
		return fmt.Errorf("store: 删除远端歌单: %w", err)
	}
	return tx.Commit()
}

// CleanRemotePlaylistsOnce 是一次性维护：清空所有远端歌单镜像，保留本地歌单。
// marker 已存在时直接返回 false，因此升级后只清一次。
func (s *Store) CleanRemotePlaylistsOnce(marker string) (bool, error) {
	if marker == "" || s.Setting(marker, "") == "1" {
		return false, nil
	}
	if err := s.DeleteRemotePlaylists(); err != nil {
		return false, err
	}
	if err := s.SetSetting(marker, "1"); err != nil {
		return false, err
	}
	return true, nil
}

// DeleteRemotePlaylistsBySource 删除指定音源的远端歌单镜像（退出该源账号时用）。
func (s *Store) DeleteRemotePlaylistsBySource(source string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: 开启事务: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM playlist_refs WHERE ref_id IN
		(SELECT id FROM playlists WHERE kind = 'remote' AND source = ?)`, source); err != nil {
		return fmt.Errorf("store: 删除远端歌单引用: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM playlist_excludes WHERE playlist_id IN
		(SELECT id FROM playlists WHERE kind = 'remote' AND source = ?)`, source); err != nil {
		return fmt.Errorf("store: 删除远端歌单隐藏项: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM playlist_items WHERE playlist_id IN
		(SELECT id FROM playlists WHERE kind = 'remote' AND source = ?)`, source); err != nil {
		return fmt.Errorf("store: 删除远端歌单条目: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM playlists WHERE kind = 'remote' AND source = ?`, source); err != nil {
		return fmt.Errorf("store: 删除远端歌单: %w", err)
	}
	return tx.Commit()
}
