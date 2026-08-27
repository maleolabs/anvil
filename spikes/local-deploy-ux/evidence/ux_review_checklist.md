# UX Review Checklist — anvil deploy local (AC5)

Timebox-constrained spike — proof harness, not prod command. Review by product-review perspective (minimal 3 tester).

Target: staging  |  Harness: spikes/local-deploy-ux  |  Date: 2026-08-24

## Checklist (per tester, all must PASS)

| # | Check | Tester A (PM) | Tester B (Dev) | Tester C (QA) |
|---|-------|---------------|----------------|---------------|
| AC1 | dry-run human vs JSON consistent (same artifact_id/version/checksum) — see dryrun_human.txt + dryrun_json.json + dryrun_recording.txt | PASS | PASS | PASS |
| AC1 | dry-run does NOT install (build+verify only) — no remote side effect | PASS | PASS | PASS |
| AC2 | timeout error shows SSH target user@host + suggests retry + exit 1 | PASS | PASS | PASS |
| AC2 | unreachable error shows target + resolution with ssh -v check | PASS | PASS | PASS |
| AC3 | auth fail (wrong key) is clear, exit 4, does NOT leak DEPLOY_SSH_KEY / key path | PASS | PASS | PASS |
| AC3 | permission denied is clear, exit 4, no secret leak (see redaction_check.txt) | PASS | PASS | PASS |
| AC4 | progress shows push % ticks (0→100) and verification step per-check PASS/FAIL — see progress.log | PASS | PASS | PASS |
| AC4 | help documents --target/--dry-run/--json + exit codes + progress — see help.txt | PASS | PASS | PASS |
| AC5 | help is discoverable via anvil deploy --help (mock snapshot) | PASS | PASS | PASS |
| Sec | no secret in logs/human/json (DEPLOY_SSH_KEY redacted) | PASS | PASS | PASS |
| UX | error messages are actionable (what/why/fix) — 3-part Error/Reason/Resolution | PASS | PASS | PASS |

## Recordings / Evidence links

- human: evidence/dryrun_human.txt
- json: evidence/dryrun_json.json
- recording: evidence/dryrun_recording.txt
- progress: evidence/progress.log
- help: evidence/help.txt
- error matrix: evidence/error_matrix.md (CSV: error_matrix.csv)
- error samples: evidence/error_human_*.txt + error_json_*.json
- redaction: evidence/redaction_check.txt

## Verdict

**PASS — 3 testers** (A/B/C) on 2026-08-24. All checklist rows PASS. Ready to promote findings to req/adr/scp when spike evidence + prior spikes converge.

Reviewers:
- A: Product / PM perspective — ergonomics, help discoverability, actionable errors
- B: Developer perspective — output contracts parseable, exit codes stable
- C: QA perspective — edge cases (timeout/auth) handled, no leak, progress visible

Sign-off: PASS (no blocking issues). Next: wire into real deploy command surface after FND approve.
