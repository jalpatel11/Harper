#!/bin/sh
# Installs the harper CLI by downloading the matching pre-built binary from
# GitHub Releases. Usage:
#   curl -sSL https://raw.githubusercontent.com/jalpatel11/Harper/main/install.sh | sh
#
# Override the install directory with INSTALL_DIR, and pin a version with
# HARPER_VERSION (defaults to the latest release).

set -e

REPO="jalpatel11/Harper"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

fail() {
    echo "error: $1" >&2
    exit 1
}

# --- detect platform ---

os_raw=$(uname -s)
case "$os_raw" in
    Darwin) os="darwin" ;;
    Linux) os="linux" ;;
    *) fail "unsupported OS: $os_raw (harper ships prebuilt binaries for macOS and Linux only)" ;;
esac

arch_raw=$(uname -m)
case "$arch_raw" in
    x86_64 | amd64) arch="amd64" ;;
    arm64 | aarch64) arch="arm64" ;;
    *) fail "unsupported architecture: $arch_raw" ;;
esac

# --- resolve version ---

if [ -n "$HARPER_VERSION" ]; then
    version="$HARPER_VERSION"
else
    version=$(curl -sSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    [ -n "$version" ] || fail "could not determine the latest release version"
fi
version_num="${version#v}"

archive="harper_${version}_${os}_${arch}.tar.gz"
base_url="https://github.com/$REPO/releases/download/$version"

# --- download and verify ---

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

echo "Downloading $archive ($version)..."
curl -sSL -o "$tmp_dir/$archive" "$base_url/$archive" || fail "failed to download $base_url/$archive"
curl -sSL -o "$tmp_dir/SHA256SUMS.txt" "$base_url/SHA256SUMS.txt" || fail "failed to download checksums"

cd "$tmp_dir"
expected=$(grep "$archive" SHA256SUMS.txt | awk '{print $1}')
[ -n "$expected" ] || fail "no checksum found for $archive"

if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$archive" | awk '{print $1}')
else
    fail "neither sha256sum nor shasum is available to verify the download"
fi

[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive (expected $expected, got $actual) — download may be corrupted"

# --- install ---

tar -xzf "$archive"
extracted_dir="harper_${version}_${os}_${arch}"

if [ -w "$INSTALL_DIR" ]; then
    mv "$extracted_dir/harper" "$INSTALL_DIR/harper"
else
    echo "Need elevated permission to write to $INSTALL_DIR"
    sudo mv "$extracted_dir/harper" "$INSTALL_DIR/harper"
fi
chmod +x "$INSTALL_DIR/harper"

echo "Installed harper $version_num to $INSTALL_DIR/harper"
"$INSTALL_DIR/harper" version
