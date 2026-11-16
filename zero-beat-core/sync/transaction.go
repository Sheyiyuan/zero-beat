package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// inputDigest survives process restarts, unlike the in-memory revision counter.
// Credentials are hashed as plaintext locally; no secret is written to state.
func (c *Client) inputDigest() (string, error) {
	snap, err := BuildSnapshot(SnapshotInput{Store: c.opts.Store, KeymapPath: c.opts.KeymapPath, Now: c.now()})
	if err != nil {
		return "", err
	}
	accounts, err := c.opts.Store.ListAccounts()
	if err != nil {
		return "", err
	}
	for _, a := range accounts {
		payload, at, err := c.opts.Store.LoadAccountAt(a.Source)
		if err != nil {
			return "", err
		}
		snap.Files["accounts/"+a.Source] = []byte(fmt.Sprintf("%d\n%s", at, payload))
	}
	// Include invalid/missing keymaps too: they must not invalidate a guard silently.
	if c.opts.KeymapPath != "" {
		raw, e := os.ReadFile(c.opts.KeymapPath)
		if e != nil && !os.IsNotExist(e) {
			return "", e
		}
		if e == nil {
			snap.Files[KeymapFile] = raw
		}
	}
	data, err := json.Marshal(snap.Files)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

type transaction struct {
	Version   int     `json:"version"`
	Phase     string  `json:"phase"`
	Before    string  `json:"before"`
	Base      string  `json:"base,omitempty"`
	Remote    string  `json:"remote,omitempty"`
	Candidate string  `json:"candidate"`
	Input     string  `json:"input"`
	Backend   Backend `json:"backend"`
}

func (c *Client) saveTransaction(tx transaction) error {
	tx.Version = 1
	data, err := encodeJSON(tx)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(c.opts.Dir, "transaction.json"), data, 0o600)
}

// A merge whose import was not completed is replayed from its local parent.
// The database is kept intact, so edits made during/after a crash are exported
// on the next round rather than overwritten by a stale candidate tree.
func (c *Client) recoverTransaction(ctx context.Context, g *Git) error {
	path := filepath.Join(c.opts.Dir, "transaction.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var tx transaction
	if err = decodeJSONStrict(data, &tx); err != nil {
		return err
	}
	if tx.Version != 1 {
		return fmt.Errorf("不支持的同步事务版本 %d", tx.Version)
	}
	switch tx.Phase {
	case "joining":
		if tx.Candidate == "" {
			return ErrCorruptRepo
		}
		key, err := LoadKeyState(c.opts.KeyPath)
		if err != nil {
			return err
		}
		if err := c.validateTree(ctx, g, tx.Candidate, key); err != nil {
			return err
		}
		head, exists, err := g.RevParse(ctx, "HEAD")
		if err != nil {
			return err
		}
		if exists && head != tx.Candidate {
			return ErrRevision
		}
		if err := g.ResetHardTo(ctx, tx.Candidate); err != nil {
			return err
		}
		if _, err := ImportSnapshot(ImportInput{Store: c.opts.Store, Dir: c.repoPath(), KeymapPath: c.opts.KeymapPath, ArchiveDir: c.opts.Dir, KeepLocal: true, Now: c.now()}); err != nil {
			return err
		}
		if key != nil {
			if _, _, err := c.importAccountsFromTree(key, key.KeyVersion); err != nil {
				return err
			}
		}

	case "merging", "prepared":
		head, err := g.Head(ctx)
		if err != nil {
			return err
		}
		if head != tx.Before {
			if tx.Candidate != "" && head != tx.Candidate {
				return ErrRevision
			}
			if tx.Candidate == "" {
				return ErrRevision
			}

			if err = g.UpdateHead(ctx, head, tx.Before); err != nil {
				return err
			}
		}
		if err = g.ResetHard(ctx); err != nil {
			return err
		}
	case "imported", "pushed":
	default:
		return fmt.Errorf("未知同步事务阶段 %q", tx.Phase)
	}
	return os.Remove(path)
}
func (c *Client) clearTransaction() error {
	return removeIfExists(filepath.Join(c.opts.Dir, "transaction.json"))
}
