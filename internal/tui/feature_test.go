package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

func openFeatureStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "feature.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestClipEditorSavesRange(t *testing.T) {
	st := openFeatureStore(t)
	m := fixtureModel()
	m.store = st
	m.duration = 213 * time.Second
	m.position = 42 * time.Second

	// 真实终端里的 Shift+C：Code 是小写 c，Text 是大写 C。
	updated, _ := m.Update(keyPress("C", 'c'))
	mm := updated.(*Model)
	if mm.view == viewQueue {
		t.Fatal("大写 C 不应打开队列")
	}
	if mm.clip == nil {
		t.Fatal("C 应打开片段编辑器")
	}
	updated, _ = mm.Update(keyPress("{", '{'))
	mm = updated.(*Model)
	mm.position = 120 * time.Second
	updated, _ = mm.Update(keyPress("}", '}'))
	mm = updated.(*Model)
	updated, _ = mm.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	mm = updated.(*Model)
	if mm.clip != nil {
		t.Fatal("enter 保存后应关闭编辑器")
	}
	clip, err := st.Clip(mm.current.ID)
	if err != nil {
		t.Fatalf("读取片段失败: %v", err)
	}
	if clip.StartMS != 42000 || clip.EndMS != 120000 {
		t.Fatalf("片段不符: %+v", clip)
	}
	if mm.position != 42*time.Second {
		t.Fatalf("保存后应从片段起点重播，实际 %v", mm.position)
	}
}

func TestStartStreamClampsToClipStart(t *testing.T) {
	st := openFeatureStore(t)
	song := store.Song{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "A", Duration: 213}
	if err := st.SetClip(song.ID, store.Clip{StartMS: 30000, EndMS: 60000}); err != nil {
		t.Fatalf("写入片段失败: %v", err)
	}
	m := fixtureModel()
	m.store = st
	m.startStream(song, source.Stream{Duration: 213 * time.Second}, 0)
	if m.position != 30*time.Second {
		t.Fatalf("无断点时应从片段起点起播，实际 %v", m.position)
	}
	if m.clipTag() == "" {
		t.Fatal("播放条应显示片段标记")
	}
}

func TestAddSongPickerWritesSelectedPlaylist(t *testing.T) {
	st := openFeatureStore(t)
	a, _ := st.CreatePlaylist("A")
	b, _ := st.CreatePlaylist("B")
	m := fixtureModel()
	m.store = st
	m.playlists = []store.Playlist{a, b}
	m.playlistIdx = 0
	m.songs = []store.Song{{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "A"}}
	m.songIdx = 0

	_ = m.addCurrentToPlaylist()
	if m.picker == nil {
		t.Fatal("a 应打开目标选择器")
	}
	idx := -1
	for i, item := range m.picker.items {
		if item.ID == b.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("目标选择器里应有 B: %+v", m.picker.items)
	}
	m.picker.idx = idx
	_ = m.chooseTarget()
	songs, err := st.PlaylistSongs(b.ID)
	if err != nil || len(songs) != 1 {
		t.Fatalf("歌曲应写入 B: %+v err=%v", songs, err)
	}
	if m.picker != nil {
		t.Fatal("加入后应关闭目标选择器")
	}
}

func TestMergePlaylistAddsReference(t *testing.T) {
	st := openFeatureStore(t)
	src, _ := st.CreatePlaylist("源")
	existing, _ := st.CreatePlaylist("已有")
	mixin, _ := st.CreateMixin("混合", []string{existing.ID})

	m := fixtureModel()
	m.store = st
	m.playlists, _ = st.ListPlaylists()
	m.playlistIdx = -1
	for i, p := range m.playlists {
		if p.ID == src.ID {
			m.playlistIdx = i
			break
		}
	}
	if m.playlistIdx < 0 {
		t.Fatal("找不到源歌单")
	}
	_ = m.openMergePicker()
	if m.picker == nil {
		t.Fatal("S 应打开混入目标选择器")
	}
	if len(m.picker.items) != 1 || m.picker.items[0].ID != mixin.ID {
		t.Fatalf("目标应只有混合歌单: %+v", m.picker.items)
	}
	m.picker.idx = 0
	_ = m.chooseTarget()
	refs, err := st.MixinRefs(mixin.ID)
	if err != nil || len(refs) != 2 {
		t.Fatalf("应追加源歌单引用: %+v err=%v", refs, err)
	}
}

func TestAddSongToCloudPlaylist(t *testing.T) {
	b, _, reg := twoFakes()
	st := openFeatureStore(t)
	m := fixtureModel()
	m.store = st
	m.sources = reg
	m.current = &store.Song{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "B1"}
	m.playlists = nil

	cmd := m.openAddSongPicker(*m.current)
	if cmd == nil {
		t.Fatal("登录且支持写入时应拉取云端歌单")
	}
	updated, _ := m.Update(cmd().(targetRemotesMsg))
	mm := updated.(*Model)
	idx := -1
	for i, item := range mm.picker.items {
		if item.Cloud && item.RemoteID == "1" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("云端歌单未出现在选择器: %+v", mm.picker.items)
	}
	mm.picker.idx = idx
	addCmd := mm.chooseTarget()
	if addCmd == nil {
		t.Fatal("选择云端歌单后应发起写入")
	}
	updated, _ = mm.Update(addCmd().(playlistAddMsg))
	_ = updated
	if b.addedTo != "1" || b.addedRef.ExternalID != "BV1" {
		t.Fatalf("B 站写入参数不符: remote=%q ref=%+v", b.addedTo, b.addedRef)
	}
}

func TestClipOverlayFrame(t *testing.T) {
	st := openFeatureStore(t)
	m := fixtureModel()
	m.store = st
	m.setActiveClip(m.current.ID, store.Clip{StartMS: 10000, EndMS: 90000})
	m.clip = &clipState{songID: m.current.ID, startMS: 10000, endMS: 90000}
	lines := assertFrame(t, m.View().Content, 100, 30)
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if !strings.Contains(body, "片段") || !strings.Contains(body, "起点") {
		t.Fatalf("片段浮层内容缺失: %q", body)
	}
}

func TestClipOverlayNarrowFrame(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 40, 12
	m.duration = 213 * time.Second
	m.position = 42 * time.Second
	m.setActiveClip(m.current.ID, store.Clip{StartMS: 10000, EndMS: 90000})
	m.clip = &clipState{songID: m.current.ID, startMS: 10000, endMS: 90000}
	assertFrame(t, m.View().Content, 40, 12)
}

func TestPickerOverlayNarrowFrame(t *testing.T) {
	m := fixtureModel()
	m.width, m.height = 40, 12
	m.picker = &targetPickerState{
		mode: targetPickerAddSong,
		song: *m.current,
		items: []playlistTarget{
			{ID: "a", Title: "本地歌单", Kind: "local"},
			{ID: "b", Title: "云端歌单", Kind: "remote", Cloud: true, Source: "bilibili", RemoteID: "1"},
			{Title: "＋ 新建本地歌单并加入", Create: true},
		},
	}
	assertFrame(t, m.View().Content, 40, 12)
}

func TestAKeyOpensPickerFromQueueAndLyrics(t *testing.T) {
	m := fixtureModel()
	m.view = viewQueue
	m.queue = []store.Song{{ID: "bilibili:BV2:1", Source: "bilibili", BVID: "BV2", Page: 1, Title: "B"}}
	m.queueCursor = 0
	updated, _ := m.Update(keyPress("a", 'a'))
	if updated.(*Model).picker == nil {
		t.Fatal("队列页按 a 应打开目标选择器")
	}

	m = fixtureModel()
	m.view = viewLyrics
	updated, _ = m.Update(keyPress("a", 'a'))
	if updated.(*Model).picker == nil {
		t.Fatal("歌词页按 a 应打开目标选择器")
	}
}

func TestSKeyOpensMergePicker(t *testing.T) {
	st := openFeatureStore(t)
	src, _ := st.CreatePlaylist("源")
	existing, _ := st.CreatePlaylist("已有")
	if _, err := st.CreateMixin("混合", []string{existing.ID}); err != nil {
		t.Fatalf("建混合歌单失败: %v", err)
	}
	m := fixtureModel()
	m.store = st
	m.playlists, _ = st.ListPlaylists()
	m.playlistIdx = 0
	for i, p := range m.playlists {
		if p.ID == src.ID {
			m.playlistIdx = i
		}
	}
	updated, _ := m.Update(keyPress("S", 'S'))
	mm := updated.(*Model)
	if mm.picker == nil || mm.picker.mode != targetPickerMerge {
		t.Fatalf("S 应打开混入选择器: %+v", mm.picker)
	}
}

func TestClipEditorCanSeekOutsideExistingRange(t *testing.T) {
	st := openFeatureStore(t)
	song := store.Song{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "A", Duration: 213}
	if err := st.SetClip(song.ID, store.Clip{StartMS: 30000, EndMS: 60000}); err != nil {
		t.Fatal(err)
	}
	m := fixtureModel()
	m.store = st
	m.current = &song
	m.duration = 213 * time.Second
	m.position = 42 * time.Second
	m.setActiveClip(song.ID, store.Clip{StartMS: 30000, EndMS: 60000})

	_ = m.openClipEditor()
	if m.clip == nil {
		t.Fatal("应打开片段编辑器")
	}
	m.seekBy(-20 * time.Second)
	if m.position != 22*time.Second {
		t.Fatalf("编辑时应能移动到旧区间外，实际 %v", m.position)
	}
	m.seekBy(60 * time.Second)
	if m.position != 82*time.Second {
		t.Fatalf("编辑时应能移动到旧终点外，实际 %v", m.position)
	}
}

func TestClipDefaultsFollowSongBounds(t *testing.T) {
	st := openFeatureStore(t)
	song := store.Song{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "A", Duration: 213}
	m := fixtureModel()
	m.store = st
	m.current = &song
	m.duration = 213 * time.Second
	m.position = 42 * time.Second
	_ = m.openClipEditor()

	// 只设起点：终点保持 0，保存后应表示「到原曲结尾」。
	m.clip.startMS = 42000
	m.clip.endMS = 0
	if cmd := m.saveClip(); cmd != nil {
		_ = cmd
	}
	clip, _ := st.Clip(song.ID)
	if clip.StartMS != 42000 || clip.EndMS != 0 {
		t.Fatalf("只设起点时应存成到结尾: %+v", clip)
	}

	// 在歌曲结尾按 } 也应回到「默认到结尾」的哨兵。
	m.position = 0
	_ = m.openClipEditor()
	m.position = 213 * time.Second
	m.setClipEnd()
	if m.clip.endMS != 0 {
		t.Fatalf("结尾设终点应为 0 哨兵，实际 %d", m.clip.endMS)
	}
	m.clip.startMS = 0
	if cmd := m.saveClip(); cmd != nil {
		_ = cmd
	}
	clip, _ = st.Clip(song.ID)
	if clip.Active() {
		t.Fatalf("两端都是默认值时应按整首播放: %+v", clip)
	}
}

func TestCancelClipEditorRestoresOriginal(t *testing.T) {
	st := openFeatureStore(t)
	song := store.Song{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "A", Duration: 213}
	if err := st.SetClip(song.ID, store.Clip{StartMS: 30000, EndMS: 60000}); err != nil {
		t.Fatal(err)
	}
	m := fixtureModel()
	m.store = st
	m.current = &song
	m.duration = 213 * time.Second
	m.position = 42 * time.Second
	_ = m.openClipEditor()
	m.position = 100 * time.Second
	m.clip.startMS = 10000
	m.clip.endMS = 120000

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	mm := updated.(*Model)
	clip, _ := st.Clip(song.ID)
	if clip.StartMS != 30000 || clip.EndMS != 60000 {
		t.Fatalf("取消不应修改已存片段: %+v", clip)
	}
	if mm.clip != nil {
		t.Fatal("取消后应关闭编辑器")
	}
	if mm.position != 30*time.Second {
		t.Fatalf("取消后位置应回到原片段内，实际 %v", mm.position)
	}
}

func TestStartStreamWhileEditingPreviewsFullSong(t *testing.T) {
	st := openFeatureStore(t)
	song := store.Song{ID: "bilibili:BV1:1", Source: "bilibili", BVID: "BV1", Page: 1, Title: "A", Duration: 213}
	if err := st.SetClip(song.ID, store.Clip{StartMS: 30000, EndMS: 60000}); err != nil {
		t.Fatal(err)
	}
	m := fixtureModel()
	m.store = st
	m.current = &song
	m.duration = 213 * time.Second
	m.position = 42 * time.Second
	_ = m.openClipEditor()
	m.startStream(song, source.Stream{Duration: 213 * time.Second}, 0)
	if m.clip == nil || !m.clip.previewing {
		t.Fatalf("编辑器应处于整首预览: %+v", m.clip)
	}
	if m.activeClip().Active() {
		t.Fatalf("预览期间不应再限制旧区间: %+v", m.activeClip())
	}
}

func TestClipOverlayShowsDefaultBounds(t *testing.T) {
	m := fixtureModel()
	m.duration = 213 * time.Second
	m.position = 42 * time.Second
	m.clip = &clipState{songID: m.current.ID, startMS: 0, endMS: 0}
	body := m.View().Content
	if !strings.Contains(body, "（默认）") || !strings.Contains(body, "原曲结尾") {
		t.Fatalf("未设边界时应显示默认说明：%q", body)
	}
}

func TestClipAllowsPosition(t *testing.T) {
	duration := 213 * time.Second
	clip := store.Clip{StartMS: 30000, EndMS: 60000}
	for target, want := range map[time.Duration]bool{
		10 * time.Second:  false,
		30 * time.Second:  true,
		45 * time.Second:  true,
		59 * time.Second:  true,
		60 * time.Second:  false,
		100 * time.Second: false,
	} {
		if got := clipAllowsPosition(target, clip, duration); got != want {
			t.Fatalf("clipAllowsPosition(%v) = %v，期望 %v", target, got, want)
		}
	}
	startOnly := store.Clip{StartMS: 42000}
	if !clipAllowsPosition(213*time.Second, startOnly, duration) || clipAllowsPosition(10*time.Second, startOnly, duration) {
		t.Fatal("只有起点时应允许到结尾，起点前不允许")
	}
	if !clipAllowsPosition(10*time.Second, store.Clip{}, duration) {
		t.Fatal("没有区间时整首都允许")
	}
}
