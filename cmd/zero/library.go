package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/app"
	"github.com/sheyiyuan/zero-beat/internal/lrclib"
	"github.com/sheyiyuan/zero-beat/internal/source"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// runLibrary 是 `zero library` 的入口：本地文件夹歌单的增删与重扫。
func runLibrary(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, libraryUsage)
		return nil
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	svc := app.New(newSources(st), st, lrclib.New())
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("用法: zero library add <路径>")
		}
		playlist, scan, err := svc.AddLocalFolder(strings.Join(args[1:], " "))
		if err != nil {
			return err
		}
		fmt.Printf("已添加文件夹歌单：%s（%s）\n", playlist.Title, playlist.RootPath)
		printScan(scan)
		return nil
	case "list", "ls":
		return listLibrary(st)
	case "scan":
		if len(args) < 2 {
			return fmt.Errorf("用法: zero library scan <歌单ID或路径>")
		}
		playlist, err := findFolderPlaylist(st, strings.Join(args[1:], " "))
		if err != nil {
			return err
		}
		scan, err := svc.RescanLibrary(playlist.ID)
		if err != nil {
			return err
		}
		printScan(scan)
		return nil
	case "remove", "rm":
		if len(args) < 2 {
			return fmt.Errorf("用法: zero library remove <歌单ID>")
		}
		if err := st.DeletePlaylist(args[1]); err != nil {
			return err
		}
		fmt.Printf("已移除文件夹歌单 %s（音频文件未删除）\n", args[1])
		return nil
	default:
		fmt.Fprintln(os.Stderr, libraryUsage)
		return fmt.Errorf("未知 library 子命令 %q", args[0])
	}
}

const libraryUsage = `用法:
  zero library add <路径>          添加本地文件夹为只读歌单并扫描
  zero library list               列出文件夹歌单与扫描状态
  zero library scan <ID或路径>    重新扫描（文件夹不可用时保留上次内容）
  zero library remove <ID>        移除歌单（不删音频文件）`

// listLibrary 打印文件夹歌单与「仅本机」状态。
func listLibrary(st *store.Store) error {
	playlists, err := st.ListPlaylists()
	if err != nil {
		return err
	}
	var found bool
	for _, p := range playlists {
		if p.Kind != store.PlaylistKindFolder {
			continue
		}
		found = true
		status := p.ScanStatus
		if status == "" {
			status = "未扫描"
		}
		line := fmt.Sprintf("%s\t%s\t%d 首\t%s\t%s", p.ID, p.Title, p.Count, status, p.RootPath)
		if p.ScanError != "" {
			line += "\t" + p.ScanError
		}
		fmt.Println(line)
	}
	if !found {
		fmt.Println("还没有文件夹歌单；用 zero library add <路径> 添加。")
	}
	return nil
}

// findFolderPlaylist 按 ID 或目录路径找文件夹歌单。
func findFolderPlaylist(st *store.Store, target string) (store.Playlist, error) {
	if p, err := st.Playlist(target); err == nil && p.Kind == store.PlaylistKindFolder {
		return p, nil
	}
	abs, err := app.ExpandPath(target)
	if err != nil {
		return store.Playlist{}, err
	}
	playlists, err := st.ListPlaylists()
	if err != nil {
		return store.Playlist{}, err
	}
	for _, p := range playlists {
		if p.Kind == store.PlaylistKindFolder && filepath.Clean(p.RootPath) == abs {
			return p, nil
		}
	}
	return store.Playlist{}, fmt.Errorf("找不到匹配的文件夹歌单：%s", target)
}

func printScan(scan app.LibraryScan) {
	fmt.Printf("%s: 扫描 %d 个文件，新增 %d，复用 %d，缺失 %d\n",
		scan.Playlist.Title, scan.Scanned, scan.Added, scan.Reused, scan.Missing)
	if scan.Subdirs > 0 {
		fmt.Printf("发现 %d 个子文件夹，未扫描（本地库只收当前层）\n", scan.Subdirs)
	}
	for _, w := range scan.Warnings {
		fmt.Println("提示：" + w)
	}
}

// runLyrics 是 `zero lyrics` 的入口：查看、绑定与解除歌词来源。
func runLyrics(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, lyricsUsage)
		return nil
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	svc := app.New(newSources(st), st, lrclib.New())
	if len(args) < 2 {
		return fmt.Errorf("用法: zero lyrics %s <歌曲ID>", args[0])
	}
	song, err := loadSong(st, args[1])
	if err != nil {
		return err
	}
	switch args[0] {
	case "show":
		return showLyrics(svc, song)
	case "bind":
		if len(args) < 3 {
			return fmt.Errorf("用法: zero lyrics bind <歌曲ID> <netease:ID|lrclib:ID|路径|song:歌曲ID>")
		}
		return bindLyrics(svc, song, strings.Join(args[2:], " "))
	case "unbind":
		if err := svc.ClearLyricBinding(song.ID); err != nil {
			return err
		}
		fmt.Printf("已解除 %s 的歌词绑定\n", song.Title)
		return nil
	default:
		fmt.Fprintln(os.Stderr, lyricsUsage)
		return fmt.Errorf("未知 lyrics 子命令 %q", args[0])
	}
}

const lyricsUsage = `用法:
  zero lyrics show <歌曲ID>     打印当前生效的歌词来源与偏移
  zero lyrics bind <歌曲ID> <目标>
        目标: netease:<songid> | lrclib:<id> | /路径/x.lrc | song:<本地歌曲ID>
  zero lyrics unbind <歌曲ID>   解除绑定，回到自动来源`

// loadSong 支持完整歌曲 ID 和「音源:外部ID[:分P]」简写。
func loadSong(st *store.Store, raw string) (store.Song, error) {
	if song, err := st.Song(raw); err == nil {
		return song, nil
	}
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) >= 2 {
		page := 1
		if len(parts) >= 3 {
			if n, err := strconv.Atoi(parts[2]); err == nil {
				page = n
			}
		}
		if song, err := st.Song(store.SongID(parts[0], parts[1], page)); err == nil {
			return song, nil
		}
	}
	return store.Song{}, fmt.Errorf("曲库里没有这首歌：%s", raw)
}

func showLyrics(svc *app.Service, song store.Song) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res := svc.Lyrics(ctx, song)
	sourceLabel := res.Source
	if sourceLabel == "" {
		sourceLabel = "未找到"
	}
	fmt.Printf("%s · %s\n来源: %s · 偏移 %+.1fs（%s）\n",
		song.Title, song.Artist, sourceLabel, float64(res.OffsetMS)/1000, res.OffsetKey)
	if res.Bound {
		fmt.Println("绑定: 生效")
	}
	if res.Degraded && res.Warning != "" {
		fmt.Println("提示：" + res.Warning)
	}
	for i, line := range res.Lines {
		if i >= 5 {
			fmt.Printf("… 共 %d 行\n", len(res.Lines))
			break
		}
		ms := line.StartMS
		fmt.Printf("[%02d:%02d.%02d] %s", ms/60000, ms/1000%60, ms%1000/10, line.Text)
		if line.Translation != "" {
			fmt.Printf(" / %s", line.Translation)
		}
		fmt.Println()
	}
	return nil
}

func bindLyrics(svc *app.Service, song store.Song, target string) error {
	target = strings.TrimSpace(target)
	cand := app.LyricCandidate{}
	switch {
	case strings.HasPrefix(target, "lrclib:"):
		id, err := strconv.ParseInt(strings.TrimPrefix(target, "lrclib:"), 10, 64)
		if err != nil || id <= 0 {
			return fmt.Errorf("LRCLIB 记录 ID 无效：%s", target)
		}
		cand = app.LyricCandidate{Kind: store.BindingKindLRCLIB, LRCLIBID: id, Mark: "lrclib"}
	case strings.HasPrefix(target, "song:"):
		id := strings.TrimPrefix(target, "song:")
		targetSong, err := svc.Store().Song(id)
		if err != nil {
			return fmt.Errorf("找不到本地曲目：%s", id)
		}
		cand = app.LyricCandidate{Kind: store.BindingKindLocalSong, SongID: targetSong.ID,
			Title: targetSong.Title, Artist: targetSong.Artist}
	case strings.HasPrefix(target, "netease:"):
		id := strings.TrimPrefix(target, "netease:")
		cand = app.LyricCandidate{Kind: store.BindingKindSource, Source: source.NetEase,
			ExternalID: id, Page: 1, Mark: "netease"}
	case strings.HasPrefix(target, "/") || strings.HasPrefix(target, "~") || strings.HasPrefix(target, "."):
		path, err := app.ExpandPath(target)
		if err != nil {
			return err
		}
		cand = app.LyricCandidate{Kind: store.BindingKindLocalFile, Path: path}
	default:
		return fmt.Errorf("无法识别的歌词目标：%s（支持 netease:ID、lrclib:ID、路径、song:ID）", target)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	preview, err := svc.PreviewLyricBinding(ctx, song, cand)
	if err != nil {
		return err
	}
	if _, cleared, err := svc.BindLyrics(ctx, song, cand); err != nil {
		return err
	} else if cleared {
		fmt.Println("目标就是这首歌自己，已解除绑定")
		return nil
	}
	fmt.Printf("已绑定 %s → %s（%d 行）\n", song.Title, preview.Label, preview.Count)
	return nil
}
