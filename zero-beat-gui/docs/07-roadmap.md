# 路线图

估算按「单人、熟悉 Go 与 Flutter、有 Android 真机」计；单位为人周。所有阶段都以「可演示 + 有验收标准」结束，不做长期不可见的重构。

这些是设计阶段的工作量区间，不是发布日期承诺。先完成 P0 的最小边界与 P1 原型，再按实测结果重估；本页是实施估算的唯一汇总，选型文档不另列竞争口径。

## P0 · Session 与 GUI 最小公开 SDK（3–4 周）

目标：先解决 Go 的 `internal/` 边界与平台依赖，只公开 P1 所需最小接口；全量业务门面在 P2/P3/P5 随功能补齐（[ADR-0006](decisions/0006-module-boundary.md)）。core/TUI 物理拆分、媒体类型与键位解耦已在 TUI 阶段落地；本阶段专注 Session 与 GUI 入口。

- [ ] 将 Stream/State/Event 抽到纯类型包，移除核心对桌面音频实现/oto 的传递依赖；拆开 keymap 的 TOML/动作定义与 Bubble Tea 解析；
- [ ] `session` 抽出最小 headless Session：`BeginPlay` → 解析 worker → `CompletePlay`；补取消/乱序结果、共享重试预算、结束去重与持久化测试；
- [ ] 最小 DTO：Song / Playlist / Stream / State；引擎事件适配保留播放代次；
- [ ] `session.Engine` 接口（Dart 侧实现），内部适配 `player.Engine`；
- [ ] `session` 提供 Open/Close、列歌单/曲目、播放；根目录显式注入，桌面遵循 TUI 路径规则；先拿实例锁再开库；
- [ ] `sync` 提供现有格式只读校验入口，拟定后端/冲突对象契约；复用 TUI 阶段的生产 Git 后端；
- [ ] TUI 行为不变（可以先不迁到 `Session`）；
- [ ] 检查各目标的 `go list -deps`，核心依赖树不得包含 tui、Bubble Tea、oto；编译 Linux/Android arm64/macOS 的最小 c-shared 入口，Windows 仅做纯 Go 核心交叉编译检查；
- [ ] 用小程序跑通 Open/Close、持锁时二次启动不触库、启动失败清理。

**Exit**：最小 SDK 的依赖边界与目标平台构建通过，TUI 回归通过；不要求先完成全量门面或物理拆仓。

## P1 · 最小闭环与独立同步原型（3–4 周）

目标：验证高风险路径。Android 播放/下载与 Git 合并分别用最小原型验证，不要求尚未在 P5 实现的完整 Android 同步 UI 提前可用。

- [ ] Flutter Linux/macOS 空壳 + Android 模拟器空壳；
- [ ] Go 核心编成 `libzero.so` / `libzero.dylib`，`ZeroStart` 返回端口/token；
- [ ] 最小业务 RPC：hello、列歌单/曲目、play/pause/seek，加上必需的 engine 命令和事件；
- [ ] Dart 加载核心、连 RPC、渲染歌单、点歌出声；
- [ ] media_kit 播 B 站 + 网易云各一首，headers 正常；
- [ ] `af=loudnorm` 在 Android/Linux/macOS 上生效（与 TUI 同曲 A/B）；
- [ ] audio_service 通知 + 锁屏（Android）与 Now Playing/媒体键（macOS）；
- [ ] Android 真机锁屏 30 分钟不中断；耳机拔出暂停；
- [ ] 快速 A→B、解析乱序、重复/旧代次事件、load 超时、重试成功后再失败；验证最多一次自动重试与无死锁；
- [ ] RPC 缺失/错误 token、慢 hello、帧/连接上限、断线重连；暂停超时不重播；
- [ ] TUI 与 GUI 互斥：先拿锁再开库，自定义 `XDG_CONFIG_HOME` 下仍共用目录；
- [ ] Linux 关窗进小窗；macOS 关窗隐藏、Cmd+Q 退出；
- [ ] Android 下载原型：共享 Go 传输 + 完整媒体流落盘处理器，无外部 ffmpeg；两音源的支持样本、取消/续传、飞行模式播放，记录容器支持矩阵；
- [ ] v2 快照只读校验（现有 Unix 秒字段、严格未知字段策略），不导入真实数据或执行写同步；
- [ ] 独立 Git 原型：临时仓库验证 CLI/go-git 的分叉三方合并、双父提交、push 后再次同步无变化；Android 验证 go-git HTTPS fetch/push。原型不承担生产恢复/自动同步能力；
- [ ] Flutter Linux 中文输入法（fcitx5/ibus）实测；
- [ ] macOS 本地签名 + 非沙盒数据目录实测（公证可后置）。

**Exit**：记录设备、依赖版本、测试样本和结果；任一必需路径失败则修改方案、ADR 与估算后再进入依赖它的阶段，不能用“后续补齐”视为通过。loudnorm/macOS 等允许的降级按风险文档记录。

## P2 · 核心可用（5–6 周）

- 曲库：歌单树、曲目列表、混合歌单展开、分页/虚拟滚动；
- 搜索：合并/单源、分页、结果入队；
- 播放：播放条、队列页、模式、音量/静音、seek、断点续播；
- 歌词：平台/本地/LRCLIB、逐行高亮、偏移、来源绑定；
- 会话：补齐搜索/歌词/设置等公开门面与 RPC，复用 Session；
- 桌面：主窗口、小窗、托盘、MPRIS；
- 设置：音质、自动响度、本地优先、主题跟随系统。

**Exit**：Linux 上可以作为日常播放器用一周；TUI 数据未被破坏；崩溃恢复后队列还在。

## P3 · 账号与下载（3–4 周）

- 扫码登录（二维码 PNG/ASCII）、账号状态、过期冻结；
- 下载：单曲/批量、进度、取消（若上游已支持）、下载管理页；
- 落地完成处理器注入：桌面 ffmpeg、Android 完整媒体流；补资源变化续传、取消、原子落盘/索引失败恢复、分享与不支持格式提示；
- 本地优先：已下载优先本地，失败回落远程；
- 设置持久化与 TUI 共享；
- 通知/错误提示统一。

**Exit**：手机与桌面都能完成「登录→搜歌→下载→离线播放」闭环；Android 在无 ffmpeg、飞行模式下通过 P1 锁定的两音源容器矩阵，导出时不承诺新增嵌入标签。

## P4 · Android 系统集成（2–3 周）

- audio_service 完整：通知按钮、封面、进度、锁屏；
- audio_session：焦点、ducking、耳机、蓝牙；
- 前台服务与电池优化提示；
- 后台 30 分钟稳定性；
- 深色/浅色、刘海/安全区、旋转。

**Exit**：通勤场景（锁屏 30 分钟 + 蓝牙耳机 + 来电打断）可用。

## P5 · GUI 跨设备同步接入（1–2 周）

- 复用 TUI 阶段的双后端、共享合并与恢复；本阶段完成 GUI RPC/配置/冲突页及端到端适配；
- 后端中立的冲突读取（base/local/remote）+ 冲突页；
- 写前格式校验：未知字段/版本不得经“跳过后重新导出”丢失；冲突期间远端/库变化后重新计算；
- 口令派生、账户 LWW、keymap 导入；
- 首次同步/仓库配置/自动同步；
- 与桌面 TUI 的双端同步集成测试。

本阶段只计 GUI 接入 1–2 人周；共享后端和模块迁移属于 TUI 前置工作，不能重复计入 GUI 估算。P1 仍须验证 Android HTTPS 与平台差异。

**Exit**：手机建歌单 → 桌面 TUI 同步可见；桌面改歌词偏移 → 手机同步可见；冲突可逐项选择且不丢数据；无系统 git 的 `zero sync` 可 pull/push；分叉合并提交含两父节点，再次同步无内容变化；双方并发修改、非快进重试、恢复及未知字段保护通过。

## P6 · 桌面打磨与发布（4–6 周，含 macOS）

- 冷启动、内存、列表性能；
- 无障碍（TalkBack/键盘导航）；
- 崩溃日志与隐私开关；
- Linux：托盘、小窗、MPRIS、AppImage/deb；
- macOS：`libzero.dylib` 嵌入/签名/公证/DMG；非沙盒数据共享；菜单栏、关窗隐藏、Dock 恢复、audio_service Now Playing；
- Android AAB；
- 文档、关于页、AGPL 源码链接；
- 发布 v0.1.0。

**Exit**：Android / Linux / macOS 三端安装包可分发；无 P0 级崩溃；同步与播放的核心路径有回归脚本。

## P7 · Windows（5–8 周，P6 之后）

按 [10-windows](10-windows.md) 执行：

- [ ] Windows 构建环境（VS2022）与 `windows-latest` CI；
- [ ] `zero-core.exe` sidecar + Job Object 生命周期；
- [ ] `smtc_windows`（rustup）接系统媒体；
- [ ] 托盘、小窗、任务栏；
- [ ] `%APPDATA%\zero-beat` 数据与 TUI zip 互斥；
- [ ] go-git 同步；
- [ ] MSIX/Inno 安装包；
- [ ] Windows 10/11、DPI、休眠、蓝牙 QA。

**Exit**：Windows 上可日常播放一周；与 TUI zip 数据互通；SMTC/托盘/小窗可用。

## 后续（v0.1 之后）

- Android 文件夹歌单（SAF 目录树）、MediaStore 导出；
- Android Auto / 车机（如需要）；
- 主题编辑器、歌词多候选；
- 把 TUI 也迁到 `session.Session`，彻底共用编排层。

## TUI 前置工作与后续边界

物理拆分已采纳，统一模块为 zero-beat-core、zero-beat-tui、zero-beat-gui。生产同步先在 TUI 验收，GUI 共用实现。完整 Session、Android 下载处理器和 GUI 独立发布仍按本路线图推进；音频和 cmd 不额外拆 module。

## 总估算

| 阶段 | 人周 |
| --- | --- |
| P0 最小 SDK 与逻辑解耦 | 3–4 |
| P1 最小闭环与独立同步原型 | 3–4 |
| P2 核心可用 | 5–6 |
| P3 账号与下载 | 3–4 |
| P4 Android 系统集成 | 2–3 |
| P5 GUI 同步接入 | 1–2 |
| P6 桌面打磨与发布（含 macOS） | 4–6 |
| **v0.1（Android + Linux + macOS）** | **21–29** |
| P7 Windows | 5–8 |
| **含 Windows** | **26–37** |

不含：已前移的 TUI 模块/后端工作、Android 文件夹歌单/重封装/嵌入标签、Android Auto、Intel 通用包、设计稿打磨。必要原型失败导致的替代实现需重新估算，不能把本表上界当作覆盖所有未知风险的承诺。
