// Tests for the Laravel adapter configuration extension (TS-P7-12): the
// declared keys under the "framework.laravel." namespace and the
// validation rules for Laravel-specific values.
package laravel

import (
	"runtime"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// TestConfigExtension_DeclaredKeys verifies that the extension declares
// the three Laravel keys under the isolated "framework.laravel." namespace
// with descriptions and defaults (TS-P7-12 AC-1, AC-2).
func TestConfigExtension_DeclaredKeys(t *testing.T) {
	result := ConfigExtension()

	if result.Extension.Framework != Framework {
		t.Errorf("Extension.Framework = %q, want %q", result.Extension.Framework, Framework)
	}

	wantKeys := []string{KeyMigrationsPath, KeyCacheStore, KeyVersion}
	if len(result.Extension.Keys) != len(wantKeys) {
		t.Fatalf("Extension.Keys length = %d, want %d", len(result.Extension.Keys), len(wantKeys))
	}

	seen := make(map[string]bool, len(result.Extension.Keys))
	for i, key := range result.Extension.Keys {
		if key.Name != wantKeys[i] {
			t.Errorf("Extension.Keys[%d].Name = %q, want %q", i, key.Name, wantKeys[i])
		}
		if !strings.HasPrefix(key.Name, "framework.laravel.") {
			t.Errorf("Extension.Keys[%d].Name = %q, want prefix \"framework.laravel.\"", i, key.Name)
		}
		if key.Description == "" {
			t.Errorf("Extension.Keys[%d].Description = empty, want a description", i)
		}
		if seen[key.Name] {
			t.Errorf("duplicate key %q", key.Name)
		}
		seen[key.Name] = true
	}

	// The migrations path and cache store keys declare Laravel defaults.
	byName := map[string]contracts.ConfigKey{}
	for _, key := range result.Extension.Keys {
		byName[key.Name] = key
	}
	if byName[KeyMigrationsPath].Default != "database/migrations" {
		t.Errorf("KeyMigrationsPath default = %q, want \"database/migrations\"", byName[KeyMigrationsPath].Default)
	}
	if byName[KeyCacheStore].Default != "file" {
		t.Errorf("KeyCacheStore default = %q, want \"file\"", byName[KeyCacheStore].Default)
	}
}

// TestValidateConfigValues_Valid verifies that valid Laravel values pass
// validation (TS-P7-12 AC-3). A path with an internal ".." segment that
// cleans to a non-escaping path (e.g. "database/../migrations" →
// "migrations") is accepted: it stays inside the release directory.
func TestValidateConfigValues_Valid(t *testing.T) {
	req := contracts.ConfigValidationRequest{
		Values: []contracts.ConfigValue{
			{Key: KeyMigrationsPath, Value: "database/migrations"},
			{Key: KeyMigrationsPath, Value: "database/../migrations"},
			{Key: KeyCacheStore, Value: "redis"},
			{Key: KeyVersion, Value: "11.0.0"},
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

// TestValidateConfigValues_BackslashTraversalWindows verifies that a
// backslash traversal segment ("..\x") is rejected on Windows, where the
// backslash is the path separator. On other platforms a backslash is an
// ordinary filename character and is not a traversal, so the test only
// applies to Windows (cross-platform behavior of filepath.Clean).
func TestValidateConfigValues_BackslashTraversalWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("backslash is a path separator only on Windows")
	}

	result := ValidateConfigValues(contracts.ConfigValidationRequest{
		Values: []contracts.ConfigValue{
			{Key: KeyMigrationsPath, Value: `..\database\migrations`},
		},
	})
	if result.Valid {
		t.Fatal("Valid = true, want false for a backslash traversal on Windows")
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "traversal") {
		t.Errorf("Errors = %v, want one traversal error", result.Errors)
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
// Laravel value: migrations path must be a non-empty relative path, cache
// store must be a known driver, version must be SemVer-compatible
// (TS-P7-12 AC-3).
func TestValidateConfigValues_Invalid(t *testing.T) {
	tests := []struct {
		name       string
		value      contracts.ConfigValue
		wantDetail string
	}{
		{
			name:       "migrations_path_empty",
			value:      contracts.ConfigValue{Key: KeyMigrationsPath, Value: ""},
			wantDetail: "must not be empty",
		},
		{
			name:       "migrations_path_absolute",
			value:      contracts.ConfigValue{Key: KeyMigrationsPath, Value: "/var/www/migrations"},
			wantDetail: "relative path",
		},
		{
			name:       "migrations_path_traversal",
			value:      contracts.ConfigValue{Key: KeyMigrationsPath, Value: "../database/migrations"},
			wantDetail: "traversal",
		},
		{
			name:       "migrations_path_dotdot",
			value:      contracts.ConfigValue{Key: KeyMigrationsPath, Value: ".."},
			wantDetail: "traversal",
		},
		{
			name:       "migrations_path_nested_traversal",
			value:      contracts.ConfigValue{Key: KeyMigrationsPath, Value: "../../database/migrations"},
			wantDetail: "traversal",
		},
		{
			name:       "migrations_path_cleaned_traversal",
			value:      contracts.ConfigValue{Key: KeyMigrationsPath, Value: "database/../../migrations"},
			wantDetail: "traversal",
		},
		{
			name:       "cache_store_unknown",
			value:      contracts.ConfigValue{Key: KeyCacheStore, Value: "memory-disk"},
			wantDetail: "not a known Laravel cache store",
		},
		{
			name:       "cache_store_empty",
			value:      contracts.ConfigValue{Key: KeyCacheStore, Value: ""},
			wantDetail: "must not be empty",
		},
		{
			name:       "version_invalid",
			value:      contracts.ConfigValue{Key: KeyVersion, Value: "11"},
			wantDetail: "not valid SemVer",
		},
		{
			name:       "version_v_prefix",
			value:      contracts.ConfigValue{Key: KeyVersion, Value: "v11.0.0"},
			wantDetail: "not valid SemVer",
		},
		{
			name:       "unknown_key",
			value:      contracts.ConfigValue{Key: "framework.laravel.php_version", Value: "8.3"},
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
			{Key: KeyMigrationsPath, Value: ""},
			{Key: KeyCacheStore, Value: "unknown-driver"},
			{Key: KeyVersion, Value: "x.y.z"},
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
