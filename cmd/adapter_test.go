// Package cmd implements the Anvil CLI commands.
//
// Tests for the adapter command group (TS-007-031, TS-007-032): the
// parent-only group registration, "anvil adapter list", and "anvil
// adapter inspect".
//
// The adapter subcommands invoke adapter executables through the command
// contract. Tests stub the adapterExecutableLookup seam to point at fake
// adapter shell scripts in t.TempDir() (the real Process Runner executes
// them), so no adapter binary needs to be on PATH. Discovery
// (TS-007-039) is made deterministic by stubbing the CLI install
// directory (stubAdapterInstallDirAt) and clearing PATH, so the fake
// binaries are the only detected adapters. The Core carries no
// known-framework catalog (ADR-026), so tests isolate the global config
// directory (XDG_CONFIG_HOME) to keep the installed-standard store
// deterministic for the unknown-adapter hint.
//
// Reference: TS-007-031, TS-007-032, ADR-026
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// stubAdapterScript returns a shell script that implements the adapter
// command contract for testing: it answers the capabilities and
// extension commands by printing the provided JSON documents to stdout
// and exits 0. capsFile and extFile are absolute paths to the JSON
// fixtures the script reads. The script uses only shell builtins (read,
// printf, echo) so it works regardless of the PATH the test sets — the
// adapter list tests replace PATH to control system scanning.
func stubAdapterScript(capsFile, extFile string) string {
	return fmt.Sprintf(`#!/bin/sh
# Stub adapter for CLI tests. Answers capabilities/extension from files.
dump() {
  while IFS= read -r line || [ -n "$line" ]; do
    printf '%%s\n' "$line"
  done < "$1"
}
case "$1" in
  capabilities) dump "%s" ;;
  extension) dump "%s" ;;
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

// isolateGlobalConfigDir points the global config directory at a fresh
// temp dir (XDG_CONFIG_HOME), so the installed-standard store
// (registry.DefaultInstalledStandardsDir) is deterministic and never
// reads the developer's real config state.
func isolateGlobalConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// seedInstalledStandard records one installed delivery lifecycle standard
// in the isolated global config directory, so the registry-client
// resolution in the unknown-adapter hint (ADR-026) is deterministic.
func seedInstalledStandard(t *testing.T, id, version string) {
	t.Helper()
	isolateGlobalConfigDir(t)
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("installed-standards dir: %v", err)
	}
	now := time.Now().UTC()
	rec := registry.InstalledStandardRecord{
		FormatVersion:   registry.RecordFormatVersion,
		ID:              id,
		Version:         version,
		ContractVersion: "1",
		Resolution:      registry.Resolution{Kind: registry.ResolutionKindIndex, Source: "/registry"},
		InstalledAt:     now,
		UpdatedAt:       now,
		Lifecycle:       registry.Lifecycle{State: registry.LifecycleStatePublished},
	}
	if _, _, err := registry.NewInstalledStandardStore(dir).Record(id, rec); err != nil {
		t.Fatalf("record installed standard: %v", err)
	}
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

// ── Adapter List (TS-007-031, TS-017-02-02) ──────────────────────────

// TestAdapterList_TableOutput verifies that "anvil adapter list" renders
// every adapter whose standard is RECORDED (the registry-driven
// installed view, TS-017-02-02) with its deployment model and recorded
// version.
//
// Reference: TS-007-031 AC-1, AC-2, TS-017-02-02
func TestAdapterList_TableOutput(t *testing.T) {
	seedInstalledStandardBatch(t, map[string]string{
		"anvil-standard-laravel": "1.0.0",
		"anvil-standard-flutter": "2.0.0",
	})
	dir := t.TempDir()
	stubAdapterLookup(t, dir)
	t.Setenv("PATH", "")
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

	for _, want := range []string{"laravel", "server", "flutter", "hybrid", "Name", "Deployment Model", "Version", "1.0.0", "2.0.0"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout should contain %q, got:\n%s", want, stdout)
		}
	}
}

// TestAdapterList_EmptyMessage verifies the empty state: when no
// standard is recorded (registry-driven installed view), the command
// says so and hints at the --available discovery flag (exit 0).
//
// Reference: TS-007-031 AC-4, TS-017-02-02
func TestAdapterList_EmptyMessage(t *testing.T) {
	isolateGlobalConfigDir(t) // no records on this machine
	t.Setenv("PATH", "")

	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "No adapters installed.") {
		t.Errorf("stdout should contain the empty message, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "--available") {
		t.Errorf("stdout should hint at 'anvil adapter list --available', got:\n%s", stdout)
	}
}

// TestAdapterList_JSON verifies the --json output: an array of
// {name, deployment_model, version} entries under the standard envelope;
// the version is the recorded standard version (registry-driven
// installed view).
//
// Reference: TS-007-031 AC-3, TS-017-02-02
func TestAdapterList_JSON(t *testing.T) {
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")
	dir := t.TempDir()
	stubAdapterLookup(t, dir)
	writeFakeAdapter(t, dir, "anvil-adapter-laravel",
		`{"capabilities":{"deployment_model":"server"}}`,
		`{"extension":{"framework":"laravel","keys":[]}}`)

	_, stdout, stderr, err := executeCommand("adapter", "list", "--json")
	if err != nil {
		t.Fatalf("adapter list --json returned unexpected error: %v (stderr: %s)", err, stderr)
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
	if entry.Version != "1.0.0" {
		t.Errorf("entry version = %q, want %q (the recorded standard version)", entry.Version, "1.0.0")
	}
	if entry.Status != "" {
		t.Errorf("entry status = %q, want %q (status only set in --available mode)", entry.Status, "")
	}
}

// ── Adapter List --available (TS-007-031, TS-016-04-01) ──────────────

// adapterListTestIndex stages a static registry index offering the given
// (id, version, lifecycle-state) releases as FULLY VALID registry
// documents (strict-parseable — the --available listing reads
// parseable documents; content is never fetched by the listing) and
// returns the index dir.
func adapterListTestIndex(t *testing.T, releases ...registry.Metadata) string {
	t.Helper()
	indexDir := t.TempDir()
	for _, md := range releases {
		installTestIndexEntry(t, indexDir, md)
	}
	return indexDir
}

// adapterListTestRelease builds a valid signed registry metadata
// document for the --available listing tests (the listing surface parses
// the documents strictly; it never fetches content or verifies trust).
func adapterListTestRelease(t *testing.T, id, version, state string) registry.Metadata {
	t.Helper()
	pub, priv := installTestKeypair(t)
	return installTestRelease(t, id, version, "https://github.com/maleolabs/"+id+"/releases/download/v"+version+"/"+id+"-"+version+".tar.gz",
		state, "", []string{"5.1.0"}, []byte("content "+id+" "+version), pub, priv)
}

// TestAdapterListAvailable_ListsRegistryAdapters verifies that
// --available lists the adapters offered for adoption in the registry
// index (TS-016-04-01): standards named anvil-standard-<name> map to
// adapter names, each shown with the highest adoptable version and its
// install status — the installed marking is registry-driven (recorded
// standard, TS-017-02-02): installed adapters keep their real
// deployment model when the executable resolves, others show "-" and
// "available".
//
// Reference: TS-007-031, TS-016-04-01, TS-017-02-02, ADR-021 §3.1
func TestAdapterListAvailable_ListsRegistryAdapters(t *testing.T) {
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")
	dir := t.TempDir()
	stubAdapterLookup(t, dir)
	writeFakeAdapter(t, dir, "anvil-adapter-laravel",
		`{"capabilities":{"deployment_model":"server"}}`,
		`{"extension":{"framework":"laravel","keys":[]}}`)
	indexDir := adapterListTestIndex(t,
		adapterListTestRelease(t, "anvil-standard-laravel", "1.0.0", registry.LifecycleStatePublished),
		adapterListTestRelease(t, "anvil-standard-laravel", "1.1.0", registry.LifecycleStatePublished),
		adapterListTestRelease(t, "anvil-standard-flutter", "2.0.0", registry.LifecycleStatePublished),
		adapterListTestRelease(t, "anvil-standard-flutter", "3.0.0", registry.LifecycleStateRetired),
	)

	_, stdout, stderr, err := executeCommand("adapter", "list", "--available", "--index", indexDir)
	if err != nil {
		t.Fatalf("adapter list --available returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	for _, want := range []string{"Name", "Deployment Model", "Version", "Status",
		"laravel", "server", "installed", "flutter", "-", "available", "1.1.0", "2.0.0"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout should contain %q, got:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "3.0.0") {
		t.Errorf("stdout should not show the retired release version:\n%s", stdout)
	}
}

// TestAdapterListAvailable_MissingIndexError verifies that a missing
// index directory produces the actionable not-found error (exit 3),
// consistent with the standard commands.
//
// Reference: TS-016-04-01, TS-P8-07
func TestAdapterListAvailable_MissingIndexError(t *testing.T) {
	_, _, stderr, err := executeCommand("adapter", "list", "--available", "--index", filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("expected index error, got nil")
	}
	if !strings.Contains(stderr, "registry index") {
		t.Errorf("stderr should report the missing registry index, got: %s", stderr)
	}
}

// TestAdapterListAvailable_Empty verifies the empty index state: an index
// offering nothing yields an informative message, exit 0.
//
// Reference: TS-007-031, TS-016-04-01
func TestAdapterListAvailable_Empty(t *testing.T) {
	isolateGlobalConfigDir(t)
	indexDir := adapterListTestIndex(t)

	_, stdout, stderr, err := executeCommand("adapter", "list", "--available", "--index", indexDir)
	if err != nil {
		t.Fatalf("adapter list --available returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "No adapters available in the registry index.") {
		t.Errorf("stdout should contain the empty message, got:\n%s", stdout)
	}
}

// TestAdapterListAvailable_JSON verifies the --available --json shape:
// entries carry name, deployment model, registry version, and status.
//
// Reference: TS-007-031 AC-3, TS-016-04-01
func TestAdapterListAvailable_JSON(t *testing.T) {
	indexDir := adapterListTestIndex(t,
		adapterListTestRelease(t, "anvil-standard-laravel", "1.0.0", registry.LifecycleStatePublished),
	)

	// Deterministic environment: no records on this machine (the
	// registry-driven installed definition), so the registry entry is
	// reported as "available" (TS-017-02-02).
	isolateGlobalConfigDir(t)
	t.Setenv("PATH", "")

	_, stdout, stderr, err := executeCommand("adapter", "list", "--available", "--json", "--index", indexDir)
	if err != nil {
		t.Fatalf("adapter list --available --json returned unexpected error: %v (stderr: %s)", err, stderr)
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
	if entry.DeploymentModel != "-" {
		t.Errorf("entry deployment_model = %q, want %q (not installed)", entry.DeploymentModel, "-")
	}
	if entry.Version != "1.0.0" {
		t.Errorf("entry version = %q, want %q", entry.Version, "1.0.0")
	}
	if entry.Status != "available" {
		t.Errorf("entry status = %q, want %q", entry.Status, "available")
	}
}

// ── Adapter Inspect (TS-007-032) ─────────────────────────────────────

// TestAdapterInspect_UnknownAdapter verifies that an adapter name whose
// standard is not recorded produces a clear error. The Core carries no
// known-framework catalog (ADR-026) and performs no binary scan
// (TS-017-02-02): with no installed delivery lifecycle standard
// recorded, the hint reports the empty registry state and points at
// standard adoption instead of listing runtime-known frameworks.
//
// Reference: TS-007-032 AC-4, TS-007-039, ADR-026, TS-017-02-02
func TestAdapterInspect_UnknownAdapter(t *testing.T) {
	isolateGlobalConfigDir(t)
	t.Setenv("PATH", "")

	_, _, stderr, err := executeCommand("adapter", "inspect", "node")
	if err == nil {
		t.Fatal("expected error for unknown adapter, got nil")
	}
	if !strings.Contains(stderr, "unknown adapter") {
		t.Errorf("stderr should mention the unknown adapter, got: %s", stderr)
	}
	if !strings.Contains(stderr, "no adapter is installed through the registry") {
		t.Errorf("stderr should report the empty registry state, got: %s", stderr)
	}
	if !strings.Contains(stderr, "anvil standard install") {
		t.Errorf("stderr should point at standard adoption, got: %s", stderr)
	}
}

// TestAdapterInspect_ExecutableNotFound verifies that a recorded adapter
// whose executable cannot be resolved produces a clear error naming the
// expected binary and the adoption path.
//
// Reference: TS-007-032 AC-4, TS-017-02-02
func TestAdapterInspect_ExecutableNotFound(t *testing.T) {
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")
	orig := adapterExecutableLookup
	adapterExecutableLookup = func(name string) (string, error) {
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { adapterExecutableLookup = orig })

	_, _, stderr, err := executeCommand("adapter", "inspect", "laravel")
	if err == nil {
		t.Fatal("expected error for missing executable, got nil")
	}
	if !strings.Contains(stderr, "no adapter binary found") {
		t.Errorf("stderr should report no adapter binary found, got: %s", stderr)
	}
	if !strings.Contains(stderr, "anvil-adapter-laravel") {
		t.Errorf("stderr should name the expected binary, got: %s", stderr)
	}
	if !strings.Contains(stderr, "anvil adapter install") {
		t.Errorf("stderr should point at the adoption path, got: %s", stderr)
	}
}

// TestAdapterInspect_HumanOutput verifies that "anvil adapter inspect
// laravel" renders all capability sections for a server-model adapter.
//
// Reference: TS-007-032 AC-1, AC-2, AC-3
func TestAdapterInspect_HumanOutput(t *testing.T) {
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")
	dir := t.TempDir()
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
	seedInstalledStandard(t, "anvil-standard-flutter", "1.0.0")
	dir := t.TempDir()
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
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")
	dir := t.TempDir()
	stubAdapterLookup(t, dir)
	writeFakeAdapter(t, dir, "anvil-adapter-laravel",
		`{"capabilities":{"deployment_model":"server","activation_phases":["migrate"]}}`,
		`{"extension":{"framework":"laravel","keys":[{"name":"framework.laravel.version","description":"version constraint"}]}}`)

	_, stdout, stderr, err := executeCommand("adapter", "inspect", "laravel", "--json")
	if err != nil {
		t.Fatalf("adapter inspect --json returned unexpected error: %v (stderr: %s)", err, stderr)
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
