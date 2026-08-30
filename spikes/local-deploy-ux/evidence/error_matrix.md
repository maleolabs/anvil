# Error Matrix — `anvil deploy` local UX (AC2+AC3)

| kind | scenario | exit_code | show_ssh_target | suggest_retry | redact_secrets |
|------|----------|-----------|-----------------|---------------|----------------|
| timeout | SSH dial timeout / network stall while pushing artifact | 1 | true | true | true |
| unreachable | Unreachable host (DNS failure, no route, connection refused) | 1 | true | true | true |
| auth_fail | SSH auth fail: wrong key, key rejected by agent | 4 | true | false | true |
| permission_denied | Permission denied on remote (ssh user not in deploy group, dir not writable) | 4 | true | false | true |
| verify_fail | Verification-before-trust gate FAIL (corrupted / tampered artifact) | 1 | false | false | true |
| config | Missing --target or unknown env in anvil.yaml | 2 | false | false | true |

Notes:
- Exit codes align with `internal/output/exitcode.go` + `cmd/help.go` conventions (0 success, 1 general/transport, 2 config, 4 precondition). Network/timeout stays 1 per carve-out (network failures are general, not runtime not-found). Auth/permission is 4.
- All errors show three-part format: Error / Reason / Resolution (via internal/output.AppError).
- Human + JSON both present: JSON envelope {version:1,status:error,error:msg} for machine, human for terminal.
- Secrets (DEPLOY_SSH_KEY, /home/.../id_ed25519, .pem) are redacted to [REDACTED] — verified in redaction_check.txt.
