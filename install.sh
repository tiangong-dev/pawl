#!/bin/sh
# One-line install for pawl:
#
#   curl -fsSL https://raw.githubusercontent.com/tiangong-dev/pawl/main/install.sh | sh
#
# Uses whatever toolchain is already on the machine, in order of preference:
# Go (go install), then npm (global @pawl-tools/cli), then a direct download of
# the prebuilt binary from the GitHub Release. Override the target release with
# PAWL_VERSION (a tag like v0.1.0, or "latest"); override the binary-download
# location with PAWL_INSTALL_DIR (default /usr/local/bin). In the direct-download
# path, a missing signature aborts the install unless PAWL_SKIP_VERIFY=1 — set
# it only to install a release that predates cosign signing.
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

# --retry covers 408/429/5xx and timeouts with exponential backoff;
# --retry-all-errors (curl >= 7.71) adds mid-transfer resets. The flags are
# fixed words, so they are expanded unquoted on purpose.
retry_flags="--retry 5 --retry-connrefused"
if curl --retry-all-errors -V >/dev/null 2>&1; then
  retry_flags="$retry_flags --retry-all-errors"
fi

ver="$VERSION"
if [ "$ver" = latest ]; then
  if have gh; then
    ver="$(gh release view --repo "$REPO" --json tagName --jq .tagName 2>/dev/null || true)"
  fi
  if [ -z "$ver" ] || [ "$ver" = latest ]; then
    # shellcheck disable=SC2086
    ver="$(curl -fsSL $retry_flags "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | cut -d'"' -f4)"
  fi
fi
[ -n "$ver" ] || { echo "pawl: could not resolve the latest release tag" >&2; exit 1; }

num="${ver#v}"
asset="pawl-${num}-${plat}-${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${ver}/${asset}"

tmp="$(mktemp -d)"
# A bare INT/TERM trap would swallow the signal and keep the script running;
# exit explicitly so the EXIT trap cleans up and the install stops.
trap 'rm -rf "$tmp"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
echo "pawl: downloading ${asset} (${ver})"

downloaded=0
if have gh; then
  if gh release download "$ver" --repo "$REPO" --pattern "$asset" --dir "$tmp" --clobber && [ -f "${tmp}/${asset}" ]; then
    downloaded=1
  else
    echo "pawl: gh release download failed, falling back to curl" >&2
  fi
fi

if [ "$downloaded" -eq 0 ]; then
  # shellcheck disable=SC2086
  curl -fsSL $retry_flags "$url" -o "${tmp}/${asset}"
fi

if have cosign; then
  # Plain --retry only: a release that predates signing returns 404, which
  # should fail fast instead of being retried.
  if curl -fsSL --retry 5 --retry-connrefused "${url}.sigstore.json" -o "${tmp}/${asset}.sigstore.json" 2>/dev/null; then
    echo "pawl: verifying signature with cosign"
    cosign verify-blob \
      --bundle "${tmp}/${asset}.sigstore.json" \
      --certificate-identity-regexp 'https://github.com/tiangong-dev/pawl/\.github/workflows/release\.yml@.*' \
      --certificate-oidc-issuer https://token.actions.githubusercontent.com \
      "${tmp}/${asset}"
  elif [ "${PAWL_SKIP_VERIFY:-}" = "1" ]; then
    echo "pawl: no .sigstore.json for ${ver} and PAWL_SKIP_VERIFY=1 — installing unverified" >&2
  else
    echo "pawl: could not fetch a signature for ${ver} — refusing to install unverified. Releases before signing was added have none; set PAWL_SKIP_VERIFY=1 to install one of those anyway." >&2
    exit 1
  fi
else
  echo "pawl: cosign not found — skipping signature verification (install cosign to verify: https://github.com/sigstore/cosign)" >&2
fi

tar -xzf "${tmp}/${asset}" -C "$tmp"

dir="${PAWL_INSTALL_DIR:-/usr/local/bin}"
if [ -w "$dir" ]; then
  install -m 0755 "${tmp}/pawl" "${dir}/pawl"
else
  echo "pawl: ${dir} is not writable — retrying with sudo"
  sudo install -m 0755 "${tmp}/pawl" "${dir}/pawl"
fi
echo "pawl: installed to ${dir}/pawl"
