# 0017 系统媒体面板用 MPRIS 桥接，封面只服务面板

- 状态：已接受
- 日期：2026-11-01
- 关联：ADR-0015（player/downloader 边界）

## 背景

终端界面不占系统媒体面板：锁屏、蓝牙耳机媒体键和桌面面板都看不到当前曲目，也不能
从面板控制播放。另一方面，终端内渲染封面需要 kitty/sixel 等能力，和 TUI 的可移植性
冲突，收益也有限。

## 决策

- **Linux 用会话 D-Bus 的 MPRIS**：`internal/sysmedia` 通过 `godbus/dbus` 注册
  `org.mpris.MediaPlayer2.zero`，实现播放/暂停/上下首/停止/进度/音量的标准方法；
  `internal/tui` 每 250ms 把内存快照 `Publish` 给服务，平台实现负责合并写系统接口。
- **系统媒体命令走事件**：`Commands()` channel → `waitSysmediaCmd` → `sysCommandMsg`，
  `Update` 只做状态映射，不直接调用播放器；与引擎事件同一套事件循环约束。
- **封面只为系统面板**：当前曲目变化时在 `tea.Cmd` 里调用 `app.Service.CoverPath`，
  带平台防盗链头下载并缓存到 `~/.config/zero-beat/cache/covers`（64 MiB 上限），
  终端不渲染、不嵌入、不接 kitty/sixel。
- **非 Linux 是空实现**：`sysmedia.New` 返回 `nil, nil`，调用方按未启用处理；
  macOS/Windows 暂不做平台适配。
- 提供 `ZERO_SYSMEDIA=0` 关闭整个系统媒体集成；封面失败只影响封面，不影响播放。

## 理由

1. MPRIS 是 Linux 桌面的事实标准，媒体键、锁屏和蓝牙耳机都通过它工作，不需要逐个
   桌面环境适配。
2. `godbus` 是纯 Go、免 cgo，不引入新的系统构建依赖；MPRIS 的命令/属性模型与现有
   播放控制器能一一映射。
3. 封面复用平台已有 URL 与适配器的防盗链头（`source.CoverHeadersProvider`），
   缓存按 URL 哈希命名，不会为终端渲染增加复杂度。
4. MPRIS 只写本机会话总线，不额外上报数据；与同步/遥测无关。

## 后果

- Linux 上注册 `org.mpris.MediaPlayer2.zero`；名称被占用时明确报错并提示先退出旧实例，
  不再退到 `.instance<pid>`（通常由单实例锁提前阻止）。
- 封面缓存占用磁盘；超过上限按最近访问时间淘汰，删除缓存后下一首会重新下载。
- 没有桌面通知；同一数据目录的单实例由 `internal/instance` 保证，不同数据目录仍可能
  抢同一个 MPRIS 名称，此时 MPRIS 初始化会明确报错，TUI 继续运行但禁用系统媒体。
  测试可用 `ZERO_MPRIS_NAME` 隔离。
- 播放状态每 250ms 更新一次，系统面板上的进度是插值显示，不是逐帧同步。
- macOS/Windows 用户没有系统面板/媒体键；以后接入需要各自的平台实现。

## 替代方案

- **直接对接各国桌面环境**：覆盖不全，维护成本高，放弃。
- **终端内渲染封面（kitty/sixel）**：破坏可移植性，且与本项目「终端优先」的定位冲突，放弃。
- **把 MPRIS 逻辑放进 TUI**：会让 TUI 直接持有 D-Bus 与 goroutine，难以单测，放弃。
