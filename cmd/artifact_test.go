// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P3-01, EPIC-003
package cmd

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
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

// ── BUG-005: packaging anchors to the project root, not the CWD ────────

// readTarEntries returns the list of entry names in a tar.gz archive.
func readTarEntries(t *testing.T, archivePath string) []string {
	t.Helper()

	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("create gzip reader: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var entries []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		entries = append(entries, hdr.Name)
	}
	return entries
}

// setupSubdirPackageProject creates a project with a root-level file and a
// subdirectory containing its own file, changes the working directory into
// the subdirectory, and returns (projectRoot, subdir).
func setupSubdirPackageProject(t *testing.T) (string, string) {
	t.Helper()
	root := setupPackageProject(t, "")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("root content\n"), 0644); err != nil {
		t.Fatalf("write root README.md: %v", err)
	}
	subdir := filepath.Join(root, "subdir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "subfile.txt"), []byte("subdir content\n"), 0644); err != nil {
		t.Fatalf("write subfile.txt: %v", err)
	}
	// Reproduce the bug trigger: invoke packaging from the subdirectory.
	chdirTo(t, subdir)
	return root, subdir
}

// TestPackage_FromSubdirectoryArchivesProjectRoot verifies that `anvil
// artifact package` invoked from a project subdirectory archives the
// project root content — not the current working directory — with the
// subdirectory preserved at its relative path (BUG-005).
//
// Reference: BUG-005, Section 12 (Validation steps 1-2, 5)
func TestPackage_FromSubdirectoryArchivesProjectRoot(t *testing.T) {
	root, _ := setupSubdirPackageProject(t)
	outDir := t.TempDir()

	_, _, stderr, err := executeCommand("artifact", "package", "--format", "tar.gz", "--output", outDir)
	if err != nil {
		t.Fatalf("artifact package from subdirectory returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	archives, err := filepath.Glob(filepath.Join(outDir, "artifact-*.tar.gz"))
	if err != nil {
		t.Fatalf("glob archives: %v", err)
	}
	if len(archives) != 1 {
		t.Fatalf("found %d artifact archives, want 1 (stderr: %s)", len(archives), stderr)
	}

	entries := readTarEntries(t, archives[0])
	entrySet := make(map[string]bool, len(entries))
	for _, e := range entries {
		entrySet[e] = true
	}

	// Root-level files must be packaged (including anvil.yaml itself).
	for _, want := range []string{
		filepath.Join(artifact.DeployableContentDir, "anvil.yaml"),
		filepath.Join(artifact.DeployableContentDir, "README.md"),
	} {
		if !entrySet[want] {
			t.Errorf("archive missing project-root file %q; entries: %v", want, entries)
		}
	}

	// The subdirectory must be preserved at its relative path.
	subEntry := filepath.Join(artifact.DeployableContentDir, "subdir", "subfile.txt")
	if !entrySet[subEntry] {
		t.Errorf("archive missing subdirectory file %q; entries: %v", subEntry, entries)
	}

	// A file packaged at the root level from the CWD would prove the
	// archive was built from the subdirectory instead of the project root.
	cwdEntry := filepath.Join(artifact.DeployableContentDir, "subfile.txt")
	if entrySet[cwdEntry] {
		t.Errorf("archive contains CWD-anchored entry %q, want project-root packaging; entries: %v", cwdEntry, entries)
	}

	// Sanity: the CWD is indeed the subdirectory during the test.
	if cwd, _ := os.Getwd(); cwd != filepath.Join(root, "subdir") {
		t.Fatalf("test setup did not chdir into subdir (cwd = %s)", cwd)
	}
}

// TestPackage_RelativeOutputAnchorsToProjectRoot verifies that a relative
// --output path resolves against the project root, not the CWD, when
// packaging runs from a project subdirectory (BUG-005).
//
// Reference: BUG-005, Section 12 (Validation step 3)
func TestPackage_RelativeOutputAnchorsToProjectRoot(t *testing.T) {
	root, subdir := setupSubdirPackageProject(t)

	_, _, stderr, err := executeCommand("artifact", "package", "--format", "tar.gz", "--output", "dist")
	if err != nil {
		t.Fatalf("artifact package with relative --output returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	archives, err := filepath.Glob(filepath.Join(root, "dist", "artifact-*.tar.gz"))
	if err != nil {
		t.Fatalf("glob archives in project-root dist: %v", err)
	}
	if len(archives) != 1 {
		t.Fatalf("found %d artifact archives in %s, want 1 (stderr: %s)", len(archives), filepath.Join(root, "dist"), stderr)
	}

	// The CWD-relative output directory must not be created.
	if _, err := os.Stat(filepath.Join(subdir, "dist")); !os.IsNotExist(err) {
		t.Errorf("CWD-relative output directory %s exists, want project-root anchoring", filepath.Join(subdir, "dist"))
	}
}

// ── F1 Security Guard: Slash Framework Name Never Probes (team review F1) ─

// TestPackage_SlashFrameworkNameNeverProbesAdapter is the packaging
// regression test for team review F1 (security blocker): a project
// declaration with a path separator in project.framework must be
// rejected BEFORE any lookup — the CWD-relative trap executable is
// never resolved and never executed; packaging degrades to the
// missing-adapter warning. adapterExecutableLookup stays the REAL
// exec.LookPath: a regression of the identifier guard would resolve and
// execute the trap and fail this test.
func TestPackage_SlashFrameworkNameNeverProbesAdapter(t *testing.T) {
	dir := setupPackageProject(t, "x/evil")
	outDir := t.TempDir()
	placeLookPathTrap(t, dir, "pwned-package")

	manifest, stderr := readPackagedManifest(t, outDir)
	assertTrapNotExecuted(t, dir, "pwned-package")
	if !strings.Contains(stderr, "Warning:") {
		t.Errorf("stderr should carry the degrade warning, got: %s", stderr)
	}
	if len(manifest.ActivationCommands) != 0 || len(manifest.RollbackCommands) != 0 {
		t.Errorf("manifest commands = %v/%v, want empty (invalid framework must not reach an adapter)", manifest.ActivationCommands, manifest.RollbackCommands)
	}
}
