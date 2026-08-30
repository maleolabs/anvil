# Spike 1 — SSH Transport Latency & Retry (Isolated Proof)

> Isolated proof-of-concept — NOT prod `anvil deploy`. Validasi distribusi 50 artifact push via SSH (scp) ke VPS dummy single target, ukur p50/p95 latency, retry idempotency jika putus di tengah.

## Constraints
- vis:anvil-manifesto, ADR-003 deterministic lifecycle, ADR-016 deployment model.
- spec:artifact-manifest (manifest.json schema: artifact_id, version, checksum, ...)
- verification-before-trust, idempotency, audit trail.

## Acceptance Criteria
- AC1: 50 artifact (tar.gz + manifest.json) berhasil push via SSH scp dengan p95 < 30s per artifact (size ~50MB lab).
- AC2: Retry setelah simulasi disconnect (kill mid-transfer) idempotent — re-push tidak corrupt, checksum manifest.json valid.
- AC3: SSH auth via ssh-agent / key-path dari anvil.yaml tidak expose secret di log.
- AC4: Laporan latency histogram + failure mode classification (auth fail, timeout, partial write).

## Structure
- `histogram.go` — latency collection, p50/p95, CSV + bucket histogram.
- `simulated_transport.go` — SimulatedTransport: local-FS "remote" dengan latency injection & failure classification (tanpa butuh VPS real). Mendukung mode real SSH jika `SPIKE_SSH_HOST` set.
- `harness.go` — orchestrates 50 artifact generation (via `internal/artifact.Package`) + push + retry.
- `verify.go` — checksum verification via `artifact.VerifyArtifact` + manifest.json checksum compare.
- `cmd/spike/main.go` — CLI harness: `go run ./spikes/local-deploy-ssh-transport/cmd/spike` → histogram.csv + retry.log
- `harness_test.go` — unit tests: histogram, retry idempotency, no secret leak, artifact manifest compliance.

## Usage (worktree)
```bash
# run harness (simulated, no SSH server needed)
go run ./spikes/local-deploy-ssh-transport/cmd/spike --count 50 --size-mb 5

# run tests
go test ./spikes/local-deploy-ssh-transport -v

# vet + validate
go vet ./spikes/local-deploy-ssh-transport/...
eka validate
```

Evidence outputs in `evidence/`:
- `histogram.csv` — per-artifact latency + summary p50/p95
- `retry.log` — retry attempts dengan failure classification & checksum verify

## Security
- Logs redact `KeyPath`, private key material, dan `DEPLOY_SSH_KEY` value.
- Gunakan `RedactSecrets()` sebelum log.
- SSH auth prefer ssh-agent; key-path fallback tidak pernah di-print full.

## Idempotency
- Transfer via temp file `<remote>.tmp.<rand>` → `fsync` → atomic `rename`.
- Partial write tidak corrupt final artifact; retry overwrites temp & rename atomically.
- Checksum verified after retry via `artifact.VerifyArtifact` + manifest.json `checksum` field.

## Real SSH Mode
Set env untuk push ke VPS dummy real:
```
DEPLOY_SERVER_HOST=1.2.3.4 DEPLOY_SERVER_USER=deploy DEPLOY_SSH_KEY=/path/key go run ./spikes/local-deploy-ssh-transport/cmd/spike --real-ssh
```
Jika tidak set, harness otomatis pakai simulated transport (local FS).
