# 项目命名

> **结论（2026-10-18）**：仓库与包名 `zero-beat`，命令 `zero`，中文名「零拍」。决策记录见 [ADR-0007](adr/0007-project-name.md)。

## 名字的含义

*Zero beat* 是无线电里的真实概念：两个振荡器频率完全相同时，差拍归零，人耳听不到拍频，只剩一个稳定的音。多源播放器要做的是同一件事——同一首歌在 B 站和网易云的版本同频，切源时听不出接缝。

卖点句：**差拍归零，切源无缝。**

中文名「零拍」承接 v1 的「半拍」：半拍只能放一个平台，零拍让两个平台同频。

## 为什么是这个组合

| 维度 | 说明 |
| --- | --- |
| 家族延续 | `half-beat`（半拍，桌面版）→ `zero-beat`（零拍，TUI 版），保留 `-beat` 后缀 |
| 命令好打 | `zero play BV1xx`，四字符，比任何版本号后缀都好按 |
| 命名空间 | AUR 上 `zero`、`zero-bin`、`zero-beat` 均未被占用 |
| 包与命令分离 | 仓库名要可搜索，命令要短——这两件事本来就不该是同一个字符串 |

## 已知代价（不是缺陷，但要认）

1. **同类项目同名**：`Finsiii/ZeroBeat-CLI` 是一个终端音乐播放器（1 星，基本停滞）。同名同赛道会互相干扰搜索，属于可接受但必须知道的代价。
2. **`zero` 太通用**：搜"zero 音乐播放器"几乎找不到本项目。缓解方式是 README 首句就把 zero beat 的术语含义讲清楚，靠「零拍」这个中文词占位。
3. **代码里 grep 噪音**：Go 里 `zero` 是常用词（zero value、zero-length），搜 `zero` 会混进来一堆无关结果。缓解：文档里命令一律用反引号，代码里不用 `zero` 当变量名。

## 定名后的操作

```bash
make check                                  # 改名前先保证绿
scripts/rename.sh <slug> [--binary <name>]  # 会自动识别当前的模块路径与二进制名
mv ~/code/zero-beat ~/code/<新目录>          # 目录名需手动改
```

手动处理清单：仓库目录名、`git remote`、GitHub 仓库名、AUR 包名（`zero-beat` / `zero-beat-bin`）、README 标题、本文件结论、ADR-0007。

## 历史候选（已被取代，留档备查）

2026-10 在 GitHub 按仓库名实测的占用情况：

| 名称 | 二进制 | 含义 | 同名仓库 | 结论 |
| --- | --- | --- | --- | --- |
| Onbeat · 合拍 | `onbeat` | 合拍：两个平台合拍 | 80 个，最大 3 星 | 强烈备选，中文梗更好，但家族感弱于 zero-beat |
| Halfnote · 二分音符 | `halfnote` | half 家族，音符时值 | 10 个，最大 2 星 | 家族一致，语义不指向混源 |
| Half Beat TUI | — | 沿用 v1 | 与 v1 冲突 | 两项目同名，包名永远要带后缀 |
| Backbeat · 反拍 | `backbeat` | 2、4 拍重音 | 82 个，`scality/backbeat` 61 星 | 曾与 Elastic 产品线重名 |
| Tandem | `tandem` | 两个平台并行驱动 | 3,702 个 | 占用严重，放弃 |

已排除：`tutti`（1,202 个）、`hepai`（hepai-lab 的 AI 框架）、`halftone`（743 个）、`meld` / `fuse` / `alloy` / `confluence`（均被知名项目占用）、`quodlibet`（已有同名播放器）。
