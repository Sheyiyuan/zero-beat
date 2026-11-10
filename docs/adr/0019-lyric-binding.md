# 0019 歌词来源绑定：用户指定优先，offset 随源

- 状态：已接受
- 日期：2026-11-10
- 关联：ADR-0018（本地音乐库）、ADR-0016（git 同步）
- 设计：[feature-local-library-and-lyrics.md](../feature-local-library-and-lyrics.md)

## 背景

B 站视频标题经常不是歌名，平台字幕与 LRCLIB 都可能匹配错；本地曲目的歌词散落在
目录、内嵌标签或另一首歌旁边。以前只能靠自动匹配，用户没有纠正手段，也没有办法
把「这次手动选中的来源」记住。

同时，歌词偏移原本是歌曲级的一个整数。一旦歌词来源可以切换，歌曲级 offset 就会
错位：换到一个时间轴不同的版本后，还带着上一个版本的偏移。

## 决策

### 一首歌一个生效绑定，portable 与 local 分开存

```go
type LyricBinding struct {
    Kind       string // source | lrclib | local_file | local_song
    Source     string
    ExternalID string
    Page       int
    CID        int64
    LRCLIBID   int64
    Path       string
    SongID     string
    Title      string
    Artist     string
    Duration   int
    OffsetMS   int
    BoundAt    int64
}
```

- `source` / `lrclib` 存 `lyric_bind:<songID>`，可跨设备复现；
- `local_file` / `local_song` 存 `lyric_bind_local:<songID>`，设备相关；
- 两个 key 永不混写；本机绑定优先，界面提示「本机绑定覆盖同步绑定」；
- `local_song` 只读目标曲目的旁挂/内嵌歌词，**不跟随目标曲目自己的绑定**，避免成环。

只存引用和展示信息，不存歌词正文：正文每次按引用重取。

### 只有「用户显式选择」才落库

自动匹配结果不写设置。绑定前必须取到至少一行带时间轴的歌词并预览；失败不落库。
绑定失效（源未登录、记录被删、文件丢失）时降级到自动来源，`LyricsResult`
带 `Degraded=true` 与 `Warning`，界面提示「绑定失效，已回退」，但**不自动清除
绑定**——用户修好文件或重新登录后还能恢复。

绑定指向歌曲自己视为解除绑定。

### offset 属于当前歌词来源

`LyricsResult.OffsetKey`（`auto` / `portable` / `local`）表示**实际生效**的来源，
而不是「是否存在绑定」：

| 当前实际来源 | offset 存哪 | 同步 |
| --- | --- | --- |
| portable 绑定 | 绑定 JSON 的 `offset_ms` | 随注释同步（歌曲非 local 时） |
| local 绑定 | local 绑定 JSON 的 `offset_ms` | 不同步 |
| 自动（无绑定或降级） | `lyric_offset:<songID>` | 按注释规则同步 |

`<` / `>` / `0` 调偏移时把 `OffsetKey` 传给 `SaveLyricOffset`：降级到自动时写
auto，不会把失效绑定的 offset 写脏。新绑定从 0 开始；切回旧绑定恢复它自己的
offset；解绑回到 auto offset。

### 绑定是否跨设备 = 目标 portable 且歌曲不是 local 源

本地曲目本身不在同步仓库里，所以它上面的 portable 绑定也按本机处理。
`ListSyncAnnotations` 跳过 `source=local` 的歌曲与 `local:` 前缀的孤儿注释；
`AnnotationFile` 只写 portable 绑定，导入时拒绝本机绑定。

## 理由

- 绑定只存引用：体积、版权、平台更新都不需要客户端处理。
- 降级而不清除：用户显式选择的数据不应被一次网络失败吃掉。
- offset 随源：换歌词版本必然换时间轴，全局单值必然错位。
- 同步边界按「可复现」判断：本机路径过去了也打不开。

## 后果

- 绑定浮层（`B`）承担搜索、预览、本地文件、解除四件事，是本期最大的 UI 增量。
- 自动来源失败与绑定失败需要区分提示；`Degraded` / `Warning` 进入 `lyricsMsg`。
- 每个自动来源仍共用一个 auto offset（平台官方 vs LRCLIB 不分开），列为后续项。

## 替代方案

| 方案 | 不采用的原因 |
| --- | --- |
| 保存歌词正文 | 体积、版权、平台更新；只存引用按需重取 |
| 每首歌一个全局 offset | 换源后偏移错位 |
| 落库自动匹配结果 | 设备/网络相关，会以「用户指定」身份进入同步 |
| B 站字幕作为 donor（v1） | 需要 CID 解析，搜索候选没有 CID，字幕质量不稳定 |
| 纯文本歌词绑定 | 没有时间轴就不能跟唱，只会制造对不齐的错觉 |
