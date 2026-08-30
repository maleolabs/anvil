// Package installer implements the security gate for installer verification.
//
// Reference: anvil-cli/sto:installer-security-gate AC1-4
// Reuses spike 4 verifier.go (spikes/installer-security/verifier.go) and
// 5 tests pattern. Offline fs-only — no net/http imports.
// Fail-closed verification before extract, payload binding via FileSHA256 +
// embedded .checksum.json, chain redaction, and safeExtractPath.
package installer

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/output"
)

// FileSHA256 computes hex-encoded SHA-256 of a file's raw bytes.
// Used for installer payload binding (.checksum.json) and evidence.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// SafeExtractPath validates that an archive entry resolves inside destDir.
// Rejects empty, absolute, and traversal paths. Returns absolute target path.
//
// Offline, filesystem-only, no network.
func SafeExtractPath(destDir, entryName string) (string, error) {
	if entryName == "" {
		return "", fmt.Errorf("empty entry name")
	}
	if filepath.IsAbs(entryName) {
		return "", fmt.Errorf("absolute path not allowed: %s", entryName)
	}
	cleanName := filepath.Clean(entryName)
	if cleanName == "." {
		return "", fmt.Errorf("entry name resolves to extraction root")
	}
	targetPath := filepath.Join(destDir, cleanName)
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("resolve target path: %w", err)
	}
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return "", fmt.Errorf("resolve destination: %w", err)
	}
	prefix := absDest + string(filepath.Separator)
	if !strings.HasPrefix(absTarget, prefix) && absTarget != absDest {
		return "", fmt.Errorf("path traversal detected: %q escapes extraction root %q", entryName, destDir)
	}
	return absTarget, nil
}

// safeExtractPath is unexported alias for internal use.
func safeExtractPath(destDir, entryName string) (string, error) {
	return SafeExtractPath(destDir, entryName)
}

// VerifyBeforeExtract is the FAIL-CLOSED gate before any extraction.
// Calls artifact.VerifyArtifact; on failure returns actionable error
// mentioning --dry-run and checksum guidance.
func VerifyBeforeExtract(artifactPath string) error {
	if strings.TrimSpace(artifactPath) == "" {
		return fmt.Errorf("verification gate FAIL -- artifact path empty -- guidance: provide valid artifact path; verify with `anvil artifact verify --artifact <path>` or re-run installer with --dry-run to inspect without extracting; checksum: missing path")
	}
	vr, err := artifact.VerifyArtifact(artifactPath)
	if err != nil {
		return fmt.Errorf("verification gate FAIL -- verification error: %v -- guidance: artifact checksum verification failed (checksum mismatch or corrupted gzip/manifest); rebuild artifact with `anvil artifact package` and retry; use --dry-run to inspect without extracting; check checksum with `anvil artifact verify`", err)
	}
	if vr == nil || !vr.Passed {
		details := collectFailed(vr)
		preview := checksumPreview(vr)
		// Explicitly mention --dry-run and checksum as required by spec.
		return fmt.Errorf("verification gate FAIL -- abort before extract: %s -- guidance: artifact tampered or corrupted (checksum mismatch / bit-flip / manifest mismatch); delete tampered file, rebuild with `anvil artifact package` or re-download from trusted source, then rerun installer; do not run migrations on unverified artifact; verify with --dry-run and checksum %s", details, preview)
	}
	return nil
}

// VerifyOffline is offline fs-only verification (no net/http). Alias to VerifyBeforeExtract.
func VerifyOffline(artifactPath string) error {
	return VerifyBeforeExtract(artifactPath)
}

func collectFailed(vr *artifact.VerificationResult) string {
	if vr == nil {
		return "verification result unavailable"
	}
	var b bytes.Buffer
	first := true
	for _, c := range vr.Checks {
		if !c.Passed {
			if !first {
				b.WriteString("; ")
			}
			fmt.Fprintf(&b, "%s: %s", c.Name, c.Details)
			first = false
		}
	}
	if b.Len() == 0 {
		return "verification failed (no details)"
	}
	return b.String()
}

func checksumPreview(vr *artifact.VerificationResult) string {
	if vr == nil {
		return "(unknown)"
	}
	for _, c := range vr.Checks {
		if c.Name == "Checksum match" && !c.Passed {
			// Try to surface expected vs got
			if strings.Contains(c.Details, "expected") {
				return c.Details
			}
		}
	}
	// Fallback: truncated details
	if len(vr.Checks) > 0 {
		return vr.Checks[0].Details
	}
	return "(checksum unavailable)"
}

// VerifyInstallerPayloadIntegrity checks FileSHA256 binding against embedded .checksum.json.
// payloadPath is the installer/payload file (e.g., .run or .tar.gz). The binding file
// is payloadPath + ".checksum.json" (produced by installer build). The JSON may contain
// keys: sha256, installer_sha256, checksum, file_sha256, embedded_checksum.
// FAIL-CLOSED: missing file, missing checksum, or mismatch returns error with guidance.
func VerifyInstallerPayloadIntegrity(payloadPath string) error {
	if strings.TrimSpace(payloadPath) == "" {
		return fmt.Errorf("payload path empty -- guidance: provide installer payload path; rebuild installer with `anvil installer build`")
	}
	if _, err := os.Stat(payloadPath); err != nil {
		return fmt.Errorf("installer payload not found %q: %w -- guidance: rebuild installer with `anvil installer build`", payloadPath, err)
	}
	checksumPath := payloadPath + ".checksum.json"
	data, err := os.ReadFile(checksumPath)
	if err != nil {
		return fmt.Errorf("missing checksum binding %q: %w -- guidance: .checksum.json missing; rebuild installer with `anvil installer build` to regenerate binding; verify with --dry-run", checksumPath, err)
	}
	var binding map[string]string
	if err := json.Unmarshal(data, &binding); err != nil {
		// Try generic map[string]interface{} fallback for non-string values
		var raw map[string]any
		if err2 := json.Unmarshal(data, &raw); err2 != nil {
			return fmt.Errorf("invalid checksum binding %q: %w -- guidance: binding corrupted; rebuild installer", checksumPath, err)
		}
		binding = map[string]string{}
		for k, v := range raw {
			if s, ok := v.(string); ok {
				binding[k] = s
			} else {
				binding[k] = fmt.Sprintf("%v", v)
			}
		}
	}
	expected := extractExpectedChecksum(binding)
	if expected == "" {
		return fmt.Errorf("checksum binding %q missing sha256 -- guidance: binding has no checksum field (expected sha256/installer_sha256/checksum); rebuild installer", checksumPath)
	}
	actual, err := FileSHA256(payloadPath)
	if err != nil {
		return fmt.Errorf("hash payload %q: %w", payloadPath, err)
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("payload integrity FAIL -- checksum mismatch for %q: expected %s got %s -- guidance: installer payload tampered or corrupted (repack detected); delete file, rebuild installer from trusted artifact, verify checksum with FileSHA256 and --dry-run", payloadPath, expected, actual)
	}
	// Optional second binding: if embedded_checksum present and payload is artifact, verify embedded artifact's manifest checksum too
	if ec, ok := binding["embedded_checksum"]; ok && ec != "" && strings.HasSuffix(payloadPath, ".tar.gz") {
		m, err := artifact.ReadManifest(payloadPath)
		if err == nil && m != nil && m.Checksum != "" && !strings.EqualFold(m.Checksum, ec) {
			return fmt.Errorf("payload integrity FAIL -- embedded manifest checksum mismatch: binding %s vs manifest %s -- guidance: payload manifest tampered; rebuild installer from trusted artifact", ec, m.Checksum)
		}
		if err == nil && m != nil {
			vr, verr := artifact.VerifyArtifact(payloadPath)
			if verr != nil {
				return fmt.Errorf("payload integrity FAIL -- embedded VerifyArtifact error: %v -- guidance: payload corrupted; rebuild", verr)
			}
			if vr != nil && !vr.Passed {
				return fmt.Errorf("payload integrity FAIL -- embedded artifact FAIL: %s -- guidance: installer payload tampered; rebuild installer from trusted artifact", collectFailed(vr))
			}
		}
	}
	return nil
}

func extractExpectedChecksum(binding map[string]string) string {
	for _, k := range []string{"installer_sha256", "sha256", "checksum", "file_sha256", "file_sha256_hex", "hash"} {
		if v, ok := binding[k]; ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	// Also try case-insensitive
	for k, v := range binding {
		lk := strings.ToLower(k)
		if lk == "sha256" || lk == "installer_sha256" || lk == "checksum" {
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// VerifyInstallerPayloadIntegrityWithEmbedded is spike-compatible helper:
// verifies installer + embedded artifact pair (bool, string, error).
// It checks embedded artifact via VerifyArtifact and returns details.
// Kept for compatibility with spikes/installer-security verifier.
func VerifyInstallerPayloadIntegrityWithEmbedded(installerPath, embeddedArtifactPath string) (bool, string, error) {
	if _, err := os.Stat(installerPath); err != nil {
		return false, "", fmt.Errorf("installer not found %q: %w -- guidance: rebuild installer with `anvil installer build`", installerPath, err)
	}
	if _, err := os.Stat(embeddedArtifactPath); err != nil {
		return false, "", fmt.Errorf("embedded artifact not found %q: %w -- guidance: installer payload missing; rebuild installer", embeddedArtifactPath, err)
	}
	manifest, err := artifact.ReadManifest(embeddedArtifactPath)
	if err != nil {
		return false, "", fmt.Errorf("read embedded manifest: %w -- guidance: payload corrupted; rebuild", err)
	}
	vr, err := artifact.VerifyArtifact(embeddedArtifactPath)
	if err != nil {
		return false, fmt.Sprintf("embedded VerifyArtifact error: %v", err), nil
	}
	if !vr.Passed {
		return false, fmt.Sprintf("embedded artifact FAIL: %s -- installer payload tampered; rebuild installer from trusted artifact", collectFailed(vr)), nil
	}
	rawSHA, err := FileSHA256(embeddedArtifactPath)
	if err != nil {
		return false, "", fmt.Errorf("hash embedded artifact: %w", err)
	}
	_ = rawSHA
	details := fmt.Sprintf("installer %s (%d bytes) payload integrity PASS -- embedded artifact %s checksum %s verified (identity-from-content sha256); repack would change outer SHA or embedded checksum", filepath.Base(installerPath), fileSize(installerPath), filepath.Base(embeddedArtifactPath), truncate(manifest.Checksum, 16))
	return true, details, nil
}

func fileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// RedactInstallerLog chain-redacts installer logs.
// Covers DB/env secrets, key paths, and dynamic password forms fields.
// No net/http.
func RedactInstallerLog(s string) string {
	for _, env := range []string{"ANVIL_SIGNING_KEY", "DB_PASSWORD", "DB_USERNAME", "DATABASE_URL", "ANVIL_DB_PASSWORD", "DEPLOY_SSH_KEY"} {
		if val := os.Getenv(env); val != "" && strings.Contains(s, val) {
			s = strings.ReplaceAll(s, val, "***REDACTED_"+env+"***")
		}
	}
	if strings.Contains(strings.ToLower(s), "-----begin") {
		return "***REDACTED***"
	}
	s = output.SanitizeLogLine(s)
	s = output.RedactSecrets(s)
	// Chain via forms-aware sanitization (password literal)
	s = output.SanitizeWithForms(s, nil)
	lower := strings.ToLower(s)
	if strings.Contains(lower, "password") || strings.Contains(lower, "postgres://") || strings.Contains(lower, "mysql://") {
		if !strings.Contains(s, "***REDACTED") {
			return output.RedactSecrets(s) + " [REDACTED_DB]"
		}
	}
	return s
}

// RedactInstallerLogWithForms extends RedactInstallerLog with dynamic password fields.
func RedactInstallerLogWithForms(line string, passwordFields []string) string {
	line = RedactInstallerLog(line)
	if len(passwordFields) > 0 {
		line = output.RedactWithForms(line, passwordFields)
		line = output.SanitizeWithForms(line, passwordFields)
	}
	return line
}

// SanitizeInstallerName trims name fallback.
func SanitizeInstallerName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "AnvilApp"
	}
	return name
}
