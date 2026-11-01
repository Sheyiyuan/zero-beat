package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	syncpkg "github.com/sheyiyuan/zero-beat/internal/sync"
)

func testSyncManager(t *testing.T, remote string) *syncpkg.Manager {
	t.Helper()
	dir := t.TempDir()
	opts := syncpkg.Options{
		Dir:        filepath.Join(dir, "sync"),
		ConfigPath: filepath.Join(dir, "sync.toml"),
		KeyPath:    filepath.Join(dir, "sync-key"),
		TokenPath:  filepath.Join(dir, "sync-token"),
		KeymapPath: filepath.Join(dir, "keymap.toml"),
	}
	if remote != "" {
		if err := syncpkg.SaveConfig(opts.ConfigPath, syncpkg.Config{Remote: remote, Branch: "main", Auto: true}); err != nil {
			t.Fatal(err)
		}
	}
	return syncpkg.NewManager(syncpkg.NewClient(opts), syncpkg.ManagerOptions{})
}

func TestRenderSyncPage(t *testing.T) {
	m := fixtureModel()
	mgr := testSyncManager(t, "https://example.com/zero.git")
	defer mgr.Close()
	m.syncMgr = mgr
	m.syncStatus = mgr.Status()
	m.view = viewSync

	frame := m.View().Content
	lines := assertFrame(t, frame, 100, 30)
	body := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"同步", "立即同步", "仓库地址", "自动同步", "配置 HTTPS 令牌"} {
		if !strings.Contains(body, want) {
			t.Fatalf("同步页应包含 %q，实际：\n%s", want, body)
		}
	}
	if strings.Contains(body, "0000") {
		t.Fatalf("同步页不应显示密钥内容：\n%s", body)
	}
}

func TestRenderSyncPageUnconfigured(t *testing.T) {
	m := fixtureModel()
	mgr := testSyncManager(t, "")
	defer mgr.Close()
	m.syncMgr = mgr
	m.syncStatus = mgr.Status()
	m.view = viewSync
	body := ansi.Strip(m.View().Content)
	for _, want := range []string{"尚未配置", "明文", "初始化"} {
		if !strings.Contains(body, want) {
			t.Fatalf("未配置时应包含 %q，实际：\n%s", want, body)
		}
	}
	// 第一行是明文确认，space 与 enter 都能切换。
	updated, _ := m.Update(keyPress(" ", tea.KeySpace))
	m = updated.(*Model)
	if !m.syncAck {
		t.Fatal("space 应勾选明文提示")
	}
}

func TestSyncPageEditingRemote(t *testing.T) {
	m := fixtureModel()
	mgr := testSyncManager(t, "")
	defer mgr.Close()
	m.syncMgr = mgr
	m.syncStatus = mgr.Status()
	m.view = viewSync

	updated, _ := m.Update(keyPress("", tea.KeyDown)) // 第一行是明文确认
	m = updated.(*Model)
	updated, _ = m.Update(keyPress("", tea.KeyEnter))
	m = updated.(*Model)
	if m.syncEdit != "remote" {
		t.Fatalf("应进入仓库地址编辑，实际 %q", m.syncEdit)
	}
	for _, r := range "git@example.com:me/zero.git" {
		updated, _ = m.Update(keyPress(string(r), r))
		m = updated.(*Model)
	}
	updated, cmd := m.Update(keyPress("", tea.KeyEnter))
	m = updated.(*Model)
	if m.syncEdit != "" {
		t.Fatal("回车后应退出编辑")
	}
	if m.syncInput != "" {
		t.Fatalf("提交后应清空输入缓冲，实际 %q", m.syncInput)
	}
	if cmd == nil {
		t.Fatal("提交仓库地址应返回保存命令")
	}
	msg := cmd()
	if _, ok := msg.(syncResultMsg); !ok {
		t.Fatalf("保存配置应返回 syncResultMsg，实际 %T", msg)
	}
	if got := mgr.Status().Remote; got != "git@example.com:me/zero.git" {
		t.Fatalf("仓库地址未保存，实际 %q", got)
	}
}

func TestRenderConflictsPage(t *testing.T) {
	m := fixtureModel()
	m.view = viewConflicts
	m.syncConflicts = syncpkg.ConflictIndex{
		TxID: "20260101-120000-abcd",
		Entities: []syncpkg.ConflictEntity{
			{
				Index: 0, Entity: "playlist:local-1", Kind: syncpkg.KindPlaylist, ID: "local-1",
				Path: "playlists/local-1.json", Local: true, Remote: true,
				Title: "通勤", LocalTitle: "通勤", RemoteTitle: "通勤（远端）",
				LocalCount: 3, RemoteCount: 4, LocalUpdated: 1761980000, RemoteUpdated: 1761980100,
			},
			{
				Index: 1, Entity: "keymap", Kind: syncpkg.KindKeymap,
				Path: "keymap.toml", Local: true, Remote: true, LocalCount: 2, RemoteCount: 3,
			},
		},
	}
	m.syncChoices = map[string]syncpkg.Choice{}

	lines := assertFrame(t, m.View().Content, 100, 30)
	body := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"冲突事务", "歌单「通勤」", "快捷键配置", "未选择", "还有 2 项未选择"} {
		if !strings.Contains(body, want) {
			t.Fatalf("冲突页应包含 %q，实际：\n%s", want, body)
		}
	}

	updated, _ := m.Update(keyPress("l", 'l'))
	m = updated.(*Model)
	if m.syncChoices["playlist:local-1"] != syncpkg.KeepLocal {
		t.Fatal("l 应选择保留本地")
	}
	updated, _ = m.Update(keyPress("", tea.KeyDown))
	m = updated.(*Model)
	updated, _ = m.Update(keyPress("r", 'r'))
	m = updated.(*Model)
	if m.syncChoices["keymap"] != syncpkg.KeepRemote {
		t.Fatal("r 应选择保留远程")
	}
	if !m.conflictsComplete() {
		t.Fatal("两项都选完后应可应用")
	}
	// b 在冲突页是「保留两者」，不能被默认返回键吃掉。
	updated, _ = m.Update(keyPress("", tea.KeyUp))
	m = updated.(*Model)
	updated, _ = m.Update(keyPress("b", 'b'))
	m = updated.(*Model)
	if m.syncChoices["playlist:local-1"] != syncpkg.KeepBoth {
		t.Fatalf("b 应选择保留两者，实际 %v（view=%v）", m.syncChoices, viewNames[m.view])
	}
}

func TestSyncStatusLineAndClick(t *testing.T) {
	m := fixtureModel()
	mgr := testSyncManager(t, "https://example.com/zero.git")
	defer mgr.Close()
	m.syncMgr = mgr
	// 伪造一个已同步状态，避免依赖真实同步。
	m.syncStatus = mgr.Status()
	m.syncStatus.LastSyncAt = time.Now()
	m.syncStatus.KeyCached = true

	frame := m.View().Content
	body := ansi.Strip(frame)
	if !strings.Contains(body, "同步") {
		t.Fatalf("状态行应显示同步摘要：\n%s", body)
	}
	zones := m.hitZones()
	found := false
	for _, zone := range zones {
		if zone.target == hitSyncStatus {
			found = true
		}
	}
	if !found {
		t.Fatal("状态行应有可点击命中区")
	}
	updated, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: 99, Y: 29, Button: tea.MouseLeft}))
	mm := updated.(*Model)
	if mm.view != viewSync {
		t.Fatalf("点击状态行应打开同步页，实际 %v", viewNames[mm.view])
	}
	_ = cmd
}

func TestSyncPageNarrowFrame(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 40, 12
	mgr := testSyncManager(t, "https://example.com/zero.git")
	defer mgr.Close()
	m.syncMgr = mgr
	m.syncStatus = mgr.Status()
	m.view = viewSync
	assertFrame(t, m.View().Content, 40, 12)

	m.view = viewConflicts
	m.syncConflicts = syncpkg.ConflictIndex{Entities: []syncpkg.ConflictEntity{
		{Index: 0, Entity: "playlist:local-1", Kind: syncpkg.KindPlaylist, ID: "local-1",
			Path: "playlists/local-1.json", Local: true, Remote: true, Title: "通勤"},
	}}
	assertFrame(t, m.View().Content, 40, 12)
}

func TestSyncPassphraseIsMasked(t *testing.T) {
	m := fixtureModel()
	mgr := testSyncManager(t, "https://example.com/zero.git")
	defer mgr.Close()
	m.syncMgr = mgr
	m.syncStatus = mgr.Status()
	m.view = viewSync
	m.syncEdit = "pass"
	m.syncInput = "super-secret-passphrase"
	m.syncPass = "super-secret-passphrase"
	body := ansi.Strip(m.View().Content)
	if strings.Contains(body, "super-secret-passphrase") {
		t.Fatalf("口令不应明文渲染：\n%s", body)
	}
	if !strings.Contains(body, "*") {
		t.Fatalf("口令输入应显示掩码：\n%s", body)
	}
}

func TestSyncPageSwitchesConnectionMode(t *testing.T) {
	dir := t.TempDir()
	opts := syncpkg.Options{
		Dir:        filepath.Join(dir, "sync"),
		ConfigPath: filepath.Join(dir, "sync.toml"),
		KeyPath:    filepath.Join(dir, "sync-key"),
		TokenPath:  filepath.Join(dir, "sync-token"),
		KeymapPath: filepath.Join(dir, "keymap.toml"),
	}
	httpsRemote := "https://github.com/me/zero-beat-sync.git"
	if err := syncpkg.SaveConfig(opts.ConfigPath, syncpkg.Config{Remote: httpsRemote, Branch: "main", Auto: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := syncpkg.NewMeta("a long enough passphrase", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	key.RemoteHash = syncpkg.RemoteHash(httpsRemote)
	if err := key.Save(opts.KeyPath); err != nil {
		t.Fatal(err)
	}
	mgr := syncpkg.NewManager(syncpkg.NewClient(opts), syncpkg.ManagerOptions{})
	defer mgr.Close()

	m := fixtureModel()
	m.syncMgr = mgr
	m.syncStatus = mgr.Status()
	m.view = viewSync
	if !m.syncStatus.KeyMatchesRepo {
		t.Fatal("初始密钥应匹配仓库")
	}
	rows := m.syncRows()
	idx := -1
	for i, row := range rows {
		if row.action == syncActMode {
			idx = i
			if !strings.Contains(row.label, "HTTPS") {
				t.Fatalf("模式行应显示 HTTPS，实际 %q", row.label)
			}
		}
	}
	if idx < 0 {
		t.Fatal("同步页应有连接方式行")
	}
	m.syncCursor = idx
	updated, cmd := m.Update(keyPress("", tea.KeyEnter))
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("切换连接方式应返回保存命令")
	}
	msg := cmd()
	if res, ok := msg.(syncResultMsg); !ok || res.err != nil {
		t.Fatalf("切换失败: %#v", msg)
	}
	status := mgr.Status()
	if status.Remote != "git@github.com:me/zero-beat-sync.git" {
		t.Fatalf("地址应改为 SSH，实际 %q", status.Remote)
	}
	if status.Auth != "SSH" {
		t.Fatalf("认证方式应显示 SSH，实际 %q", status.Auth)
	}
	if !status.KeyMatchesRepo {
		t.Fatal("切换 SSH/HTTPS 不应让本机密钥失效")
	}
}

func TestSyncPageAcceptsPaste(t *testing.T) {
	m := fixtureModel()
	mgr := testSyncManager(t, "https://example.com/zero.git")
	defer mgr.Close()
	m.syncMgr = mgr
	m.syncStatus = mgr.Status()
	m.view = viewSync

	m.syncEdit = "remote"
	m.syncInput = ""
	updated, _ := m.Update(tea.PasteMsg{Content: "https://github.com/me/zero.git\n"})
	m = updated.(*Model)
	if m.syncInput != "https://github.com/me/zero.git" {
		t.Fatalf("仓库地址应接受粘贴，实际 %q", m.syncInput)
	}

	// 口令保留内部空格，不按普通文本框折叠。
	m.syncEdit = "pass"
	m.syncInput = ""
	updated, _ = m.Update(tea.PasteMsg{Content: "two  spaces\n"})
	m = updated.(*Model)
	if m.syncInput != "two  spaces" {
		t.Fatalf("口令粘贴应保留空格，实际 %q", m.syncInput)
	}

	// 没进入编辑态时，粘贴不应写进任何缓冲。
	m.syncEdit = ""
	m.syncInput = ""
	updated, _ = m.Update(tea.PasteMsg{Content: "https://ignored.example/x"})
	m = updated.(*Model)
	if m.syncInput != "" {
		t.Fatalf("非编辑态不应接受粘贴，实际 %q", m.syncInput)
	}
}

func TestSyncPassphraseValidationKeepsEditing(t *testing.T) {
	m := fixtureModel()
	mgr := testSyncManager(t, "https://example.com/zero.git")
	defer mgr.Close()
	m.syncMgr = mgr
	m.syncStatus = mgr.Status()
	m.view = viewSync
	m.syncEdit = "pass"
	m.syncInput = "short"

	updated, _ := m.Update(keyPress("", tea.KeyEnter))
	m = updated.(*Model)
	if m.syncEdit != "pass" || m.syncInput != "short" {
		t.Fatalf("口令过短时应保留编辑态，实际 edit=%q input=%q", m.syncEdit, m.syncInput)
	}
	if m.syncPass != "" {
		t.Fatal("过短口令不应被接受")
	}
	if !strings.Contains(m.note.text, "12") {
		t.Fatalf("应提示最少 12 个字符，实际 %q", m.note.text)
	}

	// 继续补足长度后回车，口令按原样保留（包括尾随空格）。
	m.syncInput = "long enough "
	updated, _ = m.Update(keyPress("", tea.KeyEnter))
	m = updated.(*Model)
	if m.syncPass != "long enough " {
		t.Fatalf("口令应原样保存，实际 %q", m.syncPass)
	}
	if m.syncEdit != "" || m.syncInput != "" {
		t.Fatal("成功后应退出编辑并清空缓冲")
	}
}

func TestSyncInitFocusesMissingPrerequisite(t *testing.T) {
	m := fixtureModel()
	mgr := testSyncManager(t, "")
	defer mgr.Close()
	m.syncMgr = mgr
	m.syncStatus = mgr.Status()
	m.view = viewSync
	m.syncAck = false
	m.syncPass = "long enough pass"
	m.syncCursor = 0
	// 找到初始化行并回车。
	for i, row := range m.syncRows() {
		if row.action == syncActInit {
			m.syncCursor = i
		}
	}
	updated, _ := m.Update(keyPress("", tea.KeyEnter))
	m = updated.(*Model)
	if m.syncAck {
		t.Fatal("未确认明文提示时不应继续初始化")
	}
	rows := m.syncRows()
	if m.syncCursor >= len(rows) || rows[m.syncCursor].action != syncActAck {
		t.Fatalf("应把光标移到明文确认行，实际 cursor=%d", m.syncCursor)
	}
}

func TestSyncPageTypeToEdit(t *testing.T) {
	m := fixtureModel()
	mgr := testSyncManager(t, "https://example.com/zero.git")
	defer mgr.Close()
	m.syncMgr = mgr
	m.syncStatus = mgr.Status()
	m.view = viewSync
	rows := m.syncRows()
	passIdx := -1
	for i, row := range rows {
		if row.action == syncActPass {
			passIdx = i
		}
	}
	if passIdx < 0 {
		t.Fatal("应有同步口令行")
	}
	m.syncCursor = passIdx
	// 不先按 enter，直接打字应进入编辑并保留已输入的字符。
	updated, _ := m.Update(keyPress("a", 'a'))
	m = updated.(*Model)
	if m.syncEdit != "pass" || m.syncInput != "a" {
		t.Fatalf("直接打字应进入口令编辑，实际 edit=%q input=%q", m.syncEdit, m.syncInput)
	}
	updated, _ = m.Update(keyPress("b", 'b'))
	m = updated.(*Model)
	if m.syncInput != "ab" {
		t.Fatalf("后续字符应继续追加，实际 %q", m.syncInput)
	}
	// ctrl+a 是快捷键，不应往口令里插一个 a。
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a", Mod: tea.ModCtrl}))
	m = updated.(*Model)
	if m.syncInput != "ab" {
		t.Fatalf("组合键不应写入输入框，实际 %q", m.syncInput)
	}

	// j/k 仍是上下移动，不应被当作输入。
	m.syncEdit = ""
	m.syncInput = ""
	m.syncCursor = passIdx
	updated, _ = m.Update(keyPress("j", 'j'))
	m = updated.(*Model)
	if m.syncEdit != "" {
		t.Fatal("j 不应进入编辑态")
	}
	if m.syncCursor == passIdx {
		t.Fatal("j 应下移光标")
	}
}

func TestSyncPassphraseAcceptsNumpadEnter(t *testing.T) {
	m := fixtureModel()
	mgr := testSyncManager(t, "https://example.com/zero.git")
	defer mgr.Close()
	m.syncMgr = mgr
	m.syncStatus = mgr.Status()
	m.view = viewSync
	m.syncEdit = "pass"
	m.syncInput = "abcdefghijkl"
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyKpEnter}))
	m = updated.(*Model)
	if m.syncPass != "abcdefghijkl" || m.syncEdit != "" {
		t.Fatalf("小键盘回车也应确认口令，实际 pass=%q edit=%q", m.syncPass, m.syncEdit)
	}
}
