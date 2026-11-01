package sync

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

// ConflictIndex 读取指定冲突事务的索引。
func (c *Client) ConflictIndex(txid string) (ConflictIndex, error) {
	return LoadConflictIndex(c.opts.Dir, txid)
}

// ArchiveDir 返回归档目录并确保它存在。
func (c *Client) ArchiveDir() string {
	dir := filepath.Join(c.opts.Dir, "archive")
	_ = ensureDir(dir, 0o700)
	return dir
}

// Conflicts 返回当前待处理的冲突事务；没有冲突时返回零值与 os.ErrNotExist。
func (c *Client) Conflicts() (ConflictIndex, error) {
	state, err := LoadState(c.statePath())
	if err != nil {
		return ConflictIndex{}, err
	}
	if state.ConflictTx == "" {
		return ConflictIndex{}, os.ErrNotExist
	}
	return LoadConflictIndex(c.opts.Dir, state.ConflictTx)
}

// SaveChoice 记录单个实体的选择（CLI 分次选择用）；写在 tx 目录里，不进仓库。
func (c *Client) SaveChoice(txid, entity string, choice Choice) error {
	if err := ValidateChoice(choice, true); err != nil {
		return err
	}
	index, err := LoadConflictIndex(c.opts.Dir, txid)
	if err != nil {
		return err
	}
	found := false
	for _, e := range index.Entities {
		if e.Entity == entity {
			found = true
			if choice == KeepBoth && !e.SupportsBoth() {
				return fmt.Errorf("这个实体不支持「保留两者」")
			}
		}
	}
	if !found {
		return fmt.Errorf("未知冲突实体 %q", entity)
	}
	choices, _ := loadChoices(c.opts.Dir, txid)
	choices[entity] = choice
	return saveChoices(c.opts.Dir, txid, choices)
}

// ValidateChoice 校验选择值。
func ValidateChoice(choice Choice, allowBoth bool) error {
	switch choice {
	case KeepLocal, KeepRemote:
		return nil
	case KeepBoth:
		if !allowBoth {
			return fmt.Errorf("该实体不支持「保留两者」")
		}
		return nil
	}
	return fmt.Errorf("未知选择 %q", choice)
}

// PendingChoices 返回已记录的选择。
func (c *Client) PendingChoices(txid string) (map[string]Choice, error) {
	return loadChoices(c.opts.Dir, txid)
}

func choicesPath(syncDir, txid string) string {
	return filepath.Join(ConflictDir(syncDir, txid), "choices.json")
}

func loadChoices(syncDir, txid string) (map[string]Choice, error) {
	data, err := os.ReadFile(choicesPath(syncDir, txid))
	if os.IsNotExist(err) {
		return map[string]Choice{}, nil
	}
	if err != nil {
		return nil, err
	}
	choices := map[string]Choice{}
	if err := decodeJSONStrict(data, &choices); err != nil {
		return nil, fmt.Errorf("%w: 解析冲突选择: %v", ErrCorruptRepo, err)
	}
	return choices, nil
}

func saveChoices(syncDir, txid string, choices map[string]Choice) error {
	data, err := encodeJSON(choices)
	if err != nil {
		return err
	}
	return writeFileAtomic(choicesPath(syncDir, txid), data, 0o600)
}

// ResolveEntity 记录一个实体的选择；全部选完后调用 ApplyResolution。
func (c *Client) ResolveEntity(txid, entity string, choice Choice) error {
	return c.SaveChoice(txid, entity, choice)
}

// ResolvePending 读取已保存的全部选择并应用；未全部选择时返回未完成数量。
func (c *Client) ResolvePending(ctx context.Context, txid string) (Result, error) {
	choices, err := loadChoices(c.opts.Dir, txid)
	if err != nil {
		return Result{}, err
	}
	return c.Resolve(ctx, txid, choices)
}

// Resolve 应用冲突选择：重新制造合并现场，按选择写回文件，提交合并并推送。
func (c *Client) Resolve(ctx context.Context, txid string, choices map[string]Choice) (Result, error) {
	if err := c.acquire(ctx); err != nil {
		return Result{}, err
	}
	defer c.release()

	index, err := LoadConflictIndex(c.opts.Dir, txid)
	if err != nil {
		return Result{}, err
	}
	for _, entity := range index.Entities {
		choice, ok := choices[entity.Entity]
		if !ok {
			return Result{}, fmt.Errorf("还有 %d 个实体未选择", countMissing(index, choices))
		}
		if err := ValidateChoice(choice, entity.SupportsBoth()); err != nil {
			return Result{}, err
		}
		if choice == KeepBoth && !entity.SupportsBoth() {
			return Result{}, fmt.Errorf("%s 不支持「保留两者」", DescribeEntity(entity))
		}
	}

	cfg, err := LoadConfig(c.opts.ConfigPath)
	if err != nil {
		return Result{}, err
	}
	if cfg.Remote == "" {
		return Result{}, ErrNotConfigured
	}
	key, err := LoadKeyState(c.opts.KeyPath)
	if err != nil {
		return Result{}, err
	}
	token, err := LoadToken(c.opts.TokenPath)
	if err != nil {
		return Result{}, err
	}
	g, err := c.prepareGit(ctx, cfg, token)
	if err != nil {
		return Result{}, err
	}
	if err := g.Recover(ctx); err != nil {
		return Result{}, err
	}
	if index.MergeHead == "" {
		return Result{}, fmt.Errorf("冲突索引缺少合并目标，请放弃本轮后重新同步")
	}
	conflicts, err := g.MergeNoCommit(ctx, index.MergeHead)
	if err != nil {
		return Result{}, err
	}
	if len(conflicts) == 0 {
		_ = g.AbortMerge(ctx)
		return Result{}, fmt.Errorf("冲突现场已失效，请放弃本轮后重新同步")
	}

	result := Result{}
	// 重新合并会再次产生 accounts/*.age 冲突；它们仍按 updated_at 自动 LWW。
	keyVersion := 0
	if key != nil {
		keyVersion = key.KeyVersion
	}
	remaining, err := c.resolveAccountConflicts(ctx, g, conflicts, key != nil, key, keyVersion, &result)
	if err != nil {
		_ = g.AbortMerge(ctx)
		return Result{}, err
	}
	conflictSet := make(map[string]bool, len(remaining))
	for _, path := range remaining {
		conflictSet[path] = true
	}
	for _, entity := range index.Entities {
		if !conflictSet[entity.Path] {
			_ = g.AbortMerge(ctx)
			return Result{}, fmt.Errorf("冲突现场已变化（缺少 %s），请放弃本轮后重新同步", entity.Path)
		}
	}

	taken := map[string]bool{}
	for _, entity := range index.Entities {
		choice := choices[entity.Entity]
		if err := c.applyChoice(ctx, g, &result, txid, entity, choice, taken); err != nil {
			_ = g.AbortMerge(ctx)
			return Result{}, err
		}
	}
	if remaining, err := g.UnmergedPaths(ctx); err != nil {
		return Result{}, err
	} else if len(remaining) > 0 {
		return Result{}, fmt.Errorf("仍有未合并的文件：%s", strings.Join(remaining, "、"))
	}
	if err := g.CommitMerge(ctx, c.commitMessage(c.deviceID())); err != nil {
		return Result{}, err
	}
	result.Committed = true

	imp, err := ImportSnapshot(ImportInput{
		Store:      c.opts.Store,
		Dir:        c.repoPath(),
		KeymapPath: c.opts.KeymapPath,
		ArchiveDir: c.opts.Dir,
		Now:        c.now(),
	})
	if err != nil {
		return result, err
	}
	result.Playlists = imp.Playlists
	result.Annotations = imp.Annotations
	result.Deleted = imp.Deleted
	result.KeymapUpdated = imp.KeymapUpdate
	result.Warnings = append(result.Warnings, imp.Warnings...)

	if key != nil {
		applied, warnings, err := c.importAccountsFromTree(key, key.KeyVersion)
		if err != nil {
			return result, err
		}
		result.AccountsApplied = applied
		result.Warnings = append(result.Warnings, warnings...)
	}

	// 选择已落地：先归档并清状态，推送失败留给下一轮普通同步处理。
	if _, err := c.archiveTx(txid); err != nil {
		result.Warnings = append(result.Warnings, "归档冲突失败："+err.Error())
	}
	state, err := LoadState(c.statePath())
	if err != nil {
		return result, err
	}
	state.ConflictTx = ""
	state.MergeHead = ""
	state.LastResult = "ok"
	state.LastError = ""
	state.KeyIssue = ""
	state.LastSyncAt = c.now().Unix()
	state.CredentialWarnings = append([]string(nil), result.Warnings...)
	_ = SaveState(c.statePath(), state)
	_ = os.Remove(choicesPath(c.opts.Dir, txid))

	if err := g.Push(ctx); err != nil {
		if state, loadErr := LoadState(c.statePath()); loadErr == nil {
			state.LastError = SanitizeRemote(err.Error())
			state.LastResult = "error"
			_ = SaveState(c.statePath(), state)
		}
		return result, err
	}
	result.Pushed = true
	return result, nil
}

func countMissing(index ConflictIndex, choices map[string]Choice) int {
	n := 0
	for _, entity := range index.Entities {
		if _, ok := choices[entity.Entity]; !ok {
			n++
		}
	}
	return n
}

func (c *Client) deviceID() string {
	state, err := LoadState(c.statePath())
	if err != nil || state.Device == "" {
		return "device"
	}
	return state.Device
}

// applyChoice 把一个实体的选择写入工作区；删除用移除文件表示，交给 git add -A 记录。
func (c *Client) applyChoice(ctx context.Context, g *Git, result *Result, txid string, entity ConflictEntity, choice Choice, taken map[string]bool) error {
	if err := ValidateChoice(choice, entity.SupportsBoth()); err != nil {
		return err
	}
	localData, localExists, err := c.localEntityData(entity)
	if err != nil {
		return err
	}
	remoteData, remoteExists, err := ReadConflictVersion(c.opts.Dir, txid, entity, "remote")
	if err != nil {
		return err
	}
	full := filepath.Join(c.repoPath(), filepath.FromSlash(entity.Path))

	switch choice {
	case KeepLocal:
		if !localExists {
			return c.removeAndStage(ctx, g, entity.Path)
		}
		if entity.Kind == KindKeymap && keymapInvalid(c.opts.KeymapPath) {
			return fmt.Errorf("本地 keymap.toml 无效，无法作为本地版本")
		}
		if err := writeFileAtomic(full, localData, 0o600); err != nil {
			return err
		}
		return g.AddPath(ctx, entity.Path)
	case KeepRemote:
		if !remoteExists {
			return c.removeAndStage(ctx, g, entity.Path)
		}
		if err := writeFileAtomic(full, remoteData, 0o600); err != nil {
			return err
		}
		return g.AddPath(ctx, entity.Path)
	case KeepBoth:
		if !localExists || !remoteExists {
			return fmt.Errorf("%s 缺少一侧，无法保留两者", DescribeEntity(entity))
		}
		if err := writeFileAtomic(full, localData, 0o600); err != nil {
			return err
		}
		copyPath, copyData, err := c.makeCopy(entity, remoteData, taken)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(filepath.Join(c.repoPath(), filepath.FromSlash(copyPath)), copyData, 0o600); err != nil {
			return err
		}
		if err := g.AddPath(ctx, entity.Path); err != nil {
			return err
		}
		if err := g.AddPath(ctx, copyPath); err != nil {
			return err
		}
		result.Warnings = append(result.Warnings, "已保留远端副本："+copyPath)
		return nil
	}
	return nil
}

// localEntityData 在应用选择时重新读取本地当前状态，
// 这样冲突产生后用户继续编辑的内容不会丢。
func (c *Client) localEntityData(entity ConflictEntity) ([]byte, bool, error) {
	switch entity.Kind {
	case KindPlaylist:
		snap, err := c.opts.Store.PlaylistSnapshot(entity.ID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		data, err := encodeJSON(playlistFileFromSnapshot(snap))
		if err != nil {
			return nil, false, err
		}
		return data, true, nil
	case KindAnnotation:
		list, err := c.opts.Store.ListAnnotations()
		if err != nil {
			return nil, false, err
		}
		songID, err := unescapeAnnotationID(entity.ID)
		if err != nil {
			return nil, false, err
		}
		for _, a := range list {
			if a.SongID == songID {
				data, err := encodeJSON(annotationFileFrom(a))
				if err != nil {
					return nil, false, err
				}
				return data, true, nil
			}
		}
		return nil, false, nil
	case KindKeymap:
		if c.opts.KeymapPath == "" {
			return nil, false, nil
		}
		data, err := os.ReadFile(c.opts.KeymapPath)
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return data, true, nil
	}
	return nil, false, fmt.Errorf("未知冲突实体")
}

func unescapeAnnotationID(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("注释缺少歌曲 ID")
	}
	// entity.ID 已经是 URL 转义后的文件名 stem；还原成歌曲 ID。
	out, err := url.PathUnescape(id)
	if err != nil || out == "" || len(out) > 300 {
		return "", fmt.Errorf("注释歌曲 ID 无效")
	}
	return out, nil
}

// makeCopy 为「保留两者」生成 <原 ID>-copy-<n>，标题加“（副本）”。
func (c *Client) makeCopy(entity ConflictEntity, remoteData []byte, taken map[string]bool) (string, []byte, error) {
	file, err := DecodePlaylistFile(remoteData)
	if err != nil {
		return "", nil, fmt.Errorf("远端歌单无法解析：%w", err)
	}
	prefix := "local-"
	if file.Kind == "mixin" {
		prefix = "mix-"
	}
	if !strings.HasPrefix(file.ID, prefix) {
		return "", nil, fmt.Errorf("远端歌单 ID 与类型不符")
	}
	_ = entity
	for n := 1; n <= 1000; n++ {
		id := fmt.Sprintf("%s-copy-%d", file.ID, n)
		if taken[id] || fileExists(filepath.Join(c.repoPath(), filepath.FromSlash(PlaylistPath(id)))) {
			continue
		}
		if c.opts.Store != nil {
			if _, err := c.opts.Store.Playlist(id); err == nil {
				continue
			} else if !errors.Is(err, store.ErrNotFound) {
				return "", nil, err
			}
		}
		file.ID = id
		file.Title = strings.TrimSpace(file.Title) + "（副本）"
		snap := snapshotFromFile(file)
		if err := store.ValidatePlaylistSnapshot(snap); err != nil {
			return "", nil, err
		}
		data, err := encodeJSON(file)
		if err != nil {
			return "", nil, err
		}
		taken[id] = true
		return PlaylistPath(id), data, nil
	}
	return "", nil, fmt.Errorf("无法生成副本 ID")
}

// removeAndStage 删除工作区文件并只暂存该路径。
func (c *Client) removeAndStage(ctx context.Context, g *Git, path string) error {
	if err := removeIfExists(filepath.Join(c.repoPath(), filepath.FromSlash(path))); err != nil {
		return err
	}
	return g.AddPath(ctx, path)
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// archiveTx 把冲突事务整体移入 archive/<时间>/，两份版本都保留。
func (c *Client) archiveTx(txid string) (string, error) {
	src := ConflictDir(c.opts.Dir, txid)
	if !fileExists(src) {
		return "", nil
	}
	dst := filepath.Join(archivePath(c.opts.Dir, c.now()), "conflicts-"+txid)
	if err := ensureDir(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// DiscardConflict 放弃本轮冲突：归档现场、清掉状态，下次同步重新拉取。
func (c *Client) DiscardConflict(ctx context.Context, txid string) error {
	if err := c.acquire(ctx); err != nil {
		return err
	}
	defer c.release()
	cfg, err := LoadConfig(c.opts.ConfigPath)
	if err != nil {
		return err
	}
	token, _ := LoadToken(c.opts.TokenPath)
	if g, err := c.prepareGit(ctx, cfg, token); err == nil {
		_ = g.AbortMerge(ctx)
		_ = g.Recover(ctx)
	}
	if _, err := c.archiveTx(txid); err != nil {
		return err
	}
	state, err := LoadState(c.statePath())
	if err != nil {
		return err
	}
	state.ConflictTx = ""
	state.MergeHead = ""
	state.LastError = ""
	_ = os.Remove(choicesPath(c.opts.Dir, txid))
	return SaveState(c.statePath(), state)
}
