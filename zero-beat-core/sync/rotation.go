package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// rotationJournal is local-only and protected like sync-key (0600). Keeping
// both derived keys makes an interrupted rotation recoverable without storing
// either passphrase. A failed fetch leaves this file intact for a later retry.
type rotationJournal struct {
	Version    int    `json:"version"`
	RemoteHash string `json:"remote_hash"`
	Before     string `json:"before"`
	OldKey     []byte `json:"old_key"`
	OldMeta    []byte `json:"old_meta"`
	NewKey     []byte `json:"new_key"`
	NewMeta    []byte `json:"new_meta"`
}

func (c *Client) rotationPath() string { return filepath.Join(c.opts.Dir, "rotation.json") }
func (c *Client) saveRotation(tx rotationJournal) error {
	tx.Version = 1
	raw, err := encodeJSON(tx)
	if err != nil {
		return err
	}
	return writeFileAtomic(c.rotationPath(), raw, 0o600)
}
func (c *Client) recoverRotation(ctx context.Context, g *Git, remote string) error {
	raw, err := os.ReadFile(c.rotationPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var tx rotationJournal
	if err := decodeJSONStrict(raw, &tx); err != nil {
		return err
	}
	if tx.Version != 1 || tx.RemoteHash != RemoteHash(remote) {
		return fmt.Errorf("换密钥事务版本或远端不匹配")
	}
	next, err := DecodeMeta(tx.NewMeta)
	if err != nil {
		return err
	}
	fetchErr := g.Fetch(ctx)
	if fetchErr != nil && !errors.Is(fetchErr, errEmptyRemote) {
		return fetchErr
	}
	published := false
	if fetchErr == nil {
		data, ok, err := g.showFile(ctx, "FETCH_HEAD", MetaFile)
		if err != nil {
			return err
		}
		if ok {
			meta, err := DecodeMeta(data)
			if err != nil {
				return err
			}
			published = meta.KeyVersion == next.KeyVersion && meta.Keycheck == next.Keycheck
		}
	}
	if published {
		if err := writeFileAtomic(c.opts.KeyPath, tx.NewKey, 0o600); err != nil {
			return err
		}
	} else {
		head, ok, err := g.RevParse(ctx, "HEAD")
		if err != nil {
			return err
		}
		if ok && head != tx.Before {
			if tx.Before == "" {
				return fmt.Errorf("首次换密钥中断，请保留本地事务并重新初始化")
			}
			if err := g.UpdateHead(ctx, head, tx.Before); err != nil {
				return err
			}
			if err := g.ResetHard(ctx); err != nil {
				return err
			}
		}
		for path, data := range map[string][]byte{c.opts.KeyPath: tx.OldKey, filepath.Join(c.repoPath(), MetaFile): tx.OldMeta} {
			if data == nil {
				if err := removeIfExists(path); err != nil {
					return err
				}
			} else if err := writeFileAtomic(path, data, 0o600); err != nil {
				return err
			}
		}
	}
	if err := c.clearTransaction(); err != nil {
		return err
	}
	return os.Remove(c.rotationPath())
}
