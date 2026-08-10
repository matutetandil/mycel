#!/bin/sh
# Install Mycel — the declarative microservice runtime.
#
#   curl -fsSL https://raw.githubusercontent.com/matutetandil/mycel/main/install.sh | sh
#
# Installs the latest release for this platform into /usr/local/bin, or into
# $MYCEL_INSTALL_DIR. Set MYCEL_VERSION to pin a version.
#
# On a server you probably want the .deb or .rpm instead: they carry a systemd
# unit, an unprivileged user and /etc/mycel, none of which a loose binary does.
# See https://github.com/matutetandil/mycel/releases

set -eu

REPO="matutetandil/mycel"
INSTALL_DIR="${MYCEL_INSTALL_DIR:-/usr/local/bin}"

fail() { printf 'error: %s\n' "$1" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || fail "$1 is required"; }
need uname
need tar

if command -v curl >/dev/null 2>&1; then
    fetch() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
    fetch() { wget -qO- "$1"; }
else
    fail "curl or wget is required"
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
    linux|darwin) ;;
    *) fail "unsupported OS: $os — Mycel publishes linux and darwin builds" ;;
esac

arch=$(uname -m)
case "$arch" in
    x86_64|amd64)  arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) fail "unsupported architecture: $arch — Mycel publishes amd64 and arm64 builds" ;;
esac

version="${MYCEL_VERSION:-}"
if [ -z "$version" ]; then
    # Resolve the latest tag without needing jq.
    version=$(fetch "https://api.github.com/repos/$REPO/releases/latest" \
        | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
    [ -n "$version" ] || fail "could not resolve the latest release"
fi
version="${version#v}"

archive="mycel_${version}_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/v${version}/${archive}"

printf 'Installing mycel %s (%s/%s) into %s\n' "$version" "$os" "$arch" "$INSTALL_DIR"

tmp=$(mktemp -d)
# Leave nothing behind, including on a failed download.
trap 'rm -rf "$tmp"' EXIT INT TERM

fetch "$url" > "$tmp/$archive" || fail "download failed: $url"
tar -xzf "$tmp/$archive" -C "$tmp" || fail "could not extract $archive"
[ -f "$tmp/mycel" ] || fail "archive did not contain a mycel binary"

# Only reach for sudo when the target is not already writable, so the script
# stays usable for a per-user install.
if [ -w "$INSTALL_DIR" ] || [ ! -e "$INSTALL_DIR" ] && mkdir -p "$INSTALL_DIR" 2>/dev/null; then
    install -m 0755 "$tmp/mycel" "$INSTALL_DIR/mycel"
elif command -v sudo >/dev/null 2>&1; then
    printf 'Elevating with sudo to write to %s\n' "$INSTALL_DIR"
    sudo install -m 0755 "$tmp/mycel" "$INSTALL_DIR/mycel"
else
    fail "$INSTALL_DIR is not writable and sudo is unavailable — set MYCEL_INSTALL_DIR"
fi

printf '\n%s\n\n' "$("$INSTALL_DIR/mycel" version)"
printf 'Next:\n'
printf '  mycel init my-service && cd my-service\n'
printf '  mycel start\n'

case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) printf '\nNote: %s is not on your PATH.\n' "$INSTALL_DIR" ;;
esac
