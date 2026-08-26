package cmd

// Truthful exit-code enforcement tests (TS-019-03-02, T-015).
//
// The exit-code audit (docs/operations/exit-codes-audit.md) mapped every
// command and failure class to its truthful code (ADR-032, D11). These
// tests assert that mapping per command and per failure class on the
// canonical surfaces only (deprecated adapter/runtime surfaces are
// excluded from enforcement per decision D-09). They extend the
// requireExitCode pattern from exit_codes_test.go (BUG-006).
//
// Reference: TS-019-03-02, ADR-032, docs/operations/exit-codes-audit.md

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/output"
)

// ── Precondition (4) — F-01: uninitialized Server Runtime ─────────────

// TestTruthfulExitCodes_Precondition_ServerConfigGet verifies that
// "anvil server config get" against an uninitialized Runtime exits 4
// (precondition) — the F-01 finding closed for the config surface.
func TestTruthfulExitCodes_Precondition_ServerConfigGet(t *testing.T) {
	dir := t.TempDir()
	_, _, stderr, err := executeCommand("server", "config", "get", "runtime.id", "--server-root", dir)
	requireExitCode(t, err, output.ExitCodePrecondition)
	if !contains(stderr, "not initialized") {
		t.Errorf("stderr should report the uninitialized Runtime, got: %s", stderr)
	}
}

// TestTruthfulExitCodes_Precondition_ServerReleaseHistoryActiveStatusActivateCleanup
// verifies that the F-01 server-release commands gate with 4
// (precondition) when the Runtime is not initialized — closing the
// largest server-surface gap from the audit.
func TestTruthfulExitCodes_Precondition_ServerReleaseHistoryActiveStatusActivateCleanup(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		args []string
	}{
		{"history", []string{"server", "release", "history", "p", "r", "--server-root", dir}},
		{"active", []string{"server", "release", "active", "p", "--server-root", dir}},
		{"status", []string{"server", "release", "status", "p", "--server-root", dir}},
		{"activate", []string{"server", "release", "activate", "p", "r", "--server-root", dir}},
		{"cleanup", []string{"server", "release", "cleanup", "p", "r", "--server-root", dir}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, stderr, err := executeCommand(tc.args...)
			requireExitCode(t, err, output.ExitCodePrecondition)
			if !contains(stderr, "not initialized") {
				t.Errorf("stderr should report the uninitialized Runtime, got: %s", stderr)
			}
		})
	}
}

// TestTruthfulExitCodes_Precondition_InstallGateFirst verifies the §9.3
// evaluation order: the Runtime gate is the FIRST check on
// "server release install" and "deployment install", so the
// precondition category (4) is never masked by a missing artifact.
func TestTruthfulExitCodes_Precondition_InstallGateFirst(t *testing.T) {
	dir := t.TempDir()
	missingArtifact := filepath.Join(dir, "does-not-exist.tar.gz")

	t.Run("server release install", func(t *testing.T) {
		_, _, stderr, err := executeCommand("server", "release", "install", "p", missingArtifact, "--server-root", dir)
		requireExitCode(t, err, output.ExitCodePrecondition)
		if !contains(stderr, "not initialized") {
			t.Errorf("stderr should report the uninitialized Runtime, got: %s", stderr)
		}
	})

	t.Run("deployment install", func(t *testing.T) {
		_, _, stderr, err := executeCommand("deployment", "install", missingArtifact, "--server-root", dir)
		requireExitCode(t, err, output.ExitCodePrecondition)
		if !contains(stderr, "not initialized") {
			t.Errorf("stderr should report the uninitialized Runtime, got: %s", stderr)
		}
	})
}

// ── Precondition (4) — D-02: declared framework without installed standard ──

// TestTruthfulExitCodes_Precondition_StandardMissingConfigFamily verifies
// that a declared framework without an installed standard exits 4 on the
// config family (D-02): the installed standard is a required
// prerequisite of the declaration.
func TestTruthfulExitCodes_Precondition_StandardMissingConfigFamily(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	configContent := "project:\n  name: std-missing-test\n  framework: laravel\n"
	if err := os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"config get", []string{"config", "get", "project.name"}},
		{"config list", []string{"config", "list"}},
		{"config levels", []string{"config", "levels"}},
		{"config validate", []string{"config", "validate"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, stderr, err := executeCommand(tc.args...)
			requireExitCode(t, err, output.ExitCodePrecondition)
			if !contains(stderr, "not installed") {
				t.Errorf("stderr should report the missing standard, got: %s", stderr)
			}
		})
	}
}

// TestTruthfulExitCodes_Precondition_DeploymentUploadMissingCredentials
// verifies that missing SSH credentials on "deployment upload" exit 4
// (precondition, D-07): a required environment prerequisite is missing.
func TestTruthfulExitCodes_Precondition_DeploymentUploadMissingCredentials(t *testing.T) {
	for _, name := range []string{"DEPLOY_SERVER_HOST", "DEPLOY_SERVER_USER", "DEPLOY_SSH_KEY"} {
		t.Setenv(name, "")
	}
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	_, _, stderr, err := executeCommand("deployment", "upload", "target", artifactPath)
	requireExitCode(t, err, output.ExitCodePrecondition)
	if !contains(stderr, "credentials") {
		t.Errorf("stderr should report the missing SSH credentials, got: %s", stderr)
	}
}

// ── Runtime not-found (3) — F-02 ──────────────────────────────────────

// TestTruthfulExitCodes_Runtime_ReleaseLookupsNotFound verifies that
// release lookups that come up empty exit 3 (runtime not-found) on
// history/status/cleanup, and that an unregistered project lookup exits
// 3 on history (project not found in the registry).
func TestTruthfulExitCodes_Runtime_ReleaseLookupsNotFound(t *testing.T) {
	serverRoot, _, projectID := setupInstallEnvironment(t)

	t.Run("history release not found", func(t *testing.T) {
		_, _, stderr, err := executeCommand("server", "release", "history", projectID, "rel-nope", "--server-root", serverRoot)
		requireExitCode(t, err, output.ExitCodeRuntime)
		if !contains(stderr, "not found") {
			t.Errorf("stderr should report the missing release, got: %s", stderr)
		}
	})

	t.Run("history project not registered", func(t *testing.T) {
		_, _, stderr, err := executeCommand("server", "release", "history", "unknown-project", "rel-nope", "--server-root", serverRoot)
		requireExitCode(t, err, output.ExitCodeRuntime)
		if !contains(stderr, "not found") {
			t.Errorf("stderr should report the missing project, got: %s", stderr)
		}
	})

	t.Run("status release not found", func(t *testing.T) {
		_, _, stderr, err := executeCommand("server", "release", "status", projectID, "rel-nope", "--server-root", serverRoot)
		requireExitCode(t, err, output.ExitCodeRuntime)
		if !contains(stderr, "not found") {
			t.Errorf("stderr should report the missing release, got: %s", stderr)
		}
	})

	t.Run("cleanup release dir not found", func(t *testing.T) {
		_, _, stderr, err := executeCommand("server", "release", "cleanup", projectID, "rel-nope", "--server-root", serverRoot, "--force")
		requireExitCode(t, err, output.ExitCodeRuntime)
		if !contains(stderr, "not found") {
			t.Errorf("stderr should report the missing release directory, got: %s", stderr)
		}
	})

	t.Run("cleanup project not registered", func(t *testing.T) {
		_, _, stderr, err := executeCommand("server", "release", "cleanup", "unknown-project", "rel-nope", "--server-root", serverRoot, "--force")
		requireExitCode(t, err, output.ExitCodeRuntime)
		if !contains(stderr, "not found") {
			t.Errorf("stderr should report the missing project, got: %s", stderr)
		}
	})
}

// ── Configuration (2) — F-03 / D-04 / D-06 ────────────────────────────

// TestTruthfulExitCodes_Config_InvalidConfigFamily verifies that an
// invalid configuration exits 2 on the config family (F-03): the config
// category is no longer reserved for config validate and register.
func TestTruthfulExitCodes_Config_InvalidConfigFamily(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	configContent := "project:\n  name: config-test\nrelease:\n  max_retained: not-an-integer\n"
	if err := os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"config get", []string{"config", "get", "release.max_retained"}},
		{"config list", []string{"config", "list"}},
		{"config levels", []string{"config", "levels"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := executeCommand(tc.args...)
			requireExitCode(t, err, output.ExitCodeConfig)
		})
	}
}

// TestTruthfulExitCodes_Config_MalformedConfigGet verifies that a
// malformed configuration source exits 2 on "config get" (D-04 —
// malformed is invalid per the global table).
func TestTruthfulExitCodes_Config_MalformedConfigGet(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte("project: [unclosed"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, _, _, runErr := executeCommand("config", "get", "project.name")
	requireExitCode(t, runErr, output.ExitCodeConfig)
}

// ── Demoted diagnostics / informational absence (0) — D-03 ────────────

// TestTruthfulExitCodes_InformationalAbsence verifies the D-03
// carve-out: informational commands report absent resources with 0.
func TestTruthfulExitCodes_InformationalAbsence(t *testing.T) {
	dir := t.TempDir()

	t.Run("server status uninitialized", func(t *testing.T) {
		_, _, _, err := executeCommand("server", "status", "--server-root", dir)
		if err != nil {
			t.Fatalf("server status must exit 0 even when uninitialized, got error: %v", err)
		}
	})

	t.Run("server doctor uninitialized", func(t *testing.T) {
		_, _, _, err := executeCommand("server", "doctor", "--server-root", dir)
		if err != nil {
			t.Fatalf("server doctor must exit 0 (demoted diagnostics), got error: %v", err)
		}
	})

	t.Run("release active no active release", func(t *testing.T) {
		serverRoot, _, projectID := setupInstallEnvironment(t)
		_, _, _, err := executeCommand("server", "release", "active", projectID, "--server-root", serverRoot)
		if err != nil {
			t.Fatalf("release active with no active release must exit 0 (informational absence), got error: %v", err)
		}
	})
}

// ── Readiness input-resolution failure (1) — D-08 ─────────────────────

// TestTruthfulExitCodes_ReadinessInputResolutionFailure verifies the
// documented D-08 row: "server readiness" exits 0 for findings but 1
// when its inputs (registry/release) cannot be resolved.
func TestTruthfulExitCodes_ReadinessInputResolutionFailure(t *testing.T) {
	dir := t.TempDir()
	_, _, stderr, err := executeCommand("server", "readiness", "unknown-project", "rel-nope", "--server-root", dir)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !contains(stderr, "registry") {
		t.Errorf("stderr should report the input-resolution failure, got: %s", stderr)
	}
}

// ── JSON error-envelope regressions (Security F-1) ────────────────────
//
// The --json failure path once returned the envelope AND a nil error —
// the process exited 0 while reporting failure (a fictional exit code).
// The bug class lived on the deprecated adapter surface only, so these
// regression tests pin the canonical-surface behavior: a failed command
// with --json writes the status:"error" envelope to stdout, still exits
// with its category code, and never echoes the plain "Error:" presentation
// on stdout (stderr only).

// TestTruthfulExitCodes_JSONErrorEnvelope_ServerReleaseHistory verifies
// that "server release history --json" against an uninitialized Runtime
// exits 4 (precondition) with the error envelope on stdout.
func TestTruthfulExitCodes_JSONErrorEnvelope_ServerReleaseHistory(t *testing.T) {
	dir := t.TempDir()

	_, stdout, stderr, err := executeCommand("server", "release", "history", "p", "r", "--server-root", dir, "--json")
	requireExitCode(t, err, output.ExitCodePrecondition)

	var envelope output.OutputEnvelope
	if jerr := json.Unmarshal([]byte(stdout), &envelope); jerr != nil {
		t.Fatalf("stdout is not a valid JSON error envelope: %v\n%s", jerr, stdout)
	}
	if envelope.Status != "error" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "error")
	}
	if !strings.Contains(envelope.Error, "not initialized") {
		t.Errorf("envelope error = %q, want the uninitialized-Runtime message", envelope.Error)
	}
	if contains(stdout, "Error:") {
		t.Errorf("plain error presentation must not be echoed on stdout, got: %s", stdout)
	}
	// Single-print contract (platform theme): the JSON envelope is the only
	// rendering for --json failures — stderr stays empty (the root-override
	// warning aside).
	if contains(stderr, "not initialized") {
		t.Errorf("plain error presentation must not be echoed on stderr for --json, got: %s", stderr)
	}
}

// TestTruthfulExitCodes_JSONEnvelope_StandardListMissingIndex_Degrades
// verifies that "standard list --json" with no registry index DEGRADES
// (ST-021-05): the read-only listing exits 0 with a truthful SUCCESS
// envelope carrying an empty array — nothing is offered from an
// unavailable index — and the human surface carries the setup hint. The
// degradation is a real success, never a hidden failure behind a
// fictional exit code (the regression class this section pins).
func TestTruthfulExitCodes_JSONEnvelope_StandardListMissingIndex_Degrades(t *testing.T) {
	// Isolate the global config dir so the default registry index is
	// deterministically absent.
	isolateGlobalConfigDir(t)

	_, stdout, stderr, err := executeCommand("standard", "list", "--json")
	if err != nil {
		t.Fatalf("standard list --json must exit 0 with a missing index (degraded), got error: %v (stderr: %s)", err, stderr)
	}

	var envelope output.OutputEnvelope
	if jerr := json.Unmarshal([]byte(stdout), &envelope); jerr != nil {
		t.Fatalf("stdout is not a valid JSON envelope: %v\n%s", jerr, stdout)
	}
	if envelope.Status != "success" {
		t.Errorf("envelope status = %q, want %q (the degraded listing is a success, not a failure)", envelope.Status, "success")
	}
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	if !strings.Contains(string(raw), "[]") {
		t.Errorf("degraded list JSON data should be an empty array, got: %s", raw)
	}
	if contains(stdout, "Error:") {
		t.Errorf("plain error presentation must not be echoed on stdout, got: %s", stdout)
	}
}

// TestTruthfulExitCodes_JSONErrorEnvelope_ConfigValidateMalformed
// verifies that "config validate --json" on a malformed configuration
// exits 2 (config category, D-04) with the error envelope on stdout.
// The validation-errors path is excluded here by design: it emits the
// categorized validation RESULT envelope (status "success", data.valid
// false) — the error envelope belongs to the resolution/parse failures.
func TestTruthfulExitCodes_JSONErrorEnvelope_ConfigValidateMalformed(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte("project: [unclosed"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, stdout, stderr, runErr := executeCommand("config", "validate", "--json")
	requireExitCode(t, runErr, output.ExitCodeConfig)

	var envelope output.OutputEnvelope
	if jerr := json.Unmarshal([]byte(stdout), &envelope); jerr != nil {
		t.Fatalf("stdout is not a valid JSON error envelope: %v\n%s", jerr, stdout)
	}
	if envelope.Status != "error" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "error")
	}
	if contains(stdout, "Error:") {
		t.Errorf("plain error presentation must not be echoed on stdout, got: %s", stdout)
	}
	// Single-print contract (platform theme): the JSON envelope is the only
	// rendering for --json failures — stderr stays empty.
	if stderr != "" {
		t.Errorf("stderr should be empty for --json error envelope, got: %s", stderr)
	}
}

// ── Missing project context stays general (1) — D-01 ──────────────────

// TestTruthfulExitCodes_MissingProjectContextStaysGeneral verifies the
// D-01 carve-out: commands requiring a project context exit 1 when no
// project is found — the absence is a context error, not invalid
// configuration.
func TestTruthfulExitCodes_MissingProjectContextStaysGeneral(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"status", []string{"status"}},
		{"project status", []string{"project", "status"}},
		{"config get", []string{"config", "get", "project.name"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := executeCommand(tc.args...)
			requireExitCode(t, err, output.ExitCodeGeneral)
		})
	}
}
