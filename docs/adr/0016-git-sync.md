# 0016 跨设备同步：git 仓库 + 明文歌单快照 + 加密登录态

- 状态：已接受
- 日期：2026-11-01
- 关联：ADR-0012（可配置快捷键）、ADR-0013（片段与云端写入）、ADR-0015（player/downloader 边界）
- 设计：[docs/sync-design.md](../sync-design.md)

## 背景

应用的数据全部在本地：SQLite 里的歌单/队列/设置/登录态、`keymap.toml` 与下载文件。
跨设备只能手动重建歌单、重新扫码。需求是：

- 多设备同步本地/混合歌单、`keymap.toml`、单曲注释与登录态；
- 传输不绑定某一家托管服务，用户自己创建仓库；
- 冲突只出现在「同一实体两端都改」时，用户按实体整版选择；
- 登录态在仓库里只以密文存在，口令不上云；
- 同步失败不阻塞播放，状态可见、可恢复。

## 决策

### 传输用系统 git，快照用文本 JSON

调用系统 `git`，兼容 HTTPS/SSH 与任意托管（GitHub、Gitea、本地裸仓库）。认证、代理、
SSH 都交给 git，项目不接各家 API，也不自动创建仓库。

仓库布局：

```
meta.json                 # 仓库格式 v1、KDF 参数、全局盐、keycheck
keymap.toml               # 仅当本地存在时导出
playlists/<id>.json       # 一个实体一个文件，kind=local/mixin
annotations/<song>.json   # 单曲歌词偏移与片段，一歌一文件
accounts/<source>.age     # 认证加密后的登录态
.gitattributes            # 所有实体文件 merge=binary
```

一个实体一个文件让 diff 干净、冲突粒度可控；不提交 SQLite（二进制、WAL、无法合并）。

### 连接方式由 URL 派生，可一键切换

不单独保存 `auth` 字段：`http(s)://` 即 HTTPS（系统 credential helper 优先，
`sync-token` 兜底），`git@host:path` / `ssh://` 即 SSH（ssh-agent 与 `~/.ssh/config`）。
同步页提供「连接方式」的一键切换，只改写 `remote`；无法无歧义推导的地址（自定义
端口、ssh 别名、`~/` 路径）不猜，直接让用户手填。

`sync-key.remote_hash` 哈希的是归一化后的仓库身份 `host/path`（去掉 scheme、端口、
`.git` 后缀与首尾斜杠），因此同一仓库的 SSH / HTTPS 写法得到相同结果，切换连接方式
不会让本机密钥或登录态失效。

### 关闭 JSON 行级合并，同实体双改一律整版选择

`.gitattributes` 对上述文件统一 `merge=binary`。这不是按字节合并，而是禁止行级合并：
同一文件两端都改就进入冲突，不同文件互不影响。冲突按实体呈现「保留本地 / 保留远程 /
保留两者（歌单）」，落选版本与本地当前版本都归档到
`~/.config/zero-beat/sync/archive/`，零数据丢失。

解决冲突时不直接 push 覆盖：重新 `git merge --no-commit` 制造同样的冲突现场，按选择写回
工作区，提交 merge commit 保留双方历史，再导入、推送。「保留两者」生成
`<原 ID>-copy-<n>`、标题加「（副本）」。冲突页是独立页面。

### 凭据用 Argon2id + XChaCha20-Poly1305，按音源密封

```
口令 + meta.json 的 16 字节盐 → Argon2id(m=64MiB,t=3,p=1) → 32 字节主密钥
明文 {source, updated_at, device, session} → XChaCha20-Poly1305(AAD="zero-beat|credentials|v1|<source>|<key_version>")
```

- 盐全局、随机、公开，只在初始化时生成；`meta.json` 不存在时绝不重写盐。
- `keycheck = SHA-256(HKDF-SHA256(key, info="zero-beat:keycheck:v1"))`，用独立派生值
  校验口令，不复用 AEAD 密钥；在输入口令的新设备上先判断对错。
- 每次加密新 nonce，先认证后解析；认证失败只意味着密钥不对或文件被篡改，绝不当作
  内容冲突覆盖本地。
- 本地只缓存派生密钥 `sync-key`（0600），不缓存口令；`sync-key` 不进仓库、日志或
  `sync.toml`。
- KDF 参数只接受白名单（`m=65536,t=3,p=1,key_len=32`），防止远端塞入超大参数做 DoS。
- 登录态冲突做后写者胜（`updated_at`）；只有时间戳相同但内容不同时提示并保留本地。
- 登出不传播：只清本机 `accounts` 与内存会话，不删除远端密文。

### 自动同步默认开启

启动后台拉取，歌单/键位/注释/登录态变更后 debounce 30 秒推送；网络失败按
5s → 30s → 2m → 10m 退避，上限后暂停到手动重试；冲突时冻结自动同步。所有网络/磁盘
操作在 `tea.Cmd` 或 `sync.Manager` goroutine 里，`Update` 内不跑长任务。

并发保护：进程内 `Client` 互斥 + `Manager` 单飞；进程间 `flock` 文件锁；导出前记录
`app.LibraryRevision()`、导入前比较，本轮期间用户改过库就丢弃本次合并（`git reset --hard`
到本轮提交），下一轮重试，避免用旧快照覆盖用户刚做的修改。

### 本地文件

```
~/.config/zero-beat/
├── sync.toml          # remote/branch/auto，0600，不含 token
├── sync-key           # 派生密钥缓存，0600
├── sync-token         # 可选的 HTTPS token 兜底，0600
└── sync/
    ├── repo/          # 受管工作克隆
    ├── state.json     # 设备 ID、上次同步、待处理冲突（不进仓库）
    ├── lock
    ├── conflicts/<txid>/   # index.json + 每个实体的 .local/.remote/.base
    └── archive/<time>/     # 删除与冲突解决后的归档
```

token 优先使用系统 credential helper；允许把 token 存成 0600 文件作为兜底，经
`GIT_ASKPASS` 注入，不进入 argv、不写进 git 配置、不在页面长期显示。

### 删除与导入

- 本地删除歌单 → 导出时清空 `playlists/` 后重写，文件消失，提交记录删除。
- 远端删除 → 导入前归档本地快照，再删除本地 `local/mixin` 记录；`remote` 镜像永不被
  这条逻辑删除。
- `keymap.toml` 的本地删除与远端删除都不传播：一端没有配置文件不会删掉另一端的键位。
- 新设备首次同步先把远端树导入本地库，再导出，避免空库把远端歌单删光。

### 评审项的结论（原设计文档 §18）

1. 自动同步默认**开**。
2. token **允许** 0600 本地兜底，优先 credential helper。
3. 拉取后**立即**回填 source session（下一首起生效）。
4. 登出**不传播**。
5. 单曲注释**纳入首版**，一歌一文件。
6. 冲突做成**独立页面**。
7. 「保留两者」采用 `<原 ID>-copy-<n>`、标题加「（副本）」。
8. 首次配置必须勾选「歌单会以明文托管」。
9. `keymap.toml` 远端删除**不传播**。

## 理由

1. 用 git 做传输与版本存储，用户自带托管、历史可回滚，不需要服务端。
2. 一实体一文件 + 禁止行级合并，让冲突可解释、可逐项选择，不会出现「合并成功但语义错」。
3. 明文歌单让 diff/三方合并可用；只加密凭据，避免全库 E2E 摧毁合并能力。
4. LWW 处理 cookie 这类无法语义合并的数据，避免每次换 cookie 都打断用户。
5. 归档 + merge commit 保证任何选择都能回滚，降低同步的心理成本。

## 后果

- **歌单以明文托管**：初始化时强制确认；私有仓库只是访问控制，托管方/自建运维能读到
  歌单与历史版本；不做自动公开化检测。
- **git 历史不可撤销**：删掉的歌单、旧密文仍在历史里；轮换口令不等于撤销历史。
- **账号风控**：同步 cookie 可能被平台判定异常，失败时回落扫码登录。
- **同一实体双改冲突变多**：用一实体一文件 + 批量选择缓解。
- 确定性导出与随机 nonce 的矛盾：登录态在「明文未变」时复用已有密文，避免每轮产生
  无意义提交；明文变化才重新加密。
- 换口令时先用旧密钥同步一轮并入远端最新登录态，再换盐换密钥重加密；无法解密的旧
  密文从当前树移除（仍在 git 历史里），避免 meta 版本与密文版本错位。
- `LibraryRevision` 是 `internal/app` 的包级原子计数器：TUI 每个 `tea.Cmd` 都新建
  `app.Service`，字段级计数器无法跨命令比较。
- CLI 的 `sync resolve` 可以逐实体选择，选择先写进冲突目录的 `choices.json`，全部选完
  才应用；TUI 的选择只存在内存里。
- TUI 由 `internal/instance` 保证同一数据目录单实例；同步另有 `sync/lock`。
  CLI 子命令不占 TUI 锁，多进程同时写库仍由同步锁与 revision 检查兜底。

## 替代方案

- **直接同步 SQLite 文件**：二进制、WAL、平台差异，无法合并，放弃。
- **每家托管的 API**（GitHub Contents API 等）：要对接多套认证与限流，且不支持自建
  git，放弃。
- **全库 E2E 加密**：diff 与三方合并全部失效，冲突只能整库选择，放弃。
- **JSON 行级合并 + 语义合并**：播放列表顺序、删除/修改的语义难以自动判断，容易产生
  「合并成功但歌单错」，放弃。
- **凭据进冲突 UI**：cookie 无法语义合并，用户也没有判断依据，改为 LWW。
