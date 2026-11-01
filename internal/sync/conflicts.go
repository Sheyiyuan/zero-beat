package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// 冲突实体类型。
const (
	KindPlaylist   = "playlist"
	KindKeymap     = "keymap"
	KindAnnotation = "annotation"
)

// Choice 是用户对单个冲突实体的选择。
type Choice string

const (
	KeepLocal  Choice = "local"
	KeepRemote Choice = "remote"
	KeepBoth   Choice = "both"
)

// ConflictEntity 是冲突列表的一项；版本文件按序号存在 tx 目录里。
type ConflictEntity struct {
	Index         int    `json:"index"`
	Entity        string `json:"entity"` // playlist:<id> / keymap / annotation:<songID>
	Kind          string `json:"kind"`
	ID            string `json:"id,omitempty"`
	Path          string `json:"path"`
	Local         bool   `json:"local"`
	Remote        bool   `json:"remote"`
	Base          bool   `json:"base"`
	DeletedLocal  bool   `json:"deleted_local,omitempty"`
	DeletedRemote bool   `json:"deleted_remote,omitempty"`
	Title         string `json:"title,omitempty"`
	LocalTitle    string `json:"local_title,omitempty"`
	RemoteTitle   string `json:"remote_title,omitempty"`
	LocalCount    int    `json:"local_count,omitempty"`
	RemoteCount   int    `json:"remote_count,omitempty"`
	LocalRefs     int    `json:"local_refs,omitempty"`
	RemoteRefs    int    `json:"remote_refs,omitempty"`
	LocalUpdated  int64  `json:"local_updated,omitempty"`
	RemoteUpdated int64  `json:"remote_updated,omitempty"`
}

// ConflictIndex 是一个冲突事务的索引，写入 conflicts/<txid>/index.json。
type ConflictIndex struct {
	TxID      string           `json:"txid"`
	CreatedAt int64            `json:"created_at"`
	MergeHead string           `json:"merge_head"`
	Entities  []ConflictEntity `json:"entities"`
}

// SupportsBoth 判断实体是否提供「保留两者」。
func (e ConflictEntity) SupportsBoth() bool {
	return e.Kind == KindPlaylist && e.Local && e.Remote && !e.DeletedLocal && !e.DeletedRemote
}

func (e ConflictEntity) versionFile(which string) string {
	return fmt.Sprintf("%d.%s", e.Index, which)
}

// ConflictDir 返回冲突事务目录。
func ConflictDir(syncDir, txid string) string {
	return filepath.Join(syncDir, "conflicts", txid)
}

// NewTxID 生成冲突事务 ID：时间前缀便于排序，随机后缀避免碰撞。
func NewTxID(now time.Time) string {
	return now.Format("20060102-150405") + "-" + DeviceID()
}

// collectConflicts 把 git 的冲突三份版本落盘并生成索引。
func collectConflicts(ctx context.Context, g *Git, syncDir string, paths []string, mergeHead string, now time.Time) (ConflictIndex, error) {
	sort.Strings(paths)
	txid := NewTxID(now)
	dir := ConflictDir(syncDir, txid)
	if err := ensureDir(dir, 0o700); err != nil {
		return ConflictIndex{}, err
	}
	index := ConflictIndex{TxID: txid, CreatedAt: now.Unix(), MergeHead: mergeHead}
	for i, path := range paths {
		clean, err := cleanRepoPath(path)
		if err != nil {
			return ConflictIndex{}, err
		}
		entity, err := entityForPath(clean)
		if err != nil {
			return ConflictIndex{}, err
		}
		entity.Index = i
		entity.Path = clean
		local, hasLocal, err := g.stageFile(ctx, 2, clean)
		if err != nil {
			return ConflictIndex{}, err
		}
		remote, hasRemote, err := g.stageFile(ctx, 3, clean)
		if err != nil {
			return ConflictIndex{}, err
		}
		base, hasBase, err := g.stageFile(ctx, 1, clean)
		if err != nil {
			return ConflictIndex{}, err
		}
		entity.Local, entity.Remote, entity.Base = hasLocal, hasRemote, hasBase
		entity.DeletedLocal = !hasLocal
		entity.DeletedRemote = !hasRemote
		for which, data := range map[string][]byte{"local": local, "remote": remote, "base": base} {
			if data == nil {
				continue
			}
			if err := writeFileAtomic(filepath.Join(dir, entity.versionFile(which)), data, 0o600); err != nil {
				return ConflictIndex{}, err
			}
		}
		fillSummary(&entity, local, remote)
		index.Entities = append(index.Entities, entity)
	}
	data, err := encodeJSON(index)
	if err != nil {
		return ConflictIndex{}, err
	}
	if err := writeFileAtomic(filepath.Join(dir, "index.json"), data, 0o600); err != nil {
		return ConflictIndex{}, err
	}
	return index, nil
}

// LoadConflictIndex 读取一个冲突事务。
func LoadConflictIndex(syncDir, txid string) (ConflictIndex, error) {
	if txid == "" {
		return ConflictIndex{}, os.ErrNotExist
	}
	data, err := os.ReadFile(filepath.Join(ConflictDir(syncDir, txid), "index.json"))
	if err != nil {
		return ConflictIndex{}, err
	}
	var index ConflictIndex
	if err := decodeJSONStrict(data, &index); err != nil {
		return ConflictIndex{}, fmt.Errorf("%w: 解析冲突索引: %v", ErrCorruptRepo, err)
	}
	return index, nil
}

func entityForPath(path string) (ConflictEntity, error) {
	switch {
	case path == KeymapFile:
		return ConflictEntity{Entity: "keymap", Kind: KindKeymap, Title: "快捷键配置"}, nil
	case strings.HasPrefix(path, PlaylistsDir+"/") && strings.HasSuffix(path, ".json"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, PlaylistsDir+"/"), ".json")
		return ConflictEntity{Entity: "playlist:" + id, Kind: KindPlaylist, ID: id}, nil
	case strings.HasPrefix(path, AnnotationsDir+"/") && strings.HasSuffix(path, ".json"):
		stem := strings.TrimSuffix(strings.TrimPrefix(path, AnnotationsDir+"/"), ".json")
		return ConflictEntity{Entity: "annotation:" + stem, Kind: KindAnnotation, ID: stem}, nil
	}
	return ConflictEntity{}, fmt.Errorf("未知的冲突文件：%s", path)
}

func cleanRepoPath(path string) (string, error) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
		return "", fmt.Errorf("%w: 非法仓库路径 %q", ErrCorruptRepo, path)
	}
	return path, nil
}

func fillSummary(entity *ConflictEntity, local, remote []byte) {
	if entity.Kind == KindPlaylist {
		if file, err := DecodePlaylistFile(local); err == nil {
			entity.LocalTitle = file.Title
			entity.LocalCount = len(file.Items)
			entity.LocalRefs = len(file.Refs)
			entity.LocalUpdated = playlistUpdated(file)
		}
		if file, err := DecodePlaylistFile(remote); err == nil {
			entity.RemoteTitle = file.Title
			entity.RemoteCount = len(file.Items)
			entity.RemoteRefs = len(file.Refs)
			entity.RemoteUpdated = playlistUpdated(file)
		}
		switch {
		case entity.LocalTitle != "":
			entity.Title = entity.LocalTitle
		case entity.RemoteTitle != "":
			entity.Title = entity.RemoteTitle
		}
		return
	}
	if entity.Kind == KindAnnotation {
		entity.Title = entity.ID
		entity.LocalCount = 1
		return
	}
	entity.Title = "快捷键配置"
	entity.LocalCount = keymapEntryCount(local)
	entity.RemoteCount = keymapEntryCount(remote)
}

func playlistUpdated(file PlaylistFile) int64 {
	updated := file.CreatedAt
	for _, item := range file.Items {
		if item.AddedAt > updated {
			updated = item.AddedAt
		}
	}
	return updated
}

func keymapEntryCount(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	var doc struct {
		Keys map[string][]string `toml:"keys"`
	}
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return 0
	}
	return len(doc.Keys)
}

// ReadConflictVersion 读取 tx 目录里某个版本的原始内容。
func ReadConflictVersion(syncDir, txid string, entity ConflictEntity, which string) ([]byte, bool, error) {
	data, err := os.ReadFile(filepath.Join(ConflictDir(syncDir, txid), entity.versionFile(which)))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// DescribeEntity 返回冲突实体的中文名称，供 TUI 与 CLI 共用。
func DescribeEntity(entity ConflictEntity) string {
	switch entity.Kind {
	case KindPlaylist:
		if entity.Title != "" {
			return "歌单「" + entity.Title + "」"
		}
		return "歌单 " + entity.ID
	case KindAnnotation:
		return "注释 " + entity.ID
	default:
		return "快捷键配置"
	}
}

// SummarizeConflict 返回一行冲突摘要（曲目数、修改时间、删除标记）。
func SummarizeConflict(entity ConflictEntity) string {
	switch entity.Kind {
	case KindPlaylist:
		return fmt.Sprintf("本地 %s，远端 %s",
			playlistSideSummary(entity.Local, entity.LocalTitle, entity.LocalCount, entity.LocalRefs, entity.LocalUpdated),
			playlistSideSummary(entity.Remote, entity.RemoteTitle, entity.RemoteCount, entity.RemoteRefs, entity.RemoteUpdated))
	case KindAnnotation:
		return fmt.Sprintf("%s：本地 %s，远端 %s",
			DescribeEntity(entity), sideSummary(entity.Local, 0, 0, ""),
			sideSummary(entity.Remote, 0, 0, ""))
	default:
		return fmt.Sprintf("%s：本地 %s，远端 %s",
			DescribeEntity(entity), sideSummary(entity.Local, entity.LocalCount, 0, "项"),
			sideSummary(entity.Remote, entity.RemoteCount, 0, "项"))
	}
}

func playlistSideSummary(exists bool, title string, count, refs int, updated int64) string {
	if !exists {
		return "已删除"
	}
	parts := make([]string, 0, 4)
	if title != "" {
		parts = append(parts, "「"+title+"」")
	}
	parts = append(parts, fmt.Sprintf("%d 首", count))
	if updated > 0 {
		parts = append(parts, time.Unix(updated, 0).Format("01-02 15:04"))
	}
	if refs > 0 {
		parts = append(parts, fmt.Sprintf("引用 %d 个歌单", refs))
	}
	return strings.Join(parts, " · ")
}

func sideSummary(exists bool, count int, updated int64, unit string) string {
	if !exists {
		return "已删除"
	}
	parts := make([]string, 0, 2)
	if unit != "" {
		parts = append(parts, fmt.Sprintf("%d %s", count, unit))
	}
	if updated > 0 {
		parts = append(parts, time.Unix(updated, 0).Format("01-02 15:04"))
	}
	if len(parts) == 0 {
		return "存在"
	}
	return strings.Join(parts, " · ")
}
