# 设计与实现的差异

> 立项时的两份评估在 [docs/design/](design/)。这份文档说明**实际写出来的东西为什么和设计不一样**，避免下一个人按旧文档改代码。

## 对照表

| 设计里写的 | 实际实现 | 原因 |
| --- | --- | --- |
| `internal/domain`：`Track` + `TrackSource` 两层模型 | 只有 `store.Song` 一张表 | 单音源时"一首歌的多个平台版本"没有第二种情况可验证，先按实际需要建表 |
| `internal/source`：`Source` 接口 + `Capability` + 注册表 | 直接调 `bili.Client` | 只有一个实现，接口是猜的；等第二个音源出现再抽（ADR-0009） |
| `internal/audio/decoder`：native + mpv 双后端 | 只有 ffmpeg 子进程 | 编码矩阵（AAC-LC / HE-AAC / FLAC / EC-3）用 ffmpeg 一次全覆盖，先用最短路径拿到能听的声音（ADR-0008） |
| `internal/match` / `internal/resolve` | 未创建 | 依赖多音源，随混源一起做 |
| 播放直链不落库 | ✅ 一致 | 每次播放现解析 |
| 能力位驱动 UI | 暂未需要 | 只有 B 站，没有按平台分支的诱惑 |
| 先做 `zero play` headless 再写 UI | ✅ 一致 | 先验证音频链路，再上界面 |

## 播放引擎的实现细节

- `ffmpeg` 负责解封装与解码，输出 s16le / 48kHz / 立体声到 stdout。
- `gainReader` 包在 stdout 外面：读到的 PCM 直接乘增益并做 `tanh` 软限幅，所以调音量不需要重启解码。
- 位置 = 已消费字节 ÷ 字节率 + seek 基准。比"开始时间到现在"更准，暂停时自然冻结。
- seek = 杀掉 ffmpeg，用 `-ss` 重新起一个。代价是 0.3–1 秒停顿。
- 结束/失败通过 `cmd.Wait()` 的结果判断，用 generation 计数避免"旧进程的退出事件打到新曲目上"。

## 界面层的实现细节

- 只有一个 `Model`，视图用 `view` 枚举切换；没有嵌套 model，避免状态同步成本。
- 列表滚动用 `windowRange(total, cursor, height)`，渲染永远只画可视区。
- 霓虹风格统一走 `theme.go` 的样式变量；强调色沿用 v1 的 `#66ccff`。
- 每帧都按终端宽高补齐/裁剪，`view_test.go` 断言"行数 == 高度、每行宽度 == 宽度"。

## 什么时候该把抽象补回来

- **第二个音源动工前**：先写 `source.Source` 接口（登录、搜索、取流、歌词、远端歌单），再写网易云实现。
- **要做跨源匹配时**：补 `Track` + `TrackSource`，写迁移器把现有 `songs` 拆成两层。
- **ffmpeg 成为分发障碍时**：加 `decoder.Decoder` 接口，先实现 MP3/FLAC/AAC-LC 的纯 Go 后端，ffmpeg 退为兜底。
