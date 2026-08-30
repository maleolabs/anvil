// Integration test of the generic adapter registration helper (TS-P7-12)
// against the REAL compiled Laravel standard executable (TS-P7-09..TS-P7-18,
// TS-016-01-01): the Laravel delivery lifecycle standard registers with
// the Core registries through the same path a project selecting the
// "laravel" adapter uses — capabilities and extension commands dispatched
// through the real Process Runner.
//
// This test exists because AC-4 of TS-P7-20 (adapter registers with the
// registry) is only satisfied when the standard's `extension` command
// succeeds: RegisterAdapterExecutable requires BOTH the capabilities and
// the extension command to succeed (registration.go).
//
// Since the repository split (TS-016-01-01, ADR-025), the Laravel standard
// executable is no longer built from this repository: it is built from the
// anvil-standard-laravel repository. The test compiles
// `cmd/laravel-adapter` there with `go build` and invokes the resulting
// binary as a subprocess — the same pattern as the Flutter standard's
// registration test (flutter_registration_test.go, TS-016-02-01) and the
// same pattern the Laravel standard's own binary test uses in the
// anvil-standard-laravel repository. The standard repository location is
// taken from ANVIL_STANDARD_LARAVEL_DIR (the local clone path the e2e
// suite uses, E2E_STANDARD_LARAVEL_DIR; the ANVIL_-prefixed name mirrors
// ANVIL_STANDARD_FLUTTER_DIR from TS-016-02-01); the test skips when the
// variable is unset, because the standard repository lives outside the
// Core checkout. It creates NO Go import of the standard's packages from
// Core code (ADR-009 §8.1: Core never depends on adapters/standards);
// framework values appear as literals, mirroring the existing adapter
// package tests.
//
// Reference: TS-P7-20 AC-4, TS-P7-12, TS-016-01-01, TS-016-01-02,
// ADR-009 §8.1, ADR-025
package adapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
)

// buildLaravelAdapterBinary compiles the Laravel standard executable from
// the anvil-standard-laravel repository into a temp dir and returns its
// path. The standard repository path comes from ANVIL_STANDARD_LARAVEL_DIR;
// the binary name mirrors the convention `anvil-adapter-<framework>`
// (005-adapter-command-contract §10).
func buildLaravelAdapterBinary(t *testing.T) string {
	t.Helper()

	standardDir := os.Getenv("ANVIL_STANDARD_LARAVEL_DIR")
	if standardDir == "" {
		t.Skip("ANVIL_STANDARD_LARAVEL_DIR not set — the Laravel standard repository is outside the Core checkout (TS-016-01-01)")
	}
	if _, err := os.Stat(filepath.Join(standardDir, "go.mod")); err != nil {
		t.Skipf("ANVIL_STANDARD_LARAVEL_DIR %q does not contain the anvil-standard-laravel module", standardDir)
	}

	bin := filepath.Join(t.TempDir(), "anvil-adapter-laravel")
	cmd := exec.Command("go", "build", "-o", bin, "maleolabs.com/anvil-standard-laravel/cmd/laravel-adapter")
	cmd.Dir = standardDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build laravel standard executable: %v\n%s", err, out)
	}
	return bin
}

// TestRegisterAdapterExecutable_LaravelAdapter verifies that the real
// Laravel standard executable registers with the Core registries: the
// capabilities command declares the server deployment model, the four
// activation phases, the five build phases in execution order, and the
// eight verification checks, and the extension command returns the five
// Laravel config keys — the command pair RegisterAdapterExecutable
// requires (TS-P7-20 AC-4, TS-P7-12). The expected values mirror the
// standard's manifest declaration (MANIFEST.md, manifest/
// registry-metadata.json) and 005-adapter-command-contract §10.3.
func TestRegisterAdapterExecutable_LaravelAdapter(t *testing.T) {
	executable := buildLaravelAdapterBinary(t)
	capabilities := NewCapabilityRegistry()
	extensions := NewConfigExtensionRegistry()

	err := RegisterAdapterExecutable(
		context.Background(), execution.NewRunner(),
		capabilities, extensions, "laravel", executable,
	)
	if err != nil {
		t.Fatalf("RegisterAdapterExecutable for laravel returned error: %v", err)
	}

	// The capability declaration: server model, activation phases in
	// table order, build phases in execution order, eight verification
	// checks (TS-P7-20 AC-3, AC-5; 005-adapter-command-contract §10.3).
	decl, ok := capabilities.Capabilities("laravel")
	if !ok {
		t.Fatal("capability declaration not registered for laravel")
	}
	if decl.DeploymentModel != string(contracts.DeploymentModelServer) {
		t.Errorf("DeploymentModel = %q, want %q", decl.DeploymentModel, contracts.DeploymentModelServer)
	}
	wantActivation := []string{"migrate", "config_cache", "route_cache", "event_cache"}
	if !reflect.DeepEqual(decl.ActivationPhases, wantActivation) {
		t.Errorf("ActivationPhases = %v, want %v", decl.ActivationPhases, wantActivation)
	}
	wantBuild := []string{"composer", "npm", "config_cache", "route_cache", "view_cache"}
	if !reflect.DeepEqual(decl.BuildPhases, wantBuild) {
		t.Errorf("BuildPhases = %v, want %v", decl.BuildPhases, wantBuild)
	}
	wantChecks := []string{
		"vendor_present", "bootstrap_structure", "config_files",
		"artisan_file", "composer_json", "env_file",
		"app_directory", "routes_directory",
	}
	if len(decl.VerificationChecks) != len(wantChecks) {
		t.Fatalf("VerificationChecks = %d checks, want %d: %v", len(decl.VerificationChecks), len(wantChecks), decl.VerificationChecks)
	}
	for i, check := range wantChecks {
		if decl.VerificationChecks[i].Name != check {
			t.Errorf("VerificationChecks[%d].Name = %q, want %q", i, decl.VerificationChecks[i].Name, check)
		}
		if decl.VerificationChecks[i].Description == "" {
			t.Errorf("VerificationChecks[%d].Description is empty, want a description", i)
		}
	}

	// The config extension: the laravel namespace with the five
	// TS-P7-18 keys — migrations.path (default database/migrations),
	// cache.store (default file), version, php_version, composer_flags.
	ext, ok := extensions.Extension("laravel")
	if !ok {
		t.Fatal("config extension not registered for laravel")
	}
	if ext.Framework != "laravel" {
		t.Errorf("Extension.Framework = %q, want %q", ext.Framework, "laravel")
	}
	if len(ext.Keys) != 5 {
		t.Fatalf("Extension.Keys = %v, want the five TS-P7-18 keys", ext.Keys)
	}
	wantKeys := []string{
		"framework.laravel.migrations.path",
		"framework.laravel.cache.store",
		"framework.laravel.version",
		"framework.laravel.php_version",
		"framework.laravel.composer_flags",
	}
	for i, name := range wantKeys {
		if ext.Keys[i].Name != name {
			t.Errorf("Extension.Keys[%d].Name = %q, want %q", i, ext.Keys[i].Name, name)
		}
	}
}
