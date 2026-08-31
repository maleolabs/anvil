// Package cmd implements the Anvil CLI commands.
//
// Tests for the Laravel standard executable resolution contract
// (TS-016-01-02, ADR-025 §3.4, §12.1/§12.2): the DEFAULT resolution
// behavior — PATH-based lookup of `anvil-adapter-laravel` via
// exec.LookPath (005-adapter-command-contract §10.1), the capabilities
// probe (TS-007-039 §7), and lifecycle content resolution (template and
// manifest commands) — locked against the REAL executable built from the
// extracted anvil-standard-laravel repository (TS-016-01-01, ADR-025
// §6.2).
//
// These tests exist so that a future governed breaking change to the
// resolution contract (ADR-025 §12.1: a naming or lookup change is a
// governed breaking event) is visible in CI: they assert the
// anvil-adapter-* naming, the exec.LookPath lookup, the probe, and the
// lifecycle content an existing project resolves — all against the
// extracted repository, never against Core-built content.
//
// The tests build the REAL standard executable from the extracted
// repository and exercise the production resolution paths end to end —
// the same pattern as internal/adapter/laravel_registration_test.go:
// the standard repository location comes from ANVIL_STANDARD_LARAVEL_DIR
// (falling back to E2E_STANDARD_LARAVEL_DIR, the variable the e2e suite
// uses — mirroring the Flutter track of TS-016-02-02); the tests skip
// when it is unset, because the standard repository lives outside the
// Core checkout. They create NO Go import of the standard's packages
// (ADR-009 §8.1: Core never depends on adapters/standards); the
// executable is compiled with `go build` and invoked as a subprocess
// through the real Process Runner.
//
// Reference: TS-016-01-02, ADR-025 §3.4/§6.2/§12.1, ADR-009 §8.1,
// 005-adapter-command-contract §10
package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/adapter"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/registry"
)

// ── Helpers ──────────────────────────────────────────────────────────

// laravelStandardDir returns the anvil-standard-laravel checkout the
// tests build the standard executable from. It honors
// ANVIL_STANDARD_LARAVEL_DIR (the variable the existing registration
// test uses, the pattern this file follows) and falls back to
// E2E_STANDARD_LARAVEL_DIR (the variable scripts/e2e/e2e_lib.sh uses);
// the test skips when neither is set or the directory is not the
// standard module, because the standard repository lives outside the
// Core checkout (TS-016-01-01).
func laravelStandardDir(t *testing.T) string {
	t.Helper()

	standardDir := os.Getenv("ANVIL_STANDARD_LARAVEL_DIR")
	if standardDir == "" {
		standardDir = os.Getenv("E2E_STANDARD_LARAVEL_DIR")
	}
	if standardDir == "" {
		t.Skip("ANVIL_STANDARD_LARAVEL_DIR not set — the Laravel standard repository is outside the Core checkout (TS-016-01-01)")
	}
	if _, err := os.Stat(filepath.Join(standardDir, "go.mod")); err != nil {
		t.Skipf("standard directory %q does not contain the anvil-standard-laravel module", standardDir)
	}
	return standardDir
}

// buildLaravelStandardExecutable compiles the Laravel standard executable
// (anvil-adapter-laravel) from the extracted anvil-standard-laravel
// repository into a temp dir and returns its path. The binary name
// mirrors the resolution contract convention `anvil-adapter-<framework>`
// (005-adapter-command-contract §10) — the default naming this ticket
// locks. The build output lives in t.TempDir(), so it is cleaned up with
// the test.
func buildLaravelStandardExecutable(t *testing.T) string {
	t.Helper()

	standardDir := laravelStandardDir(t)
	bin := filepath.Join(t.TempDir(), "anvil-adapter-laravel")
	cmd := exec.Command("go", "build", "-o", bin, "maleolabs.com/anvil-standard-laravel/cmd/laravel-adapter")
	cmd.Dir = standardDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build laravel standard executable: %v\n%s", err, out)
	}
	return bin
}

// laravelStandardOnPath builds the standard executable from the
// extracted repository and puts its directory on PATH, making the
// executable resolution contract deterministic: the binary is the only
// resolvable anvil-adapter-laravel. Returns the directory containing
// the binary.
func laravelStandardOnPath(t *testing.T) string {
	t.Helper()
	bin := buildLaravelStandardExecutable(t)
	dir := filepath.Dir(bin)
	stubAdapterInstallDirAt(t, t.TempDir()) // CLI dir has nothing installed
	t.Setenv("PATH", dir)
	return dir
}

// ── PATH-Based Lookup (005-adapter-command-contract §10.1) ───────────

// TestAdapterExecutableLookup_LaravelStandardOnPath verifies the default
// executable resolution contract: the Core resolves the Laravel standard
// through exec.LookPath("anvil-adapter-<framework>") — the production
// adapterExecutableLookup value — and the extracted repository's binary,
// installed on PATH under the default name `anvil-adapter-laravel`, is
// found. A governed breaking change to the naming or the lookup (ADR-025
// §12.1) fails this test.
//
// Reference: TS-016-01-02, 005-adapter-command-contract §10.1, ADR-025
// §3.4
func TestAdapterExecutableLookup_LaravelStandardOnPath(t *testing.T) {
	dir := laravelStandardOnPath(t)

	path, err := adapterExecutableLookup("anvil-adapter-laravel")
	if err != nil {
		t.Fatalf("adapterExecutableLookup(\"anvil-adapter-laravel\") failed: %v", err)
	}
	want := filepath.Join(dir, "anvil-adapter-laravel")
	if path != want {
		t.Errorf("resolved executable = %q, want %q", path, want)
	}
}

// ── Capabilities Probe (TS-007-039 §7, TS-017-02-02) ─────────────────

// TestProbeAdapterExecutable_LaravelStandardFromExtractedRepo verifies
// the post-gate executable resolution contract and probe: the real
// standard executable built from the extracted repository is resolved
// BY NAME (anvil-adapter-laravel on PATH, ADR-025 decision 4) and
// answers the capabilities probe, and its declared capabilities match
// the standard manifest declaration (005-adapter-command-contract §10.3)
// — deployment model `server`, the four activation phases, the five
// build phases in execution order, and the eight verification checks. A
// probe-failing or misdeclaring executable would fail this test,
// surfacing a broken extraction in CI. (The closed-set SET resolution —
// resolveAdapterSet — was removed at the switch-over gate, TS-017-02-02:
// discovery is registry-driven; the executable probe below is the
// per-name resolution contract.)
//
// Reference: TS-016-01-02, TS-007-039 §7, TS-017-02-02, ADR-025 §3.4
func TestProbeAdapterExecutable_LaravelStandardFromExtractedRepo(t *testing.T) {
	dir := laravelStandardOnPath(t)

	executable, err := resolveAdapterExecutable("laravel")
	if err != nil {
		t.Fatalf("resolveAdapterExecutable(laravel) failed: %v", err)
	}
	if want := filepath.Join(dir, "anvil-adapter-laravel"); executable != want {
		t.Errorf("laravel executable = %q, want %q", executable, want)
	}

	// The capabilities the probe read must match the standard's
	// declared capabilities (005-adapter-command-contract §10.3).
	decl, err := invokeAdapterCapabilities(context.Background(), "laravel", executable)
	if err != nil {
		t.Fatalf("invokeAdapterCapabilities failed: %v", err)
	}
	if decl.Declaration.DeploymentModel != string(contracts.DeploymentModelServer) {
		t.Errorf("DeploymentModel = %q, want %q", decl.Declaration.DeploymentModel, contracts.DeploymentModelServer)
	}
	wantActivation := []string{"migrate", "config_cache", "route_cache", "event_cache"}
	if !reflect.DeepEqual(decl.Declaration.ActivationPhases, wantActivation) {
		t.Errorf("ActivationPhases = %v, want %v", decl.Declaration.ActivationPhases, wantActivation)
	}
	wantBuild := []string{"composer", "npm", "config_cache", "route_cache", "view_cache"}
	if !reflect.DeepEqual(decl.Declaration.BuildPhases, wantBuild) {
		t.Errorf("BuildPhases = %v, want %v", decl.Declaration.BuildPhases, wantBuild)
	}
	wantChecks := []string{
		"vendor_present", "bootstrap_structure", "config_files",
		"artisan_file", "composer_json", "env_file",
		"app_directory", "routes_directory",
	}
	if len(decl.Declaration.VerificationChecks) != len(wantChecks) {
		t.Fatalf("VerificationChecks = %d checks, want %d: %v", len(decl.Declaration.VerificationChecks), len(wantChecks), decl.Declaration.VerificationChecks)
	}
	for i, check := range wantChecks {
		if decl.Declaration.VerificationChecks[i].Name != check {
			t.Errorf("VerificationChecks[%d].Name = %q, want %q", i, decl.Declaration.VerificationChecks[i].Name, check)
		}
	}
}

// ── Lifecycle Content Resolution (ADR-025 §3.4) ──────────────────────

// TestLaravelLifecycleContent_FromExtractedRepo verifies that the
// lifecycle content an existing Laravel project resolves — the pipeline
// template (template command, TS-007-038) and the artifact manifest
// commands (manifest command, TS-P7-15/TS-P7-16) — comes from the
// extracted standard executable: the build pipeline stages/tasks in
// execution order and the activation/rollback command strings an
// existing invocation reads at packaging time. A regression in the
// extracted repository's lifecycle content fails this test.
//
// Reference: TS-016-01-02, TS-007-038, TS-P7-15, TS-P7-16, ADR-025 §3.4
func TestLaravelLifecycleContent_FromExtractedRepo(t *testing.T) {
	dir := laravelStandardOnPath(t)
	executable := filepath.Join(dir, "anvil-adapter-laravel")
	coord := adapterCoordinator()

	tmpl, err := coord.InvokeTemplate(context.Background(), "laravel", executable, contracts.TemplateRequest{Framework: "laravel"})
	if err != nil {
		t.Fatalf("InvokeTemplate failed: %v", err)
	}
	if tmpl.Build == nil {
		t.Fatal("TemplateResult.Build is nil, want the Laravel build pipeline")
	}
	if tmpl.Build.Pipeline.Name != "build" {
		t.Errorf("build pipeline name = %q, want %q", tmpl.Build.Pipeline.Name, "build")
	}
	var stages []string
	for _, stage := range tmpl.Build.Pipeline.Stages {
		stages = append(stages, stage.Name)
		for _, task := range stage.Tasks {
			switch task.Name {
			case "composer-install":
				if task.Command != "composer" {
					t.Errorf("composer-install command = %q, want %q", task.Command, "composer")
				}
			case "npm-build":
				if task.Command != "npm" {
					t.Errorf("npm-build command = %q, want %q", task.Command, "npm")
				}
			}
		}
	}
	wantStages := []string{"dependencies", "assets", "optimize"}
	if !reflect.DeepEqual(stages, wantStages) {
		t.Errorf("build pipeline stages = %v, want %v", stages, wantStages)
	}

	manifest, err := coord.InvokeManifestCommands(context.Background(), "laravel", executable)
	if err != nil {
		t.Fatalf("InvokeManifestCommands failed: %v", err)
	}
	wantActivation := []string{
		"php artisan migrate --force",
		"php artisan config:cache",
		"php artisan route:cache",
		"php artisan view:cache",
	}
	if !reflect.DeepEqual(manifest.ActivationCommands, wantActivation) {
		t.Errorf("activation commands = %v, want %v", manifest.ActivationCommands, wantActivation)
	}
	wantRollback := []string{"php artisan migrate:rollback"}
	if !reflect.DeepEqual(manifest.RollbackCommands, wantRollback) {
		t.Errorf("rollback commands = %v, want %v", manifest.RollbackCommands, wantRollback)
	}
}

// ── Registry Metadata Consistency ────────────────────────────────────

// TestLaravelStandardManifest_RegistryMetadataConsistent verifies the
// standard's source manifest — the registry metadata the standard
// declares (manifest/registry-metadata.json) — parses with the Core
// registry client and is consistent with the resolved executable: the
// manifest id `anvil-standard-laravel` maps to the `laravel` framework
// the executable registers under, the declared contract version is
// 1.0.0, and the framework-version support scope covers Laravel
// 10.0.0/11.0.0/12.0.0 (005-adapter-command-contract §10.1). This locks
// the metadata side of the resolution contract so a drift between the
// executable and its manifest is visible in CI.
//
// Reference: TS-016-01-02, ADR-021 §5.4, ADR-025 §3.4
func TestLaravelStandardManifest_RegistryMetadataConsistent(t *testing.T) {
	standardDir := laravelStandardDir(t)
	executable := buildLaravelStandardExecutable(t)

	data, err := os.ReadFile(filepath.Join(standardDir, "manifest", "registry-metadata.json"))
	if err != nil {
		t.Fatalf("read standard manifest: %v", err)
	}
	result, err := registry.Parse(data)
	if err != nil {
		t.Fatalf("standard manifest does not parse with the registry client: %v", err)
	}
	md := result.Metadata
	if md.ID != "anvil-standard-laravel" {
		t.Errorf("manifest id = %q, want %q", md.ID, "anvil-standard-laravel")
	}
	if framework := strings.TrimPrefix(md.ID, "anvil-standard-"); framework != "laravel" {
		t.Errorf("manifest id framework segment = %q, want %q", framework, "laravel")
	}
	if md.ContractVersion != "1.0.0" {
		t.Errorf("manifest contractVersion = %q, want %q", md.ContractVersion, "1.0.0")
	}
	wantVersions := []string{"10.0.0", "11.0.0", "12.0.0"}
	if !reflect.DeepEqual(md.Capability.FrameworkVersion, wantVersions) {
		t.Errorf("manifest frameworkVersion scope = %v, want %v", md.Capability.FrameworkVersion, wantVersions)
	}

	// The resolved executable must report the deployment model the
	// manifest/MANIFEST.md declares (server).
	decl, err := adapter.NewCoordinator(execution.NewRunner(), adapter.NewCapabilityRegistry()).
		InvokeCapabilities(context.Background(), "laravel", executable)
	if err != nil {
		t.Fatalf("invoke capabilities on the resolved executable: %v", err)
	}
	if decl.Declaration.DeploymentModel != string(contracts.DeploymentModelServer) {
		t.Errorf("resolved executable DeploymentModel = %q, want %q", decl.Declaration.DeploymentModel, contracts.DeploymentModelServer)
	}
}
