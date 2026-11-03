# 播放顺序控制落地方案（MPRIS Shuffle / LoopStatus）

- 状态：已实现（代码与单测完成；KDE 展开视图手工验收待做一次）
- 日期：2026-11-03
- 关联：[ADR 0017](adr/0017-system-media.md)、[系统媒体与封面](guide/sysmedia.md)
- 预估：1.5–2 人天（含测试与 KDE 手工验证）

## 目标

让 KDE 媒体控件（Plasma 5.27 / 6.x 展开视图）里的“随机”和“循环”按钮可以控制
zero-beat 的播放顺序，并让 TUI、MPRIS 属性、系统面板三处状态始终一致。

## 非目标

- 不修改 KDE / Plasma 侧代码：面板本来就有这两个按钮，只差播放器把属性做成可写。
- 不支持“随机 + 单曲循环”这类正交组合（见 D4）。
- 不涉及 Windows / macOS 的系统媒体集成。
- 不实现 MPRIS `TrackList` / `Playlists` 接口。

## 结论

KDE 侧零改动。zero-beat 侧做两件事：

1. 播放器补一个真正的“顺序（播完即停）”模式，让 MPRIS 的 `LoopStatus=None` 有真实对应；
2. `sysmedia` 把 `Shuffle` / `LoopStatus` 改成可写属性，写入后映射为模式切换，并回发
   `PropertiesChanged`。

不采用 go-musicfox “忽略客户端目标值、只做循环切换”的做法。它的循环顺序恰好和 KDE
对齐，所以看起来能用，但 `playerctl loop none` 这类按目标值写入的客户端会失效，且
`Shuffle` 变化连带的 `LoopStatus` 可能短暂不同步。zero-beat 的 `sysCommandMsg` 在
应用命令后会立即 `publishNowPlaying()`（`internal/tui/model.go:466`），天然适合做
“按目标值映射 + 立即回发”。

## 1. 现状

| 位置 | 现状 | 影响 |
| --- | --- | --- |
| `internal/player/controller.go:16` | 只有 `ModeSequence(0)` / `ModeLoopOne(1)` / `ModeShuffle(2)` | 没有“播完即停”状态，`LoopStatus=None` 无法表达 |
| `internal/player/queue.go:237` | `NextIndex` 末尾回 0 | 现有“顺序”实际是列表循环 |
| `internal/sysmedia/service_linux.go:124` | `LoopStatus` / `Shuffle` 内省为 `read` | 面板按钮可点，但写入被拒 |
| `internal/sysmedia/service_linux.go:264` | `LoopStatus` 恒返回 `"None"` | 面板无法看到真实循环状态 |
| `internal/sysmedia/service_linux.go:398` | `props.Set` 只接受 `Volume` | 写入直接返回 `PropertyReadOnly` |
| `internal/sysmedia/service_linux.go:185` | `propertyChanges` 不比较 `Shuffle` / `LoopStatus` | 即使改了也不发 `PropertiesChanged` |
| `internal/tui/model.go:2206` | 快照只有 `Shuffle: mode == shuffle` | 没有循环状态字段 |
| `internal/tui/model.go:1253` | `p` 键 `% 3` | 模式循环写死三态 |
| `internal/app/settings.go:32` | 读取模式时限制 `0..2` | 新模式无法持久化 |

KDE 侧：`ExpandedRepresentation.qml` 早已有 `shuffleButton` 和 `repeatButton`，
`enabled` 只要求 `CanControl` 且属性不是 `Unknown`；点击直接写 MPRIS 属性。
libkmpris 的 `PlayerContainer::setLoopStatus/setShuffle` 不做乐观更新，等
`PropertiesChanged` 才刷新按钮，所以回发信号是硬要求。

## 2. 设计决策

### D1. 新增“顺序（播完即停）”，现有“顺序”更名“列表循环”

枚举值不变，只在末尾追加：

```go
const (
    ModeListLoop Mode = iota // 0，原 ModeSequence，行为不变（末尾回 0）
    ModeLoopOne              // 1
    ModeShuffle              // 2
    ModeOrdered              // 3，新增：播完最后一首停止
)
```

- 好处：`settings` 里已保存的 0/1/2 不需要迁移；老用户行为不变，只是 0 的显示名从
  “顺序”变成更准确的“列表循环”。
- 代价：状态栏/文档里的模式名变化，需要在发布说明里写一句。
- 备选：保留常量名 `ModeSequence`，只改 `String()`。不推荐，名字和语义会长期打架。
  本方案按“`ModeSequence` → `ModeListLoop`”机械重命名，编译器会找出全部引用。

### D2. MPRIS 写入按目标值映射，last-write-wins

`Shuffle` 和 `LoopStatus` 各自映射到一个 `Mode`，谁后写入谁生效：

| 写入 | 结果模式 | 回发的 Shuffle | 回发的 LoopStatus |
| --- | --- | --- | --- |
| `LoopStatus=None` | `ModeOrdered` | false | `None` |
| `LoopStatus=Playlist` | `ModeListLoop` | false | `Playlist` |
| `LoopStatus=Track` | `ModeLoopOne` | false | `Track` |
| `Shuffle=true` | `ModeShuffle` | true | `Playlist` |
| `Shuffle=false` | 恢复到进入随机前的模式 | false | 对应值 |

写入 `LoopStatus` 时如果当前在随机模式，会退出随机并恢复原始队列顺序；写入
`Shuffle=true` 时如果当前是单曲循环/顺序，会进入随机。面板会在同一次
`PropertiesChanged` 里收到两个属性的真实值。

### D3. 关闭随机时恢复进入随机前的循环模式

`Model` 增加 `modeBeforeShuffle playMode`（仅内存，不持久化）：

- 进入 `ModeShuffle` 时记录当前模式；
- `Shuffle=false` 且当前在随机时，恢复到该模式；
- 如果是从恢复的旧会话直接进入随机，该字段为零值 `ModeListLoop`，行为可预期。

比 go-musicfox“关随机固定回列表循环”更贴近用户预期，代码量约 10 行。

### D4. 不做随机 × 循环正交组合

MPRIS 规范里 `Shuffle` 和 `LoopStatus` 是正交的，zero-beat 仍是单维 `Mode`。因此
“随机 + 单曲循环”无法表达。本方案接受这个有损映射：任一次写入都会覆盖另一个维度，
并在同一次属性变更里把两个真实值都发出去，避免面板显示错位。真要支持组合，需要把
模式重构成 `(shuffle bool, loop LoopStatus)` 两维，属于后续独立需求。

### D5. `p` 键改为四态循环

```go
m.setMode((m.mode + 1) % 4)
```

顺序为：列表循环 → 单曲 → 随机 → 顺序 → 列表循环。老用户从默认的“列表循环”
按 `p` 仍然是“单曲”，前两步与旧版一致；新增的“顺序”在随机之后。

### D6. 顺序模式的边界行为

- 自动播放到队尾：不再回到第一首，状态停在 `Ended`；再按播放会重播最后一首
  （沿用现有 `Toggle` 语义）。
- 手动 `Next` 在最后一首：不动作；手动 `Previous` 在第一首：不动作。
- MPRIS `CanGoNext` / `CanGoPrevious` 在顺序模式的边界分别置 `false`，面板会禁用按钮。

## 3. 模式模型

| 值 | 常量 | 显示名 | 末尾行为 | 首端 Previous | MPRIS |
| --- | --- | --- | --- | --- | --- |
| 0 | `ModeListLoop` | 列表循环 | 回第一首 | 到最后一首 | `Playlist` / false |
| 1 | `ModeLoopOne` | 单曲 | 重播当前 | 上一首（回绕） | `Track` / false |
| 2 | `ModeShuffle` | 随机 | 洗牌队列循环 | 洗牌队列上一首 | `Playlist` / true |
| 3 | `ModeOrdered` | 顺序 | 停止 | 不动作 | `None` / false |

`ModeLoopOne` 的 `Previous` 未特判单曲，行为是上一首（回绕），本方案不改。

## 4. MPRIS 契约

### 4.1 读取

```text
Shuffle:     bool        // mode == ModeShuffle
LoopStatus:  "None" | "Track" | "Playlist"
CanGoNext:   队列长度 > 1 且不是（顺序模式 + 最后一首）
CanGoPrevious: 队列长度 > 1 且不是（顺序模式 + 第一首）
```

`Snapshot.Loop` 为空时统一按 `None` 处理，避免首次发布时出现 `""` 与 `"None"` 的
无意义抖动。

### 4.2 写入

- `Shuffle` 必须是 `bool`，否则 `org.freedesktop.DBus.Error.InvalidArgs`；
- `LoopStatus` 必须是 `None` / `Track` / `Playlist` 之一，大小写敏感，其他值
  `InvalidArgs`；
- 其他属性保持只读，返回 `PropertyReadOnly`；
- 写入只入队 `Command`，不直接在 D-Bus goroutine 里改播放器状态。

### 4.3 信号

`propertyChanges` 增加两项比较，值为空时先归一化：

```go
if old.Shuffle != snap.Shuffle {
    names = append(names, "Shuffle")
}
if normalizeLoop(old.Loop) != normalizeLoop(snap.Loop) {
    names = append(names, "LoopStatus")
}
```

TUI 的 `sysCommandMsg` 分支本来就在 `applySysCommand` 之后立即
`publishNowPlaying()`；实现时 `setMode` 末尾也会发布一次，所以面板点击和 TUI 按 `p`
都不需要等 250ms tick。

## 5. 改动清单

### 5.1 `internal/player/controller.go`

- 枚举与 `String()`：`ModeSequence` → `ModeListLoop`，追加 `ModeOrdered`，显示名调整。
- `Next()`：在单曲判断之后加顺序模式边界：

```go
if c.state.Mode == ModeOrdered && c.state.Index >= len(c.state.Queue)-1 {
    return store.Song{}, 0, false
}
```

- `Prev()`：开头加：

```go
if c.state.Mode == ModeOrdered && c.state.Index <= 0 {
    return store.Song{}, 0, false
}
```

- `NextSongID()`：列表循环保持 `NextIndex` + 回绕；顺序模式只返回 `Index+1`，越界返回空。

### 5.2 `internal/tui/model.go`

- 常量别名：`modeSequence` → `modeListLoop`，新增 `modeOrdered`。
- 新增字段 `modeBeforeShuffle playMode`；`setMode` 进入随机前记录：

```go
if next == modeShuffle && m.mode != modeShuffle {
    m.modeBeforeShuffle = m.mode
}
```

- `p` 键：`% 3` → `% 4`。
- `advance()` / `nextQueueSongID()` / `prefetchNextCmd()` 的 `modeSequence` 判断：
  - 列表循环：维持回绕预取；
  - 顺序模式：仅在 `Index+1 < len(Queue)` 时预取；
  - 单曲/随机：不预取（现状）。
- `publishNowPlaying()`：

```go
snap := sysmedia.Snapshot{
    // ...
    Shuffle: m.mode == modeShuffle,
    Loop:    loopForMode(m.mode),
    CanNext: canGoNext(m.mode, m.queueIdx, len(m.queue)),
    CanPrev: canGoPrev(m.mode, m.queueIdx, len(m.queue)),
}
```

- 新增几个纯函数，便于表驱动测试：

```go
func loopForMode(m playMode) sysmedia.LoopStatus
func modeForLoop(l sysmedia.LoopStatus) (playMode, bool)
func canGoNext(m playMode, idx, n int) bool
func canGoPrev(m playMode, idx, n int) bool
```

- `applySysCommand` 增加：

```go
case sysmedia.CommandSetShuffle:
    if cmd.Shuffle {
        m.setMode(modeShuffle)
    } else if m.mode == modeShuffle {
        m.setMode(m.modeBeforeShuffle)
    }
case sysmedia.CommandSetLoopStatus:
    if next, ok := modeForLoop(cmd.Loop); ok {
        m.setMode(next)
    }
```

- 队列播完（`advance` 返回 `ok=false` 且顺序模式）可选加一条
  `m.setNotice("已播放到队尾", false)`，不影响状态机。

### 5.3 `internal/sysmedia/types.go`

```go
type LoopStatus string

const (
    LoopNone     LoopStatus = "None"
    LoopTrack    LoopStatus = "Track"
    LoopPlaylist LoopStatus = "Playlist"
)

func (l LoopStatus) Valid() bool { /* 三值之一 */ }
```

- `Snapshot` 增加 `Loop LoopStatus`。
- `CommandKind` 末尾追加 `CommandSetShuffle`、`CommandSetLoopStatus`。
- `Command` 增加 `Shuffle bool`、`Loop LoopStatus`。

### 5.4 `internal/sysmedia/service_linux.go`

- 内省：`LoopStatus`、`Shuffle` 的 `Access` 改为 `readwrite`。
- `playerPropertyLocked`：

```go
case "LoopStatus":
    return dbus.MakeVariant(string(normalizeLoop(s.snap.Loop))), true
```

- `propertyChanges`：按 4.3 增加两项。
- `props.Set`：从“只允许 Volume”改为 switch，增加 `Shuffle` / `LoopStatus` 的类型校验与
  入队；未知属性仍返回 `PropertyReadOnly`。
- 新增 `normalizeLoop(LoopStatus) LoopStatus`（空值转 `LoopNone`）。

### 5.5 `internal/app/settings.go`

- `Preferences()` 的模式范围从 `0..2` 改为 `0..3`。

### 5.6 文档

- `docs/guide/playback.md:23`：模式改为四种，说明“顺序=播完即停、列表循环=回绕”，
  预取说明从“顺序模式”改为“列表循环 / 顺序非末尾”。
- `docs/reference/keys.md:16`：`p` 的四态循环。
- `docs/guide/sysmedia.md`：可控制项增加“随机 / 循环（上一首、下一首之外的播放顺序）”，
  注明面板紧凑视图没有这两个按钮，需要展开媒体控件。
- `docs/guide/limitations.md`：注明随机与循环在 zero-beat 里是单维模式，不支持
  “随机 + 单曲循环”这类组合。
- 发布说明补一条：模式显示名变化，老配置无需迁移。

## 6. 边界行为

| 场景 | 行为 |
| --- | --- |
| 队列为空时写入模式 | 正常切换模式，快照发布空曲目；不崩溃 |
| 队列只有一首，顺序模式 | `CanGoNext`/`CanGoPrevious` 为 false，播放结束后停止 |
| 最后一首自然结束 | 不回到第一首，`PlaybackStatus=Stopped`；再按播放重播最后一首 |
| 暂停时从面板切模式 | 立即切换并入队持久化，不影响暂停状态 |
| 旧配置 mode=0/1/2 | 原值加载，0 显示为“列表循环”，行为不变 |
| 旧版本二进制读到 mode=3 | 旧代码的 `0..2` 校验会忽略，退回默认模式，不会崩溃 |
| 连续快速点击 | 命令通道容量 8，满了会丢弃（现有行为），状态最终以最后一次成功命令为准 |
| 随机中切循环 | 退出随机、恢复原队列顺序，再进入对应循环模式 |
| 随机中关闭随机 | 恢复到 `modeBeforeShuffle`；旧会话恢复时默认列表循环 |

## 7. 测试计划

### 单元测试

- `internal/player/controller_test.go`
  - 顺序模式最后一首 `Next` 返回 `ok=false`，索引不变；
  - 顺序模式第一首 `Prev` 返回 `ok=false`，索引不变；
  - `NextSongID` 四模式表驱动；
  - 现有 `SetMode(ModeSequence)` 引用改为 `ModeListLoop`。
- `internal/tui/model_test.go` / `sysmedia_test.go`
  - `publishNowPlaying` 四模式 → `(Shuffle, Loop, CanNext, CanPrev)` 表驱动；
  - `applySysCommand` 五种写入 → 模式表驱动；
  - 随机中关闭随机恢复 `modeBeforeShuffle`；
  - `p` 连按四次回到原模式。
- `internal/sysmedia/service_linux_test.go`
  - `Properties.Set("Shuffle", true/false)` → 收到对应 `Command`；
  - `Properties.Set("LoopStatus", "None"/"Track"/"Playlist")` → 收到对应 `Command`；
  - 非法值返回 `InvalidArgs`，未知属性返回 `PropertyReadOnly`；
  - `propertyChanges` 在 `Shuffle` / `Loop` 变化时包含对应名称；
  - 内省字符串里两个属性为 `readwrite`。
- `internal/app`（如有 settings 测试）
  - mode=3 能读出，mode=4 被忽略。

### 集成测试

沿用 `ZERO_MPRIS_NAME` 隔离真实会话总线，在现有
`TestMPRISExportsStateAndCommands` 里追加：

```go
obj.Call(propsIface+".Set", 0, mprisPlayerIface, "LoopStatus", dbus.MakeVariant("Track"))
// 断言 <-svc.Commands() == CommandSetLoopStatus{Loop: LoopTrack}
```

## 8. 手工验收

### KDE（Plasma 5.27 / 6.x）

1. 展开媒体控件，确认循环按钮初始状态与 TUI 状态栏一致；
2. 连续点击循环：`None → Playlist → Track → None`，每次 TUI 状态栏同步变化；
3. 点击随机：按钮点亮，TUI 进入随机，循环按钮显示 `Playlist`；
4. 随机中点击循环：退出随机，队列顺序恢复，随机按钮熄灭；
5. 暂停状态下切模式，再播放，状态不丢；
6. 重启 zero-beat，面板状态与持久化模式一致；
7. 队列只有一首、最后一首自然结束两种边界。

### 协议层（本机 GNOME 也可）

```bash
playerctl -l
playerctl --player=zero shuffle on
playerctl --player=zero loop Track
busctl --user get-property org.mpris.MediaPlayer2.zero \
  /org/mpris/MediaPlayer2 org.mpris.MediaPlayer2.Player LoopStatus
busctl --user get-property org.mpris.MediaPlayer2.zero \
  /org/mpris/MediaPlayer2 org.mpris.MediaPlayer2.Player Shuffle
```

重点验证 `playerctl loop none` 确实进入“顺序（播完即停）”，而不是被当作“下一个循环模式”。

## 9. 实施顺序

| 阶段 | 内容 | 估时 |
| --- | --- | --- |
| 1 | player 模式模型（含 `Next`/`Prev`/`NextSongID`）+ 单测 | 0.5 天 |
| 2 | sysmedia 可写属性、命令、`propertyChanges` + 单测 | 0.5 天 |
| 3 | TUI 接线（快照、命令映射、`p` 四态、预取、CanNext/Prev）+ 单测 | 0.5 天 |
| 4 | 文档、playerctl/KDE 手工验收、发布说明 | 0.5 天 |

建议按三个阶段拆提交，便于回滚：

1. `feat(player): 增加顺序播放（播完即停）模式`
2. `feat(sysmedia): 支持写入 Shuffle/LoopStatus`
3. `feat(tui): 系统媒体面板控制播放顺序`

## 10. 风险与回滚

- **显示名变化**：`顺序` → `列表循环` 会让老用户第一次看到不同文案。行为未变，发布说明
  写清楚；如果反馈强烈，可只回退 `String()`，不影响 MPRIS。
- **KDE 版本**：5.27 与 6.x 已确认有按钮；更老的 Plasma 没有这两个按钮，但也不会因为
  属性可写而变差。
- **有损组合**：随机与循环不能正交，已在 D4 说明；后续要支持需重构 `Mode`。
- **CanGoNext 变 false**：顺序模式最后一首会禁用面板“下一首”，与 TUI 行为一致；如果
  产品希望保留“按了没反应”，可以只在 `Next()` 里 no-op，不改 `CanNext`。
- **回滚**：三个提交可独立回退；`settings` 的 0/1/2 数值不变，回退后旧代码能正常读取。

## 11. 验收标准

- [ ] KDE 展开视图的随机/循环按钮可点击，图标状态与 TUI 一致；
- [ ] `playerctl shuffle on/off`、`loop none/track/playlist` 行为符合 MPRIS 语义；
- [ ] TUI 按 `p` 四态循环，顺序模式播完即停；
- [ ] 随机关闭后恢复进入随机前的模式与队列顺序；
- [ ] `PropertiesChanged` 同时覆盖 `Shuffle` 与 `LoopStatus`；
- [ ] 旧配置 0/1/2 正常加载，新配置 3 正常持久化；
- [ ] `go test ./...` 通过。
