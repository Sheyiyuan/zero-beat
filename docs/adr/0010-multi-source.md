# ADR-0010 接入网易云时补齐 source.Source，并把歌曲改成源无关标识

- 状态：已接受
- 日期：2026-10-20

## 背景

ADR-0009 明确写了：只有一个音源时接口是猜的，等第二个音源出现再抽。v0.3 要接网易云，
这个条件满足了。需要同时解决三件事：

1. TUI 不能出现 `if source == "bilibili"` 的分支；
2. 歌曲表不能再用 `bvid` 当唯一平台标识；
3. 旧的 B 站单源数据库不能丢歌单、队列和播放进度。

## 决策

- 新增 `internal/source`：`Source` 接口、`ID`、`Capability`、`Quality`、`Ref/Track/Candidate`、
  `Stream/Resolved`、`Playlist`、`QRCode/LoginStatus` 与 `Registry`。
- B 站通过 `internal/source/bilibili` 适配现有 `internal/bili`；网易云在
  `internal/netease` 实现接口，协议层使用 `go-musicfox/netease-music`（MIT），
  复用 weapi/cloudsearch、二维码 chainId、`song_url_v1` 与歌词/歌单协议。
- `store.Song` 增加 `remote_id` 与 `album`；`SongID(source, externalID, page)` 作为稳定 ID。
  B 站旧 ID 保持不变（`bilibili:<BV>:1`），网易云为 `netease:<songID>:1`。
- `store.Playlist` 增加 `source`；远端歌单用 `source + remote_id` 定位，本地歌单 source 为空。
- `Open` 时自动迁移旧库：重建 songs 表（唯一键改为 `source, remote_id, page`）、
  从 `bvid` 回填 `remote_id`、从歌单 ID 前缀回填 `playlists.source`。
- 登录、搜索、取流、歌词、远端歌单全部走 `Source`。搜索可以选单源或并发合并；
  队列只存 `source + external_id`，播放时再解析。
- 两个平台的远端歌单都不自动导入，统一用「浏览歌单/收藏夹」按需加入；
  网易云额外提供「每日推荐」入口，选中时再拉取。
- 升级后用一次性 marker 清空旧远端歌单镜像，避免历史自动同步数据残留。

## 理由

1. 两个真实实现能校验接口形状；v0.1 里 B 站特有的 CID/多 P 被适配器吸收，TUI 不知道。
2. `remote_id` 是平台无关的最小标识，足够支撑混源队列、本地歌单与断点恢复；
   现在拆 `Track`/`TrackSource` 仍然没有跨源匹配来验证，属于过度设计。
3. 网易云协议已经不能只靠 legacy `/api` 长期稳定：新版扫码需要 `chainId`，取流有 8 档
   `song_url_v1`，搜索走 cloudsearch/weapi。成熟库把这些细节与加密封装好了，
   继续自研会持续跟接口变化，违背「把精力放在播放器体验上」的目标。
4. 协议库用 MIT，与本项目 AGPL-3.0 兼容；其 UNM 依赖是 GPL-3.0，但项目显式关闭 UNM。
5. 迁移在 `Open` 内一次完成，旧用户不需要手动导库。

## 后果

- `Source` 接口有十余个方法，新增音源的实现成本明确；但 TUI 与存储不需要再改。
- 网易云是非官方接口，风控/接口变更风险由 `netease` 包吸收；版权与会员限制通过错误文案暴露。
- 新增 `go-musicfox/netease-music` 及其 UNM 传递依赖；UNM 已关闭，但分发时需保留
  [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md)。
- 仍没有跨源自动匹配与回退：同一首歌的两个版本不会自动绑定，需要后续 `match`/`resolve`。
- 仍没有 `domain.Track` + `TrackSource` 两层模型；如果以后要做「一首歌多平台版本」，
  需要迁移 `songs` 并补匹配器。

## 替代方案

- 在 TUI 里直接持有 `bili.Client` + `netease.Client`，按 song.Source 分支：实现快，
  但每加一个功能都要写两遍，违背 ADR-0005/0009，放弃。
- 自研标准库 legacy 客户端：零依赖、可完整用 httptest 覆盖，但二维码缺少 chainId、
  搜索/音质档位不完整，长期维护成本高。v0.3 前半段曾这样实现，最终切换到成熟库。
