// Package cmd implements the Anvil CLI commands.
//
// Tests for the adoption-time installed v1.x adapter recognition and
// migration (TS-017-01-02, T-004; ADR-028 §3, §12.3): when a project
// with a declared framework and an installed v1.x adapter is used, the
// runtime identifies the installed adapter through the authoritative
// mapping table (TS-017-01-01 — consumed as data via
// ANVIL_ADAPTER_STANDARD_MAPPING, never hard-coded), maps it to the
// corresponding delivery lifecycle standard, and records the migration
// outcome. These tests lock the behavior:
//
//   - first-party cases (Laravel, Flutter): recognized at adoption time
//     and recorded (migration pending when the standard is missing);
//   - completed migration: when the mapped standard is installed, the
//     outcome is recorded as migrated (resolution switched to the
//     standard);
//   - the resolution semantics of the calling commands are UNCHANGED:
//     the standard-missing hard-fail (ADR-026 decision 3) still fires,
//     and recognition never modifies project state;
//   - no adapter installed, or a framework outside the first-party
//     mapping: no recognition, no record, no notice;
//   - a mapping artifact that cannot be read surfaces an explicit
//     warning and never blocks the resolution path.
//
// Reference: TS-017-01-02, TS-017-01-01 §7, ADR-028 §3, §12.3,
// ADR-026 decision 3
package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maleolabs.com/anvil/internal/registry"
)

// authoritativeMappingPathCmd returns the absolute path of the
// maintained adapter-to-standard mapping artifact relative to this
// package's test working directory (go test runs with the package
// directory as the working directory). The path is absolute because the
// commands under test chdir into temp project directories; a relative
// path would resolve against the wrong directory.
func authoritativeMappingPathCmd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	path := filepath.Join(wd, "..", "docs", "planning", "ANVIL_V2_ADAPTER_STANDARD_MAPPING.md")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("adapter mapping not present (EKA mode) — %v", err)
	}
	return path
}

// seedAdapterRecognitionEnv isolates the config environment (global
// config dir, installed-standard store, migration store), installs a
// probe-validated fake adapter binary for framework (closed-set
// discovery, TS-007-039), stubs the executable lookup, and points the
// recognition logic at the authoritative mapping artifact and the
// compatibility matrix record (the corpus is co-located with the
// engine). It returns the fake adapter directory.
func seedAdapterRecognitionEnv(t *testing.T, framework string) string {
	t.Helper()
	isolateConfigEnvironment(t)
	dir := stubInstalledAdapter(t, framework, adapterCapabilitiesJSON("server"), adapterExtensionJSON(framework))
	stubAdapterLookup(t, dir)
	// ANVIL_ADAPTER_STANDARD_MAPPING and ANVIL_COMPATIBILITY_MATRIX
	// must be set AFTER isolateConfigEnvironment (which clears the
	// ANVIL_* prefix).
	t.Setenv(registry.EnvAdapterStandardMapping, authoritativeMappingPathCmd(t))
	t.Setenv(registry.EnvCompatibilityMatrix, authoritativeMatrixPathCmd(t))
	return dir
}

// authoritativeMatrixPathCmd returns the absolute path of the
// compatibility matrix record relative to this package's test working
// directory (go test runs with the package directory as the working
// directory). The path is absolute because the commands under test
// chdir into temp project directories; a relative path would resolve
// against the wrong directory.
func authoritativeMatrixPathCmd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	path := filepath.Join(wd, "..", "docs", "specification-corpus", "compatibility-matrix.json")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("compatibility matrix not present (EKA mode) — %v", err)
	}
	return path
}

// seedRecognizedProject writes a project declaring framework into a
// fresh temp dir and chdirs into it (writeProjectConfig semantics).
func seedRecognizedProject(t *testing.T, framework string) {
	t.Helper()
	writeProjectConfig(t, t.TempDir(), "project:\n  name: migrated-project\n  framework: "+framework+"\n")
}

// migrationRecordPath returns the migration outcome record path for the
// adapter under the isolated global config dir.
func migrationRecordPath(t *testing.T, adapterName string) string {
	t.Helper()
	dir, err := registry.DefaultAdapterMigrationsDir()
	if err != nil {
		t.Fatalf("resolve migration store dir: %v", err)
	}
	return filepath.Join(dir, adapterName+".json")
}

// readMigrationOutcome reads and decodes the migration outcome record
// for the adapter.
func readMigrationOutcome(t *testing.T, adapterName string) registry.MigrationOutcome {
	t.Helper()
	raw, err := os.ReadFile(migrationRecordPath(t, adapterName))
	if err != nil {
		t.Fatalf("read migration outcome record: %v", err)
	}
	var outcome registry.MigrationOutcome
	if err := json.Unmarshal(raw, &outcome); err != nil {
		t.Fatalf("decode migration outcome record: %v", err)
	}
	return outcome
}

// seedMigrationStandard records an installed delivery lifecycle standard
// without config extension content (the pass-through case of the
// standard-driven framework validation) into the ALREADY isolated
// installed-standard store (unlike the shared seedInstalledStandard in
// adapter_test.go, this does not re-isolate the config dir). The record
// declares the given contract version (TS-017-01-03: the declared
// contract version is what migration validation checks).
func seedMigrationStandard(t *testing.T, id, version, contractVersion string) {
	t.Helper()
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("installed-standards dir: %v", err)
	}
	now := time.Now().UTC()
	rec := registry.InstalledStandardRecord{
		FormatVersion:   registry.RecordFormatVersion,
		ID:              id,
		Version:         version,
		ContractVersion: contractVersion,
		Resolution:      registry.Resolution{Kind: registry.ResolutionKindIndex, Source: "/registry"},
		InstalledAt:     now,
		UpdatedAt:       now,
		Lifecycle:       registry.Lifecycle{State: registry.LifecycleStatePublished},
	}
	if _, _, err := registry.NewInstalledStandardStore(dir).Record(rec.ID, rec); err != nil {
		t.Fatalf("record installed standard: %v", err)
	}
}

// TestInstalledAdapterRecognition_ConfigValidate_Laravel is the Laravel
// first-party adoption-time case (TS-017-01-02 DoD): a project with a
// declared framework and an installed v1.x adapter is used (config
// validate), the runtime recognizes the installed adapter through the
// authoritative mapping and records the migration outcome — while the
// command's resolution semantics (standard-missing hard-fail, ADR-026
// decision 3) are unchanged and project state is untouched.
func TestInstalledAdapterRecognition_ConfigValidate_Laravel(t *testing.T) {
	seedAdapterRecognitionEnv(t, "laravel")
	seedRecognizedProject(t, "laravel")

	_, stdout, stderr, err := executeCommand("config", "validate")

	// The standard-missing hard-fail is unchanged (ADR-026 decision 3):
	// a declared framework without an installed standard still fails.
	if err == nil {
		t.Fatal("config validate should hard-fail — the standard is not installed (ADR-026 decision 3)")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error should state the missing standard, got: %v", err)
	}

	// The migration outcome is recorded: the installed v1.x adapter was
	// recognized and maps to the standard (migration pending).
	outcome := readMigrationOutcome(t, "laravel")
	if outcome.Status != registry.MigrationStatusRecognized {
		t.Errorf("recorded status = %q, want recognized (standard not installed — migration pending)", outcome.Status)
	}
	if outcome.StandardID != "anvil-standard-laravel" {
		t.Errorf("recorded standard = %q, want anvil-standard-laravel (from the authoritative mapping)", outcome.StandardID)
	}
	if outcome.AdapterExecutable != "anvil-adapter-laravel" {
		t.Errorf("recorded executable = %q, want anvil-adapter-laravel", outcome.AdapterExecutable)
	}

	// The recognition notice is surfaced explicitly (recorded, never
	// silent — A2), and the mapping artifact was readable (no
	// unavailability warning).
	if !strings.Contains(stderr, "recognized") || !strings.Contains(stderr, "anvil-standard-laravel") {
		t.Errorf("stderr should surface the recorded recognition, got: %s", stderr)
	}
	if strings.Contains(stderr, "adapter recognition unavailable") {
		t.Errorf("no recognition-unavailable warning expected, got: %s", stderr)
	}
	if strings.Contains(stdout, "recognized") {
		t.Errorf("recognition notice must not pollute stdout, got: %s", stdout)
	}
}

// TestInstalledAdapterRecognition_ConfigValidate_Flutter is the Flutter
// first-party adoption-time case (TS-017-01-02 DoD): the second
// first-party row of the authoritative mapping recognizes identically —
// the row set drives recognition, not any code-side list.
func TestInstalledAdapterRecognition_ConfigValidate_Flutter(t *testing.T) {
	seedAdapterRecognitionEnv(t, "flutter")
	seedRecognizedProject(t, "flutter")

	_, _, stderr, err := executeCommand("config", "validate")
	if err == nil {
		t.Fatal("config validate should hard-fail — the standard is not installed (ADR-026 decision 3)")
	}

	outcome := readMigrationOutcome(t, "flutter")
	if outcome.Status != registry.MigrationStatusRecognized {
		t.Errorf("recorded status = %q, want recognized", outcome.Status)
	}
	if outcome.StandardID != "anvil-standard-flutter" {
		t.Errorf("recorded standard = %q, want anvil-standard-flutter (from the authoritative mapping)", outcome.StandardID)
	}
	if !strings.Contains(stderr, "anvil-standard-flutter") {
		t.Errorf("stderr should surface the recorded recognition, got: %s", stderr)
	}
}

// TestInstalledAdapterRecognition_ConfigValidate_Migrated is the
// completed-migration case (TS-017-01-02 DoD: "migration switches
// resolution to the standard"; TS-017-01-03: contract-version
// validation at migration): with the mapped standard installed and its
// declared contract version supported by the runtime, the outcome is
// recorded as migrated pinning the version and the validated declared
// contract version, and the command succeeds — the recognized adapter
// carried the project until the standard took over.
func TestInstalledAdapterRecognition_ConfigValidate_Migrated(t *testing.T) {
	seedAdapterRecognitionEnv(t, "laravel")
	seedMigrationStandard(t, "anvil-standard-laravel", "2.1.0", "1.0.0")
	seedRecognizedProject(t, "laravel")

	_, stdout, stderr, err := executeCommand("config", "validate")
	if err != nil {
		t.Fatalf("config validate should succeed with the standard installed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "Configuration is valid.") {
		t.Errorf("stdout should report valid configuration, got: %s", stdout)
	}

	outcome := readMigrationOutcome(t, "laravel")
	if outcome.Status != registry.MigrationStatusMigrated {
		t.Errorf("recorded status = %q, want migrated — resolution switched to the standard", outcome.Status)
	}
	if outcome.StandardVersion != "2.1.0" {
		t.Errorf("recorded standard version = %q, want 2.1.0 (the pinned version of the installed record)", outcome.StandardVersion)
	}
	// The contract-version seam is filled with the VALIDATED declared
	// contract version of the installed standard (TS-017-01-03).
	if outcome.ContractVersion != "1.0.0" {
		t.Errorf("recorded contract version = %q, want 1.0.0 (the declared contract version of the installed standard)", outcome.ContractVersion)
	}
	if !strings.Contains(stderr, "resolution switched to delivery lifecycle standard anvil-standard-laravel 2.1.0") {
		t.Errorf("stderr should surface the completed migration, got: %s", stderr)
	}
	if !strings.Contains(stderr, "declared contract version 1.0.0 validated against supported contract major(s) [1]") {
		t.Errorf("stderr should surface the validated contract version, got: %s", stderr)
	}
}

// TestInstalledAdapterRecognition_Init is the init adoption-time case:
// a framework-declared init with an installed v1.x adapter records the
// recognition; with the standard installed, init succeeds and records
// the completed migration.
func TestInstalledAdapterRecognition_Init(t *testing.T) {
	// Without the standard: init hard-fails (TS-015-02-02, ADR-026
	// decision 3) but the recognition outcome is recorded.
	seedAdapterRecognitionEnv(t, "laravel")
	chdirTemp(t, func(t *testing.T, dir string) {
		_, _, stderr, err := executeCommand("init", "demo", "--framework", "laravel")
		if err == nil {
			t.Fatal("init should hard-fail — the standard is not installed (ADR-026 decision 3)")
		}
		if !strings.Contains(stderr, "recognized") {
			t.Errorf("stderr should surface the recorded recognition, got: %s", stderr)
		}
		outcome := readMigrationOutcome(t, "laravel")
		if outcome.Status != registry.MigrationStatusRecognized {
			t.Errorf("recorded status = %q, want recognized", outcome.Status)
		}
	})

	// With the standard installed: init succeeds and the migration
	// outcome is recorded as migrated.
	seedAdapterRecognitionEnv(t, "laravel")
	seedMigrationStandard(t, "anvil-standard-laravel", "1.0.0", "1.0.0")
	chdirTemp(t, func(t *testing.T, dir string) {
		_, _, stderr, err := executeCommand("init", "demo", "--framework", "laravel")
		if err != nil {
			t.Fatalf("init should succeed with the standard installed: %v\nstderr: %s", err, stderr)
		}
		if !strings.Contains(stderr, "resolution switched to delivery lifecycle standard anvil-standard-laravel 1.0.0") {
			t.Errorf("stderr should surface the completed migration, got: %s", stderr)
		}
		outcome := readMigrationOutcome(t, "laravel")
		if outcome.Status != registry.MigrationStatusMigrated || outcome.StandardVersion != "1.0.0" {
			t.Errorf("recorded outcome = %+v, want migrated at 1.0.0", outcome)
		}
	})
}

// TestInstalledAdapterRecognition_NoAdapterInstalled verifies that a
// declared framework WITHOUT an installed adapter triggers no
// recognition: no record, no notice — recognition is never assumed from
// the declaration alone (RFC-P7; TS-007-039 §7), and the resolution
// path is unchanged.
func TestInstalledAdapterRecognition_NoAdapterInstalled(t *testing.T) {
	isolateConfigEnvironment(t)
	// No adapter anywhere: stub the CLI install dir to an empty
	// directory and clear PATH so discovery is deterministic.
	stubAdapterInstallDirAt(t, t.TempDir())
	t.Setenv("PATH", "")
	t.Setenv(registry.EnvAdapterStandardMapping, authoritativeMappingPathCmd(t))
	seedRecognizedProject(t, "laravel")

	_, _, stderr, err := executeCommand("config", "validate")
	if err == nil {
		t.Fatal("config validate should hard-fail — the standard is not installed (ADR-026 decision 3)")
	}
	if _, err := os.Stat(migrationRecordPath(t, "laravel")); !os.IsNotExist(err) {
		t.Errorf("no migration outcome should be recorded without an installed adapter (stat error: %v)", err)
	}
	if strings.Contains(stderr, "recognized") {
		t.Errorf("no recognition notice expected without an installed adapter, got: %s", stderr)
	}
}

// TestInstalledAdapterRecognition_UnknownFramework verifies that a
// framework outside the first-party mapping is not recognized: a
// third-party adapter (no mapping row, §7) yields no record and no
// notice, while the resolution path is unchanged.
func TestInstalledAdapterRecognition_UnknownFramework(t *testing.T) {
	seedAdapterRecognitionEnv(t, "rails")
	seedRecognizedProject(t, "rails")

	_, _, stderr, err := executeCommand("config", "validate")
	if err == nil {
		t.Fatal("config validate should hard-fail — the standard is not installed (ADR-026 decision 3)")
	}
	if _, err := os.Stat(migrationRecordPath(t, "rails")); !os.IsNotExist(err) {
		t.Errorf("no migration outcome should be recorded for a non-first-party framework (stat error: %v)", err)
	}
	if strings.Contains(stderr, "recognized") || strings.Contains(stderr, "adapter recognition unavailable") {
		t.Errorf("no recognition notice expected for a non-first-party framework, got: %s", stderr)
	}
}

// TestInstalledAdapterRecognition_MappingUnavailable verifies that an
// unreadable mapping artifact — an existing file that fails the §7
// contract parse, with a probe-validated adapter present to recognize —
// surfaces an explicit warning and never blocks the resolution path:
// recognition is a transition mechanism (ADR-028 §12.3), not a
// resolution gate. (A MISSING artifact is a silent no-op — recognition
// is simply not configured on that system, and no probe is spawned;
// that case is covered by the no-adapter/no-mapping paths above.)
func TestInstalledAdapterRecognition_MappingUnavailable(t *testing.T) {
	isolateConfigEnvironment(t)
	// A probe-validated adapter IS installed — there is something to
	// recognize — and the mapping artifact EXISTS but is not a valid
	// mapping table (broken artifact).
	dir := stubInstalledAdapter(t, "laravel", adapterCapabilitiesJSON("server"), adapterExtensionJSON("laravel"))
	stubAdapterLookup(t, dir)
	broken := filepath.Join(t.TempDir(), "broken-mapping.md")
	if err := os.WriteFile(broken, []byte("# not a mapping table\n| unrelated |\n|---|\n| x |\n"), 0644); err != nil {
		t.Fatalf("write broken mapping artifact: %v", err)
	}
	t.Setenv(registry.EnvAdapterStandardMapping, broken)
	seedRecognizedProject(t, "laravel")

	_, _, stderr, err := executeCommand("config", "validate")
	if err == nil {
		t.Fatal("config validate should still hard-fail — the standard is not installed (ADR-026 decision 3)")
	}
	if !strings.Contains(stderr, "adapter recognition unavailable") {
		t.Errorf("stderr should surface the unavailability explicitly, got: %s", stderr)
	}
	if !strings.Contains(stderr, "no mapping table found") {
		t.Errorf("stderr should name the broken mapping artifact, got: %s", stderr)
	}
	if _, err := os.Stat(migrationRecordPath(t, "laravel")); !os.IsNotExist(err) {
		t.Errorf("no migration outcome should be recorded when the mapping cannot be read (stat error: %v)", err)
	}
}

// chdirTemp runs fn with the working directory set to a fresh temp dir
// and restores the original directory immediately afterwards (the
// writeProjectConfig pattern, but with immediate restore so a test can
// chdir multiple times without polluting the working directory for
// later steps).
func chdirTemp(t *testing.T, fn func(t *testing.T, dir string)) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("restore working directory %q: %v", orig, err)
		}
	}()
	fn(t, dir)
}

// TestInstalledAdapterRecognition_ConfigValidate_Mismatch is the
// contract-version mismatch case (TS-017-01-03): the mapped standard IS
// installed but its declared contract version targets a contract major
// this runtime does not support. The mismatch NEVER silently passes —
// the outcome is recorded as recognized (the migration did not
// complete) with the declared contract version recorded, and an
// actionable report states the declared version, the supported set, and
// the remediation. The command's resolution path is unchanged: the
// standard is installed, so config validate still succeeds.
func TestInstalledAdapterRecognition_ConfigValidate_Mismatch(t *testing.T) {
	seedAdapterRecognitionEnv(t, "laravel")
	// The standard is installed declaring contract version 2.0.0 —
	// unsupported by this runtime (supported contract majors: {1}).
	seedMigrationStandard(t, "anvil-standard-laravel", "2.1.0", "2.0.0")
	seedRecognizedProject(t, "laravel")

	_, stdout, stderr, err := executeCommand("config", "validate")
	if err != nil {
		t.Fatalf("config validate should still succeed — the standard is installed (resolution unchanged): %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "Configuration is valid.") {
		t.Errorf("stdout should report valid configuration, got: %s", stdout)
	}

	// The mismatch outcome is recorded: status recognized (the
	// migration did NOT complete — never silent acceptance) with the
	// declared contract version recorded for auditability.
	outcome := readMigrationOutcome(t, "laravel")
	if outcome.Status != registry.MigrationStatusRecognized {
		t.Errorf("recorded status = %q, want recognized — a mismatch never silently completes the migration", outcome.Status)
	}
	if outcome.StandardVersion != "" {
		t.Errorf("recorded standard version = %q, want empty — the migration did not complete", outcome.StandardVersion)
	}
	if outcome.ContractVersion != "2.0.0" {
		t.Errorf("recorded contract version = %q, want 2.0.0 (the declared contract version that failed validation)", outcome.ContractVersion)
	}

	// The mismatch report is actionable: what was declared, why it
	// fails, and how to resolve it.
	if !strings.Contains(stderr, "declared contract version 2.0.0 is not supported by this runtime") {
		t.Errorf("stderr should state the unsupported declared contract version, got: %s", stderr)
	}
	if !strings.Contains(stderr, "supported contract major(s): [1]") {
		t.Errorf("stderr should state the supported contract majors, got: %s", stderr)
	}
	if !strings.Contains(stderr, "migration did NOT complete") {
		t.Errorf("stderr should state that the migration did not complete, got: %s", stderr)
	}
	if !strings.Contains(stderr, "anvil standard update anvil-standard-laravel <version>") {
		t.Errorf("stderr should carry the remediation (standard update), got: %s", stderr)
	}
	if !strings.Contains(stderr, "mismatch outcome recorded") {
		t.Errorf("stderr should state the mismatch is recorded, got: %s", stderr)
	}
}

// TestInstalledAdapterRecognition_MatrixUnavailable verifies the
// fail-closed validation path of TS-017-01-03: when the compatibility
// matrix cannot be read (an existing file that fails the pinned record
// shape), contract-version validation at migration cannot run — a
// migration outcome is NEVER recorded without validation (declared,
// validated, and recorded — never assumed, ADR-024 §3.6;supported
// majors are never silently defaulted, PM binding decision 3). The
// unavailability is surfaced explicitly and the resolution path is
// unchanged.
func TestInstalledAdapterRecognition_MatrixUnavailable(t *testing.T) {
	isolateConfigEnvironment(t)
	// A probe-validated adapter IS installed — there is something to
	// recognize — and the mapping artifact is readable;only the
	// compatibility matrix is a broken artifact.
	dir := stubInstalledAdapter(t, "laravel", adapterCapabilitiesJSON("server"), adapterExtensionJSON("laravel"))
	stubAdapterLookup(t, dir)
	t.Setenv(registry.EnvAdapterStandardMapping, authoritativeMappingPathCmd(t))
	broken := filepath.Join(t.TempDir(), "broken-matrix.json")
	if err := os.WriteFile(broken, []byte("{not a matrix record"), 0644); err != nil {
		t.Fatalf("write broken matrix artifact: %v", err)
	}
	t.Setenv(registry.EnvCompatibilityMatrix, broken)
	seedRecognizedProject(t, "laravel")

	_, _, stderr, err := executeCommand("config", "validate")
	if err == nil {
		t.Fatal("config validate should still hard-fail — the standard is not installed (ADR-026 decision 3)")
	}
	if !strings.Contains(stderr, "adapter recognition unavailable") {
		t.Errorf("stderr should surface the recognition unavailability explicitly, got: %s", stderr)
	}
	if !strings.Contains(stderr, "compatibility matrix") {
		t.Errorf("stderr should name the compatibility matrix, got: %s", stderr)
	}
	if _, err := os.Stat(migrationRecordPath(t, "laravel")); !os.IsNotExist(err) {
		t.Errorf("no migration outcome should be recorded when validation cannot run (stat error: %v)", err)
	}
}
