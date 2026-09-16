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
#   GITHUB_TOKEN      token used when 'gh' is unavailable (else `gh auth token`)
set -euo pipefail

REPO="${MYCOLAB_REPO:-carlesoctav/mycolab}"
VERSION="${MYCOLAB_VERSION:-latest}"
[ "${1:-}" != "" ] && VERSION="$1"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

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

"$INSTALL_DIR/mycolab" version
case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*) echo "Note: $INSTALL_DIR is not on your PATH." ;;
esac
echo "Done. Try: mycolab list"
