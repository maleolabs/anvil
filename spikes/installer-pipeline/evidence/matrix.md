# Spike 3 — Pipeline Integration Matrix (AC1–AC4)

> repo=. output=dist/installer generated via `go run ./spikes/installer-pipeline/cmd/spike`

## AC1 — anvil.yaml installer block (ADR-005 unified config)

- **installer.name**: `DemoApp` (sanitized: `DemoApp`)
- **installer.icon**: `spikes/installer-pipeline/fixtures/app.ico`
- **installer.artifactSource**: `.`
- **installer.osTargets**: `[windows linux]` (resolvedFrom: project:anvil.yaml)
- **redact**: true — `ANVIL_SIGNING_KEY` / `id_rsa` masked via `internal/output.RedactSecrets` (see `redact-check.log`)

## AC2 — anvil installer build --target windows (reuse internal/artifact.Package → bundle → tooling mock)

- **windows**: `DemoApp-Setup.exe` size=1678311 bytes artifact_id=c11ab3a752170191 checksum=c11ab3a752170191 via `NSISMock` (simulated, real would exec `makensis`)
  - verify: 6 checks PASS (see `build-human.log`)
- **linux**: `DemoApp.run` size=49758 bytes via `MakeselfMock` (real would exec `makeself.sh`)
- **output**: `dist/installer/` — `DemoApp-Setup.exe` + `DemoApp.run` (+ `.installer-state.json` for idempotency)

## AC3 — Idempotent & verification-before-trust

- **idempotent**: true — second build same checksum+config → skip rebuild (hash via `.installer-state.json`, see `idempotent.log`)
- **verify-before-trust**: true — tampered artifact → `VerifyArtifact` FAIL → abort before embed (no installer written), see `tamper.log`

## AC4 — Error handling & output (human + --json envelope v1, --dry-run)

- **human**: `RenderHuman` via `output.PlainStepReporter`-style steps (Build artifact ✓, Verify artifact ✓, Build installer ✓)
- **json**: envelope v1 `{"version":"1","status":"success","data":{target,dry_run,artifact_id,version,checksum,installer_path,verify}}` via `output.WriteJSON` konsisten dengan `cmd/deploy`
- **human+JSON consistent**: true — same artifact_id/version/checksum in both (ValidateHumanJSONConsistency)
- **--dry-run**: `dry-run-human.log` (verify only) + `dry-run-json.log` (envelope with `dry_run:true`), no `dist/installer` write
- **error envelope**: unsupported target → `{"version":"1","status":"error","error":"..."}` exit 2 (config), tamper → exit 1 (general)

## Code Structure

- `config.go` — `InstallerConfig`, `LoadInstallerConfig` (4-level resolver), `SanitizeInstallerName`, `RedactInstallerLog`, icon gate (.ico/.png)
- `pipeline.go` — `Builder` (NSISMock/MakeselfMock), `RunPipeline` (Package → VerifyBeforeTrust → Builder → dist/installer), `RenderHuman`/`RenderJSON` (envelope v1), idempotent state, exit codes
- `harness.go` — `RunHarness` AC1-4 + evidence emission
- `cmd/spike/main.go` — CLI: `go run ./spikes/installer-pipeline/cmd/spike [--target windows] [--json] [--dry-run]`

## How to Integrate into forge-anvil-cli

1. **Schema** — add to `internal/config/schema.go` `CoreSchema()`: `installer.name` (string, required), `installer.icon` (string), `installer.artifactSource` (string, default `.`), `installer.osTargets` (array, default `[windows,linux]`), with validation mirroring `config.go:ValidateInstallerConfig`.
2. **Command** — add `cmd/installer.go`: parent `installer` cobra command (`Use: "installer"`) with `build` subcommand (`anvil installer build --target windows|linux [--dry-run] [--json]`) wiring `LoadInstallerConfig` → `RunPipeline` (promoted from spike to `internal/installer/pipeline.go`). Reuse existing `AddJSONFlag` + `ReportError`/`output.AppError` + `SanitizeLogLine` pattern from `cmd/deploy.go`.
3. **Builder** — promote `spikes/installer-pipeline/pipeline.go:Builder` to `internal/installer/builder.go` (NSIS + Makeself). Real toolchains gated: `exec.LookPath("makensis")` / `makeself.sh` when present, else simulated (CI). Icon fixture validation via `VerifyIcon` (spike 2 helpers).
4. **Idempotency** — keep `dist/installer/.installer-state.json` exactly as spike; respect `.gitignore` entry for `dist/`.
5. **Tests** — promote `harness_test.go` AC1-4 as `internal/installer/*_test.go` + `cmd/installer_test.go` (human vs json consistency via `ValidateHumanJSONConsistency`).
6. **Docs** — add `anvil installer --help` + `anvil.yaml` installer block to `docs/` + update `anvil-cli/fnd:anvil-installer` with conclusion linking `spikes/installer-pipeline/evidence/matrix.md`.
