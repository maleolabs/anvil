package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/output"
)

// TestInstaller_Registered verifies installer and installer build are registered.
func TestInstaller_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "installer" {
			found = true
			// check subcommand build
			hasBuild := false
			for _, sub := range c.Commands() {
				if sub.Name() == "build" {
					hasBuild = true
					break
				}
			}
			if !hasBuild {
				t.Error("installer build subcommand not found")
			}
			break
		}
	}
	if !found {
		t.Error("installer command not found in root")
	}
}

// TestInstallerBuild_HelpDocumentsArtifactReuseAndPluggable verifies --help covers --artifact reuse + pluggable setup + exit codes.
func TestInstallerBuild_HelpDocumentsArtifactReuseAndPluggable(t *testing.T) {
	_, stdout, _, err := executeCommand("installer", "build", "--help")
	if err != nil {
		t.Fatalf("installer build --help returned error: %v", err)
	}
	// must contain --artifact reuse documentation
	for _, needle := range []string{"--artifact", "artifact", "--target", "--dry-run", "--json", "windows", "linux", "installer.forms", "superAdmin", "forms"} {
		if !strings.Contains(strings.ToLower(stdout), strings.ToLower(needle)) {
			t.Errorf("help missing %q in stdout:\n%s", needle, stdout)
		}
	}
	// Must document pluggable setup (generic example) and envelope/exit codes
	if !strings.Contains(stdout, "Exit codes") {
		t.Errorf("help missing Exit codes in stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Pluggable") && !strings.Contains(strings.ToLower(stdout), "pluggable") {
		// fallback check for generic forms mention
		if !strings.Contains(stdout, "installer.forms") {
			t.Errorf("help missing pluggable setup / installer.forms documentation:\n%s", stdout)
		}
	}
	if !strings.Contains(stdout, "Examples:") {
		t.Errorf("help missing Examples section:\n%s", stdout)
	}
	for _, ex := range []string{"anvil installer build --target linux", "anvil installer build --target windows"} {
		if !strings.Contains(stdout, ex) {
			t.Errorf("help missing example %q:\n%s", ex, stdout)
		}
	}
}

// TestInstallerBuild_DryRun_NoBundle verifies --dry-run verifies only and does not create bundle file.
func TestInstallerBuild_DryRun_NoBundle(t *testing.T) {
	dir := t.TempDir()
	_, _, _, err := executeCommand("init", "installer-dryrun", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Human dry-run
	_, stdout, stderr, err := executeCommand("installer", "build", "--target", "linux", "--dry-run")
	if err != nil {
		t.Fatalf("installer build dry-run failed: %v\nstdout:%s\nstderr:%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "dry-run") && !strings.Contains(strings.ToLower(stdout), "dry-run") {
		t.Errorf("human missing dry-run marker in %q", stdout)
	}
	if !strings.Contains(stdout, "Verify") {
		t.Errorf("human missing Verify in %q", stdout)
	}
	if !strings.Contains(stdout, "PASS") {
		t.Errorf("human missing PASS in %q", stdout)
	}
	if !strings.Contains(stdout, "artifact_id") {
		t.Errorf("human missing artifact_id in %q", stdout)
	}
	if !strings.Contains(stdout, "artifact_reused") {
		t.Errorf("human missing artifact_reused in %q", stdout)
	}
	// Ensure no bundle file was created at .anvil/installers
	installersDir := filepath.Join(dir, ".anvil", "installers")
	if fi, err := os.Stat(installersDir); err == nil && fi.IsDir() {
		entries, _ := os.ReadDir(installersDir)
		if len(entries) != 0 {
			t.Errorf("dry-run should not produce bundle, but %d files in %s", len(entries), installersDir)
		}
	}

	// JSON dry-run should have envelope v1 with required fields and no bundle_path or empty
	_, jsonOut, _, err := executeCommand("installer", "build", "--target", "linux", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("installer build dry-run json failed: %v\nstdout:%s", err, jsonOut)
	}
	var env output.OutputEnvelope
	if err := json.Unmarshal([]byte(jsonOut), &env); err != nil {
		t.Fatalf("parse envelope: %v\nstdout:%s", err, jsonOut)
	}
	if env.Version != "1" || env.Status != "success" {
		t.Errorf("envelope version=%q status=%q want 1/success", env.Version, env.Status)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonOut), &raw); err != nil {
		t.Fatalf("parse raw envelope: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	for _, k := range []string{"target", "artifact_id", "verify", "artifact_reused"} {
		if _, ok := data[k]; !ok {
			t.Errorf("json data missing %q", k)
		}
	}
	if data["target"] != "linux" {
		t.Errorf("target=%v want linux", data["target"])
	}
	// dry_run should be true
	if data["dry_run"] != true {
		t.Errorf("dry_run=%v want true", data["dry_run"])
	}
	// verify.passed must be true
	if v, ok := data["verify"].(map[string]interface{}); ok {
		if v["passed"] != true {
			t.Errorf("verify.passed=%v want true", v["passed"])
		}
	}
	// bundle_path should be absent or empty for dry-run
	if bp, ok := data["bundle_path"]; ok && bp != nil {
		if s, ok := bp.(string); ok && s != "" {
			// If bundle_path is present, ensure file does NOT exist (dry-run no bundle)
			if _, err := os.Stat(s); err == nil {
				t.Errorf("dry-run bundle_path %q should not exist as file", s)
			}
		}
	}
}

// TestInstallerBuild_ArtifactReuse verifies --artifact path reuses artifact (Verify then Bundle, skip Package).
func TestInstallerBuild_ArtifactReuse(t *testing.T) {
	dir := t.TempDir()
	_, _, _, err := executeCommand("init", "installer-reuse", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Create a reusable artifact via internal artifact package (simulates `anvil artifact package` / release)
	tmpOut := t.TempDir()
	// Need project identity via RequireProject-like loading; use direct artifact.Package with discovered root
	pkgRes, err := artifact.Package(artifact.PackageOptions{
		SourceDir: dir,
		OutputDir: tmpOut,
		Formats:   []string{"tar.gz"},
		Include:   nil,
		Exclude:   nil,
		Version:   "1.2.3",
		Source:    "installer-reuse",
		ProjectID: "installer-reuse",
	})
	if err != nil {
		t.Fatalf("package for reuse: %v", err)
	}
	artifactPath := pkgRes.ArtifactPath
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("reuse artifact not found: %v", err)
	}
	// Verify it passes before reuse
	vr, err := artifact.VerifyArtifact(artifactPath)
	if err != nil || !vr.Passed {
		t.Fatalf("reuse artifact verify should PASS: %v passed=%v", err, vr.Passed)
	}

	// Now build installer with --artifact reuse (human)
	_, stdout, stderr, err := executeCommand("installer", "build", "--target", "linux", "--artifact", artifactPath)
	if err != nil {
		t.Fatalf("installer build --artifact failed: %v\nstdout:%s\nstderr:%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "artifact_reused") {
		t.Errorf("human missing artifact_reused in %q", stdout)
	}
	if !strings.Contains(stdout, "true") {
		t.Errorf("human should show artifact_reused true for reuse, got %q", stdout)
	}
	if !strings.Contains(stdout, pkgRes.Manifest.ArtifactID) {
		t.Errorf("human missing reused artifact_id %q in %q", pkgRes.Manifest.ArtifactID, stdout)
	}

	// JSON path with reuse
	_, jsonOut, _, err := executeCommand("installer", "build", "--target", "windows", "--artifact", artifactPath, "--json")
	if err != nil {
		t.Fatalf("installer build --artifact json failed: %v\nstdout:%s", err, jsonOut)
	}
	var env output.OutputEnvelope
	if err := json.Unmarshal([]byte(jsonOut), &env); err != nil {
		t.Fatalf("parse envelope: %v\nstdout:%s", err, jsonOut)
	}
	if env.Version != "1" || env.Status != "success" {
		t.Errorf("envelope version=%q status=%q want 1/success", env.Version, env.Status)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonOut), &raw); err != nil {
		t.Fatalf("parse raw: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data["artifact_reused"] != true {
		t.Errorf("artifact_reused=%v want true", data["artifact_reused"])
	}
	if data["target"] != "windows" {
		t.Errorf("target=%v want windows", data["target"])
	}
	if got, _ := data["artifact_id"].(string); got != pkgRes.Manifest.ArtifactID {
		t.Errorf("artifact_id %q != reused %q", got, pkgRes.Manifest.ArtifactID)
	}
	// verify.passed must be true
	if v, ok := data["verify"].(map[string]interface{}); ok {
		if v["passed"] != true {
			t.Errorf("verify.passed=%v want true", v["passed"])
		}
	}
	// bundle_path should exist and be nsi or run or payload
	if bp, ok := data["bundle_path"].(string); ok {
		if bp == "" {
			t.Errorf("bundle_path empty for non-dry-run reuse")
		} else {
			if _, err := os.Stat(bp); err != nil {
				t.Errorf("bundle_path %q should exist: %v", bp, err)
			}
		}
	}
}

// TestInstallerBuild_HumanJSONConsistency validates human+JSON logical consistency (target/verify/artifact_id).
func TestInstallerBuild_HumanJSONConsistency(t *testing.T) {
	dir := t.TempDir()
	_, _, _, err := executeCommand("init", "installer-consistent", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, human, _, err := executeCommand("installer", "build", "--target", "linux", "--dry-run")
	if err != nil {
		t.Fatalf("human run failed: %v", err)
	}
	_, jsonOut, _, err := executeCommand("installer", "build", "--target", "linux", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("json run failed: %v", err)
	}
	// Parse artifact_id from JSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonOut), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	artifactID, _ := data["artifact_id"].(string)
	target, _ := data["target"].(string)
	if artifactID == "" {
		t.Fatal("json missing artifact_id")
	}
	// Human must contain same target and artifact_id
	if !strings.Contains(human, target) {
		t.Errorf("human missing target %q — human+JSON not consistent", target)
	}
	if !strings.Contains(human, artifactID) {
		t.Errorf("human missing artifact_id %q — human+JSON not consistent (human len %d)", artifactID[:min(16, len(artifactID))], len(human))
	}
	// Use exported helper for full gate
	if err := ValidateInstallerHumanJSONConsistency(human, jsonOut, target, artifactID); err != nil {
		t.Errorf("ValidateInstallerHumanJSONConsistency failed: %v\nhuman:%s\njson:%s", err, human, jsonOut)
	}
	// Also with --artifact reuse consistency
	tmpOut := t.TempDir()
	pkgRes, err := artifact.Package(artifact.PackageOptions{
		SourceDir: dir,
		OutputDir: tmpOut,
		Formats:   []string{"tar.gz"},
		Version:   "1.2.3",
		Source:    "installer-consistent",
		ProjectID: "installer-consistent",
	})
	if err != nil {
		t.Fatalf("package for reuse consistency: %v", err)
	}
	_, human2, _, err := executeCommand("installer", "build", "--target", "windows", "--artifact", pkgRes.ArtifactPath, "--dry-run")
	if err != nil {
		t.Fatalf("human reuse dry-run failed: %v", err)
	}
	_, jsonOut2, _, err := executeCommand("installer", "build", "--target", "windows", "--artifact", pkgRes.ArtifactPath, "--dry-run", "--json")
	if err != nil {
		t.Fatalf("json reuse dry-run failed: %v", err)
	}
	var raw2 map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonOut2), &raw2); err != nil {
		t.Fatalf("unmarshal raw2: %v", err)
	}
	var data2 map[string]interface{}
	if err := json.Unmarshal(raw2["data"], &data2); err != nil {
		t.Fatalf("unmarshal data2: %v", err)
	}
	artifactID2, _ := data2["artifact_id"].(string)
	target2, _ := data2["target"].(string)
	if err := ValidateInstallerHumanJSONConsistency(human2, jsonOut2, target2, artifactID2); err != nil {
		t.Errorf("reuse ValidateInstallerHumanJSONConsistency failed: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestInstallerBuild_InvalidTarget_ConfigExit2 verifies invalid target maps to exit 2 config error.
func TestInstallerBuild_InvalidTarget_ConfigExit2(t *testing.T) {
	dir := t.TempDir()
	_, _, _, err := executeCommand("init", "installer-bad-target", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, _, stderr, err := executeCommand("installer", "build", "--target", "macos")
	if err == nil {
		t.Fatal("expected error for invalid target, got nil")
	}
	requireExitCode(t, err, output.ExitCodeConfig)
	if !strings.Contains(strings.ToLower(stderr), "invalid target") {
		t.Errorf("stderr missing invalid target guidance, got: %s", stderr)
	}
	// JSON error envelope for same case
	_, stdoutJSON, _, err := executeCommand("installer", "build", "--target", "invalid", "--json")
	if err == nil {
		t.Fatal("expected json error for invalid target, got nil")
	}
	requireExitCode(t, err, output.ExitCodeConfig)
	var env output.OutputEnvelope
	if err := json.Unmarshal([]byte(stdoutJSON), &env); err != nil {
		t.Fatalf("parse json error envelope: %v\nstdout:%s", err, stdoutJSON)
	}
	if env.Status != "error" || env.Version != "1" {
		t.Errorf("json error envelope status=%q version=%q want error/1", env.Status, env.Version)
	}
}

// TestInstallerBuild_MissingTarget_ConfigExit2 verifies missing --target maps to exit 2.
func TestInstallerBuild_MissingTarget_ConfigExit2(t *testing.T) {
	dir := t.TempDir()
	_, _, _, err := executeCommand("init", "installer-missing-target", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, _, stderr, err := executeCommand("installer", "build", "--dry-run")
	if err == nil {
		t.Fatal("expected error for missing --target, got nil")
	}
	requireExitCode(t, err, output.ExitCodeConfig)
	if !strings.Contains(stderr, "missing required flag --target") {
		t.Errorf("stderr missing missing --target guidance, got: %s", stderr)
	}
}

// TestInstallerBuild_Tamper_Exit1 verifies tampered artifact via --artifact reports exit 1.
func TestInstallerBuild_Tamper_Exit1(t *testing.T) {
	dir := t.TempDir()
	_, _, _, err := executeCommand("init", "installer-tamper", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	tmpOut := t.TempDir()
	pkgRes, err := artifact.Package(artifact.PackageOptions{
		SourceDir: dir,
		OutputDir: tmpOut,
		Formats:   []string{"tar.gz"},
		Version:   "1.0.0",
		Source:    "installer-tamper",
		ProjectID: "installer-tamper",
	})
	if err != nil {
		t.Fatalf("package: %v", err)
	}
	// Tamper the artifact by corrupting a byte in the middle (breaks checksum / gzip)
	raw, err := os.ReadFile(pkgRes.ArtifactPath)
	if err != nil {
		t.Fatalf("read for tamper: %v", err)
	}
	// Flip a byte near the middle; ensure we don't truncate gzip header (keep first 10 bytes intact)
	if len(raw) > 100 {
		raw[50] ^= 0xFF
		raw[60] ^= 0xAA
	} else if len(raw) > 20 {
		raw[15] ^= 0xFF
	}
	if err := os.WriteFile(pkgRes.ArtifactPath, raw, 0644); err != nil {
		t.Fatalf("write tampered: %v", err)
	}

	_, _, stderr, err := executeCommand("installer", "build", "--target", "linux", "--artifact", pkgRes.ArtifactPath, "--dry-run")
	if err == nil {
		t.Fatal("expected tamper error for --artifact dry-run, got nil")
	}
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(strings.ToLower(stderr), "verify") && !strings.Contains(strings.ToLower(stderr), "tamper") {
		// stderr should mention verify FAIL or tampered
		t.Logf("stderr for tamper (human) = %s", stderr)
	}
	// JSON tamper should also be exit 1 and error envelope
	_, jsonOut, _, err := executeCommand("installer", "build", "--target", "linux", "--artifact", pkgRes.ArtifactPath, "--dry-run", "--json")
	if err == nil {
		t.Fatal("expected json tamper error, got nil")
	}
	requireExitCode(t, err, output.ExitCodeGeneral)
	var env output.OutputEnvelope
	if err := json.Unmarshal([]byte(jsonOut), &env); err != nil {
		t.Fatalf("parse json tamper envelope: %v\nstdout:%s", err, jsonOut)
	}
	if env.Status != "error" {
		t.Errorf("tamper json status=%q want error", env.Status)
	}
}

// TestInstallerBuild_JSONEnvelopeContract verifies JSON envelope v1 success contract.
func TestInstallerBuild_JSONEnvelopeContract(t *testing.T) {
	dir := t.TempDir()
	_, _, _, err := executeCommand("init", "installer-json-contract", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, jsonOut, _, err := executeCommand("installer", "build", "--target", "linux", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("json contract failed: %v\nstdout:%s", err, jsonOut)
	}
	var env output.OutputEnvelope
	if err := json.Unmarshal([]byte(jsonOut), &env); err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	if env.Version != "1" || env.Status != "success" {
		t.Errorf("envelope version=%q status=%q want 1/success", env.Version, env.Status)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonOut), &raw); err != nil {
		t.Fatalf("parse raw: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	for _, k := range []string{"target", "artifact_id", "verify", "artifact_reused"} {
		if _, ok := data[k]; !ok {
			t.Errorf("json data missing %q", k)
		}
	}
	if v, ok := data["verify"].(map[string]interface{}); ok {
		if _, ok := v["passed"]; !ok {
			t.Errorf("verify missing passed")
		}
		if _, ok := v["checks"]; !ok {
			t.Errorf("verify missing checks")
		}
	}
}

// TestInstaller_FlagNaming ensures flags follow kebab-case and include required ones.
func TestInstaller_FlagNaming(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"installer", "build"})
	if err != nil {
		t.Fatalf("find installer build: %v", err)
	}
	for _, name := range []string{"target", "artifact", "dry-run", "json"} {
		if f := cmd.Flags().Lookup(name); f == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
}
