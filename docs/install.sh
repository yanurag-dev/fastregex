#!/usr/bin/env bash
set -euo pipefail

REPO="yanurag-dev/GrepTurbo"
BINARY="grepturbo"
ARCHIVE_PREFIX="GrepTurbo"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS
OS="$(uname -s)"
case "$OS" in
  Linux)  OS="Linux" ;;
  Darwin) OS="Darwin" ;;
  *)
    echo "Unsupported OS: $OS" >&2
    exit 1
    ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)          ARCH="x86_64" ;;
  amd64)           ARCH="x86_64" ;;
  arm64|aarch64)   ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

# Fetch latest release tag
echo "Fetching latest release..."
TAG="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' \
  | sed -E 's/.*"tag_name": "([^"]+)".*/\1/')"

if [ -z "$TAG" ]; then
  echo "Failed to determine latest release tag." >&2
  exit 1
fi

VERSION="${TAG#v}"
FILENAME="${ARCHIVE_PREFIX}_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${TAG}/${FILENAME}"
CHECKSUM_URL="https://github.com/${REPO}/releases/download/${TAG}/checksums.txt"

echo "Installing ${BINARY} ${TAG} (${OS}/${ARCH})..."

# Download to temp dir
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "$URL" -o "${TMP}/${FILENAME}"
curl -fsSL "$CHECKSUM_URL" -o "${TMP}/checksums.txt"

# Verify checksum
# Prefer shasum on macOS — BSD sha256sum lacks --check support
cd "$TMP"
if [ "$OS" = "Darwin" ] && command -v shasum &>/dev/null; then
  grep "${FILENAME}" checksums.txt | shasum -a 256 --check --status
elif command -v sha256sum &>/dev/null && sha256sum --version &>/dev/null 2>&1; then
  grep "${FILENAME}" checksums.txt | sha256sum --check --status
elif command -v shasum &>/dev/null; then
  grep "${FILENAME}" checksums.txt | shasum -a 256 --check --status
else
  echo "Warning: no sha256sum or shasum found, skipping checksum verification." >&2
fi
cd - >/dev/null

# Extract and install
tar -xzf "${TMP}/${FILENAME}" -C "$TMP"

if [ ! -w "$INSTALL_DIR" ]; then
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo install -m 755 "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  install -m 755 "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

echo ""
echo "${BINARY} ${TAG} installed to ${INSTALL_DIR}/${BINARY}"
echo "Run '${BINARY} --help' to get started."
