package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/config"
)

// TestConfigValidateCommand_ValidConfig verifies that:
//
//	anvil config validate
//
// on a valid project configuration exits 0 and prints a success result.
func TestConfigValidateCommand_ValidConfig(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: validate-test
  version: 2.1.0
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
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

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to project directory %q: %v", dir, err)
	}

	_, stdout, stderr, err := executeCommand("config", "validate")
	if err != nil {
		t.Fatalf("config validate returned unexpected error for valid config: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "Configuration is valid") {
		t.Errorf("stdout should contain the success result, got: %s", stdout)
	}
}

// TestConfigValidateCommand_ValidConfigJSON verifies that:
//
//	anvil config validate --json
//
// on a valid configuration produces a machine-readable result with
// valid=true and error_count=0.
func TestConfigValidateCommand_ValidConfigJSON(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: validate-test
  version: 2.1.0
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
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

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to project directory %q: %v", dir, err)
	}

	_, stdout, stderr, err := executeCommand("config", "validate", "--json")
	if err != nil {
		t.Fatalf("config validate --json returned unexpected error for valid config: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	result, err := parseConfigValidationJSON(t, stdout)
	if err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if !result.Valid {
		t.Error("JSON result should report valid=true")
	}
	if result.ErrorCount != 0 {
		t.Errorf("JSON result should report error_count=0, got %d", result.ErrorCount)
	}
	if len(result.Errors) != 0 {
		t.Errorf("JSON result should have no error categories, got: %v", result.Errors)
	}
}

// TestConfigValidateCommand_InvalidConfig verifies that:
//
//	anvil config validate
//
// on an invalid configuration exits non-zero and prints the validation
// error with its category.
func TestConfigValidateCommand_InvalidConfig(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: validate-test
release:
  max_retained: not-an-integer
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
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

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to project directory %q: %v", dir, err)
	}

	_, stdout, stderr, err := executeCommand("config", "validate")
	if err == nil {
		t.Fatal("expected non-zero exit (error) for invalid configuration, got nil")
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for invalid configuration, got: %s", stdout)
	}
	if !contains(stderr, "configuration is invalid") {
		t.Errorf("stderr should state the configuration is invalid, got: %s", stderr)
	}
	if !contains(stderr, "type:") {
		t.Errorf("stderr should contain the 'type' error category, got: %s", stderr)
	}
	if !contains(stderr, "release.max_retained") {
		t.Errorf("stderr should identify the invalid key, got: %s", stderr)
	}
	if !contains(stderr, "expected integer") {
		t.Errorf("stderr should state the expected type, got: %s", stderr)
	}
}

// TestConfigValidateCommand_InvalidConfigCategorized verifies that
// validation errors are grouped by category (required, type, allowed,
// format) so operators can act on the output.
func TestConfigValidateCommand_InvalidConfigCategorized(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	// Covers all four categories: missing project.name (required),
	// max_retained as string (type), retention_policy outside the allowed
	// set (allowed), and a non-SemVer version (format).
	configContent := `project:
  version: not-semver
release:
  max_retained: not-an-integer
  retention_policy: keep-all
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
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

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to project directory %q: %v", dir, err)
	}

	_, stdout, stderr, err := executeCommand("config", "validate")
	if err == nil {
		t.Fatal("expected non-zero exit (error) for invalid configuration, got nil")
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for invalid configuration, got: %s", stdout)
	}

	for _, category := range []string{"required:", "type:", "allowed:", "format:"} {
		if !contains(stderr, category) {
			t.Errorf("stderr should contain the '%s' error category, got: %s", category, stderr)
		}
	}
	for _, key := range []string{"project.name", "project.version", "release.max_retained", "release.retention_policy"} {
		if !contains(stderr, key) {
			t.Errorf("stderr should identify key %q, got: %s", key, stderr)
		}
	}
}

// TestConfigValidateCommand_InvalidConfigJSON verifies that:
//
//	anvil config validate --json
//
// on an invalid configuration produces a machine-readable result with
// valid=false and the categorized errors as structured data.
func TestConfigValidateCommand_InvalidConfigJSON(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  version: not-semver
release:
  max_retained: not-an-integer
  retention_policy: keep-all
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
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

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to project directory %q: %v", dir, err)
	}

	_, stdout, _, err := executeCommand("config", "validate", "--json")
	if err == nil {
		t.Fatal("expected non-zero exit (error) for invalid configuration, got nil")
	}

	result, err := parseConfigValidationJSON(t, stdout)
	if err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if result.Valid {
		t.Error("JSON result should report valid=false")
	}
	if result.ErrorCount != 4 {
		t.Errorf("JSON result should report error_count=4, got %d", result.ErrorCount)
	}
	for _, category := range []string{"required", "type", "allowed", "format"} {
		records, ok := result.Errors[category]
		if !ok || len(records) == 0 {
			t.Errorf("JSON result should contain errors for category %q, got: %v", category, result.Errors)
		}
	}
	if err := hasErrorRecord(result.Errors["type"], "release.max_retained"); !err {
		t.Error("JSON 'type' category should identify release.max_retained")
	}
	if err := hasErrorRecord(result.Errors["format"], "project.version"); !err {
		t.Error("JSON 'format' category should identify project.version")
	}
}

// TestConfigValidateCommand_OutsideProject verifies that running
//
//	anvil config validate
//
// outside an Anvil project reports the missing required values and exits
// non-zero.
func TestConfigValidateCommand_OutsideProject(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("failed to restore original directory %q: %v", orig, err)
		}
	}()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to temp directory %q: %v", dir, err)
	}

	_, _, stderr, err := executeCommand("config", "validate")
	if err == nil {
		t.Fatal("expected error when running 'config validate' outside project, got nil")
	}
	if !contains(stderr, "required") {
		t.Errorf("stderr should report the missing required value category, got: %s", stderr)
	}
	if !contains(stderr, "project.name") {
		t.Errorf("stderr should identify the missing key project.name, got: %s", stderr)
	}
}

// TestConfigValidateCommand_UnreadableConfig verifies that a configuration
// that cannot be resolved (malformed YAML) exits non-zero with a
// resolution error rather than a validation result.
func TestConfigValidateCommand_UnreadableConfig(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	if err := os.WriteFile(configPath, []byte("project: [unclosed"), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
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

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to project directory %q: %v", dir, err)
	}

	_, _, stderr, err := executeCommand("config", "validate")
	if err == nil {
		t.Fatal("expected error for unreadable configuration, got nil")
	}
	if !contains(stderr, "could not resolve configuration") {
		t.Errorf("stderr should report the resolution failure, got: %s", stderr)
	}
}

// TestConfigValidateCommand_UnreadableConfigJSON verifies that a
// configuration that cannot be resolved (malformed YAML) exits non-zero
// and emits the standard JSON error envelope when --json is used — a
// resolution failure is not a validation result, so no categorized
// validation data is produced.
func TestConfigValidateCommand_UnreadableConfigJSON(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	if err := os.WriteFile(configPath, []byte("project: [unclosed"), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
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

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to project directory %q: %v", dir, err)
	}

	_, stdout, _, err := executeCommand("config", "validate", "--json")
	if err == nil {
		t.Fatal("expected error for unreadable configuration, got nil")
	}

	var envelope struct {
		Version string `json:"version"`
		Status  string `json:"status"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout should be a JSON error envelope, got: %s (%v)", stdout, err)
	}
	if envelope.Status != "error" {
		t.Errorf("envelope status should be 'error', got %q", envelope.Status)
	}
	if envelope.Error == "" {
		t.Error("envelope error should describe the resolution failure")
	}
}

// TestConfigValidateCommand_NoArgs verifies that passing unexpected
// arguments to the validate command is rejected.
func TestConfigValidateCommand_NoArgs(t *testing.T) {
	isolateConfigEnvironment(t)
	_, _, stderr, err := executeCommand("config", "validate", "unexpected-arg")
	if err == nil {
		t.Fatal("expected error when passing unexpected argument, got nil")
	}
	if !contains(stderr, "unknown command") && !contains(stderr, "accepts 0 arg") {
		t.Errorf("stderr should report the argument error, got: %s", stderr)
	}
}

// TestConfigValidateCommand_NoFilesModified verifies that
//
//	anvil config validate
//
// does not create, modify, or delete any files.
func TestConfigValidateCommand_NoFilesModified(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: no-modify-test
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
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

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to project directory %q: %v", dir, err)
	}

	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to list directory before: %v", err)
	}

	_, _, _, err = executeCommand("config", "validate")
	if err != nil {
		t.Fatalf("config validate returned unexpected error: %v", err)
	}

	entriesAfter, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to list directory after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory entry count changed: before=%d, after=%d",
			len(entriesBefore), len(entriesAfter))
	}
	for i := range entriesBefore {
		if entriesBefore[i].Name() != entriesAfter[i].Name() {
			t.Errorf("entry %d name changed: before=%q, after=%q",
				i, entriesBefore[i].Name(), entriesAfter[i].Name())
		}
	}
}

// ── Unit Tests ───────────────────────────────────────────────────────

// TestCategorizeValidationError verifies the deterministic derivation of
// presentation categories from the config engine's validation errors.
func TestCategorizeValidationError(t *testing.T) {
	tests := []struct {
		name     string
		err      config.ValidationError
		expected string
	}{
		{
			name:     "missing required value",
			err:      config.ValidationError{Key: "project.name", Expected: "required string value"},
			expected: validationCategoryRequired,
		},
		{
			name:     "type mismatch",
			err:      config.ValidationError{Key: "release.max_retained", Expected: "integer"},
			expected: validationCategoryType,
		},
		{
			name:     "value outside allowed set",
			err:      config.ValidationError{Key: "release.retention_policy", Expected: "one of [keep-last]"},
			expected: validationCategoryAllowed,
		},
		{
			name:     "format violation",
			err:      config.ValidationError{Key: "project.version", Expected: "valid SemVer string (e.g. \"1.2.3\")"},
			expected: validationCategoryFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := categorizeValidationError(tt.err); got != tt.expected {
				t.Errorf("categorizeValidationError(%v) = %q, want %q", tt.err, got, tt.expected)
			}
		})
	}
}

// TestGroupValidationErrorsByCategory verifies that errors are grouped
// under their derived categories.
func TestGroupValidationErrorsByCategory(t *testing.T) {
	errs := []config.ValidationError{
		{Key: "project.name", Expected: "required string value"},
		{Key: "release.max_retained", Expected: "integer"},
		{Key: "release.retention_policy", Expected: "one of [keep-last]"},
		{Key: "project.version", Expected: "valid SemVer string (e.g. \"1.2.3\")"},
	}

	grouped := groupValidationErrorsByCategory(errs)

	if len(grouped[validationCategoryRequired]) != 1 {
		t.Errorf("required category should contain 1 error, got %d", len(grouped[validationCategoryRequired]))
	}
	if len(grouped[validationCategoryType]) != 1 {
		t.Errorf("type category should contain 1 error, got %d", len(grouped[validationCategoryType]))
	}
	if len(grouped[validationCategoryAllowed]) != 1 {
		t.Errorf("allowed category should contain 1 error, got %d", len(grouped[validationCategoryAllowed]))
	}
	if len(grouped[validationCategoryFormat]) != 1 {
		t.Errorf("format category should contain 1 error, got %d", len(grouped[validationCategoryFormat]))
	}
}

// isolateConfigEnvironment shields a test from ambient configuration
// sources on developer/CI machines: the global config directory is
// redirected to an isolated temp dir, and any ANVIL_CFG_* environment
// variables (execution-level overrides) plus ANVIL_ENV (environment-level
// file selection) are cleared for the duration of the test. The original
// environment is restored on cleanup.
func isolateConfigEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	type envPair struct{ name, value string }
	var cleared []string
	var saved []envPair
	for _, kv := range os.Environ() {
		name, value, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		if strings.HasPrefix(name, config.EnvPrefix) || name == "ANVIL_ENV" {
			cleared = append(cleared, name)
			saved = append(saved, envPair{name: name, value: value})
			if err := os.Unsetenv(name); err != nil {
				t.Fatalf("failed to unset %s: %v", name, err)
			}
		}
	}
	t.Cleanup(func() {
		for _, name := range cleared {
			_ = os.Unsetenv(name)
		}
		for _, pair := range saved {
			_ = os.Setenv(pair.name, pair.value)
		}
	})
}

// TestBuildValidationResult_SortsRecordsByKey verifies that records within
// a category are sorted by key, so the JSON output is deterministic across
// runs (the engine reports errors in map-iteration order).
func TestBuildValidationResult_SortsRecordsByKey(t *testing.T) {
	errs := []config.ValidationError{
		{Key: "release.max_retained", Expected: "integer"},
		{Key: "artifact.manifest", Expected: "boolean"},
		{Key: "release.auto_verify", Expected: "boolean"},
	}

	result := buildValidationResult(errs)

	records, ok := result.Errors[validationCategoryType]
	if !ok || len(records) != 3 {
		t.Fatalf("expected 3 type-category records, got: %v", result.Errors[validationCategoryType])
	}
	want := []string{"artifact.manifest", "release.auto_verify", "release.max_retained"}
	for i, key := range want {
		if records[i].Key != key {
			t.Errorf("record %d should be %q (sorted), got %q — full order: %v",
				i, key, records[i].Key, recordKeys(records))
		}
	}
}

// TestConfigValidateCommand_JSONDeterministic verifies that repeated
// --json runs over the same invalid configuration produce byte-identical
// output with records sorted by key within each category.
func TestConfigValidateCommand_JSONDeterministic(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	// Three type errors whose keys are intentionally NOT in sorted order
	// in the file, so any map-iteration ordering would be observable.
	configContent := `project:
  name: deterministic-test
release:
  max_retained: nope
  auto_verify: nope
artifact:
  manifest: nope
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
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

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to project directory %q: %v", dir, err)
	}

	_, stdout1, _, err := executeCommand("config", "validate", "--json")
	if err == nil {
		t.Fatal("expected error for invalid configuration, got nil")
	}
	_, stdout2, _, err := executeCommand("config", "validate", "--json")
	if err == nil {
		t.Fatal("expected error for invalid configuration, got nil")
	}
	if stdout1 != stdout2 {
		t.Errorf("repeated --json runs should produce identical output\n--- run 1 ---\n%s\n--- run 2 ---\n%s", stdout1, stdout2)
	}

	result, err := parseConfigValidationJSON(t, stdout1)
	if err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	typeRecords := result.Errors[validationCategoryType]
	want := []string{"artifact.manifest", "release.auto_verify", "release.max_retained"}
	if len(typeRecords) != len(want) {
		t.Fatalf("expected %d type records, got %d: %v", len(want), len(typeRecords), typeRecords)
	}
	for i, key := range want {
		if typeRecords[i].Key != key {
			t.Errorf("record %d should be %q (sorted), got %q — full order: %v",
				i, key, typeRecords[i].Key, recordKeys(typeRecords))
		}
	}
}

// recordKeys extracts the key of each record for assertion messages.
func recordKeys(records []configErrorRecord) []string {
	keys := make([]string, len(records))
	for i, r := range records {
		keys[i] = r.Key
	}
	return keys
}

// parseConfigValidationJSON decodes the machine-readable validation
// result from the standard output envelope.
func parseConfigValidationJSON(t *testing.T, stdout string) (configValidationResult, error) {
	t.Helper()

	var envelope struct {
		Version string `json:"version"`
		Status  string `json:"status"`
		Data    struct {
			Valid      bool                           `json:"valid"`
			ErrorCount int                            `json:"error_count"`
			Errors     map[string][]configErrorRecord `json:"errors"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		return configValidationResult{}, err
	}
	if envelope.Version != "1" {
		t.Errorf("JSON envelope should use version 1, got %q", envelope.Version)
	}
	if envelope.Status != "success" {
		t.Errorf("JSON envelope status should be 'success', got %q", envelope.Status)
	}
	return configValidationResult{
		Valid:      envelope.Data.Valid,
		ErrorCount: envelope.Data.ErrorCount,
		Errors:     envelope.Data.Errors,
	}, nil
}

// hasErrorRecord reports whether the records contain an entry for the
// given key.
func hasErrorRecord(records []configErrorRecord, key string) bool {
	for _, r := range records {
		if r.Key == key {
			return true
		}
	}
	return false
}
