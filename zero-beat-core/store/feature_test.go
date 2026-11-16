package store

import (
	"testing"
	"time"
)

func TestClipRoundTrip(t *testing.T) {
	st := openTestStore(t)
	id := "bilibili:BV1:1"

	got, err := st.Clip(id)
	if err != nil || got.Active() {
		t.Fatalf("初始不应有片段: %+v err=%v", got, err)
	}
	if err := st.SetClip(id, Clip{StartMS: 12340, EndMS: 234560}); err != nil {
		t.Fatalf("写入片段失败: %v", err)
	}
	got, err = st.Clip(id)
	if err != nil {
		t.Fatalf("读取片段失败: %v", err)
	}
	if got.StartMS != 12340 || got.EndMS != 234560 {
		t.Fatalf("片段不符: %+v", got)
	}
	if err := st.SetClip(id, Clip{}); err != nil {
		t.Fatalf("清空片段失败: %v", err)
	}
	got, _ = st.Clip(id)
	if got.Active() {
		t.Fatalf("清空后不应还有片段: %+v", got)
	}
}

func TestClipRejectsInvalidRange(t *testing.T) {
	st := openTestStore(t)
	if err := st.SetClip("song", Clip{StartMS: 1000, EndMS: 1000}); err == nil {
		t.Fatal("终点等于起点应报错")
	}
	if err := st.SetClip("song", Clip{StartMS: 2000, EndMS: 1000}); err == nil {
		t.Fatal("终点小于起点应报错")
	}
}

func TestAddMixinRef(t *testing.T) {
	st := openTestStore(t)
	source, err := st.CreatePlaylist("源")
	if err != nil {
		t.Fatalf("建源歌单失败: %v", err)
	}
	existing, err := st.CreatePlaylist("已有")
	if err != nil {
		t.Fatalf("建引用歌单失败: %v", err)
	}
	mixin, err := st.CreateMixin("混合", []string{existing.ID})
	if err != nil {
		t.Fatalf("建混合歌单失败: %v", err)
	}
	added, err := st.AddMixinRef(mixin.ID, source.ID)
	if err != nil || !added {
		t.Fatalf("追加引用失败: added=%v err=%v", added, err)
	}
	added, err = st.AddMixinRef(mixin.ID, source.ID)
	if err != nil || added {
		t.Fatalf("重复引用应幂等: added=%v err=%v", added, err)
	}
	refs, err := st.MixinRefs(mixin.ID)
	if err != nil || len(refs) != 2 {
		t.Fatalf("引用数量不符: %+v err=%v", refs, err)
	}
	if _, err := st.AddMixinRef(mixin.ID, mixin.ID); err == nil {
		t.Fatal("混合歌单不能引用自己")
	}
	local, _ := st.CreatePlaylist("本地")
	if _, err := st.AddMixinRef(local.ID, source.ID); err == nil {
		t.Fatal("本地歌单不能添加引用")
	}
	nested, _ := st.CreateMixin("嵌套", []string{source.ID})
	if _, err := st.AddMixinRef(mixin.ID, nested.ID); err == nil {
		t.Fatal("混合歌单不能引用另一个混合歌单")
	}
}

func TestClipSurvivesUpsert(t *testing.T) {
	st := openTestStore(t)
	song, err := st.UpsertSong(Song{Source: "bilibili", BVID: "BV1", Page: 1, Title: "A", Duration: 200})
	if err != nil {
		t.Fatalf("写入歌曲失败: %v", err)
	}
	if err := st.SetClip(song.ID, Clip{StartMS: 1000, EndMS: 5000}); err != nil {
		t.Fatalf("写入片段失败: %v", err)
	}
	if _, err := st.UpsertSong(Song{Source: "bilibili", BVID: "BV1", Page: 1, Title: "B", Duration: 201, AddedAt: time.Now()}); err != nil {
		t.Fatalf("二次写入歌曲失败: %v", err)
	}
	got, _ := st.Clip(song.ID)
	if got.StartMS != 1000 || got.EndMS != 5000 {
		t.Fatalf("重新 Upsert 不应丢掉片段: %+v", got)
	}
}
