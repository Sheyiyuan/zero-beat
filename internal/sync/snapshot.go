package sync

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// PlaylistFile 是仓库里的歌单快照；字段顺序固定，保证连续导出逐字节一致。
type PlaylistFile struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Kind      string     `json:"kind"`
	CreatedAt int64      `json:"created_at"`
	Items     []SongFile `json:"items"`
	Refs      []RefFile  `json:"refs,omitempty"`
	Excludes  []string   `json:"excludes,omitempty"`
}

// SongFile 是一首歌的同步形态；不写 id，导入时由 (source, remote_id, page) 派生。
type SongFile struct {
	Source   string `json:"source"`
	RemoteID string `json:"remote_id"`
	BVID     string `json:"bvid,omitempty"`
	Page     int    `json:"page"`
	CID      int64  `json:"cid,omitempty"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Duration int    `json:"duration"`
	Cover    string `json:"cover"`
	AddedAt  int64  `json:"added_at"`
}

// RefFile 是混合歌单引用的另一个歌单；远端歌单只保留定位字段。
type RefFile struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Source   string `json:"source,omitempty"`
	RemoteID string `json:"remote_id,omitempty"`
}

// AnnotationFile 是单曲歌词偏移、单曲音量补偿、播放片段与歌词绑定；
// 一歌一文件，按歌曲 ID 稳定。LyricBind 只出现可跨设备复现的绑定。
type AnnotationFile struct {
	SongID        string              `json:"song_id"`
	LyricOffsetMS int                 `json:"lyric_offset_ms,omitempty"`
	CompDB        float64             `json:"comp_db,omitempty"`
	ClipStartMS   int64               `json:"clip_start_ms,omitempty"`
	ClipEndMS     int64               `json:"clip_end_ms,omitempty"`
	LyricBind     *store.LyricBinding `json:"lyric_bind,omitempty"`
}

// GitAttributes 关闭 JSON 的行级合并：同一实体两端都改就整版冲突。
const GitAttributes = `# zero-beat sync：同一实体两端都改时进入冲突，由用户整版选择。
playlists/*.json   merge=binary
annotations/*.json merge=binary
keymap.toml        merge=binary
meta.json          merge=binary
accounts/*.age     merge=binary
`

// Snapshot 是一次导出的全部仓库文件；Paths 为仓库相对路径。
type Snapshot struct {
	Files       map[string][]byte
	Playlists   int
	Annotations int
	Accounts    int
	Warnings    []string
}

// SnapshotInput 是导出所需的本地数据。
type SnapshotInput struct {
	Store      *store.Store
	KeymapPath string
	Key        []byte // nil 表示没有可用密钥，跳过登录态
	KeyVersion int
	Device     string
	// ExistingDir 是受管克隆根目录：登录态明文未变时复用已有密文，
	// 避免每次导出都因随机 nonce 产生无意义 diff。
	ExistingDir string
	Now         time.Time
}

// PlaylistPath 返回歌单在仓库里的相对路径；ID 已带 local-/mix- 前缀。
func PlaylistPath(id string) string { return PlaylistsDir + "/" + id + ".json" }

// AnnotationPath 返回单曲注释路径；歌曲 ID 做 URL 转义，避免冒号等字符影响文件名。
func AnnotationPath(songID string) string {
	return AnnotationsDir + "/" + url.PathEscape(songID) + ".json"
}

// AccountPath 返回登录态密文路径。
func AccountPath(source string) string { return AccountsDir + "/" + source + ".age" }

// BuildSnapshot 从本地库生成确定性快照。
func BuildSnapshot(in SnapshotInput) (Snapshot, error) {
	snap := Snapshot{Files: map[string][]byte{}}
	if in.Store == nil {
		return snap, fmt.Errorf("同步缺少存储")
	}
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	// 只导出可同步歌单；sync_excluded 的过滤在 store 里，导入删除共用同一份集合。
	playlists, err := in.Store.ListPlaylistSnapshots(store.PlaylistKindLocal, store.PlaylistKindMixin)
	if err != nil {
		return snap, err
	}
	sort.Slice(playlists, func(i, j int) bool { return playlists[i].ID < playlists[j].ID })
	for _, p := range playlists {
		// 防御：能走到这里的歌单一定是不排除同步的。如果它引用了文件夹歌单，
		// 说明库里已经处于不一致状态；静默导出会在别的设备上留下断引用。
		for _, ref := range p.Refs {
			if ref.Kind == store.PlaylistKindFolder {
				return snap, fmt.Errorf("歌单 %s 引用了文件夹歌单 %s，不能同步", p.Title, ref.Title)
			}
		}
		// 静默过滤本地曲目会在导入时删掉本机条目，所以宁可直接失败。
		for _, song := range p.Songs {
			if song.Source == "local" {
				return snap, fmt.Errorf("歌单 %s 含本地曲目 %q，不能同步", p.Title, song.Title)
			}
		}
		file := playlistFileFromSnapshot(p)
		data, err := encodeJSON(file)
		if err != nil {
			return snap, err
		}
		snap.Files[PlaylistPath(p.ID)] = data
		snap.Playlists++
	}

	// 本地曲目上的注释不进仓库：曲目本身不在仓库里，附属数据过去没有意义。
	annotations, err := in.Store.ListSyncAnnotations()
	if err != nil {
		return snap, err
	}
	for _, a := range annotations {
		if !a.Active() {
			continue
		}
		data, err := encodeJSON(annotationFileFrom(a))
		if err != nil {
			return snap, err
		}
		snap.Files[AnnotationPath(a.SongID)] = data
		snap.Annotations++
	}

	if in.KeymapPath != "" {
		if data, err := os.ReadFile(in.KeymapPath); err == nil {
			switch {
			case len(data) > 512*1024:
				snap.Warnings = append(snap.Warnings, "keymap.toml 过大，已跳过同步")
			case keymapInvalid(in.KeymapPath):
				snap.Warnings = append(snap.Warnings, "本地 keymap.toml 无效，已跳过同步")
			default:
				snap.Files[KeymapFile] = data
			}
		} else if !os.IsNotExist(err) {
			snap.Warnings = append(snap.Warnings, "读取 keymap.toml 失败："+err.Error())
		}
	}

	if len(in.Key) > 0 {
		accounts, err := in.Store.ListAccounts()
		if err != nil {
			return snap, err
		}
		for _, info := range accounts {
			payload, updatedAt, err := in.Store.LoadAccountAt(info.Source)
			if err != nil {
				snap.Warnings = append(snap.Warnings, "读取登录态失败："+info.Source)
				continue
			}
			var session map[string]string
			if err := json.Unmarshal(payload, &session); err != nil {
				snap.Warnings = append(snap.Warnings, "登录态格式无效："+info.Source)
				continue
			}
			if len(session) == 0 {
				continue
			}
			cred := Credential{Source: info.Source, UpdatedAt: updatedAt, Device: in.Device, Session: session}
			if reused, ok := reuseAccountCiphertext(in.ExistingDir, in.Key, in.KeyVersion, cred); ok {
				snap.Files[AccountPath(info.Source)] = reused
				snap.Accounts++
				continue
			}
			sealed, err := EncryptCredential(in.Key, info.Source, in.KeyVersion, cred)
			if err != nil {
				return snap, err
			}
			snap.Files[AccountPath(info.Source)] = sealed
			snap.Accounts++
		}
	}
	return snap, nil
}

// keymapInvalid 用与启动时相同的校验规则检查本地配置；无效时宁可不同步。
func keymapInvalid(path string) bool {
	km, _, err := keymap.LoadWithWarnings(path)
	return err != nil || km == nil
}

// reuseAccountCiphertext 在明文（音源、更新时间、会话）未变时沿用已有密文。
// 信封带随机 nonce，如果每次都重新加密，仓库每轮都会产生一次无意义提交。
func reuseAccountCiphertext(repoDir string, key []byte, keyVersion int, cred Credential) ([]byte, bool) {
	if repoDir == "" || len(key) == 0 {
		return nil, false
	}
	data, err := readRepoFile(repoDir, AccountPath(cred.Source))
	if err != nil {
		return nil, false
	}
	existing, err := DecryptCredential(key, cred.Source, keyVersion, data)
	if err != nil {
		return nil, false
	}
	if existing.UpdatedAt != cred.UpdatedAt || !sameSession(existing.Session, cred.Session) {
		return nil, false
	}
	return data, true
}

func sameSession(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func playlistFileFromSnapshot(p store.PlaylistSnapshot) PlaylistFile {
	file := PlaylistFile{
		ID:        p.ID,
		Title:     p.Title,
		Kind:      p.Kind,
		CreatedAt: p.CreatedAt,
		Items:     make([]SongFile, 0, len(p.Songs)),
	}
	for _, song := range p.Songs {
		file.Items = append(file.Items, SongFile{
			Source:   song.Source,
			RemoteID: song.ExternalID(),
			BVID:     song.BVID,
			Page:     song.Page,
			CID:      song.CID,
			Title:    song.Title,
			Artist:   song.Artist,
			Album:    song.Album,
			Duration: song.Duration,
			Cover:    song.Cover,
			AddedAt:  song.AddedAt.Unix(),
		})
	}
	for _, ref := range p.Refs {
		file.Refs = append(file.Refs, RefFile{
			ID:       ref.ID,
			Kind:     ref.Kind,
			Title:    ref.Title,
			Source:   ref.Source,
			RemoteID: ref.RemoteID,
		})
	}
	if len(p.Excludes) > 0 {
		file.Excludes = append([]string(nil), p.Excludes...)
		sort.Strings(file.Excludes)
	}
	return file
}

func annotationFileFrom(a store.SongAnnotation) AnnotationFile {
	file := AnnotationFile{
		SongID:        a.SongID,
		LyricOffsetMS: a.LyricOffsetMS,
		CompDB:        a.CompDB,
		ClipStartMS:   a.ClipStartMS,
		ClipEndMS:     a.ClipEndMS,
	}
	if a.LyricBind != nil && a.LyricBind.Portable() {
		bind := *a.LyricBind
		file.LyricBind = &bind
	}
	return file
}

// WriteSnapshot 把快照写入受管克隆。playlists/ 与 annotations/ 先清空再重写，
// 避免残留文件复活已删除歌单；accounts/ 与 keymap.toml 不清空（删除不传播）。
func WriteSnapshot(dir string, snap Snapshot) error {
	for _, sub := range []string{PlaylistsDir, AnnotationsDir, AccountsDir} {
		if err := ensureDir(filepath.Join(dir, sub), 0o700); err != nil {
			return err
		}
	}
	for _, sub := range []string{PlaylistsDir, AnnotationsDir} {
		if err := os.RemoveAll(filepath.Join(dir, sub)); err != nil {
			return fmt.Errorf("清空 %s: %w", sub, err)
		}
		if err := ensureDir(filepath.Join(dir, sub), 0o700); err != nil {
			return err
		}
	}
	paths := make([]string, 0, len(snap.Files))
	for path := range snap.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := writeFileAtomic(full, snap.Files[path], 0o600); err != nil {
			return err
		}
	}
	return writeFileAtomic(filepath.Join(dir, GitattributesFile), []byte(GitAttributes), 0o600)
}

// ImportInput 是导入所需的本地数据。
type ImportInput struct {
	Store      *store.Store
	Dir        string
	KeymapPath string
	ArchiveDir string // 同步根目录；实际写入 ArchiveDir/archive/<time>/
	// KeepLocal 用于新设备/首次同步的「先导入远端、再导出合并」：
	// 只做增量导入，不删除远端没有的本地歌单，也不覆盖已有的本地 keymap。
	KeepLocal bool
	Now       time.Time
}

// ImportResult 描述一次导入的结果。
type ImportResult struct {
	Playlists    int
	Deleted      []string
	Annotations  int
	KeymapUpdate bool
	Warnings     []string
}

// ImportSnapshot 把工作区里的仓库文件导入本地库。单个实体失败只记录警告，
// 不中断其他实体；I/O 级错误才返回 error。
func ImportSnapshot(in ImportInput) (ImportResult, error) {
	res := ImportResult{}
	if in.Store == nil {
		return res, fmt.Errorf("同步缺少存储")
	}
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	remoteIDs, warnings := importPlaylists(in)
	res.Playlists = len(remoteIDs)
	res.Warnings = append(res.Warnings, warnings...)

	if !in.KeepLocal {
		local, err := in.Store.ListPlaylistSnapshots("local", "mixin")
		if err != nil {
			return res, err
		}
		for _, p := range local {
			if remoteIDs[p.ID] {
				continue
			}
			if in.ArchiveDir != "" {
				if err := archivePlaylist(in.ArchiveDir, p, in.Now); err != nil {
					res.Warnings = append(res.Warnings, "归档删除的歌单失败："+p.ID)
				}
			}
			if err := in.Store.DeletePlaylistForSync(p.ID); err != nil {
				res.Warnings = append(res.Warnings, "删除本地歌单失败："+p.ID)
				continue
			}
			res.Deleted = append(res.Deleted, p.ID)
		}
	}

	annotationIDs, warnings := importAnnotations(in)
	res.Annotations = len(annotationIDs)
	res.Warnings = append(res.Warnings, warnings...)
	if !in.KeepLocal {
		if err := deleteMissingAnnotations(in.Store, annotationIDs); err != nil {
			res.Warnings = append(res.Warnings, err.Error())
		}
	}

	if in.KeymapPath != "" {
		updated, warning := importKeymap(in.Dir, in.KeymapPath, in.KeepLocal)
		res.KeymapUpdate = updated
		if warning != "" {
			res.Warnings = append(res.Warnings, warning)
		}
	}
	return res, nil
}

func importPlaylists(in ImportInput) (map[string]bool, []string) {
	ids := map[string]bool{}
	var warnings []string
	entries, err := os.ReadDir(filepath.Join(in.Dir, PlaylistsDir))
	if err != nil && !os.IsNotExist(err) {
		warnings = append(warnings, "读取 playlists/ 失败："+err.Error())
		return ids, warnings
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		data, err := readRepoFile(in.Dir, PlaylistsDir+"/"+name)
		if err != nil {
			ids[id] = true
			warnings = append(warnings, "读取歌单失败："+name)
			continue
		}
		var file PlaylistFile
		if err := decodeJSONStrict(data, &file); err != nil {
			ids[id] = true
			warnings = append(warnings, "解析歌单失败："+name)
			continue
		}
		if file.ID != id {
			ids[id] = true
			warnings = append(warnings, "歌单 ID 与文件名不符："+name)
			continue
		}
		snap := snapshotFromFile(file)
		if err := store.ValidatePlaylistSnapshot(snap); err != nil {
			ids[id] = true
			warnings = append(warnings, "歌单无效："+name+"（"+err.Error()+"）")
			continue
		}
		if err := in.Store.ImportPlaylistSnapshot(snap); err != nil {
			ids[id] = true
			warnings = append(warnings, "导入歌单失败："+name+"（"+err.Error()+"）")
			continue
		}
		ids[id] = true
	}
	return ids, warnings
}

func snapshotFromFile(file PlaylistFile) store.PlaylistSnapshot {
	snap := store.PlaylistSnapshot{
		Playlist: store.Playlist{
			ID:        file.ID,
			Title:     file.Title,
			Kind:      file.Kind,
			CreatedAt: file.CreatedAt,
		},
		Excludes: append([]string(nil), file.Excludes...),
	}
	for _, item := range file.Items {
		snap.Songs = append(snap.Songs, store.Song{
			Source:   item.Source,
			RemoteID: item.RemoteID,
			BVID:     item.BVID,
			Page:     item.Page,
			CID:      item.CID,
			Title:    item.Title,
			Artist:   item.Artist,
			Album:    item.Album,
			Duration: item.Duration,
			Cover:    item.Cover,
			AddedAt:  time.Unix(item.AddedAt, 0),
		})
	}
	for _, ref := range file.Refs {
		snap.Refs = append(snap.Refs, store.Playlist{
			ID:       ref.ID,
			Kind:     ref.Kind,
			Title:    ref.Title,
			Source:   ref.Source,
			RemoteID: ref.RemoteID,
		})
	}
	return snap
}

func importAnnotations(in ImportInput) (map[string]bool, []string) {
	ids := map[string]bool{}
	var warnings []string
	entries, err := os.ReadDir(filepath.Join(in.Dir, AnnotationsDir))
	if err != nil && !os.IsNotExist(err) {
		warnings = append(warnings, "读取 annotations/ 失败："+err.Error())
		return ids, warnings
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		stem := strings.TrimSuffix(name, ".json")
		songID, err := url.PathUnescape(stem)
		if err != nil || songID == "" || len(songID) > 300 {
			warnings = append(warnings, "注释文件名无效："+name)
			continue
		}
		// 本地曲目注释不该出现在仓库；出现了也不导入，免得在本机留下孤儿设置。
		if strings.HasPrefix(songID, "local:") {
			warnings = append(warnings, "仓库含本地曲目注释，已跳过："+name)
			continue
		}
		ids[songID] = true
		data, err := readRepoFile(in.Dir, AnnotationsDir+"/"+name)
		if err != nil {
			warnings = append(warnings, "读取注释失败："+name)
			continue
		}
		var file AnnotationFile
		if err := decodeJSONStrict(data, &file); err != nil {
			warnings = append(warnings, "解析注释失败："+name)
			continue
		}
		if file.SongID != songID {
			warnings = append(warnings, "注释歌曲 ID 与文件名不符："+name)
			continue
		}
		annotation := store.SongAnnotation{
			SongID:        file.SongID,
			LyricOffsetMS: file.LyricOffsetMS,
			CompDB:        file.CompDB,
			ClipStartMS:   file.ClipStartMS,
			ClipEndMS:     file.ClipEndMS,
		}
		if file.LyricBind != nil {
			if !file.LyricBind.Portable() {
				// 仓库里出现本机绑定说明导出端有 bug；不导入，免得把路径带到这台设备。
				warnings = append(warnings, "注释含本机歌词绑定，已忽略："+name)
			} else {
				annotation.LyricBind = file.LyricBind
			}
		}
		if err := in.Store.SaveAnnotation(annotation); err != nil {
			warnings = append(warnings, "写入注释失败："+name)
		}
	}
	return ids, warnings
}

// deleteMissingAnnotations 只清理「可同步」的注释集合，
// 本机曲目上的注释不会因为远端没有就被删掉。
func deleteMissingAnnotations(st *store.Store, keep map[string]bool) error {
	local, err := st.ListSyncAnnotations()
	if err != nil {
		return err
	}
	for _, a := range local {
		if keep[a.SongID] {
			continue
		}
		if err := st.DeleteAnnotation(a.SongID); err != nil {
			return err
		}
	}
	return nil
}

func importKeymap(dir, keymapPath string, keepLocal bool) (bool, string) {
	remote := filepath.Join(dir, KeymapFile)
	info, err := os.Lstat(remote)
	if err != nil {
		// 远端没有 keymap.toml 时保留本地配置，删除不传播。
		return false, ""
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, "远端 keymap.toml 不是普通文件，已跳过"
	}
	if keepLocal && fileExists(keymapPath) {
		// 首次同步且本地已有配置：不覆盖，交给合并冲突让用户选。
		return false, "本地已有 keymap.toml，首次同步会按冲突处理"
	}
	if info.Size() > 512*1024 {
		return false, "远端 keymap.toml 过大，已跳过"
	}
	km, warnings, err := keymap.LoadWithWarnings(remote)
	if err != nil || km == nil {
		return false, "远端 keymap.toml 无效，已跳过：" + errText(err)
	}
	data, err := os.ReadFile(remote)
	if err != nil {
		return false, ""
	}
	current, err := os.ReadFile(keymapPath)
	if err == nil && string(current) == string(data) {
		return false, ""
	}
	if err := writeFileAtomic(keymapPath, data, 0o600); err != nil {
		return false, "写入 keymap.toml 失败：" + err.Error()
	}
	if len(warnings) > 0 {
		return true, "远端 keymap.toml 部分降级：" + strings.Join(warnings, "；")
	}
	return true, ""
}

func errText(err error) string {
	if err == nil {
		return "格式错误"
	}
	return err.Error()
}

func archivePlaylist(root string, p store.PlaylistSnapshot, now time.Time) error {
	file := playlistFileFromSnapshot(p)
	data, err := encodeJSON(file)
	if err != nil {
		return err
	}
	path := filepath.Join(archivePath(root, now), "deleted-playlists", p.ID+".json")
	return writeFileAtomic(path, data, 0o600)
}

// DecodePlaylistFile 解析一个歌单文件（供冲突摘要和测试使用）。
func DecodePlaylistFile(data []byte) (PlaylistFile, error) {
	var file PlaylistFile
	if err := decodeJSONStrict(data, &file); err != nil {
		return PlaylistFile{}, err
	}
	return file, nil
}
