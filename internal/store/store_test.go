package store

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestUpsertSongIsIdempotent(t *testing.T) {
	st := openTestStore(t)

	first, err := st.UpsertSong(Song{Source: "bilibili", BVID: "BV1xx411c7mD", Page: 1, Title: "旧标题", Artist: "UP"})
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	second, err := st.UpsertSong(Song{Source: "bilibili", BVID: "BV1xx411c7mD", Page: 1, Title: "新标题", Artist: "UP"})
	if err != nil {
		t.Fatalf("二次写入失败: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("同一 BV 同一分 P 应得到同一 ID: %s vs %s", first.ID, second.ID)
	}
	if second.Title != "新标题" {
		t.Fatalf("标题未更新: %q", second.Title)
	}

	// 不同分 P 是不同条目
	third, err := st.UpsertSong(Song{Source: "bilibili", BVID: "BV1xx411c7mD", Page: 2, Title: "P2"})
	if err != nil {
		t.Fatalf("写入 P2 失败: %v", err)
	}
	if third.ID == first.ID {
		t.Fatal("不同分 P 的 ID 不应相同")
	}
}

func TestPlaylistLifecycle(t *testing.T) {
	st := openTestStore(t)

	if _, err := st.EnsurePlaylist(Playlist{ID: "local-default", Title: "本地歌单", Kind: "local"}); err != nil {
		t.Fatalf("建歌单失败: %v", err)
	}
	songs := []Song{
		{Source: "bilibili", BVID: "BV1", Page: 1, Title: "A", Duration: 100},
		{Source: "bilibili", BVID: "BV2", Page: 1, Title: "B", Duration: 200},
	}
	for _, s := range songs {
		if err := st.AddSongToPlaylist("local-default", s); err != nil {
			t.Fatalf("加入歌单失败: %v", err)
		}
	}
	// 重复加入不应产生两条
	if err := st.AddSongToPlaylist("local-default", songs[0]); err != nil {
		t.Fatalf("重复加入失败: %v", err)
	}

	got, err := st.PlaylistSongs("local-default")
	if err != nil {
		t.Fatalf("读取歌单失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("期望 2 首，实际 %d", len(got))
	}

	p, err := st.Playlist("local-default")
	if err != nil {
		t.Fatalf("读取歌单元信息失败: %v", err)
	}
	if p.Count != 2 {
		t.Fatalf("歌单计数应为 2，实际 %d", p.Count)
	}

	if err := st.RemoveSongFromPlaylist("local-default", got[0].ID); err != nil {
		t.Fatalf("移除失败: %v", err)
	}
	got, _ = st.PlaylistSongs("local-default")
	if len(got) != 1 {
		t.Fatalf("移除后期望 1 首，实际 %d", len(got))
	}
}

func TestReplacePlaylistSongs(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.EnsurePlaylist(Playlist{ID: "fav", Title: "收藏夹", Kind: "remote", RemoteID: "123"}); err != nil {
		t.Fatalf("建歌单失败: %v", err)
	}
	first := []Song{{Source: "bilibili", BVID: "BV1", Page: 1, Title: "A"}}
	if err := st.ReplacePlaylistSongs("fav", first); err != nil {
		t.Fatalf("替换失败: %v", err)
	}
	second := []Song{
		{Source: "bilibili", BVID: "BV2", Page: 1, Title: "B"},
		{Source: "bilibili", BVID: "BV3", Page: 1, Title: "C"},
	}
	if err := st.ReplacePlaylistSongs("fav", second); err != nil {
		t.Fatalf("二次替换失败: %v", err)
	}
	got, _ := st.PlaylistSongs("fav")
	if len(got) != 2 || got[0].BVID != "BV2" || got[1].BVID != "BV3" {
		t.Fatalf("替换结果不符: %+v", got)
	}
}

func TestQueueRoundTrip(t *testing.T) {
	st := openTestStore(t)
	for _, bv := range []string{"BV1", "BV2", "BV3"} {
		if _, err := st.UpsertSong(Song{Source: "bilibili", BVID: bv, Page: 1, Title: bv}); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}
	ids := []string{SongID("bilibili", "BV2", 1), SongID("bilibili", "BV3", 1)}
	if err := st.SaveQueue(QueueState{IDs: ids, Index: 1, Position: 1234}); err != nil {
		t.Fatalf("保存队列失败: %v", err)
	}
	state, err := st.LoadQueue()
	if err != nil {
		t.Fatalf("读取队列失败: %v", err)
	}
	if len(state.IDs) != 2 || state.IDs[0] != ids[0] || state.Index != 1 || state.Position != 1234 {
		t.Fatalf("队列状态不符: %+v", state)
	}
	songs, err := st.SongsByIDs(state.IDs)
	if err != nil || len(songs) != 2 {
		t.Fatalf("按 ID 取歌失败: %v %+v", err, songs)
	}
}

func TestSettingsAndAccount(t *testing.T) {
	st := openTestStore(t)

	if got := st.Setting("volume", "0.8"); got != "0.8" {
		t.Fatalf("默认值未生效: %q", got)
	}
	if err := st.SetSetting("volume", "0.5"); err != nil {
		t.Fatalf("写设置失败: %v", err)
	}
	if got := st.Setting("volume", "0.8"); got != "0.5" {
		t.Fatalf("设置未持久化: %q", got)
	}

	if _, err := st.LoadAccount("bilibili"); err != ErrNotFound {
		t.Fatalf("未登录时应返回 ErrNotFound，实际 %v", err)
	}
	if err := st.SaveAccount("bilibili", []byte(`{"SESSDATA":"x"}`)); err != nil {
		t.Fatalf("保存登录态失败: %v", err)
	}
	payload, err := st.LoadAccount("bilibili")
	if err != nil || string(payload) != `{"SESSDATA":"x"}` {
		t.Fatalf("登录态读取不符: %v %q", err, payload)
	}
}
