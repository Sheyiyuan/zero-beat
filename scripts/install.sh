#!/bin/sh
# zero-beat Linux/macOS 安装脚本。
#
# 用法:
#   sh install.sh
#   sh install.sh --version v0.4.0
#   sh install.sh --prefix /usr/local
#   sh install.sh --bin-dir "$HOME/bin"
#   sh install.sh --uninstall
#
# 环境变量: ZERO_BEAT_REPO, VERSION, PREFIX, BINDIR
set -eu

REPO="${ZERO_BEAT_REPO:-sheyiyuan/zero-beat}"
VERSION="${VERSION:-}"
PREFIX="${PREFIX:-$HOME/.local}"
BINDIR="${BINDIR:-$PREFIX/bin}"
UNINSTALL=0

usage() {
	cat <<'USAGE'
zero-beat installer

Options:
  --version <vX.Y.Z>  指定版本；默认 latest release
  --prefix <dir>      安装前缀，默认 $HOME/.local
  --bin-dir <dir>     直接指定二进制目录，默认 <prefix>/bin
  --uninstall         删除已安装的 zero 二进制
  -h, --help          显示帮助

Environment:
  ZERO_BEAT_REPO      GitHub 仓库，默认 sheyiyuan/zero-beat
  VERSION             同 --version
  PREFIX / BINDIR     同 --prefix / --bin-dir
USAGE
}

fail() {
	echo "install: $*" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
		--version)
			[ $# -ge 2 ] || fail "--version 需要参数"
			VERSION="$2"
			shift 2
			;;
		--prefix)
			[ $# -ge 2 ] || fail "--prefix 需要参数"
			PREFIX="$2"
			BINDIR="$PREFIX/bin"
			shift 2
			;;
		--bin-dir)
			[ $# -ge 2 ] || fail "--bin-dir 需要参数"
			BINDIR="$2"
			shift 2
			;;
		--uninstall)
			UNINSTALL=1
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			usage >&2
			fail "未知参数: $1"
			;;
	esac
done

if [ "$UNINSTALL" -eq 1 ]; then
	if [ -e "$BINDIR/zero" ]; then
		rm -f "$BINDIR/zero"
		echo "已删除 $BINDIR/zero"
	else
		echo "未找到 $BINDIR/zero"
	fi
	exit 0
fi

case "$(uname -s)" in
	Linux) OS=linux ;;
	Darwin) OS=darwin ;;
	*) fail "只支持 Linux / macOS；Windows 请用 release 里的 zip（不保证可用）" ;;
esac

case "$(uname -m)" in
	x86_64|amd64) ARCH=amd64 ;;
	arm64|aarch64) ARCH=arm64 ;;
	*) fail "不支持的架构: $(uname -m)" ;;
esac

command -v curl >/dev/null 2>&1 || fail "需要 curl"
command -v tar >/dev/null 2>&1 || fail "需要 tar"

if [ -z "$VERSION" ]; then
	api="https://api.github.com/repos/$REPO/releases/latest"
	latest=$(curl -fsSL --retry 3 "$api" 2>/dev/null || true)
	VERSION=$(printf '%s\n' "$latest" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
	[ -n "$VERSION" ] || fail "无法获取最新版本；可显式传 --version v0.4.0"
fi
VERSION=${VERSION#v}
TAG="v$VERSION"
ASSET="zero_${VERSION}_${OS}_${ARCH}.tar.gz"
BASE="${ZERO_BEAT_BASE_URL:-https://github.com/$REPO/releases/download/$TAG}"

TMP=$(mktemp -d "${TMPDIR:-/tmp}/zero-beat.XXXXXX")
trap 'rm -rf "$TMP"' EXIT INT TERM

echo "下载 $ASSET ..."
curl -fsSL --retry 3 --retry-delay 1 -o "$TMP/$ASSET" "$BASE/$ASSET" || fail "下载失败: $BASE/$ASSET"
curl -fsSL --retry 3 --retry-delay 1 -o "$TMP/checksums.txt" "$BASE/checksums.txt" || fail "下载校验文件失败"

if command -v sha256sum >/dev/null 2>&1; then
	ACTUAL=$(sha256sum "$TMP/$ASSET" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	ACTUAL=$(shasum -a 256 "$TMP/$ASSET" | awk '{print $1}')
else
	fail "需要 sha256sum 或 shasum 校验下载文件"
fi
EXPECTED=$(awk -v a="$ASSET" '$2 == a || $2 == "*" a { print $1; exit }' "$TMP/checksums.txt")
[ -n "$EXPECTED" ] || fail "校验文件中没有 $ASSET"
[ "$ACTUAL" = "$EXPECTED" ] || fail "SHA256 校验失败"

tar -xzf "$TMP/$ASSET" -C "$TMP"
SRC="$TMP/zero"
[ -f "$SRC" ] || SRC=$(find "$TMP" -type f -name zero | head -n 1)
[ -n "$SRC" ] || fail "压缩包里没有 zero 二进制"

mkdir -p "$BINDIR"
if command -v install >/dev/null 2>&1; then
	install -m 0755 "$SRC" "$BINDIR/zero"
else
	cp "$SRC" "$BINDIR/zero"
	chmod 0755 "$BINDIR/zero"
fi
echo "已安装 $BINDIR/zero"

if ! command -v ffmpeg >/dev/null 2>&1; then
	echo
	echo "提示: 还没有找到 ffmpeg。播放需要它，可执行："
	case "$OS" in
		darwin) echo "  brew install ffmpeg" ;;
		linux)
			if command -v apt-get >/dev/null 2>&1; then
				echo "  sudo apt install ffmpeg"
			elif command -v dnf >/dev/null 2>&1; then
				echo "  sudo dnf install ffmpeg"
			elif command -v pacman >/dev/null 2>&1; then
				echo "  sudo pacman -S ffmpeg"
			elif command -v zypper >/dev/null 2>&1; then
				echo "  sudo zypper install ffmpeg"
			else
				echo "  请从 https://ffmpeg.org/download.html 安装 ffmpeg"
			fi
			;;
	esac
fi

case ":${PATH}:" in
	*":$BINDIR:"*) ;;
	*)
		echo
		echo "提示: 把 $BINDIR 加入 PATH 后可直接运行 zero"
		;;
esac

echo
echo "完成。运行 $BINDIR/zero --version 验证安装。"
