package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/config"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/source"
	syncpkg "github.com/sheyiyuan/zero-beat/internal/sync"
)

// 同步相关的 tea.Msg。
type syncEventMsg struct{ ev syncpkg.Event }

type syncStatusMsg struct {
	status          syncpkg.Status
	localOnly       int
	localAnnotation int
}

type syncConflictsMsg struct {
	index syncpkg.ConflictIndex
	err   error
}

type syncResultMsg struct {
	kind      string
	joined    bool
	reusedKey bool
	result    syncpkg.Result
	err       error
}

type sessionsAppliedMsg struct{ sources []source.ID }

// 同步页的动作 ID。
const (
	syncActAck       = "ack"
	syncActRemote    = "remote"
	syncActBranch    = "branch"
	syncActPass      = "pass"
	syncActInit      = "init"
	syncActNow       = "now"
	syncActConflicts = "conflicts"
	syncActRotate    = "rotate"
	syncActForget    = "forget"
	syncActAuto      = "auto"
	syncActToken     = "token"
	syncActArchive   = "archive"
	syncActTest      = "test"
	syncActDiscard   = "discard"
	syncActMode      = "mode"
)

type syncRow struct {
	action   string
	label    string
	hint     string
	disabled bool
}

// leaveSyncPage 离开同步页时清掉口令缓冲，避免它在内存里长期停留。
func (m *Model) leaveSyncPage() {
	m.view = viewLibrary
	m.syncEdit = ""
	m.syncInput = ""
	m.syncPass = ""
}

// openSyncPage 打开同步页并刷新状态。
func (m *Model) openSyncPage() tea.Cmd {
	m.view = viewSync
	m.syncEdit = ""
	m.syncInput = ""
	if m.syncMgr == nil {
		m.syncStatus = syncpkg.Status{}
		return nil
	}
	return m.syncStatusCmd()
}

// waitSyncCmd 把 Manager 事件桥接成 tea.Msg；每处理一次都要重新挂上。
func waitSyncCmd(m *syncpkg.Manager) tea.Cmd {
	if m == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-m.Events()
		if !ok {
			return nil
		}
		return syncEventMsg{ev: ev}
	}
}

func (m *Model) syncConfigured() bool { return m.syncStatus.Configured }

func (m *Model) syncRows() []syncRow {
	rows := make([]syncRow, 0, 12)
	if !m.syncConfigured() || !m.syncStatus.KeyMatchesRepo {
		ack := "[ ] 我已知晓：歌单会以明文推送到仓库"
		if m.syncAck {
			ack = "[x] 我已知晓：歌单会以明文推送到仓库"
		}
		rows = append(rows, syncRow{action: syncActAck, label: ack, hint: "space 切换"})
	}
	remote := m.syncStatus.Remote
	if remote == "" {
		remote = "未设置"
	}
	rows = append(rows, syncRow{action: syncActRemote, label: "仓库地址 · " + truncate(remote, 48), hint: "enter 编辑"})
	if m.syncStatus.Remote != "" {
		mode := syncpkg.RemoteMode(m.syncStatus.Remote)
		next, convertible := syncpkg.ToggleRemoteScheme(m.syncStatus.Remote)
		hint := ""
		if convertible {
			hint = "enter 切换为 " + syncModeLabel(syncpkg.RemoteMode(next))
		}
		rows = append(rows, syncRow{
			action:   syncActMode,
			label:    "连接方式 · " + syncModeLabel(mode),
			hint:     hint,
			disabled: !convertible,
		})
	}
	if m.syncStatus.Remote != "" {
		rows = append(rows, syncRow{action: syncActTest, label: "测试连接", hint: "enter 执行"})
	}
	rows = append(rows, syncRow{action: syncActBranch, label: "分支 · " + m.syncStatus.Branch, hint: "enter 编辑"})
	pass := "未输入"
	if m.syncPass != "" {
		pass = "已输入（提交后清除）"
	}
	rows = append(rows, syncRow{action: syncActPass, label: "同步口令 · " + pass, hint: "enter 输入（至少 12 字符）"})
	if !m.syncConfigured() || !m.syncStatus.KeyMatchesRepo {
		hint := "enter 执行"
		switch {
		case !m.syncAck:
			hint = "先按 space 确认明文提示"
		case m.syncPass == "":
			hint = "先输入同步口令"
		}
		rows = append(rows, syncRow{action: syncActInit, label: "初始化 / 加入仓库", hint: hint})
	}
	rows = append(rows, syncRow{action: syncActNow, label: "立即同步", hint: "enter 执行", disabled: !m.syncConfigured()})
	if m.syncStatus.Conflicts > 0 || m.syncStatus.ConflictTx != "" {
		rows = append(rows, syncRow{action: syncActConflicts,
			label: fmt.Sprintf("处理冲突 · %d 项", m.syncStatus.Conflicts), hint: "enter 打开"})
		rows = append(rows, syncRow{action: syncActDiscard, label: "放弃本轮冲突并重新拉取", hint: "enter 执行"})
	}
	if m.syncConfigured() {
		rows = append(rows, syncRow{action: syncActRotate, label: "更换口令并推送", hint: "enter 执行"})
	}
	if m.syncStatus.KeyCached {
		rows = append(rows, syncRow{action: syncActForget, label: "忘记本机密钥", hint: "enter 执行"})
	}
	auto := "开"
	if !m.syncStatus.Auto {
		auto = "关"
	}
	rows = append(rows, syncRow{action: syncActAuto, label: "自动同步 · " + auto, hint: "enter 切换", disabled: !m.syncConfigured()})
	if strings.HasPrefix(m.syncStatus.Remote, "http://") || strings.HasPrefix(m.syncStatus.Remote, "https://") {
		rows = append(rows, syncRow{action: syncActToken, label: "配置 HTTPS 令牌（0600 兜底）", hint: "enter 输入"})
	}
	rows = append(rows, syncRow{action: syncActArchive, label: "打开归档目录", hint: "enter 打开"})
	return rows
}

func (m *Model) renderSync(h int) []string {
	out := make([]string, 0, h)
	add := func(line string) { out = append(out, truncate(line, m.width)) }

	status := m.syncStatus
	if m.syncMgr == nil && !status.Configured {
		add(" " + st.warn.Render("同步不可用：未能初始化同步管理器。"))
	} else if !status.Configured {
		add(" " + st.muted.Render("同步尚未配置。填写仓库地址与口令，确认明文提示后初始化。"))
		add(" " + st.dim.Render(truncate("地址可直接填 SSH（git@host:owner/repo.git）或 HTTPS。", max(1, m.width-4))))
	} else {
		line := " " + st.muted.Render("状态 ") + st.text.Render(status.Summary())
		if status.Conflicts > 0 {
			line += st.dim.Render(" · ") + st.warn.Render(fmt.Sprintf("%d 个冲突", status.Conflicts))
		}
		add(line)
		add(" " + st.muted.Render("仓库 ") + st.text.Render(truncate(status.Remote, max(1, m.width-10))))
		keyline := "未保存"
		if status.KeyCached {
			keyline = fmt.Sprintf("已保存（版本 %d）", status.KeyVersion)
			if !status.KeyMatchesRepo {
				keyline = fmt.Sprintf("版本 %d，属于另一个仓库", status.KeyVersion)
			}
		}
		add(fmt.Sprintf(" %s %s · %s %s · %s %s",
			st.muted.Render("分支"), st.text.Render(status.Branch),
			st.muted.Render("认证"), st.text.Render(status.Auth),
			st.muted.Render("密钥"), st.text.Render(keyline)))
	}
	if m.localOnly > 0 || m.localAnnotation > 0 {
		add(fmt.Sprintf(" %s %s · %s %s", st.muted.Render("仅本机"),
			st.text.Render(fmt.Sprintf("%d 个歌单", m.localOnly)),
			st.muted.Render("本机注释"), st.text.Render(fmt.Sprintf("%d 条", m.localAnnotation))))
		add(" " + st.dim.Render(truncate("仅本机数据不进仓库，其它设备不会看到，也不会被同步删除。", max(1, m.width-4))))
	}
	if status.KeyIssue != "" {
		add(" " + st.warn.Render(truncate("密钥："+status.KeyIssue, max(1, m.width-4))))
	}
	if status.LastError != "" {
		add(" " + st.err.Render(truncate("上次失败："+status.LastError, max(1, m.width-4))))
	}
	switch syncpkg.RemoteMode(status.Remote) {
	case syncpkg.ModeSSH:
		add(" " + st.dim.Render(truncate("SSH 使用 ssh-agent / ~/.ssh/config；22 端口不通时可切 HTTPS。", max(1, m.width-4))))
	case syncpkg.ModeHTTPS:
		add(" " + st.dim.Render(truncate("HTTPS 使用系统 credential helper；也可配置 0600 令牌。", max(1, m.width-4))))
	}

	if m.syncEdit != "" {
		add("")
		input := m.syncInput
		if syncEditMasked(m.syncEdit) {
			// 用 ASCII 星号而不是圆点：极简终端/字体里 • 可能宽度异常或显示为空白。
			input = strings.Repeat("*", len([]rune(input)))
		}
		cursor := st.accent.Render("▏")
		add(" " + st.accent.Render(syncEditLabel(m.syncEdit)) + st.text.Render(truncate(input, max(1, m.width-16))) + cursor)
		add(" " + st.dim.Render("enter 确认 · esc 取消"+(syncEditSecret(m.syncEdit)+"")))
		return padLines(out, h)
	}

	add("")
	rows := m.syncRows()
	if m.syncCursor >= len(rows) {
		m.syncCursor = len(rows) - 1
	}
	if m.syncCursor < 0 {
		m.syncCursor = 0
	}
	visible := h - len(out) - 1
	if visible < 1 {
		visible = 1
	}
	start := 0
	if m.syncCursor >= visible {
		start = m.syncCursor - visible + 1
	}
	for i := start; i < len(rows) && i < start+visible; i++ {
		row := rows[i]
		marker := "  "
		style := st.text
		if i == m.syncCursor {
			marker = st.accent.Render("› ")
			style = st.accent
		}
		line := " " + marker + style.Render(truncate(row.label, max(1, m.width-6)))
		if row.hint != "" && !row.disabled && m.width > visWidth(line)+len(row.hint)+4 {
			line += st.dim.Render("  " + row.hint)
		}
		if row.disabled {
			line = " " + marker + st.dim.Render(truncate(row.label, max(1, m.width-6)))
		}
		add(line)
	}
	if m.syncBusy {
		add("")
		add(" " + st.warn.Render("同步进行中…"))
	} else if len(status.Warnings) > 0 {
		add("")
		add(" " + st.warn.Render(truncate("提示："+status.Warnings[0], max(1, m.width-4))))
	}
	if status.Configured {
		add("")
		add(" " + st.dim.Render(truncate("私有仓库只提供访问控制；登录态同步可能被平台风控失效。", max(1, m.width-4))))
	}
	return padLines(out, h)
}

func syncModeLabel(mode string) string {
	switch mode {
	case syncpkg.ModeSSH:
		return "SSH"
	case syncpkg.ModeHTTPS:
		return "HTTPS"
	case syncpkg.ModeLocal:
		return "本地仓库"
	}
	return "其他"
}

func syncEditLabel(edit string) string {
	switch edit {
	case "remote":
		return "仓库地址："
	case "branch":
		return "分支："
	case "pass":
		return "同步口令："
	case "token":
		return "HTTPS 令牌："
	}
	return "输入："
}

func syncEditMasked(edit string) bool {
	return edit == "pass" || edit == "token"
}

func syncEditSecret(edit string) string {
	if edit == "pass" || edit == "token" {
		return "（输入不回显，保存后立即从界面清除）"
	}
	return ""
}

func (m *Model) handleSyncKey(key tea.Key) (tea.Model, tea.Cmd) {
	if isCtrl(key, 'c') {
		m.shutdown()
		return m, tea.Quit
	}
	if m.syncEdit != "" {
		switch key.Code {
		case tea.KeyEscape:
			m.syncEdit = ""
			m.syncInput = ""
			return m, nil
		case tea.KeyEnter, tea.KeyKpEnter:
			return m, m.commitSyncEdit()
		case tea.KeyBackspace:
			if r := []rune(m.syncInput); len(r) > 0 {
				m.syncInput = string(r[:len(r)-1])
			}
			return m, nil
		case tea.KeySpace:
			m.syncInput += " "
			return m, nil
		}
		if key.Text != "" && !hasControlModifier(key) {
			m.syncInput += key.Text
		}
		return m, nil
	}
	// 直接打字即可开始编辑当前行，不必先按 enter；导航/返回/同步键仍优先。
	if key.Text != "" && key.Text != " " && !hasControlModifier(key) && !m.syncNavigationKey(key) {
		if rows := m.syncRows(); m.syncCursor >= 0 && m.syncCursor < len(rows) {
			switch rows[m.syncCursor].action {
			case syncActRemote, syncActBranch, syncActPass, syncActToken:
				m.syncEdit = rows[m.syncCursor].action
				m.syncInput = key.Text
				return m, nil
			}
		}
	}
	switch {
	case key.Code == tea.KeyTab:
		step := 1
		if isShiftTab(key) {
			step = -1
		}
		return m.openPage(m.cyclePage(step))
	case m.match(keymap.Sync, key), m.match(keymap.Back, key):
		m.leaveSyncPage()
		return m, nil
	case key.Code == tea.KeyEscape:
		m.leaveSyncPage()
		return m, nil
	case m.match(keymap.Up, key):
		if m.syncCursor > 0 {
			m.syncCursor--
		}
		return m, nil
	case m.match(keymap.Down, key):
		if m.syncCursor < len(m.syncRows())-1 {
			m.syncCursor++
		}
		return m, nil
	case key.Code == tea.KeySpace || key.Text == " ":
		rows := m.syncRows()
		if m.syncCursor >= 0 && m.syncCursor < len(rows) && rows[m.syncCursor].action == syncActAck {
			m.syncAck = !m.syncAck
		}
		return m, nil
	case isEnterKey(key):
		rows := m.syncRows()
		if m.syncCursor < 0 || m.syncCursor >= len(rows) {
			return m, nil
		}
		return m, m.runSyncRow(rows[m.syncCursor])
	}
	return m, nil
}

func (m *Model) runSyncRow(row syncRow) tea.Cmd {
	if m.syncMgr == nil {
		m.setNotice("同步不可用", true)
		return nil
	}
	switch row.action {
	case syncActAck:
		m.syncAck = !m.syncAck
	case syncActRemote:
		m.syncEdit = "remote"
		m.syncInput = m.syncStatus.Remote
	case syncActBranch:
		m.syncEdit = "branch"
		m.syncInput = m.syncStatus.Branch
	case syncActPass:
		m.syncEdit = "pass"
		m.syncInput = ""
	case syncActToken:
		m.syncEdit = "token"
		m.syncInput = ""
	case syncActInit:
		if !m.syncAck {
			m.focusSyncAction(syncActAck)
			m.setNotice("先按 space 确认「歌单会以明文推送」提示", true)
			return nil
		}
		if m.syncPass == "" {
			m.focusSyncAction(syncActPass)
			m.setNotice("先输入同步口令（至少 12 个字符）", true)
			return nil
		}
		return m.initSyncCmd()
	case syncActNow:
		if !m.syncStatus.Configured {
			m.setNotice("先配置仓库地址", true)
			return nil
		}
		m.syncBusy = true
		m.syncMgr.SyncNow()
	case syncActConflicts:
		return m.loadConflictsCmd()
	case syncActMode:
		next, ok := syncpkg.ToggleRemoteScheme(m.syncStatus.Remote)
		if !ok {
			m.syncEdit = "remote"
			m.syncInput = m.syncStatus.Remote
			m.setNotice("当前地址无法自动转换，请手动填写 SSH 或 HTTPS 地址", true)
			return nil
		}
		mgr := m.syncMgr
		branch := m.syncStatus.Branch
		auto := m.syncStatus.Auto
		return func() tea.Msg {
			err := mgr.Client().Configure(next, branch, auto)
			return syncResultMsg{kind: "mode", err: err}
		}
	case syncActTest:
		return m.testSyncConnectionCmd()
	case syncActDiscard:
		return m.discardSyncConflictCmd()
	case syncActRotate:
		if m.syncPass == "" {
			m.focusSyncAction(syncActPass)
			m.setNotice("先输入新口令", true)
			return nil
		}
		return m.rotateSyncCmd()
	case syncActForget:
		return m.forgetSyncKeyCmd()
	case syncActAuto:
		return m.toggleSyncAutoCmd()
	case syncActArchive:
		path := m.syncMgr.Client().ArchiveDir()
		if err := openLocalFile(path); err != nil {
			m.setError(err)
		} else {
			m.setNotice("已打开归档目录："+path, false)
		}
	}
	return nil
}

// hasControlModifier 判断是否带 ctrl/alt/super；这些组合键不应写入文本框。
func hasControlModifier(key tea.Key) bool {
	return key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModSuper)
}

// isEnterKey 同时接受主键盘与小键盘回车；Bubble Tea 把后者报成 KeyKpEnter。
func isEnterKey(key tea.Key) bool {
	return key.Code == tea.KeyEnter || key.Code == tea.KeyKpEnter
}

// syncNavigationKey 判断是否是可打印键位上的导航/返回/同步快捷键，
// 避免「直接打字进入编辑」把 j/k 或自定义的 b 抢走。
func (m *Model) syncNavigationKey(key tea.Key) bool {
	return m.match(keymap.Up, key) || m.match(keymap.Down, key) ||
		m.match(keymap.Back, key) || m.match(keymap.Sync, key)
}

// focusSyncAction 把光标移到指定动作行，让缺少的前置步骤可见。
func (m *Model) focusSyncAction(action string) {
	for i, row := range m.syncRows() {
		if row.action == action {
			m.syncCursor = i
			return
		}
	}
}

func (m *Model) commitSyncEdit() tea.Cmd {
	edit := m.syncEdit
	value := strings.TrimSpace(m.syncInput)
	switch edit {
	case "pass":
		// 口令按原样保存：不裁剪空格，也不在长度不足时退出编辑态，
		// 否则用户会以为「回车没反应」。
		raw := m.syncInput
		if len([]rune(raw)) < syncpkg.MinPassphraseRunes {
			m.setNotice(fmt.Sprintf("口令至少 %d 个字符，可继续输入", syncpkg.MinPassphraseRunes), true)
			return nil
		}
		m.syncPass = raw
		m.syncEdit = ""
		m.syncInput = ""
		m.setNotice("同步口令已输入（不会显示，提交后清除）", false)
		return nil
	case "token":
		m.syncEdit = ""
		m.syncInput = ""
		mgr := m.syncMgr
		return func() tea.Msg {
			err := mgr.Client().SetToken(value)
			return syncResultMsg{kind: "token", err: err}
		}
	case "remote", "branch":
		if edit == "remote" && value == "" {
			m.setNotice("仓库地址不能为空，可继续输入或 esc 取消", true)
			return nil
		}
		remote, branch := m.syncStatus.Remote, m.syncStatus.Branch
		if edit == "remote" {
			remote = value
		} else {
			branch = value
		}
		if remote == "" {
			m.setNotice("仓库地址不能为空", true)
			return nil
		}
		m.syncEdit = ""
		m.syncInput = ""
		mgr := m.syncMgr
		return func() tea.Msg {
			err := mgr.Client().Configure(remote, branch, mgr.Status().Auto)
			return syncResultMsg{kind: "configure", err: err}
		}
	}
	m.syncEdit = ""
	m.syncInput = ""
	return nil
}

func (m *Model) initSyncCmd() tea.Cmd {
	if !m.syncAck {
		m.setNotice("先确认「歌单会以明文推送」提示", true)
		return nil
	}
	if m.syncStatus.Remote == "" {
		m.setNotice("先填写仓库地址", true)
		return nil
	}
	if m.syncPass == "" {
		m.setNotice("先输入同步口令", true)
		return nil
	}
	mgr := m.syncMgr
	remote := m.syncStatus.Remote
	branch := m.syncStatus.Branch
	auto := m.syncStatus.Auto
	pass := m.syncPass
	m.syncPass = ""
	m.syncBusy = true
	return func() tea.Msg {
		out, err := mgr.Client().Init(context.Background(), remote, branch, auto, pass)
		return syncResultMsg{kind: "init", joined: out.Joined, reusedKey: out.ReusedKey, result: out.Synced, err: err}
	}
}

func (m *Model) rotateSyncCmd() tea.Cmd {
	if m.syncPass == "" {
		m.setNotice("先输入新口令", true)
		return nil
	}
	mgr := m.syncMgr
	pass := m.syncPass
	m.syncPass = ""
	m.syncBusy = true
	return func() tea.Msg {
		res, err := mgr.Client().SetPassphrase(context.Background(), pass)
		return syncResultMsg{kind: "rotate", result: res, err: err}
	}
}

func (m *Model) forgetSyncKeyCmd() tea.Cmd {
	mgr := m.syncMgr
	return func() tea.Msg {
		err := mgr.Client().ForgetKey()
		return syncResultMsg{kind: "forget", err: err}
	}
}

func (m *Model) toggleSyncAutoCmd() tea.Cmd {
	mgr := m.syncMgr
	auto := !m.syncStatus.Auto
	return func() tea.Msg {
		err := mgr.Client().SetAuto(auto)
		return syncResultMsg{kind: "auto", err: err}
	}
}

func (m *Model) syncStatusCmd() tea.Cmd {
	mgr := m.syncMgr
	if mgr == nil {
		return nil
	}
	return func() tea.Msg {
		localOnly, localAnnotations := m.service().LocalOnlyCounts()
		return syncStatusMsg{status: mgr.Status(), localOnly: localOnly, localAnnotation: localAnnotations}
	}
}

func (m *Model) testSyncConnectionCmd() tea.Cmd {
	mgr := m.syncMgr
	return func() tea.Msg {
		joined, err := mgr.Client().Probe(context.Background())
		return syncResultMsg{kind: "test", joined: joined, err: err}
	}
}

func (m *Model) discardSyncConflictCmd() tea.Cmd {
	mgr := m.syncMgr
	txid := m.syncStatus.ConflictTx
	if txid == "" {
		m.setNotice("没有待处理的冲突", false)
		return nil
	}
	m.syncBusy = true
	return func() tea.Msg {
		if err := mgr.Client().DiscardConflict(context.Background(), txid); err != nil {
			return syncResultMsg{kind: "discard", err: err}
		}
		mgr.SyncNow()
		return syncResultMsg{kind: "discard"}
	}
}

func (m *Model) loadConflictsCmd() tea.Cmd {
	mgr := m.syncMgr
	return func() tea.Msg {
		index, err := mgr.Client().Conflicts()
		return syncConflictsMsg{index: index, err: err}
	}
}

func (m *Model) applyConflictsCmd() tea.Cmd {
	if m.syncMgr == nil || m.syncConflicts.TxID == "" {
		return nil
	}
	choices := make(map[string]syncpkg.Choice, len(m.syncChoices))
	for k, v := range m.syncChoices {
		choices[k] = v
	}
	txid := m.syncConflicts.TxID
	mgr := m.syncMgr
	m.syncBusy = true
	return func() tea.Msg {
		res, err := mgr.Client().Resolve(context.Background(), txid, choices)
		return syncResultMsg{kind: "resolve", result: res, err: err}
	}
}

// conflictsComplete 判断是否每个实体都做了选择。
func (m *Model) conflictsComplete() bool {
	for _, entity := range m.syncConflicts.Entities {
		if _, ok := m.syncChoices[entity.Entity]; !ok {
			return false
		}
	}
	return len(m.syncConflicts.Entities) > 0
}

func (m *Model) conflictMissing() int {
	n := 0
	for _, entity := range m.syncConflicts.Entities {
		if _, ok := m.syncChoices[entity.Entity]; !ok {
			n++
		}
	}
	return n
}

func (m *Model) renderConflicts(h int) []string {
	out := make([]string, 0, h)
	add := func(line string) { out = append(out, truncate(line, m.width)) }
	index := m.syncConflicts
	add(fmt.Sprintf(" %s %s · %s", st.muted.Render("冲突事务"), index.TxID,
		st.muted.Render(fmt.Sprintf("共 %d 项", len(index.Entities)))))
	add(" " + st.dim.Render("逐项选择；全部选完后按 enter 应用并推送。"))
	add("")
	entities := index.Entities
	visible := max(1, (h-5)/2)
	if m.syncConflictIdx >= len(entities) {
		m.syncConflictIdx = len(entities) - 1
	}
	if m.syncConflictIdx < 0 {
		m.syncConflictIdx = 0
	}
	start := 0
	if m.syncConflictIdx >= visible {
		start = m.syncConflictIdx - visible + 1
	}
	for i := start; i < len(entities) && i < start+visible; i++ {
		entity := entities[i]
		marker := "  "
		if i == m.syncConflictIdx {
			marker = st.accent.Render("› ")
		}
		choice, ok := m.syncChoices[entity.Entity]
		choiceText := st.warn.Render("未选择")
		if ok {
			choiceText = st.ok.Render(syncChoiceLabel(choice))
		}
		line := " " + marker + st.text.Render(truncate(syncpkg.DescribeEntity(entity), max(1, m.width-20))) +
			st.dim.Render(" · ") + choiceText
		add(line)
		add("    " + st.dim.Render(truncate(syncpkg.SummarizeConflict(entity), max(1, m.width-6))))
	}
	if len(entities) > visible {
		add(" " + st.dim.Render(fmt.Sprintf("%d/%d", m.syncConflictIdx+1, len(entities))))
	}
	add("")
	add(" " + st.dim.Render("↑/↓ 选择 · ←/→ 或 l/r 本地/远程 · b 保留两者 · L/R 全部 · enter 应用 · esc 返回"))
	if missing := m.conflictMissing(); missing > 0 {
		add(" " + st.warn.Render(fmt.Sprintf("还有 %d 项未选择，应用不可用", missing)))
	}
	return padLines(out, h)
}

func syncChoiceLabel(choice syncpkg.Choice) string {
	switch choice {
	case syncpkg.KeepLocal:
		return "保留本地"
	case syncpkg.KeepRemote:
		return "保留远程"
	case syncpkg.KeepBoth:
		return "保留两者"
	}
	return "未选择"
}

func (m *Model) handleConflictKey(key tea.Key) (tea.Model, tea.Cmd) {
	if isCtrl(key, 'c') {
		m.shutdown()
		return m, tea.Quit
	}
	entities := m.syncConflicts.Entities
	switch {
	case key.Code == tea.KeyTab:
		m.view = viewSync
		return m, m.syncStatusCmd()
	// b 在冲突页是「保留两者」，优先于可配置的返回键；返回用 esc。
	case isKey(key, "b"):
		m.setConflictChoice(syncpkg.KeepBoth)
		return m, nil
	case m.match(keymap.Back, key), key.Code == tea.KeyEscape:
		m.view = viewSync
		return m, nil
	case m.match(keymap.Up, key):
		if m.syncConflictIdx > 0 {
			m.syncConflictIdx--
		}
		return m, nil
	case m.match(keymap.Down, key):
		if m.syncConflictIdx < len(entities)-1 {
			m.syncConflictIdx++
		}
		return m, nil
	case isKey(key, "l"), m.match(keymap.Left, key):
		m.setConflictChoice(syncpkg.KeepLocal)
		return m, nil
	case isKey(key, "r"), m.match(keymap.Right, key):
		m.setConflictChoice(syncpkg.KeepRemote)
		return m, nil
	case isKey(key, "L"):
		for _, entity := range entities {
			m.syncChoices[entity.Entity] = syncpkg.KeepLocal
		}
		return m, nil
	case isKey(key, "R"):
		for _, entity := range entities {
			m.syncChoices[entity.Entity] = syncpkg.KeepRemote
		}
		return m, nil
	case isEnterKey(key):
		if !m.conflictsComplete() {
			m.setNotice("还有冲突未选择", true)
			return m, nil
		}
		return m, m.applyConflictsCmd()
	}
	return m, nil
}

func (m *Model) setConflictChoice(choice syncpkg.Choice) {
	if m.syncConflictIdx < 0 || m.syncConflictIdx >= len(m.syncConflicts.Entities) {
		return
	}
	entity := m.syncConflicts.Entities[m.syncConflictIdx]
	if choice == syncpkg.KeepBoth && !entity.SupportsBoth() {
		m.setNotice("这个实体不支持「保留两者」", true)
		return
	}
	if m.syncChoices == nil {
		m.syncChoices = map[string]syncpkg.Choice{}
	}
	m.syncChoices[entity.Entity] = choice
}

// handleSyncResult 处理初始化、改口令、解冲突等长操作的结果。
func (m *Model) handleSyncResult(msg syncResultMsg) (tea.Model, tea.Cmd) {
	m.syncBusy = false
	var cmds []tea.Cmd
	if msg.err != nil {
		m.setError(msg.err)
		if errors.Is(msg.err, syncpkg.ErrConflict) {
			m.setWarn("有冲突待处理，打开同步页选择")
		}
		return m, tea.Batch(append(cmds, m.syncStatusCmd())...)
	}
	switch msg.kind {
	case "init":
		if msg.joined {
			m.setNotice("已加入同步仓库", false)
		} else if msg.reusedKey {
			m.setNotice("已创建同步仓库并复用本机密钥", false)
		} else {
			m.setNotice("已创建同步仓库并推送", false)
		}
		if len(msg.result.AccountsApplied) > 0 {
			cmds = append(cmds, m.applySessionsCmd())
		}
		if msg.result.KeymapUpdated {
			m.reloadKeymap()
		}
		cmds = append(cmds, m.loadPlaylistsCmd())
		m.syncAck = true
	case "resolve":
		m.view = viewSync
		m.syncConflicts = syncpkg.ConflictIndex{}
		m.syncChoices = map[string]syncpkg.Choice{}
		m.setNotice("冲突已解决并推送", false)
		cmds = append(cmds, m.loadPlaylistsCmd())
		if len(msg.result.AccountsApplied) > 0 {
			cmds = append(cmds, m.applySessionsCmd())
		}
		if msg.result.KeymapUpdated {
			m.reloadKeymap()
		}
	case "configure":
		m.setNotice("仓库配置已保存；换了仓库请重新初始化/加入", false)
	case "mode":
		m.setNotice("连接方式已切换；密钥与登录态保持不变", false)
	case "test":
		if msg.joined {
			m.setNotice("连接成功：远端已有同步数据，可输入口令加入", false)
		} else {
			m.setNotice("连接成功：远端为空，可初始化", false)
		}
	case "discard":
		m.syncConflicts = syncpkg.ConflictIndex{}
		m.syncChoices = map[string]syncpkg.Choice{}
		m.view = viewSync
		m.setNotice("已放弃本轮冲突，正在重新拉取", false)
	case "token":
		m.setNotice("HTTPS 令牌已更新", false)
	case "forget":
		m.setNotice("已删除本机同步密钥", false)
	case "auto":
		m.setNotice("自动同步开关已更新", false)
	case "rotate":
		m.setNotice("口令已更换；其他设备需要重新输入", false)
		if len(msg.result.AccountsApplied) > 0 {
			cmds = append(cmds, m.applySessionsCmd())
		}
		cmds = append(cmds, m.loadPlaylistsCmd())
	}
	for _, warning := range msg.result.Warnings {
		m.setWarn(warning)
		break
	}
	cmds = append(cmds, m.syncStatusCmd())
	return m, tea.Batch(cmds...)
}

// handleSyncEvent 把后台事件折进界面状态。
func (m *Model) handleSyncEvent(ev syncpkg.Event) (tea.Model, tea.Cmd) {
	m.syncStatus = ev.Status
	cmds := []tea.Cmd{waitSyncCmd(m.syncMgr)}
	switch ev.Kind {
	case syncpkg.EventStarted:
		m.syncBusy = true
	case syncpkg.EventApplied, syncpkg.EventKeyIssue:
		m.syncBusy = false
		if len(ev.Result.AccountsApplied) > 0 {
			cmds = append(cmds, m.applySessionsCmd())
		}
		if ev.Result.KeymapUpdated {
			m.reloadKeymap()
		}
		cmds = append(cmds, m.loadPlaylistsCmd())
		for _, warning := range ev.Result.Warnings {
			m.setWarn(warning)
			break
		}
		if ev.Kind == syncpkg.EventKeyIssue && ev.Result.KeyIssue != nil {
			m.setWarn("同步跳过登录态：" + ev.Result.KeyIssue.Error())
		}
	case syncpkg.EventConflict:
		m.syncBusy = false
		m.setWarn(fmt.Sprintf("同步有 %d 个冲突待处理", ev.Status.Conflicts))
	case syncpkg.EventError:
		m.syncBusy = false
		if ev.Err != nil {
			m.setError(ev.Err)
		}
	case syncpkg.EventStatus:
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) applySessionsCmd() tea.Cmd {
	svc := m.service()
	return func() tea.Msg {
		return sessionsAppliedMsg{sources: svc.ApplyStoredSessions()}
	}
}

// reloadKeymap 在同步导入新键位后立即重载；失败时保留旧键位。
func (m *Model) reloadKeymap() {
	path, err := config.KeymapPath()
	if err != nil {
		return
	}
	km, warnings, err := keymap.LoadWithWarnings(path)
	if err != nil || km == nil {
		m.setWarn("同步的快捷键配置无效，继续使用旧键位")
		return
	}
	m.keys = km
	if len(warnings) > 0 {
		m.setWarn("快捷键配置部分降级：" + strings.Join(warnings, "；"))
	}
}

// syncStatusText 返回状态行右侧的同步摘要；未配置时不占位。
func (m *Model) syncStatusText() string {
	if m.syncMgr == nil {
		return ""
	}
	status := m.syncStatus
	if !status.Configured && status.ConflictTx == "" && status.LastError == "" {
		return ""
	}
	if m.syncBusy {
		return "同步中…"
	}
	return status.Summary()
}

// syncStatusLineVisible 判断右下角状态是否真的渲染出来了（被提示/载入覆盖时不可点击）。
func (m *Model) syncStatusLineVisible() bool {
	return m.syncStatusText() != "" && m.note.text == "" && !m.songsBusy && !m.searchBusy
}

// notifySyncIfChanged 用一个进程内计数器观察歌单/登录态变化，
// 变化后交给 Manager 做 debounce 推送。
func (m *Model) notifySyncIfChanged() {
	if m.syncMgr == nil {
		return
	}
	rev := app.LibraryRevision()
	if m.syncRev == 0 {
		m.syncRev = rev
		return
	}
	if rev == m.syncRev {
		return
	}
	m.syncRev = rev
	m.syncMgr.NotifyChange()
}

func padLines(lines []string, h int) []string {
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return lines
}
