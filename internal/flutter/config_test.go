// Tests for the Flutter adapter configuration extension (TS-P7-26): the
// declared keys under the "framework.flutter." namespace and the
// validation rules for Flutter-specific values.
package flutter

import (
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// TestConfigExtension_DeclaredKeys verifies that the extension declares
// the two Flutter keys under the isolated "framework.flutter." namespace
// with descriptions (TS-P7-26 AC-1, AC-2). The targets key declares the
// default "web,apk" — the array default ["web","apk"] represented as a
// comma-separated string; build_args declares no default: it is an
// optional key.
func TestConfigExtension_DeclaredKeys(t *testing.T) {
	result := ConfigExtension()

	if result.Extension.Framework != Framework {
		t.Errorf("Extension.Framework = %q, want %q", result.Extension.Framework, Framework)
	}

	wantKeys := []string{KeyTargets, KeyBuildArgs}
	if len(result.Extension.Keys) != len(wantKeys) {
		t.Fatalf("Extension.Keys length = %d, want %d", len(result.Extension.Keys), len(wantKeys))
	}

	seen := make(map[string]bool, len(result.Extension.Keys))
	for i, key := range result.Extension.Keys {
		if key.Name != wantKeys[i] {
			t.Errorf("Extension.Keys[%d].Name = %q, want %q", i, key.Name, wantKeys[i])
		}
		if !strings.HasPrefix(key.Name, "framework.flutter.") {
			t.Errorf("Extension.Keys[%d].Name = %q, want prefix \"framework.flutter.\"", i, key.Name)
		}
		if key.Description == "" {
			t.Errorf("Extension.Keys[%d].Description = empty, want a description", i)
		}
		if seen[key.Name] {
			t.Errorf("duplicate key %q", key.Name)
		}
		seen[key.Name] = true
	}

	// The targets key declares the comma-separated default "web,apk";
	// build_args is optional and declares none.
	byName := map[string]contracts.ConfigKey{}
	for _, key := range result.Extension.Keys {
		byName[key.Name] = key
	}
	if byName[KeyTargets].Default != "web,apk" {
		t.Errorf("KeyTargets default = %q, want \"web,apk\"", byName[KeyTargets].Default)
	}
	if byName[KeyBuildArgs].Default != "" {
		t.Errorf("KeyBuildArgs default = %q, want empty (optional key)", byName[KeyBuildArgs].Default)
	}
}

// TestValidateConfigValues_Valid verifies that valid Flutter values pass
// validation (TS-P7-26 AC-3): targets as comma-separated known target
// lists, and build_args as empty or a safe whitespace-separated argument
// string.
func TestValidateConfigValues_Valid(t *testing.T) {
	req := contracts.ConfigValidationRequest{
		Values: []contracts.ConfigValue{
			{Key: KeyTargets, Value: "web,apk"},
			{Key: KeyTargets, Value: "ios"},
			{Key: KeyTargets, Value: "web,apk,ios"},
			{Key: KeyBuildArgs, Value: ""},
			{Key: KeyBuildArgs, Value: "--release --split-per-abi"},
		},
	}

	result := ValidateConfigValues(req)
	if !result.Valid {
		t.Fatalf("Valid = false, want true (errors: %v)", result.Errors)
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want empty", result.Errors)
	}
}

// TestValidateConfigValues_Empty verifies that an empty request is valid
// — no values, no errors.
func TestValidateConfigValues_Empty(t *testing.T) {
	result := ValidateConfigValues(contracts.ConfigValidationRequest{})
	if !result.Valid {
		t.Errorf("Valid = false, want true (errors: %v)", result.Errors)
	}
}

// TestValidateConfigValues_Invalid verifies the validation rules for each
// Flutter value: targets must be a non-empty comma-separated list of
// known target names (empty tokens are malformed, unknown names are
// rejected), build_args must be a safe argument string when present, and
// unknown keys are rejected (TS-P7-26 AC-3).
func TestValidateConfigValues_Invalid(t *testing.T) {
	tests := []struct {
		name       string
		value      contracts.ConfigValue
		wantDetail string
	}{
		{
			name:       "targets_empty",
			value:      contracts.ConfigValue{Key: KeyTargets, Value: ""},
			wantDetail: "must not be empty",
		},
		{
			name:       "targets_unknown",
			value:      contracts.ConfigValue{Key: KeyTargets, Value: "winphone"},
			wantDetail: "not a known Flutter target",
		},
		{
			name:       "targets_unknown_inline",
			value:      contracts.ConfigValue{Key: KeyTargets, Value: "web,winphone,apk"},
			wantDetail: "not a known Flutter target",
		},
		{
			name:       "targets_malformed_leading_comma",
			value:      contracts.ConfigValue{Key: KeyTargets, Value: ",web"},
			wantDetail: "empty target name",
		},
		{
			name:       "targets_malformed_trailing_comma",
			value:      contracts.ConfigValue{Key: KeyTargets, Value: "web,"},
			wantDetail: "empty target name",
		},
		{
			name:       "targets_malformed_double_comma",
			value:      contracts.ConfigValue{Key: KeyTargets, Value: "web,,apk"},
			wantDetail: "empty target name",
		},
		{
			name:       "targets_whitespace_token",
			value:      contracts.ConfigValue{Key: KeyTargets, Value: "web, apk"},
			wantDetail: "not a known Flutter target",
		},
		{
			name:       "build_args_semicolon",
			value:      contracts.ConfigValue{Key: KeyBuildArgs, Value: "a;rm -rf /"},
			wantDetail: "shell metacharacters",
		},
		{
			name:       "build_args_command_substitution",
			value:      contracts.ConfigValue{Key: KeyBuildArgs, Value: "--dart-define=$(id)"},
			wantDetail: "shell metacharacters",
		},
		{
			name:       "build_args_pipe",
			value:      contracts.ConfigValue{Key: KeyBuildArgs, Value: "--release | sh"},
			wantDetail: "shell metacharacters",
		},
		{
			name:       "unknown_key",
			value:      contracts.ConfigValue{Key: "framework.flutter.unknown_key", Value: "x"},
			wantDetail: "unknown configuration key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateConfigValues(contracts.ConfigValidationRequest{Values: []contracts.ConfigValue{tt.value}})
			if result.Valid {
				t.Fatal("Valid = true, want false")
			}
			if len(result.Errors) != 1 {
				t.Fatalf("Errors = %v, want exactly one error", result.Errors)
			}
			if !strings.Contains(result.Errors[0], tt.value.Key) {
				t.Errorf("Error = %q, want mention of the key %q", result.Errors[0], tt.value.Key)
			}
			if !strings.Contains(result.Errors[0], tt.wantDetail) {
				t.Errorf("Error = %q, want it to contain %q", result.Errors[0], tt.wantDetail)
			}
		})
	}
}

// TestValidateConfigValues_MultipleErrors verifies that all invalid
// values are reported, not just the first.
func TestValidateConfigValues_MultipleErrors(t *testing.T) {
	req := contracts.ConfigValidationRequest{
		Values: []contracts.ConfigValue{
			{Key: KeyTargets, Value: ""},
			{Key: KeyTargets, Value: "winphone"},
			{Key: KeyBuildArgs, Value: "--release;rm -rf /"},
		},
	}

	result := ValidateConfigValues(req)
	if result.Valid {
		t.Fatal("Valid = true, want false")
	}
	if len(result.Errors) != 3 {
		t.Errorf("Errors length = %d, want 3 (errors: %v)", len(result.Errors), result.Errors)
	}
}
