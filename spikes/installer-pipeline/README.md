# Spike 3 — Pipeline Integration (Isolated Proof)

> Isolated proof-of-concept — NOT prod `anvil installer build`. Prototipe `anvil.yaml#installer` → `anvil installer build --target windows|linux` yang reuse `internal/artifact.Package` & `internal/config` (ADR-005 unified config), wiring ke spike 2 winner interface mock (NSIS + Makeself), idempotent & verification-before-trust, human + --json envelope v1 konsisten dengan `cmd/deploy`, `--dry-run` only verify tanpa build.

## Constraints
- vis:anvil-manifesto, ADR-003 deterministic lifecycle, ADR-005 unified config, spec:artifact-manifest (identity-from-content, checksum)
- Reuse `internal/artifact.Package` + `VerifyArtifact` + `internal/config` resolver/env/redact dimana mungkin
- Simulate NSIS/Makeself invocation (don't need real .exe) — spike 2 winner mock
- Follow `spikes/local-deploy-e2e` evidence pattern — human log + json envelope
- Do NOT modify EKA published objects. Timebox 4 hari. Depends on spike 2 decision (NSIS + Makeself mock).
- Do NOT modify `internal/config` CoreSchema — spike isolates installer schema extension (config.go) mirroring ADR-005.

## Acceptance Criteria
- **AC1** `anvil.yaml` `installer: { name, icon, artifactSource, osTargets }` parsed via ADR-005 unified config, env override & redaction verified → `config.go:LoadInstallerConfig` + `harness.go:verifyAC1`
- **AC2** `anvil installer build --target windows` reuse `internal/artifact.Package` → bundle → invoke tooling (spike 2 winner interface mock) → output installer binary di `dist/installer/` → `pipeline.go:RunPipeline`, `harness.go:verifyAC2`
- **AC3** Pipeline idempotent & verification-before-trust: checksum verify sebelum embed, abort jika manifest invalid (tamper test) → `pipeline.go:verifyBeforeEmbed`, `harness.go:verifyAC3`
- **AC4** Error handling & output: human + --json envelope v1 konsisten dengan `cmd/deploy` pattern, --dry-run only verify tanpa build → `pipeline.go:RenderHuman/RenderJSON`, `harness.go:verifyAC4`

## Structure
- `config.go` — `InstallerConfig` + `LoadInstallerConfig` (ADR-005 4-level resolver: defaults → global file → project `anvil.yaml` → env `ANVIL_CFG_INSTALLER_*`) + `Validate` + `SanitizeInstallerName` + `RedactInstallerLog` + fixtures `anvil.yaml` parsing (icon .ico/.png gate)
- `pipeline.go` — `PipelineConfig`, `PipelineResult`, `RunPipeline` (Package → VerifyBeforeTrust → Builder mock → dist/installer output), `Builder` interface (NSISMock/MakeselfMock), idempotency via `.installer-state.json`, envelope v1 renderers, exit codes
- `harness.go` — `HarnessConfig`, `HarnessResult`, `RunHarness` orchestrating AC1-4 + evidence emission (`build-human.log`, `build-json.log`, `dry-run-human.log`, `dry-run-json.log`, `tamper.log`, `redact-check.log`, `matrix.md`)
- `harness_test.go` — per-AC unit gates
- `cmd/spike/main.go` — CLI: `go run ./spikes/installer-pipeline/cmd/spike [--target windows] [--json] [--dry-run]`
- `evidence/` — generated artifacts (git-kept placeholder + harness outputs)
- `fixtures/anvil.yaml` — sample installer block (name, icon, artifactSource, osTargets)

## Usage
```bash
# full AC1-4 harness (default — writes evidence/*.log + matrix.md)
go run ./spikes/installer-pipeline/cmd/spike

# single target build (like anvil installer build --target windows)
go run ./spikes/installer-pipeline/cmd/spike --target windows --size-mb 2

# json envelope (machine-readable, envelope v1)
go run ./spikes/installer-pipeline/cmd/spike --target linux --json

# dry-run (verify only, no installer binary)
go run ./spikes/installer-pipeline/cmd/spike --target windows --dry-run
go run ./spikes/installer-pipeline/cmd/spike --target windows --dry-run --json

# tests + vet
go test ./spikes/installer-pipeline -v -count=1
go vet ./spikes/installer-pipeline/...

# inspect evidence
cat spikes/installer-pipeline/evidence/build-human.log
cat spikes/installer-pipeline/evidence/build-json.log
cat spikes/installer-pipeline/evidence/matrix.md
```

## Config — anvil.yaml installer block (AC1)
```yaml
project:
  name: demo-app
  version: 1.0.0
installer:
  name: DemoApp
  icon: fixtures/app.ico        # .ico for windows, .png for linux (AC2 gate)
  artifactSource: .             # project root (fed to artifact.Package)
  osTargets: [windows, linux]   # allowed targets; --target must be subset
```
- **Env override (ADR-005 Execution level, ANVIL_CFG_ prefix):**
  - `ANVIL_CFG_INSTALLER_NAME=OverrideName` → `installer.name`
  - `ANVIL_CFG_INSTALLER_ICON=/tmp/icon.ico`
  - `ANVIL_CFG_INSTALLER_ARTIFACT_SOURCE=/tmp/src`
  - `ANVIL_CFG_INSTALLER_OS_TARGETS=windows` (comma-separated)
- **Redaction:** `RedactInstallerLog` masks `ANVIL_SIGNING_KEY`, `PRIVATE KEY`, key paths (`id_rsa`, `.pem`) via `internal/output.RedactSecrets` + `SanitizeLogLine` — verified in `harness_test.go` & `redact-check.log`.

## Pipeline — reuse & idempotency (AC2/AC3)
- **Reuse `internal/artifact.Package`:** `PipelineConfig.SourceDir` resolved dari `installer.artifactSource` → `artifact.Package` dengan include/exclude dari project config; manifest carries identity-from-content + checksum.
- **Verification-before-trust gate:** `verifyBeforeEmbed` calls `artifact.VerifyArtifact` on built tar.gz; if manifest invalid / checksum mismatch → abort before builder invocation, no installer written, error envelope (exit 1) dengan `Reason: verification gate FAIL`.
- **Tooling mock (spike 2 winners):** `NSISMock` (windows → `<Name>-Setup.exe`) + `MakeselfMock` (linux → `<Name>.run`) — writes faithful header + manifest embed header + incompressible payload stub; real NSIS/Makeself would be invoked here in prod (`exec makensis`, `makeself.sh`).
- **Idempotent:** `dist/installer/.installer-state.json` stores `{artifact_id, installer_name, target, checksum, size}`; second run with same artifact checksum + config → skip rebuild, return existing path, log `idempotent: skip rebuild`.

## Output — human + json envelope v1 (AC4)
- **Human:** `RenderHuman` via `output.PlainStepReporter`-style steps (Build artifact ✓, Verify artifact ✓, Build installer ✓) + artifact_id/version/checksum/path
- **JSON envelope v1 (TS-P8-05):** `{"version":"1","status":"success|error","data":{target, dry_run, artifact_id, version, checksum, installer_path, verify:{passed,checks}}}` — `RenderJSON` writes via `output.WriteJSON`/`WriteJSONError` konsisten dengan `cmd/deploy`.
- **--dry-run:** `RunPipeline --dry-run` executes Package + VerifyBeforeTrust only, returns `installer_path=""`, `dry_run:true` — no `dist/installer/` write (AC4).

## Evidence outputs (evidence/)
- `build-human.log` — human path for `--target windows`
- `build-json.log` — json envelope for `--target windows` (machine-readable)
- `dry-run-human.log` / `dry-run-json.log` — dry-runOnly verify
- `tamper.log` — tampered artifact → verify FAIL → abort before embed proof
- `redact-check.log` — redaction proof (ANVIL_SIGNING_KEY masked)
- `matrix.md` — AC1-4 overview + wiring notes for `forge-anvil-cli` integration

## Integration — how to wire into forge-anvil-cli (for report)
See `pipeline.go` section `Integration notes` + `evidence/matrix.md` Next Steps. Summary: add `installer.*` to `internal/config.CoreSchema`, register `anvil installer` parent cobra command (`cmd/installer.go`) with `build` subcommand mirroring `RunPipeline` wiring, reuse `Builder` from `internal/installer` (NSIS + Makeself, real toolchains gated by build tag / env), add `dist/installer/` to `.gitignore` + artifact output.
