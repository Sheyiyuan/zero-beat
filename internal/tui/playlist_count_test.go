package tui

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// TestRefreshSyncsSidebarPlaylistCount 锁住这个回归：按 r 刷新远端歌单后曲目数变了，
// 左侧歌单栏的数字要立刻跟着变，不能等下一次全量加载歌单（新建/加入歌单）才更新。
func TestRefreshSyncsSidebarPlaylistCount(t *testing.T) {
	b, _, reg := twoFakes()
	st, err := store.Open(filepath.Join(t.TempDir(), "sidebar-count.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.EnsurePlaylist(store.Playlist{
		ID: "bili-fav-1", Source: "bilibili", Title: "B 收藏", Kind: "remote", RemoteID: "1",
	}); err != nil {
		t.Fatalf("建远端歌单失败: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := st.AddSongToPlaylist("bili-fav-1", store.Song{
			Source: "bilibili", RemoteID: fmt.Sprintf("BVold%d", i), Title: fmt.Sprintf("缓存 %d", i),
		}); err != nil {
			t.Fatalf("写入缓存曲目失败: %v", err)
		}
	}

	m := fixtureModel()
	m.sources = reg
	m.store = st
	updated, _ := m.Update(m.loadPlaylistsCmd()())
	mm := updated.(*Model)
	if got := mm.playlists[0].Count; got != 3 {
		t.Fatalf("启动时侧栏计数应为缓存里的 3，实际 %d", got)
	}

	// 远端现在只剩两首：按 r 强制刷新后，侧栏要立刻显示 2。
	b.tracks = []source.Track{
		{Source: source.Bilibili, ExternalID: "BV1", Page: 1, Title: "新歌 1", Duration: 100},
		{Source: source.Bilibili, ExternalID: "BV2", Page: 1, Title: "新歌 2", Duration: 120},
	}
	updated, cmd := mm.Update(keyPress("r", 'r'))
	mm = updated.(*Model)
	if cmd == nil {
		t.Fatal("r 应返回刷新命令")
	}
	updated, _ = mm.Update(cmd())
	mm = updated.(*Model)
	if len(mm.songs) != 2 {
		t.Fatalf("刷新后应载入 2 首，实际 %d", len(mm.songs))
	}
	if got := mm.playlists[0].Count; got != 2 {
		t.Fatalf("刷新后侧栏计数应为 2，实际 %d", got)
	}

	// 移除歌曲同样会让曲目数变化，侧栏计数也要立刻跟上。
	updated, cmd = mm.Update(keyPress("d", 'd'))
	mm = updated.(*Model)
	if cmd == nil {
		t.Fatal("d 应返回重新载入命令")
	}
	updated, _ = mm.Update(cmd())
	mm = updated.(*Model)
	if len(mm.songs) != 1 {
		t.Fatalf("移除后应剩 1 首，实际 %d", len(mm.songs))
	}
	if got := mm.playlists[0].Count; got != 1 {
		t.Fatalf("移除后侧栏计数应为 1，实际 %d", got)
	}
}
