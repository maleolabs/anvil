# Spike 1 — Installer Boundary Proof (Isolated Harness)

> Isolated proof — NOT prod installer. Validates dumb-wrapper + standard-owned setup:
> `artifact.Package` → installer dummy (zip) → extract to user-chosen lokasi → trigger `anvil standard setup` (mock: `php artisan migrate --force && db:seed` + `storage:link`) → verify super-admin + storage:link. AC4: idempotency / rollback on cancel & migrate fail.

## Constraints
- vis:anvil-manifesto §4 domain ownership (installer dumb wrapper, standard owns Laravel setup)
- ADR-003 lifecycle, ADR-005 config
- spec:artifact-manifest (manifest.json identity-from-content, checksum)
- spec:lifecycle-model
- Reuse `internal/artifact.Package` / `VerifyArtifact` / `ExtractArtifact` where possible
- Do NOT modify published EKA objects. Timebox 5 hari. Spike code is isolated under `spikes/installer-boundary/`.

## AC Mapping
- **AC1** Laravel sample via `artifact.Package` → `tar.gz` + `manifest.json` identity-from-content, checksum valid spec:artifact-manifest → `harness.go:BuildLaravelArtifact`, `verifier.go:ValidateManifestSchema`.
- **AC2** Bundle artifact to installer dummy (zip + shell script) → extract to user-chosen lokasi → trigger standard hook `php artisan migrate --force && db:seed` via contract → verify super-admin + storage:link PASS (mock if Laravel not available; contract documented) → `installer.go:BuildInstaller`, `standard_hook.go:MockStandardSetup.Setup`, `installer.go:Install`, `harness.go:RunHarness`.
- **AC3** Trigger point jelas: `extract → invoke embedded anvil runtime anvil standard setup` — document interface, not shell duplikasi logic → `standard_hook.go:StandardSetup` interface, `installer.go:installerScriptContent` shows `anvil standard setup --install-root`, `Install` calls `hook.Setup` (no shell duplication). AC3 test asserts installer script contains `anvil standard setup` and does NOT contain `migrate --force`.
- **AC4** Idempotency / rollback on cancel & migrate fail → `installer.go:Install` uses staging tmp + `.anvil-install-state.json` for idempotency, `CancelAfterBytes` injection for cancel mid-extract (no corrupt, retry safe), `MockStandardSetup.FailNext` for migrate fail → rollback + actionable error.

## Boundary Contract (AC3)

```
┌─────────────────────┐        extract         ┌──────────────────────┐
│ Installer (dumb     │  ──────────────────►   │ anvil standard setup │
│ wrapper)            │   $INSTALL_ROOT chosen │ (anvil-standard-     │
│ - bundle artifact   │   by user              │  laravel owned)      │
│ - zip + installer.sh│   trigger point:       │ - migrate --force    │
│ - does NOT run     │   hook.Setup(ctx,      │ - db:seed            │
│   migrate/seed      │   installRoot)         │ - storage:link       │
└─────────────────────┘                        └──────────────────────┘
```

- Installer interface: `StandardSetup.Setup(ctx, installRoot)` — future prod: `anvil standard setup --install-root <path>` reading `activation_commands` from manifest (ADR-017).
- State: `<installRoot>/.anvil-install-state.json` holds `artifact_id` for idempotency. Install lokasi vs `~/.anvil` (global runtime state) is separate: installer respects user-chosen installRoot; global runtime config stays in `~/.anvil` (future container support).
- Installer is variant-aware: `linux-makeself` (`installer.sh`) vs `windows-nsis` (`installer.bat`) — both dumb wrappers, same delegation.

## Structure
- `harness.go` — `BuildLaravelArtifact` + `RunHarness` orchestrating AC1-4; idempotency + cancel + rollback scenarios.
- `installer.go` — `BuildInstaller` (zip bundling) + `Install` (dumb-wrapper extract → hook → verify → state → rollback).
- `standard_hook.go` — `StandardSetup` interface + `MockStandardSetup` (simulates `migrate --force`, `db:seed` super-admin, `storage:link`) + `VerifyStandardHookResults`.
- `verifier.go` — manifest schema + verification helpers (reuses `internal/artifact`).
- `harness_test.go` — per-AC tests.
- `cmd/spike/main.go` — CLI: `go run ./spikes/installer-boundary/cmd/spike` → `evidence/*.log`, `evidence/*.json`.
- `evidence/` — git-kept placeholder + harness outputs.

## Usage

```bash
# run harness (simulated, no Laravel/PHP needed)
go run ./spikes/installer-boundary/cmd/spike

# custom variant / version
go run ./spikes/installer-boundary/cmd/spike --variant windows-nsis --version 1.2.3 --install-root /tmp/myapp

# run tests
go test ./spikes/installer-boundary -v -count=1

# vet
go vet ./spikes/installer-boundary/...
```

## Evidence outputs (evidence/)
- `install.log` — full harness log (AC1-4)
- `verify.log` — per-check verification output
- `artifact.json` — manifest snapshot
- `installer.json` — installer metadata
- `install-state.json` — installed state marker copy
- `friction-checklist.md` — auto-generated (populated by harness)
- `install-windows.log` / `install-cancel.log` / `install-migrate-fail.log` — via tests
- On real run: also `summary.json`

## Isolation
- Temp dirs only, no prod side effects, no published EKA mutation.
- All code under `spikes/installer-boundary/`; resumable via `go test` / `go run`.
- Real Laravel not required; mock documents contract so promotion can replace MockStandardSetup with `anvil-standard-laravel`.

## Friction Checklist (pre vs post)
See `evidence/friction-checklist.md` after `go run`. Summary:
- Before installer: manual `scp`/`git clone`, edit `.env`, `composer install`, `php artisan migrate --force`, `php artisan db:seed`, `php artisan storage:link`, fix perms — 7+ manual steps Windows vs Linux diverged.
- After dumb-wrapper: user runs installer → chooses lokasi → installer extracts → `anvil standard setup` automates migrate/seed/link — 1 step, standard owns logic, idempotent retry.

## Promotion blockers
- Replace mock with real `anvil-standard-laravel` + real DB (sqlite/mysql) in VM; verify `storage:link` symlink on Windows (NSIS needs admin or junction fallback).
- `~/.anvil` vs install lokasi ownership final decision (ADR needed).
- Authentic Authenticode / Notarization for Windows NSIS / Linux Makeself not covered in spike.
