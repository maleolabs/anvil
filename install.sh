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

# ── Helpers ────────────────────────────────────────────────────────
_now() {
  # POSIX-compatible timestamp in seconds
  date +%s 2>/dev/null || echo "0"
}

_elapsed() {
  _start="$1"
  _end="$2"
  _diff=$((_end - _start))
  if [ "$_diff" -lt 60 ]; then
    echo "${_diff}s"
  else
    _mins=$((_diff / 60))
    _secs=$((_diff % 60))
    echo "${_mins}m ${_secs}s"
  fi
}

_step_start() {
  STEP_NAME="$1"
  STEP_BEGIN=$(_now)
  printf "  ⠋ %s...\n" "$STEP_NAME"
}

_step_ok() {
  _end=$(_now)
  _time=$(_elapsed "$STEP_BEGIN" "$_end")
  printf "  ✓ %s (%s)\n" "$STEP_NAME" "$_time"
}

_step_fail() {
  _end=$(_now)
  _time=$(_elapsed "$STEP_BEGIN" "$_end")
  printf "  ✗ %s (%s)\n" "$STEP_NAME" "$_time"
  if [ -n "${1:-}" ]; then
    printf "    %s\n" "$1"
  fi
  exit 1
}

_step_warn() {
  _end=$(_now)
  _time=$(_elapsed "$STEP_BEGIN" "$_end")
  printf "  ⚠ %s (%s)\n" "$STEP_NAME" "$_time"
  if [ -n "${1:-}" ]; then
    printf "    %s\n" "$1"
  fi
}

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

# ── Header ─────────────────────────────────────────────────────────
TOTAL_START=$(_now)
echo ""
echo "Anvil CLI Installer"
echo "───────────────────"
echo ""

# ── Detect platform ────────────────────────────────────────────────
_step_start "Detecting platform"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
  linux)   ;;
  darwin)  OS="darwin" ;;
  *)
    _step_fail "Unsupported OS '$OS'. Only Linux and macOS are supported."
    ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    _step_fail "Unsupported architecture '$ARCH'. Only amd64 and arm64 are supported."
    ;;
esac

BINARY="anvil-${OS}-${ARCH}"
_step_ok "$OS/$ARCH → $BINARY"

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
_step_start "Downloading $BINARY"

HTTP_CODE="$(curl -fsSL -w '%{http_code}' -o "$TMPDIR/$BINARY" "$DOWNLOAD_URL" 2>/dev/null || true)"
if [ "$HTTP_CODE" != "200" ]; then
  _step_fail "Download failed (HTTP $HTTP_CODE)" "URL: $DOWNLOAD_URL"
fi

chmod +x "$TMPDIR/$BINARY"
_step_ok "Downloaded $BINARY"

# ── Verify checksum ────────────────────────────────────────────────
_step_start "Verifying checksum"

CHECKSUM_AVAILABLE=0
CHECKSUM_VERIFIED=0

if curl -fsSL -o "$TMPDIR/SHA256SUMS.txt" "$CHECKSUM_URL" 2>/dev/null; then
  CHECKSUM_AVAILABLE=1
  if (cd "$TMPDIR" && sha256sum -c --ignore-missing SHA256SUMS.txt 2>/dev/null) || \
     (cd "$TMPDIR" && shasum -a 256 -c --ignore-missing SHA256SUMS.txt 2>/dev/null); then
    CHECKSUM_VERIFIED=1
  fi
fi

if [ "$CHECKSUM_VERIFIED" -eq 1 ]; then
  _step_ok "Checksum verified"
elif [ "$CHECKSUM_AVAILABLE" -eq 1 ]; then
  _step_fail "Checksum mismatch" "Downloaded binary may be corrupted or tampered."
else
  _step_warn "Checksum file not available, skipping verification"
fi

# ── Install ────────────────────────────────────────────────────────
_step_start "Installing to $INSTALL_DIR/anvil"

mkdir -p "$INSTALL_DIR"

if [ -w "$INSTALL_DIR" ]; then
  mv "$TMPDIR/$BINARY" "$INSTALL_DIR/anvil"
else
  sudo mv "$TMPDIR/$BINARY" "$INSTALL_DIR/anvil"
fi

_step_ok "Installed to $INSTALL_DIR/anvil"

# ── Summary ────────────────────────────────────────────────────────
TOTAL_END=$(_now)
TOTAL_TIME=$(_elapsed "$TOTAL_START" "$TOTAL_END")

echo ""
echo "───────────────────"
echo "Anvil installed successfully!"
echo ""
echo "  Binary: $INSTALL_DIR/anvil"
echo "  Time:   $TOTAL_TIME"
echo ""
echo "Run 'anvil --help' to get started."
echo "Run 'anvil init <name>' to create a new project."
