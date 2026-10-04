#!/usr/bin/env bash
# mycolab installer — fetches a prebuilt binary from GitHub Releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/carlesoctav/mycolab/main/install.sh | bash
#
# Env overrides:
#   MYCOLAB_VERSION   release tag to install ("latest" by default)
#   INSTALL_DIR       where to put the binary (~/.local/bin by default)
#   INSTALL_DEPS      install dependencies (uv, colab-cli, lsyncd, rsync) (default: 1; set 0 to skip)
#   GITHUB_TOKEN      optional token to avoid GitHub API rate limits
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
	AUTH_HEADER=()
	if [ -n "$TOKEN" ]; then
		AUTH_HEADER=(-H "Authorization: Bearer $TOKEN")
	fi

	if [ "$VERSION" = "latest" ]; then
		VERSION="$(curl -fsSL ${AUTH_HEADER[@]+"${AUTH_HEADER[@]}"} \
			"https://api.github.com/repos/$REPO/releases/latest" |
			grep -o '"tag_name": *"[^"]*"' | cut -d'"' -f4)"
		[ -n "$VERSION" ] || { echo "error: could not resolve latest release" >&2; exit 1; }
	fi
	echo "Installing mycolab $VERSION ($OS/$ARCH) to $INSTALL_DIR ..."
	TMP="$(mktemp -d)"
	trap 'rm -rf "$TMP"' EXIT

	DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"
	if ! curl -fsSL ${AUTH_HEADER[@]+"${AUTH_HEADER[@]}"} -o "$TMP/$ASSET" "$DOWNLOAD_URL"; then
		# Fallback via GitHub API asset endpoint if direct release redirect fails
		if command -v python3 >/dev/null 2>&1; then
			ASSET_ID="$(curl -fsSL ${AUTH_HEADER[@]+"${AUTH_HEADER[@]}"} \
				"https://api.github.com/repos/$REPO/releases/tags/$VERSION" |
				ASSET="$ASSET" python3 -c "import json,os,sys; print(next(a['id'] for a in json.load(sys.stdin)['assets'] if a['name'] == os.environ['ASSET']))")"
			[ -n "$ASSET_ID" ] || { echo "error: asset $ASSET not found in $VERSION" >&2; exit 1; }
			curl -fsSL -L ${AUTH_HEADER[@]+"${AUTH_HEADER[@]}"} -H "Accept: application/octet-stream" \
				-o "$TMP/$ASSET" "https://api.github.com/repos/$REPO/releases/assets/$ASSET_ID"
		else
			echo "error: failed to download $DOWNLOAD_URL" >&2
			exit 1
		fi
	fi
fi

tar -xzf "$TMP/$ASSET" -C "$TMP"
mkdir -p "$INSTALL_DIR"
install -m 755 "$TMP/mycolab" "$INSTALL_DIR/mycolab"

install_deps() {
	echo "Checking and installing dependencies (uv, colab-cli, lsyncd, rsync)..."
	local sudo_cmd=""
	if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1; then
		sudo_cmd="sudo"
	fi

	# 1. Distro-independent system packages (lsyncd, rsync)
	if command -v dnf >/dev/null 2>&1; then
		local pkgs=()
		command -v lsyncd >/dev/null 2>&1 || pkgs+=(lsyncd)
		command -v rsync >/dev/null 2>&1 || pkgs+=(rsync)
		if [ "${#pkgs[@]}" -gt 0 ]; then
			echo "Installing system packages with dnf: ${pkgs[*]} ..."
			$sudo_cmd dnf install -y "${pkgs[@]}" || echo "Warning: failed to install with dnf: ${pkgs[*]}" >&2
		fi
	elif command -v apt-get >/dev/null 2>&1; then
		local pkgs=()
		command -v lsyncd >/dev/null 2>&1 || pkgs+=(lsyncd)
		command -v rsync >/dev/null 2>&1 || pkgs+=(rsync)
		if [ "${#pkgs[@]}" -gt 0 ]; then
			echo "Installing system packages with apt-get: ${pkgs[*]} ..."
			$sudo_cmd apt-get update -qq || true
			$sudo_cmd DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "${pkgs[@]}" || echo "Warning: failed to install with apt: ${pkgs[*]}" >&2
		fi
	elif command -v pacman >/dev/null 2>&1; then
		local pkgs=()
		command -v lsyncd >/dev/null 2>&1 || pkgs+=(lsyncd)
		command -v rsync >/dev/null 2>&1 || pkgs+=(rsync)
		if [ "${#pkgs[@]}" -gt 0 ]; then
			$sudo_cmd pacman -S --noconfirm "${pkgs[@]}" || echo "Warning: failed to install with pacman: ${pkgs[*]}" >&2
		fi
	elif command -v brew >/dev/null 2>&1; then
		command -v lsyncd >/dev/null 2>&1 || brew install lsyncd || true
		command -v rsync >/dev/null 2>&1 || brew install rsync || true
	fi

	# 2. uv (fast Python package and tool runner)
	if ! command -v uv >/dev/null 2>&1; then
		echo "Installing uv..."
		curl -LsSf https://astral.sh/uv/install.sh | sh
		export PATH="$HOME/.local/bin:$HOME/.cargo/bin:$PATH"
	fi

	# 3. colab-cli via uv tool (pinned to 0.7.4)
	if ! command -v colab >/dev/null 2>&1; then
		echo "Installing colab-cli 0.7.4 via uv tool..."
		uv tool install --force "google-colab-cli==0.7.4" || echo "Warning: 'uv tool install google-colab-cli==0.7.4' failed; install manually with 'uv tool install google-colab-cli==0.7.4'." >&2
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
