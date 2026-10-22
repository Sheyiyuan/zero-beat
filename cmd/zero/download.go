package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/download"
	"github.com/sheyiyuan/zero-beat/internal/source"
)

var errDownloadHelp = errors.New("download help")

const downloadUsage = `用法: zero download <目标...> [选项]
目标: 各音源支持的链接或 ID（由音源适配器解析）
选项:
  -o, --out <目录或文件>   输出位置（默认 ~/Music/zero-beat）
  -q, --quality <档位>     auto|high|lossless（默认 auto）
  -p, --page <分P>         分页/分 P（默认从链接读取，否则 1）
  -j, --jobs <数量>        并发下载数（默认 2，范围 1-8）
  -f, --force              覆盖已存在的文件，并从零开始下载
`

type downloadOptions struct {
	targets []string
	out     string
	quality string
	page    int
	jobs    int
	force   bool
}

func parseDownloadArgs(args []string) (downloadOptions, error) {
	o := downloadOptions{quality: "auto", jobs: 2}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			return o, errDownloadHelp
		case a == "-o" || a == "--out":
			i++
			if i >= len(args) {
				return o, errors.New("-o 需要一个路径")
			}
			o.out = args[i]
		case strings.HasPrefix(a, "--out="):
			o.out = strings.TrimPrefix(a, "--out=")
		case strings.HasPrefix(a, "-o="):
			o.out = strings.TrimPrefix(a, "-o=")
		case a == "-q" || a == "--quality":
			i++
			if i >= len(args) {
				return o, errors.New("-q 需要一个值")
			}
			o.quality = args[i]
		case strings.HasPrefix(a, "--quality="):
			o.quality = strings.TrimPrefix(a, "--quality=")
		case strings.HasPrefix(a, "-q="):
			o.quality = strings.TrimPrefix(a, "-q=")
		case a == "-p" || a == "--page":
			i++
			if i >= len(args) {
				return o, errors.New("-p 需要一个分 P")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 1 {
				return o, errors.New("-p 需要正整数")
			}
			o.page = n
		case strings.HasPrefix(a, "--page="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--page="))
			if err != nil || n < 1 {
				return o, errors.New("-p 需要正整数")
			}
			o.page = n
		case a == "-j" || a == "--jobs":
			i++
			if i >= len(args) {
				return o, errors.New("-j 需要一个数量")
			}
			n, err := parseJobs(args[i])
			if err != nil {
				return o, err
			}
			o.jobs = n
		case strings.HasPrefix(a, "--jobs="):
			n, err := parseJobs(strings.TrimPrefix(a, "--jobs="))
			if err != nil {
				return o, err
			}
			o.jobs = n
		case strings.HasPrefix(a, "-j="):
			n, err := parseJobs(strings.TrimPrefix(a, "-j="))
			if err != nil {
				return o, err
			}
			o.jobs = n
		case a == "-f" || a == "--force":
			o.force = true
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("未知参数 %s", a)
		default:
			o.targets = append(o.targets, a)
		}
	}
	if len(o.targets) == 0 {
		return o, errors.New("缺少下载目标")
	}
	return o, nil
}

func parseJobs(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 8 {
		return 0, errors.New("-j 需要 1-8 之间的整数")
	}
	return n, nil
}

func parseDownloadQuality(s string) (source.Quality, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return source.QualityAuto, nil
	case "high":
		return source.QualityHigh, nil
	case "lossless":
		return source.QualityLossless, nil
	default:
		return source.QualityAuto, fmt.Errorf("未知音质 %q（可用 auto/high/lossless）", s)
	}
}

type trackLookup interface {
	Track(context.Context, source.Ref) (source.Track, error)
}

func resolveDownload(ctx context.Context, src source.Source, ref source.Ref, quality source.Quality) (source.Track, source.Stream, error) {
	var meta source.Track
	if lookup, ok := src.(trackLookup); ok {
		track, err := lookup.Track(ctx, ref)
		if err != nil {
			fmt.Fprintf(os.Stderr, "提示：读取曲目信息失败，将用 ID 命名：%v\n", err)
		} else {
			meta = track
		}
	}

	resolved, err := src.Resolve(ctx, ref, quality)
	if err != nil {
		return source.Track{}, source.Stream{}, err
	}
	if resolved.Stream.URL == "" {
		return source.Track{}, source.Stream{}, errors.New("音源没有返回可下载地址")
	}

	track := resolved.Track
	if track.Title == "" {
		track.Title = meta.Title
	}
	if track.Artist == "" {
		track.Artist = meta.Artist
	}
	if track.Album == "" {
		track.Album = meta.Album
	}
	if track.Duration == 0 {
		track.Duration = meta.Duration
	}
	if track.Cover == "" {
		track.Cover = meta.Cover
	}
	if track.Source == "" {
		track.Source = ref.Source
	}
	if track.ExternalID == "" {
		track.ExternalID = ref.ExternalID
	}
	if track.Page <= 0 {
		track.Page = ref.Page
	}
	return track, resolved.Stream, nil
}

type downloadJob struct {
	ref     source.Ref
	track   source.Track
	stream  source.Stream
	outPath string
	label   string
	skip    bool
}

func runDownload(args []string) error {
	opts, err := parseDownloadArgs(args)
	if errors.Is(err, errDownloadHelp) {
		fmt.Print(downloadUsage)
		return nil
	}
	if err != nil {
		return err
	}
	quality, err := parseDownloadQuality(opts.quality)
	if err != nil {
		return err
	}

	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	sources := newSources(st)

	refs := make([]source.Ref, 0, len(opts.targets))
	for _, target := range opts.targets {
		ref, matched, err := sources.ParseTarget(target, opts.page)
		if err != nil {
			return fmt.Errorf("%s: %w", target, err)
		}
		if !matched {
			return fmt.Errorf("%s: 无法识别的下载目标", target)
		}
		refs = append(refs, ref)
	}
	if len(refs) > 1 && opts.out != "" && !isDirArg(opts.out) {
		return errors.New("下载多个目标时 -o 必须是目录")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	jobs := make([]*downloadJob, 0, len(refs))
	var resolveErrs []error
	for i, ref := range refs {
		src := sources.Get(ref.Source)
		if src == nil {
			err := fmt.Errorf("音源不可用: %s", ref.Source)
			fmt.Fprintf(os.Stderr, "失败 [%d/%d] %s: %v\n", i+1, len(refs), ref.Key(), err)
			resolveErrs = append(resolveErrs, err)
			continue
		}
		if len(refs) > 1 {
			fmt.Printf("解析 [%d/%d] %s ...\n", i+1, len(refs), ref.Key())
		} else {
			fmt.Printf("解析 %s ...\n", ref.Key())
		}
		job, err := buildDownloadJob(ctx, src, ref, quality, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "失败 [%d/%d] %s: %v\n", i+1, len(refs), ref.Key(), err)
			resolveErrs = append(resolveErrs, err)
			continue
		}
		if job.skip {
			fmt.Printf("已存在，跳过：%s\n", job.outPath)
			continue
		}
		job.label = fmt.Sprintf("%d/%d", i+1, len(refs))
		jobs = append(jobs, job)
	}
	if len(jobs) == 0 {
		return summarizeDownloadErrors(resolveErrs)
	}

	dedupeDownloadPaths(jobs)
	multi := len(jobs) > 1

	for _, job := range jobs {
		qualityText := job.stream.Quality
		if qualityText == "" {
			qualityText = "未知音质"
		}
		dur := job.stream.Duration
		if dur == 0 && job.track.Duration > 0 {
			dur = time.Duration(job.track.Duration) * time.Second
		}
		title := strings.TrimSpace(job.track.Title)
		if job.track.Artist != "" {
			title = strings.TrimSpace(job.track.Artist + " — " + title)
		}
		if multi {
			line := fmt.Sprintf("[%s] %s · %s", job.label, title, qualityText)
			if dur > 0 {
				line += " · " + download.FormatDuration(dur)
			}
			fmt.Println(line)
		} else {
			fmt.Printf("曲目：%s\n", title)
			meta := []string{"音质 " + qualityText}
			if dur > 0 {
				meta = append(meta, "时长 "+download.FormatDuration(dur))
			}
			fmt.Println(strings.Join(meta, " · "))
		}
	}
	if len(jobs) > 1 {
		fmt.Printf("开始下载 %d 个（并发 %d）\n", len(jobs), minInt(opts.jobs, len(jobs)))
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, opts.jobs)
	var mu sync.Mutex
	var runErrs []error
	canceled := false
	for _, job := range jobs {
		wg.Add(1)
		go func(job *downloadJob) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			if err := downloadJobFile(ctx, job, multi, opts.force); err != nil {
				if errors.Is(err, context.Canceled) {
					mu.Lock()
					canceled = true
					mu.Unlock()
					return
				}
				mu.Lock()
				runErrs = append(runErrs, err)
				mu.Unlock()
				dlLogf("[%s] 失败：%v\n", job.label, err)
			}
		}(job)
	}
	wg.Wait()

	if canceled || ctx.Err() != nil {
		fmt.Fprintln(os.Stderr, "已取消；未完成的 .part 文件已保留，可重新运行继续下载。")
		return nil
	}
	errs := append(resolveErrs, runErrs...)
	if len(errs) == 0 {
		if len(jobs) > 1 {
			fmt.Printf("完成 %d 个下载\n", len(jobs))
		}
		return nil
	}
	if len(jobs) > 1 {
		fmt.Fprintf(os.Stderr, "完成 %d 个，失败 %d 个\n", len(jobs)-len(runErrs), len(errs))
	}
	return summarizeDownloadErrors(errs)
}

func buildDownloadJob(ctx context.Context, src source.Source, ref source.Ref, quality source.Quality, opts downloadOptions) (*downloadJob, error) {
	track, stream, err := resolveDownload(ctx, src, ref, quality)
	if err != nil {
		return nil, err
	}
	if track.Title == "" {
		track.Title = ref.ExternalID
	}
	if track.Source == "" {
		track.Source = ref.Source
	}
	if track.ExternalID == "" {
		track.ExternalID = ref.ExternalID
	}

	ext := download.Ext(stream.Codec)
	outPath, err := resolveDownloadOutput(opts.out, track, ext)
	if err != nil {
		return nil, err
	}
	if !opts.force {
		if _, err := os.Stat(outPath); err == nil {
			return &downloadJob{outPath: outPath, skip: true}, nil
		}
	}
	return &downloadJob{
		ref:     ref,
		track:   track,
		stream:  stream,
		outPath: outPath,
	}, nil
}

func downloadJobFile(ctx context.Context, job *downloadJob, multi bool, force bool) error {
	part := download.PartPath(job.outPath, job.track)
	if info, err := os.Stat(part); err == nil && info.Size() > 0 {
		if multi {
			dlLogf("[%s] 继续下载（已有 %s）\n", job.label, download.HumanBytes(info.Size()))
		} else {
			fmt.Fprintf(os.Stderr, "继续下载：已有 %s\n", download.HumanBytes(info.Size()))
		}
	}

	last := time.Time{}
	printed := false
	onProgress := func(p download.Progress) {
		now := time.Now()
		interval := 200 * time.Millisecond
		if multi {
			interval = time.Second
		}
		final := p.Total > 0 && p.Done >= p.Total
		if !final && !last.IsZero() && now.Sub(last) < interval {
			return
		}
		last = now
		printed = true
		if multi {
			if p.Total > 0 {
				dlLogf("[%s] %s / %s (%.0f%%)\n", job.label,
					download.HumanBytes(p.Done), download.HumanBytes(p.Total),
					float64(p.Done)*100/float64(p.Total))
			} else {
				dlLogf("[%s] %s\n", job.label, download.HumanBytes(p.Done))
			}
			return
		}
		if p.Total > 0 {
			dlLogf("\r下载中  %s / %s (%.0f%%)", download.HumanBytes(p.Done),
				download.HumanBytes(p.Total), float64(p.Done)*100/float64(p.Total))
		} else {
			dlLogf("\r下载中  %s", download.HumanBytes(p.Done))
		}
	}

	result, err := download.Run(ctx, job.track, job.stream, job.outPath, force, onProgress)
	if !multi && printed {
		dlLogf("\n")
	}
	if err != nil {
		if errors.Is(err, download.ErrExists) {
			fmt.Printf("已存在，跳过：%s\n", job.outPath)
			return nil
		}
		return err
	}

	done := fmt.Sprintf("已下载：%s", result.Path)
	if result.Size > 0 {
		done += "（" + download.HumanBytes(result.Size) + "）"
	}
	if multi {
		dlLogf("[%s] %s\n", job.label, done)
	} else {
		fmt.Println(done)
	}
	return nil
}

func dedupeDownloadPaths(jobs []*downloadJob) {
	used := make(map[string]bool, len(jobs))
	for _, job := range jobs {
		if used[job.outPath] {
			ext := filepath.Ext(job.outPath)
			base := strings.TrimSuffix(job.outPath, ext)
			for i := 2; ; i++ {
				candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
				if !used[candidate] {
					job.outPath = candidate
					break
				}
			}
		}
		used[job.outPath] = true
	}
}

func summarizeDownloadErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return fmt.Errorf("%d 个下载失败", len(errs))
}

// downloadToPart 把音频流写入 .part 文件；已有内容时用 Range 续传。
func resolveDownloadOutput(arg string, track source.Track, ext string) (string, error) {
	name := download.Filename(track) + ext
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return filepath.Join(download.DefaultDir(), name), nil
	}
	if isDirArg(arg) {
		if err := os.MkdirAll(arg, 0o755); err != nil {
			return "", fmt.Errorf("创建输出目录: %w", err)
		}
		return filepath.Join(arg, name), nil
	}
	if filepath.Ext(arg) == "" {
		arg += ext
	}
	if err := os.MkdirAll(filepath.Dir(arg), 0o755); err != nil {
		return "", fmt.Errorf("创建输出目录: %w", err)
	}
	return arg, nil
}

func isDirArg(arg string) bool {
	if info, err := os.Stat(arg); err == nil {
		return info.IsDir()
	}
	if strings.HasSuffix(arg, string(os.PathSeparator)) {
		return true
	}
	return filepath.Ext(arg) == ""
}

var dlLogMu sync.Mutex

func dlLogf(format string, args ...any) {
	dlLogMu.Lock()
	defer dlLogMu.Unlock()
	fmt.Fprintf(os.Stderr, format, args...)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
