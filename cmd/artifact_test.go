// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P3-01, EPIC-003
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/project"
)

// TestArtifactCmd_Registered verifies that the artifact command is registered
// as a subcommand of the root command.
func TestArtifactCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "artifact" {
			found = true
			break
		}
	}

	if !found {
		t.Error("artifact command not found in root command's subcommands")
	}
}

// TestPackageCmd_Registered verifies that the package subcommand is registered
// under the artifact command.
func TestPackageCmd_Registered(t *testing.T) {
	var artifactSub *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Use == "artifact" {
			artifactSub = c
			break
		}
	}

	if artifactSub == nil {
		t.Fatal("artifact command not found")
	}

	found := false
	for _, c := range artifactSub.Commands() {
		if c.Use == "package" {
			found = true
			break
		}
	}

	if !found {
		t.Error("package subcommand not found under artifact command")
	}
}

// TestPackageCmd_Help verifies that the package command has the expected help
// text and usage information.
func TestPackageCmd_Help(t *testing.T) {
	if packageCmd.Short == "" {
		t.Error("package command short description is empty")
	}

	if packageCmd.Long == "" {
		t.Error("package command long description is empty")
	}

	if packageCmd.Use != "package" {
		t.Errorf("package command Use = %q, want %q", packageCmd.Use, "package")
	}
}

// TestPackageCmd_RunE verifies the package command has a RunE handler set.
func TestPackageCmd_RunE(t *testing.T) {
	if packageCmd.RunE == nil {
		t.Error("package command RunE handler is nil")
	}
}

// TestPackageCmd_NoArgs verifies the package command rejects arguments.
func TestPackageCmd_NoArgs(t *testing.T) {
	if packageCmd.Args == nil {
		t.Error("package command Args validator is nil, expected cobra.NoArgs")
		return
	}

	// Use a cobra.Command to test the args validator (cobra.NoArgs requires
	// a non-nil command reference even though it doesn't inspect it).
	cmd := &cobra.Command{Use: "package"}

	// The command accepts no arguments - calling with args should fail.
	err := packageCmd.Args(cmd, []string{"extra-arg"})
	if err == nil {
		t.Error("expected error when passing arguments to package command")
	}
}

// ── Manifest Command Wiring (TS-P7-15, TS-P7-16) ─────────────────────

// manifestCommandsFixture is the Laravel-style ManifestCommandResult JSON
// the fake adapter returns for the manifest command.
const manifestCommandsFixture = `{"activation_commands":["php artisan migrate --force","php artisan config:cache","php artisan route:cache","php artisan view:cache"],"rollback_commands":["php artisan migrate:rollback"]}`

// writeManifestAdapter writes an executable stub adapter named name into
// dir that answers the manifest command by catting manifestJSON, and
// returns the absolute path to the stub.
func writeManifestAdapter(t *testing.T, dir, name, manifestJSON string) string {
	t.Helper()
	path := filepath.Join(dir, name)

	fixture := filepath.Join(dir, name+".manifest.json")
	if err := os.WriteFile(fixture, []byte(manifestJSON), 0644); err != nil {
		t.Fatalf("write manifest fixture: %v", err)
	}
	script := fmt.Sprintf(`#!/bin/sh
# Stub adapter for artifact package tests. Answers the manifest command.
case "$1" in
  manifest) cat "%s" ;;
  *) echo "unknown command $1" >&2; exit 2 ;;
esac
exit 0
`, fixture)
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write stub adapter %s: %v", name, err)
	}
	return path
}

// setupPackageProject creates a project directory with anvil.yaml (name,
// version, and optional project.framework), changes the working directory
// into it, and returns the directory.
func setupPackageProject(t *testing.T, framework string) string {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, project.ConfigFileName)
	yaml := "project:\n  name: pkg-test\n  version: 1.0.0\n  description: packaging test\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write anvil.yaml: %v", err)
	}
	if framework != "" {
		if err := writeProjectFramework(configPath, framework); err != nil {
			t.Fatalf("seed framework: %v", err)
		}
	}
	chdirTo(t, dir)
	return dir
}

// readPackagedManifest runs `anvil artifact package --format tar.gz
// --output outDir` and returns the manifest read from the produced
// archive. The stderr of the command is returned for warning assertions.
func readPackagedManifest(t *testing.T, outDir string) (*artifact.Manifest, string) {
	t.Helper()
	_, _, stderr, err := executeCommand("artifact", "package", "--format", "tar.gz", "--output", outDir)
	if err != nil {
		t.Fatalf("artifact package returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	archives, err := filepath.Glob(filepath.Join(outDir, "artifact-*.tar.gz"))
	if err != nil {
		t.Fatalf("glob archives: %v", err)
	}
	if len(archives) != 1 {
		t.Fatalf("found %d artifact archives, want 1 (stderr: %s)", len(archives), stderr)
	}
	manifest, err := artifact.ReadManifest(archives[0])
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	return manifest, stderr
}

// TestPackage_FillsManifestCommandsFromAdapter verifies that `anvil
// artifact package` fetches the framework's activation and rollback
// command strings from the adapter executable through the manifest
// command and stores them in the artifact manifest (TS-P7-15, TS-P7-16 —
// the wiring deferred from the adapter batch, now implemented).
//
// Reference: TS-P7-15 AC-2, TS-P7-16 AC-2, 005-adapter-command-contract §10.10
func TestPackage_FillsManifestCommandsFromAdapter(t *testing.T) {
	dir := setupPackageProject(t, "laravel")
	stubAdapterLookup(t, dir)
	writeManifestAdapter(t, dir, "anvil-adapter-laravel", manifestCommandsFixture)

	manifest, stderr := readPackagedManifest(t, t.TempDir())

	wantActivation := []string{
		"php artisan migrate --force",
		"php artisan config:cache",
		"php artisan route:cache",
		"php artisan view:cache",
	}
	if !reflect.DeepEqual(manifest.ActivationCommands, wantActivation) {
		t.Errorf("manifest ActivationCommands = %v, want %v", manifest.ActivationCommands, wantActivation)
	}
	wantRollback := []string{"php artisan migrate:rollback"}
	if !reflect.DeepEqual(manifest.RollbackCommands, wantRollback) {
		t.Errorf("manifest RollbackCommands = %v, want %v", manifest.RollbackCommands, wantRollback)
	}
	if strings.Contains(stderr, "Warning:") {
		t.Errorf("stderr should not contain warnings, got: %s", stderr)
	}
}

// TestPackage_MissingAdapterPackagesWithoutCommands verifies that a
// project whose framework adapter executable is not installed still
// packages successfully: a warning is printed and the manifest omits the
// activation/rollback commands (backward compatible — packaging is a
// core operation and must not be blocked by the optional adapter).
//
// Reference: TS-P7-15, TS-P7-16, ADR-009 §9.7
func TestPackage_MissingAdapterPackagesWithoutCommands(t *testing.T) {
	setupPackageProject(t, "laravel")
	outDir := t.TempDir()

	// No adapter executable resolves anywhere.
	orig := adapterExecutableLookup
	adapterExecutableLookup = func(name string) (string, error) {
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { adapterExecutableLookup = orig })

	manifest, stderr := readPackagedManifest(t, outDir)
	if !strings.Contains(stderr, "Warning:") {
		t.Errorf("stderr should contain a warning about the missing adapter, got: %s", stderr)
	}
	if len(manifest.ActivationCommands) != 0 {
		t.Errorf("manifest ActivationCommands = %v, want empty", manifest.ActivationCommands)
	}
	if len(manifest.RollbackCommands) != 0 {
		t.Errorf("manifest RollbackCommands = %v, want empty", manifest.RollbackCommands)
	}
}

// TestPackage_NoFrameworkPackagesWithoutCommands verifies the
// pre-existing behavior: a project without a framework packages without
// manifest commands and never consults an adapter (the failing lookup
// proves the adapter is not invoked).
//
// Reference: TS-P7-15, TS-P7-16
func TestPackage_NoFrameworkPackagesWithoutCommands(t *testing.T) {
	setupPackageProject(t, "")
	outDir := t.TempDir()

	orig := adapterExecutableLookup
	adapterExecutableLookup = func(name string) (string, error) {
		t.Fatalf("adapter lookup called for %q with no framework", name)
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { adapterExecutableLookup = orig })

	manifest, _ := readPackagedManifest(t, outDir)
	if len(manifest.ActivationCommands) != 0 {
		t.Errorf("manifest ActivationCommands = %v, want empty", manifest.ActivationCommands)
	}
	if len(manifest.RollbackCommands) != 0 {
		t.Errorf("manifest RollbackCommands = %v, want empty", manifest.RollbackCommands)
	}
}
