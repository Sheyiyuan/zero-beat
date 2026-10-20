# ADR-0003 音频走解码器抽象：native 优先，mpv 兜底

- 状态：已被 [ADR-0008](0008-ffmpeg-first.md) 取代（首版用 ffmpeg 单后端）
- 日期：2026-10-18

## 背景

桌面版把解码、缓冲、seek 全部外包给 WebKit 的 `<audio>`。TUI 没有这层，且要面对两个平台的编码矩阵：

| 来源 | 编码 | 纯 Go 可解 |
| --- | --- | --- |
| 网易云 standard / higher / exhigh | MP3 | ✅ |
| 网易云 lossless / hires | FLAC | ✅ |
| B 站 30280 / 30232（`mp4a.40.2`） | AAC-LC | ⚠️ 依赖新库 |
| B 站 30216（`mp4a.40.5`） | HE-AAC | ❌ |
| 大会员轨 | FLAC / EC-3 | FLAC ✅ / EC-3 ❌ |

## 决策

播放引擎只认 `source.Stream`；解码做成 `Decoder` 接口，计划两套实现：

- `native`：纯 Go（`go-mp3` / `flac` / `go-aac`），覆盖绝大多数曲目，无外部依赖；
- `mpv`：外部进程 + JSON IPC，兜底 HE-AAC、EC-3 以及 native 解码失败的场景。

**选轨时优先挑 native 可解的编码**（B 站直接选 30280），而不是解不了再换后端。

## 理由

1. 单一路线都不可行：纯 Go 解不了 HE-AAC，纯 mpv 则把一个"单文件 TUI 工具"变成"必须装 mpv"。
2. 两条路线共用同一套引擎与 DSP，切换成本只是实现一个接口。
3. 兜底路径同时解决了设备兼容性与疑难文件的排查手段。

## 后果

- 需要维护两套后端与一套能力探测（`domain.Codec.DecodableNative()`）。
- `go-aac` 很新（2026-07 建仓，LGPL-2.1），必须先 spike 验证再作为主路径；不达标就把 native 限定在 MP3/FLAC。
- 对外文档要写清 mpv 是可选依赖。

## 替代方案

- 只用 ffmpeg 管道（PCM 输出到 oto）：实现最省事，但 seek 需重启进程，且引入更大的外部依赖。
- 只用纯 Go：HE-AAC 与 EC-3 直接放弃，等于永久牺牲部分 B 站曲目。
