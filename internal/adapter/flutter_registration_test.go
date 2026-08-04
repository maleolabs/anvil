// Integration test of the generic adapter registration helper (TS-P7-12)
// against the REAL compiled Flutter adapter executable (TS-P7-20): the
// Flutter adapter registers with the Core registries through the same
// path a project selecting the "flutter" adapter uses — capabilities and
// extension commands dispatched through the real Process Runner.
//
// This test exists because AC-4 of TS-P7-20 (adapter registers with the
// registry) is only satisfied when the adapter's `extension` command
// succeeds: RegisterAdapterExecutable requires BOTH the capabilities and
// the extension command to succeed (registration.go), and the Flutter
// adapter's extension scaffold (internal/flutter/config.go) is what makes
// registration possible.
//
// The test compiles cmd/flutter-adapter with `go build` and invokes the
// resulting binary as a subprocess — the same pattern as
// internal/laravel/binary_test.go. It creates NO Go import of
// internal/flutter from Core code (ADR-009 §8.1: Core never depends on
// adapters); framework values appear as literals, mirroring the existing
// adapter package tests.
//
// Reference: TS-P7-20 AC-4, TS-P7-12, ADR-009 §8.1
package adapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
)

// buildFlutterAdapterBinary compiles the Flutter adapter executable into
// a temp dir and returns its path. The module root is located by walking
// up from this test file to the go.mod. The binary name mirrors the
// convention `anvil-adapter-<framework>` (005-adapter-command-contract
// §10).
func buildFlutterAdapterBinary(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test file")
		}
		dir = parent
	}

	bin := filepath.Join(t.TempDir(), "anvil-adapter-flutter")
	cmd := exec.Command("go", "build", "-o", bin, "maleolabs.com/anvil/cmd/flutter-adapter")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build flutter adapter binary: %v\n%s", err, out)
	}
	return bin
}

// TestRegisterAdapterExecutable_FlutterAdapter verifies that the real
// Flutter adapter executable registers with the Core registries: the
// capabilities command declares the hybrid model, the three build
// targets, and the two verification checks, and the extension command
// returns the Flutter config keys — the command pair
// RegisterAdapterExecutable requires (TS-P7-20 AC-4, TS-P7-12). Before
// the extension scaffold existed, this test failed with "adapter process
// failed" because the Flutter adapter exited 2 for the extension
// command.
func TestRegisterAdapterExecutable_FlutterAdapter(t *testing.T) {
	executable := buildFlutterAdapterBinary(t)
	capabilities := NewCapabilityRegistry()
	extensions := NewConfigExtensionRegistry()

	err := RegisterAdapterExecutable(
		context.Background(), execution.NewRunner(),
		capabilities, extensions, "flutter", executable,
	)
	if err != nil {
		t.Fatalf("RegisterAdapterExecutable for flutter returned error: %v", err)
	}

	// The capability declaration: hybrid model, three build targets in
	// table order, no activation phases (TS-P7-20 AC-3, AC-5).
	decl, ok := capabilities.Capabilities("flutter")
	if !ok {
		t.Fatal("capability declaration not registered for flutter")
	}
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

	// The config extension: the flutter namespace with the two
	// TS-P7-26 keys — targets (default "web,apk") and build_args.
	ext, ok := extensions.Extension("flutter")
	if !ok {
		t.Fatal("config extension not registered for flutter")
	}
	if ext.Framework != "flutter" {
		t.Errorf("Extension.Framework = %q, want %q", ext.Framework, "flutter")
	}
	if len(ext.Keys) != 2 {
		t.Fatalf("Extension.Keys = %v, want the two TS-P7-26 keys", ext.Keys)
	}
	wantKeys := []string{"framework.flutter.targets", "framework.flutter.build_args"}
	for i, name := range wantKeys {
		if ext.Keys[i].Name != name {
			t.Errorf("Extension.Keys[%d].Name = %q, want %q", i, ext.Keys[i].Name, name)
		}
	}
}
