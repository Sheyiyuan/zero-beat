# 架构

## 分层

```
┌──────────────────────────────────────────────────────────┐
│ cmd/hbp          CLI 子命令：tui / play / login           │
├──────────────────────────────────────────────────────────┤
│ internal/tui     Bubble Tea v2：布局、键位、页面           │
├──────────────────────────────────────────────────────────┤
│ internal/resolve 选源 → 回退 → 预取 → 续签                │
├────────────────────┬────────────────────┬────────────────┤
│ source/bilibili    │ source/netease     │ 后续平台        │
├────────────────────┴────────────────────┴────────────────┤
│ internal/audio    引擎 + decoder(native/mpv) + dsp        │
├──────────────────────────────────────────────────────────┤
│ internal/store    SQLite        │ internal/match 跨源匹配 │
├──────────────────────────────────────────────────────────┤
│ internal/domain   与平台无关的模型（Track / TrackSource） │
└──────────────────────────────────────────────────────────┘
```

依赖方向单向向上：`domain` 不认识任何上层，`source` 不认识 `tui`，`audio` 不认识平台。

## 四条核心契约

**1. `source.Stream`** —— 音源与播放引擎之间唯一的接口。

```go
type Stream struct {
    URL        string
    Headers    map[string]string // B 站要 Referer，网易云要 cookie
    Codec      domain.Codec      // 决定用哪个解码后端
    DurationMS int64
    ExpiresAt  time.Time
    Size       int64             // -1 未知（B 站不支持 HEAD）
}
```

**2. `audio/decoder.Decoder`** —— 解码后端。

```go
type Decoder interface {
    Name() string                              // "native" / "mpv"
    Supports(codec domain.Codec) bool
    Open(ctx, *source.Stream) (io.ReadCloser, Format, error) // 输出 s16le PCM
}
```

native 覆盖 MP3 / FLAC / AAC-LC；mpv 兜底 HE-AAC / EC-3。**选轨时优先挑 native 可解的编码**，而不是解不了再换后端。

**3. `audio.Engine`** —— 播放状态机，唯一出口是事件通道。

```go
Events() <-chan Event   // UI 必须转成 tea.Msg，不能在回调里改状态
```

**4. `source.Source`** —— 平台适配，八个方法 + 能力位。

```go
ID() / Caps() / Account() / Login() / Logout()
Search() / Resolve() / Lyrics()
RemotePlaylists() / SyncRemote()
```

登录通过 `LoginInteraction` 回调把二维码和输入交给 UI —— 这是 v1 最该改的地方：v1 把轮询写死在 service 里，TUI 无法渲染进度。

## 数据模型

两层结构，详见 [docs/design/02-multi-source-plan.md](docs/design/02-multi-source-plan.md) 第 3 节：

- `tracks`：用户视角的一首歌，无平台字段，带 `norm_key` 供跨源匹配。
- `track_sources`：这首歌在某个平台的版本（BV 号 / song id、音质、可用性、是否首选）。
- 歌单引用 `track_id`，与平台解耦；播放时才解析成 `Stream`。

## 禁止事项

1. **禁止在 UI 里按平台名分支** —— 用 `Capability` 位，否则抽象会烂掉。
2. **禁止把播放直链写进数据库** —— 会过期。
3. **禁止在 Bubble Tea 的 `Update` 里做阻塞调用** —— 音频与网络事件一律 channel + `tea.Cmd`。
4. **禁止自己实现网易云加密协议** —— 用 `netease-music`，接口变了跟着升级。
5. **禁止在日志里打印凭证** —— SESSDATA / MUSIC_U 必须脱敏。
6. **禁止把"跨源替换音源"设为默认开启** —— 保持"只播用户自己能播的内容"这条边界。

## 依赖选型

| 用途 | 选择 | 状态 |
| --- | --- | --- |
| TUI 框架 | `charm.land/bubbletea/v2` | 待引入（需 Go 1.26） |
| 样式与布局 | `charm.land/lipgloss/v2` | 待引入 |
| 常用控件 | `charm.land/bubbles/v2` | 待引入 |
| 音频输出 | `github.com/ebitengine/oto/v3` | 待引入（免 cgo，三平台） |
| MP3 / FLAC 解码 | `hajimehoshi/go-mp3` / `mewkiz/flac` | 待引入 |
| AAC-LC 解码 | `tphakala/go-aac`（纯 Go，LGPL-2.1，较新） | 待 spike 验证 |
| 外部兜底解码 | `mpv` 子进程 + JSON IPC | 待引入（可选依赖） |
| 网易云 API | `github.com/go-musicfox/netease-music` | 待引入 |
| 数据库 | `gorm.io/gorm` + SQLite（沿用 v1） | 待搬迁 |
| 配置 | TOML（`koanf`） | 待引入 |
| 系统集成 | `godbus/dbus/v5`（MPRIS）、`gen2brain/beeep` | 二期 |

## 与 v1 的差异

| 方面 | v1 | 本仓库 |
| --- | --- | --- |
| 播放 | WebKit `<audio>` + WebAudio 增益 | 自建引擎，Decoder 可插拔 |
| 音频代理 | 必需（浏览器 CORS/防盗链） | 不需要；外部播放器方案下作为 header 网关保留 |
| 数据模型 | `Song` 混装 B 站字段 | `Track` + `TrackSource` |
| 设置 | `PlayerSetting.config`（`map[string]any`） | 强类型 TOML |
| 登录 | service 内轮询 | `LoginInteraction` 回调，UI 驱动 |
| 凭证 | 明文 SESSDATA | OS keyring（P4） |
| 音质选轨 | `dash.audio[0]`（最低码率） | 按带宽降序 + 可解码性优先 |
