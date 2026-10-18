#!/usr/bin/env bash
# 把占位名 hbp 改成正式项目名。
#
# 用法:
#   scripts/rename.sh <new-slug> [module-path] [--dry-run]
#
# 例:
#   scripts/rename.sh onbeat
#   scripts/rename.sh onbeat github.com/sheyiyuan/onbeat
#
# 说明:
#   - 只改文件内容与 cmd/<slug> 目录名，仓库目录本身请手动 mv；
#   - 脚本自身不会被修改，方便回滚；
#   - 改完记得改 README 标题与 docs/NAMING.md 的结论。
set -euo pipefail

OLD_SLUG="hbp"
OLD_MODULE="github.com/sheyiyuan/hbp"

DRY_RUN=0
ARGS=()
for arg in "$@"; do
	case "$arg" in
	--dry-run) DRY_RUN=1 ;;
	*) ARGS+=("$arg") ;;
	esac
done

if [ "${#ARGS[@]}" -lt 1 ]; then
	echo "用法: scripts/rename.sh <new-slug> [module-path] [--dry-run]" >&2
	exit 1
fi

NEW_SLUG="${ARGS[0]}"
NEW_MODULE="${ARGS[1]:-github.com/sheyiyuan/$NEW_SLUG}"

case "$NEW_SLUG" in
[!a-z]* | *[!a-z0-9-]*)
	echo "slug 只能使用小写字母、数字和连字符: $NEW_SLUG" >&2
	exit 1
	;;
esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "项目名: $OLD_SLUG -> $NEW_SLUG"
echo "模块路径: $OLD_MODULE -> $NEW_MODULE"

FILES=$(grep -rlE "$OLD_SLUG|$OLD_MODULE" . \
	--exclude-dir=.git --exclude-dir=bin \
	--exclude=rename.sh || true)

if [ -z "$FILES" ]; then
	echo "没有需要修改的文件。"
else
	echo "将修改以下文件："
	echo "$FILES" | sed 's/^/  /'
	if [ "$DRY_RUN" -eq 0 ]; then
		# shellcheck disable=SC2086
		echo "$FILES" | xargs perl -pi -e "s|\Q$OLD_MODULE\E|$NEW_MODULE|g; s/\\b$OLD_SLUG\\b/$NEW_SLUG/g"
	fi
fi

if [ -d "cmd/$OLD_SLUG" ]; then
	echo "目录: cmd/$OLD_SLUG -> cmd/$NEW_SLUG"
	if [ "$DRY_RUN" -eq 0 ]; then
		mv "cmd/$OLD_SLUG" "cmd/$NEW_SLUG"
	fi
fi

if [ "$DRY_RUN" -eq 0 ]; then
	echo
	echo "完成。下一步："
	echo "  1. make check"
	echo "  2. mv $ROOT ~/code/$NEW_SLUG"
	echo "  3. 手动更新 README 标题、docs/NAMING.md 结论与 git remote"
else
	echo "(dry-run，未修改任何文件)"
fi
