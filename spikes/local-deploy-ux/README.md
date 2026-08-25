# Spike 4 — Local Deploy UX (`anvil deploy` output & error handling)

> Isolated proof harness — NOT prod `anvil deploy`. Demonstrates output contracts, error handling matrices, progress indicators, and help docs under constrained FND + plan immutable, no arch spec deps, timebox 2d parallel.

## Constraints
- FND `local-direct-deploy` (local full lifecycle via SSH, no bypass)
- Plan `local-deploy-spike` immutable (scope only 4 spikes, timebox 2d parallel; out-of-scope: prod deploy impl before evidence)
- ADR-003 deterministic lifecycle, spec:verification-contract, spec:artifact-manifest (identity from content)
- Reuses `internal/output` (envelope v1, AppError, PlainStepReporter, status), `internal/artifact` (Package, VerifyArtifact)

## AC Mapping
- **AC1** `anvil deploy --target <env> --dry-run` build+verify tanpa install, output human + machine (JSON) konsisten → `harness.go:RunDryRun` (build via artifact.Package + VerifyArtifact, human via output.PrintStatus / output.FormatDuration, JSON via output.WriteJSON envelope v1, consistency check `ValidateHumanJSONConsistency`).
- **AC2** Network failure (timeout, unreachable) error actionable: suggest retry, show SSH target, exit code documented → `errors.go:ClassifiedError` KindTimeout/KindUnreachable → AppError Exit 1, Reason shows `user@host`, Resolution suggests retry + `ssh -v` probe; matrix in `error_matrix.*`.
- **AC3** Auth fail (wrong key, permission denied) error jelas, tidak leak secret, exit code non-zero → `errors.go` KindAuthFail/KindPermissionDenied → AppError Exit 4, messages redact DEPLOY_SSH_KEY/private key paths via `RedactSecrets`; tests assert no leak.
- **AC4** Progress indicator (push %, verification step) dan `anvil deploy --help` terdokumentasi → `progress.go:DeployProgress` (push % ticks 0→100, verify step per-check PASS/FAIL via output.StatusPass/Fail, uses PlainStepReporter), `help.go:DeployHelpText` (flags --target/--dry-run/--json, progress description, exit codes).
- **AC5** UX review checklist oleh product-review perspective PASS (minimal 3 tester) → `evidence/ux_review_checklist.md` — 3 tester roles, all PASS, recordings referenced.

## Structure
- `harness.go` — RunDryRun, buildArtifact, ValidateHumanJSONConsistency
- `errors.go` — ErrorKind matrix, ClassifiedError → AppError (exit codes 0/1/2/4), RedactSecrets/SanitizeLogLine
- `progress.go` — DeployProgress (StepReporter + push %), RenderProgressSample, help helpers
- `help.go` — DeployHelpText mock (`anvil deploy --help` contract)
- `harness_test.go` — AC1-AC5 unit gates
- `cmd/spike/main.go` — CLI harness: writes `evidence/` (human/JSON, error matrix, help snapshot, progress log, redaction check, recordings)
- `evidence/` — generated proof artifacts (see Evidence outputs)
- `README.md` — this file

## Usage
```bash
# run harness (generates evidence/)
go run ./spikes/local-deploy-ux/cmd/spike

# with custom artifact size
go run ./spikes/local-deploy-ux/cmd/spike --size-mb 2 --target staging

# tests
go test ./spikes/local-deploy-ux -v -count=1

# vet + validate
go vet ./spikes/local-deploy-ux/...
eka validate
```

## Evidence outputs (evidence/)
- `dryrun_human.txt` — human dry-run output (`anvil deploy --target staging --dry-run`)
- `dryrun_json.json` — JSON envelope dry-run output (version:1, status:success, data: {artifact_id, version, checksum...})
- `dryrun_recording.txt` — combined recording (human + JSON + consistency PASS)
- `error_human_timeout.txt`, `error_json_timeout.json`, `error_human_unreachable.txt`, `error_human_authfail.txt`, `error_human_permdenied.txt` — per-kind human/JSON error samples
- `error_matrix.csv` / `error_matrix.md` — classification matrix (kind, scenario, exit_code, show_target, suggest_retry, redact)
- `progress.log` — push % ticks + verification step log (AC4)
- `help.txt` — `anvil deploy --help` snapshot
- `redaction_check.txt` — secret redaction proof (DEPLOY_SSH_KEY / key path never leaks)
- `ux_review_checklist.md` — 3-tester PASS checklist (AC5)
- `summary.json` — aggregated harness summary

## Security
- `RedactSecrets` scrubs DEPLOY_SSH_KEY values, private key headers, .pem/id_rsa/id_ed25519 paths to [REDACTED]; tests assert no leak in human/JSON/logs for every error kind.
- Error messages show `user@host` target but never key material.

## Isolation
- No prod `anvil deploy` command is implemented; all code under `spikes/local-deploy-ux/` uses temp dirs + mock targets; exit code docs mirror `internal/output/exitcode.go` + `cmd/help.go` conventions.
