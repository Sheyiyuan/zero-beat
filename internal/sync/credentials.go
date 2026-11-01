package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

// accountSourceFromPath 从 accounts/<source>.age 提取音源；非法路径返回空字符串。
func accountSourceFromPath(path string) string {
	if !strings.HasPrefix(path, AccountsDir+"/") || !strings.HasSuffix(path, ".age") {
		return ""
	}
	source := strings.TrimSuffix(strings.TrimPrefix(path, AccountsDir+"/"), ".age")
	if source == "" || strings.ContainsAny(source, "/\\\x00") || len(source) > 64 {
		return ""
	}
	return source
}

func credentialPayload(cred Credential) ([]byte, error) {
	return json.Marshal(cred.Session)
}

// lwwRemoteAccounts 读取远端（FETCH_HEAD）的密文，按 updated_at 决定是否回填本地库。
// 本地较新时什么都不做；时间戳相同但内容不同时保留本地并给出提示。
func (c *Client) lwwRemoteAccounts(ctx context.Context, g *Git, key *KeyState, keyVersion int) ([]string, []string, error) {
	if key == nil {
		return nil, nil, nil
	}
	files, err := g.LsTree(ctx, "FETCH_HEAD")
	if err != nil {
		return nil, nil, err
	}
	var applied, warnings []string
	for _, path := range files {
		source := accountSourceFromPath(path)
		if source == "" {
			continue
		}
		data, ok, err := g.showFile(ctx, "FETCH_HEAD", path)
		if err != nil {
			return applied, warnings, err
		}
		if !ok {
			continue
		}
		cred, err := DecryptCredential(key.Key, source, keyVersion, data)
		if err != nil {
			if version, ok := EnvelopeKeyVersion(data); ok && version != keyVersion {
				// 别的设备轮换口令后留下的旧密文：跳过它，不算本机密钥错误。
				warnings = append(warnings, "远端登录态仍是旧密钥版本，已跳过："+source)
				continue
			}
			if errors.Is(err, ErrKeyMismatch) {
				return applied, warnings, err
			}
			warnings = append(warnings, "远端登录态无法解密："+source)
			continue
		}
		updated, appliedNow, warn, err := c.mergeRemoteCredential(cred)
		if err != nil {
			return applied, warnings, err
		}
		if appliedNow {
			applied = append(applied, source)
		}
		_ = updated
		if warn != "" {
			warnings = append(warnings, warn)
		}
	}
	return applied, warnings, nil
}

// mergeRemoteCredential 把一条远端凭据并入本地库，返回是否写入。
func (c *Client) mergeRemoteCredential(cred Credential) (int64, bool, string, error) {
	if c.opts.Store == nil {
		return 0, false, "", nil
	}
	payload, err := credentialPayload(cred)
	if err != nil {
		return 0, false, "", err
	}
	localUpdated, err := c.opts.Store.AccountUpdatedAt(cred.Source)
	if errors.Is(err, store.ErrNotFound) {
		if err := c.opts.Store.SaveAccountAt(cred.Source, payload, cred.UpdatedAt); err != nil {
			return 0, false, "", err
		}
		return cred.UpdatedAt, true, "", nil
	}
	if err != nil {
		return 0, false, "", err
	}
	switch {
	case cred.UpdatedAt > localUpdated:
		if err := c.opts.Store.SaveAccountAt(cred.Source, payload, cred.UpdatedAt); err != nil {
			return 0, false, "", err
		}
		return cred.UpdatedAt, true, "", nil
	case cred.UpdatedAt == localUpdated:
		localPayload, _, err := c.opts.Store.LoadAccountAt(cred.Source)
		if err == nil && !bytes.Equal(localPayload, payload) {
			return localUpdated, false, "登录态时间戳相同但内容不同（" + cred.Source + "），已保留本地", nil
		}
	}
	return localUpdated, false, "", nil
}

// importAccountsFromTree 在合并完成后导入最终工作区里的登录态。
func (c *Client) importAccountsFromTree(key *KeyState, keyVersion int) ([]string, []string, error) {
	if key == nil || c.opts.Store == nil {
		return nil, nil, nil
	}
	dir := filepath.Join(c.repoPath(), AccountsDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var applied, warnings []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".age") {
			continue
		}
		source := strings.TrimSuffix(entry.Name(), ".age")
		if source == "" || strings.ContainsAny(source, "/\\\x00") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil || info.Size() > maxRepoFileSize {
			warnings = append(warnings, "远端登录态过大或不可读："+source)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, "读取远端登录态失败："+source)
			continue
		}
		cred, err := DecryptCredential(key.Key, source, keyVersion, data)
		if err != nil {
			if errors.Is(err, ErrKeyMismatch) || errors.Is(err, ErrCredentialReject) {
				// 旧密钥留下的密文或未来版本的密文：保留文件，不覆盖本地。
				warnings = append(warnings, "远端登录态不可解密："+source)
				continue
			}
			warnings = append(warnings, "远端登录态无效："+source)
			continue
		}
		_, appliedNow, warn, err := c.mergeRemoteCredential(cred)
		if err != nil {
			return applied, warnings, err
		}
		if appliedNow {
			applied = append(applied, source)
		}
		if warn != "" {
			warnings = append(warnings, warn)
		}
	}
	return applied, warnings, nil
}

// resolveAccountConflicts 自动处理 accounts/*.age 的合并冲突：按密文内的 updated_at
// 做后写者胜，不进入冲突选择界面。返回仍需用户处理的非凭据冲突路径。
func (c *Client) resolveAccountConflicts(ctx context.Context, g *Git, paths []string, keyAvailable bool, key *KeyState, keyVersion int, result *Result) ([]string, error) {
	var remaining []string
	for _, path := range paths {
		source := accountSourceFromPath(path)
		if source == "" {
			remaining = append(remaining, path)
			continue
		}
		ours, hasOurs, err := g.stageFile(ctx, 2, path)
		if err != nil {
			return nil, err
		}
		theirs, hasTheirs, err := g.stageFile(ctx, 3, path)
		if err != nil {
			return nil, err
		}
		winner := ours
		switch {
		case !hasOurs && hasTheirs:
			winner = theirs
		case hasOurs && !hasTheirs:
			winner = ours
		case hasOurs && hasTheirs && keyAvailable && key != nil:
			oursCred, oursErr := DecryptCredential(key.Key, source, keyVersion, ours)
			theirsCred, theirsErr := DecryptCredential(key.Key, source, keyVersion, theirs)
			switch {
			case oursErr == nil && theirsErr == nil:
				switch {
				case theirsCred.UpdatedAt > oursCred.UpdatedAt:
					winner = theirs
				case theirsCred.UpdatedAt == oursCred.UpdatedAt && !bytes.Equal(ours, theirs):
					result.Warnings = append(result.Warnings,
						"登录态时间戳相同但内容不同（"+source+"），已保留本地")
					winner = ours
				default:
					winner = ours
				}
			case oursErr != nil && theirsErr == nil:
				winner = theirs
			case theirsErr != nil && oursErr == nil:
				winner = ours
			default:
				winner = ours
				result.Warnings = append(result.Warnings, "登录态密文无法解密（"+source+"），已保留本地")
			}
		case hasOurs && hasTheirs:
			// 没有密钥时无法比较时间戳，保留远端，避免覆盖其他设备。
			winner = theirs
		}
		full := filepath.Join(c.repoPath(), filepath.FromSlash(path))
		if winner == nil {
			if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		} else if err := writeFileAtomic(full, winner, 0o600); err != nil {
			return nil, err
		}
		if err := g.AddPath(ctx, path); err != nil {
			return nil, err
		}
	}
	return remaining, nil
}
