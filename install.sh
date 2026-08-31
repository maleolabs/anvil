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
#
# With --with-adapters <name[,name...]>, the first-party standard
# executables (laravel, flutter) are resolved from THEIR OWN
# repositories' releases — the registry distribution channel
# (ADR-025 §3.5, ADR-030): since the repository split, adapter binaries
# are published by the standard repositories (maleolabs/anvil-standard-
# <name>), never by Core. Each binary is downloaded from the standard's
# latest release; the release's registry metadata document is verified
# against the installer's PINNED publisher key (detached Ed25519
# signature over the raw document bytes, F-1) BEFORE its
# attestation-bound binary digests are trusted (TS-014-04-04) — a
# same-channel attacker who swaps the binary, the unsigned
# SHA256SUMS.txt, AND the metadata document cannot forge the detached
# signature. Signature verification failure aborts the install; a
# release published WITHOUT the signed material (e.g. the
# already-published v1.0.0) falls back to the release's SHA256SUMS.txt
# with an EXPLICIT warning — never a silent trust downgrade.
#
# Installer trust model: install.sh verifies the CHANNEL-level integrity
# of every downloaded asset (for adapters: the publisher-signed release
# metadata document, verified with the pinned key — the out-of-band
# trust root, same basis as the CLI's anchors — then the
# attestation-bound digests inside it; for releases without the signed
# material: the release's SHA256SUMS.txt). The full registry trust
# validation — content integrity, publisher attestation, and the
# operator's trust anchor allowlist (ADR-022) — is performed by the
# CLI after installation:
#   anvil standard install <id> <version>
#   anvil adapter install <name>
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
# First-party standard repositories (ADR-025 §4.1): adapter binaries
# are release assets of the STANDARD's repository, not of Core. The
# adapter name maps to the standard repo by the identity convention
# (ADR-021 §3.1): <name> → anvil-standard-<name>.
STANDARD_ORG="maleolabs"
STANDARD_REPO_PREFIX="anvil-standard-"

# ── Pinned publisher verification keys (TS-014-04-04, F-1) ─────────
# The out-of-band trust root of the installer (the same basis as the
# CLI's trust anchors allowlist): each first-party standard repository
# signs its release metadata document with its STABLE release-signing
# key (RELEASE_SIGNING_KEY), and the matching Ed25519 public key (PEM,
# PKIX "PUBLIC KEY" block) is pinned HERE — never fetched from the
# release channel. The detached signature
# registry-metadata-<version>.json.sig is verified against this key
# before any digest inside the document is trusted.
#
# RELEASE-GATE: these literals must be provisioned together with the
# standard repositories' stable signing keys (out of band). Until a key
# is provisioned, installs of that standard use the no-material
# fallback (SHA256SUMS.txt + explicit warning) — never silent, never
# fail-closed against releases that merely predate the provisioning.
ANVIL_PUBKEY_LARAVEL=""
ANVIL_PUBKEY_FLUTTER=""

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
      echo "  --with-adapters <list>      Also install standard executables from the standard"
      echo "                              repositories' latest releases (comma-separated:"
      echo "                              laravel,flutter) — each release's metadata"
      echo "                              document is verified against the installer's"
      echo "                              pinned publisher key (Ed25519 detached"
      echo "                              signature, F-1) before its attestation-bound"
      echo "                              digests are trusted; releases without the"
      echo "                              signed material fall back to SHA256SUMS.txt"
      echo "                              with an explicit warning"
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
# Every download is restricted to https (--proto =https): release
# material is never fetched over a plaintext channel.
_download_asset() {
  _asset="$1"
  _url="$2"
  _http_code="$(curl -fsSL --proto =https -w '%{http_code}' -o "$TMPDIR/$_asset" "$_url" 2>/dev/null || true)"
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
# Usage: _verify_checksum <asset-name> [<checksum-url>] [required]
# The checksum URL defaults to CHECKSUM_URL (the Core release channel);
# adapter installs pass the standard repository's release checksum URL
# and mark the verification REQUIRED (fail-closed): an adapter binary is
# never installed without a verified checksum — a checksum file that
# cannot be fetched aborts the install. The CLI download keeps the
# legacy behavior (a missing checksum file skips verification with a
# note).
_verify_checksum() {
  _asset="$1"
  _sums_url="${2:-$CHECKSUM_URL}"
  _required="${3:-0}"
  CHECKSUM_AVAILABLE=0
  CHECKSUM_VERIFIED=0

  if curl -fsSL --proto =https -o "$TMPDIR/SHA256SUMS.txt" "$_sums_url" 2>/dev/null; then
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
  elif [ "$_required" = "1" ]; then
    echo "Checksum file not available - refusing to install $_asset unverified (fail-closed)."
    echo "URL: $_sums_url"
    exit 1
  else
    echo "Checksum file not available, skipping verification"
    return 0
  fi
}

# _base64_decode — `base64 -d` (GNU) or `base64 -D` (BSD/macOS).
_base64_decode() {
  if printf 'eA==' | base64 -d >/dev/null 2>&1; then
    base64 -d
  else
    base64 -D
  fi
}

# _verify_metadata_signature <doc> <sig-b64-file> <pubkey-pem> — verifies
# the DETACHED Ed25519 signature over the RAW document bytes
# (registry-metadata-<v>.json.sig, base64 of the 64-byte signature; F-1)
# with the installer's pinned publisher key. Return codes:
#   0  signature verified
#   1  verification FAILED (document tampered or wrong key) — fail closed
#   2  openssl unavailable — caller degrades to the no-material path
#      (checksum fallback + explicit warning, never silent)
# OpenSSL 3.x verifies raw Ed25519 input with `pkeyutl -rawin`; when that
# form is unsupported (LibreSSL / older OpenSSL) the `dgst -verify`
# variant is tried. The signature is verified over the file bytes as
# fetched — never over a re-serialized document.
_verify_metadata_signature() {
  _doc="$1"
  _sig="$2"
  _key="$3"

  if ! command -v openssl >/dev/null 2>&1; then
    return 2
  fi

  # The pinned key arrives as a PEM literal (ANVIL_PUBKEY_*); openssl
  # needs it on disk.
  _inkey="$TMPDIR/pinned-key-$$.pem"
  printf '%s\n' "$_key" > "$_inkey"
  _sigbin="$TMPDIR/metadata-sig-$$.bin"
  _errlog="$TMPDIR/openssl-err-$$.log"
  cat "$_sig" | _base64_decode > "$_sigbin" 2>/dev/null || {
    rm -f "$_inkey" "$_sigbin" "$_errlog"
    return 1
  }
  if openssl pkeyutl -verify -pubin -inkey "$_inkey" -rawin -sigfile "$_sigbin" -in "$_doc" >/dev/null 2>"$_errlog"; then
    rm -f "$_inkey" "$_sigbin" "$_errlog"
    return 0
  fi
  if grep -qiE "unknown option|invalid option|usage|unrecognized" "$_errlog" 2>/dev/null; then
    if openssl dgst -verify "$_inkey" -signature "$_sigbin" "$_doc" >/dev/null 2>&1; then
      rm -f "$_inkey" "$_sigbin" "$_errlog"
      return 0
    fi
  fi
  rm -f "$_inkey" "$_sigbin" "$_errlog"
  return 1
}

# _latest_release_tag <repo> — resolves the latest release tag of a
# standard repository through the GitHub latest/download redirect (no API
# call, no jq): the final URL after the redirect carries the tag.
# Usage: _latest_release_tag "maleolabs/anvil-standard-laravel"
_latest_release_tag() {
  curl -fsSL --proto =https -o /dev/null -w '%{url_effective}' \
    "https://github.com/$1/releases/latest/download/SHA256SUMS.txt" 2>/dev/null \
    | sed -E 's#^https://github.com/[^/]+/[^/]+/releases/download/([^/]+)/.*#\1#'
}

# _extract_asset_digest <doc> <asset> — extracts the declared sha-256
# digest of the named release asset from the pretty-printed registry
# metadata document produced by the standard release pipeline
# (json.MarshalIndent, 2-space indent; each trust.contentDigests entry
# object carries its "name" field LAST — metadata.go field order).
# Prints nothing when the document carries no entry for the asset (the
# release predates binary attestation, TS-014-04-04). POSIX awk:
# remember the most recent "digest" line; when the asset's name line
# appears, that digest is the same entry's own digest.
_extract_asset_digest() {
  awk -v asset="\"$2\"" '
    /"digest":/ {
      line = $0
      sub(/^.*"digest": "/, "", line)
      sub(/".*$/, "", line)
      digest = line
    }
    /"name":/ && index($0, asset) > 0 { print digest; exit }
  ' "$1"
}

# _verify_attested_digest <asset> <expected> — fail-closed verification
# of a downloaded asset against the attestation-bound digest declared in
# the release's registry metadata document (TS-014-04-04). A mismatch
# means the binary was tampered with or the release is broken — the
# install aborts with an actionable error.
_verify_attested_digest() {
  _asset="$1"
  _expected="$2"

  if command -v sha256sum >/dev/null 2>&1; then
    _actual="$(sha256sum "$TMPDIR/$_asset" | cut -d' ' -f1)"
  elif command -v shasum >/dev/null 2>&1; then
    _actual="$(shasum -a 256 "$TMPDIR/$_asset" | cut -d' ' -f1)"
  else
    echo "Cannot verify $_asset: neither sha256sum nor shasum is available."
    exit 1
  fi

  if [ "$_expected" != "$_actual" ]; then
    echo "Attestation-bound digest mismatch for $_asset — the binary was tampered with or the release is broken."
    echo "  declared: $_expected"
    echo "  actual:   $_actual"
    exit 1
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
    # Adapters resolve from the FIRST-PARTY STANDARD repositories'
    # releases — the registry distribution channel after the repository
    # split (ADR-025 §3.5, ADR-030): the adapter name maps to the
    # standard repo by the identity convention (anvil-standard-<name>,
    # ADR-021 §3.1) and the binary is a release asset of the STANDARD's
    # release, never of Core. The asset naming contract is unchanged
    # (anvil-adapter-<name>-<os>-<arch>, ADR-009 §8.1).
    _adapter_repo="$STANDARD_ORG/$STANDARD_REPO_PREFIX$_adapter"
    _adapter_base="https://github.com/$_adapter_repo/releases/latest/download"
    _asset="anvil-adapter-${_adapter}-${OS}-${ARCH}"

    _run_step "Download $_asset (from $_adapter_repo release)" _download_asset "$_asset" "$_adapter_base/$_asset"

    # ── Attestation-bound verification (TS-014-04-04, F-1) ──────────
    # The release's registry metadata document is verified against the
    # installer's PINNED publisher key (detached Ed25519 signature over
    # the raw document bytes, registry-metadata-<v>.json.sig) BEFORE any
    # digest inside it is trusted — a same-channel attacker who swaps the
    # binary, the SHA256SUMS.txt, AND the document (recomputing the
    # digest) cannot forge the detached signature. The document lives in
    # the SAME release as the binary; its version is resolved from the
    # release tag.
    _tag="$(_latest_release_tag "$_adapter_repo")"
    _meta_version="$(printf '%s' "${_tag#v}" | sed -E 's/-(test|pre)(\.[0-9]+)?$//')"
    _meta_asset="registry-metadata-$_meta_version.json"
    _meta_url="https://github.com/$_adapter_repo/releases/download/$_tag/$_meta_asset"

    if ! curl -fsSL --proto =https -o "$TMPDIR/$_meta_asset" "$_meta_url" 2>/dev/null; then
      echo "Could not fetch the release's registry metadata document ($_meta_url) — refusing to install $_asset unverified."
      echo "The document could not be retrieved (network or channel error). This is NOT the legacy no-material path;"
      echo "re-run the install once the channel is reachable, or report the broken release to the publisher."
      exit 1
    fi

    case "$_adapter" in
      laravel) _pubkey="$ANVIL_PUBKEY_LARAVEL" ;;
      flutter) _pubkey="$ANVIL_PUBKEY_FLUTTER" ;;
      *) _pubkey="" ;;
    esac

    if curl -fsSL --proto =https -o "$TMPDIR/$_meta_asset.sig" "$_meta_url.sig" 2>/dev/null; then
      # ── Signed metadata path ──
      if [ -z "$_pubkey" ]; then
        echo ""
        echo "WARNING: release $_tag of $_adapter_repo carries a detached metadata signature, but the installer has"
        echo "         no pinned publisher key for $_adapter_repo yet (release-gate provisioning). Falling back to"
        echo "         SHA256SUMS.txt (same-channel checksum — weaker trust)."
        echo ""
        _run_step "Verify checksum" _verify_checksum "$_asset" "$_adapter_base/SHA256SUMS.txt" 1
      elif ! command -v openssl >/dev/null 2>&1; then
        echo ""
        echo "WARNING: release $_tag of $_adapter_repo carries a detached metadata signature, but openssl is not"
        echo "         available on this host, so the signature cannot be verified. Falling back to SHA256SUMS.txt"
        echo "         (same-channel checksum — weaker trust)."
        echo ""
        _run_step "Verify checksum" _verify_checksum "$_asset" "$_adapter_base/SHA256SUMS.txt" 1
      else
        if _verify_metadata_signature "$TMPDIR/$_meta_asset" "$TMPDIR/$_meta_asset.sig" "$_pubkey"; then
          printf "  %s %-40s\n" "✓" "Verify publisher signature"
        else
          # rc=2 (openssl unavailable) was handled above; any failure
          # here is a REAL verification failure — fail closed.
          printf "  %s %-40s\n" "✗" "Verify publisher signature"
          echo ""
          echo "Metadata signature verification FAILED for $_meta_asset — the document was tampered with or is not"
          echo "signed by the pinned publisher key of $_adapter_repo. Refusing to install $_asset unverified."
          echo "Re-fetch the release, or report the broken release to the publisher (F-1)."
          exit 1
        fi
        _asset_digest="$(_extract_asset_digest "$TMPDIR/$_meta_asset" "$_asset")"
        if [ -n "$_asset_digest" ]; then
          _run_step "Verify attestation-bound digest" _verify_attested_digest "$_asset" "$_asset_digest"
        else
          # Structural guard (review finding): the verified document
          # carries names but no entry for THIS asset (or no names at
          # all — legacy shape). Never silent.
          if grep -q '"name":' "$TMPDIR/$_meta_asset"; then
            echo ""
            echo "WARNING: the verified metadata of $_tag declares no attestation-bound digest for $_asset."
            echo "         Verifying against SHA256SUMS.txt (same-channel checksum — weaker trust)."
          else
            echo ""
            echo "WARNING: the verified metadata of $_tag declares no named binary digests (legacy release shape)."
            echo "         Verifying against SHA256SUMS.txt (same-channel checksum — weaker trust)."
          fi
          echo ""
          _run_step "Verify checksum" _verify_checksum "$_asset" "$_adapter_base/SHA256SUMS.txt" 1
        fi
      fi
    else
      # ── No detached signature (legacy v1.0.0 release) ──
      echo ""
      echo "WARNING: release $_tag of $_adapter_repo carries no detached metadata signature (releases published"
      echo "         before binary attestation, TS-014-04-04 F-1). Verifying against SHA256SUMS.txt (same-channel"
      echo "         checksum — weaker trust)."
      echo ""
      _run_step "Verify checksum" _verify_checksum "$_asset" "$_adapter_base/SHA256SUMS.txt" 1
    fi

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
  echo "  Adapters: $INSTALLED_ADAPTERS (from the standard repositories' releases)"
  echo ""
  echo "Next: run 'anvil standard install <id> <version>' and 'anvil adapter install <name>'"
  echo "to complete the registry adoption (trust validation with your trust anchors, ADR-022)."
  echo "Note: the installed executables become discoverable after 'anvil adapter install <name>' (registry adoption, ADR-022)."
fi
echo "  Time:   $TOTAL_TIME"
echo ""
echo "Run 'anvil --help' to get started."
echo "Run 'anvil init <name>' to create a new project."
