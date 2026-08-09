// Package cmd implements the Anvil CLI commands.
//
// Tests for "anvil adapter uninstall" (TS-007-037): identifier-safety
// validation (no accidental file removal — the known-framework whitelist
// is gone, ADR-026), removal of an installed binary, the graceful
// not-installed path (exit 0), and --json output validity. No network is
// involved; the install-directory seam points at t.TempDir().
//
// Reference: TS-007-037, ADR-026
package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// TestAdapterUninstall_InvalidName verifies that an unsafe adapter name
// is rejected before any filesystem activity — only safe
// "anvil-adapter-*" identifiers can be removed. The former
// known-framework whitelist (laravel, flutter) is gone (ADR-026);
// identifier safety is the only name gate left.
//
// Reference: TS-007-037 AC-3, §3, §7, ADR-026
func TestAdapterUninstall_InvalidName(t *testing.T) {
	stubAdapterInstallDirAt(t, t.TempDir())
	isolateGlobalConfigDir(t)

	_, _, stderr, err := executeCommand("adapter", "uninstall", "../evil")
	if err == nil {
		t.Fatal("expected error for unsafe adapter name, got nil")
	}
	if !strings.Contains(stderr, "invalid adapter name") {
		t.Errorf("stderr should reject the unsafe name, got: %s", stderr)
	}
}

// TestAdapterUninstall_UncatalogedNameNotInstalledGraceful verifies that
// a valid identifier the Core never cataloged (e.g. "node") is not
// rejected by a known-set gate: with no installed binary the command
// reports "not installed" and exits 0 (ADR-026 — the runtime carries no
// framework knowledge).
//
// Reference: TS-007-037 AC-5, ADR-026
func TestAdapterUninstall_UncatalogedNameNotInstalledGraceful(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	isolateGlobalConfigDir(t)

	_, stdout, stderr, err := executeCommand("adapter", "uninstall", "node")
	if err != nil {
		t.Fatalf("adapter uninstall returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "is not installed") {
		t.Errorf("stdout should report the not-installed state, got:\n%s", stdout)
	}
}

// TestAdapterUninstall_UncatalogedNameRemovesBinary verifies that a
// valid identifier the Core never cataloged is removed when its binary
// exists — the command operates on installed state, not on a
// runtime-known set (ADR-026).
//
// Reference: TS-007-037 AC-4, ADR-026
func TestAdapterUninstall_UncatalogedNameRemovesBinary(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	isolateGlobalConfigDir(t)
	writeTestFile(t, dir, "anvil-adapter-node", "binary")

	_, stdout, stderr, err := executeCommand("adapter", "uninstall", "node")
	if err != nil {
		t.Fatalf("adapter uninstall returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	targetPath := filepath.Join(dir, "anvil-adapter-node")
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Errorf("adapter binary still exists after uninstall (stat err: %v)", err)
	}
	if !strings.Contains(stdout, "Adapter node uninstalled") {
		t.Errorf("stdout should confirm removal, got:\n%s", stdout)
	}
}

// TestAdapterUninstall_RemovesInstalledBinary verifies that uninstalling
// a present adapter removes the binary from the CLI directory.
//
// Reference: TS-007-037 AC-4
func TestAdapterUninstall_RemovesInstalledBinary(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	isolateGlobalConfigDir(t)
	writeTestFile(t, dir, "anvil-adapter-laravel", "binary")

	_, stdout, stderr, err := executeCommand("adapter", "uninstall", "laravel")
	if err != nil {
		t.Fatalf("adapter uninstall returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Errorf("adapter binary still exists after uninstall (stat err: %v)", err)
	}
	if !strings.Contains(stdout, "Adapter laravel uninstalled") {
		t.Errorf("stdout should confirm removal, got:\n%s", stdout)
	}
}

// TestAdapterUninstall_NotInstalledGraceful verifies that uninstalling an
// absent adapter reports an informative message and exits 0 (non-fatal).
//
// Reference: TS-007-037 AC-5
func TestAdapterUninstall_NotInstalledGraceful(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	isolateGlobalConfigDir(t)

	_, stdout, stderr, err := executeCommand("adapter", "uninstall", "laravel")
	if err != nil {
		t.Fatalf("adapter uninstall returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "is not installed") {
		t.Errorf("stdout should report the not-installed state, got:\n%s", stdout)
	}
}

// TestAdapterUninstall_NotInstalledJSON verifies the JSON shape for the
// graceful not-installed path (success envelope, exit 0).
//
// Reference: TS-007-037 AC-5, AC-6
func TestAdapterUninstall_NotInstalledJSON(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	isolateGlobalConfigDir(t)

	_, stdout, stderr, err := executeCommand("adapter", "uninstall", "flutter", "--json")
	if err != nil {
		t.Fatalf("adapter uninstall --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal(jsonEnvelopeFromStdout(t, stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if envelope.Status != "success" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "success")
	}

	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var result adapterBinaryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("envelope data is not an uninstall result: %v\n%s", err, raw)
	}
	if result.Adapter != "flutter" {
		t.Errorf("result.adapter = %q, want %q", result.Adapter, "flutter")
	}
	if result.Status != "not installed" {
		t.Errorf("result.status = %q, want %q", result.Status, "not installed")
	}
	if result.Path != filepath.Join(dir, "anvil-adapter-flutter") {
		t.Errorf("result.path = %q, want %q", result.Path, filepath.Join(dir, "anvil-adapter-flutter"))
	}
}

// TestAdapterUninstall_RemovedJSON verifies the JSON shape after a real
// removal.
//
// Reference: TS-007-037 AC-4, AC-6
func TestAdapterUninstall_RemovedJSON(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	isolateGlobalConfigDir(t)
	writeTestFile(t, dir, "anvil-adapter-laravel", "binary")

	_, stdout, stderr, err := executeCommand("adapter", "uninstall", "laravel", "--json")
	if err != nil {
		t.Fatalf("adapter uninstall --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal(jsonEnvelopeFromStdout(t, stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var result adapterBinaryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("envelope data is not an uninstall result: %v\n%s", err, raw)
	}
	if result.Status != "uninstalled" {
		t.Errorf("result.status = %q, want %q", result.Status, "uninstalled")
	}
	if result.Message == "" {
		t.Error("result.message should not be empty")
	}
}

// TestAdapterUninstall_InvalidNameJSON verifies the error envelope for an
// unsafe adapter name with --json (errors are conveyed through the
// machine-readable envelope, TS-P8-05) AND that the process still exits
// non-zero — a failure must never exit 0 (TS-019-03-02).
//
// Reference: TS-007-037 AC-3, AC-6, ADR-026
func TestAdapterUninstall_InvalidNameJSON(t *testing.T) {
	stubAdapterInstallDirAt(t, t.TempDir())
	isolateGlobalConfigDir(t)

	_, stdout, _, err := executeCommand("adapter", "uninstall", "../evil", "--json")
	if err == nil {
		t.Fatal("adapter uninstall --json should return an error for an invalid adapter name (exit non-zero), got nil")
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal(jsonEnvelopeFromStdout(t, stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if envelope.Status != "error" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "error")
	}
}

// ── Record Removal (TS-017-02-02, team review F4) ────────────────────

// TestAdapterUninstall_RemovesRecordAndBinary verifies the post-gate
// uninstall contract (team review F4): "installed" is the registry
// record, so uninstall removes BOTH the installed-standard record and
// the binary — the adapter disappears from the registry-driven installed
// view (adapter list) entirely.
//
// Reference: TS-017-02-02, TS-007-037
func TestAdapterUninstall_RemovesRecordAndBinary(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	isolateGlobalConfigDir(t)
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")
	writeTestFile(t, dir, "anvil-adapter-laravel", "binary")

	_, stdout, stderr, err := executeCommand("adapter", "uninstall", "laravel")
	if err != nil {
		t.Fatalf("adapter uninstall returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	// Binary removed.
	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Errorf("adapter binary still exists after uninstall (stat err: %v)", err)
	}
	// Record removed — the registry-driven installed view is empty.
	storeDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("installed-standards dir: %v", err)
	}
	if _, err := registry.NewInstalledStandardStore(storeDir).Get("anvil-standard-laravel"); !errors.Is(err, registry.ErrRecordNotFound) {
		t.Errorf("installed-standard record should be gone after uninstall (err: %v)", err)
	}
	if !strings.Contains(stdout, "record removed") {
		t.Errorf("stdout should confirm the record removal, got:\n%s", stdout)
	}

	// The registry-driven installed view no longer lists the adapter.
	_, stdoutList, stderrList, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderrList)
	}
	if strings.Contains(stdoutList, "laravel") {
		t.Errorf("adapter list should not list an uninstalled adapter, got:\n%s", stdoutList)
	}
}

// TestAdapterUninstall_RecordOnlyRemoved verifies that uninstalling a
// recorded adapter whose binary is already gone still removes the
// record — post-gate the record is the installed truth, so a dangling
// record must not survive uninstall.
//
// Reference: TS-017-02-02, team review F4
func TestAdapterUninstall_RecordOnlyRemoved(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	isolateGlobalConfigDir(t)
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")

	_, stdout, stderr, err := executeCommand("adapter", "uninstall", "laravel")
	if err != nil {
		t.Fatalf("adapter uninstall returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "uninstalled") || strings.Contains(stdout, "is not installed") {
		t.Errorf("record-only uninstall should report removal, got:\n%s", stdout)
	}
	storeDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("installed-standards dir: %v", err)
	}
	if _, err := registry.NewInstalledStandardStore(storeDir).Get("anvil-standard-laravel"); !errors.Is(err, registry.ErrRecordNotFound) {
		t.Errorf("installed-standard record should be gone after uninstall (err: %v)", err)
	}
}
