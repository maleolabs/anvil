// Package cmd implements the Anvil CLI commands.
//
// Integration tests for the Flutter executable resolution contract
// (TS-016-02-02). After the repository split (TS-016-02-01, ADR-025),
// the Core resolves the Flutter lifecycle content through the
// anvil-adapter-flutter executable built from the extracted
// anvil-standard-flutter repository. These tests lock the DEFAULT
// resolution contract (ADR-025 §3.4, §12.1/§12.2) so a future governed
// breaking change — renaming the executable, breaking the capabilities
// probe, or changing the lifecycle content — is visible in CI:
//
//   - PATH-based lookup of "anvil-adapter-flutter" through the
//     production seam adapterExecutableLookup (= exec.LookPath,
//     005-adapter-command-contract §10);
//   - the capabilities probe (TS-007-039 §7): a binary is a valid
//     adapter only when it answers the capabilities command, and the
//     extracted standard reports the expected declaration — hybrid
//     deployment model, build targets web/apk/ios, verification checks
//     pubspec_yaml + lib_directory — consistent with the standard's
//     registry metadata (manifest declares contract version 1.0.0 and
//     the Flutter stable 3.0.0–3.32.0 framework scope);
//   - lifecycle content resolution through the standard command
//     contract: pipeline templates (template command), manifest
//     activation/rollback commands (manifest command), and declared
//     verification checks (verify command).
//
// The tests build the REAL standard executable from the extracted
// repository and exercise the production resolution paths end to end —
// the same pattern as internal/adapter/flutter_registration_test.go:
// the standard repository location comes from ANVIL_STANDARD_FLUTTER_DIR
// (falling back to E2E_STANDARD_FLUTTER_DIR, the variable the e2e suite
// uses); the tests skip when it is unset, because the standard
// repository lives outside the Core checkout. They create NO Go import
// of the standard's packages from Core code (ADR-009 §8.1): framework
// values appear as literals.
//
// Reference: TS-016-02-02, ADR-025 §3.4, §12.1/§12.2, TS-007-039,
// 005-adapter-command-contract §10
package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/project"
)

// ── Helpers ──────────────────────────────────────────────────────────

// flutterStandardDir returns the anvil-standard-flutter checkout the
// tests build the standard executable from. It honors
// ANVIL_STANDARD_FLUTTER_DIR (the variable the existing registration
// test uses, the pattern this file follows) and falls back to
// E2E_STANDARD_FLUTTER_DIR (the variable scripts/e2e/e2e_lib.sh uses);
// the test skips when neither is set or the directory is not the
// standard module, because the standard repository lives outside the
// Core checkout (TS-016-02-01).
func flutterStandardDir(t *testing.T) string {
	t.Helper()

	standardDir := os.Getenv("ANVIL_STANDARD_FLUTTER_DIR")
	if standardDir == "" {
		standardDir = os.Getenv("E2E_STANDARD_FLUTTER_DIR")
	}
	if standardDir == "" {
		t.Skip("ANVIL_STANDARD_FLUTTER_DIR not set — the Flutter standard repository is outside the Core checkout (TS-016-02-01)")
	}
	if _, err := os.Stat(filepath.Join(standardDir, "go.mod")); err != nil {
		t.Skipf("standard directory %q does not contain the anvil-standard-flutter module", standardDir)
	}
	return standardDir
}

// The standard executable is compiled ONCE per test process and shared
// by all resolution tests (sync.Once — the same build-once pattern as
// the Laravel resolution tests, cmd/adapter_laravel_resolution_test.go
// in T-002): rebuilding the binary per test would run `go build` six
// times per suite run. The binary directory lives for the whole test
// process (os.MkdirTemp without cleanup — a per-run fixture, never
// t.TempDir, which would be removed when the first test finishes).
var (
	flutterStandardBinaryOnce sync.Once
	flutterStandardBinaryPath string
	flutterStandardBinaryErr  error
)

// buildFlutterStandardBinary compiles the Flutter standard executable
// from the extracted anvil-standard-flutter repository and returns its
// path. The binary name mirrors the resolution contract convention
// `anvil-adapter-<framework>` (005-adapter-command-contract §10) — the
// default naming this ticket locks. The build runs once per test
// process; every caller receives the same shared binary.
func buildFlutterStandardBinary(t *testing.T) string {
	t.Helper()

	standardDir := flutterStandardDir(t)
	flutterStandardBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "anvil-flutter-resolution-*")
		if err != nil {
			flutterStandardBinaryErr = fmt.Errorf("create binary dir: %w", err)
			return
		}
		bin := filepath.Join(dir, "anvil-adapter-flutter")
		cmd := exec.Command("go", "build", "-o", bin, "maleolabs.com/anvil-standard-flutter/cmd/flutter-adapter")
		cmd.Dir = standardDir
		if out, err := cmd.CombinedOutput(); err != nil {
			flutterStandardBinaryErr = fmt.Errorf("build flutter standard executable: %v\n%s", err, out)
			return
		}
		flutterStandardBinaryPath = bin
	})
	if flutterStandardBinaryErr != nil {
		t.Fatalf("build flutter standard executable: %v", flutterStandardBinaryErr)
	}
	return flutterStandardBinaryPath
}

// placeFlutterStandardOnPath builds the standard executable from the
// extracted repository and puts its directory on PATH, making PATH-based
// discovery deterministic: the binary is found exactly where the
// anvil-adapter-flutter contract says. Returns the executable path.
func placeFlutterStandardOnPath(t *testing.T) string {
	t.Helper()
	bin := buildFlutterStandardBinary(t)
	t.Setenv("PATH", filepath.Dir(bin))
	return bin
}

// flutterResolutionProject creates a Flutter-shaped project directory
// with anvil.yaml (project.framework = flutter), pubspec.yaml, and
// lib/main.dart, changes the working directory into it, and returns the
// directory path. The files mirror what a Flutter project created by
// "flutter create" carries, so the standard's verification checks pass
// against packaged artifacts of this project.
func flutterResolutionProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	config := `project:
  name: resolution-test
  version: 1.0.0
  framework: flutter
artifact:
  include: []
`
	if err := os.WriteFile(filepath.Join(dir, project.ConfigFileName), []byte(config), 0644); err != nil {
		t.Fatalf("write anvil.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pubspec.yaml"), []byte("name: resolution_test\n"), 0644); err != nil {
		t.Fatalf("write pubspec.yaml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0755); err != nil {
		t.Fatalf("create lib/: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib", "main.dart"), []byte("void main() {}\n"), 0644); err != nil {
		t.Fatalf("write lib/main.dart: %v", err)
	}

	chdirTo(t, dir)
	return dir
}

// readFlutterStandardManifest reads the standard's source manifest
// (standard/manifest.json in the extracted repository) as a generic
// document. The manifest is registry-metadata format content — the
// resolution tests assert the declared contract version and framework
// scope it ships, without parsing it through the registry client
// (registry publication is TS-016-03-02 scope).
func readFlutterStandardManifest(t *testing.T) map[string]any {
	t.Helper()
	standardDir := flutterStandardDir(t)
	data, err := os.ReadFile(filepath.Join(standardDir, "standard", "manifest.json"))
	if err != nil {
		t.Fatalf("read standard manifest: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse standard manifest: %v", err)
	}
	return doc
}

// packageFlutterResolutionArtifact packages the current project
// (framework flutter) into a fresh output dir and returns the produced
// artifact archive path plus the captured stderr.
func packageFlutterResolutionArtifact(t *testing.T) (string, string) {
	t.Helper()
	outDir := t.TempDir()
	_, _, stderr, err := executeCommand("artifact", "package", "--format", "tar.gz", "--output", outDir)
	if err != nil {
		t.Fatalf("artifact package returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	matches, err := filepath.Glob(filepath.Join(outDir, "artifact-*.tar.gz"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected one artifact archive in %s, got %v (err: %v)", outDir, matches, err)
	}
	return matches[0], stderr
}

// artifactManifestFromArchive extracts manifest.json from an Anvil
// artifact archive (tar.gz) and returns it as a generic document.
func artifactManifestFromArchive(t *testing.T, archivePath string) map[string]any {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open artifact archive: %v", err)
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("open gzip reader: %v", err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		if filepath.Base(hdr.Name) != artifact.ManifestFile {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read manifest from archive: %v", err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("parse artifact manifest: %v", err)
		}
		return doc
	}
	t.Fatalf("artifact archive %s contains no %s", archivePath, artifact.ManifestFile)
	return nil
}

// ── PATH-Based Lookup (ADR-025 §3.4, 005-adapter-command-contract §10) ─

// TestFlutterResolution_PathBasedLookupOfAnvilAdapterFlutter verifies
// the naming/resolution default of the contract: the Core resolves the
// Flutter lifecycle executable by the fixed name "anvil-adapter-flutter"
// on PATH through the production lookup seam (adapterExecutableLookup =
// exec.LookPath). The binary here is the one built from the extracted
// anvil-standard-flutter repository, so the test proves existing
// invocations resolve the post-split executable without any contract
// change. A governed breaking change that renames the executable or
// switches resolution away from PATH fails this test.
//
// Reference: TS-016-02-02, ADR-025 §3.4, §12.1/§12.2,
// 005-adapter-command-contract §10
func TestFlutterResolution_PathBasedLookupOfAnvilAdapterFlutter(t *testing.T) {
	bin := placeFlutterStandardOnPath(t)

	executable, err := adapterExecutableLookup("anvil-adapter-flutter")
	if err != nil {
		t.Fatalf("adapterExecutableLookup(%q) failed: %v", "anvil-adapter-flutter", err)
	}
	if executable != bin {
		t.Errorf("resolved executable = %q, want %q (the binary built from the extracted standard repository)", executable, bin)
	}
}

// ── Capabilities Probe (TS-007-039 §7) ───────────────────────────────

// TestFlutterResolution_CapabilitiesProbeReportsStandardDeclaration
// verifies the capabilities probe of the default contract: the resolved
// anvil-adapter-flutter executable answers the capabilities command with
// the declaration the extracted Flutter standard ships — hybrid
// deployment model, the three build targets (web, apk, ios), and the
// two structural verification checks (pubspec_yaml, lib_directory).
// The declared values are asserted against the standard's registry
// metadata (standard/manifest.json: contract version 1.0.0 and the
// Flutter stable framework scope 3.0.0–3.32.0), so the executable side
// and the metadata side of the default contract stay coherent.
//
// Reference: TS-016-02-02, TS-007-039 §7, ADR-025 §3.4
func TestFlutterResolution_CapabilitiesProbeReportsStandardDeclaration(t *testing.T) {
	bin := placeFlutterStandardOnPath(t)

	result, err := invokeAdapterCapabilities(context.Background(), "flutter", bin)
	if err != nil {
		t.Fatalf("capabilities probe of %q failed: %v", bin, err)
	}
	decl := result.Declaration
	if decl.DeploymentModel != string(contracts.DeploymentModelHybrid) {
		t.Errorf("DeploymentModel = %q, want %q", decl.DeploymentModel, contracts.DeploymentModelHybrid)
	}
	wantPhases := []string{"web", "apk", "ios"}
	if !reflect.DeepEqual(decl.BuildPhases, wantPhases) {
		t.Errorf("BuildPhases = %v, want %v", decl.BuildPhases, wantPhases)
	}
	if len(decl.ActivationPhases) != 0 {
		t.Errorf("ActivationPhases = %v, want none for the hybrid model", decl.ActivationPhases)
	}
	if len(decl.VerificationChecks) != 2 {
		t.Fatalf("VerificationChecks = %v, want the two structural checks", decl.VerificationChecks)
	}
	wantChecks := []string{"pubspec_yaml", "lib_directory"}
	for i, name := range wantChecks {
		if decl.VerificationChecks[i].Name != name {
			t.Errorf("VerificationChecks[%d].Name = %q, want %q", i, decl.VerificationChecks[i].Name, name)
		}
	}

	// Registry-metadata consistency: the extracted standard's manifest
	// declares the contract version line and the framework version scope
	// the capability declaration must be coherent with.
	manifest := readFlutterStandardManifest(t)
	if got := manifest["contractVersion"]; got != "1.0.0" {
		t.Errorf("manifest contractVersion = %v, want %q", got, "1.0.0")
	}
	capability, ok := manifest["capability"].(map[string]any)
	if !ok {
		t.Fatalf("manifest capability = %v, want an object", manifest["capability"])
	}
	versions, ok := capability["frameworkVersion"].([]any)
	if !ok {
		t.Fatalf("manifest capability.frameworkVersion = %v, want a list", capability["frameworkVersion"])
	}
	for _, boundary := range []string{"3.0.0", "3.32.0"} {
		found := false
		for _, v := range versions {
			if v == boundary {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("manifest capability.frameworkVersion should include %q (Flutter stable scope), got %v", boundary, versions)
		}
	}
}

// ── Discovery Surface (TS-007-039) ───────────────────────────────────

// TestFlutterResolution_AdapterListAndInspectSurface verifies the
// user-visible resolution surface: the PATH-discovered
// anvil-adapter-flutter binary appears in "anvil adapter list" with the
// hybrid deployment model, and "anvil adapter inspect flutter" renders
// the declared capabilities — the probe-validated discovery path
// existing Flutter invocations depend on.
//
// Reference: TS-016-02-02, TS-007-039 AC-1, AC-2
func TestFlutterResolution_AdapterListAndInspectSurface(t *testing.T) {
	stubAdapterInstallDirAt(t, t.TempDir()) // CLI dir has nothing installed
	placeFlutterStandardOnPath(t)

	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "flutter") || !strings.Contains(stdout, "hybrid") {
		t.Errorf("adapter list should show flutter with the hybrid model, got:\n%s", stdout)
	}

	_, stdout, stderr, err = executeCommand("adapter", "inspect", "flutter")
	if err != nil {
		t.Fatalf("adapter inspect flutter returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	for _, want := range []string{"Adapter: flutter", "hybrid", "web", "apk", "ios", "pubspec_yaml", "lib_directory"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("adapter inspect flutter should contain %q, got:\n%s", want, stdout)
		}
	}
}

// ── Lifecycle Content: Pipeline Templates (template command) ─────────

// TestFlutterResolution_AdapterUseMaterializesLifecycleContent verifies
// lifecycle content resolution through the standard command contract:
// "anvil adapter use flutter" resolves the PATH-discovered executable,
// records project.framework, and materializes the build and CI pipeline
// templates from the extracted repository's template command
// (ADR-020 §1) — the pipeline content (flutter-web/apk/ios targets and
// their platform metadata) comes from anvil-standard-flutter, not from
// Core (TS-015-01-02, ADR-026). A split that breaks template resolution
// leaves the pipeline files missing and fails this test.
//
// Reference: TS-016-02-02, TS-007-033, ADR-020 §1, ADR-026
func TestFlutterResolution_AdapterUseMaterializesLifecycleContent(t *testing.T) {
	dir := setupUseProject(t)
	stubAdapterInstallDirAt(t, t.TempDir())
	placeFlutterStandardOnPath(t)

	_, stdout, stderr, err := executeCommand("adapter", "use", "flutter")
	if err != nil {
		t.Fatalf("adapter use flutter returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if got := projectFramework(t, dir); got != "flutter" {
		t.Errorf("project.framework = %q, want %q", got, "flutter")
	}
	if !strings.Contains(stdout, "Adapter flutter is now active") {
		t.Errorf("stdout should confirm activation, got:\n%s", stdout)
	}

	buildYAML, err := os.ReadFile(filepath.Join(dir, ".anvil", "pipelines", "build.yaml"))
	if err != nil {
		t.Fatalf("read generated build pipeline: %v", err)
	}
	for _, want := range []string{"flutter-web", "flutter-apk", "flutter-ios", "platforms:"} {
		if !strings.Contains(string(buildYAML), want) {
			t.Errorf("generated build.yaml should contain %q (template from the extracted standard), got:\n%s", want, buildYAML)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".anvil", "pipelines", "ci.yaml")); err != nil {
		t.Errorf("ci.yaml should be generated alongside build.yaml, got: %v", err)
	}
}

// ── Lifecycle Content: Manifest Commands (manifest command) ──────────

// TestFlutterResolution_ArtifactPackageResolvesManifestCommands
// verifies lifecycle content resolution at packaging time: with
// project.framework = flutter, "anvil artifact package" resolves
// anvil-adapter-flutter on PATH and dispatches the manifest command —
// the packaging completes without the missing-adapter warning and
// without a manifest-command failure, and the artifact manifest records
// no activation commands (the hybrid model declares none, so the empty
// slices are omitted from the manifest, 005-adapter-command-contract
// §10.10). The warning-free packaging is the positive resolution
// evidence: both the LookPath step and the manifest dispatch succeeded
// against the extracted executable.
//
// Reference: TS-016-02-02, TS-P7-15, TS-P7-16, ADR-017
func TestFlutterResolution_ArtifactPackageResolvesManifestCommands(t *testing.T) {
	flutterResolutionProject(t)
	stubAdapterInstallDirAt(t, t.TempDir())
	placeFlutterStandardOnPath(t)

	artifactPath, stderr := packageFlutterResolutionArtifact(t)
	if strings.Contains(stderr, "not found; packaging without manifest activation/rollback commands") {
		t.Errorf("packaging should not report a missing adapter executable, got: %s", stderr)
	}
	if strings.Contains(stderr, "could not fetch manifest commands") {
		t.Errorf("packaging should not report a manifest command failure, got: %s", stderr)
	}

	manifest := artifactManifestFromArchive(t, artifactPath)
	if _, ok := manifest["activation_commands"]; ok {
		t.Errorf("artifact manifest should omit activation_commands for the hybrid model, got: %v", manifest)
	}
	if _, ok := manifest["rollback_commands"]; ok {
		t.Errorf("artifact manifest should omit rollback_commands for the hybrid model, got: %v", manifest)
	}
}

// ── Lifecycle Content: Verification Checks (verify command) ──────────

// TestFlutterResolution_ArtifactVerifyRunsDeclaredChecks verifies the
// verification half of lifecycle content resolution: "anvil artifact
// verify" resolves anvil-adapter-flutter, probes its capabilities, and
// runs the declared checks (pubspec_yaml, lib_directory) against the
// packaged artifact — both PASS for a Flutter-shaped project
// (ST-007-004). A split that breaks executable resolution or the
// declared-check contract fails this test.
//
// Reference: TS-016-02-02, ST-007-004, TS-P7-11, TS-P7-25
func TestFlutterResolution_ArtifactVerifyRunsDeclaredChecks(t *testing.T) {
	flutterResolutionProject(t)
	stubAdapterInstallDirAt(t, t.TempDir())
	placeFlutterStandardOnPath(t)

	artifactPath, _ := packageFlutterResolutionArtifact(t)

	_, stdout, stderr, err := executeCommand("artifact", "verify", artifactPath)
	if err != nil {
		t.Fatalf("artifact verify returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "Artifact verification: PASSED") {
		t.Errorf("generic verification should pass, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Framework verification:") {
		t.Errorf("stdout should open the framework verification section, got:\n%s", stdout)
	}
	for _, want := range []string{"pubspec_yaml", "lib_directory"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("framework verification should run the %q check, got:\n%s", want, stdout)
		}
	}
}
