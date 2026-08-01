// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P9-06
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSystemDiagnoseCommand_RegistersUnderSystem verifies that the
// diagnose command is registered as a subcommand of the system command.
//
// Reference: ST-P9-06
func TestSystemDiagnoseCommand_RegistersUnderSystem(t *testing.T) {
	systemSub, _, err := rootCmd.Find([]string{"system", "diagnose"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"system\", \"diagnose\"]) returned error: %v", err)
	}
	if systemSub == nil {
		t.Fatal("rootCmd.Find([\"system\", \"diagnose\"]) returned nil command")
	}
	if systemSub.Use != "diagnose" {
		t.Errorf("command Use = %q, want %q", systemSub.Use, "diagnose")
	}

	// Verify it is nested under system (not directly under root).
	_, _, err = rootCmd.Find([]string{"diagnose"})
	if err == nil {
		t.Error("rootCmd.Find([\"diagnose\"]) should have failed (diagnose is not a direct subcommand)")
	}
}

// TestSystemDiagnoseCommand_JsonFlag verifies that the --json flag is
// registered on the diagnose command.
//
// Reference: ST-P9-06
func TestSystemDiagnoseCommand_JsonFlag(t *testing.T) {
	diagnoseCmd, _, err := rootCmd.Find([]string{"system", "diagnose"})
	if err != nil {
		t.Fatalf("failed to find diagnose command: %v", err)
	}

	flag := diagnoseCmd.Flags().Lookup("json")
	if flag == nil {
		t.Error("--json flag should be on the diagnose subcommand")
	}
}

// TestSystemDiagnoseCommand_ServerRootFlag verifies that the --server-root
// flag is registered on the diagnose command.
//
// Reference: ST-P9-06
func TestSystemDiagnoseCommand_ServerRootFlag(t *testing.T) {
	diagnoseCmd, _, err := rootCmd.Find([]string{"system", "diagnose"})
	if err != nil {
		t.Fatalf("failed to find diagnose command: %v", err)
	}

	flag := diagnoseCmd.Flags().Lookup("server-root")
	if flag == nil {
		t.Error("--server-root flag should be on the diagnose subcommand")
	}
}

// TestSystemDiagnoseCommand_Healthy verifies that when no issues are
// detected, the diagnostic report is clean and exits with code 0.
//
// Reference: ST-P9-06
func TestSystemDiagnoseCommand_Healthy(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	_, stdout, stderr, err := executeCommand("system", "diagnose", "--server-root", dir)
	if err != nil {
		t.Fatalf("diagnose command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "Health: HEALTHY") {
		t.Errorf("stdout should contain 'Health: HEALTHY', got: %s", stdout)
	}
	if !contains(stdout, "No issues detected across all inspected components") {
		t.Errorf("stdout should contain the clean summary, got: %s", stdout)
	}
}

// TestSystemDiagnoseCommand_Issues verifies that detected issues are
// reported with severity, location, context classification, and owner +
// next action recommendations, exiting with code 1.
//
// Reference: ST-P9-06 (ST-009-006 AC1/AC2/AC4)
func TestSystemDiagnoseCommand_Issues(t *testing.T) {
	dir := t.TempDir()
	// Don't set up anything — every component has issues.

	_, stdout, stderr, err := executeCommand("system", "diagnose", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error (exit code 1) when issues are found, got nil")
	}

	// Critical issues drive the three-state health to UNHEALTHY.
	if !contains(stdout, "Health: UNHEALTHY") {
		t.Errorf("stdout should contain 'Health: UNHEALTHY' for critical issues, got: %s", stdout)
	}

	// Issues with severity and location.
	if !contains(stdout, "Issues (") {
		t.Errorf("stdout should contain the issues section, got: %s", stdout)
	}
	if !contains(stdout, "[CRITICAL] runtime — active_symlink") {
		t.Errorf("stdout should contain the runtime issue with severity, got: %s", stdout)
	}

	// Context classification per finding (ADR-015).
	if !contains(stdout, "Contexts (") {
		t.Errorf("stdout should contain the contexts section, got: %s", stdout)
	}
	if !contains(stdout, "[server_runtime] runtime") {
		t.Errorf("stdout should classify runtime findings as server_runtime, got: %s", stdout)
	}
	if !contains(stdout, "[release] release") {
		t.Errorf("stdout should classify release findings as release context, got: %s", stdout)
	}

	// Owner + next action per finding (recommendations).
	if !contains(stdout, "Recommendations (") {
		t.Errorf("stdout should contain the recommendations section, got: %s", stdout)
	}
	if !contains(stdout, "[EPIC-005]") {
		t.Errorf("stdout should reference the owning Epic for runtime findings, got: %s", stdout)
	}

	// Structured error on stderr.
	if !contains(stderr, "diagnostic issues found") {
		t.Errorf("stderr should contain 'diagnostic issues found', got: %s", stderr)
	}
}

// TestSystemDiagnoseCommand_IssueHowLine verifies that each reported
// issue carries its actionable "How:" step directly under the cause,
// referencing the Epic that owns the resolution (ST-P9-03 AC1/AC2).
//
// Reference: ST-P9-03 (ST-009-003 AC1/AC2)
func TestSystemDiagnoseCommand_IssueHowLine(t *testing.T) {
	dir := t.TempDir()
	// Don't set up anything — every component has issues.

	_, stdout, stderr, err := executeCommand("system", "diagnose", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error (exit code 1) when issues are found, got nil")
	}

	// Each issue renders its own "How:" line under the cause, and at
	// least one references the owning Epic EPIC-005 (runtime findings).
	howLineFound := false
	howLineWithEpicFound := false
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "    How: ") {
			howLineFound = true
			if strings.Contains(line, "[EPIC-005]") {
				howLineWithEpicFound = true
			}
		}
	}
	if !howLineFound {
		t.Errorf("stdout should contain at least one 'How:' line under an issue, got: %s", stdout)
	}
	if !howLineWithEpicFound {
		t.Errorf("a How line should reference the owning Epic [EPIC-005], got: %s", stdout)
	}

	// The structured error on stderr is unchanged.
	if !contains(stderr, "diagnostic issues found") {
		t.Errorf("stderr should contain 'diagnostic issues found', got: %s", stderr)
	}
}

// TestSystemDiagnoseCommand_ContextClassification verifies that the
// diagnostic report classifies findings into the correct architectural
// contexts: artifact presence findings are classified as Artifact (not
// Release), and config findings as Development.
//
// Reference: ST-P9-06 (ST-009-006 AC1/AC3)
func TestSystemDiagnoseCommand_ContextClassification(t *testing.T) {
	dir := t.TempDir()
	// Don't set up anything — release/artifact checks fail.

	_, stdout, _, err := executeCommand("system", "diagnose", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error when issues are found, got nil")
	}

	// Artifact identity is separated from Release identity: the artifact
	// presence failure is classified as artifact context.
	if !contains(stdout, "[artifact] release — artifact_presence") {
		t.Errorf("stdout should classify artifact presence findings as artifact context, got: %s", stdout)
	}
	if !contains(stdout, "[release] release — release_directory") {
		t.Errorf("stdout should classify release directory findings as release context, got: %s", stdout)
	}
}

// TestSystemDiagnoseCommand_ConfigIssueClassifiedAsDevelopment verifies
// that a broken project configuration is reported as a Development context
// finding — the report does not silently skip a broken configuration.
//
// Reference: ST-P9-06 (ST-009-006 AC1)
func TestSystemDiagnoseCommand_ConfigIssueClassifiedAsDevelopment(t *testing.T) {
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

	_, stdout, _, err := executeCommand("system", "diagnose", "--server-root", serverDir)
	if err == nil {
		t.Fatal("expected error when issues are found, got nil")
	}

	if !contains(stdout, "config_load") {
		t.Errorf("stdout should report the config load failure, got: %s", stdout)
	}
	if !contains(stdout, "[development] config — config_load") {
		t.Errorf("stdout should classify the config finding as development context, got: %s", stdout)
	}
	if !contains(stdout, "[EPIC-002]") {
		t.Errorf("stdout should reference EPIC-002 as the owner of config findings, got: %s", stdout)
	}
}

// TestSystemDiagnoseCommand_Json verifies that --json produces the standard
// envelope with issues, contexts, and recommendations.
//
// Reference: ST-P9-06 (ST-009-006 — machine-readable report)
func TestSystemDiagnoseCommand_Json(t *testing.T) {
	dir := t.TempDir()
	// Don't set up anything — issues exist.

	_, stdout, _, err := executeCommand("system", "diagnose", "--server-root", dir, "--json")

	if err == nil {
		t.Fatal("expected error when issues are found, got nil")
	}

	if !contains(stdout, "\"version\": \"1\"") {
		t.Errorf("JSON output should contain the envelope version, got: %s", stdout)
	}
	if !contains(stdout, "\"issues\"") {
		t.Errorf("JSON output should contain the issues field, got: %s", stdout)
	}
	if !contains(stdout, "\"contexts\"") {
		t.Errorf("JSON output should contain the contexts field, got: %s", stdout)
	}
	if !contains(stdout, "\"server_runtime\"") {
		t.Errorf("JSON output should contain the server_runtime context, got: %s", stdout)
	}
	if !contains(stdout, "\"recommendations\"") {
		t.Errorf("JSON output should contain the recommendations field, got: %s", stdout)
	}
}

// TestSystemDiagnoseCommand_SecureExecution verifies that running the
// diagnostic report does not modify any platform state.
//
// Reference: ST-P9-06
func TestSystemDiagnoseCommand_SecureExecution(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory before: %v", err)
	}

	_, _, _, _ = executeCommand("system", "diagnose", "--server-root", dir)

	entriesAfter, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory contents changed: before=%d entries, after=%d entries",
			len(entriesBefore), len(entriesAfter))
	}
}
