# 架构

## 分层

```
┌───────────────────────────────────────────────────────┐
│ cmd/zero        CLI：tui / play / login / demo         │
├───────────────────────────────────────────────────────┤
│ internal/tui    Bubble Tea v2：模型、渲染、按键、命令   │
├──────────────┬──────────────────┬─────────────────────┤
│ internal/bili│ internal/audio   │ internal/store      │
│ 平台客户端    │ 播放引擎          │ SQLite 持久化        │
├──────────────┴──────────────────┴─────────────────────┤
│ internal/lyrics  歌词解析与时间轴                       │
│ internal/config  数据目录                              │
└───────────────────────────────────────────────────────┘
```

依赖方向：`tui` 认识所有人；`bili` / `audio` / `store` / `lyrics` 互不认识；`config` 谁都可以用。

## 数据流

**一次播放的完整路径**

```
用户在列表里按 enter
  → Update 拿到 KeyPressMsg
  → playSong()：置 buffering、记住当前曲目
  → playSongCmd（tea.Cmd，在 goroutine 里跑）
       ├─ 缺 CID 时先 GET /x/web-interface/view 解出分 P 的 cid
       └─ GET /x/player/playurl?fnval=4048
       └─ pickAudio()：优先 flac → 优先 mp4a.40.2 里带宽最大的 → 兜底任意轨
  → streamMsg 回到 Update
  → engine.Play(audio.Stream{URL, Headers, Codec, ExpiresAt...})
       ├─ exec ffmpeg -headers "Referer: ..." -i URL -f s16le -ar 48000 -ac 2 pipe:1
       ├─ 把 stdout 包进 gainReader（应用音量 × 补偿，tanh 软限幅）
       └─ oto.Player 从 gainReader 拉 PCM
  → 并行发起 lyricsCmd（/x/player/v2 → 字幕 JSON → []lyrics.Line）
```

**进度与状态**

- 播放位置：`engine.Position()` 用「已交给声卡的 PCM 字节数 ÷ 字节率 + seek 基准」换算，250ms 的 `tickMsg` 拉一次。
- 播放事件：`engine.Events()` → `waitEngineCmd` 转成 `engineEventMsg`；`EventEnded` 按模式自动下一首，`EventFailed` 提示错误。

## 关键契约

只有两个结构体是跨包契约：

```go
// 音源 → 引擎
type audio.Stream struct {
    URL        string
    Headers    map[string]string // B 站要 Referer
    Codec      string            // 记录实际编码，便于排障
    DurationMS int64
    ExpiresAt  time.Time
    Quality    string            // 展示用：192k / FLAC
}

// 引擎 → UI
type audio.Event struct {
    Type  audio.EventType // StateChanged / Ended / Failed
    State audio.State
    Err   error
}
```

音源负责把平台差异（Referer、cookie、编码选择）压进 `Stream`，引擎只认它。播放直链**不落库**：B 站地址约 2 小时过期，落库只会产出脏数据。

## 数据模型

```sql
songs(id, source, bvid, page, cid, title, artist, duration, cover, added_at)
  UNIQUE(source, bvid, page)          -- 同一 BV 同一分 P 视为同一首
playlists(id, title, kind, remote_id, created_at)   -- kind: local / remote
playlist_items(playlist_id, song_id, position, added_at)
queue(id=1, data, position, queue_index, updated)   -- 单行，存队列 JSON
settings(key, value)                                -- volume / comp_db / mode
accounts(source, payload, updated_at)               -- 登录 cookie（JSON）
```

## 事件循环约束（改 UI 前必读）

1. **Update 里不许有网络或磁盘长任务**。所有 IO 都放到 `tea.Cmd`，结果用 `Msg` 回来。
2. **引擎回调不许直接改 Model**。只往 channel 发事件，由 `waitEngineCmd` 转成消息。
3. **每处理一次引擎事件都要重新挂 `waitEngineCmd`**，否则事件流会断。
4. **渲染必须精确到行**：每行可见宽度等于终端宽度，总行数等于终端高度；`internal/tui/view_test.go` 会校验。
5. 所有宽度计算走 `ansi.StringWidth` / `ansi.Truncate`，不要用 `len()`，否则中文会错位。

## 依赖

| 用途 | 依赖 | 备注 |
| --- | --- | --- |
| TUI | `charm.land/bubbletea/v2` `lipgloss/v2` | 需要 Go 1.26 |
| 音频输出 | `github.com/ebitengine/oto/v3` | 免 cgo，PulseAudio / CoreAudio / WASAPI |
| 解码 | 系统 `ffmpeg` | 覆盖 AAC-LC / HE-AAC / FLAC / EC-3 |
| SQLite | `modernc.org/sqlite` | 纯 Go，免 cgo |
| 二维码 | `github.com/skip2/go-qrcode` | 半块字符渲染 |
| 宽度计算 | `github.com/charmbracelet/x/ansi` | 中文/emoji 宽度 |

## 已知简化（与最初设计的差异）

最初设计里有 `domain.Track` + `domain.TrackSource` 两层模型、`source.Source` 接口、`audio/decoder` 抽象与 `match` / `resolve` 包。实现第一版时这些都被删掉了，理由是**只有一个音源时抽象是猜的**：详见 [ADR-0009](docs/adr/0009-concrete-first.md) 与 [docs/IMPLEMENTATION.md](docs/IMPLEMENTATION.md)。

加网易云时应当先补 `source.Source` 接口，再把两个实现塞进去。
