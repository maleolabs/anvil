package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/project"
)

// TestStatus_JSONEnvelopeContract verifies anvil status --json emits envelope v1 success (AC2).
func TestStatus_JSONEnvelopeContract(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, project.ConfigFileName)
	configContent := `project:
  name: status-json-test
  version: 1.0.0
  description: test
artifact:
  include: ["**/*"]
  exclude: [".git/**"]
  output: .anvil/artifacts
  manifest: true
release:
  max_retained: 5
  retention_policy: keep-last
  auto_verify: true
  version_schema: semver
runtime:
  install_root: .anvil/releases
  shared_resources: .anvil/shared
  active_symlink: .anvil/active
  temp_dir: .anvil/tmp
global:
  log_level: info
  output_format: human
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	_, stdout, stderr, err := executeCommand("status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v\nstdout:%s\nstderr:%s", err, stdout, stderr)
	}
	if stderr != "" && !contains(stderr, "Warning") {
		t.Errorf("unexpected stderr: %s", stderr)
	}
	var env output.OutputEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("parse envelope: %v\nstdout:%s", err, stdout)
	}
	if env.Version != "1" {
		t.Errorf("version=%q want 1", env.Version)
	}
	if env.Status != "success" {
		t.Errorf("status=%q want success", env.Status)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("parse raw: %v", err)
	}
	var inner map[string]interface{}
	if err := json.Unmarshal(raw["data"], &inner); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if _, ok := inner["project"]; !ok {
		t.Errorf("json data missing project")
	}
	if _, ok := inner["lifecycle"]; !ok {
		t.Errorf("json data missing lifecycle")
	}
	if _, ok := inner["configuration"]; !ok {
		t.Errorf("json data missing configuration")
	}
}

// TestStatus_HumanShowsLifecycleAndConfiguration verifies human status shows lifecycle + configuration (observability AC3).
func TestStatus_HumanShowsLifecycleAndConfiguration(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, project.ConfigFileName)
	configContent := `project:
  name: status-human-test
  version: 2.0.0
artifact:
  include: ["**/*"]
  exclude: [".git/**"]
  output: .anvil/artifacts
  manifest: true
release:
  max_retained: 5
  retention_policy: keep-last
  auto_verify: true
  version_schema: semver
runtime:
  install_root: .anvil/releases
  shared_resources: .anvil/shared
  active_symlink: .anvil/active
  temp_dir: .anvil/tmp
global:
  log_level: info
  output_format: human
  no_color: false
  auto_progress: true
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	_, stdout, _, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status failed: %v stdout:%s", err, stdout)
	}
	for _, needle := range []string{"Project", "Name", "Version", "Lifecycle", "Configuration valid"} {
		if !contains(stdout, needle) {
			t.Errorf("human missing %q in %q", needle, stdout)
		}
	}
}

// TestDeploy_HumanShowsVerifyPerCheckAndPushTicks verifies deploy human shows verify per-check PASS/FAIL and push ticks when applicable (AC1).
// For dry-run, only verify per-check is asserted; push ticks are verified via output.EmitPushProgress unit test.
func TestDeploy_Observability_VerifyPerCheck(t *testing.T) {
	dir := t.TempDir()
	_, _, _, err := executeCommand("init", "observability-verify-test", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	_, stdout, _, err := executeCommand("deploy", "--target", "staging", "--dry-run")
	if err != nil {
		t.Fatalf("deploy dry-run failed: %v stdout:%s", err, stdout)
	}
	// verify per-check PASS/FAIL visible via PrintStatus
	if !contains(stdout, "PASS") {
		t.Errorf("deploy human missing PASS per-check in %q", stdout)
	}
	if !contains(stdout, "Verify") {
		t.Errorf("deploy human missing Verify step in %q", stdout)
	}
	// also should show artifact_id / version / checksum
	if !contains(stdout, "artifact_id") {
		t.Errorf("deploy human missing artifact_id in %q", stdout)
	}
	// push ticks are not expected in dry-run, but verify ticks helper ensures 0→100 coverage
}
