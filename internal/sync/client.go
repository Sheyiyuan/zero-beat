package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/store"
)

// ErrRevision 表示导入期间本地库又被修改；本轮放弃导入，下一轮重试。
var ErrRevision = errors.New("本地库在同步期间被修改")

// Options 是同步客户端的全部外部依赖；测试用临时目录替换默认路径。
type Options struct {
	Store      *store.Store
	Dir        string // ~/.config/zero-beat/sync
	ConfigPath string // sync.toml
	KeyPath    string // sync-key
	TokenPath  string // sync-token
	KeymapPath string // 本地 keymap.toml
	Revision   func() uint64
	Now        func() time.Time
	GitTimeout time.Duration
}

// Client 是同步操作的入口；同一时刻只允许一个操作。
type Client struct {
	opts Options
	mu   chan struct{}
}

// NewClient 构造客户端；Dir 为空时使用系统临时目录下的 zero-beat-sync。
func NewClient(opts Options) *Client {
	if opts.Dir == "" {
		dir, err := os.MkdirTemp("", "zero-beat-sync")
		if err == nil {
			opts.Dir = dir
		}
	}
	c := &Client{opts: opts, mu: make(chan struct{}, 1)}
	c.mu <- struct{}{}
	return c
}

func (c *Client) now() time.Time {
	if c.opts.Now != nil {
		return c.opts.Now()
	}
	return time.Now()
}

func (c *Client) revision() uint64 {
	if c.opts.Revision == nil {
		return 0
	}
	return c.opts.Revision()
}

func (c *Client) statePath() string { return filepath.Join(c.opts.Dir, "state.json") }
func (c *Client) repoPath() string  { return filepath.Join(c.opts.Dir, "repo") }

// Result 描述一次成功同步。
type Result struct {
	Committed       bool
	Pushed          bool
	Playlists       int
	Annotations     int
	Accounts        int
	Deleted         []string
	KeymapUpdated   bool
	Warnings        []string
	AccountsApplied []string
	KeyIssue        error
	// MetaUpgraded 表示本轮把仓库格式从 v1 升到了 v2；旧客户端从此刻起无法同步。
	MetaUpgraded bool
}

// Status 返回当前配置与上次同步结果。
func (c *Client) Status() Status {
	cfg, _ := LoadConfig(c.opts.ConfigPath)
	st, _ := LoadState(c.statePath())
	key, _ := LoadKeyState(c.opts.KeyPath)
	token, _ := LoadToken(c.opts.TokenPath)
	status := Status{
		Configured: cfg.Remote != "",
		Remote:     SanitizeRemote(cfg.Remote),
		Branch:     cfg.Branch,
		Auth:       AuthLabel(cfg.Remote, token != ""),
		Auto:       cfg.Auto,
		KeyCached:  key != nil,
		LastResult: st.LastResult,
		LastError:  st.LastError,
		KeyIssue:   st.KeyIssue,
		ConflictTx: st.ConflictTx,
		Warnings:   append([]string(nil), st.CredentialWarnings...),
	}
	if key != nil {
		status.KeyVersion = key.KeyVersion
		status.KeyMatchesRepo = key.RemoteHash == "" || key.RemoteHash == RemoteHash(cfg.Remote)
	}
	if st.LastSyncAt > 0 {
		status.LastSyncAt = time.Unix(st.LastSyncAt, 0)
	}
	if st.ConflictTx != "" {
		if index, err := LoadConflictIndex(c.opts.Dir, st.ConflictTx); err == nil {
			status.Conflicts = len(index.Entities)
		}
	}
	return status
}

// Probe 检查远端是否已有 meta.json；用于初始化前决定「新建」还是「加入」。
func (c *Client) Probe(ctx context.Context) (bool, error) {
	if err := c.acquire(ctx); err != nil {
		return false, err
	}
	defer c.release()
	cfg, err := LoadConfig(c.opts.ConfigPath)
	if err != nil {
		return false, err
	}
	if cfg.Remote == "" {
		return false, ErrNotConfigured
	}
	token, err := LoadToken(c.opts.TokenPath)
	if err != nil {
		return false, err
	}
	g, err := c.prepareGit(ctx, cfg, token)
	if err != nil {
		return false, err
	}
	if err := g.Fetch(ctx); err != nil {
		if errors.Is(err, errEmptyRemote) {
			return false, nil
		}
		return false, err
	}
	if _, ok, err := g.showFile(ctx, "FETCH_HEAD", MetaFile); err != nil {
		return false, err
	} else if ok {
		return true, nil
	}
	files, err := g.LsTree(ctx, "FETCH_HEAD")
	if err != nil {
		return false, err
	}
	if len(files) > 0 {
		return false, fmt.Errorf("%w: 远端仓库不是 zero-beat 同步仓库（缺少 meta.json）", ErrCorruptRepo)
	}
	return false, nil
}

// Configure 保存仓库地址、分支与自动同步开关；remote 为空时拒绝。
func (c *Client) Configure(remote, branch string, auto bool) error {
	remote = strings.TrimSpace(remote)
	if err := ValidateRemote(remote); err != nil {
		return err
	}
	if branch == "" {
		branch = "main"
	}
	if err := ValidateBranch(branch); err != nil {
		return err
	}
	return SaveConfig(c.opts.ConfigPath, Config{Remote: remote, Branch: branch, Auto: auto})
}

// SetAuto 修改自动同步开关。
func (c *Client) SetAuto(auto bool) error {
	cfg, err := LoadConfig(c.opts.ConfigPath)
	if err != nil {
		return err
	}
	if cfg.Remote == "" {
		return ErrNotConfigured
	}
	cfg.Auto = auto
	return SaveConfig(c.opts.ConfigPath, cfg)
}

var branchRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// ValidateBranch 拒绝会破坏 refspec 的分支名。
func ValidateBranch(branch string) error {
	if branch == "" || len(branch) > 200 || !branchRe.MatchString(branch) ||
		strings.HasPrefix(branch, "-") || strings.Contains(branch, "..") ||
		strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".lock") {
		return fmt.Errorf("分支名无效：%q", branch)
	}
	return nil
}

// ValidateRemote 做最低限度的安全检查：拒绝换行、首字符 - 与超长 URL；
// HTTPS URL 里不允许带用户名/口令，token 必须交给 credential helper 或 0600 文件。
func ValidateRemote(remote string) error {
	if remote == "" {
		return fmt.Errorf("仓库地址不能为空")
	}
	if len(remote) > 2048 {
		return fmt.Errorf("仓库地址过长")
	}
	if strings.ContainsAny(remote, "\n\r\x00") || strings.HasPrefix(remote, "-") {
		return fmt.Errorf("仓库地址无效")
	}
	if strings.HasPrefix(remote, "http://") || strings.HasPrefix(remote, "https://") {
		u, err := url.Parse(remote)
		if err != nil || u.Host == "" {
			return fmt.Errorf("仓库地址无效")
		}
		if u.User != nil {
			return fmt.Errorf("不要把 token 写进仓库地址，请用 credential helper 或同步页的令牌")
		}
	}
	return nil
}

// remoteCredentialsRe 只匹配 http(s) URL 里的 userinfo；SSH 的 git@host 不是秘密，
// 也不能被抹掉，否则界面上的 ssh:// 地址会无法编辑/切换。
var remoteCredentialsRe = regexp.MustCompile(`(?i)(https?://)[^/@\s]+@`)

// SanitizeRemote 去掉 HTTPS URL 里的用户名/口令，避免写进日志或状态。
func SanitizeRemote(remote string) string {
	return remoteCredentialsRe.ReplaceAllString(remote, "${1}***@")
}

// InitOutcome 描述初始化结果。
type InitOutcome struct {
	Joined    bool // true 表示加入了已有仓库（远端已有 meta.json）
	ReusedKey bool // true 表示空远端复用了本机已有密钥，口令未更改
	Synced    Result
}

// Init 初始化或加入仓库：空仓库生成盐与密钥；已有 meta.json 时校验口令并导入。
func (c *Client) Init(ctx context.Context, remote, branch string, auto bool, passphrase string) (InitOutcome, error) {
	if err := c.Configure(remote, branch, auto); err != nil {
		return InitOutcome{}, err
	}
	if err := c.acquire(ctx); err != nil {
		return InitOutcome{}, err
	}
	defer c.release()

	cfg, err := LoadConfig(c.opts.ConfigPath)
	if err != nil {
		return InitOutcome{}, err
	}
	token, err := LoadToken(c.opts.TokenPath)
	if err != nil {
		return InitOutcome{}, err
	}
	g, err := c.prepareGit(ctx, cfg, token)
	if err != nil {
		return InitOutcome{}, err
	}
	if err := g.EnsureRepo(ctx); err != nil {
		return InitOutcome{}, err
	}
	if err := g.Recover(ctx); err != nil {
		return InitOutcome{}, err
	}
	fetchErr := g.Fetch(ctx)
	empty := errors.Is(fetchErr, errEmptyRemote)
	if fetchErr != nil && !empty {
		return InitOutcome{}, fetchErr
	}

	metaData, hasMeta := []byte(nil), false
	if !empty {
		if data, ok, err := g.showFile(ctx, "FETCH_HEAD", MetaFile); err != nil {
			return InitOutcome{}, err
		} else if ok {
			metaData, hasMeta = data, true
		}
	}

	if hasMeta {
		meta, err := DecodeMeta(metaData)
		if err != nil {
			return InitOutcome{}, err
		}
		key, err := meta.Unlock(passphrase)
		if err != nil {
			return InitOutcome{}, err
		}
		key.RemoteHash = RemoteHash(cfg.Remote)
		if err := key.Save(c.opts.KeyPath); err != nil {
			return InitOutcome{}, err
		}
		result, err := c.syncWithLock(ctx, cfg, key, g, syncRunOptions{})
		if err != nil {
			return InitOutcome{Joined: true}, err
		}
		return InitOutcome{Joined: true, Synced: result}, nil
	}

	if !empty {
		files, err := g.LsTree(ctx, "FETCH_HEAD")
		if err != nil {
			return InitOutcome{}, err
		}
		if len(files) > 0 {
			return InitOutcome{}, fmt.Errorf("%w: 远端仓库不是 zero-beat 同步仓库（缺少 meta.json）", ErrCorruptRepo)
		}
	}

	// 空仓库：如果本机已有同一远端的密钥，复用它，避免无谓地让其他设备失效。
	key, _ := LoadKeyState(c.opts.KeyPath)
	reused := false
	meta := Meta{}
	if key != nil && key.RemoteHash == RemoteHash(cfg.Remote) {
		reused = true
		meta = Meta{
			V:          MetaVersion,
			KeyVersion: key.KeyVersion,
			KDF:        key.KDF,
			Salt:       encodeBase64(key.Salt),
			Keycheck:   encodeBase64(Keycheck(key.Key)),
			CreatedAt:  c.now().Unix(),
		}
	} else {
		var err error
		meta, key, err = NewMeta(passphrase, c.now())
		if err != nil {
			return InitOutcome{}, err
		}
		key.RemoteHash = RemoteHash(cfg.Remote)
		if err := key.Save(c.opts.KeyPath); err != nil {
			return InitOutcome{}, err
		}
	}
	data, err := meta.Encode()
	if err != nil {
		return InitOutcome{}, err
	}
	if err := writeFileAtomic(filepath.Join(c.repoPath(), MetaFile), data, 0o600); err != nil {
		return InitOutcome{}, err
	}
	result, err := c.syncWithLock(ctx, cfg, key, g, syncRunOptions{})
	if err != nil {
		return InitOutcome{}, err
	}
	return InitOutcome{Synced: result, ReusedKey: reused}, nil
}

// acquire 串行化同一进程内的同步操作。
func (c *Client) acquire(ctx context.Context) error {
	select {
	case <-c.mu:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) release() { c.mu <- struct{}{} }

// Sync 执行一轮完整同步：取锁、拉取、合并、导入、推送与状态记录。
func (c *Client) Sync(ctx context.Context) (Result, error) {
	if err := c.acquire(ctx); err != nil {
		return Result{}, err
	}
	defer c.release()
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
	return c.syncWithLock(ctx, cfg, key, g, syncRunOptions{})
}

// prepareGit 创建受管克隆并配置认证。
func (c *Client) prepareGit(ctx context.Context, cfg Config, token string) (*Git, error) {
	g := NewGit(c.repoPath(), cfg.Remote, cfg.Branch)
	if c.opts.GitTimeout > 0 {
		g.Timeout = c.opts.GitTimeout
	}
	if token != "" {
		askpass := filepath.Join(c.opts.Dir, "askpass.sh")
		if _, err := WriteAskPass(askpass); err != nil {
			return nil, err
		}
		g.AskPass = askpass
		g.TokenFile = c.opts.TokenPath
	}
	if err := g.EnsureRepo(ctx); err != nil {
		return nil, err
	}
	return g, nil
}

// syncRunOptions 控制一轮同步的内部行为。
type syncRunOptions struct {
	// skipRecover 用于换口令：工作区里有刚写入的新 meta.json，不能被 Recover 清掉。
	skipRecover bool
	// publishMeta 表示本轮会提交工作区里的 meta.json；密钥按它校验。
	publishMeta bool
}

// syncWithLock 在客户端互斥之外再拿进程间文件锁，并记录错误状态。
func (c *Client) syncWithLock(ctx context.Context, cfg Config, key *KeyState, g *Git, opts syncRunOptions) (Result, error) {
	lock, err := AcquireLock(filepath.Join(c.opts.Dir, "lock"))
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()

	state, err := LoadState(c.statePath())
	if err != nil {
		return Result{}, err
	}
	if !fileExists(c.statePath()) {
		_ = SaveState(c.statePath(), state)
	}
	if state.ConflictTx != "" {
		if _, err := LoadConflictIndex(c.opts.Dir, state.ConflictTx); err != nil {
			if os.IsNotExist(err) {
				// 冲突目录被手工删掉：清掉过期状态，继续正常同步。
				state.ConflictTx = ""
				state.MergeHead = ""
				_ = SaveState(c.statePath(), state)
			} else {
				return Result{}, ErrConflict
			}
		} else {
			return Result{}, ErrConflict
		}
	}
	result, err := c.syncLocked(ctx, cfg, key, g, opts)
	if err != nil {
		// syncLocked 可能已经写入冲突事务/密钥状态，重新读取后再记录错误。
		if reloaded, loadErr := LoadState(c.statePath()); loadErr == nil {
			state = reloaded
		}
		c.recordError(&state, err, cfg)
		return result, err
	}
	if reloaded, loadErr := LoadState(c.statePath()); loadErr == nil {
		state = reloaded
	}
	state.LastSyncAt = c.now().Unix()
	state.LastResult = "ok"
	state.LastError = ""
	state.MergeHead = ""
	state.KeyIssue = ""
	if result.KeyIssue != nil {
		state.KeyIssue = result.KeyIssue.Error()
	}
	state.CredentialWarnings = append([]string(nil), result.Warnings...)
	_ = SaveState(c.statePath(), state)
	return result, nil
}

func (c *Client) recordError(state *State, err error, cfg Config) {
	state.LastError = SanitizeRemote(err.Error())
	state.LastResult = "error"
	state.CredentialWarnings = nil
	_ = SaveState(c.statePath(), *state)
}

// syncLocked 是同步主体；调用方必须已持有进程锁。
func (c *Client) syncLocked(ctx context.Context, cfg Config, key *KeyState, g *Git, opts syncRunOptions) (Result, error) {
	result := Result{}
	state, err := LoadState(c.statePath())
	if err != nil {
		return result, err
	}
	if !opts.skipRecover {
		if err := g.Recover(ctx); err != nil {
			return result, err
		}
	}
	// 推送被拒后重试整轮，绝不做 force push。
	for attempt := 0; attempt < 3; attempt++ {
		err := c.round(ctx, g, cfg, &state, key, &result, opts)
		if err == nil {
			return result, nil
		}
		if errors.Is(err, ErrPushRejected) && attempt < 2 {
			continue
		}
		return result, err
	}
	return result, ErrPushRejected
}

func (c *Client) round(ctx context.Context, g *Git, cfg Config, state *State, key *KeyState, result *Result, opts syncRunOptions) error {
	fetchErr := g.Fetch(ctx)
	empty := errors.Is(fetchErr, errEmptyRemote)
	if fetchErr != nil && !empty {
		return fetchErr
	}
	hasRemote := false
	if !empty {
		if _, ok, err := g.RevParse(ctx, "FETCH_HEAD"); err != nil {
			return err
		} else if ok {
			hasRemote = true
		}
	}

	var remoteMeta Meta
	hasMeta := false
	if hasRemote {
		data, ok, err := g.showFile(ctx, "FETCH_HEAD", MetaFile)
		if err != nil {
			return err
		}
		if ok {
			remoteMeta, err = DecodeMeta(data)
			if err != nil {
				return err
			}
			hasMeta = true
		} else {
			files, err := g.LsTree(ctx, "FETCH_HEAD")
			if err != nil {
				return err
			}
			if len(files) > 0 {
				return fmt.Errorf("%w: 远端仓库缺少 meta.json", ErrCorruptRepo)
			}
			hasRemote = false
		}
	}

	keyAvailable, keyVersion, keyIssue := c.checkKey(cfg, remoteMeta, hasMeta, key, hasRemote, opts.publishMeta)
	result.KeyIssue = keyIssue
	if keyIssue != nil {
		result.Warnings = append(result.Warnings, keyIssue.Error())
	}
	if !hasRemote && !keyAvailable && !opts.publishMeta {
		return fmt.Errorf("%w: 远端为空且本机没有可用密钥，请先完成初始化", ErrNotConfigured)
	}

	// 新设备首次同步：先把远端快照导入本地库，避免空库导出把远端歌单删光。
	if hasRemote && !g.HasHead(ctx) {
		if err := g.ResetHardTo(ctx, "FETCH_HEAD"); err != nil {
			return err
		}
		imp, err := ImportSnapshot(ImportInput{
			Store:      c.opts.Store,
			Dir:        c.repoPath(),
			KeymapPath: c.opts.KeymapPath,
			ArchiveDir: c.opts.Dir,
			KeepLocal:  true,
			Now:        c.now(),
		})
		if err != nil {
			return err
		}
		result.Warnings = append(result.Warnings, imp.Warnings...)
		if keyAvailable {
			applied, warns, err := c.importAccountsFromTree(key, keyVersion)
			if err != nil {
				return err
			}
			result.AccountsApplied = append(result.AccountsApplied, applied...)
			result.Warnings = append(result.Warnings, warns...)
		}
	}

	// 凭据 LWW：远端较新则先回填本地库，导出时就会带上它。
	// 换口令时不读远端旧密文：它们本来就用旧密钥加密，等本地重新加密覆盖。
	if keyAvailable && hasRemote && hasMeta && !opts.publishMeta {
		applied, warnings, err := c.lwwRemoteAccounts(ctx, g, key, keyVersion)
		if errors.Is(err, ErrKeyMismatch) {
			keyAvailable = false
			result.KeyIssue = err
			result.Warnings = append(result.Warnings, "远端登录态无法解密，已跳过凭据同步")
		} else if err != nil {
			return err
		} else {
			result.AccountsApplied = append(result.AccountsApplied, applied...)
			result.Warnings = append(result.Warnings, warnings...)
			// 远端较新的密文直接取到工作区：导出时复用同一份文件，避免无意义 diff。
			for _, source := range applied {
				if err := g.CheckoutFrom(ctx, "FETCH_HEAD", AccountPath(source)); err != nil {
					result.Warnings = append(result.Warnings, "更新本地登录态文件失败："+source)
				}
			}
		}
	}

	revBefore := c.revision()
	snap, err := BuildSnapshot(SnapshotInput{
		Store:       c.opts.Store,
		KeymapPath:  c.opts.KeymapPath,
		Key:         keyBytes(keyAvailable, key),
		KeyVersion:  keyVersion,
		Device:      state.Device,
		ExistingDir: c.repoPath(),
		Now:         c.now(),
	})
	if err != nil {
		return err
	}
	result.Warnings = append(result.Warnings, snap.Warnings...)
	result.Accounts = snap.Accounts
	if err := WriteSnapshot(c.repoPath(), snap); err != nil {
		return err
	}
	// 换口令后，无法用新密钥重写的旧密文从当前树里移除；它们仍在 git 历史里。
	if opts.publishMeta {
		pruneAccountFiles(c.repoPath(), snap.Files)
	}
	// 远端被清空但本机仍有密钥时，用同一把密钥重建 meta.json。
	if !hasRemote && keyAvailable {
		if err := c.ensureMetaFile(key, keyVersion); err != nil {
			return err
		}
	}
	// 防御：远端有 meta 而本地区缺失时先取回来，避免合并把远端 meta 当成「本地删除」。
	if hasMeta && !fileExists(filepath.Join(c.repoPath(), MetaFile)) {
		if err := g.CheckoutFrom(ctx, "FETCH_HEAD", MetaFile); err != nil {
			return err
		}
	}
	// v1 仓库在成功同步的这一轮原地升到 v2：提交后旧客户端读到的是版本不匹配，
	// 而不是带着不认识的字段继续导出。升级不可回退。
	upgraded, err := c.upgradeMeta()
	if err != nil {
		return err
	}
	if upgraded {
		result.MetaUpgraded = true
		result.Warnings = append(result.Warnings,
			"仓库已升级到 v2，旧版本客户端将无法同步；请先升级所有设备")
	}
	committed, err := g.CommitAll(ctx, c.commitMessage(state.Device))
	if err != nil {
		return err
	}
	result.Committed = committed
	preMergeHead, _ := g.Head(ctx)

	if hasRemote {
		conflicts, err := g.Merge(ctx, "FETCH_HEAD")
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			for _, path := range conflicts {
				if path == MetaFile {
					_ = g.AbortMerge(ctx)
					return fmt.Errorf("%w: meta.json 冲突，需要人工处理新密钥", ErrKeyMismatch)
				}
			}
			remaining, err := c.resolveAccountConflicts(ctx, g, conflicts, keyAvailable, key, keyVersion, result)
			if err != nil {
				return err
			}
			if len(remaining) > 0 {
				mergeHead := g.MergeHead(ctx)
				if mergeHead == "" {
					if rev, ok, err := g.RevParse(ctx, "FETCH_HEAD"); err == nil && ok {
						mergeHead = rev
					}
				}
				index, err := collectConflicts(ctx, g, c.opts.Dir, remaining, mergeHead, c.now())
				if err != nil {
					return err
				}
				_ = g.AbortMerge(ctx)
				state.ConflictTx = index.TxID
				state.MergeHead = index.MergeHead
				_ = SaveState(c.statePath(), *state)
				return ErrConflict
			}
			if err := g.CommitMerge(ctx, c.commitMessage(state.Device)); err != nil {
				return err
			}
			result.Committed = true
		}
	}

	// 合并后、导入前再检查一次：本轮期间用户改过库就丢弃这次合并，下一轮重做，
	// 避免把用户刚改的内容用旧快照覆盖掉。
	if c.revision() != revBefore {
		if preMergeHead != "" {
			_ = g.ResetHardTo(ctx, preMergeHead)
		}
		return ErrRevision
	}

	imp, err := ImportSnapshot(ImportInput{
		Store:      c.opts.Store,
		Dir:        c.repoPath(),
		KeymapPath: c.opts.KeymapPath,
		ArchiveDir: c.opts.Dir,
		Now:        c.now(),
	})
	if err != nil {
		return err
	}
	result.Playlists = imp.Playlists
	result.Annotations = imp.Annotations
	result.Deleted = append(result.Deleted, imp.Deleted...)
	result.KeymapUpdated = result.KeymapUpdated || imp.KeymapUpdate
	result.Warnings = append(result.Warnings, imp.Warnings...)

	if keyAvailable {
		applied, warnings, err := c.importAccountsFromTree(key, keyVersion)
		if err != nil {
			return err
		}
		result.AccountsApplied = appendUnique(result.AccountsApplied, applied...)
		result.Warnings = append(result.Warnings, warnings...)
	}

	if err := g.Push(ctx); err != nil {
		return err
	}
	result.Pushed = true

	state.LastSyncAt = c.now().Unix()
	state.LastResult = "ok"
	state.LastError = ""
	state.MergeHead = ""
	state.KeyIssue = ""
	if result.KeyIssue != nil {
		state.KeyIssue = result.KeyIssue.Error()
	}
	state.CredentialWarnings = append([]string(nil), result.Warnings...)
	if err := SaveState(c.statePath(), *state); err != nil {
		return err
	}
	return nil
}

// checkKey 判断本地密钥能否用于当前远端；不能时只跳过凭据，不影响歌单。
func (c *Client) checkKey(cfg Config, meta Meta, hasMeta bool, key *KeyState, hasRemote bool, publishMeta bool) (bool, int, error) {
	if key == nil {
		if hasMeta {
			return false, 0, ErrKeyMissing
		}
		return false, 0, nil
	}
	if key.RemoteHash != "" && key.RemoteHash != RemoteHash(cfg.Remote) {
		return false, key.KeyVersion, fmt.Errorf("%w: 本机密钥属于另一个仓库", ErrKeyMismatch)
	}
	if publishMeta {
		// 换口令：工作区里的 meta.json 即将提交，按它而不是远端旧版本校验。
		if local, err := LoadMetaFile(c.repoPath()); err == nil {
			if salt, err := local.SaltBytes(); err == nil && bytes.Equal(salt, key.Salt) && local.KeyVersion == key.KeyVersion {
				return true, key.KeyVersion, nil
			}
		}
		return false, key.KeyVersion, fmt.Errorf("%w: 新 meta.json 与密钥不匹配", ErrKeyMismatch)
	}
	if !hasMeta {
		// 远端为空（首次推送或分支被清空）：用本机密钥重建 meta.json 是安全的。
		return true, key.KeyVersion, nil
	}
	salt, err := meta.SaltBytes()
	if err != nil {
		return false, key.KeyVersion, err
	}
	if !bytes.Equal(salt, key.Salt) {
		return false, key.KeyVersion, ErrKeyMismatch
	}
	if meta.KeyVersion > key.KeyVersion {
		return false, key.KeyVersion, fmt.Errorf("%w: 仓库已更换密钥，请重新输入口令", ErrKeyMismatch)
	}
	if key.KeyVersion > meta.KeyVersion {
		return false, key.KeyVersion, fmt.Errorf("%w: 仓库密钥版本低于本机，可能被回滚", ErrKeyMismatch)
	}
	return true, key.KeyVersion, nil
}

func keyBytes(available bool, key *KeyState) []byte {
	if !available || key == nil {
		return nil
	}
	return key.Key
}

// pruneAccountFiles 删除当前树里没有对应明文的 accounts/*.age。
// 只在换口令后调用，避免旧密钥密文继续以“当前版本”存在。
func pruneAccountFiles(repoDir string, keep map[string][]byte) {
	dir := filepath.Join(repoDir, AccountsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".age") {
			continue
		}
		path := AccountsDir + "/" + entry.Name()
		if _, ok := keep[path]; ok {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

// upgradeMeta 把工作区里的 meta.json 升到当前版本；已经是当前版本或还没初始化时
// 返回 false。只改版本号，不重新派生密钥，所以升级对其它设备是纯格式变更。
func (c *Client) upgradeMeta() (bool, error) {
	path := filepath.Join(c.repoPath(), MetaFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	meta, err := DecodeMeta(data)
	if err != nil {
		return false, err
	}
	if meta.V >= MetaVersion {
		return false, nil
	}
	meta.V = MetaVersion
	encoded, err := meta.Encode()
	if err != nil {
		return false, err
	}
	if err := writeFileAtomic(path, encoded, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) ensureMetaFile(key *KeyState, keyVersion int) error {
	if key == nil {
		return ErrKeyMissing
	}
	path := filepath.Join(c.repoPath(), MetaFile)
	if fileExists(path) {
		return nil
	}
	meta := Meta{
		V:          MetaVersion,
		KeyVersion: keyVersion,
		KDF:        key.KDF,
		Salt:       encodeBase64(key.Salt),
		Keycheck:   encodeBase64(Keycheck(key.Key)),
		CreatedAt:  c.now().Unix(),
	}
	data, err := meta.Encode()
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

func (c *Client) commitMessage(device string) string {
	return fmt.Sprintf("sync: %s %s", device, c.now().Format("2006-01-02 15:04:05"))
}

func appendUnique(dst []string, items ...string) []string {
	seen := make(map[string]bool, len(dst))
	for _, item := range dst {
		seen[item] = true
	}
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		dst = append(dst, item)
	}
	return dst
}

// SetToken 保存可选的 HTTPS 令牌兜底（0600）；空字符串表示删除。
func (c *Client) SetToken(token string) error { return SaveToken(c.opts.TokenPath, token) }

// ForgetKey 删除本机缓存的派生密钥；远端密文与登录态保持不变。
func (c *Client) ForgetKey() error {
	if err := os.Remove(c.opts.KeyPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除本机同步密钥: %w", err)
	}
	return nil
}

// SetPassphrase 用新口令重新派生密钥并重加密全部登录态。
// 旧密钥可用时先拉取一轮，把远端最新登录态并入本地，再用新密钥重写。
func (c *Client) SetPassphrase(ctx context.Context, newPassphrase string) (Result, error) {
	if err := c.acquire(ctx); err != nil {
		return Result{}, err
	}
	defer c.release()
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
	if key != nil && key.RemoteHash != "" && key.RemoteHash != RemoteHash(cfg.Remote) {
		return Result{}, fmt.Errorf("%w: 本机密钥属于另一个仓库", ErrKeyMismatch)
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
	empty := false
	if err := g.Fetch(ctx); err != nil {
		if !errors.Is(err, errEmptyRemote) {
			return Result{}, err
		}
		empty = true
	}
	baseVersion := 1
	if key != nil {
		baseVersion = key.KeyVersion
	}
	if !empty {
		if data, ok, err := g.showFile(ctx, "FETCH_HEAD", MetaFile); err == nil && ok {
			if meta, err := DecodeMeta(data); err == nil && meta.KeyVersion > baseVersion {
				baseVersion = meta.KeyVersion
			}
		}
	}
	if key != nil {
		// 先把远端最新登录态并入本地；冲突时交给用户先解决。
		if _, err := c.syncWithLock(ctx, cfg, key, g, syncRunOptions{}); err != nil {
			return Result{}, err
		}
	}

	meta, newKey, err := NewMeta(newPassphrase, c.now())
	if err != nil {
		return Result{}, err
	}
	newKey.KeyVersion = baseVersion + 1
	meta.KeyVersion = newKey.KeyVersion
	newKey.RemoteHash = RemoteHash(cfg.Remote)
	data, err := meta.Encode()
	if err != nil {
		return Result{}, err
	}

	// 备份旧 meta 与密钥：同步失败时回滚，避免本机与远端密钥版本错位。
	metaPath := filepath.Join(c.repoPath(), MetaFile)
	oldMeta, oldMetaErr := os.ReadFile(metaPath)
	oldKey, oldKeyErr := os.ReadFile(c.opts.KeyPath)
	if err := writeFileAtomic(metaPath, data, 0o600); err != nil {
		return Result{}, err
	}
	if err := newKey.Save(c.opts.KeyPath); err != nil {
		return Result{}, err
	}
	result, err := c.syncWithLock(ctx, cfg, newKey, g, syncRunOptions{skipRecover: true, publishMeta: true})
	if err != nil {
		if oldMetaErr == nil {
			_ = writeFileAtomic(metaPath, oldMeta, 0o600)
		} else {
			_ = os.Remove(metaPath)
		}
		if oldKeyErr == nil {
			_ = writeFileAtomic(c.opts.KeyPath, oldKey, 0o600)
		} else {
			_ = os.Remove(c.opts.KeyPath)
		}
		return result, err
	}
	return result, nil
}
