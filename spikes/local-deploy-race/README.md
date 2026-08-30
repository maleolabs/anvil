# Spike 3 — Race local-vs-CI concurrent deploy + state locking

> Isolated proof-of-concept — NOT prod `anvil deploy`. Validates concurrent deploy determinism when local and CI deploy to same target simultaneously, with `runtime.OperationLock` flock preventing dual-active (ADR-003 only one active).

## Constraints
- ADR-003 deterministic lifecycle (only-one-active)
- ADR-014 baseline safety: reject concurrent activation/rollback
- ADR-031 keep list: locking + state survives crashes
- ADR-036 §3 lifecycle never depends on diagnostics (lock never gates on condition)
- FND local-direct-deploy (state drift local-vs-CI race)
- spec:lifecycle-model (deterministic lifecycle)

## AC Mapping
- **AC1** Simulasi concurrent deploy (local + CI mock) — hanya satu menang, deterministic, tidak ada dual-active → `harness.go:runConcurrentActivateRace` (barrier + goroutines + `coordinator.Activate` with `runtime.OperationLock`), `AssertOnlyOneActive`, state dumps before/after.
- **AC2** State locking / optimistic reject terbukti — deploy kedua ditolak atau queued dengan error jelas, tidak corrupt `/.anvil` state → `runLockContentionProof` (8 contenders flock, exactly one wins, LOCK_NB reject `another lifecycle operation is in progress`), `runConcurrentInstallRace` (holder holds lock 300ms, 2 contenders rejected), `checkStateIntegrity` (runtime-state.json + releases parseable).
- **AC3** Idempotency: retry deploy yang kalah tidak create duplicate release → `runIdempotencyCheck` (retry same artifactID 3x, expect `already installed`, release count unchanged, no duplicate JSON).
- **AC4** Dokumentasi rekomendasi guard: dev allow local, prod require allowlist/confirm (input ADR) → `harness.go:BuildGuardRecommendation` + `evidence/guard_recommendation.md` (dev allow, staging soft gate --confirm, prod CI-only + allowlist + prompt).

## Structure
- `harness.go` — orchestrates Build → Negotiate → Push → Lock contention (8 goroutines) → Install race (holder simulation) → Activate race (barrier) → Idempotency → Integrity + guard doc.
- `transport_mock.go` — `LocalFSTransport` mocks SSH via local FS atomic rename, latency simulation, failure classification. Reused from spike2.
- `mock_target.go` — `MockTarget` implements `deployment.Target` compatibility check.
- `verifier.go` — verification gate helpers, manifest schema validation.
- `status.go` — status query wrapping `server.QueryLifecycleStatus` + release queries.
- `audit.go` — AuditLogger JSON-lines (kept for completeness, not central to race).
- `harness_test.go` — tests per AC.
- `cmd/spike/main.go` — CLI: `go run ./spikes/local-deploy-race/cmd/spike` → `evidence/race.log`, `evidence/lock.log`, `evidence/state_*.json`, `evidence/guard_recommendation.md`, `evidence/summary.json`.

## Usage
```bash
# run full race harness (simulated, no VPS needed)
go run ./spikes/local-deploy-race/cmd/spike --deployer-user devuser

# run with custom size
go run ./spikes/local-deploy-race/cmd/spike --deployer-user devuser --size-mb 1

# run tests
go test ./spikes/local-deploy-race -v -count=1

# vet + validate
go vet ./spikes/local-deploy-race/...
eka validate
```

## Evidence outputs (evidence/)
- `race.log` — full harness run log (concurrent run logs + state dumps + lock behavior proof)
- `concurrent.log` — copy of race log focused on concurrent section
- `lock.log` — lock contention proof (8 contenders, holder errors, state dump)
- `state_before.json` / `state_after_install.json` / `state_after_activate.json` — state dumps before/after (AC1 deterministic, AC2 no corruption)
- `artifact_local.json` / `artifact_ci.json` — manifest snapshots (identity-from-content)
- `guard_recommendation.md` — AC4 guard doc (dev allow local, prod allowlist/confirm)
- `summary.json` — full RaceResult JSON (machine evidence)

## Security
- Lock file `runtime-state.lock` mode 0600 (owner-only), O_NOFOLLOW + regular-file check, self-healing chmod (see `internal/runtime/lock.go`).
- Audit log redaction via `RedactSecrets`/`SanitizeLogLine`.
- No secrets in evidence logs.

## Isolation
- No prod `anvil deploy` command implemented; all code under `spikes/local-deploy-race/` uses temp dirs.
- Reuses spike2 harness as base but implements race isolation with `runtime.OperationLock` flock, optimistic concurrency, goroutine barrier.

## Depends on
- spike2 artifact harness (`spikes/local-deploy-e2e`) — transport mock pattern, verifier, status helpers.
- `internal/runtime.OperationLock` flock (cross-process).
- `internal/server.ServerReleaseCoordinator` (Install/Activate with embedded locking + idempotency).
