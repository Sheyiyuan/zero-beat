// Package sync 把本地歌单、快捷键与加密登录态同步到任意 git 仓库。
//
// 依赖方向：sync → store / config / keymap，不依赖 Bubble Tea；
// 所有网络与磁盘长任务都应在调用方（TUI 的 tea.Cmd 或 CLI）里执行。
package sync

import "errors"

// 错误分类见 docs/sync-design.md §19；调用方用 errors.Is 判断后给出可操作提示。
var (
	ErrNotConfigured    = errors.New("未配置同步仓库")
	ErrKeyMissing       = errors.New("需要输入同步口令")
	ErrKeyMismatch      = errors.New("同步口令不正确或仓库已更换密钥")
	ErrAuth             = errors.New("仓库认证失败")
	ErrNetwork          = errors.New("网络不可用")
	ErrConflict         = errors.New("有冲突待处理")
	ErrSchema           = errors.New("仓库格式版本过高")
	ErrPushRejected     = errors.New("远端有新提交")
	ErrDirtyWorktree    = errors.New("上次同步未完成")
	ErrBusy             = errors.New("另一个同步正在进行")
	ErrCredentialReject = errors.New("登录态密文无法解出")
	ErrGitMissing       = errors.New("找不到 git 命令")
	ErrCorruptRepo      = errors.New("同步仓库内容无效")
	ErrGit              = errors.New("git 操作失败")
)
