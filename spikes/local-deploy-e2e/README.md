# Spike 2 — Local → VPS Full Lifecycle E2E (Isolated Proof Harness)

> Isolated proof-of-concept — NOT prod `anvil deploy`. Validates the full deterministic lifecycle
> locally: `anvil build` → push (simulated SSH) → `Coordinator.Install` → Verify (verification-contract)
> → Activate → `anvil status` → Rollback, with audit trail.

## Constraints
- ADR-003 deterministic lifecycle (only-one-active, enforced transitions)
- FND local-direct-deploy (full lifecycle via SSH, no bypass)
- spec:artifact-manifest (manifest.json: artifact_id from content, version, checksum, checksum_type, project_id)
- spec:lifecycle-model (Ready → Activating → Active → Archived/RollingBack …)
- spec:verification-contract (verification-before-trust gate before Activate)
- deployment.Negotiate capability check, state-before-assumptions, identity-from-content

## AC Mapping
- **AC1** `anvil build` lokal menghasilkan artifact + manifest.json valid → `artifact.Package` + `artifact.VerifyArtifact` + manifest field checks (`harness.go:BuildArtifact`).
- **AC2** Push + `Coordinator.Install` sukses, Negotiate PASS → `deployment.Negotiate` + `LocalFSTransport.Deliver` (mock SSH via atomic rename) + `ServerReleaseCoordinator.Install` (`harness.go`).
- **AC3** Verification gate PASS sebelum Activate; Activate ditolak jika verify FAIL → `VerifiedActivate` checks `artifact.VerifyArtifact` BEFORE `Coordinator.Activate`; negative test tampers artifact and asserts Activate rejected (`verifier.go`, `harness.go`).
- **AC4** Activate sets release active, `anvil status` observable, Rollback restores previous, lifecycle enforced → `release.GetActiveRelease`, `server.QueryLifecycleStatus`, `Coordinator.Rollback`, state-machine invariant checked (`status.go`, `harness.go`).
- **AC5** Audit trail who deployed logged di server → `AuditLogger` appends JSON lines `{user, timestamp, action, artifact_id, release_id}` to `<installRoot>/audit.log` (`audit.go`).

## Structure
- `harness.go` — orchestrates Build → Negotiate → Push → Install → Verify → Activate → Status → Rollback + audit.
- `transport_mock.go` — `LocalFSTransport` implements `deployment.Transport` via local FS + atomic `tmp → rename`, throughput latency simulation, failure classification. Reuses pattern from spike1 `SimulatedTransport` but adapts to `deployment.Transport.Deliver`.
- `mock_target.go` — `MockTarget` implements `deployment.Target` with `ValidateCompatibility`; `FailingTarget` for negative negotiation test.
- `verifier.go` — verification gate helpers, tamper helper for negative test, manifest schema validation.
- `audit.go` — `AuditLogger` (SSH user + timestamp JSON-lines) with redaction.
- `status.go` — status query helpers wrapping `server.QueryLifecycleStatus` + `release` queries, formatted logging.
- `harness_test.go` — unit/e2e tests per AC.
- `cmd/spike/main.go` — CLI harness: `go run ./spikes/local-deploy-e2e/cmd/spike` → `evidence/e2e.log`, `evidence/verify.log`, `evidence/status.json`, `evidence/rollback.log`, `evidence/audit.log`.

## Usage

```bash
# run full e2e harness (simulated, no VPS needed)
go run ./spikes/local-deploy-e2e/cmd/spike --deployer-user devuser

# run with custom sizes
go run ./spikes/local-deploy-e2e/cmd/spike --deployer-user devuser --size-mb 1

# run tests
go test ./spikes/local-deploy-e2e -v -count=1

# vet + validate
go vet ./spikes/local-deploy-e2e/...
eka validate
```

## Evidence outputs (evidence/)
- `e2e.log` — full harness run log
- `verify.log` — per-check verification output (PASS/FAIL per check)
- `status.json` — status snapshots after Activate1, Activate2, Rollback
- `rollback.log` — rollback result
- `audit.log` — audit trail JSON-lines (user + timestamp)
- `artifact1.json` / `artifact2.json` — manifest snapshots

## Security
- Audit log never stores private key material; `RedactSecrets` scrubs `KeyPath`, `DEPLOY_SSH_KEY`.
- Transport logs only basenames, not full paths with secrets.

## Isolation
- No prod command `anvil deploy` is implemented; all code is under `spikes/local-deploy-e2e/` and uses temp dirs.
- Simulated transport: local FS mock; set `SPIKE_SSH_HOST` not required here — spike1 covers real SSH.
