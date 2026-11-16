# 数据与同步

## 三类数据

| 类别 | 位置 | 跨设备 | 谁写 |
| --- | --- | --- | --- |
| SQLite | Linux `~/.config/zero-beat/zero-beat.db`；macOS `~/Library/Application Support/zero-beat/`；Windows `%APPDATA%\zero-beat`；Android 应用沙盒 | 否 | 只由 Go 核心 |
| 设备文件 | 下载、封面缓存、实例锁、sync 克隆 | 否 | Go 核心 |
| sync 仓库 | 用户自己的 git 仓库（`sync/repo`） | **是** | 只由 Go 核心 |

Dart 永远不直接读写 SQLite 或 sync 仓库；所有数据操作走 RPC。

## sync 仓库：唯一的跨端契约

格式见 zero-beat 的 [同步设计](https://github.com/Sheyiyuan/zero-beat/blob/main/docs/sync-design.md) 与 [ADR-0020](https://github.com/Sheyiyuan/zero-beat/blob/main/docs/adr/0020-sync-format-v2.md)。v2 布局：

```
meta.json                      # 版本、盐、keycheck；meta.v=2
playlists/<id>.json            # 歌单（一实体一文件，merge=binary）
annotations/<song>.json        # 单曲注释：歌词偏移/绑定、单曲补偿、片段
keymap.toml                    # 键位覆盖
accounts/<source>.age          # 登录态密文（Argon2id + XChaCha20-Poly1305）
.gitattributes                 # merge=binary
```

同步的内容（与 TUI 完全一致）：

- 本地歌单、混合歌单；混合歌单中的远端引用只保存定位信息，导入侧补占位后按平台刷新，不同步完整远端镜像；
- 单曲注释：歌词偏移、歌词绑定（portable）、单曲音量补偿、片段；
- `keymap.toml`；
- 加密后的登录态（accounts LWW，按密文内 `updated_at`）。

**不同步**（设备态 / 本机数据）：

| 数据 | 原因 |
| --- | --- |
| `queue`、播放进度 | 跟设备走；换设备续播没有意义 |
| 下载文件与 `downloads` 索引 | 路径/文件是本机的 |
| 全局音量、全局补偿、自动响度、音质、模式、鼠标偏好 | 设备偏好 |
| 文件夹歌单、仅本机歌单、本地曲目及其注释 | 依赖本机文件路径 |
| 远端歌单镜像、每日推荐 | 按平台刷新；只有混合歌单中的引用信息随快照传递 |
| 本地歌词绑定（`local_file` / `local_song`） | 指向本机文件 |
| 主题（颜色） | 目前不进仓库；GUI 默认跟随系统深浅色 |

## Git 后端：CLI 优先 + go-git 兜底（TUI/GUI 共用）

手机、Windows、以及没装 git 的 Linux 都不保证有 `git` 可执行文件；**TUI 也要能不依赖系统 git 同步**。所以兜底必须放在 zero-beat 的 sync 包里，而不是 GUI 仓库：

- `auto`（默认）：Linux/macOS 优先可用 CLI，否则 go-git；Windows/Android 优先 go-git；
- 强制选择：`ZERO_SYNC_BACKEND=cli|gogit`（或 `sync.toml` 配置），TUI 的 `zero sync --backend gogit` 可直接验证；
- Android、Windows 默认 go-git；Linux/macOS 默认 CLI，但缺失时自动回退；
- 两个后端共用同一套快照、导入、冲突索引、加密与合并逻辑；GUI 只通过 `sync` 调用。

后端在同步开始前选择，整轮操作固定使用一个后端。网络、认证、冲突等业务失败不触发中途切换；启动探测发现 CLI 不可用时才回退，手动切换也必须先结束/恢复上一轮事务。

go-git 后端要覆盖 init/clone、fetch、push、checkout、add、commit、reset、status：

- 认证：HTTPS token 或 SSH key（`x/crypto/ssh`），密钥放应用沙盒；
- 合并由共享的实体合并器完成；后端只提供传输、对象读写与引用更新。不能依赖 go-git 的 fast-forward merge 实现分叉合并，也不能让 CLI 另用一套 `git merge` 决策。
- 冲突输入为固定提交的 `base/local/remote` 字节及存在标记；删除不能用空字节文件表示。CLI 从 `git show <commit>:<path>` 读取，go-git 从对应 tree 读取，不依赖 index stage 1/2/3。
- 现有 `Git.Merge`、`collectConflicts`、`Resolve` 和恢复逻辑需要共同重构。冲突实体展示保持兼容；本地事务元数据若变化须版本化并拒绝不兼容恢复，不能把它当成仅替换 git 命令的工作。

### 合并历史与事务

1. 获取同步锁，fetch 并固定远端头 `R`；先对当前本地头与 `R` 执行下述写前格式校验，通过后才导出本地改动形成头 `L`，记录导出时的库 revision。`L`、`R` 及后续冲突均用提交哈希引用，不使用随下一次 fetch 改变的名字。
2. 检查祖先关系：`L == R` 无需合并；`L` 是 `R` 的祖先则快进；`R` 是 `L` 的祖先则直接推送 `L`。分叉时计算真实 merge-base，不能直接使用任意“上次同步 base”。缓存仅在验证它仍是本次算法选定的共同祖先后复用。
3. v1 支持唯一 merge-base；多个最佳共同祖先或两条非空但无共同祖先的历史，明确报不支持并保留本地数据，不猜基准。空远端初始化、新设备首次 clone 单独走初始化流程，不属于无共同祖先合并。
4. 对 base/`L`/`R` 路径并集作实体级三方比较：两边相同取共同结果；一边未改取另一边（包括删除）；两边不同且都修改则冲突。delete/modify、add/add 不同内容也必须冲突；accounts 沿用现有解密后 LWW 及同时间戳处理，`meta.json` 的密钥/格式冲突阻止自动合并。
5. 无冲突或完成选择后，创建合并提交 `M`，**父节点有且只有 `[L, R]`**，写入最终树。普通单父提交不能代替分叉合并。两个后端统一提交身份与消息规则；测试固定时间，生产一致性主要比较树与父节点，不要求不同时间产生同一提交哈希。
6. 引用更新使用预期旧头进行比较并交换；对象写入与提交完成后才更新引用。导入前复查库 revision，用户期间有新编辑则保留它、按现有 revision 保护重做本轮，不能用旧快照覆盖。
7. 冲突期间保存 `base/L/R`、库 revision 与三份实体快照；用户选择只适用于这组输入。恢复/提交前重新 fetch 并检查 revision；任一输入已变则重新合并并要求确认新冲突，不把旧选择盲套到新远端。
8. push 非快进时重新 fetch，带着已保留的本地提交重新计算祖先和合并；沿用核心的有界重试/退避，绝不 force push。记录本地事务阶段以便崩溃后恢复，不能仅保存“文件已合并”就宣称同步成功。

### 后端一致性验收与工作量

- CLI/go-git 运行同一套用例：空仓库、新设备、快进、双方离线修改不同/相同实体、delete/modify、add/add、accounts LWW、密钥冲突、无共同祖先与多个 merge-base。
- 检查最终文件树、冲突分类、合并提交两个父节点；远端头必须是待推送提交的祖先。完成后再同步一次，无额外内容改动、无重复冲突。
- 覆盖冲突期间用户编辑/远端推进、push 被抢先、进程中断恢复，以及 CLI→go-git→CLI 顺序切换同一受管克隆。
- 用现有 TUI 的 v2 样本与导入/导出测试固定字节格式；TUI 阶段实现共享后端与回归；P1 验证 Android HTTPS，P5 接入 GUI。

生产后端和模块迁移已前移至 TUI 工作；GUI P5 只计算配置、冲突 UI 与 RPC 接入的 1–2 人周。历史 6.5–8.5 人周后端估算不再重复计入 GUI 总量。

### 为什么不在 GUI 仓库 fork sync

「把 git 功能合进来当兜底」听起来省事，但同步是**字节级契约**：

- 快照 JSON 的字段顺序/缩进/时间格式只要差一点，git 就会把无改动误判为改动；
- 加密参数、accounts LWW、冲突文件格式任何一处不一致，都可能让另一台设备解密失败或丢数据；
- 一旦 GUI 仓库里有一份拷贝，TUI 修 bug 时它不会自动跟上，最后变成两个仓库、两种行为。

所以兜底的顺序是：**公开 SDK 优先 → go-git 后端内置 → 只读查看同步快照 → 延后同步**；不复制 sync 实现。只读模式不导入数据库、不导出或推送，也不能计为双向同步验收通过。

## 确定性序列化

跨端同步最容易踩的坑不是格式，而是**同一份内容序列化出不同字节**：

- JSON 字段顺序、缩进、结尾换行必须与 zero-beat 的导出一致；
- 保持 v2 现有字段类型与单位：如 `meta.created_at`、歌单 `created_at`、曲目 `added_at`、账号 `updated_at` 使用 Unix 秒整数；注释偏移/片段用毫秒，歌曲 `duration` 用秒。RPC 的 RFC3339/毫秒 DTO 规则不能套进同步文件；
- 一实体一文件的文件名规则一致；
- 任何「重新导出后字节不同」都会让 git 把无改动误判为改动，制造假冲突。

因此 v1 不在 Dart 里实现快照导出；快照与导入全部留在 Go，Dart 只展示冲突与选择。以锁定的核心版本及其 `testdata`/导出样本为规范，验证无改动的连续导出逐字节一致，包括登录态未变时复用已有密文。

## SQLite

- 位置：桌面统一复用 TUI `config.DataDir()`，Linux 尊重 `XDG_CONFIG_HOME`，macOS 为 `~/Library/Application Support/zero-beat`（App 关闭沙盒），Windows 为 `%APPDATA%\zero-beat`；Android 沙盒路径由 Flutter 传入，所有库/同步/缓存路径从该根目录注入，不得再次调用桌面默认路径；
- 打开参数：`journal_mode(WAL)`、`busy_timeout(5000)`，与 zero-beat 一致；
- 迁移：只由 Go 的 `store.migrate()` 执行；桌面必须先取得实例锁，再调用会建表/迁移的 `store.Open()`；GUI 不直接建表、不改 schema；
- 兼容：固定依赖核心版本，并验证“GUI 写入/迁移后，受支持的 TUI 仍可读取”；同步仓库的 `meta.v` 不代表 SQLite schema 版本。未验证的旧 TUI/降级组合不承诺可读，不能只靠先启动一次核心判断兼容；
- 备份：导出/导入功能后续再加；至少提供「打开数据目录」与「清空缓存」。

## 移动端存储与权限

- 下载默认写应用沙盒（`Downloads/` 子目录），通过系统分享或「导出到文件」交给用户；
- Android 10+ 不做「任意路径写入」；需要公共目录时用 MediaStore（后续）；
- 封面缓存放应用缓存目录，受系统清理影响，可重新下载；
- 文件夹歌单（SAF 目录树）不在 v1；本地文件先支持「从文件选择器导入单曲」，后续再做目录扫描；
- 本地库的内容指纹必须与桌面算法一致（zero-beat 已有跨平台已知答案测试），否则同一文件在两端会得到不同身份。

### Android 下载：完整媒体流直接落盘

**当前缺口**：`zero-beat-core/download.Run` 在下载后必经 `ffmpegRemux`，使用 `exec.LookPath/CommandContext`；把它包装成 SDK 不会使其在 Android 可用。media_kit 提供播放能力，不提供这条下载封装链路。

v0.1 采用以下设计，决策见 [ADR-0009](decisions/0009-android-download.md)：

- Go 下载器拆分为共享 HTTP 传输/续传、可注入的完成处理器、最终索引写入。桌面仍使用 ffmpeg 完成处理器；Android 使用完整媒体流落盘处理器，不运行外部 ffmpeg，也不在 Dart 重写下载任务队列。
- Android 只接受可由单次 HTTP 资源完整保存、并通过容器检查的音频。P1 先验证 B 站完整 M4A/MP4 音频资源与网易云 MP3/FLAC 等实际样本；HLS/DASH 清单、缺少初始化段的碎片、未知容器不在支持范围。编码名称与容器不同，不能直接复用按 codec 推断扩展名的规则。
- 先写稳定 `.part` 文件，校验响应范围、长度与资源身份；Range 被忽略则重下，资源发生变化不能拼接旧片段。取消/失败不写成功索引；完成校验后同文件系统原子改名，再登记 `downloads`，索引失败留下的文件须可在重试时重新校验和登记。
- Android 保留原容器和已有标签，不承诺写入标题/艺人标签。应用内展示使用 Go 曲库元数据；分享导出会明确保留原媒体标签。重封装、转码和嵌入标签另行评估原生/libav 后端，不隐含纳入 v0.1。
- 能力同时报告完成处理器与已验证容器；不支持的流返回明确错误，不伪装成下载成功。断网时命中有效下载必须直接本地播放；文件损坏时报告本地失败，联网才尝试远程回落。

P1 必须在 Android 真机、未安装 ffmpeg 的环境下覆盖两音源的支持样本、取消/续传和飞行模式播放。P3 再交付批量任务、下载管理与分享；若必需音源无法完成此闭环，必须修改下载方案和排期，不能以桌面下载通过替代手机验收。

### macOS / Windows 的差异

- macOS：关闭 App Sandbox 后可直接访问用户目录与 `~/Library/Application Support/zero-beat`，与 TUI 共用；下载/本地库不经过 SAF/MediaStore；
- Windows：`%APPDATA%\zero-beat`；下载/本地库可用普通文件路径，但仍受杀软与权限影响；
- 两个平台的下载都依赖 ffmpeg（Homebrew / 随包 / 检测），没有 ffmpeg 时只禁用下载，不阻塞播放与同步。

## 隐私与安全

- 登录态在 SQLite 里是明文 JSON（与 TUI 现状一致）；Android 后续可用 Keystore 包一层；
- 同步仓库里的账号是 XChaCha20-Poly1305 密文，口令只在本机内存；
- RPC 只绑回环；日志不打印 URL query、Cookie、Token；
- 与 TUI 相同的免责声明：音源内容版权归平台，项目不托管内容。

## 兼容与升级

- 读 `meta.json` 的 `v`：`v=1` 可读并按 zero-beat 规则升级；`v=2` 正常；`v>2` 拒绝写入并提示升级 App；
- 当前核心使用严格 JSON 解码（`DisallowUnknownFields`），并没有未知字段往返保留能力。v1 不承诺透传未知字段，也不在 v2 内自行扩展格式；新字段须先经过核心的兼容/版本决策。
- SDK/双后端实现需新增写前校验：对本地头和 fetched 远端树中受管实体做格式检查；遇未知字段/不支持版本应在数据库导入、快照覆盖、提交或 push 前中止写同步，提示升级。现有部分导入路径“警告后跳过实体”不能作为可安全重新导出的依据。可解密账号也做严格校验；不能解密的账号沿用现有密钥异常处理，保留原密文，不覆盖成空账号。
- 验收包含带未知字段的 v2 样本：除 fetch 的对象/远端引用与错误记录外，本地数据库、工作树及本地分支均不被改写，不执行 push。只读查看快照可用于诊断，不能随后自动导出覆盖。
- 多设备升级顺序：先升级所有客户端，再让任意一台执行同步（与 zero-beat v0.7.0 的说明一致）。
