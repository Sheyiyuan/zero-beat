# ADR-0007 项目名暂用 hbp，定名后脚本改名

- 状态：提议中（等待定名）
- 日期：2026-10-18

## 背景

需要先动工再定名。占位名要满足：能当 Go module path、目录名、二进制名，且改名成本可控。

## 决策

- 占位：slug `hbp`，module `github.com/sheyiyuan/hbp`，二进制 `hbp`。
- 候选与取舍写在 [docs/NAMING.md](../NAMING.md)，推荐 **Onbeat · 合拍**。
- 定名后用 `scripts/rename.sh <slug>` 一次性替换 module path、import、`cmd/` 目录、Makefile 与文档。

## 后果

- 改名时只需保证：工作区干净、`make check` 通过、脚本 dry-run 过一遍。
- 仓库目录名、git remote、AUR 包名需要手动处理（脚本会提示）。
- 二进制名会变，文档里的示例命令要跟着改（脚本覆盖 Markdown）。

## 替代方案

沿用 `half-beat` 家族名（零改名成本），代价是两个项目同名、包与仓库要永久带后缀区分。
