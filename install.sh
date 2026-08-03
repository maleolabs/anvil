#!/bin/sh
# install.sh — Install Anvil CLI
#
# Usage:
#   curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh
#   curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh -s -- --version v0.1.0
#   curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh -s -- --to ~/.local/bin
#   curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh -s -- --with-adapters laravel,flutter
#
# Downloads the latest (or specified) Anvil release binary for the
# current OS and architecture, verifies its checksum, and installs
# it to /usr/local/bin (or a custom path via --to).
# With --with-adapters <name[,name...]>, adapter binaries (laravel,
# flutter) are downloaded, verified, and installed alongside the CLI.
#
# Supported platforms:
#   - Linux   (amd64, arm64)
#   - macOS   (amd64, arm64)

set -eu

# ── Helpers ────────────────────────────────────────────────────────
_now() {
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

# _run_step executes a command and reports success/failure with timing.
# Usage: _run_step "Step name" command args...
_run_step() {
  _name="$1"
  shift
  _begin=$(_now)
  
  # Execute the command, capture output
  _output=$("$@" 2>&1) && _rc=0 || _rc=$?
  
  _end=$(_now)
  _time=$(_elapsed "$_begin" "$_end")
  
  if [ "$_rc" -eq 0 ]; then
    printf "  ✓ %-40s (%s)\n" "$_name" "$_time"
  else
    printf "  ✗ %-40s (%s)\n" "$_name" "$_time"
    if [ -n "$_output" ]; then
      printf "    %s\n" "$_output"
    fi
    exit 1
  fi
}

# ── Config ─────────────────────────────────────────────────────────
REPO="maleolabs/anvil"
DEFAULT_INSTALL_DIR="/usr/local/bin"
VERSION="latest"
INSTALL_DIR="$DEFAULT_INSTALL_DIR"
ADAPTERS=""

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
    --with-adapters)
      ADAPTERS="$2"
      shift 2
      ;;
    --help)
      echo "Usage: install.sh [--version vX.Y.Z] [--to <dir>] [--with-adapters <name[,name...]>]"
      echo ""
      echo "  --version vX.Y.Z            Install a specific version (default: latest)"
      echo "  --to <dir>                  Install to a custom directory (default: /usr/local/bin)"
      echo "  --with-adapters <list>      Also install adapter binaries (comma-separated: laravel,flutter)"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1"
      echo "Usage: install.sh [--version vX.Y.Z] [--to <dir>] [--with-adapters <name[,name...]>]"
      exit 1
      ;;
  esac
done

# ── Validate adapter names ─────────────────────────────────────────
ADAPTER_LIST=""
if [ -n "$ADAPTERS" ]; then
  ADAPTER_LIST="$(printf '%s' "$ADAPTERS" | tr ',' ' ')"
  if [ -z "$ADAPTER_LIST" ]; then
    echo "--with-adapters requires at least one adapter name. Supported: laravel, flutter"
    exit 1
  fi
  for _adapter in $ADAPTER_LIST; do
    case "$_adapter" in
      laravel|flutter) ;;
      *)
        echo "Unknown adapter '$_adapter'. Supported: laravel, flutter"
        exit 1
        ;;
    esac
  done
fi

# ── Header ─────────────────────────────────────────────────────────
TOTAL_START=$(_now)
echo ""
echo "Anvil CLI Installer"
echo "───────────────────"
echo ""

# ── Detect platform ────────────────────────────────────────────────
_detect_platform() {
  _os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  _arch="$(uname -m)"

  case "$_os" in
    linux)   ;;
    darwin)  _os="darwin" ;;
    *)
      echo "Unsupported OS '$_os'. Only Linux and macOS are supported."
      exit 1
      ;;
  esac

  case "$_arch" in
    x86_64|amd64) _arch="amd64" ;;
    aarch64|arm64) _arch="arm64" ;;
    *)
      echo "Unsupported architecture '$_arch'. Only amd64 and arm64 are supported."
      exit 1
      ;;
  esac

  echo "${_os}/${_arch}"
}

_detect_result=$(_detect_platform)
printf "  ✓ %-40s\n" "Platform: $_detect_result"

OS="$(echo "$_detect_result" | cut -d'/' -f1)"
ARCH="$(echo "$_detect_result" | cut -d'/' -f2)"
BINARY="anvil-${OS}-${ARCH}"

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
# _download_asset downloads an asset to $TMPDIR and makes it executable.
# Usage: _download_asset <asset-name> <url>
_download_asset() {
  _asset="$1"
  _url="$2"
  _http_code="$(curl -fsSL -w '%{http_code}' -o "$TMPDIR/$_asset" "$_url" 2>/dev/null || true)"
  if [ "$_http_code" != "200" ]; then
    echo "Download failed (HTTP $_http_code)"
    echo "URL: $_url"
    exit 1
  fi
  chmod +x "$TMPDIR/$_asset"
}

_download() {
  _download_asset "$BINARY" "$DOWNLOAD_URL"
}

_run_step "Download $BINARY" _download

# ── Verify checksum ────────────────────────────────────────────────
# _verify_checksum verifies a downloaded asset against SHA256SUMS.txt.
# Usage: _verify_checksum <asset-name>
_verify_checksum() {
  _asset="$1"
  CHECKSUM_AVAILABLE=0
  CHECKSUM_VERIFIED=0

  if curl -fsSL -o "$TMPDIR/SHA256SUMS.txt" "$CHECKSUM_URL" 2>/dev/null; then
    CHECKSUM_AVAILABLE=1

    # Extract expected hash from SHA256SUMS.txt
    # The file may contain "binaries/anvil-linux-amd64" or just "anvil-linux-amd64"
    EXPECTED_HASH=""
    while IFS= read -r line; do
      case "$line" in
        *"$_asset")
          # Extract hash (first field, two-space separator)
          EXPECTED_HASH="${line%%  *}"
          break
          ;;
      esac
    done < "$TMPDIR/SHA256SUMS.txt"

    if [ -n "$EXPECTED_HASH" ]; then
      # Compute actual hash of downloaded asset
      ACTUAL_HASH=""
      if command -v sha256sum >/dev/null 2>&1; then
        ACTUAL_HASH="$(sha256sum "$TMPDIR/$_asset" | cut -d' ' -f1)"
      elif command -v shasum >/dev/null 2>&1; then
        ACTUAL_HASH="$(shasum -a 256 "$TMPDIR/$_asset" | cut -d' ' -f1)"
      fi

      if [ -n "$ACTUAL_HASH" ]; then
        if [ "$EXPECTED_HASH" = "$ACTUAL_HASH" ]; then
          CHECKSUM_VERIFIED=1
        fi
      fi
    fi
  fi

  if [ "$CHECKSUM_VERIFIED" -eq 1 ]; then
    return 0
  elif [ "$CHECKSUM_AVAILABLE" -eq 1 ]; then
    echo "Checksum mismatch - downloaded binary may be corrupted or tampered."
    exit 1
  else
    echo "Checksum file not available, skipping verification"
    return 0
  fi
}

_run_step "Verify checksum" _verify_checksum "$BINARY"

# ── Install ────────────────────────────────────────────────────────
_install() {
  mkdir -p "$INSTALL_DIR"

  if [ -w "$INSTALL_DIR" ]; then
    mv "$TMPDIR/$BINARY" "$INSTALL_DIR/anvil"
  else
    sudo mv "$TMPDIR/$BINARY" "$INSTALL_DIR/anvil"
  fi
}

_run_step "Install to $INSTALL_DIR/anvil" _install

# ── Install adapters (optional) ────────────────────────────────────
# _install_adapter moves a downloaded adapter asset into place.
# Usage: _install_adapter <adapter-name>
_install_adapter() {
  _adapter="$1"
  _asset="anvil-adapter-${_adapter}-${OS}-${ARCH}"
  mkdir -p "$INSTALL_DIR"

  if [ -w "$INSTALL_DIR" ]; then
    mv "$TMPDIR/$_asset" "$INSTALL_DIR/anvil-adapter-$_adapter"
  else
    sudo mv "$TMPDIR/$_asset" "$INSTALL_DIR/anvil-adapter-$_adapter"
  fi
}

INSTALLED_ADAPTERS=""
if [ -n "$ADAPTER_LIST" ]; then
  for _adapter in $ADAPTER_LIST; do
    _asset="anvil-adapter-${_adapter}-${OS}-${ARCH}"
    _url="$BASE_URL/$_asset"

    _run_step "Download $_asset" _download_asset "$_asset" "$_url"
    _run_step "Verify checksum" _verify_checksum "$_asset"
    _run_step "Install to $INSTALL_DIR/anvil-adapter-$_adapter" _install_adapter "$_adapter"

    INSTALLED_ADAPTERS="${INSTALLED_ADAPTERS:+$INSTALLED_ADAPTERS, }$_adapter"
  done
fi

# ── Summary ────────────────────────────────────────────────────────
TOTAL_END=$(_now)
TOTAL_TIME=$(_elapsed "$TOTAL_START" "$TOTAL_END")

echo ""
echo "───────────────────"
echo "Anvil installed successfully!"
echo ""
echo "  Binary: $INSTALL_DIR/anvil"
if [ -n "$INSTALLED_ADAPTERS" ]; then
  echo "  Adapters: $INSTALLED_ADAPTERS"
fi
echo "  Time:   $TOTAL_TIME"
echo ""
echo "Run 'anvil --help' to get started."
echo "Run 'anvil init <name>' to create a new project."
