// Package cmd implements the Anvil CLI commands.
//
// Tests for the adapter command group (TS-007-031, TS-007-032): the
// parent-only group registration, "anvil adapter list", and "anvil
// adapter inspect".
//
// The adapter subcommands invoke adapter executables through the command
// contract. Tests stub the adapterExecutableLookup seam to point at fake
// adapter shell scripts in t.TempDir() (the real Process Runner executes
// them), so no adapter binary needs to be on PATH. The
// adapterKnownFrameworks seam is stubbed to control the known framework
// set deterministically.
//
// Reference: TS-007-031, TS-007-032
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/output"
)

// stubAdapterScript returns a shell script that implements the adapter
// command contract for testing: it answers the capabilities and
// extension commands by printing the provided JSON documents to stdout
// and exits 0. capsFile and extFile are absolute paths to the JSON
// fixtures the script cats.
func stubAdapterScript(capsFile, extFile string) string {
	return fmt.Sprintf(`#!/bin/sh
# Stub adapter for CLI tests. Answers capabilities/extension from files.
case "$1" in
  capabilities) cat "%s" ;;
  extension) cat "%s" ;;
  *) echo "unknown command $1" >&2; exit 2 ;;
esac
exit 0
`, capsFile, extFile)
}

// writeFakeAdapter writes an executable stub adapter named name into dir
// that answers the capabilities/extension commands with the provided JSON
// documents. It returns the absolute path to the stub.
func writeFakeAdapter(t *testing.T, dir, name, capabilitiesJSON, extensionJSON string) string {
	t.Helper()
	path := filepath.Join(dir, name)

	// The stub cats per-command JSON fixtures next to the script; the
	// script embeds their absolute paths so the working directory does
	// not matter.
	capsFile := filepath.Join(dir, name+".caps.json")
	extFile := filepath.Join(dir, name+".ext.json")
	if err := os.WriteFile(capsFile, []byte(capabilitiesJSON), 0644); err != nil {
		t.Fatalf("write capabilities fixture: %v", err)
	}
	if err := os.WriteFile(extFile, []byte(extensionJSON), 0644); err != nil {
		t.Fatalf("write extension fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte(stubAdapterScript(capsFile, extFile)), 0755); err != nil {
		t.Fatalf("write stub adapter %s: %v", name, err)
	}
	return path
}

// writeFailingAdapter writes an executable stub adapter that fails every
// invocation with a non-zero exit code.
func writeFailingAdapter(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\necho \"stub adapter failed on purpose\" >&2\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write failing stub adapter %s: %v", name, err)
	}
	return path
}

// stubAdapterLookup makes adapterExecutableLookup resolve every
// "anvil-adapter-<name>" to dir/<name> and registers cleanup.
func stubAdapterLookup(t *testing.T, dir string) {
	t.Helper()
	orig := adapterExecutableLookup
	adapterExecutableLookup = func(name string) (string, error) {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		return path, nil
	}
	t.Cleanup(func() { adapterExecutableLookup = orig })
}

// stubKnownFrameworks replaces adapterKnownFrameworks for the test and
// registers cleanup.
func stubKnownFrameworks(t *testing.T, frameworks []string) {
	t.Helper()
	orig := adapterKnownFrameworks
	adapterKnownFrameworks = func() []string { return frameworks }
	t.Cleanup(func() { adapterKnownFrameworks = orig })
}

// ── Command Group Registration ───────────────────────────────────────

// TestAdapterCommand_RegistersSubcommands verifies that the adapter group
// is a parent-only namespace (ADR-010 §6.7) with the list, inspect, use,
// install, and uninstall subcommands registered.
//
// Reference: TS-007-031, TS-007-032, TS-007-033, TS-007-037
func TestAdapterCommand_RegistersSubcommands(t *testing.T) {
	group, _, err := rootCmd.Find([]string{"adapter"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"adapter\"]) returned error: %v", err)
	}
	if group == nil {
		t.Fatal("adapter command group not found")
	}

	// Parent-only: no RunE, Run, or Args (ADR-010 §6.7).
	if group.RunE != nil {
		t.Error("adapter group has RunE set; parent groups should not have execution logic")
	}
	if group.Run != nil {
		t.Error("adapter group has Run set; parent groups should not have execution logic")
	}
	if group.Args != nil {
		t.Error("adapter group has custom Args validator; parent groups should not")
	}

	registered := make(map[string]bool)
	for _, sub := range group.Commands() {
		registered[sub.Name()] = true
	}
	for _, want := range []string{"list", "inspect", "use", "install", "uninstall"} {
		if !registered[want] {
			t.Errorf("adapter subcommand %q not registered", want)
		}
	}
}

// ── Adapter List (TS-007-031) ────────────────────────────────────────

// TestAdapterList_TableOutput verifies that "anvil adapter list" renders
// each known adapter with its deployment model.
//
// Reference: TS-007-031 AC-1, AC-2
func TestAdapterList_TableOutput(t *testing.T) {
	dir := t.TempDir()
	stubKnownFrameworks(t, []string{"flutter", "laravel"})
	stubAdapterLookup(t, dir)
	writeFakeAdapter(t, dir, "anvil-adapter-laravel",
		`{"capabilities":{"deployment_model":"server"}}`,
		`{"extension":{"framework":"laravel","keys":[]}}`)
	writeFakeAdapter(t, dir, "anvil-adapter-flutter",
		`{"capabilities":{"deployment_model":"hybrid"}}`,
		`{"extension":{"framework":"flutter","keys":[]}}`)

	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	for _, want := range []string{"laravel", "server", "flutter", "hybrid", "Name", "Deployment Model", "Version"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout should contain %q, got:\n%s", want, stdout)
		}
	}
}

// TestAdapterList_NotInstalledMarker verifies that known frameworks whose
// executable does not resolve are listed with a "not installed" marker
// instead of failing the command.
//
// Reference: TS-007-031 AC-1
func TestAdapterList_NotInstalledMarker(t *testing.T) {
	stubKnownFrameworks(t, []string{"laravel", "flutter"})
	// No adapter executables exist anywhere; lookup always fails.
	orig := adapterExecutableLookup
	adapterExecutableLookup = func(name string) (string, error) {
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { adapterExecutableLookup = orig })

	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	if !strings.Contains(stdout, "not installed") {
		t.Errorf("stdout should contain the 'not installed' marker, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "laravel") || !strings.Contains(stdout, "flutter") {
		t.Errorf("stdout should list both known frameworks, got:\n%s", stdout)
	}
}

// TestAdapterList_CapabilitiesFailureShowsUnknown verifies that a
// resolvable adapter whose capabilities request fails is shown with an
// "unknown" state instead of failing the whole list.
//
// Reference: TS-007-031 AC-1
func TestAdapterList_CapabilitiesFailureShowsUnknown(t *testing.T) {
	dir := t.TempDir()
	stubKnownFrameworks(t, []string{"laravel", "flutter"})
	stubAdapterLookup(t, dir)
	writeFailingAdapter(t, dir, "anvil-adapter-laravel")
	writeFakeAdapter(t, dir, "anvil-adapter-flutter",
		`{"capabilities":{"deployment_model":"hybrid"}}`,
		`{"extension":{"framework":"flutter","keys":[]}}`)

	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	if !strings.Contains(stdout, "unknown") {
		t.Errorf("stdout should show the failed adapter as 'unknown', got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "hybrid") {
		t.Errorf("stdout should still show the healthy adapter, got:\n%s", stdout)
	}
}

// TestAdapterList_EmptyMessage verifies the empty state: when no known
// frameworks exist, the command prints an appropriate message and exits 0.
//
// Reference: TS-007-031 AC-4
func TestAdapterList_EmptyMessage(t *testing.T) {
	stubKnownFrameworks(t, nil)

	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "No adapters available.") {
		t.Errorf("stdout should contain the empty message, got:\n%s", stdout)
	}
}

// TestAdapterList_JSON verifies the --json output: an array of
// {name, deployment_model, version} entries under the standard envelope.
//
// Reference: TS-007-031 AC-3
func TestAdapterList_JSON(t *testing.T) {
	dir := t.TempDir()
	stubKnownFrameworks(t, []string{"laravel"})
	stubAdapterLookup(t, dir)
	writeFakeAdapter(t, dir, "anvil-adapter-laravel",
		`{"capabilities":{"deployment_model":"server"}}`,
		`{"extension":{"framework":"laravel","keys":[]}}`)

	_, stdout, stderr, err := executeCommand("adapter", "list", "--json")
	if err != nil {
		t.Fatalf("adapter list --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if envelope.Status != "success" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "success")
	}

	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var entries []adapterListEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("envelope data is not an adapter list: %v\n%s", err, raw)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %s", len(entries), raw)
	}
	entry := entries[0]
	if entry.Name != "laravel" {
		t.Errorf("entry name = %q, want %q", entry.Name, "laravel")
	}
	if entry.DeploymentModel != "server" {
		t.Errorf("entry deployment_model = %q, want %q", entry.DeploymentModel, "server")
	}
	if entry.Version != "-" {
		t.Errorf("entry version = %q, want %q (not declared by the command contract)", entry.Version, "-")
	}
}

// ── Adapter Inspect (TS-007-032) ─────────────────────────────────────

// TestAdapterInspect_UnknownAdapter verifies that an unknown adapter name
// produces a clear error naming the known adapters.
//
// Reference: TS-007-032 AC-4
func TestAdapterInspect_UnknownAdapter(t *testing.T) {
	stubKnownFrameworks(t, []string{"laravel", "flutter"})

	_, _, stderr, err := executeCommand("adapter", "inspect", "node")
	if err == nil {
		t.Fatal("expected error for unknown adapter, got nil")
	}
	if !strings.Contains(stderr, "unknown adapter") {
		t.Errorf("stderr should mention the unknown adapter, got: %s", stderr)
	}
	if !strings.Contains(stderr, "known adapters") {
		t.Errorf("stderr should list known adapters, got: %s", stderr)
	}
}

// TestAdapterInspect_ExecutableNotFound verifies that a known adapter
// whose executable is not on PATH produces a clear error naming the
// expected binary.
//
// Reference: TS-007-032 AC-4
func TestAdapterInspect_ExecutableNotFound(t *testing.T) {
	stubKnownFrameworks(t, []string{"laravel"})
	orig := adapterExecutableLookup
	adapterExecutableLookup = func(name string) (string, error) {
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { adapterExecutableLookup = orig })

	_, _, stderr, err := executeCommand("adapter", "inspect", "laravel")
	if err == nil {
		t.Fatal("expected error for missing executable, got nil")
	}
	if !strings.Contains(stderr, "no adapter found for framework") {
		t.Errorf("stderr should report no adapter found, got: %s", stderr)
	}
	if !strings.Contains(stderr, "anvil-adapter-laravel") {
		t.Errorf("stderr should name the expected binary, got: %s", stderr)
	}
}

// TestAdapterInspect_HumanOutput verifies that "anvil adapter inspect
// laravel" renders all capability sections for a server-model adapter.
//
// Reference: TS-007-032 AC-1, AC-2, AC-3
func TestAdapterInspect_HumanOutput(t *testing.T) {
	dir := t.TempDir()
	stubKnownFrameworks(t, []string{"laravel"})
	stubAdapterLookup(t, dir)
	writeFakeAdapter(t, dir, "anvil-adapter-laravel",
		`{"capabilities":{"deployment_model":"server","build_phases":["composer","npm"],"activation_phases":["migrate","config_cache"],"verification_checks":[{"name":"vendor_present","description":"validates vendor presence"}]}}`,
		`{"extension":{"framework":"laravel","keys":[{"name":"framework.laravel.migrations.path","description":"Relative path to migrations","default":"database/migrations"},{"name":"framework.laravel.php_version","description":"PHP version constraint"}]}}`)

	_, stdout, stderr, err := executeCommand("adapter", "inspect", "laravel")
	if err != nil {
		t.Fatalf("adapter inspect returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	for _, want := range []string{
		"Adapter: laravel",
		"Deployment Model:",
		"  server",
		"Build Phases:",
		"  composer",
		"  npm",
		"Activation Phases:",
		"  migrate",
		"  config_cache",
		"Verification Checks:",
		"  vendor_present",
		"validates vendor presence",
		"Config Keys:",
		"  framework.laravel.migrations.path",
		"default: database/migrations, required: false",
		"default: none, required: false",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout should contain %q, got:\n%s", want, stdout)
		}
	}
}

// TestAdapterInspect_HybridNoActivationPhases verifies that a hybrid-model
// adapter (flutter) renders "(none)" for the activation phases section
// instead of failing or producing empty output.
//
// Reference: TS-007-032 AC-2
func TestAdapterInspect_HybridNoActivationPhases(t *testing.T) {
	dir := t.TempDir()
	stubKnownFrameworks(t, []string{"flutter"})
	stubAdapterLookup(t, dir)
	writeFakeAdapter(t, dir, "anvil-adapter-flutter",
		`{"capabilities":{"deployment_model":"hybrid","build_phases":["build_web"]}}`,
		`{"extension":{"framework":"flutter","keys":[]}}`)

	_, stdout, stderr, err := executeCommand("adapter", "inspect", "flutter")
	if err != nil {
		t.Fatalf("adapter inspect returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	if !strings.Contains(stdout, "  hybrid") {
		t.Errorf("stdout should show the hybrid deployment model, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Activation Phases:") || !strings.Contains(stdout, "(none)") {
		t.Errorf("stdout should render activation phases as '(none)', got:\n%s", stdout)
	}
}

// TestAdapterInspect_JSON verifies the --json output carries the full
// capability declaration and configuration extension under the envelope.
//
// Reference: TS-007-032 AC-5
func TestAdapterInspect_JSON(t *testing.T) {
	dir := t.TempDir()
	stubKnownFrameworks(t, []string{"laravel"})
	stubAdapterLookup(t, dir)
	writeFakeAdapter(t, dir, "anvil-adapter-laravel",
		`{"capabilities":{"deployment_model":"server","activation_phases":["migrate"]}}`,
		`{"extension":{"framework":"laravel","keys":[{"name":"framework.laravel.version","description":"version constraint"}]}}`)

	_, stdout, stderr, err := executeCommand("adapter", "inspect", "laravel", "--json")
	if err != nil {
		t.Fatalf("adapter inspect --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if envelope.Status != "success" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "success")
	}

	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var inspect adapterInspectJSON
	if err := json.Unmarshal(raw, &inspect); err != nil {
		t.Fatalf("envelope data is not an inspect result: %v\n%s", err, raw)
	}
	if inspect.Framework != "laravel" {
		t.Errorf("framework = %q, want %q", inspect.Framework, "laravel")
	}
	if inspect.Capabilities.DeploymentModel != "server" {
		t.Errorf("capabilities.deployment_model = %q, want %q", inspect.Capabilities.DeploymentModel, "server")
	}
	if len(inspect.Capabilities.ActivationPhases) != 1 || inspect.Capabilities.ActivationPhases[0] != "migrate" {
		t.Errorf("capabilities.activation_phases = %v, want [migrate]", inspect.Capabilities.ActivationPhases)
	}
	if inspect.ConfigExtension.Framework != "laravel" {
		t.Errorf("config_extension.framework = %q, want %q", inspect.ConfigExtension.Framework, "laravel")
	}
	if len(inspect.ConfigExtension.Keys) != 1 {
		t.Errorf("config_extension.keys = %v, want 1 key", inspect.ConfigExtension.Keys)
	}
}
