#!/usr/bin/env bash
# verify_checksum.sh — AC2 checksum verification for spike local-deploy-ssh-transport
# Usage: ./verify_checksum.sh <artifact.tar.gz> [remote.tar.gz]
# Exits 0 if checksum valid, 1 otherwise. Uses artifact.VerifyArtifact (manifest.json checksum).

set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <artifact.tar.gz> [remote.tar.gz]" >&2
  echo "  verifies manifest.json checksum + artifact immutability via anvil internal/artifact.VerifyArtifact" >&2
  echo "  if remote path given, also compares file sha256 (idempotency check)" >&2
  exit 2
fi

ART="$1"
REMOTE="${2:-}"

if [[ ! -f "$ART" ]]; then
  echo "artifact not found: $ART" >&2
  exit 1
fi

echo "[verify] checking $ART ..."
go run ./spikes/local-deploy-ssh-transport/scripts/verify.go "$ART" "$REMOTE"
echo "[verify] done"
