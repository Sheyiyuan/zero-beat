package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/lrclib"
	"github.com/sheyiyuan/zero-beat/internal/lyrics"
	"github.com/sheyiyuan/zero-beat/internal/source"
	localsource "github.com/sheyiyuan/zero-beat/internal/source/local"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// lyricFake 按 ref 返回不同歌词，用来区分「自动来源」和「绑定来源」。
type lyricFake struct {
	*baseFake
	byRef map[string][]lyrics.Line
}

func (f *lyricFake) Lyrics(_ context.Context, ref source.Ref) ([]lyrics.Line, string, error) {
	if lines, ok := f.byRef[ref.ExternalID]; ok {
		return lines, "测试歌词", nil
	}
	return nil, "", nil
}

func lyricFixture(t *testing.T) (*store.Store, *lyricFake, store.Song) {
	t.Helper()
	st := openTestStore(t)
	fake := &lyricFake{
		baseFake: &baseFake{id: "fake", caps: source.CapOfficialLyrics, logged: true},
		byRef: map[string][]lyrics.Line{
			"1": {{StartMS: 1000, Text: "自动来源"}},
			"2": {{StartMS: 2000, Text: "绑定来源"}},
		},
	}
	song := store.NormalizeSong(store.Song{Source: "fake", RemoteID: "1", Page: 1, Title: "晴天", Artist: "周杰伦", Duration: 200})
	if _, err := st.UpsertSong(song); err != nil {
		t.Fatal(err)
	}
	return st, fake, song
}

func TestLyricsUsesAutoSource(t *testing.T) {
	st, fake, song := lyricFixture(t)
	svc := New(source.NewRegistry(fake), st, nil)
	res := svc.Lyrics(context.Background(), song)
	if len(res.Lines) != 1 || res.Lines[0].Text != "自动来源" {
		t.Fatalf("应使用平台自动歌词: %+v", res)
	}
	if res.OffsetKey != store.BindingAuto || res.Bound || res.Degraded {
		t.Fatalf("自动来源状态不符: %+v", res)
	}
}

func TestLyricsFallsBackToLRCLIB(t *testing.T) {
	st, fake, song := lyricFixture(t)
	fake.byRef = map[string][]lyrics.Line{} // 平台没有歌词
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			t.Errorf("意外请求 %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": 7, "trackName": "晴天", "artistName": "周杰伦", "duration": 200,
			"syncedLyrics": "[00:01.00]LRCLIB 兜底",
		}})
	}))
	defer srv.Close()
	svc := New(source.NewRegistry(fake), st, lrclib.NewWithBaseURL(srv.URL))
	res := svc.Lyrics(context.Background(), song)
	if len(res.Lines) != 1 || res.Lines[0].Text != "LRCLIB 兜底" || res.Source != "LRCLIB" {
		t.Fatalf("应回退到 LRCLIB: %+v", res)
	}
}

func TestLyricsPortableBindingWinsAndKeepsOwnOffset(t *testing.T) {
	st, fake, song := lyricFixture(t)
	binding := store.LyricBinding{Kind: store.BindingKindSource, Source: "fake", ExternalID: "2", Page: 1, Title: "晴天"}
	if err := st.SaveLyricBinding(song.ID, binding); err != nil {
		t.Fatal(err)
	}
	if err := st.SetLyricOffset(song.ID, store.BindingPortable, 400); err != nil {
		t.Fatal(err)
	}
	svc := New(source.NewRegistry(fake), st, nil)
	res := svc.Lyrics(context.Background(), song)
	if len(res.Lines) != 1 || res.Lines[0].Text != "绑定来源" {
		t.Fatalf("绑定应优先于自动来源: %+v", res)
	}
	if !res.Bound || res.OffsetKey != store.BindingPortable || res.OffsetMS != 400 {
		t.Fatalf("绑定状态/来源不符: %+v", res)
	}
	if !strings.HasPrefix(res.Source, "绑定 · ") {
		t.Fatalf("绑定来源标签不符: %q", res.Source)
	}
}

func TestLyricsLocalFileBindingWinsOverPortable(t *testing.T) {
	st, fake, song := lyricFixture(t)
	if err := st.SaveLyricBinding(song.ID, store.LyricBinding{Kind: store.BindingKindSource, Source: "fake", ExternalID: "2", Page: 1}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "x.lrc")
	if err := os.WriteFile(path, []byte("[00:01.00]本机歌词\n[00:02.00]第二句"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveLyricBinding(song.ID, store.LyricBinding{Kind: store.BindingKindLocalFile, Path: path}); err != nil {
		t.Fatal(err)
	}
	svc := New(source.NewRegistry(fake), st, nil)
	res := svc.Lyrics(context.Background(), song)
	if len(res.Lines) != 2 || res.Lines[0].Text != "本机歌词" {
		t.Fatalf("本机绑定应优先: %+v", res)
	}
	if res.OffsetKey != store.BindingLocal || res.Source != "绑定 · 本地 LRC" {
		t.Fatalf("本机绑定来源不符: %+v", res)
	}
}

func TestLyricsBrokenBindingDegradesToAuto(t *testing.T) {
	st, fake, song := lyricFixture(t)
	if err := st.SaveLyricBinding(song.ID, store.LyricBinding{Kind: store.BindingKindLocalFile, Path: "/no/such/file.lrc"}); err != nil {
		t.Fatal(err)
	}
	svc := New(source.NewRegistry(fake), st, nil)
	res := svc.Lyrics(context.Background(), song)
	if !res.Degraded || res.Warning == "" {
		t.Fatalf("绑定失效应标记降级并给出提示: %+v", res)
	}
	if len(res.Lines) != 1 || res.Lines[0].Text != "自动来源" {
		t.Fatalf("降级后应使用自动来源: %+v", res)
	}
	if res.OffsetKey != store.BindingAuto {
		t.Fatalf("降级后 offset 应归 auto: %s", res.OffsetKey)
	}
	// 失效绑定不会被自动清除，用户修好文件后还能恢复。
	if _, scope, _ := st.LyricBinding(song.ID); scope != store.BindingLocal {
		t.Fatal("降级不应清除绑定")
	}
}

func TestLyricsLRCLIBBinding(t *testing.T) {
	st, fake, song := lyricFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/get/9" {
			t.Errorf("意外请求 %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 9, "trackName": "晴天", "artistName": "周杰伦",
			"syncedLyrics": "[00:03.00]LRCLIB 绑定",
		})
	}))
	defer srv.Close()
	if err := st.SaveLyricBinding(song.ID, store.LyricBinding{Kind: store.BindingKindLRCLIB, LRCLIBID: 9}); err != nil {
		t.Fatal(err)
	}
	svc := New(source.NewRegistry(fake), st, lrclib.NewWithBaseURL(srv.URL))
	res := svc.Lyrics(context.Background(), song)
	if len(res.Lines) != 1 || res.Lines[0].Text != "LRCLIB 绑定" || res.OffsetKey != store.BindingPortable {
		t.Fatalf("LRCLIB 绑定未生效: %+v", res)
	}
}

func TestSaveLyricOffsetRoutesByScope(t *testing.T) {
	st, _, song := lyricFixture(t)
	binding := store.LyricBinding{Kind: store.BindingKindSource, Source: "fake", ExternalID: "2", Page: 1}
	if err := st.SaveLyricBinding(song.ID, binding); err != nil {
		t.Fatal(err)
	}
	svc := New(nil, st, nil)
	svc.SaveLyricOffset(song.ID, store.BindingPortable, 500)
	if got, _ := st.PortableLyricBinding(song.ID); got.OffsetMS != 500 {
		t.Fatalf("portable offset 未写进绑定: %+v", got)
	}
	if got := st.LyricOffset(song.ID, store.BindingAuto); got != 0 {
		t.Fatalf("portable offset 不应写 auto: %d", got)
	}
	svc.SaveLyricOffset(song.ID, store.BindingAuto, -250)
	if got := st.LyricOffset(song.ID, store.BindingAuto); got != -250 {
		t.Fatalf("auto offset 不符: %d", got)
	}
}

func TestPreviewRejectsUntimedLyrics(t *testing.T) {
	st, fake, song := lyricFixture(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.lrc")
	if err := os.WriteFile(path, []byte("没有时间轴的一行歌词\n第二行"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New(source.NewRegistry(fake), st, nil)
	cand := LyricCandidate{Kind: store.BindingKindLocalFile, Path: path}
	if _, err := svc.PreviewLyricBinding(context.Background(), song, cand); err == nil {
		t.Fatal("没有时间轴的歌词不应通过预览")
	}
	if _, _, err := svc.BindLyrics(context.Background(), song, cand); err == nil {
		t.Fatal("没有时间轴的歌词不应落库")
	}
	if _, scope, _ := st.LyricBinding(song.ID); scope != store.BindingAuto {
		t.Fatal("失败预览不应写入绑定")
	}
}

func TestBindLyricsPreviewsThenSaves(t *testing.T) {
	st, fake, song := lyricFixture(t)
	svc := New(source.NewRegistry(fake), st, nil)
	cand := LyricCandidate{Kind: store.BindingKindSource, Source: "fake", ExternalID: "2", Page: 1, Title: "晴天", Artist: "周杰伦"}
	preview, err := svc.PreviewLyricBinding(context.Background(), song, cand)
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if preview.Count != 1 || len(preview.Lines) != 1 {
		t.Fatalf("预览行数不符: %+v", preview)
	}
	binding, cleared, err := svc.BindLyrics(context.Background(), song, cand)
	if err != nil || cleared {
		t.Fatalf("绑定失败: %v cleared=%v", err, cleared)
	}
	if binding.Kind != store.BindingKindSource || binding.OffsetMS != 0 || binding.BoundAt == 0 {
		t.Fatalf("绑定字段不符: %+v", binding)
	}
	saved, scope, _ := st.LyricBinding(song.ID)
	if scope != store.BindingPortable || saved.ExternalID != "2" {
		t.Fatalf("绑定未落库: %+v scope=%s", saved, scope)
	}
}

func TestBindLyricsSameSongClears(t *testing.T) {
	st, fake, song := lyricFixture(t)
	if err := st.SaveLyricBinding(song.ID, store.LyricBinding{Kind: store.BindingKindSource, Source: "fake", ExternalID: "2", Page: 1}); err != nil {
		t.Fatal(err)
	}
	svc := New(source.NewRegistry(fake), st, nil)
	cand := LyricCandidate{Kind: store.BindingKindSource, Source: "fake", ExternalID: "1", Page: 1}
	_, cleared, err := svc.BindLyrics(context.Background(), song, cand)
	if err != nil || !cleared {
		t.Fatalf("绑定到自身应视为解除: %v cleared=%v", err, cleared)
	}
	if _, scope, _ := st.LyricBinding(song.ID); scope != store.BindingAuto {
		t.Fatalf("解除后应无绑定: %s", scope)
	}
}

func TestSearchLyricCandidatesMergesSources(t *testing.T) {
	st, fake, song := lyricFixture(t)
	fake.search = source.SearchPage{Candidates: []source.Candidate{
		{Track: source.Track{Source: "fake", ExternalID: "2", Page: 1, Title: "晴天", Artist: "周杰伦", Duration: 200}},
		{Track: source.Track{Source: "fake", ExternalID: "3", Page: 1, Title: "晴天（翻唱）", Artist: "别人", Duration: 240}},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "trackName": "晴天", "artistName": "周杰伦", "duration": 200, "syncedLyrics": "[00:01.00]x"},
				{"id": 2, "trackName": "晴天", "artistName": "周杰伦", "duration": 201, "plainLyrics": "没有时间轴"},
			})
		default:
			t.Errorf("意外请求 %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	// 让 fake 也充当网易云来源，走同一个 Search。
	netease := &lyricFake{baseFake: &baseFake{id: source.NetEase, caps: source.CapSearch, logged: true, search: fake.search}}
	svc := New(source.NewRegistry(netease), st, lrclib.NewWithBaseURL(srv.URL))
	cands, err := svc.SearchLyricCandidates(context.Background(), song, "晴天", "")
	if err != nil {
		t.Fatalf("搜索候选失败: %v", err)
	}
	var haveSource, haveLRCLIB bool
	for _, c := range cands {
		switch c.Kind {
		case store.BindingKindSource:
			haveSource = true
		case store.BindingKindLRCLIB:
			haveLRCLIB = true
			if c.LRCLIBID == 2 && (c.HasSynced || !c.Known) {
				t.Fatalf("无时间轴候选应标记 Known 且 HasSynced=false: %+v", c)
			}
		}
	}
	if !haveSource || !haveLRCLIB {
		t.Fatalf("两个来源都应出现在候选里: %+v", cands)
	}
	// 精确匹配（标题+歌手+时长）应排在翻唱前面。
	if cands[0].ExternalID != "2" {
		t.Fatalf("接近度排序不符: %+v", cands)
	}
	// 按来源过滤。
	only, err := svc.SearchLyricCandidates(context.Background(), song, "晴天", "lrclib")
	if err != nil {
		t.Fatalf("过滤搜索失败: %v", err)
	}
	for _, c := range only {
		if c.Kind != store.BindingKindLRCLIB {
			t.Fatalf("过滤后不应出现其它来源: %+v", only)
		}
	}
}

func TestUsersNeverReportLocalSource(t *testing.T) {
	st, _, _ := lyricFixture(t)
	// 使用真实本地源：它不置 CapAccount，账号摘要与登录页都不该看到它。
	localSrc := localsource.New(st, lyrics.DefaultLocalConfig())
	svc := New(source.NewRegistry(localSrc), st, nil)
	users, expired := svc.Users(context.Background())
	if len(users) != 0 || len(expired) != 0 {
		t.Fatalf("本地源不应出现在账号摘要里: %+v %+v", users, expired)
	}
	if ids := source.NewRegistry(localSrc).AccountIDs(); len(ids) != 0 {
		t.Fatalf("AccountIDs 不应包含本地源: %v", ids)
	}
}

// 防止「测试夹具时间」相关字段被误用：BoundAt 必须是秒级时间戳。
func TestBindingBoundAtIsUnixSeconds(t *testing.T) {
	st, fake, song := lyricFixture(t)
	svc := New(source.NewRegistry(fake), st, nil)
	binding, _, err := svc.BindLyrics(context.Background(), song, LyricCandidate{
		Kind: store.BindingKindSource, Source: "fake", ExternalID: "2", Page: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding.BoundAt < time.Now().Add(-time.Minute).Unix() {
		t.Fatalf("BoundAt 应为秒级时间戳: %d", binding.BoundAt)
	}
}

// TestLyricsLocalSongBindingUsesTargetSidecar 验证 local_song 绑定读目标曲目的旁挂歌词。
func TestLyricsLocalSongBindingUsesTargetSidecar(t *testing.T) {
	st := openTestStore(t)
	dir := t.TempDir()
	folder, err := st.CreateFolderPlaylist("", dir)
	if err != nil {
		t.Fatal(err)
	}
	target := store.NormalizeSong(store.Song{Source: "local", RemoteID: "m:target", Page: 1, Title: "夜航星"})
	if err := os.WriteFile(filepath.Join(dir, "target.flac"), []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "target.lrc"), []byte("[00:01.00]目标曲目的歌词\n[00:02.00]第二句"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceLibraryItems(folder.ID, []store.LibraryItem{{
		Song: target, Path: filepath.Join(dir, "target.flac"), ContentHash: "h",
	}}); err != nil {
		t.Fatal(err)
	}
	platform := store.NormalizeSong(store.Song{Source: "fake", RemoteID: "1", Page: 1, Title: "晴天"})
	if _, err := st.UpsertSong(platform); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveLyricBinding(platform.ID, store.LyricBinding{
		Kind: store.BindingKindLocalSong, SongID: target.ID, Title: "夜航星",
	}); err != nil {
		t.Fatal(err)
	}
	localSrc := localsource.New(st, lyrics.DefaultLocalConfig())
	svc := New(source.NewRegistry(localSrc), st, nil)
	res := svc.Lyrics(context.Background(), platform)
	if len(res.Lines) != 2 || res.Lines[0].Text != "目标曲目的歌词" {
		t.Fatalf("local_song 绑定未取到旁挂歌词: %+v", res)
	}
	if res.OffsetKey != store.BindingLocal || !res.Bound {
		t.Fatalf("local_song 绑定来源不符: %+v", res)
	}
}

// TestRelocateLocalBindingKeepsOffset 验证「重新定位」只换路径，不丢偏移。
func TestRelocateLocalBindingKeepsOffset(t *testing.T) {
	st, fake, song := lyricFixture(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "a.lrc")
	second := filepath.Join(dir, "b.lrc")
	if err := os.WriteFile(first, []byte("[00:01.00]第一版"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("[00:01.00]重新定位后的歌词"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New(source.NewRegistry(fake), st, nil)
	cand := LyricCandidate{Kind: store.BindingKindLocalFile, Path: first}
	if _, _, err := svc.BindLyrics(context.Background(), song, cand); err != nil {
		t.Fatal(err)
	}
	svc.SaveLyricOffset(song.ID, store.BindingLocal, 700)

	cand.Path = second
	if _, _, err := svc.BindLyrics(context.Background(), song, cand); err != nil {
		t.Fatalf("重新定位失败: %v", err)
	}
	saved, ok := st.LocalLyricBinding(song.ID)
	if !ok || saved.Path != second {
		t.Fatalf("路径未更新: %+v ok=%v", saved, ok)
	}
	if saved.OffsetMS != 700 {
		t.Fatalf("重新定位应保留 offset: %d", saved.OffsetMS)
	}
	// 换成另一个平台来源时仍然从 0 开始。
	if _, _, err := svc.BindLyrics(context.Background(), song, LyricCandidate{
		Kind: store.BindingKindSource, Source: "fake", ExternalID: "2", Page: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if portable, ok := st.PortableLyricBinding(song.ID); !ok || portable.OffsetMS != 0 {
		t.Fatalf("换源的新绑定应从 0 开始: %+v", portable)
	}
}
