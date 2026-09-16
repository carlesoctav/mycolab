#!/usr/bin/env bash
# mycolab installer — fetches a prebuilt binary from GitHub Releases.
#
# Usage (this repo is private, so a token is required):
#   curl -fsSL -H "Authorization: Bearer $(gh auth token)" \
#     https://raw.githubusercontent.com/carlesoctav/mycolab/main/install.sh | bash
#
# Env overrides:
#   MYCOLAB_VERSION   release tag to install ("latest" by default)
#   INSTALL_DIR       where to put the binary (~/.local/bin by default)
#   GITHUB_TOKEN      token used for the download (else `gh auth token`)
set -euo pipefail

REPO="${MYCOLAB_REPO:-carlesoctav/mycolab}"
VERSION="${MYCOLAB_VERSION:-latest}"
[ "${1:-}" != "" ] && VERSION="$1"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Resolve a token: the repo is private, anonymous downloads get a 404.
TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
if [ -z "$TOKEN" ] && command -v gh >/dev/null 2>&1; then
	TOKEN="$(gh auth token 2>/dev/null || true)"
fi
if [ -z "$TOKEN" ]; then
	echo "error: no GitHub token found. Install 'gh' and run 'gh auth login'," >&2
	echo "or set GITHUB_TOKEN to a token with repo read access." >&2
	exit 1
fi

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

if [ "$VERSION" = "latest" ]; then
	VERSION="$(curl -fsSL -H "Authorization: Bearer $TOKEN" \
		"https://api.github.com/repos/$REPO/releases/latest" |
		grep -o '"tag_name": *"[^"]*"' | cut -d'"' -f4)"
	[ -n "$VERSION" ] || { echo "error: could not resolve latest release" >&2; exit 1; }
fi

ASSET="mycolab_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"
echo "Installing mycolab $VERSION ($OS/$ARCH) to $INSTALL_DIR ..."

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
curl -fsSL -H "Authorization: Bearer $TOKEN" -o "$TMP/$ASSET" "$URL"
tar -xzf "$TMP/$ASSET" -C "$TMP"
mkdir -p "$INSTALL_DIR"
install -m 755 "$TMP/mycolab" "$INSTALL_DIR/mycolab"

"$INSTALL_DIR/mycolab" version
case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*) echo "Note: $INSTALL_DIR is not on your PATH." ;;
esac
echo "Done. Try: mycolab list"
