package spklocaldeployux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"maleolabs.com/anvil/internal/output"
)

// ── Error Classification Matrix (AC2, AC3) ────────────────────────────

// DeployErrorKind classifies deploy failures for actionable handling.
type DeployErrorKind string

const (
	KindTimeout          DeployErrorKind = "timeout"
	KindUnreachable      DeployErrorKind = "unreachable"
	KindAuthFail         DeployErrorKind = "auth_fail"
	KindPermissionDenied DeployErrorKind = "permission_denied"
	KindVerifyFail       DeployErrorKind = "verify_fail"
	KindConfig           DeployErrorKind = "config"
)

// ErrorSpec defines expected error presentation per kind.
type ErrorSpec struct {
	Kind           DeployErrorKind `json:"kind"`
	Scenario       string          `json:"scenario"`
	ExitCode       int             `json:"exit_code"`
	ShowSSHTarget  bool            `json:"show_ssh_target"`
	SuggestRetry   bool            `json:"suggest_retry"`
	RedactSecrets  bool            `json:"redact_secrets"`
	SampleHumanErr string          `json:"sample_human"`
	SampleJSONErr  string          `json:"sample_json"`
}

// ErrorMatrix is the canonical AC2+AC3 matrix shipped as evidence.
var ErrorMatrix = []ErrorSpec{
	{
		Kind:          KindTimeout,
		Scenario:      "SSH dial timeout / network stall while pushing artifact",
		ExitCode:      output.ExitCodeGeneral, // 1 — general/transport per exitCodesDetail carve-out
		ShowSSHTarget: true,
		SuggestRetry:  true,
		RedactSecrets: true,
	},
	{
		Kind:          KindUnreachable,
		Scenario:      "Unreachable host (DNS failure, no route, connection refused)",
		ExitCode:      output.ExitCodeGeneral, // 1 — general
		ShowSSHTarget: true,
		SuggestRetry:  true,
		RedactSecrets: true,
	},
	{
		Kind:          KindAuthFail,
		Scenario:      "SSH auth fail: wrong key, key rejected by agent",
		ExitCode:      output.ExitCodePrecondition, // 4 — credential precondition
		ShowSSHTarget: true,
		SuggestRetry:  false,
		RedactSecrets: true,
	},
	{
		Kind:          KindPermissionDenied,
		Scenario:      "Permission denied on remote (ssh user not in deploy group, dir not writable)",
		ExitCode:      output.ExitCodePrecondition, // 4
		ShowSSHTarget: true,
		SuggestRetry:  false,
		RedactSecrets: true,
	},
	{
		Kind:          KindVerifyFail,
		Scenario:      "Verification-before-trust gate FAIL (corrupted / tampered artifact)",
		ExitCode:      output.ExitCodeGeneral, // 1 — validation
		ShowSSHTarget: false,
		SuggestRetry:  false,
		RedactSecrets: true,
	},
	{
		Kind:          KindConfig,
		Scenario:      "Missing --target or unknown env in anvil.yaml",
		ExitCode:      output.ExitCodeConfig, // 2
		ShowSSHTarget: false,
		SuggestRetry:  false,
		RedactSecrets: true,
	},
}

// ClassifiedError builds the user-facing AppError for a given kind and target.
// It never leaks secrets: key material / DEPLOY_SSH_KEY values are redacted.
func ClassifiedError(kind DeployErrorKind, sshTarget string, underlying error) *output.AppError {
	sshTarget = SanitizeTarget(sshTarget)
	switch kind {
	case KindTimeout:
		return &output.AppError{
			Message:       fmt.Sprintf("Deploy failed: connection timeout while pushing to %s", sshTarget),
			Reason:        fmt.Sprintf("SSH dial to %s timed out (network stall or host unreachable). Underlying: %v", sshTarget, redactErr(underlying)),
			Resolution:    fmt.Sprintf("Retry: anvil deploy --target <env> (same artifact is resumable). Check host %s is reachable: ssh -v %s 'echo ok'. If persistent, verify firewall / security group.", sshTarget, sshTarget),
			Err:           underlying,
			ExitCodeValue: output.ExitCodeGeneral,
		}
	case KindUnreachable:
		return &output.AppError{
			Message:       fmt.Sprintf("Deploy failed: host unreachable %s", sshTarget),
			Reason:        fmt.Sprintf("Could not connect to %s (DNS failure or connection refused). Underlying: %v", sshTarget, redactErr(underlying)),
			Resolution:    fmt.Sprintf("Verify server.targets.<env> in anvil.yaml: host/user must be resolvable. Try ssh %s 'echo ok' manually. Retry after DNS/network fix.", sshTarget),
			Err:           underlying,
			ExitCodeValue: output.ExitCodeGeneral,
		}
	case KindAuthFail:
		return &output.AppError{
			Message:       fmt.Sprintf("Deploy failed: SSH authentication failed for %s", sshTarget),
			Reason:        fmt.Sprintf("Server rejected key for %s (wrong key or ssh-agent has no key). Underlying: %v", sshTarget, redactErr(underlying)),
			Resolution:    fmt.Sprintf("Check the key for env in anvil.yaml server.targets.<env>.sshKeyPath (file must exist, 0600). Add key to agent: ssh-add <key>. Test: ssh -i <key> %s 'echo ok'. No secret is printed in this message.", sshTarget),
			Err:           underlying,
			ExitCodeValue: output.ExitCodePrecondition,
		}
	case KindPermissionDenied:
		return &output.AppError{
			Message:       fmt.Sprintf("Deploy failed: permission denied on %s", sshTarget),
			Reason:        fmt.Sprintf("Authenticated as %s but remote denied write via SSH (deploy dir not writable or user not allowed). Underlying: %v", sshTarget, redactErr(underlying)),
			Resolution:    fmt.Sprintf("On %s ensure deploy user owns the install dir and has write permission (ssh %s 'ls -ld <installDir>'). Check server_init was run. No credential is leaked.", sshTarget, sshTarget),
			Err:           underlying,
			ExitCodeValue: output.ExitCodePrecondition,
		}
	case KindVerifyFail:
		return &output.AppError{
			Message:       "Deploy failed: artifact verification FAIL — install rejected",
			Reason:        fmt.Sprintf("verification-before-trust gate FAIL (checksum or manifest mismatch). Underlying: %v", redactErr(underlying)),
			Resolution:    "Do not retry the same artifact — rebuild: anvil deploy --target <env> will rebuild, re-verify, then push. If repeat FAIL, check source filtering or disk corruption. Run with --dry-run to inspect verify checks.",
			Err:           underlying,
			ExitCodeValue: output.ExitCodeGeneral,
		}
	case KindConfig:
		return &output.AppError{
			Message:       "Deploy failed: unknown target env",
			Reason:        fmt.Sprintf("server.targets.<env> not found in anvil.yaml. Underlying: %v", redactErr(underlying)),
			Resolution:    "Add the env under server.targets in anvil.yaml: targets:\n  staging: {host: ..., user: ..., sshKeyPath: ...}. Or use --target with an existing env. See anvil deploy --help.",
			Err:           underlying,
			ExitCodeValue: output.ExitCodeConfig,
		}
	default:
		return &output.AppError{
			Message:       fmt.Sprintf("Deploy failed: %s", redactErr(underlying)),
			Reason:        fmt.Sprintf("Unclassified failure (kind=%s). Underlying: %v", kind, redactErr(underlying)),
			Resolution:    "Retry with --dry-run to isolate build/verify. Check anvil deploy --help for target setup.",
			Err:           underlying,
			ExitCodeValue: output.ExitCodeGeneral,
		}
	}
}

func redactErr(err error) string {
	if err == nil {
		return "<nil>"
	}
	return RedactSecrets(err.Error())
}

// SanitizeTarget ensures SSH target shown as user@host (no key material).
func SanitizeTarget(t string) string {
	t = strings.TrimSpace(t)
	t = RedactSecrets(t)
	if t == "" {
		return "unknown-target"
	}
	return t
}

// ── Secret Redaction (AC3) ────────────────────────────────────────────

// RedactSecrets scrubs secret-like substrings from any user-visible string.
// Never leak DEPLOY_SSH_KEY, private key content, or full key paths with secrets.
func RedactSecrets(s string) string {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "deploy_ssh_key") || strings.Contains(lower, "begin open") || strings.Contains(lower, "private") {
		return "***REDACTED***"
	}
	// redact key-like paths: contains id_rsa, id_ed25519, .pem, or generic "key" with slash
	if strings.Contains(s, "/") && (strings.Contains(lower, "key") || strings.Contains(lower, "id_rsa") || strings.Contains(lower, "id_ed25519") || strings.Contains(lower, ".pem")) {
		return filepath.Base(s) + " [REDACTED_PATH]"
	}
	if strings.Contains(lower, "id_rsa") || strings.Contains(lower, "id_ed25519") || strings.Contains(lower, ".pem") {
		return "***REDACTED_KEY***"
	}
	return s
}

// SanitizeLogLine redacts env var values and key paths in a log line.
func SanitizeLogLine(line string) string {
	for _, env := range []string{"DEPLOY_SSH_KEY", "DEPLOY_SERVER_HOST", "DEPLOY_SERVER_USER", "ANVIL_SSH_KEY"} {
		if val := os.Getenv(env); val != "" && strings.Contains(line, val) {
			line = strings.ReplaceAll(line, val, "***REDACTED_"+env+"***")
		}
	}
	if strings.Contains(strings.ToLower(line), ".pem") || strings.Contains(strings.ToLower(line), "id_rsa") || strings.Contains(strings.ToLower(line), "id_ed25519") {
		parts := strings.Fields(line)
		for i, p := range parts {
			lp := strings.ToLower(p)
			if strings.Contains(lp, ".pem") || strings.Contains(p, "id_rsa") || strings.Contains(p, "id_ed25519") {
				parts[i] = filepath.Base(p) + "[REDACTED]"
			}
		}
		line = strings.Join(parts, " ")
	}
	return RedactSecrets(line)
}

// AssertNoSecretLeak checks that s does not contain raw secret values.
func AssertNoSecretLeak(s string) error {
	for _, env := range []string{"DEPLOY_SSH_KEY", "DEPLOY_SERVER_HOST"} {
		if val := os.Getenv(env); val != "" && strings.Contains(s, val) {
			return fmt.Errorf("secret leak: %s value found in output", env)
		}
	}
	lower := strings.ToLower(s)
	// allow [REDACTED] markers but not raw private key headers
	if strings.Contains(lower, "begin open") || strings.Contains(lower, "private key") {
		if !strings.Contains(s, "***REDACTED***") {
			return fmt.Errorf("secret leak: private key material in output")
		}
	}
	return nil
}

// FormatAppErrorHuman returns the 3-part human rendering via internal/output.
func FormatAppErrorHuman(e *output.AppError) string {
	return output.FormatAppError(e)
}
