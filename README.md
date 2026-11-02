# zero-beat · 零拍

<p align="center">
  <img src="./assets/readme/hero.svg" width="100%" alt="zero-beat 零拍：左上角是应用图标，Bilibili 与网易云音乐双源 TUI 播放器，两个平台，为你同频">
</p>

**B 站 + 网易云音乐终端播放器。** 在同一个终端里搜索、收藏、排队、播放两个平台的音频，带逐行歌词。

> 名字取自无线电里的 *zero beat*：两个振荡器频率完全相同时差拍归零，听不出接缝。

> 本项目是 [Half Beat Player](https://github.com/Sheyiyuan/Half-Beat-Player) 的续作（至少是精神续作），延续「在电脑上听 B 站视频」的出发点；TUI 形态与播放器交互受到 [go-musicfox](https://github.com/go-musicfox/go-musicfox) 的启发。项目以个人使用为主，不承诺功能路线图、平台兼容性或问题响应速度，欢迎 issue 和 PR。

> **状态：v0.6.0 待发布**（本地已就绪，打 tag 后发布）。双源已打通：B 站 + 网易云扫码登录、单源/合并搜索、收藏夹与歌单、混源队列、本地混源歌单、播放、歌词、断点恢复、下一首预解析；v0.4 加入播放片段、同源云端歌单写入与歌单引用混入；v0.5 补齐下载管理与本地优先播放；v0.5.1 加入曲库批量选择；v0.6.0 加入**跨设备同步**（git 仓库、明文歌单快照、加密登录态、冲突选择）与 **Linux 系统媒体面板**（MPRIS + 封面缓存）。升级前请先看 [v0.6.0 发布说明](docs/release-notes-v0.6.0.md) 与 [Release](https://github.com/Sheyiyuan/zero-beat/releases)。
> 跨源自动匹配与更多能力见 [ROADMAP.md](ROADMAP.md)。

---

## 现在能做什么

- **双源**：B 站与网易云各自扫码登录，单源/合并搜索，混源队列，本地歌单可混装两平台曲目。
- **歌单**：收藏夹与远端歌单按需导入，混合歌单用引用而非复制，同源云端歌单可加歌，同步歌单可解锁为本地。
- **播放**：B 站优先 FLAC / 192k AAC-LC、网易云优先 320k；播放片段、音量补偿、睡眠定时、断点恢复、CDN 续签、下一首预解析。
- **歌词**：平台歌词优先、LRCLIB 兜底，底部常驻当前句并渐变高亮，偏移可调并记住。
- **下载**：TUI 批量下载与下载管理页、CLI 多目标与 Range 续传、ffmpeg 无损封装写标签、默认本地优先播放。
- **跨设备同步**：歌单、键位、注释与加密登录态同步到任意 git 仓库，冲突按实体整版选择，自动同步默认开启。
- **系统集成**：Linux 经 MPRIS 接管系统媒体面板与媒体键，封面缓存只供面板读取；TUI 单实例锁；佐洛红 / 半拍蓝配色与深/浅色模式、自定义主题颜色；快捷键可自定义。

界面截图、键位与逐项行为见下面的文档地图。

## 界面

曲库：左栏歌单，右栏曲目，底部常驻歌词与播放条；左上角提示 `Tab` 切换页面。曲目前的 `[B]` / `[云]` 是来源标记。

[![曲库界面](docs/images/library.webp)](docs/images/library.webp)

歌词页 / 队列页 / 搜索页：

[![歌词页](docs/images/lyrics.webp)](docs/images/lyrics.webp)
[![队列页](docs/images/queue.webp)](docs/images/queue.webp)
[![搜索页](docs/images/search.webp)](docs/images/search.webp)

同步页（`ctrl+s`）：状态、仓库与分支、连接方式（SSH/HTTPS）、密钥、自动同步、冲突入口和归档目录；右下角的同步状态也可以直接点。Linux 下系统媒体面板由 MPRIS 接管，封面只在系统面板显示，不占终端。

## 快速开始

```bash
curl -fsSLO https://github.com/Sheyiyuan/zero-beat/releases/latest/download/install.sh
sh install.sh
zero
```

装完还需要系统 `ffmpeg`（负责 AAC / HE-AAC / FLAC 解码）。进界面后按 `L` 扫码登录，`f` 导入歌单，`enter` 播放；不想联网先看界面可以跑 `zero demo`。

官方支持 Linux / macOS；Windows 提供交叉编译 zip 但不做适配、不维护。Arch 有 AUR 包 `zero-beat-git`。

## 文档

具体指南放在 `docs/`，本文件只留入口。改动用户可见行为时，请改对应的指南，不要再往 README 里加细节。

### 使用指南

| 文档 | 内容 |
| --- | --- |
| [安装与依赖](docs/guide/install.md) | 依赖、平台支持、安装脚本、AUR、源码构建、桌面入口 |
| [快速上手](docs/guide/quickstart.md) | 首次启动、扫码登录、视图轮转、第一次播放、断点恢复 |
| [曲库、歌单与搜索](docs/guide/library.md) | 本地/音源搜索、导入收藏、本地歌单、混合歌单、云端写入、批量选择 |
| [播放、队列与歌词](docs/guide/playback.md) | 音质、音量补偿、播放模式、队列、片段、歌词页、睡眠定时 |
| [下载与本地优先](docs/guide/download.md) | TUI 下载、下载管理页、本地优先、CLI 下载参数 |
| [跨设备同步](docs/guide/sync.md) | 同步页与冲突页、SSH/HTTPS、自动同步、安全模型 |
| [系统媒体与封面](docs/guide/sysmedia.md) | Linux MPRIS、封面缓存、单实例 |
| [主题](docs/guide/theme.md) | 佐洛红 / 半拍蓝、深色/浅色与 `COLORFGBG`、自定义颜色 |
| [已知限制](docs/guide/limitations.md) | 音源、播放、下载、同步、平台、终端各处的边界 |
| [数据与隐私](docs/guide/privacy.md) | 本地存储、网络访问、同步加密、免责声明 |

### 参考

| 文档 | 内容 |
| --- | --- |
| [按键速查](docs/reference/keys.md) | 默认键位全表、鼠标、媒体键 |
| [自定义快捷键](docs/reference/keymap.md) | `keymap.toml`、动作 ID、冲突处理与版本迁移 |
| [CLI 子命令](docs/reference/cli.md) | 全部子命令、环境变量、数据目录位置 |

### 开发

| 文档 | 内容 |
| --- | --- |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 分层、模块清单、数据流、关键契约、事件循环约束 |
| [docs/adr/](docs/adr/README.md) | 架构决策记录（含废弃与修订） |
| [docs/sync-design.md](docs/sync-design.md) | 同步设计、威胁模型与实施拆分 |
| [开发与发布](docs/guide/development.md) | `make check`、UI 帧尺寸测试、GoReleaser、AUR |
| [ROADMAP.md](ROADMAP.md) | 已完成与下一步 |
| [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) | 第三方依赖与许可 |

## 第三方

核心依赖 [go-musicfox/netease-music](https://github.com/go-musicfox/netease-music)（MIT）、系统 [FFmpeg](https://ffmpeg.org/)（外部运行时依赖，不随本项目分发）。完整许可与 UNM 关闭说明见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

## 免责声明

本项目仅供学习、研究与技术交流，不用于商业用途，不承诺平台兼容性或问题响应速度；B 站与网易云的音频、封面、商标等内容版权归对应平台及权利人所有，本项目不提供、不托管任何音源内容，也不绕过版权或会员限制。使用可能遇到账号风控、封禁、接口失效或数据丢失，风险自担。完整条款见 [数据与隐私](docs/guide/privacy.md#免责声明)。

本项目与 B 站、网易云官方无关联，也未获得其授权或认可。

## 许可证

[AGPL-3.0](LICENSE)。
