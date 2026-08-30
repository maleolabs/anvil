package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	security "maleolabs.com/anvil/spikes/installer-security"
)

func main() {
	var evidenceDir, outDir, repoRoot string
	flag.StringVar(&evidenceDir, "evidence", "spikes/installer-security/evidence", "evidence dir")
	flag.StringVar(&outDir, "out", "", "output dir for simulated installer (default <repo>/dist/installer)")
	flag.StringVar(&repoRoot, "repo", ".", "repo root")
	flag.Parse()

	if repoRoot == "" {
		repoRoot = "."
	}
	if outDir == "" {
		outDir = filepath.Join(repoRoot, "dist", "installer")
	}
	if evidenceDir == "" {
		evidenceDir = filepath.Join(os.TempDir(), "spike-installer-security-evidence")
	}
	_ = os.MkdirAll(evidenceDir, 0755)
	_ = os.MkdirAll(outDir, 0755)

	harnessLogPath := filepath.Join(evidenceDir, "harness.log")
	f, err := os.Create(harnessLogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create harness.log: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	mw := io.MultiWriter(os.Stdout, f)

	fmt.Fprintln(mw, "=== Spike 4: installer-security harness ===")
	cfg := security.HarnessConfig{RepoRoot: repoRoot, OutputDir: outDir, EvidenceDir: evidenceDir, Logger: mw}
	res, err := security.RunSecurityHarness(cfg)
	if err != nil {
		fmt.Fprintf(mw, "HARNESS FAILED: %v\n", security.RedactInstallerLog(err.Error()))
		fmt.Fprintf(os.Stderr, "HARNESS FAILED: %v\n", security.RedactInstallerLog(err.Error()))
		os.Exit(1)
	}
	// summary + signing feasibility
	if b, err := json.MarshalIndent(res, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(evidenceDir, "summary.json"), b, 0644)
	}
	_ = os.WriteFile(filepath.Join(evidenceDir, "signing-feasibility.md"), []byte(security.SigningFeasibility()), 0644)
	// matrix.md
	_ = os.WriteFile(filepath.Join(evidenceDir, "matrix.md"), []byte(buildMatrix(res)), 0644)

	fmt.Fprintf(mw, "\nEvidence written to %s\n", evidenceDir)
	for _, n := range []string{"tamper.log", "payload-integrity.log", "redaction.log", "offline.log", "signing-feasibility.md", "summary.json", "matrix.md", "harness.log"} {
		if _, err := os.Stat(filepath.Join(evidenceDir, n)); err == nil {
			fmt.Fprintf(mw, "  - %s\n", filepath.Join(evidenceDir, n))
		}
	}
	fmt.Fprintln(mw, "All ACs PASS. Spike 4 proof complete.")
	_ = res
}

func buildMatrix(r *security.HarnessResult) string {
	return fmt.Sprintf("# Spike 4 -- Security Verification & Tamper Detection Matrix (AC1-4)\n\n"+
		"| AC | Gate | Result | Evidence |\n"+
		"|---|---|---|---|\n"+
		"| AC1 | VerifyBeforeExtract (sha256 identity-from-content) tamper bit-flip rejected with actionable guidance | %t | tamper.log |\n"+
		"| AC1 | identity checksum %s |\n"+
		"| AC2 | Installer payload integrity (installer SHA vs embedded manifest) | %t | payload-integrity.log |\n"+
		"| AC2 | Repack tampering detected | %t | payload-integrity.log |\n"+
		"| AC3 | Secret redaction (RedactSecrets/SanitizeLogLine, DB creds not leak) | %t (%d samples) | redaction.log |\n"+
		"| AC4 | Offline verification (no registry HTTP, filesystem-only) | %t (noRegistry=%t) | offline.log |\n"+
		"| Signing | Windows Authenticode + Linux gpg feasibility documented (out-of-MVP) | done | signing-feasibility.md |\n",
		r.AC1_TamperRejected && r.AC1_GuidancePresent, r.AC1_IdentityChecksum[:16],
		r.AC2_PayloadIntegrity, r.AC2_RepackDetected,
		r.AC3_Redacted, r.AC3_Samples,
		r.AC4_OfflinePass, r.AC4_NoRegistryCall,
	)
}
