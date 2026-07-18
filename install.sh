#!/bin/sh
# gogents installer — downloads a release binary and installs it onto your PATH.
#
#   curl -fsSL https://raw.githubusercontent.com/dccoding1118/gogents/main/install.sh | sh
#
# Environment overrides:
#   VERSION      release tag to install   (default: the latest release)
#   INSTALL_DIR  install destination      (default: /usr/local/bin)
#
# Supports linux and macOS on amd64/arm64. Windows is not supported.
set -eu

REPO="dccoding1118/gogents"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

err() { echo "install.sh: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"; }

need curl
need tar
need uname

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux | darwin) ;;
  *) err "unsupported OS '$os' — gogents supports linux and macOS only (Windows is not supported)" ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) err "unsupported architecture '$arch'" ;;
esac

# Resolve the version: an explicit VERSION wins, otherwise ask GitHub for the
# latest release tag (unauthenticated, so this needs the repo to be public).
version="${VERSION:-}"
if [ -z "$version" ]; then
  version="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | head -n1 \
    | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')"
  [ -n "$version" ] || err "could not resolve the latest release; set VERSION=vX.Y.Z explicitly"
fi

name="gogents_${version}_${os}_${arch}"
base="https://github.com/${REPO}/releases/download/${version}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading ${name}.tar.gz (${version})..."
curl -fsSL -o "${tmp}/${name}.tar.gz" "${base}/${name}.tar.gz" \
  || err "download failed — is ${version} a published release for ${os}/${arch}?"

# Verify the checksum when the release ships checksums.txt and a tool is present.
if curl -fsSL -o "${tmp}/checksums.txt" "${base}/checksums.txt" 2>/dev/null; then
  if command -v sha256sum >/dev/null 2>&1; then
    sum="sha256sum -c"
  elif command -v shasum >/dev/null 2>&1; then
    sum="shasum -a 256 -c"
  else
    sum=""
  fi
  if [ -n "$sum" ]; then
    ( cd "$tmp" && grep "${name}.tar.gz" checksums.txt | $sum - >/dev/null 2>&1 ) \
      || err "checksum verification failed for ${name}.tar.gz"
    echo "Checksum verified."
  fi
fi

tar -xzf "${tmp}/${name}.tar.gz" -C "$tmp"
binary="${tmp}/${name}/gogents"
[ -f "$binary" ] || err "archive did not contain the expected gogents binary"

echo "Installing to ${INSTALL_DIR}/gogents..."
mkdir -p "$INSTALL_DIR" 2>/dev/null || true
if [ -w "$INSTALL_DIR" ]; then
  install -m 0755 "$binary" "${INSTALL_DIR}/gogents"
elif command -v sudo >/dev/null 2>&1; then
  echo "Elevating with sudo to write ${INSTALL_DIR}..."
  sudo install -m 0755 "$binary" "${INSTALL_DIR}/gogents"
else
  err "cannot write to ${INSTALL_DIR} and sudo is unavailable; re-run with INSTALL_DIR set to a writable path"
fi

echo "gogents ${version} installed to ${INSTALL_DIR}/gogents"
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) echo "note: ${INSTALL_DIR} is not on your PATH — add it to run 'gogents' directly" ;;
esac
