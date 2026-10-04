#!/usr/bin/env bash
# mycolab installer — fetches a prebuilt binary from GitHub Releases.
#
# Usage (this repo is private, so auth is required):
#   curl -fsSL -H "Authorization: Bearer $(gh auth token)" \
#     https://raw.githubusercontent.com/carlesoctav/mycolab/main/install.sh | bash
#
# Env overrides:
#   MYCOLAB_VERSION   release tag to install ("latest" by default)
#   INSTALL_DIR       where to put the binary (~/.local/bin by default)
#   INSTALL_DEPS      install dependencies (uv, colab-cli, lsyncd, nfs, rsync) (default: 1; set 0 to skip)
#   GITHUB_TOKEN      token used when 'gh' is unavailable (else `gh auth token`)
set -euo pipefail

REPO="${MYCOLAB_REPO:-carlesoctav/mycolab}"
VERSION="${MYCOLAB_VERSION:-latest}"
[ "${1:-}" != "" ] && VERSION="$1"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
INSTALL_DEPS="${INSTALL_DEPS:-1}"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
linux | darwin) ;;
*) echo "error: unsupported OS '$OS' (need linux or darwin)" >&2; exit 1 ;;
esac
ARCH="$(uname -m)"
case "$ARCH" in
x86_64 | amd64) ARCH="amd64" ;;
aarch64 | arm64) ARCH="arm64" ;;
*) echo "error: unsupported arch '$ARCH' (need amd64 or arm64)" >&2; exit 1 ;;
esac
ASSET="mycolab_${OS}_${ARCH}.tar.gz"

have_gh() {
	command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1
}

if have_gh; then
	if [ "$VERSION" = "latest" ]; then
		VERSION="$(gh api "repos/$REPO/releases/latest" --jq .tag_name)"
		[ -n "$VERSION" ] || { echo "error: could not resolve latest release" >&2; exit 1; }
	fi
	echo "Installing mycolab $VERSION ($OS/$ARCH) to $INSTALL_DIR ..."
	TMP="$(mktemp -d)"
	trap 'rm -rf "$TMP"' EXIT
	gh release download "$VERSION" --repo "$REPO" --pattern "$ASSET" --dir "$TMP" --clobber
else
	TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
	if [ -z "$TOKEN" ]; then
		echo "error: 'gh' is not logged in and no token found." >&2
		echo "Install 'gh' and run 'gh auth login', or set GITHUB_TOKEN to a" >&2
		echo "token with repo read access (python3 is also required)." >&2
		exit 1
	fi
	if ! command -v python3 >/dev/null 2>&1; then
		echo "error: python3 is required for the token download path (or install 'gh')." >&2
		exit 1
	fi
	if [ "$VERSION" = "latest" ]; then
		VERSION="$(curl -fsSL -H "Authorization: Bearer $TOKEN" \
			"https://api.github.com/repos/$REPO/releases/latest" |
			grep -o '"tag_name": *"[^"]*"' | cut -d'"' -f4)"
		[ -n "$VERSION" ] || { echo "error: could not resolve latest release" >&2; exit 1; }
	fi
	echo "Installing mycolab $VERSION ($OS/$ARCH) to $INSTALL_DIR ..."
	TMP="$(mktemp -d)"
	trap 'rm -rf "$TMP"' EXIT
	ASSET_ID="$(curl -fsSL -H "Authorization: Bearer $TOKEN" \
		"https://api.github.com/repos/$REPO/releases/tags/$VERSION" |
		ASSET="$ASSET" python3 -c "import json,os,sys; print(next(a['id'] for a in json.load(sys.stdin)['assets'] if a['name'] == os.environ['ASSET']))")"
	[ -n "$ASSET_ID" ] || { echo "error: asset $ASSET not found in $VERSION" >&2; exit 1; }
	curl -fsSL -L -H "Authorization: Bearer $TOKEN" -H "Accept: application/octet-stream" \
		-o "$TMP/$ASSET" "https://api.github.com/repos/$REPO/releases/assets/$ASSET_ID"
fi

tar -xzf "$TMP/$ASSET" -C "$TMP"
mkdir -p "$INSTALL_DIR"
install -m 755 "$TMP/mycolab" "$INSTALL_DIR/mycolab"

install_deps() {
	echo "Checking and installing dependencies (uv, colab-cli, lsyncd, rsync, nfs)..."
	local sudo_cmd=""
	if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1; then
		sudo_cmd="sudo"
	fi

	# 1. Distro-independent system packages (lsyncd, rsync, nfs client)
	if command -v dnf >/dev/null 2>&1; then
		echo "Detected dnf (Fedora/RHEL/CentOS) — checking system packages..."
		local pkgs=()
		command -v lsyncd >/dev/null 2>&1 || pkgs+=(lsyncd)
		command -v rsync >/dev/null 2>&1 || pkgs+=(rsync)
		[ -x /sbin/mount.nfs ] || pkgs+=(nfs-utils)
		if [ "${#pkgs[@]}" -gt 0 ]; then
			echo "Installing system packages with dnf: ${pkgs[*]} ..."
			$sudo_cmd dnf install -y "${pkgs[@]}" || echo "Warning: failed to install some packages with dnf; you can install them manually: ${pkgs[*]}" >&2
		fi
	elif command -v apt-get >/dev/null 2>&1; then
		echo "Detected apt (Debian/Ubuntu) — checking system packages..."
		local pkgs=()
		command -v lsyncd >/dev/null 2>&1 || pkgs+=(lsyncd)
		command -v rsync >/dev/null 2>&1 || pkgs+=(rsync)
		[ -x /sbin/mount.nfs ] || pkgs+=(nfs-common)
		if [ "${#pkgs[@]}" -gt 0 ]; then
			echo "Installing system packages with apt-get: ${pkgs[*]} ..."
			$sudo_cmd apt-get update -qq || true
			$sudo_cmd DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "${pkgs[@]}" || echo "Warning: failed to install some packages with apt; you can install them manually: ${pkgs[*]}" >&2
		fi
	elif command -v pacman >/dev/null 2>&1; then
		echo "Detected pacman (Arch Linux) — checking system packages..."
		local pkgs=()
		command -v lsyncd >/dev/null 2>&1 || pkgs+=(lsyncd)
		command -v rsync >/dev/null 2>&1 || pkgs+=(rsync)
		[ -x /sbin/mount.nfs ] || pkgs+=(nfs-utils)
		if [ "${#pkgs[@]}" -gt 0 ]; then
			$sudo_cmd pacman -S --noconfirm "${pkgs[@]}" || echo "Warning: failed to install some packages with pacman: ${pkgs[*]}" >&2
		fi
	elif command -v brew >/dev/null 2>&1; then
		echo "Detected Homebrew (macOS) — checking packages..."
		command -v lsyncd >/dev/null 2>&1 || brew install lsyncd || true
		command -v rsync >/dev/null 2>&1 || brew install rsync || true
	fi

	# 2. uv (fast Python package and tool runner)
	if ! command -v uv >/dev/null 2>&1; then
		echo "Installing uv..."
		curl -LsSf https://astral.sh/uv/install.sh | sh
		export PATH="$HOME/.local/bin:$HOME/.cargo/bin:$PATH"
	fi

	# 3. colab-cli via uv tool
	if ! command -v colab >/dev/null 2>&1; then
		echo "Installing colab-cli via uv tool..."
		uv tool install --force colab-cli || echo "Warning: 'uv tool install colab-cli' failed; install manually with 'uv tool install colab-cli'." >&2
	else
		echo "colab-cli is already installed ($(colab --version 2>/dev/null || echo 'ready'))."
	fi
}

if [ "$INSTALL_DEPS" = "1" ] || [ "$INSTALL_DEPS" = "true" ]; then
	install_deps
fi

"$INSTALL_DIR/mycolab" version
case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*) echo "Note: $INSTALL_DIR is not on your PATH." ;;
esac
echo "Done. Try: mycolab list"
