#!/bin/sh
# zero-beat Linux/macOS 安装脚本。
#
# 用法:
#   sh install.sh
#   sh install.sh --version v0.4.1
#   sh install.sh --prefix /usr/local
#   sh install.sh --bin-dir "$HOME/bin"
#   sh install.sh --no-path          # 不自动修改 shell 配置
#   sh install.sh --uninstall
#
# 环境变量: ZERO_BEAT_REPO, VERSION, PREFIX, BINDIR, ZERO_BEAT_NO_PATH
set -eu

REPO="${ZERO_BEAT_REPO:-sheyiyuan/zero-beat}"
VERSION="${VERSION:-}"
PREFIX="${PREFIX:-$HOME/.local}"
BINDIR="${BINDIR:-$PREFIX/bin}"
UNINSTALL=0
NO_PATH=0
case "${ZERO_BEAT_NO_PATH:-0}" in
	1|true|yes) NO_PATH=1 ;;
esac

usage() {
	cat <<'USAGE'
zero-beat installer

Options:
  --version <vX.Y.Z>  指定版本；默认 latest release
  --prefix <dir>      安装前缀，默认 $HOME/.local
  --bin-dir <dir>     直接指定二进制目录，默认 <prefix>/bin
  --no-path           不自动把安装目录写入 shell 配置
  --uninstall         删除已安装的 zero 二进制及自动写入的 PATH 配置
  -h, --help          显示帮助

Environment:
  ZERO_BEAT_REPO      GitHub 仓库，默认 sheyiyuan/zero-beat
  VERSION             同 --version
  PREFIX / BINDIR     同 --prefix / --bin-dir
  ZERO_BEAT_NO_PATH   设为 1 等同 --no-path
USAGE
}

fail() {
	echo "install: $*" >&2
	exit 1
}

warn() {
	echo "install: $*" >&2
}

# 用于把路径安全地放进双引号字符串。
quote_path() {
	printf '%s' "$1" | sed 's/[\\"$`]/\\&/g'
}

detect_shell() {
	_shell="${SHELL:-}"
	if [ -z "$_shell" ]; then
		_shell=$(ps -p $$ -o comm= 2>/dev/null || true)
	fi
	SHELL_NAME=$(basename "${_shell:-sh}" 2>/dev/null || echo sh)
	case "$SHELL_NAME" in
		zsh|bash|fish|dash|ksh|sh) ;;
		*) SHELL_NAME=sh ;;
	esac
}

rc_file() {
	case "$SHELL_NAME" in
		zsh) printf '%s\n' "${ZDOTDIR:-$HOME}/.zshrc" ;;
		fish) printf '%s\n' "${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish" ;;
		bash)
			if [ "$(uname -s 2>/dev/null)" = Darwin ]; then
				printf '%s\n' "$HOME/.bash_profile"
			elif [ -f "$HOME/.bashrc" ]; then
				printf '%s\n' "$HOME/.bashrc"
			elif [ -f "$HOME/.bash_profile" ]; then
				printf '%s\n' "$HOME/.bash_profile"
			else
				printf '%s\n' "$HOME/.bashrc"
			fi
			;;
		*) printf '%s\n' "$HOME/.profile" ;;
	esac
}

path_line() {
	if [ "$SHELL_NAME" = fish ]; then
		printf 'fish_add_path "%s"  # zero-beat\n' "$(quote_path "$BINDIR")"
	else
		printf 'export PATH="%s:$PATH"  # zero-beat\n' "$(quote_path "$BINDIR")"
	fi
}

remove_path_config() {
	detect_shell
	_rc=$(rc_file)
	if [ ! -f "$_rc" ]; then
		return 0
	fi
	if ! grep -qF '# zero-beat' "$_rc"; then
		return 0
	fi
	_tmp="$_rc.zero-beat.$$"
	if grep -vF '# zero-beat' "$_rc" >"$_tmp"; then
		mv "$_tmp" "$_rc"
		echo "已从 $_rc 移除 zero-beat 的 PATH 配置"
	else
		rm -f "$_tmp"
		warn "无法更新 $_rc，请手动删除含 # zero-beat 的行"
	fi
}

configure_path() {
	if [ "$NO_PATH" -eq 1 ]; then
		echo "已跳过 PATH 配置（--no-path）"
		return 0
	fi
	case ":${PATH}:" in
		*":$BINDIR:"*)
			echo "PATH 中已包含 $BINDIR"
			return 0
			;;
	esac

	detect_shell
	_rc=$(rc_file)
	_line=$(path_line)

	if [ -f "$_rc" ] && grep -qF '# zero-beat' "$_rc"; then
		echo "已在 $_rc 中配置过 PATH（未重复写入）"
	else
		_dir=$(dirname "$_rc")
		if [ ! -d "$_dir" ]; then
			mkdir -p "$_dir" 2>/dev/null || true
		fi
		if printf '\n%s\n' "$_line" >>"$_rc" 2>/dev/null; then
			echo "已把 $BINDIR 写入 $_rc"
		else
			warn "无法写入 $_rc，请手动把 $BINDIR 加入 PATH"
			return 0
		fi
	fi

	echo "当前终端立即生效："
	case "$SHELL_NAME" in
		fish) echo "  source \"$_rc\"" ;;
		*) echo "  . \"$_rc\"" ;;
	esac
	echo "或重新打开终端。"
}

# 不依赖 api.github.com：先看 releases/latest 的跳转地址，失败再退回 GitHub API。
latest_version() {
	final=$(curl -fsSL --connect-timeout 10 --max-time 30 \
		--retry 3 --retry-delay 1 -o /dev/null -w '%{url_effective}' \
		"https://github.com/$REPO/releases/latest" 2>/dev/null || true)
	case "$final" in
		*/releases/tag/*)
			printf '%s\n' "${final##*/}"
			return 0
			;;
	esac

	json=$(curl -fsSL --connect-timeout 5 --max-time 20 \
		--retry 2 --retry-delay 1 \
		-H 'Accept: application/vnd.github+json' \
		"https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null || true)
	tag=$(printf '%s\n' "$json" |
		sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
		head -n 1)
	if [ -z "$tag" ]; then
		return 1
	fi
	printf '%s\n' "$tag"
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
		--no-path)
			NO_PATH=1
			shift
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

# 统一成绝对路径，避免 PATH 判断和写入配置时出现相对路径。
case "$BINDIR" in
	/*) ;;
	*) BINDIR="$PWD/$BINDIR" ;;
esac
if [ "$BINDIR" != "/" ]; then
	BINDIR=${BINDIR%/}
fi

if [ "$UNINSTALL" -eq 1 ]; then
	if [ -e "$BINDIR/zero" ]; then
		rm -f "$BINDIR/zero"
		echo "已删除 $BINDIR/zero"
	else
		echo "未找到 $BINDIR/zero"
	fi
	remove_path_config
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
	if ! VERSION=$(latest_version); then
		fail "无法获取最新版本；请显式指定，例如 sh install.sh --version v0.4.1"
	fi
fi
VERSION=${VERSION#v}
case "$VERSION" in
	''|*[!0-9.]*) fail "版本号格式无效: $VERSION（应形如 v0.4.1）" ;;
esac
TAG="v$VERSION"
ASSET="zero_${VERSION}_${OS}_${ARCH}.tar.gz"
BASE="${ZERO_BEAT_BASE_URL:-https://github.com/$REPO/releases/download/$TAG}"

TMP=$(mktemp -d "${TMPDIR:-/tmp}/zero-beat.XXXXXX")
trap 'rm -rf "$TMP"' EXIT INT TERM

echo "下载 $ASSET ..."
curl -fsSL --connect-timeout 15 --retry 3 --retry-delay 1 \
	-o "$TMP/$ASSET" "$BASE/$ASSET" || fail "下载失败: $BASE/$ASSET"
curl -fsSL --connect-timeout 15 --retry 3 --retry-delay 1 \
	-o "$TMP/checksums.txt" "$BASE/checksums.txt" || fail "下载校验文件失败"

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

echo
configure_path

echo
echo "完成。运行 $BINDIR/zero --version 验证安装。"
