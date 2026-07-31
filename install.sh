#!/bin/sh
# install.sh — Install Anvil CLI
#
# Usage:
#   curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh
#   curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh -s -- --version v0.1.0
#   curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh -s -- --to ~/.local/bin
#
# Downloads the latest (or specified) Anvil release binary for the
# current OS and architecture, verifies its checksum, and installs
# it to /usr/local/bin (or a custom path via --to).
#
# Supported platforms:
#   - Linux   (amd64, arm64)
#   - macOS   (amd64, arm64)

set -eu

# ── Config ─────────────────────────────────────────────────────────
REPO="maleolabs/anvil"
DEFAULT_INSTALL_DIR="/usr/local/bin"
VERSION="latest"
INSTALL_DIR="$DEFAULT_INSTALL_DIR"

# ── Parse args ─────────────────────────────────────────────────────
while [ $# -gt 0 ]; do
  case "$1" in
    --version)
      VERSION="$2"
      shift 2
      ;;
    --to)
      INSTALL_DIR="$2"
      shift 2
      ;;
    --help)
      echo "Usage: install.sh [--version vX.Y.Z] [--to <dir>]"
      echo ""
      echo "  --version vX.Y.Z  Install a specific version (default: latest)"
      echo "  --to <dir>        Install to a custom directory (default: /usr/local/bin)"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1"
      echo "Usage: install.sh [--version vX.Y.Z] [--to <dir>]"
      exit 1
      ;;
  esac
done

# ── Detect platform ────────────────────────────────────────────────
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
  linux)   ;;
  darwin)  OS="darwin" ;;
  *)
    echo "Error: unsupported OS '$OS'. Only Linux and macOS are supported."
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Error: unsupported architecture '$ARCH'. Only amd64 and arm64 are supported."
    exit 1
    ;;
esac

BINARY="anvil-${OS}-${ARCH}"
echo "Detected: $OS/$ARCH → $BINARY"

# ── Resolve download URL ───────────────────────────────────────────
if [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/$REPO/releases/latest/download"
else
  BASE_URL="https://github.com/$REPO/releases/download/$VERSION"
fi

DOWNLOAD_URL="$BASE_URL/$BINARY"
CHECKSUM_URL="$BASE_URL/SHA256SUMS.txt"

# ── Create temp directory ──────────────────────────────────────────
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

# ── Download binary ────────────────────────────────────────────────
echo "Downloading $BINARY..."
HTTP_CODE="$(curl -fsSL -w '%{http_code}' -o "$TMPDIR/$BINARY" "$DOWNLOAD_URL" 2>/dev/null || true)"
if [ "$HTTP_CODE" != "200" ]; then
  echo "Error: failed to download $BINARY (HTTP $HTTP_CODE)"
  echo "  URL: $DOWNLOAD_URL"
  exit 1
fi

chmod +x "$TMPDIR/$BINARY"

# ── Verify checksum ────────────────────────────────────────────────
echo "Verifying checksum..."
if HTTP_CHECKSUM="$(curl -fsSL -w '%{http_code}' -o "$TMPDIR/SHA256SUMS.txt" "$CHECKSUM_URL" 2>/dev/null)"; then
  if [ "$HTTP_CHECKSUM" = "200" ]; then
    (cd "$TMPDIR" && sha256sum -c --ignore-missing SHA256SUMS.txt 2>/dev/null) || \
    (cd "$TMPDIR" && shasum -a 256 -c --ignore-missing SHA256SUMS.txt 2>/dev/null) || {
      echo "Warning: checksum verification failed, but continuing..."
    }
  fi
else
  echo "Warning: checksum file not available, skipping verification."
fi

# ── Install ────────────────────────────────────────────────────────
mkdir -p "$INSTALL_DIR"

# Check if install directory is writable
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMPDIR/$BINARY" "$INSTALL_DIR/anvil"
else
  echo "Cannot write to $INSTALL_DIR. Trying sudo..."
  sudo mv "$TMPDIR/$BINARY" "$INSTALL_DIR/anvil"
fi

echo ""
echo "Anvil installed to $INSTALL_DIR/anvil"
echo ""
echo "Run 'anvil --help' to get started."
echo "Run 'anvil init <name>' to create a new project."
