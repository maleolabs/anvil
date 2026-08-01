// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P9-01
package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestServerDoctorCommand_RegistersUnderServer verifies that the doctor
// command is registered as a subcommand of the server command.
//
// Reference: ST-P9-01
func TestServerDoctorCommand_RegistersUnderServer(t *testing.T) {
	serverSub, _, err := rootCmd.Find([]string{"server", "doctor"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"server\", \"doctor\"]) returned error: %v", err)
	}
	if serverSub == nil {
		t.Fatal("rootCmd.Find([\"server\", \"doctor\"]) returned nil command")
	}
	if serverSub.Use != "doctor" {
		t.Errorf("command Use = %q, want %q", serverSub.Use, "doctor")
	}

	// Verify it is nested under server (not directly under root).
	_, _, err = rootCmd.Find([]string{"doctor"})
	if err == nil {
		t.Error("rootCmd.Find([\"doctor\"]) should have failed (doctor is not a direct subcommand)")
	}
}

// TestServerDoctorCommand_JsonFlag verifies that the --json flag is
// registered on the doctor command.
//
// Reference: ST-P9-01
func TestServerDoctorCommand_JsonFlag(t *testing.T) {
	doctorCmd, _, err := rootCmd.Find([]string{"server", "doctor"})
	if err != nil {
		t.Fatalf("failed to find doctor command: %v", err)
	}

	flag := doctorCmd.Flags().Lookup("json")
	if flag == nil {
		t.Error("--json flag should be on the doctor subcommand")
	}
}

// TestServerDoctorCommand_ServerRootFlag verifies that the --server-root
// flag is registered on the doctor command.
//
// Reference: ST-P9-01
func TestServerDoctorCommand_ServerRootFlag(t *testing.T) {
	doctorCmd, _, err := rootCmd.Find([]string{"server", "doctor"})
	if err != nil {
		t.Fatalf("failed to find doctor command: %v", err)
	}

	flag := doctorCmd.Flags().Lookup("server-root")
	if flag == nil {
		t.Error("--server-root flag should be on the doctor subcommand")
	}
}

// TestServerDoctorCommand_Healthy verifies that when all components are
// healthy, the doctor reports "Healthy" and exits with code 0.
//
// Reference: ST-P9-01 (ST-009-001 AC1)
func TestServerDoctorCommand_Healthy(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	_, stdout, stderr, err := executeCommand("server", "doctor", "--server-root", dir)
	if err != nil {
		t.Fatalf("doctor command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "Health: HEALTHY") {
		t.Errorf("stdout should contain 'Health: HEALTHY', got: %s", stdout)
	}
}

// TestServerDoctorCommand_Degraded verifies that when non-critical checks
// fail, the doctor reports "Degraded" with failure details and exits with
// code 1.
//
// The setup removes the releases directory from an otherwise healthy
// server root: the runtime and release components fail while the other
// components pass — producing the degraded state.
//
// Reference: ST-P9-01 (ST-009-001 AC2)
func TestServerDoctorCommand_Degraded(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	// Break only the releases directory: runtime + release components fail,
	// everything else passes → degraded (not unhealthy).
	if err := os.RemoveAll(filepath.Join(dir, "releases")); err != nil {
		t.Fatalf("remove releases dir: %v", err)
	}

	_, stdout, stderr, err := executeCommand("server", "doctor", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error (exit code 1) when degraded, got nil")
	}

	if !contains(stdout, "Health: DEGRADED") {
		t.Errorf("stdout should contain 'Health: DEGRADED', got: %s", stdout)
	}

	// Per-component details with failed check counts.
	if !contains(stdout, "[FAIL] runtime") || !contains(stdout, "[FAIL] release") {
		t.Errorf("stdout should list failed components, got: %s", stdout)
	}
	if !contains(stdout, "[PASS] config") {
		t.Errorf("stdout should list the passing config component, got: %s", stdout)
	}

	// Guidance for failed checks (issues derived from the failed checks).
	if !contains(stdout, "Issues (") {
		t.Errorf("stdout should contain guidance issues for failed checks, got: %s", stdout)
	}

	// Structured error on stderr.
	if !contains(stderr, "platform health is degraded") {
		t.Errorf("stderr should contain 'platform health is degraded', got: %s", stderr)
	}
}

// TestServerDoctorCommand_Unhealthy verifies that when all checks fail,
// the doctor reports "Unhealthy" and exits with code 1.
//
// The test changes the working directory to a temp directory containing an
// invalid anvil.yaml so that the config component fails too, and isolates
// the global config directory to keep discovery deterministic.
//
// Reference: ST-P9-01 (ST-009-001 AC3)
func TestServerDoctorCommand_Unhealthy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Project directory with an invalid anvil.yaml: config sources exist,
	// so the config component must fail to load.
	projectDir := t.TempDir()
	invalidConfig := "project:\n  name: 12345\n" // project.name must be a string
	if err := os.WriteFile(filepath.Join(projectDir, "anvil.yaml"), []byte(invalidConfig), 0644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("failed to restore original directory %q: %v", orig, err)
		}
	}()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("failed to change to project directory: %v", err)
	}

	// Empty server root: every server component fails.
	serverDir := t.TempDir()

	_, stdout, stderr, err := executeCommand("server", "doctor", "--server-root", serverDir)
	if err == nil {
		t.Fatal("expected error (exit code 1) when unhealthy, got nil")
	}

	if !contains(stdout, "Health: UNHEALTHY") {
		t.Errorf("stdout should contain 'Health: UNHEALTHY', got: %s", stdout)
	}

	// The config load failure must be reported as a failed config component.
	if !contains(stdout, "[FAIL] config") || !contains(stdout, "config_load") {
		t.Errorf("stdout should report the config load failure, got: %s", stdout)
	}

	if !contains(stderr, "platform health is unhealthy") {
		t.Errorf("stderr should contain 'platform health is unhealthy', got: %s", stderr)
	}
}

// TestServerDoctorCommand_UnhealthyJson verifies that --json works when the
// platform is unhealthy: the report is written as JSON with the unhealthy
// status and the command exits non-zero.
//
// Reference: ST-P9-01
func TestServerDoctorCommand_UnhealthyJson(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	projectDir := t.TempDir()
	invalidConfig := "project:\n  name: 12345\n" // project.name must be a string
	if err := os.WriteFile(filepath.Join(projectDir, "anvil.yaml"), []byte(invalidConfig), 0644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("failed to restore original directory %q: %v", orig, err)
		}
	}()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("failed to change to project directory: %v", err)
	}

	serverDir := t.TempDir()

	_, stdout, _, err := executeCommand("server", "doctor", "--server-root", serverDir, "--json")
	if err == nil {
		t.Fatal("expected error when platform is unhealthy, got nil")
	}

	if !contains(stdout, "\"status\": \"unhealthy\"") {
		t.Errorf("JSON output should contain data.status unhealthy, got: %s", stdout)
	}
	if !contains(stdout, "\"config_load\"") {
		t.Errorf("JSON output should contain the config_load check, got: %s", stdout)
	}
}

// TestServerDoctorCommand_JsonOutput verifies that --json produces the
// standard envelope with the three-state health status and components.
//
// Reference: ST-P9-01
func TestServerDoctorCommand_JsonOutput(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	_, stdout, _, err := executeCommand("server", "doctor", "--server-root", dir, "--json")
	if err != nil {
		t.Fatalf("doctor command returned unexpected error: %v", err)
	}

	if !contains(stdout, "\"version\": \"1\"") {
		t.Errorf("JSON output should contain the envelope version, got: %s", stdout)
	}
	if !contains(stdout, "\"status\": \"healthy\"") {
		t.Errorf("JSON output should contain data.status healthy, got: %s", stdout)
	}
	if !contains(stdout, "\"components\"") {
		t.Errorf("JSON output should contain the components field, got: %s", stdout)
	}
}

// TestServerDoctorCommand_JsonNotHealthy verifies that --json works when
// the platform is not healthy: the report is still written as JSON and the
// command exits non-zero.
//
// The server root is empty (all server components fail), but the project
// configuration discovered from the test working directory remains valid,
// so the config component passes — the platform is degraded, not
// unhealthy. The global config directory is isolated to keep the outcome
// deterministic.
//
// Reference: ST-P9-01
func TestServerDoctorCommand_JsonNotHealthy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	// Don't set up anything — the server components will fail.

	_, stdout, _, err := executeCommand("server", "doctor", "--server-root", dir, "--json")

	if err == nil {
		t.Fatal("expected error when platform is not healthy, got nil")
	}

	if !contains(stdout, "\"status\": \"degraded\"") {
		t.Errorf("JSON output should contain data.status degraded, got: %s", stdout)
	}
}

// TestServerDoctorCommand_AllComponentsReported verifies that every
// component is reported individually in the output.
//
// Reference: ST-P9-01 (ST-009-001 AC4)
func TestServerDoctorCommand_AllComponentsReported(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	_, stdout, _, err := executeCommand("server", "doctor", "--server-root", dir)
	if err != nil {
		t.Fatalf("doctor command returned unexpected error: %v", err)
	}

	expectedComponents := []string{"runtime", "config", "release", "server_readiness", "registry"}
	for _, comp := range expectedComponents {
		if !contains(stdout, comp) {
			t.Errorf("stdout should contain component %q, got: %s", comp, stdout)
		}
	}
}

// TestServerDoctorCommand_ConfigNotAvailable verifies that when no project
// configuration exists, the config component is reported as not available
// (a passing informational check) instead of disappearing from the output,
// and the platform remains healthy.
//
// Reference: ST-P9-01 (m-5, ST-009-001 — project may or may not exist)
func TestServerDoctorCommand_ConfigNotAvailable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Change to an empty directory with no project configuration anywhere.
	emptyDir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("failed to restore original directory %q: %v", orig, err)
		}
	}()
	if err := os.Chdir(emptyDir); err != nil {
		t.Fatalf("failed to change to empty directory: %v", err)
	}

	serverDir := t.TempDir()
	setupHealthyServerRoot(t, serverDir)

	_, stdout, stderr, err := executeCommand("server", "doctor", "--server-root", serverDir)
	if err != nil {
		t.Fatalf("doctor command returned unexpected error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// The platform is still healthy without a project.
	if !contains(stdout, "Health: HEALTHY") {
		t.Errorf("stdout should contain 'Health: HEALTHY', got: %s", stdout)
	}

	// The config component is present and reported as not available.
	_, jsonOut, _, err := executeCommand("server", "doctor", "--server-root", serverDir, "--json")
	if err != nil {
		t.Fatalf("doctor --json returned unexpected error: %v", err)
	}
	if !contains(jsonOut, "\"config_availability\"") {
		t.Errorf("JSON output should contain the config_availability check, got: %s", jsonOut)
	}
	if !contains(jsonOut, "not available") {
		t.Errorf("JSON output should report the config component as not available, got: %s", jsonOut)
	}
}

// TestServerDoctorCommand_SecureExecution verifies that running the doctor
// command does not modify any platform state.
//
// Reference: ST-P9-01 (ST-009-001 AC6)
func TestServerDoctorCommand_SecureExecution(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory before: %v", err)
	}

	_, _, _, _ = executeCommand("server", "doctor", "--server-root", dir)

	entriesAfter, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory contents changed: before=%d entries, after=%d entries",
			len(entriesBefore), len(entriesAfter))
	}
}
