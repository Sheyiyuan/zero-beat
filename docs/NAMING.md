# 项目命名

> 状态：**待定**。仓库暂用占位名 `hbp`，定名后 `scripts/rename.sh <slug>` 一键改。

## 约束

1. 二进制要好打。日常命令长度控制在 4–10 字符，能 Tab 补全。
2. 中文要能读、最好有梗。这个项目的用户是中文用户。
3. 不与已知产品撞名（Elastic 的 Beat 家族、音乐播放器、diff 工具等）。
4. 仓库名、Go module path、AUR 包名、crate/npm 名都要能占。
5. 与 v1「半拍 / Half Beat」的关系要说得清：是续作还是新品牌，必须选一个，不能含糊。

## 候选（2026-10 在 GitHub 按仓库名实测）

| 名称 | 二进制 | 含义 | 同名仓库 | 评价 |
| --- | --- | --- | --- | --- |
| **Onbeat · 合拍** | `onbeat` | 合拍：两个平台合拍，也指身心同步 | 80 个，最大 3 星 | **推荐**。中文有梗，延续"拍"字，语义直指混源；英文占用基本空闲 |
| **Halfnote · 二分音符** | `halfnote` | 延续 half 家族（half-pi 半派 / half-beat 半拍） | 10 个，最大 2 星 | 备选。家族一致、几乎空闲，但语义不指向混源，且与 v1 同族易混 |
| **Half Beat TUI** | `hbp` | 直接沿用 v1 | 与 v1 冲突 | 零成本，但两个项目同名，AUR 要变成 `half-beat-tui-bin`，文档到处要带后缀 |
| **Backbeat · 反拍** | `backbeat` | 反拍是 2、4 拍的重音，"另一个平台的应答" | 82 个，`scality/backbeat` 61 星 | 音乐梗最好，但曾与 Elastic 的 Beat 产品线重名 |
| **Tandem · 串联** | `tandem` | 双人自行车：两个平台并行驱动一个播放器 | 3,702 个 | 语义好，但占用严重，不建议 |

已排除：`tutti`（1,202 个同名，`tutti-os` 3.8k 星）、`hepai`（hepai-lab 的 AI 框架）、`halftone`（743 个）、`meld` / `fuse` / `alloy` / `confluence`（均被知名项目占用）、`quodlibet`（已有同名播放器）。

## 推荐：Onbeat · 合拍

理由：

1. **叙事完整**：v1 是「半拍」（只能放一个平台），v2 是「合拍」（两个平台一起响）。名字自带升级关系。
2. **语义双关**：合拍 = 节拍对齐，也 = 两个平台配合默契；跨源兜底正是"合拍"的行为。
3. **中文优先**：中文名好念、能当梗，英文 `onbeat` 简洁且占用低。
4. **不撞 v1**：`half-beat` 留给桌面版，两个项目各自有身份。

代价：放弃 "half" 家族的一致性。如果你更看重家族感，选 **Halfnote**。

## 定名后的操作

```bash
# 1. 预览要改哪些文件
scripts/rename.sh onbeat --dry-run

# 2. 执行（会改 go.mod、所有 import、cmd 目录、Makefile、文档）
scripts/rename.sh onbeat

# 3. 校验
make check

# 4. 手动：仓库目录改名、git remote、README 标题、本文件结论
```

## 待定项

- [ ] 最终 slug（`onbeat` / `halfnote` / 其它）
- [ ] 中文名是否同步启用（合拍 / 二分音符）
- [ ] AUR 包名与 GitHub 仓库名
