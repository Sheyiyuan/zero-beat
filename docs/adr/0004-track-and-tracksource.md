# ADR-0004 数据模型分 Track / TrackSource 两层，直链不落库

- 状态：部分实现（直链不落库；`TrackSource` 仍未拆，见 ADR-0010）
- 日期：2026-10-18

## 背景

v1 的 `Song` 把「平台标识」和「曲目身份」混在一张表里：`bvid`、`pageNumber`、`totalPages`、`streamUrl`、`streamUrlExpiresAt` 全是 B 站字段。混源歌单要求同一首歌能有多个平台的版本。

## 决策

拆成两层：

- `tracks`：用户视角的一首歌，无平台字段，带 `norm_key` 用于跨源匹配；
- `track_sources`：这首歌在某个平台的版本（`source` + `external_id` + `page`，音质、可用性、是否首选）。

歌单只引用 `track_id`。**播放直链不入库**，播放时解析成 `Stream`（带 `ExpiresAt`）。

## 理由

1. 混源与「跨源兜底」都要求「一首歌 → 多个平台版本」的一对多关系。
2. 直链会过期（B 站约 2 小时、网易云更短），落库只会制造「缓存即脏数据」。
3. `norm_key` 让匹配、去重、迁移都变成纯数据操作，可单测。

## 后果

- 需要写 v1 数据导入器：`Song` → `Track` + `TrackSource(bilibili)`，`Favorite` → `Playlist` + `RemotePlaylist`。
- 播放前必须有一次解析+回退流程（`internal/resolve`），比 v1 多一层。
- 需要人工确认的匹配队列与「手动关联」界面。

## 替代方案

保留单表 + `extra` JSON 字段：改动最小，但查询、去重、迁移都会退化成字符串处理，且永远长不出跨源能力。
