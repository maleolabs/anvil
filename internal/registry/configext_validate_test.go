package registry

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/config"
)

// TestConfigExtensionContent_FrameworkConfigRules verifies the mechanical
// conversion of the standard's content into the validation engine's rule
// shape (TS-015-03-02): every declared key becomes a rule qualified under
// the framework's own namespace (framework.<namespace>.<name>, ADR-005
// §4.4) carrying the contract's required declaration — the rules come
// from the installed standard, never from runtime knowledge (ADR-026
// decision 2).
func TestConfigExtensionContent_FrameworkConfigRules(t *testing.T) {
	got := sampleConfigExtension().FrameworkConfigRules()
	want := []config.FrameworkConfigRule{
		{Key: "framework.laravel.version", Required: false},
		{Key: "framework.laravel.cache.store", Required: true},
		{Key: "framework.laravel.build_args", Required: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FrameworkConfigRules() = %+v, want %+v", got, want)
	}
}

// resolvedFrameworkConfig returns a resolved flat configuration carrying
// valid values for every key of the laravel sample content.
func resolvedFrameworkConfig() map[string]interface{} {
	return map[string]interface{}{
		"project.framework":             "laravel",
		"framework.laravel.version":     "11.0.0",
		"framework.laravel.cache.store": "redis",
		"framework.laravel.build_args":  "--no-dev",
	}
}

// TestValidateFrameworkConfig_Valid verifies the enforcement success case
// (TS-015-03-02 DoD: framework config is validated against the standard's
// rules): a resolved configuration satisfying the installed standard's
// declared rules passes without errors.
func TestValidateFrameworkConfig_Valid(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	rec.ConfigExtension = sampleConfigExtension()
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	errs, err := store.ValidateFrameworkConfig("laravel", resolvedFrameworkConfig())
	if err != nil {
		t.Fatalf("ValidateFrameworkConfig: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("ValidateFrameworkConfig returned %d errors for valid config: %v", len(errs), errs)
	}
}

// TestValidateFrameworkConfig_Invalid verifies that violations of the
// installed standard's declared rules are reported (TS-015-03-02 DoD:
// validation errors identify the offending key and the expected format):
// a missing required key and a non-string value each yield an error with
// the fully-qualified offending key and the expected format.
func TestValidateFrameworkConfig_Invalid(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	rec.ConfigExtension = sampleConfigExtension()
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	cfg := map[string]interface{}{
		"project.framework":             "laravel",
		"framework.laravel.version":     11, // non-string
		"framework.laravel.cache.store": "redis",
		// framework.laravel.build_args: missing required
	}
	errs, err := store.ValidateFrameworkConfig("laravel", cfg)
	if err != nil {
		t.Fatalf("ValidateFrameworkConfig: %v", err)
	}
	if len(errs) != 2 {
		t.Fatalf("ValidateFrameworkConfig returned %d errors, want 2: %v", len(errs), errs)
	}

	byKey := map[string]config.ValidationError{}
	for _, e := range errs {
		byKey[e.Key] = e
	}
	typeErr, ok := byKey["framework.laravel.version"]
	if !ok {
		t.Errorf("expected an error for framework.laravel.version, got: %v", byKey)
	} else if !strings.Contains(typeErr.Expected, "string") {
		t.Errorf("framework.laravel.version error should describe the expected string format, got %q", typeErr.Expected)
	}
	reqErr, ok := byKey["framework.laravel.build_args"]
	if !ok {
		t.Errorf("expected an error for framework.laravel.build_args, got: %v", byKey)
	} else if !strings.Contains(reqErr.Expected, "required") {
		t.Errorf("framework.laravel.build_args error should describe the expected required value, got %q", reqErr.Expected)
	}
}

// TestValidateFrameworkConfig_NoContent verifies the no-content outcome
// (command-contract §4.1): a standard that declares no config extension
// content supplies no rules — the framework section passes through with
// no errors (the init-time warning of the missing-extension handling
// lives in TS-015-03-01; enforcement here enforces only what the standard
// declares).
func TestValidateFrameworkConfig_NoContent(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	cfg := map[string]interface{}{
		"project.framework":         "laravel",
		"framework.laravel.version": "anything",
	}
	errs, err := store.ValidateFrameworkConfig("laravel", cfg)
	if err != nil {
		t.Fatalf("ValidateFrameworkConfig: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("ValidateFrameworkConfig returned %d errors for a standard without content: %v", len(errs), errs)
	}
}

// TestValidateFrameworkConfig_NotInstalled verifies the standard-missing
// hand-off (ADR-026 decision 3): a declared framework with no installed
// standard cannot be validated — the wrapped ErrStandardNotInstalled
// hands off to the caller's hard-fail semantics (never a silent
// pass-through).
func TestValidateFrameworkConfig_NotInstalled(t *testing.T) {
	store := newTestStore(t)

	cfg := map[string]interface{}{
		"project.framework":          "rails",
		"framework.rails.version":    "7.0.0",
		"framework.rails.cache":      "redis",
		"framework.rails.build_args": "--no-dev",
	}
	_, err := store.ValidateFrameworkConfig("rails", cfg)
	if err == nil {
		t.Fatal("ValidateFrameworkConfig expected an error for a missing standard, got nil")
	}
	if !errors.Is(err, ErrStandardNotInstalled) {
		t.Errorf("ValidateFrameworkConfig error = %v, want wrapped %v", err, ErrStandardNotInstalled)
	}
}

// TestValidateFrameworkConfig_CorruptRecord verifies that a store failure
// passes through as a real failure — never a silent pass-through.
func TestValidateFrameworkConfig_CorruptRecord(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := os.WriteFile(recordFilePath(store, rec.ID), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("corrupt record: %v", err)
	}

	cfg := map[string]interface{}{
		"project.framework":             "laravel",
		"framework.laravel.version":     "11.0.0",
		"framework.laravel.cache.store": "redis",
		"framework.laravel.build_args":  "--no-dev",
	}
	_, err := store.ValidateFrameworkConfig("laravel", cfg)
	if err == nil {
		t.Fatal("ValidateFrameworkConfig expected an error for a corrupt record, got nil")
	}
	if !errors.Is(err, ErrRecordCorrupt) {
		t.Errorf("ValidateFrameworkConfig error = %v, want wrapped %v", err, ErrRecordCorrupt)
	}
}

// TestValidateFrameworkConfig_NamespaceViolation verifies that a record
// whose content namespace does not match the declared framework is an
// actionable error — the record is inconsistent with the standard it
// belongs to (namespace isolation, C6).
func TestValidateFrameworkConfig_NamespaceViolation(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	content := sampleConfigExtension()
	content.Namespace = "rails"
	rec.ConfigExtension = content
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	cfg := map[string]interface{}{
		"project.framework": "laravel",
	}
	_, err := store.ValidateFrameworkConfig("laravel", cfg)
	if err == nil {
		t.Fatal("ValidateFrameworkConfig expected a namespace violation error, got nil")
	}
	if !strings.Contains(err.Error(), "namespace") {
		t.Errorf("ValidateFrameworkConfig error should identify the namespace violation, got: %v", err)
	}
}
