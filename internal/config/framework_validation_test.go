package config

import (
	"strings"
	"testing"
)

// frameworkValidationFixture returns the standard-supplied rules of a
// laravel config extension exercising every contract field (TS-015-03-02):
// a defaulted optional key, a required key with a default, and a required
// key without a default — mirroring registry sampleConfigExtension.
func frameworkValidationFixture() []FrameworkConfigRule {
	return []FrameworkConfigRule{
		{Key: "framework.laravel.version", Required: false},
		{Key: "framework.laravel.cache.store", Required: true},
		{Key: "framework.laravel.build_args", Required: true},
	}
}

// TestFrameworkConfigKey verifies the fully-qualified key convention of
// declared framework config keys (ADR-005 §4.4): framework.<namespace>.<name>.
func TestFrameworkConfigKey(t *testing.T) {
	if got, want := FrameworkConfigKey("laravel", "version"), "framework.laravel.version"; got != want {
		t.Errorf("FrameworkConfigKey(laravel, version) = %q, want %q", got, want)
	}
}

// TestValidateFrameworkRules_Valid verifies that a resolved configuration
// satisfying every standard-supplied rule passes without errors
// (TS-015-03-02 DoD: framework config is validated against the standard's
// rules): all required keys present with string values.
func TestValidateFrameworkRules_Valid(t *testing.T) {
	cfg := map[string]interface{}{
		"framework.laravel.version":     "11.0.0",
		"framework.laravel.cache.store": "redis",
		"framework.laravel.build_args":  "--no-dev",
	}
	if errs := ValidateFrameworkRules(frameworkValidationFixture(), cfg); len(errs) != 0 {
		t.Errorf("ValidateFrameworkRules returned %d errors for valid config: %v", len(errs), errs)
	}
}

// TestValidateFrameworkRules_OptionalAbsent verifies that a declared
// optional key absent from the resolved configuration is valid (the
// standard's default applies) — only required declarations are enforced.
func TestValidateFrameworkRules_OptionalAbsent(t *testing.T) {
	cfg := map[string]interface{}{
		"framework.laravel.cache.store": "redis",
		"framework.laravel.build_args":  "--no-dev",
	}
	if errs := ValidateFrameworkRules(frameworkValidationFixture(), cfg); len(errs) != 0 {
		t.Errorf("ValidateFrameworkRules returned %d errors when an optional key is absent: %v", len(errs), errs)
	}
}

// TestValidateFrameworkRules_RequiredMissing verifies the required
// enforcement (TS-015-03-02 DoD: validation errors identify the offending
// key and the expected format): a required key absent from the resolved
// configuration yields one error identifying the fully-qualified key and
// the expected value shape.
func TestValidateFrameworkRules_RequiredMissing(t *testing.T) {
	cfg := map[string]interface{}{
		"framework.laravel.version": "11.0.0",
	}
	errs := ValidateFrameworkRules(frameworkValidationFixture(), cfg)
	if len(errs) != 2 {
		t.Fatalf("ValidateFrameworkRules returned %d errors, want 2 (both required keys missing): %v", len(errs), errs)
	}

	seen := map[string]bool{}
	for _, err := range errs {
		if err.Key != "framework.laravel.cache.store" && err.Key != "framework.laravel.build_args" {
			t.Errorf("unexpected offending key %q, want framework.laravel.cache.store or framework.laravel.build_args", err.Key)
		}
		if seen[err.Key] {
			t.Errorf("offending key %q reported more than once", err.Key)
		}
		seen[err.Key] = true
		if !strings.HasPrefix(err.Expected, "required ") {
			t.Errorf("error for %q should describe the expected required value, got Expected %q", err.Key, err.Expected)
		}
	}
}

// TestValidateFrameworkRules_NonString verifies the string-only shape
// enforcement (the config extension contract declares string-only
// defaults and deliberately extends no other value type): a non-string
// value for a declared key yields an error identifying the offending key
// and the expected type.
func TestValidateFrameworkRules_NonString(t *testing.T) {
	cfg := map[string]interface{}{
		"framework.laravel.version":     11, // int, not string
		"framework.laravel.cache.store": "redis",
		"framework.laravel.build_args":  "--no-dev",
	}
	errs := ValidateFrameworkRules(frameworkValidationFixture(), cfg)
	if len(errs) != 1 {
		t.Fatalf("ValidateFrameworkRules returned %d errors, want 1: %v", len(errs), errs)
	}
	if errs[0].Key != "framework.laravel.version" {
		t.Errorf("error should identify the offending key framework.laravel.version, got %q", errs[0].Key)
	}
	if !strings.Contains(errs[0].Expected, "string") {
		t.Errorf("error should describe the expected string format, got Expected %q", errs[0].Expected)
	}
}

// TestValidateFrameworkRules_CollectsAll verifies the non-fail-fast
// collection of the engine (mirroring the canonical schema engine): every
// violation is reported in a single pass.
func TestValidateFrameworkRules_CollectsAll(t *testing.T) {
	cfg := map[string]interface{}{
		"framework.laravel.version":     true, // non-string
		"framework.laravel.cache.store": 42,   // non-string
		// framework.laravel.build_args: missing required
	}
	errs := ValidateFrameworkRules(frameworkValidationFixture(), cfg)
	if len(errs) != 3 {
		t.Fatalf("ValidateFrameworkRules returned %d errors, want 3: %v", len(errs), errs)
	}
}

// TestValidateFrameworkRules_NoRules verifies that a standard declaring no
// config extension rules enforces nothing: no rules, no errors.
func TestValidateFrameworkRules_NoRules(t *testing.T) {
	cfg := map[string]interface{}{
		"framework.laravel.version": "11.0.0",
	}
	if errs := ValidateFrameworkRules(nil, cfg); len(errs) != 0 {
		t.Errorf("ValidateFrameworkRules returned %d errors with no rules: %v", len(errs), errs)
	}
}

// TestValidateFrameworkRules_UndeclaredKeysPassThrough verifies the
// pass-through semantics (C6, command-contract §4.5): keys under the
// framework namespace that the standard does not declare are not
// interpreted by the runtime — the standard validates its own extended
// values.
func TestValidateFrameworkRules_UndeclaredKeysPassThrough(t *testing.T) {
	cfg := map[string]interface{}{
		"framework.laravel.version":        "11.0.0",
		"framework.laravel.cache.store":    "redis",
		"framework.laravel.build_args":     "--no-dev",
		"framework.laravel.undeclared.key": "any value",
	}
	if errs := ValidateFrameworkRules(frameworkValidationFixture(), cfg); len(errs) != 0 {
		t.Errorf("ValidateFrameworkRules returned %d errors for undeclared keys: %v", len(errs), errs)
	}
}
