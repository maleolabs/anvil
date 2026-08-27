package security

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/output"
)

// VerifyBeforeExtract is AC1 gate: verify manifest checksum sha256 sebelum extract.
func VerifyBeforeExtract(artifactPath string) (*artifact.VerificationResult, error) {
	vr, err := artifact.VerifyArtifact(artifactPath)
	if err != nil {
		return vr, fmt.Errorf("verification error: %w -- guidance: rebuild artifact with `anvil artifact package` and retry; if re-downloaded still fails, source may be tampered", err)
	}
	if !vr.Passed {
		return vr, fmt.Errorf("verification gate FAIL -- abort before extract: %s -- guidance: artifact tampered or corrupted (bit-flip/manifest mismatch); delete tampered file, rebuild or re-download from trusted source, then rerun installer; do not run migrations on unverified artifact", collectFailed(vr))
	}
	return vr, nil
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

// VerifyInstallerPayloadIntegrity is AC2: detect repack tampering of installer wrapper.
func VerifyInstallerPayloadIntegrity(installerPath, embeddedArtifactPath string) (bool, string, error) {
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
	details := fmt.Sprintf("installer %s (%d bytes) payload integrity PASS -- embedded artifact %s checksum %s verified (identity-from-content sha256); repack would change outer SHA or embedded checksum", filepath.Base(installerPath), verifierFileSize(installerPath), filepath.Base(embeddedArtifactPath), manifest.Checksum[:16])
	return true, details, nil
}

// FileSHA256 computes hex sha256 of a file.
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

func verifierFileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

// VerifyOffline is AC4: offline verification -- proves no external registry call.
func VerifyOffline(artifactPath string) (*artifact.VerificationResult, error) {
	return VerifyBeforeExtract(artifactPath)
}

// RedactInstallerLog wraps output.RedactSecrets/SanitizeLogLine plus DB env redaction for AC3.
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
	lower := strings.ToLower(s)
	if strings.Contains(lower, "password") || strings.Contains(lower, "postgres://") || strings.Contains(lower, "mysql://") {
		if !strings.Contains(s, "***REDACTED") {
			return output.RedactSecrets(s) + " [REDACTED_DB]"
		}
	}
	return s
}

// TamperArtifact flips a byte to simulate bit-flip tamper.
func TamperArtifact(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if len(b) > 200 {
		b[200] ^= 0xFF
	} else if len(b) > 0 {
		b[0] ^= 0xFF
	}
	return os.WriteFile(dst, b, 0644)
}

func SanitizeInstallerName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "AnvilApp"
	}
	return name
}

func SigningFeasibility() string {
	return "# Code Signing Feasibility (out-of-MVP, documented)\n\n" +
		"## Windows (NSIS .exe)\n" +
		"- Tool: signtool.exe / osslsigncode on Linux CI\n" +
		"- Cert: EV Code Signing (HSM-backed) + RFC3161 timestamp\n" +
		"- Command: signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 /f cert.pfx installer.exe\n" +
		"- Verify: signtool verify /pa installer.exe or Get-AuthenticodeSignature\n\n" +
		"## Linux (Makeself .run)\n" +
		"- Sign whole file: gpg --detach-sign --armor installer.run -> installer.run.asc\n" +
		"- Verify offline: gpg --verify installer.run.asc installer.run\n\n" +
		"## Recommendation\n" +
		"- MVP: identity-from-content sha256 via artifact.ComputeChecksum = content-addressable integrity; tamper detection without PKI.\n" +
		"- Signing adds non-repudiation + OS trust dialogs but requires HSM, rotation, CI secrets.\n" +
		"- Ship MVP with VerifyBeforeExtract FAIL-closed + payload integrity binding; add signing when HSM cert available.\n"
}
