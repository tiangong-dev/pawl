#!/bin/sh
# One-line install for pawl:
#
#   curl -fsSL https://raw.githubusercontent.com/tiangong-dev/pawl/main/install.sh | sh
#
# Uses whatever toolchain is already on the machine, in order of preference:
# Go (go install), then npm (global @pawl-tools/cli), then a direct download of
# the prebuilt binary from the GitHub Release. Override the target release with
# PAWL_VERSION (a tag like v0.1.0, or "latest"); override the binary-download
# location with PAWL_INSTALL_DIR (default /usr/local/bin).
set -eu

REPO="tiangong-dev/pawl"
VERSION="${PAWL_VERSION:-latest}"

have() { command -v "$1" >/dev/null 2>&1; }

if have go; then
  echo "pawl: installing via go install (@${VERSION})"
  exec go install "github.com/${REPO}/cmd/pawl@${VERSION}"
fi

if have npm; then
  # npm versions carry no leading v; a tag like v0.1.0 maps to 0.1.0.
  spec="@pawl-tools/cli"
  [ "$VERSION" != latest ] && spec="@pawl-tools/cli@${VERSION#v}"
  echo "pawl: installing via npm (-g ${spec})"
  exec npm install -g "$spec"
fi

echo "pawl: no go or npm found — downloading a prebuilt binary"
os="$(uname -s)"
machine="$(uname -m)"
case "$os" in
  Linux)  plat=linux ;;
  Darwin) plat=darwin ;;
  *) echo "pawl: no prebuilt binary for OS '$os' — install Go or Node and re-run" >&2; exit 1 ;;
esac
case "$machine" in
  x86_64 | amd64) arch=x64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "pawl: no prebuilt binary for arch '$machine'" >&2; exit 1 ;;
esac

ver="$VERSION"
if [ "$ver" = latest ]; then
  ver="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | cut -d'"' -f4)"
fi
[ -n "$ver" ] || { echo "pawl: could not resolve the latest release tag" >&2; exit 1; }

num="${ver#v}"
asset="pawl-${num}-${plat}-${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${ver}/${asset}"

tmp="$(mktemp -d)"
echo "pawl: downloading ${asset} (${ver})"
curl -fsSL "$url" -o "${tmp}/${asset}"
tar -xzf "${tmp}/${asset}" -C "$tmp"

dir="${PAWL_INSTALL_DIR:-/usr/local/bin}"
if [ -w "$dir" ]; then
  install -m 0755 "${tmp}/pawl" "${dir}/pawl"
else
  echo "pawl: ${dir} is not writable — retrying with sudo"
  sudo install -m 0755 "${tmp}/pawl" "${dir}/pawl"
fi
rm -rf "$tmp"
echo "pawl: installed to ${dir}/pawl"
