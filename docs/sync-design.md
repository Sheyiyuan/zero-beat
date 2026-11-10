# 同步设计：Git 仓库、歌单快照与加密登录态

- 状态：已落地（2026-11-01）
- 日期：2026-11-01
- 关联：ADR-0012（可配置快捷键）、ADR-0015（player/downloader 边界）、[ADR-0016](adr/0016-git-sync.md)（同步决策）
- 实现：`internal/sync/`、`cmd/zero/sync.go`、`internal/tui/sync.go`

## 0. 先看这五个审查点

1. **歌单以明文存放在私有仓库**，用仓库访问控制代替内容加密；历史版本不会随删除消失。
2. **登录态采用后写者胜（LWW），不进入冲突选择界面**；只有密钥不匹配/时间戳相同但内容不同才提示。
3. **关闭 JSON 的行级合并**，同一实体两端修改一律交给用户整版选择；换来行为可预测，牺牲“自动合并互不重叠的改动”。
4. **登出不传播**：A 设备退出登录不会让 B 设备掉线，远端密文也不主动删除。
5. **自动同步默认开启**（启动拉取、变更后 debounce 推送）；如果希望默认关闭，需要现在定。

## 1. 背景与现状

应用的数据全部在本地 `~/.config/zero-beat/`：

- SQLite：`songs / playlists / playlist_items / playlist_refs / playlist_excludes / queue / settings / accounts / downloads`，见 [store.go](../internal/store/store.go#L95-L160)。
- `keymap.toml`：动作 ID 覆盖默认键位，导入时必须过 `keymap.LoadWithWarnings` 的冲突校验。
- 登录态：`accounts.payload` 是明文 JSON（SESSDATA / MUSIC_U 等），见 [store.go](../internal/store/store.go#L1161-L1191)。
- 设备态：`queue`、`downloads.path`、音量/音质/模式等 `settings` 按设备各自不同。

目前没有任何导出/迁移能力。跨设备只能手动重建歌单、重新扫码。

## 2. 目标与非目标

**目标**

- 用 GitHub 私有仓库或任意自建 git 服务作为传输与版本存储。
- 多设备自动同步本地/混合歌单、`keymap.toml`、单曲注释（P2）、加密后的登录态。
- 冲突仅在“同一实体两端都改”时出现，用户按实体选择保留哪一版。
- 凭据在仓库中只以密文形式存在；口令不上云，新设备只输一次。
- 同步失败不阻塞播放，状态可见、可恢复。

**非目标**

- 实时协作、秒级同步；git 的延迟和冲突模型不适合。
- 同步 `queue`、`downloads`、音量/音质/模式/鼠标开关等设备态。
- 同步 `kind=remote` 的远端歌单镜像和每日推荐入口（可由平台重建）。
- 加密歌单；不做全库 E2E 加密（会摧毁 diff 与三方合并）。
- 自动创建 GitHub 仓库、注册 OAuth App；仓库由用户手动创建。
- 替用户绕过平台风控；登录态同步只是“省一次扫码”。
- Windows 适配（项目现状即如此）。

## 3. 决策摘要

| 主题 | 决策 | 理由 |
| --- | --- | --- |
| 传输 | 调用系统 `git`，兼容 HTTPS/SSH 与任意托管 | 自建服务无需对接各家 API；认证、代理、SSH 由 git 处理 |
| 快照 | 文本 JSON，一个实体一个文件 | diff 干净、冲突粒度可控；不提交 SQLite |
| 合并 | git 三方合并 + `merge=binary` 关闭 JSON 行级合并 | 删除/修改交给 git；同实体并发修改必冲突，不出现“合并成功但语义错” |
| 冲突 | 按实体选择本地/远程/保留两者，落选版本归档 | 不做语义合并；零数据丢失 |
| 凭据 | Argon2id + XChaCha20-Poly1305，每音源一个密文文件 | 认证加密、密文可整体覆盖；不参与文本合并 |
| 盐 | 全局随机盐写入仓库 `meta.json` | 新设备只输口令即可派生同一密钥，不需要背盐或拷贝密钥 |
| 本地密钥 | `~/.config/zero-beat/sync-key`，0600，存派生密钥 | 跨重启自动同步；口令可能被复用，存派生密钥比存口令好 |
| 凭据冲突 | 常规 LWW（`updated_at` 优先），不弹冲突；仅时间戳相同但内容不同时提示 | cookie 无法语义合并；旧密文重放用单调时间挡掉 |
| 自动同步 | single-flight + debounce + 退避；冲突时冻结 | 不阻塞 UI，不重复推送，不堆积冲突 |
| 登录态应用 | 拉取后立即回填 source session（待确认） | 新设备首次启动无感；失败回落到扫码 |

## 4. 同步范围

| 数据 | 是否同步 | 载体 | 说明 |
| --- | --- | --- | --- |
| `kind=local` 歌单 | 是 | `playlists/<id>.json` | 含曲目元数据、顺序、`created_at` |
| `kind=mixin` 歌单 | 是 | `playlists/<id>.json` | 含 `refs`、`excludes`、自己加入的曲目 |
| `kind=remote` 镜像、每日推荐 | 否 | — | 打开时按平台刷新；混合歌单引用它时在导入侧补占位 |
| `keymap.toml` | 是 | `keymap.toml` | 原样同步；导入前校验 |
| 单曲歌词偏移、单曲音量补偿、片段 | 是（P2） | `annotations/<song>.json` | 按歌曲 ID 稳定；一歌一文件避免整文件冲突 |
| 登录态 | 是 | `accounts/<source>.age` | 加密；按 `updated_at` LWW |
| 音量/全局补偿/自动响度均衡/音质/模式/鼠标/本地优先/当前歌单 | 否 | — | 设备态，跨设备同步会互相覆盖；单曲补偿在注释里 |
| `queue`、`downloads` | 否 | — | 设备态；下载路径是绝对路径 |
| SQLite 文件 | 否 | — | 二进制、WAL、无法合并 |

## 5. 仓库布局

```
zero-beat-sync/
├── meta.json
├── keymap.toml                  # 仅当本地存在时导出
├── playlists/
│   ├── local-<id>.json
│   └── mix-<id>.json
├── annotations/
│   └── <url-escaped-song-id>.json   # 歌词偏移/绑定、补偿与片段
├── accounts/
│   ├── bilibili.age
│   └── netease.age
└── .gitattributes
```

`.gitattributes`：

```gitattributes
playlists/*.json      merge=binary
annotations/*.json    merge=binary
keymap.toml           merge=binary
meta.json             merge=binary
accounts/*.age        merge=binary
```

`merge=binary` 的作用不是“按字节合并”，而是**禁止行级合并**：同一文件两端都改就进入冲突，符合“整版选择”的产品语义。不同文件互不影响，因此同时在两台设备改不同歌单仍然可以自动合并。

## 6. 快照格式

### 6.1 `meta.json`

当前格式为 `v=2`（v0.7.0 起）。`v=1` 仓库仍可读取，客户端会在首次成功同步后原地升级，升级不可回退。

```json
{
  "v": 2,
  "key_version": 1,
  "kdf": {"alg": "argon2id", "m": 65536, "t": 3, "p": 1, "key_len": 32},
  "salt": "base64-16-bytes",
  "keycheck": "base64-hmac",
  "created_at": 1761980000
}
```

- `salt` 只生成一次；只有显式更换口令时才重建。
- `keycheck = SHA-256(HKDF-SHA256(主密钥, info="zero-beat:keycheck:v1"))`，用独立派生值校验口令，不复用 AEAD 密钥；它用于在新设备输入口令后先判断对错，不能反推主密钥（任何校验值都允许离线猜口令，这是 KDF 成本参数要足够高的原因）。
- 不记录每次同步时间、App 版本、设备名，避免无意义 diff。
- 版本高于客户端支持时拒绝同步；字段缺失时拒绝，不猜测。
- v1→v2 只改版本号，不重新派生密钥：新增「仅本机」边界与注释里的 `lyric_bind` 字段。旧客户端读到 v2 会要求升级，不允许降级写回。
- 仓库已有 `meta.json` 时，`init` 必须转为“加入已有仓库”流程，绝不重写盐或密钥；否则所有设备会立刻无法解密。

### 6.2 歌单文件

```json
{
  "id": "local-1761980000000000000",
  "title": "通勤",
  "kind": "local",
  "created_at": 1761980000,
  "items": [
    {
      "source": "bilibili",
      "remote_id": "BV1xx411c7mD",
      "bvid": "BV1xx411c7mD",
      "page": 1,
      "cid": 0,
      "title": "曲名",
      "artist": "UP 主",
      "album": "",
      "duration": 213,
      "cover": "",
      "added_at": 1761980000
    }
  ]
}
```

混合歌单增加引用与隐藏：

```json
{
  "id": "mix-1761980000000000000",
  "title": "周末",
  "kind": "mixin",
  "created_at": 1761980000,
  "refs": [
    {"id": "local-1761970000000000000", "kind": "local", "title": "收藏"},
    {"id": "bili-fav-123", "kind": "remote", "source": "bilibili", "remote_id": "123", "title": "B站收藏夹"}
  ],
  "excludes": ["bilibili:BV1xx411c7mD:1"],
  "items": []
}
```

要点：

- 歌曲不写 `id`；导入时由 `(source, remote_id, page)` 重新派生，和 `store.SongID` 一致。
- `kind=remote` 的被引用歌单不在仓库中；导入时按 `id/source/remote_id` 调用 `EnsurePlaylist` 建占位，打开时照常刷新。远端镜像 ID 在适配器里是稳定的（`bili-fav-<mediaID>`、`netease-pl-<id>`）。
- `excludes` 保存歌曲 ID，顺序无关，导出时排序。
- `items` 的数组顺序就是 `position`，不单独写 position。
- 只有 `sync_excluded=0` 的歌单会被导出；文件夹歌单（`kind=folder`）与仅本机歌单不在仓库里，导入时遇到 `kind=folder` 直接跳过并告警。
- 引用文件夹歌单的混合歌单必须从创建时就是仅本机，因此仓库里不会出现指向 `folder` 的 `refs`。

### 6.3 确定性导出（硬性要求）

否则每次同步都会产生整文件 diff，冲突率会失控：

- 使用 struct 而不是 map；字段顺序由 struct 定义决定。
- 所有 map 在序列化前排序（`encoding/json` 对 map 的 key 排序，但不要依赖它承载语义）。
- 数组按业务顺序固定：曲目按 position，引用按 position，excludes 按 ID 字典序。
- 两个空格缩进、末尾一个换行、UTF-8、不做时间戳/主机名注入。
- 同一数据库连续导出两次，输出必须逐字节相同；用单测锁定。

### 6.4 删除语义

- 本地删除歌单 → 导出时删除对应文件 → 提交记录删除。
- 拉取到远端删除 → 导入前把本地快照归档，再删除本地 `local/mixin` 记录；`remote` 镜像永不被此逻辑删除。
- 导出采用“清空 `playlists/*.json` 后重写”的方式，避免残留文件复活已删除歌单；`accounts/` 不参与清空。
- `keymap.toml` 的本地删除**不传播**：远端文件保留，避免一台设备临时没有配置文件就删掉另一台的键位。

## 7. 同步流程

### 7.1 状态机

```
Idle
 └─ Pulling: fetch → 凭据 LWW（可选）→ 导出 → 提交 → merge
      ├─ Clean: 导入 → Push → Idle
      ├─ Conflict: 快照 ours/theirs → abort → Conflict
      └─ Error: 退避 → Idle
Conflict
 └─ Resolving: 用户逐项选择 → 重新 merge 并套用选择 → 提交 → 导入 → Push
KeyMismatch
 └─ 等用户输入口令/新密钥；只跳过凭据，歌单仍可同步
```

### 7.2 正常一轮

1. 取同步锁；检查工作区干净（残留 merge 则 `merge --abort`，残留脏文件则恢复到 HEAD）。
2. `git fetch origin <branch>`。
3. 若本地有密钥：解密远端 `accounts/*.age`，按 `updated_at` 做 LWW；远端较新则先导入本地库。失败则标记 `KeyMismatch`，跳过凭据，不影响歌单。
4. 导出快照到工作区：清空并重写 `playlists/`，按需写 `keymap.toml`、`annotations/`、`accounts/`。
5. 若工作区有变化：`git add -A`，`git commit -m "sync: <device> <timestamp>"`。
6. `git merge --no-edit FETCH_HEAD`。
   - 干净：合并结果即工作区。
   - 冲突：记录 `ours`（`:2:path`）、`theirs`（`:3:path`）、`base`（`:1:path`，可为空）到冲突目录，然后 `git merge --abort`，进入 `Conflict`。
7. 导入合并后的树（单实体事务，失败跳过并记录）。
8. `git push`。非快进则重新 fetch + merge + 重试（有次数上限，绝不 force push）。
9. 更新本地状态；发 `syncAppliedMsg`。

### 7.3 首次配置与加入已有仓库

- **初始化**：用户填远程 URL；远程为空时生成盐、派生密钥、写 `meta.json`，首次导出后提交并推送。
- **加入**：远程已有 `meta.json` 时，`init` 转为加入流程——读盐与参数，输入口令，校验 `keycheck`，导入快照，缓存密钥。绝不重写 `meta.json`。
- 不自动创建远程仓库；GitHub 空仓库或本地 `git init --bare` 都可以。
- 初始化时明确提示：歌单会以明文托管；登录态会以密文托管；登录态可能被平台风控失效。

### 7.4 并发保护

- **进程内**：`sync.Manager` 单飞，忙时合并请求。
- **进程间**：`sync/lock` 文件锁；拿不到锁直接跳过本轮。TUI 另由 `internal/instance`
  保证同一数据目录单实例；同步不依赖它，CLI 也能单独运行。
- **库写入竞态**：导出用只读事务拿一致快照；导入前检查 `LibraryRevision`（歌单/账号变更计数）。若本轮期间本地有写，放弃导入、下一轮重试，避免覆盖用户刚做的修改。
- 冲突处理期间允许用户继续编辑；点“应用”时以**当前本地状态**作为“本地版本”快照，避免丢掉冲突产生后的编辑。

## 8. 冲突模型

### 8.1 冲突粒度

每个实体一项：歌单文件、`keymap.toml`、`annotations/<song>.json`。凭据不在此列表，按 §9.3 的 LWW 处理。绝不提供“整仓保留本地/远程”作为唯一操作，否则会连带丢掉其他不相关改动。

### 8.2 选项

| 选项 | 行为 |
| --- | --- |
| 保留本地 | 用当前本地快照覆盖该实体；远端版本留在 git 历史 |
| 保留远程 | 导入远端快照；本地版本先归档 |
| 保留两者 | 歌单生成 `<原 ID>-copy-<n>`、标题加“（副本）”；键位/注释不提供 |
| 全部本地 / 全部远程 | 批量操作，仅作为快捷方式 |

每个选项必须展示：本地/远端曲目数、最后修改时间、是否删除、涉及哪些远端引用。没有这些信息用户只能盲选。

### 8.3 归档

- 冲突快照目录：`~/.config/zero-beat/sync/conflicts/<txid>/`，含 `index.json` 和每个实体的 `.local` / `.remote` 文件。
- 解决后整体移入 `~/.config/zero-beat/sync/archive/<时间>/`，同时保留两份，不只留落选方。
- 拉取导致的删除也先归档再删本地记录；归档不进入仓库。

### 8.4 重新合并

解决冲突时不直接 push 覆盖：

1. 重新 `git merge --no-commit FETCH_HEAD`，制造同样的冲突状态。
2. 按用户选择写入工作区（删除则 `git rm`），`git add -A`。
3. `git commit` 生成 merge commit，保留双方历史。
4. 导入、push。

这样即使选择“远程”，本地历史仍可回滚，不会丢数据。

### 8.5 不算冲突的情况

- 凭据两边都变：LWW，不弹窗；时间戳相同但内容不同才进冲突。
- 远端删除、本地未改：自动删除并归档。
- 本地删除、远端未改：自动删除远端文件。
- 删除 vs 修改：交给用户选。
- `meta.json` 冲突（两台设备同时换密钥）：不进入普通冲突 UI，停在“需要输入新密钥/人工处理”，因为选错会导致所有凭据不可解。

## 9. 凭据加密

### 9.1 威胁模型（详见 §14）

- 仓库读权限泄露、误设公开、被 fork、token 被偷：只拿到密文与歌单。
- 本地被完全攻破：本来就有的明文 cookie / 密钥 / token 全部暴露，加密不改善这一层。
- 仓库写权限泄露：可以篡改/回滚歌单，但无法伪造凭据密文（AEAD 认证）；可以 DoS。
- 托管方/自建运维：能读歌单，读不到凭据。

### 9.2 密钥派生

```
口令 + meta.json 中的 16 字节盐
  → Argon2id(m=64MiB, t=3, p=1, 输出 32 字节)
  → 主密钥（缓存到本地 sync-key）
```

- 盐：全局、随机、公开、只生成一次。它保证相同口令在不同同步配置下产生不同密钥，让预计算表无法跨仓库复用；它不让爆破变慢，慢的是 KDF 参数。
- 非机密：盐和 nonce 都存在文件里，可公开。
- `sync-key` 存派生密钥而不是口令：口令可能被用户复用在别处，派生密钥只对这一套同步有效。
- 口令建议至少 12 个字符或由密码管理器生成，不要复用账号密码；KDF 参数固定，不随仓库内容变化。

### 9.3 单音源密文

AAD 绑定来源和版本，防止把 bilibili 密文挪到 netease 文件：

```text
zero-beat|credentials|v1|<source>|<key_version>
```

信封：

```json
{
  "v": 1,
  "source": "bilibili",
  "key_version": 1,
  "nonce": "base64-24-bytes",
  "data": "base64-ciphertext-and-tag"
}
```

明文：

```json
{
  "source": "bilibili",
  "updated_at": 1761980000,
  "device": "a1b2c3d4",
  "session": {"SESSDATA": "..."}
}
```

实现示意：

```go
aead, _ := chacha20poly1305.NewX(key)      // XChaCha20-Poly1305
nonce := make([]byte, aead.NonceSize())     // 24 字节，每次新生成
rand.Read(nonce)
sealed := aead.Seal(nil, nonce, plaintext, aad)

plain, err := aead.Open(nil, nonce, sealed, aad) // 密钥错/篡改 → err
```

- 每次写凭据生成新 nonce，绝不复用；盐不变，因为它只喂 KDF。
- 先认证后解析：`Open` 成功后才 `json.Unmarshal`，再做字段校验。
- 认证失败只有两种解释：密钥不对或文件被篡改。提示“输入同步密钥”，**不得**当作内容冲突覆盖本地。
- 远端 `updated_at` 比本地旧则忽略，顺带挡住旧密文重放。

### 9.4 本地密钥文件

`~/.config/zero-beat/sync-key`（0600）：

```json
{
  "v": 1,
  "key_version": 1,
  "kdf": {"alg": "argon2id", "m": 65536, "t": 3, "p": 1, "key_len": 32},
  "salt": "base64",
  "key": "base64-32-bytes",
  "remote_hash": "sha256(remote-url)"
}
```

- 只是缓存；重新输入口令即可重建。
- `remote_hash` 用于发现“换了仓库但沿用了旧密钥”；哈希的是归一化仓库身份
  `host/path`（去 scheme、端口、`.git` 与斜杠），同一仓库的 SSH/HTTPS 写法一致。
- 不进仓库、不进日志、不进 `sync.toml`；`chmod 0600`。

### 9.5 新设备流程

1. 同步页填仓库 URL 与认证，点“从仓库恢复”。
2. 拉取 `meta.json`，读盐与 KDF 参数。
3. 输入口令，派生密钥，校验 `keycheck`。
4. 解密 `accounts/*.age`，导入登录态；缓存密钥到本机 `sync-key`。
5. 后续自动同步不再询问。

盐不需要用户记住，也不建议把 `sync-key` 当迁移文件；输入口令即可。若用户主动拷贝 `sync-key`，本质上是把密钥载体带走，需按秘密文件对待。

### 9.6 换口令

1. 旧密钥可用时：新盐 → 新密钥 → 用旧密钥解密全部 credentials → 用新密钥重加密 → 升 `key_version` → 更新 `meta.json` 与本地 `sync-key`。
2. 旧密钥已丢失时：只能用本地库里的明文登录态重新加密，远端旧密文留作历史。
3. 其他设备拉到更高的 `key_version` 时，先提示输入新口令，不要把它误判为“密钥损坏”；新口令校验通过前保留本地旧密钥与登录态。
4. 旧密文仍留在 git 历史，旧密钥持有者仍可解开；文档必须写明“轮换不等于撤销历史”。

### 9.7 登录态应用时机

- 已确认采用方案 A：拉取后立即 `SetSession` 回填，下一首开始生效；已解析的流不受影响。
- 本轮正在播放的源失败时回落到扫码/重新登录。

### 9.8 登出策略

- 登出只清本机 `accounts` 与 source 内存会话，**不删除远端密文**，不影响其他设备。
- 若 A 删除远端文件，B 下次推送会把自己仍有效的会话传回去，语义混乱；因此不做远端删除。
- 需要“全设备退出”时后续可加显式操作，不放进首版。

## 10. 自动同步

| 触发 | 行为 |
| --- | --- |
| 启动 | 后台 pull；不阻塞 TUI |
| 歌单/键位/注释变更 | debounce 30 秒后 push |
| 登录 / 会话变化 | debounce 30 秒后推送；登出只清本地，不推送远端删除（§9.8） |
| 冲突未解决 | 冻结自动同步，避免叠加；只允许手动“放弃本轮并重新拉取” |
| 网络/认证失败 | 退避 5s → 30s → 2m → 10m，上限后暂停直到手动重试 |
| 仓库为空/首次 | 自动初始化分支并推送 |
| 推送非快进 | fetch + merge + 重试，绝不 force push |

约束：

- 所有网络/磁盘操作在 `tea.Cmd` 或 `sync.Manager` goroutine 中完成，不进入 `Update`，符合 [ARCHITECTURE.md](../ARCHITECTURE.md#L191-L197)。
- 事件通过 channel 转成 `syncEventMsg`，由 `waitSyncCmd` 消费；每次事件后重新挂起等待。
- 状态位常驻但不打扰：上次同步时间、结果、冲突数、密钥状态。

## 11. UI 与 CLI

### 11.1 同步页（新增）

内容分区：

1. 状态：未配置 / 同步中 / 上次成功时间 / 冲突 N / 错误摘要。
2. 仓库：URL、分支、连接方式（由 URL 派生，可在 SSH / HTTPS 间一键切换）、认证提示、测试连接。
3. 密钥：已保存 / 未保存；更换口令；忘记本机密钥。
4. 开关：自动同步。
5. 操作：立即同步、打开冲突、打开归档目录。
6. 提示：私有仓库只提供访问控制；登录态可能被平台风控失效。

约束：

- 口令输入掩码；提交后立即从 Model 清除；不落日志、不进 argv。
- token 优先走系统 credential helper；页面只存“已配置”，不长期显示明文。
- 新增页面会改变现有 Tab 页映射与 `view_test` 的行数断言，需同步更新。

### 11.2 冲突视图

- 冲突列表逐项展示：实体名、本地/远端摘要、最后修改、删除标记。
- 单选“本地 / 远程”，歌单额外提供“保留两者”；支持批量。
- 未全部选择时“应用”禁用；应用过程在后台执行并显示进度。
- 退出应用后冲突仍保留，下次打开继续。

### 11.3 状态行

底部提示只显示一行：`同步 12:30 成功` / `3 个冲突` / `密钥不可用` / `同步失败：认证`。点击/回车进入同步页（鼠标已有命中机制）。

### 11.4 CLI

```
zero sync init --remote <url> [--branch main]
zero sync status
zero sync pull
zero sync push
zero sync conflicts
zero sync resolve <tx> <entity> --keep local|remote|both
zero sync key rotate
```

- 口令从 stdin 交互读取（无回显），不使用 `--passphrase` 参数。
- `--repo <path>` 供测试使用本地裸仓库。
- token 通过 credential helper 或环境中的 `GIT_ASKPASS`，不写入命令行。

## 12. 代码落点

```
internal/sync/
  crypt.go       # 派生、seal/open、keycheck、AAD
  meta.go        # meta.json 与本地 sync-key
  config.go      # sync.toml、token 兜底、GIT_ASKPASS
  state.go       # state.json、进程间锁、状态摘要
  snapshot.go    # 导出/导入、确定性序列化、归档
  git.go         # git 命令封装、工作克隆、错误分类
  conflicts.go   # 冲突索引、ours/theirs 快照与摘要
  client.go      # 同步状态机、init/加入、换口令
  credentials.go # 登录态 LWW、密文导入、账号冲突
  resolve.go     # 重新合并、按选择写回、归档
  manager.go     # 单飞、debounce、退避、冻结、事件
  options.go     # 默认路径与 LibraryRevision 回调

cmd/zero/sync.go      # 子命令
cmd/zero/main.go      # 分发、usage、构造 Manager
internal/tui/sync.go  # 同步页、冲突视图、状态行、事件
internal/tui/model.go # 页面枚举、接线、revision 轮询
internal/config/      # SyncDir/SyncTomlPath/SyncKeyPath/SyncTokenPath
```

`syncManager` 只是把 `Client` 包成单飞 + 定时器；`Client` 同时服务 CLI 与 TUI，
所以所有命令都能在没有 Bubble Tea 的情况下测试。

依赖方向：`sync` → `store` / `config` / `keymap`，不依赖 Bubble Tea；`tui` 持有 `*sync.Manager`（与 `player.Controller` 同级），构造时注入；`app` 不新增播放逻辑，只补少量会话回填方法。

### 12.1 store 需要补的接口

现有 `PlaylistSongs` 会展开混合歌单，不能直接用于导出“自己加的曲目”；`playlist_excludes` 也没有公开读取。建议新增：

```go
type PlaylistSnapshot struct {
    Playlist
    Songs    []Song      // 仅 playlist_items，按 position
    Refs     []Playlist  // mixin 引用，按 position
    Excludes []string    // 被隐藏的继承曲目
}

func (s *Store) PlaylistSnapshot(id string) (PlaylistSnapshot, error)
func (s *Store) ListPlaylistSnapshots(kinds ...string) ([]PlaylistSnapshot, error)
func (s *Store) ImportPlaylistSnapshot(snap PlaylistSnapshot) error
func (s *Store) DeletePlaylistForSync(id string) error
func (s *Store) AccountUpdatedAt(source string) (int64, error)
func (s *Store) ListAccounts() ([]string, error)
```

`LibraryRevision` 是内存态计数器，由 `app.Service` 在歌单/账号变更时自增，不放进 `store`；`sync.Manager` 在导出前记录、导入前比较，不一致就放弃本轮导入。

`ImportPlaylistSnapshot` 必须：

- 单事务写 `playlists`、`songs`、`playlist_items`、`playlist_refs`、`playlist_excludes`。
- 保留 `created_at` 与 `added_at`；`EnsurePlaylist` 新建时使用当前时间，无法保留远端 `created_at`，不能直接复用。
- 按 `(source, remote_id, page)` upsert 歌曲；引用歌单不存在时先建 `remote` 占位。
- 校验 `kind` 只能是 `local` / `mixin`，ID 前缀匹配，拒绝路径字符。

### 12.2 同步 Manager 与 TUI

- `tui.New(...)` 增加 `*sync.Manager` 参数；`demo` 和测试传 nil。
- 新增 `waitSyncCmd`，与 `waitEngineCmd` 一样每次事件后重新挂起。
- `syncAppliedMsg` 到达后重新加载歌单列表；不触碰队列/当前播放。
- 键位文件导入后，提示“下次启动生效”，或显式重载 `keymap`（需评估对当前按键路由的影响）。
- 登录态导入后按 §9.7 的决策回填或延迟。

### 12.3 git 封装细节

- 启动时检查 `git` 是否存在，缺失给出安装提示。
- `exec.CommandContext` + 超时；`GIT_TERMINAL_PROMPT=0`；捕获 stderr 做错误分类。
- 认证：优先 `git credential fill`；成功后通知页面“已配置”。若用户选择保存 token，写入 0600 文件并明确警告。
- 工作克隆固定在 `~/.config/zero-beat/sync/repo`，不是用户的代码仓库。
- 崩溃恢复：启动时若存在 `MERGE_HEAD` 则 abort；工作区脏文件恢复到 HEAD 后重新导出。

## 13. 本地文件与权限

```
~/.config/zero-beat/
├── sync.toml          # remote/branch/auto，0600（不含 token）
├── sync-key           # 派生密钥，0600
├── sync-token         # 可选的 HTTPS token 兜底，0600
├── sync/
│   ├── repo/          # 受管工作克隆
│   ├── state.json     # 上次提交、时间、账号哈希、设备 ID、待处理冲突
│   ├── lock
│   ├── conflicts/<txid>/
│   └── archive/<time>/
```

`state.json` 不进仓库；`repo/` 里的远程 URL 不含 token。

## 14. 安全与威胁模型

| 场景 | 暴露面 | 缓解 |
| --- | --- | --- |
| 仓库被读（私有仓库协作者、被 fork、误设公开） | 歌单明文、凭据密文、历史版本 | 凭据加密；初始化时明确提示歌单会以明文托管；不做自动公开化检测（不接各家 API） |
| git token 泄露 | 可写/可删仓库，不能解密凭据 | token 走 credential helper；最小 scope 单仓库；页面不显示明文；可撤销 |
| 本地设备被攻破 | 明文 cookie、`sync-key`、token | 与现状等价，不承诺改善；0600、数据目录 0700 |
| 设备丢失 | 同上 | 撤销 token；更换口令并重加密，但旧密钥仍能解开历史密文 |
| 恶意仓库内容 | 注入键位/歌单/伪造凭据 | 凭据 AEAD 认证无法伪造；歌单/键位按不可信输入校验；KDF 参数只接受白名单；限制文件大小 |
| 重放旧密文 | 旧 cookie 被恢复 | 比较密文内 `updated_at`，旧值忽略 |
| git 历史 | 已删除的歌单/凭据仍在 | 凭据是密文；歌单历史无法删除，文档说明 |
| 提交信息 | 泄露歌单名/设备名 | 提交信息只用随机设备 ID 与时间，不含标题 |
| 日志/崩溃 | 泄露口令或 cookie | 禁止打印 payload、token、口令；错误信息脱敏 URL |
| 多进程并发 | 仓库损坏、库覆盖 | 同步文件锁；导入前 revision 检查；TUI 单实例锁（`internal/instance`） |

额外说明：

- 私有仓库的“私有”只对平台用户生效；自建 git 的运维能看到明文歌单。
- 加密不改变本地 SQLite 仍存明文 cookie 的事实；若要做本地加密，应与 ROADMAP 的 OS keyring 一起做，不在本设计内。
- 远端 KDF 参数可能存在超大 `m` 的 DoS 参数，只接受 `m=65536/t=3/p=1/key_len=32` 或明确的兼容集合。

## 15. 兼容性与版本

- `meta.json.v` 是仓库格式版本；客户端只写 1，读到时大于 1 直接拒绝，提示升级。
- `key_version` 只用于检测密钥更换，不参与派生；换口令后旧设备无法解密新密文，但可以用旧密钥读历史密文。
- 两个设备使用不同 App 版本时，旧版本不得部分写入新格式；导出/导入字段严格校验。
- 仓库回滚（用户手动 reset）可能让 `key_version` 倒退；发现本地 `sync-key` 版本高于远端时提示，不自动覆盖。
- 未知字段：当前策略是拒绝或忽略；导出会重写文件，因此不做“未知字段保留”。这是有意的简单性，写入 ADR。

## 16. 测试计划

**单元**

- `crypt`：正确口令往返；错口令失败；篡改密文/AAD 失败；nonce 每次不同；`keycheck` 校验；超大 KDF 参数拒绝；key 文件 0600。
- `snapshot`：连续两次导出逐字节相同；空库；混合歌单 refs/excludes；远端引用占位；导入幂等；`created_at/added_at` 保留；非法 kind/ID/路径拒绝。
- `merge`：同歌单双改冲突；不同歌单双改自动合并；删除 vs 修改；删除 vs 未改；`merge=binary` 行为；冲突快照 ours/theirs；重新合并后历史保留双亲。
- `state`：冲突持久化与恢复；归档；`meta.json` 冲突进入人工状态。
- 凭据：远端较新/本地较新/时间戳相同；本地登录不被旧密文覆盖；密钥不可用时不导出凭据。

**集成**

- 本地裸仓库 + 两个独立数据目录模拟 A/B 设备；用真实 `git`，不联网。
- 场景：首次推送；第二设备加入（口令、keycheck）；离线双改；推送被拒后重试；冲突解决；密钥轮换；仓库被 reset。
- 崩溃恢复：同步中途 kill，重启后清理 merge 状态并重试。

**TUI**

- 同步页与冲突页的渲染行数/宽度断言，遵守 [view_test.go](../internal/tui/view_test.go) 的精确行约束。
- 掩码输入、退出清除、状态行不挤占布局。
- 用 fake manager 断言同步调用只发生在 `tea.Cmd`，`Update` 内不触发网络或磁盘长任务。

**安全**

- 提交树中不存在 `SESSDATA` / `MUSIC_U` 明文；日志不包含口令/token/会话。
- 文件权限断言（Unix，Windows 跳过）。
- 远端 JSON 注入非法键位/超长文件被拒绝。

**手工**

- GitHub 私有仓库（HTTPS token + credential helper）、Gitea、纯 SSH 各跑一次。
- 两平台同时登录，验证风控失效时的回落路径。

## 17. 实施拆分

M1–M4 已实现（2026-11-01）；TUI 单实例锁随后补在 `internal/instance`。

1. **M1 快照与 git**：`snapshot`、`git`、`merge`、CLI `pull/push`，无凭据；本地裸仓库集成测试。先把“确定性导出 + 冲突选择”跑通。
2. **M2 凭据加密**：`crypt`、`sync-key`、`meta.json`、LWW、密钥不匹配状态；CLI 加入 `key set/rotate`。
3. **M3 TUI 与自动同步**：同步页、冲突页、状态行、`Manager`、debounce/退避、revision 检查。
4. **M4 收尾**：单曲注释、归档浏览、密钥轮换 UI、隐私提示与文档；补 ADR-0016。

## 18. 风险与开放问题

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| 平台风控使同步 cookie 失效 | 新设备仍要扫码 | 保留扫码；按设备关闭凭据同步；失败回落 |
| 关闭行级合并导致冲突变多 | 用户操作成本 | 一实体一文件；批量选择；冲突只在同实体双改时出现 |
| 歌单明文托管 | 收听记录泄露 | 初始化提示；推荐私有仓库；未来可选全库加密 |
| 密钥/口令丢失 | 远端凭据不可解 | 本地登录态仍在；重新登录后用新口令重加密 |
| 历史不可撤销 | 旧密文/旧歌单仍可读 | 文档说明；敏感场景建议新仓库 |
| 时钟偏差 | LWW 顺序错 | 秒级时间戳 + 内容哈希；同时间不同内容进冲突 |
| 双进程 | 仓库/库损坏 | 文件锁；revision 检查；TUI 单实例锁 |
| git 未安装/认证失败 | 无法同步 | 初始化检测；错误分类与可操作提示 |
| 大曲库导出耗时 | 同步变慢 | 一实体一文件 + 只提交差异；后续可做增量，但首版不做 |

**需要评审拍板**

1. 自动同步默认开还是默认关？默认开会影响首次配置后的行为。
2. token 只走 credential helper，还是允许 0600 本地保存作为兜底？
3. 凭据拉取后立即回填，还是下次启动生效？
4. 登出是否要传播到其他设备？（当前设计：不传播）
5. 单曲注释是否纳入首版？纳入则采用一歌一文件。
6. 冲突页做成独立页面，还是同步页内的子视图？
7. “保留两者”的副本 ID/标题规则是否可接受？
8. 是否需要在首次配置时强制勾选“歌单会明文上传”提示？
9. `keymap.toml` 的远端删除是否应该传播？（当前设计：不传播）

**评审结论（2026-11-01）：全部按推荐值落地。** 自动同步默认开；token 允许 0600 兜底
（优先 credential helper）；登录态拉取后立即回填；登出不传播；单曲注释纳入首版、
一歌一文件；冲突做成独立页面；副本用 `<原 ID>-copy-<n>` 且标题加“（副本）”；首次
配置强制勾选明文托管提示；`keymap.toml` 远端删除不传播。实现细节与偏离记录见
[ADR-0016](adr/0016-git-sync.md)。

## 19. 附录：错误分类

| 错误 | UI 文案方向 | 自动处理 |
| --- | --- | --- |
| `ErrNotConfigured` | 未配置同步仓库 | 进同步页 |
| `ErrKeyMissing` | 需要输入同步密钥 | 暂停凭据，歌单继续 |
| `ErrKeyMismatch` | 密钥不正确或仓库已更换密钥 | 不覆盖本地，等输入 |
| `ErrAuth` | 仓库认证失败 | 退避，提示检查 token/SSH |
| `ErrNetwork` | 网络不可用 | 退避重试 |
| `ErrConflict` | 有 N 个冲突待处理 | 冻结自动同步 |
| `ErrSchema` | 仓库格式版本过高 | 拒绝同步，提示升级 |
| `ErrPushRejected` | 远端有新提交 | 自动 fetch/merge 重试 |
| `ErrDirtyWorktree` | 上次同步未完成 | 启动时恢复后重试 |

## 20. 参考

- 现有分层与事件循环约束：[ARCHITECTURE.md](../ARCHITECTURE.md)
- 数据模型：[store.go](../internal/store/store.go#L95-L190)
- 键位加载与冲突规则：[keymap.go](../internal/keymap/keymap.go#L180-L226)
- 配置目录：[config.go](../internal/config/config.go#L12-L57)
- 登录态读写：[store.go](../internal/store/store.go#L1161-L1191)
