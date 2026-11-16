#!/usr/bin/env bash
# 把 monorepo 的组件目录通过 git subtree 推送到对应仓库。
# 需要先添加远程：core / gui / site。
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

: "${BRANCH:=main}"

for pair in "zero-beat-core core" "zero-beat-gui gui" "zero-beat-site site"; do
  read -r dir remote <<<"$pair"
  if ! git remote get-url "$remote" >/dev/null 2>&1; then
    echo "缺少远程 $remote；先运行 git remote add $remote <url>" >&2
    exit 1
  fi
  echo "==> subtree $dir -> $remote/$BRANCH"
  git subtree push --prefix="$dir" "$remote" "$BRANCH"
done

echo "完成。zero-beat-tui 是原仓库历史，不在此脚本中改写。"
