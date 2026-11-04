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
	"sort"
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

// Playlist 是一个歌单，Kind 为「remote」（某音源的远端歌单/收藏夹镜像）、
// 「local」或「mixin」（引用其他歌单并叠加自己的曲目）。
// Frozen 只对远端镜像有意义：登录态失效或退出登录后停止刷新，但保留本地曲目，
// 混合歌单对它的引用也不会断。
type Playlist struct {
	ID        string
	Source    string
	Title     string
	Kind      string
	RemoteID  string
	Count     int
	Frozen    bool
	CreatedAt int64
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
	frozen    INTEGER NOT NULL DEFAULT 0,
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
	hasPlaylistFrozen, err := s.hasColumn("playlists", "frozen")
	if err != nil {
		return err
	}
	if !hasPlaylistFrozen {
		if _, err := s.db.Exec(`ALTER TABLE playlists ADD COLUMN frozen INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("store: 升级歌单冻结字段: %w", err)
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

// EnsurePlaylist 确保歌单存在（按 ID 幂等）。重新导入或登录后补齐远端歌单时
// 会顺带解冻，让镜像恢复刷新。
func (s *Store) EnsurePlaylist(p Playlist) (Playlist, error) {
	if p.ID == "" {
		return Playlist{}, fmt.Errorf("store: 歌单缺少 ID")
	}
	if p.Kind == "" {
		p.Kind = "local"
	}
	_, err := s.db.Exec(`
INSERT INTO playlists (id, source, title, kind, remote_id, frozen, created_at) VALUES (?, ?, ?, ?, ?, 0, ?)
ON CONFLICT(id) DO UPDATE SET source = excluded.source, title = excluded.title,
	kind = excluded.kind, remote_id = excluded.remote_id, frozen = 0`,
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
	if _, err := s.db.Exec(`UPDATE playlists SET kind = 'local', source = '', remote_id = '', frozen = 0 WHERE id = ?`, id); err != nil {
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
SELECT p.id, p.source, p.title, p.kind, p.remote_id, p.frozen, p.created_at,
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
		if err := rows.Scan(&p.ID, &p.Source, &p.Title, &p.Kind, &p.RemoteID, &p.Frozen, &p.CreatedAt, &p.Count); err != nil {
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
SELECT p.id, p.source, p.title, p.kind, p.remote_id, p.frozen, p.created_at,
       (SELECT COUNT(*) FROM playlist_items i WHERE i.playlist_id = p.id)
FROM playlists p WHERE p.id = ?`, id)
	var p Playlist
	err := row.Scan(&p.ID, &p.Source, &p.Title, &p.Kind, &p.RemoteID, &p.Frozen, &p.CreatedAt, &p.Count)
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
SELECT p.id, p.source, p.title, p.kind, p.remote_id, p.frozen, p.created_at,
       (SELECT COUNT(*) FROM playlist_items i WHERE i.playlist_id = p.id)
FROM playlists p
ORDER BY CASE p.kind WHEN 'remote' THEN 0 WHEN 'mixin' THEN 1 ELSE 2 END, p.created_at`)
	if err != nil {
		return nil, fmt.Errorf("store: 列出歌单: %w", err)
	}
	var out []Playlist
	for rows.Next() {
		var p Playlist
		if err := rows.Scan(&p.ID, &p.Source, &p.Title, &p.Kind, &p.RemoteID, &p.Frozen, &p.CreatedAt, &p.Count); err != nil {
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

// SongCompDB 读取一首歌的额外音量补偿（dB，相对全局）；没有设置时返回 0。
func (s *Store) SongCompDB(songID string) (float64, error) {
	songID = strings.TrimSpace(songID)
	if songID == "" {
		return 0, nil
	}
	raw := strings.TrimSpace(s.Setting("song_comp:"+songID, ""))
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("store: 解析单曲音量补偿: %w", err)
	}
	return v, nil
}

// SetSongCompDB 写入一首歌的额外音量补偿；0 dB 等价于清除，范围 ±12 dB。
func (s *Store) SetSongCompDB(songID string, db float64) error {
	songID = strings.TrimSpace(songID)
	if songID == "" {
		return fmt.Errorf("store: 歌曲 ID 不能为空")
	}
	if db < -12 {
		db = -12
	}
	if db > 12 {
		db = 12
	}
	if db == 0 {
		_, err := s.db.Exec(`DELETE FROM settings WHERE key = ?`, "song_comp:"+songID)
		if err != nil {
			return fmt.Errorf("store: 清除单曲音量补偿: %w", err)
		}
		return nil
	}
	return s.SetSetting("song_comp:"+songID, strconv.FormatFloat(db, 'f', 1, 64))
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

// SaveAccount 保存音源登录态（JSON）。拿到新登录态意味着该音源重新可用，
// 顺带解冻它的远端歌单镜像，让下次刷新恢复同步。
func (s *Store) SaveAccount(source string, payload []byte) error {
	_, err := s.db.Exec(`INSERT INTO accounts (source, payload, updated_at) VALUES (?, ?, ?)
ON CONFLICT(source) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at`,
		source, string(payload), time.Now().Unix())
	if err != nil {
		return fmt.Errorf("store: 保存登录态: %w", err)
	}
	s.unfreezeSource(source)
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

// DeleteRemotePlaylists 清空所有远端收藏夹镜像及其条目；只给显式清理用。
// 退出登录不再删镜像，而是按音源冻结（见 FreezeRemotePlaylistsBySource）。
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

// FreezeRemotePlaylistsBySource 冻结某音源的远端歌单镜像：保留歌单、曲目、
// 隐藏项与混合歌单引用，只停止刷新。登录态过期或用户退出登录时调用。
func (s *Store) FreezeRemotePlaylistsBySource(source string) (int, error) {
	return s.setSourcePlaylistsFrozen(source, true)
}

// UnfreezeRemotePlaylistsBySource 解冻某音源的远端歌单镜像，重新登录后调用。
func (s *Store) UnfreezeRemotePlaylistsBySource(source string) (int, error) {
	return s.setSourcePlaylistsFrozen(source, false)
}

func (s *Store) unfreezeSource(source string) {
	_, _ = s.setSourcePlaylistsFrozen(source, false)
}

func (s *Store) setSourcePlaylistsFrozen(source string, frozen bool) (int, error) {
	value := 0
	if frozen {
		value = 1
	}
	res, err := s.db.Exec(`UPDATE playlists SET frozen = ? WHERE kind = 'remote' AND source = ? AND frozen <> ?`,
		value, source, value)
	if err != nil {
		return 0, fmt.Errorf("store: 更新歌单冻结状态: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: 读取冻结数量: %w", err)
	}
	return int(n), nil
}

// DeleteRemotePlaylistsBySource 显式清空指定音源的远端歌单镜像。
// 退出登录不走这里：那会连同混合歌单的引用一起删掉，改为冻结。
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

// AccountInfo 是音源登录态的元信息，不含 payload。
type AccountInfo struct {
	Source    string
	UpdatedAt int64
}

// ListAccounts 列出本地保存了登录态的音源。
func (s *Store) ListAccounts() ([]AccountInfo, error) {
	rows, err := s.db.Query(`SELECT source, updated_at FROM accounts ORDER BY source`)
	if err != nil {
		return nil, fmt.Errorf("store: 列出登录态: %w", err)
	}
	defer rows.Close()
	var out []AccountInfo
	for rows.Next() {
		var a AccountInfo
		if err := rows.Scan(&a.Source, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AccountUpdatedAt 返回音源登录态的最后修改时间；没有记录时返回 ErrNotFound。
func (s *Store) AccountUpdatedAt(source string) (int64, error) {
	var updated int64
	err := s.db.QueryRow(`SELECT updated_at FROM accounts WHERE source = ?`, source).Scan(&updated)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("store: 读取登录态时间: %w", err)
	}
	return updated, nil
}

// LoadAccountAt 读取登录态及其最后修改时间。
func (s *Store) LoadAccountAt(source string) ([]byte, int64, error) {
	var (
		payload string
		updated int64
	)
	err := s.db.QueryRow(`SELECT payload, updated_at FROM accounts WHERE source = ?`, source).Scan(&payload, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("store: 读取登录态: %w", err)
	}
	return []byte(payload), updated, nil
}

// SaveAccountAt 保存登录态并保留给定的修改时间；updatedAt 为 0 时使用当前时间。
// 同步导入用它保留远端时间戳，让 LWW 比较有稳定基准。
func (s *Store) SaveAccountAt(source string, payload []byte, updatedAt int64) error {
	if updatedAt <= 0 {
		updatedAt = time.Now().Unix()
	}
	_, err := s.db.Exec(`INSERT INTO accounts (source, payload, updated_at) VALUES (?, ?, ?)
ON CONFLICT(source) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at`,
		source, string(payload), updatedAt)
	if err != nil {
		return fmt.Errorf("store: 保存登录态: %w", err)
	}
	s.unfreezeSource(source)
	return nil
}

// PlaylistSnapshot 是歌单的完整可导出形态：歌单行、自己加入的曲目、混合歌单的
// 引用与被隐藏的继承曲目。与 PlaylistSongs 不同，它不展开混合歌单。
type PlaylistSnapshot struct {
	Playlist
	Songs    []Song     // 仅 playlist_items，按 position；AddedAt 取 playlist_items.added_at
	Refs     []Playlist // mixin 引用，按 position
	Excludes []string   // 被隐藏的继承曲目 ID，已排序
}

// playlistRow 只读歌单行本身，不计算曲目数、不展开混合歌单；同步导出用。
func (s *Store) playlistRow(id string) (Playlist, error) {
	row := s.db.QueryRow(`SELECT id, source, title, kind, remote_id, created_at FROM playlists WHERE id = ?`, id)
	var p Playlist
	err := row.Scan(&p.ID, &p.Source, &p.Title, &p.Kind, &p.RemoteID, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Playlist{}, ErrNotFound
	}
	if err != nil {
		return Playlist{}, fmt.Errorf("store: 读歌单行: %w", err)
	}
	return p, nil
}

// PlaylistSnapshot 读取一个歌单的完整快照（不展开混合歌单）。
func (s *Store) PlaylistSnapshot(id string) (PlaylistSnapshot, error) {
	p, err := s.playlistRow(id)
	if err != nil {
		return PlaylistSnapshot{}, err
	}
	return s.playlistSnapshotRow(p)
}

func (s *Store) playlistSnapshotRow(p Playlist) (PlaylistSnapshot, error) {
	snap := PlaylistSnapshot{Playlist: p}
	rows, err := s.db.Query(`
SELECT s.id, s.source, s.remote_id, s.bvid, s.page, s.cid, s.title, s.artist, s.album,
       s.duration, s.cover, i.added_at
FROM playlist_items i JOIN songs s ON s.id = i.song_id
WHERE i.playlist_id = ? ORDER BY i.position`, p.ID)
	if err != nil {
		return PlaylistSnapshot{}, fmt.Errorf("store: 读取歌单快照: %w", err)
	}
	for rows.Next() {
		song, err := scanSong(rows)
		if err != nil {
			rows.Close()
			return PlaylistSnapshot{}, err
		}
		snap.Songs = append(snap.Songs, song)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return PlaylistSnapshot{}, err
	}
	rows.Close()

	if p.Kind == "mixin" {
		refs, err := s.MixinRefs(p.ID)
		if err != nil {
			return PlaylistSnapshot{}, err
		}
		snap.Refs = refs
		excludes, err := s.playlistExcludes(p.ID)
		if err != nil {
			return PlaylistSnapshot{}, err
		}
		snap.Excludes = excludes
	}
	return snap, nil
}

// ListPlaylistSnapshots 按导出顺序列出歌单快照；kinds 为空时返回全部。
// 它不计算曲目数、不展开混合歌单，避免大曲库导出时重复展开引用。
func (s *Store) ListPlaylistSnapshots(kinds ...string) ([]PlaylistSnapshot, error) {
	rows, err := s.db.Query(`
SELECT id, source, title, kind, remote_id, created_at FROM playlists
ORDER BY CASE kind WHEN 'remote' THEN 0 WHEN 'mixin' THEN 1 ELSE 2 END, created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("store: 列出歌单快照: %w", err)
	}
	want := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}
	var playlists []Playlist
	for rows.Next() {
		var p Playlist
		if err := rows.Scan(&p.ID, &p.Source, &p.Title, &p.Kind, &p.RemoteID, &p.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if len(want) > 0 && !want[p.Kind] {
			continue
		}
		playlists = append(playlists, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	out := make([]PlaylistSnapshot, 0, len(playlists))
	for _, p := range playlists {
		snap, err := s.playlistSnapshotRow(p)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, nil
}

func (s *Store) playlistExcludes(playlistID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT song_id FROM playlist_excludes WHERE playlist_id = ?`, playlistID)
	if err != nil {
		return nil, fmt.Errorf("store: 读取歌单隐藏项: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, rows.Err()
}

// ImportPlaylistSnapshot 在单事务内写入快照：歌单行、歌曲、条目、引用与隐藏项。
// created_at / added_at 按快照保留。引用歌单缺失时先建占位（远端歌单不导出）。
func (s *Store) ImportPlaylistSnapshot(snap PlaylistSnapshot) error {
	if err := ValidatePlaylistSnapshot(snap); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: 开启事务: %w", err)
	}
	defer tx.Rollback()

	for _, ref := range snap.Refs {
		if err := importPlaylistRefPlaceholder(tx, ref); err != nil {
			return err
		}
	}

	createdAt := snap.CreatedAt
	if createdAt <= 0 {
		createdAt = time.Now().Unix()
	}
	if _, err := tx.Exec(`
INSERT INTO playlists (id, source, title, kind, remote_id, created_at) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET title = excluded.title, kind = excluded.kind,
	source = excluded.source, remote_id = excluded.remote_id`,
		snap.ID, snap.Source, snap.Title, snap.Kind, snap.RemoteID, createdAt); err != nil {
		return fmt.Errorf("store: 写入歌单: %w", err)
	}
	for _, q := range []string{
		`DELETE FROM playlist_items WHERE playlist_id = ?`,
		`DELETE FROM playlist_refs WHERE playlist_id = ?`,
		`DELETE FROM playlist_excludes WHERE playlist_id = ?`,
	} {
		if _, err := tx.Exec(q, snap.ID); err != nil {
			return fmt.Errorf("store: 覆盖歌单: %w", err)
		}
	}
	for i, song := range snap.Songs {
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
			snap.ID, song.ID, i+1, added.Unix()); err != nil {
			return fmt.Errorf("store: 写入歌单项: %w", err)
		}
	}
	for i, ref := range snap.Refs {
		if _, err := tx.Exec(`INSERT INTO playlist_refs (playlist_id, ref_id, position) VALUES (?, ?, ?)`,
			snap.ID, ref.ID, i); err != nil {
			return fmt.Errorf("store: 写入歌单引用: %w", err)
		}
	}
	for _, songID := range snap.Excludes {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO playlist_excludes (playlist_id, song_id) VALUES (?, ?)`,
			snap.ID, songID); err != nil {
			return fmt.Errorf("store: 写入歌单隐藏项: %w", err)
		}
	}
	return tx.Commit()
}

func importPlaylistRefPlaceholder(tx *sql.Tx, ref Playlist) error {
	if err := validatePlaylistID(ref.Kind, ref.ID); err != nil {
		return err
	}
	title := strings.TrimSpace(ref.Title)
	if title == "" {
		title = ref.ID
	}
	created := ref.CreatedAt
	if created <= 0 {
		created = time.Now().Unix()
	}
	_, err := tx.Exec(`
INSERT INTO playlists (id, source, title, kind, remote_id, created_at) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING`,
		ref.ID, ref.Source, title, ref.Kind, ref.RemoteID, created)
	if err != nil {
		return fmt.Errorf("store: 写入引用歌单占位: %w", err)
	}
	return nil
}

// DeletePlaylistForSync 删除本地/混合歌单及其条目，并清掉其他混合歌单对它的引用。
// 与 DeletePlaylist 不同，它不阻止被引用的歌单被删除：同步导入以远端为准。
func (s *Store) DeletePlaylistForSync(id string) error {
	p, err := s.Playlist(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if p.Kind != "local" && p.Kind != "mixin" {
		return fmt.Errorf("store: 远端歌单不能由同步删除")
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
	if _, err := tx.Exec(`DELETE FROM playlist_refs WHERE ref_id = ?`, id); err != nil {
		return fmt.Errorf("store: 清理歌单引用: %w", err)
	}
	return tx.Commit()
}

// ValidatePlaylistSnapshot 校验同步导入的歌单字段，拒绝路径字符与非法 kind。
func ValidatePlaylistSnapshot(snap PlaylistSnapshot) error {
	if err := validatePlaylistID(snap.Kind, snap.ID); err != nil {
		return err
	}
	title := strings.TrimSpace(snap.Title)
	if title == "" {
		return fmt.Errorf("store: 歌单标题不能为空")
	}
	if len([]rune(title)) > 200 {
		return fmt.Errorf("store: 歌单标题过长")
	}
	for _, ref := range snap.Refs {
		if ref.Kind == "mixin" {
			return fmt.Errorf("store: 混合歌单不能再引用混合歌单")
		}
		if err := validatePlaylistID(ref.Kind, ref.ID); err != nil {
			return err
		}
		if ref.ID == snap.ID {
			return fmt.Errorf("store: 歌单不能引用自己")
		}
	}
	if len(snap.Songs) > maxSnapshotSongs {
		return fmt.Errorf("store: 歌单曲目过多")
	}
	return nil
}

const maxSnapshotSongs = 20000

func validatePlaylistID(kind, id string) error {
	if id == "" || len(id) > 200 {
		return fmt.Errorf("store: 歌单 ID 长度无效")
	}
	switch kind {
	case "local":
		if !strings.HasPrefix(id, "local-") || len(id) == len("local-") {
			return fmt.Errorf("store: 本地歌单 ID 必须以 local- 开头")
		}
	case "mixin":
		if !strings.HasPrefix(id, "mix-") || len(id) == len("mix-") {
			return fmt.Errorf("store: 混合歌单 ID 必须以 mix- 开头")
		}
	case "remote":
		// 远端镜像 ID 由适配器生成（bili-fav-* / netease-*），只要求安全字符。
	default:
		return fmt.Errorf("store: 未知歌单类型 %q", kind)
	}
	for _, r := range id {
		if r == '/' || r == '\\' || r == 0 || r < 0x20 || r == 0x7f {
			return fmt.Errorf("store: 歌单 ID 含非法字符")
		}
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("store: 歌单 ID 含非法路径片段")
	}
	return nil
}

// SongAnnotation 是单曲的同步附属信息：歌词偏移与播放片段。
type SongAnnotation struct {
	SongID        string  `json:"song_id"`
	LyricOffsetMS int     `json:"lyric_offset_ms,omitempty"`
	CompDB        float64 `json:"comp_db,omitempty"`
	ClipStartMS   int64   `json:"clip_start_ms,omitempty"`
	ClipEndMS     int64   `json:"clip_end_ms,omitempty"`
}

// Active 判断注释里是否有需要同步的内容。
func (a SongAnnotation) Active() bool {
	return a.LyricOffsetMS != 0 || a.CompDB != 0 || a.ClipStartMS > 0 || a.ClipEndMS > 0
}

// ListAnnotations 列出全部单曲注释（歌词偏移、单曲补偿与片段都存于 settings）。
func (s *Store) ListAnnotations() ([]SongAnnotation, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings WHERE key LIKE 'clip:%' OR key LIKE 'lyric_offset:%' OR key LIKE 'song_comp:%'`)
	if err != nil {
		return nil, fmt.Errorf("store: 读取单曲注释: %w", err)
	}
	defer rows.Close()
	byID := map[string]*SongAnnotation{}
	get := func(id string) *SongAnnotation {
		a := byID[id]
		if a == nil {
			a = &SongAnnotation{SongID: id}
			byID[id] = a
		}
		return a
	}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		switch {
		case strings.HasPrefix(key, "clip:"):
			var c Clip
			if err := json.Unmarshal([]byte(value), &c); err != nil {
				continue
			}
			a := get(strings.TrimPrefix(key, "clip:"))
			a.ClipStartMS, a.ClipEndMS = c.StartMS, c.EndMS
		case strings.HasPrefix(key, "lyric_offset:"):
			offset, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				continue
			}
			get(strings.TrimPrefix(key, "lyric_offset:")).LyricOffsetMS = offset
		case strings.HasPrefix(key, "song_comp:"):
			comp, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				continue
			}
			get(strings.TrimPrefix(key, "song_comp:")).CompDB = comp
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]SongAnnotation, 0, len(byID))
	for _, a := range byID {
		if a.Active() {
			out = append(out, *a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SongID < out[j].SongID })
	return out, nil
}

// SaveAnnotation 写入单曲注释；零值字段对应的设置会被删除。
func (s *Store) SaveAnnotation(a SongAnnotation) error {
	a.SongID = strings.TrimSpace(a.SongID)
	if a.SongID == "" {
		return fmt.Errorf("store: 注释缺少歌曲 ID")
	}
	if !a.Active() {
		return s.DeleteAnnotation(a.SongID)
	}
	if a.LyricOffsetMS != 0 {
		if err := s.SetSetting("lyric_offset:"+a.SongID, strconv.Itoa(a.LyricOffsetMS)); err != nil {
			return err
		}
	} else {
		if _, err := s.db.Exec(`DELETE FROM settings WHERE key = ?`, "lyric_offset:"+a.SongID); err != nil {
			return fmt.Errorf("store: 清除歌词偏移: %w", err)
		}
	}
	if err := s.SetSongCompDB(a.SongID, a.CompDB); err != nil {
		return err
	}
	return s.SetClip(a.SongID, Clip{StartMS: a.ClipStartMS, EndMS: a.ClipEndMS})
}

// DeleteAnnotation 清除一首歌的歌词偏移、单曲补偿与片段。
func (s *Store) DeleteAnnotation(songID string) error {
	if _, err := s.db.Exec(`DELETE FROM settings WHERE key = ? OR key = ? OR key = ?`,
		"clip:"+songID, "lyric_offset:"+songID, "song_comp:"+songID); err != nil {
		return fmt.Errorf("store: 清除单曲注释: %w", err)
	}
	return nil
}
