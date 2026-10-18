#!/usr/bin/env bash
# 项目改名：模块路径、二进制名、目录与文档。
#
# 用法:
#   scripts/rename.sh <slug> [--binary <name>] [--module <path>] [--dry-run]
#
# 例:
#   scripts/rename.sh zero-beat --binary zero
#   scripts/rename.sh onbeat
#
# 说明:
#   - 从 go.mod 与 Makefile 自动读出当前的模块路径与二进制名，可反复使用；
#   - 会改：go.mod、所有 import、cmd/<binary> 目录、Makefile、main.go 的 appName、文档里的名字；
#   - 不会改：仓库目录名、git remote、AUR 包名（结尾会提示）；
#   - 跳过 scripts/rename.sh 自身。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OLD_MODULE=$(sed -n 's|^module[[:space:]]*||p' go.mod | head -1)
OLD_SLUG="${OLD_MODULE##*/}"
OLD_BINARY=$(sed -n 's|^BINARY[[:space:]]*:=[[:space:]]*||p' Makefile | head -1)

DRY_RUN=0
NEW_SLUG=""
NEW_BINARY=""
NEW_MODULE=""
while [ "$#" -gt 0 ]; do
	case "$1" in
	--dry-run) DRY_RUN=1 ;;
	--binary) NEW_BINARY="${2:?--binary 需要一个名字}"; shift ;;
	--module) NEW_MODULE="${2:?--module 需要一个路径}"; shift ;;
	-*) echo "未知参数: $1" >&2; exit 1 ;;
	*) NEW_SLUG="$1" ;;
	esac
	shift
done

if [ -z "$NEW_SLUG" ]; then
	echo "用法: scripts/rename.sh <slug> [--binary <name>] [--module <path>] [--dry-run]" >&2
	exit 1
fi
NEW_BINARY="${NEW_BINARY:-$NEW_SLUG}"
NEW_MODULE="${NEW_MODULE:-${OLD_MODULE%/*}/$NEW_SLUG}"

for value in "$NEW_SLUG" "$NEW_BINARY"; do
	case "$value" in
	[!a-z]* | *[!a-z0-9-]*)
		echo "只能用小写字母、数字和连字符: $value" >&2
		exit 1
		;;
	esac
done

echo "模块路径: $OLD_MODULE -> $NEW_MODULE"
echo "二进制:   $OLD_BINARY -> $NEW_BINARY"
echo "仓库 slug: $OLD_SLUG -> $NEW_SLUG"

FILES=$(grep -rlE "$OLD_MODULE|\\b$OLD_BINARY\\b" . \
	--exclude-dir=.git --exclude-dir=bin --exclude=rename.sh 2>/dev/null || true)

if [ -z "$FILES" ]; then
	echo "没有需要修改的文件。"
else
	echo "涉及文件:"
	echo "$FILES" | sed 's|^\./|  |'
	if [ "$DRY_RUN" -eq 0 ]; then
		# shellcheck disable=SC2086
		echo "$FILES" | xargs perl -pi -e "s|\Q$OLD_MODULE\E|$NEW_MODULE|g; s/\\b$OLD_BINARY\\b/$NEW_BINARY/g"
	fi
fi

if [ -d "cmd/$OLD_BINARY" ] && [ "$OLD_BINARY" != "$NEW_BINARY" ]; then
	echo "目录: cmd/$OLD_BINARY -> cmd/$NEW_BINARY"
	if [ "$DRY_RUN" -eq 0 ]; then
		mv "cmd/$OLD_BINARY" "cmd/$NEW_BINARY"
	fi
fi

if [ "$DRY_RUN" -eq 0 ]; then
	cat <<TIP

完成。还需要手动处理：
  1. make check                       # 验证
  2. 仓库目录改名                     # mv <旧目录> <新目录>
  3. git remote / GitHub 仓库名
  4. AUR 包名（<slug> 或 <slug>-bin）
  5. README 标题、docs/NAMING.md 的结论、docs/adr/0007 的状态
TIP
else
	echo "(dry-run，未修改任何文件)"
fi
