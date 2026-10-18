# hbp（暂定名）

**跨平台音乐终端播放器。** 一个歌单里可以同时装 B 站和网易云的单曲，某个平台放不了，就自动换另一个平台的版本。

> **状态：脚手架阶段。** 仓库里只有骨架、接口契约和文档，还不能播放。进度见 [ROADMAP.md](ROADMAP.md)。
>
> 项目名待定，候选与讨论见 [docs/NAMING.md](docs/NAMING.md)。

---

## 为什么重写

v1（[Half Beat Player](https://github.com/Sheyiyuan/Half-Beat-Player)）是 Wails 桌面应用：B 站单源，19,742 行前端绑死在 Web 技术上。新项目要做三件 v1 做不到的事：

1. **双源**：B 站 + 网易云，各自独立登录，共用一套曲库与歌单。
2. **混源歌单**：一首歌可以挂多个平台的版本，互为备份。网易云灰歌就放 B 站那版，B 站视频被删就放网易云那版。
3. **终端优先**：SSH 可用、键盘驱动、单文件分发，不依赖 WebView。

## 与 v1 的关系

| | v1 | 本仓库 |
| --- | --- | --- |
| 形态 | Wails 桌面应用 | TUI + CLI |
| 音源 | B 站 | B 站 + 网易云（可扩展） |
| 代码 | 冻结，只修 bug | 搬家 + 重构 |

领域层（数据模型、数据库、歌词解析、网络防护、业务逻辑）从 v1 搬迁，不做"从零重想业务"；前端与 Wails 绑定全部丢弃。用户数据通过 v1 数据库导入器迁移。

## 目录结构

```
cmd/hbp/              CLI 入口（子命令骨架）
internal/
  domain/             与平台无关的领域模型（Track / TrackSource / Quality）
  store/              SQLite 表结构、迁移、v1 导入器
  source/             音源抽象与注册表
    bilibili/         B 站实现（从 v1 搬迁）
    netease/          网易云实现（基于 netease-music）
  match/              跨源匹配（判断两个条目是不是同一首歌）
  resolve/            选源、回退、预取、续签
  audio/              播放引擎、解码后端（native / mpv）、DSP
  lyrics/             歌词解析与多源聚合
  tui/                Bubble Tea v2 界面
  config/             TOML 配置
docs/
  adr/                架构决策记录
  design/             两份前置评估（可行性、多源方案）
```

## 快速开始

需要 Go 1.24+ 能编译当前骨架；**引入 charm v2 与 oto v3 后要求 Go 1.26+**（见 `go.mod` 演进说明）。

```bash
make build      # 产出 bin/hbp
./bin/hbp       # 打印用法
make check      # gofmt + vet + test
```

## 关键设计

- **三层职责**：`domain` 与平台无关 → `source` 做平台适配 → `resolve` 决定这一首从哪放。
- **唯一契约**：音源交给播放引擎的只有 `Stream{URL, Headers, Codec, ExpiresAt}`，平台差异不外泄。
- **直链不落库**：B 站签名约 2 小时、网易云更短，落库就是负担。
- **能力驱动 UI**：界面看 `Capability` 位决定显隐，不写 `if source == "bilibili"`。

详见 [ARCHITECTURE.md](ARCHITECTURE.md) 与 [ADR](docs/adr/README.md)。

## 文档

| 文档 | 内容 |
| --- | --- |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 分层、核心契约、禁止事项、依赖选型 |
| [ROADMAP.md](ROADMAP.md) | 阶段划分、验收标准、M0 spike 清单 |
| [docs/design/01-tui-feasibility.md](docs/design/01-tui-feasibility.md) | TUI 可行性评估（音频实测、终端难点、工时） |
| [docs/design/02-multi-source-plan.md](docs/design/02-multi-source-plan.md) | 多源方案（数据模型、匹配、回退、风险） |
| [docs/adr/](docs/adr/README.md) | 架构决策记录 |
| [docs/NAMING.md](docs/NAMING.md) | 命名候选与结论（待定） |

## 命名

`hbp` 是占位名。候选与取舍见 [docs/NAMING.md](docs/NAMING.md)。定下来后：

```bash
scripts/rename.sh onbeat          # 改模块路径、包引用、cmd 目录、文档
```

## 许可证

[AGPL-3.0](LICENSE)，与 v1 保持一致；领域层代码搬迁自 v1。音乐内容版权归各平台与权利人所有，本项目不提供任何音源内容。
