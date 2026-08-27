package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maleolabs.com/anvil/internal/registry"
)

// writeProjectConfig writes an anvil.yaml with the given content into dir
// and changes the process working directory to dir for the duration of
// the test, restoring it on cleanup (the standard config-command test
// pattern).
func writeProjectConfig(t *testing.T, dir, configContent string) {
	t.Helper()
	configPath := filepath.Join(dir, "anvil.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("failed to restore original directory %q: %v", orig, err)
		}
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to project directory %q: %v", dir, err)
	}
}

// seedConfigExtensionStandard records an installed delivery lifecycle
// standard carrying configuration extension content (TS-015-03-02): the
// laravel namespace with a defaulted optional key, a required key with a
// default, and a required key without a default — the same shape
// exercised by the registry and engine tests.
func seedConfigExtensionStandard(t *testing.T) {
	t.Helper()
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("installed-standards dir: %v", err)
	}
	now := time.Now().UTC()
	rec := registry.InstalledStandardRecord{
		FormatVersion:   registry.RecordFormatVersion,
		ID:              "anvil-standard-laravel",
		Version:         "1.2.3",
		ContractVersion: "1.0.0",
		Resolution:      registry.Resolution{Kind: registry.ResolutionKindIndex, Source: "/registry"},
		InstalledAt:     now,
		UpdatedAt:       now,
		Lifecycle:       registry.Lifecycle{State: registry.LifecycleStatePublished},
		ConfigExtension: &registry.ConfigExtensionContent{
			Namespace: "laravel",
			Keys: []registry.ConfigExtensionKey{
				{Name: "version", Description: "Laravel version.", Default: "11.0.0"},
				{Name: "cache.store", Description: "Cache store.", Default: "redis", Required: true},
				{Name: "build_args", Description: "Extra build args.", Required: true},
			},
		},
	}
	if _, _, err := registry.NewInstalledStandardStore(dir).Record(rec.ID, rec); err != nil {
		t.Fatalf("record installed standard: %v", err)
	}
}

// seedStandardWithoutContent records an installed delivery lifecycle
// standard that declares no configuration extension content (a standard
// may declare nothing in a category — command-contract §4.1).
func seedStandardWithoutContent(t *testing.T) {
	t.Helper()
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("installed-standards dir: %v", err)
	}
	now := time.Now().UTC()
	rec := registry.InstalledStandardRecord{
		FormatVersion:   registry.RecordFormatVersion,
		ID:              "anvil-standard-laravel",
		Version:         "1.2.3",
		ContractVersion: "1.0.0",
		Resolution:      registry.Resolution{Kind: registry.ResolutionKindIndex, Source: "/registry"},
		InstalledAt:     now,
		UpdatedAt:       now,
		Lifecycle:       registry.Lifecycle{State: registry.LifecycleStatePublished},
	}
	if _, _, err := registry.NewInstalledStandardStore(dir).Record(rec.ID, rec); err != nil {
		t.Fatalf("record installed standard: %v", err)
	}
}

// TestConfigValidateCommand_FrameworkConfigValid verifies the
// standard-driven framework validation success case (TS-015-03-02 DoD:
// framework config is validated against the standard's rules): a project
// declaring the laravel framework whose framework section satisfies the
// installed standard's declared rules validates with exit 0.
func TestConfigValidateCommand_FrameworkConfigValid(t *testing.T) {
	isolateConfigEnvironment(t)
	seedConfigExtensionStandard(t)
	writeProjectConfig(t, t.TempDir(), `project:
  name: framework-valid
  framework: laravel
framework:
  laravel:
    version: 11.0.0
    cache.store: redis
    build_args: --no-dev
`)

	_, stdout, stderr, err := executeCommand("config", "validate")
	if err != nil {
		t.Fatalf("config validate returned unexpected error for valid framework config: %v\nstderr: %s", err, stderr)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "Configuration is valid") {
		t.Errorf("stdout should contain the success result, got: %s", stdout)
	}
}

// TestConfigValidateCommand_FrameworkConfigInvalid verifies that
// violations of the installed standard's declared rules are reported
// (TS-015-03-02 DoD: validation errors identify the offending key and the
// expected format): a missing required key and a non-string value exit
// non-zero with the fully-qualified framework keys and the expected
// formats, sourced from the standard's declaration — never from runtime
// knowledge.
func TestConfigValidateCommand_FrameworkConfigInvalid(t *testing.T) {
	isolateConfigEnvironment(t)
	seedConfigExtensionStandard(t)
	writeProjectConfig(t, t.TempDir(), `project:
  name: framework-invalid
  framework: laravel
framework:
  laravel:
    version: 11
    cache.store: redis
`)

	_, stdout, stderr, err := executeCommand("config", "validate")
	if err == nil {
		t.Fatal("expected non-zero exit for invalid framework config, got nil")
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for invalid framework config, got: %s", stdout)
	}
	if !contains(stderr, "configuration is invalid") {
		t.Errorf("stderr should state the configuration is invalid, got: %s", stderr)
	}
	// The offending key is the fully-qualified framework key (ADR-005 §4.4).
	if !contains(stderr, "framework.laravel.version") {
		t.Errorf("stderr should identify the offending key framework.laravel.version, got: %s", stderr)
	}
	if !contains(stderr, "framework.laravel.build_args") {
		t.Errorf("stderr should identify the offending key framework.laravel.build_args, got: %s", stderr)
	}
	// The expected format comes from the standard's rules: string-only
	// shape and required declaration.
	if !contains(stderr, "string") {
		t.Errorf("stderr should describe the expected string format, got: %s", stderr)
	}
	if !contains(stderr, "required") {
		t.Errorf("stderr should describe the expected required value, got: %s", stderr)
	}
	// Framework violations group under the standard presentation
	// categories (type for the value shape, required for the missing key).
	if !contains(stderr, "type:") {
		t.Errorf("stderr should contain the 'type' error category, got: %s", stderr)
	}
	if !contains(stderr, "required:") {
		t.Errorf("stderr should contain the 'required' error category, got: %s", stderr)
	}
}

// TestConfigValidateCommand_FrameworkConfigInvalidJSON verifies the
// machine-readable result for framework validation violations: the
// offending fully-qualified keys and their expected formats appear as
// structured records in the categorized JSON output.
func TestConfigValidateCommand_FrameworkConfigInvalidJSON(t *testing.T) {
	isolateConfigEnvironment(t)
	seedConfigExtensionStandard(t)
	writeProjectConfig(t, t.TempDir(), `project:
  name: framework-invalid
  framework: laravel
framework:
  laravel:
    version: 11
    cache.store: redis
`)

	_, stdout, _, err := executeCommand("config", "validate", "--json")
	if err == nil {
		t.Fatal("expected non-zero exit for invalid framework config, got nil")
	}

	result, err := parseConfigValidationJSON(t, stdout)
	if err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if result.Valid {
		t.Error("JSON result should report valid=false")
	}
	if result.ErrorCount != 2 {
		t.Errorf("JSON result should report error_count=2, got %d", result.ErrorCount)
	}
	if !hasErrorRecord(result.Errors["type"], "framework.laravel.version") {
		t.Error("JSON 'type' category should identify framework.laravel.version")
	}
	if !hasErrorRecord(result.Errors["required"], "framework.laravel.build_args") {
		t.Error("JSON 'required' category should identify framework.laravel.build_args")
	}
}

// TestConfigValidateCommand_FrameworkStandardWithoutContent verifies the
// no-content outcome (command-contract §4.1): a resolved standard that
// declares no config extension content supplies no rules — the framework
// section passes through and the configuration validates (the
// missing-extension hand-off of TS-015-03-01).
func TestConfigValidateCommand_FrameworkStandardWithoutContent(t *testing.T) {
	isolateConfigEnvironment(t)
	seedStandardWithoutContent(t)
	writeProjectConfig(t, t.TempDir(), `project:
  name: framework-no-content
  framework: laravel
framework:
  laravel:
    anything: at-all
`)

	_, stdout, stderr, err := executeCommand("config", "validate")
	if err != nil {
		t.Fatalf("config validate returned unexpected error for a standard without content: %v\nstderr: %s", err, stderr)
	}
	if !contains(stdout, "Configuration is valid") {
		t.Errorf("stdout should contain the success result, got: %s", stdout)
	}
}

// TestConfigValidateCommand_FrameworkStandardNotInstalled verifies the
// standard-missing hard-fail at the config validation surface (ADR-026
// decision 3, the failure semantics of TS-015-02-02): a declared
// framework without an installed standard cannot be validated against the
// standard's rules — the command fails with an actionable remediation
// stating what is missing and how to resolve it, never a silent
// pass-through.
func TestConfigValidateCommand_FrameworkStandardNotInstalled(t *testing.T) {
	isolateConfigEnvironment(t)
	writeProjectConfig(t, t.TempDir(), `project:
  name: framework-missing-standard
  framework: laravel
framework:
  laravel:
    version: 11.0.0
`)

	_, stdout, stderr, err := executeCommand("config", "validate")
	if err == nil {
		t.Fatal("expected non-zero exit for a declared framework without an installed standard, got nil")
	}
	if stdout != "" {
		t.Errorf("expected empty stdout, got: %s", stdout)
	}
	if !contains(stderr, "not installed") {
		t.Errorf("stderr should state the standard is not installed, got: %s", stderr)
	}
	if !contains(stderr, "anvil-standard-laravel") {
		t.Errorf("stderr should identify the missing standard id, got: %s", stderr)
	}
	if !contains(stderr, "anvil standard install") {
		t.Errorf("stderr should provide the actionable remediation (anvil standard install), got: %s", stderr)
	}
}

// TestConfigValidateCommand_FrameworkStandardNotInstalledJSON verifies
// the standard-missing hard-fail JSON envelope: a resolution-level
// failure, not a validation result.
func TestConfigValidateCommand_FrameworkStandardNotInstalledJSON(t *testing.T) {
	isolateConfigEnvironment(t)
	writeProjectConfig(t, t.TempDir(), `project:
  name: framework-missing-standard
  framework: laravel
framework:
  laravel:
    version: 11.0.0
`)

	_, stdout, _, err := executeCommand("config", "validate", "--json")
	if err == nil {
		t.Fatal("expected non-zero exit for a declared framework without an installed standard, got nil")
	}
	if !contains(stdout, `"status":"error"`) && !strings.Contains(stdout, `"status": "error"`) {
		t.Errorf("stdout should be an error envelope, got: %s", stdout)
	}
	if !contains(stdout, "not installed") {
		t.Errorf("error envelope should state the standard is not installed, got: %s", stdout)
	}
}

// TestConfigValidateCommand_NoFrameworkUnaffected verifies that projects
// WITHOUT a framework declaration remain fully functional (TS-015-03-02
// DoD, ADR-026 §12.2 — non-breaking): config validate succeeds even when
// the installed-standard store is empty — nothing about a framework-free
// project reads the registry.
func TestConfigValidateCommand_NoFrameworkUnaffected(t *testing.T) {
	isolateConfigEnvironment(t)
	writeProjectConfig(t, t.TempDir(), `project:
  name: plain-app
  version: 2.1.0
`)

	_, stdout, stderr, err := executeCommand("config", "validate")
	if err != nil {
		t.Fatalf("config validate must remain fully functional without a framework declaration: %v\nstderr: %s", err, stderr)
	}
	if !contains(stdout, "Configuration is valid") {
		t.Errorf("stdout should contain the success result, got: %s", stdout)
	}
}

// TestConfigListCommand_FrameworkConfigInvalid verifies the load-path
// enforcement (TS-015-03-02): the config family exercises the same
// validation as 'config validate' — a framework section violating the
// installed standard's declared rules fails 'anvil config list' with the
// offending key, so implicit (load-time) and explicit validation never
// diverge.
func TestConfigListCommand_FrameworkConfigInvalid(t *testing.T) {
	isolateConfigEnvironment(t)
	seedConfigExtensionStandard(t)
	writeProjectConfig(t, t.TempDir(), `project:
  name: framework-invalid
  framework: laravel
framework:
  laravel:
    version: 11
    cache.store: redis
`)

	_, _, stderr, err := executeCommand("config", "list")
	if err == nil {
		t.Fatal("expected non-zero exit for invalid framework config, got nil")
	}
	if !contains(stderr, "framework.laravel.version") {
		t.Errorf("stderr should identify the offending key framework.laravel.version, got: %s", stderr)
	}
	if !contains(stderr, "framework.laravel.build_args") {
		t.Errorf("stderr should identify the offending key framework.laravel.build_args, got: %s", stderr)
	}
}

// TestConfigListCommand_NoFrameworkUnaffected verifies the load-path
// non-breaking guarantee: 'anvil config list' on a project without a
// framework declaration succeeds with an empty store.
func TestConfigListCommand_NoFrameworkUnaffected(t *testing.T) {
	isolateConfigEnvironment(t)
	writeProjectConfig(t, t.TempDir(), `project:
  name: plain-app
`)

	_, stdout, stderr, err := executeCommand("config", "list")
	if err != nil {
		t.Fatalf("config list must remain fully functional without a framework declaration: %v\nstderr: %s", err, stderr)
	}
	if !contains(stdout, "project.name") {
		t.Errorf("stdout should list resolved keys, got: %s", stdout)
	}
}
