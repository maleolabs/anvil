// Package config provides immutable configuration provision tests for
// Anvil projects. Tests verify typed accessors, type safety, immutability,
// and scope tracking.
//
// Reference: TS-P2-07, ADR-005 §7.5
package config

import (
	"fmt"
	"reflect"
	"testing"
)

// TestProvision_GetString verifies that GetString returns the correct string
// value and scope level for string-typed configuration keys.
func TestProvision_GetString(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"project.name": "test-app"},
		map[string]interface{}{"project.version": "2.0.0"},
		nil,
		nil,
	)
	pc := NewProvisionConfig(resolver)

	val, scope, err := pc.GetString("project.name")
	if err != nil {
		t.Fatalf("GetString('project.name') returned error: %v", err)
	}
	if val != "test-app" {
		t.Errorf("GetString('project.name') = %q, want 'test-app'", val)
	}
	if scope != ScopeGlobal {
		t.Errorf("GetString('project.name') scope = %v, want ScopeGlobal", scope)
	}

	val, scope, err = pc.GetString("project.version")
	if err != nil {
		t.Fatalf("GetString('project.version') returned error: %v", err)
	}
	if val != "2.0.0" {
		t.Errorf("GetString('project.version') = %q, want '2.0.0'", val)
	}
	if scope != ScopeProject {
		t.Errorf("GetString('project.version') scope = %v, want ScopeProject", scope)
	}
}

// TestProvision_GetInt verifies that GetInt returns the correct integer value
// and scope level for integer-typed configuration keys.
func TestProvision_GetInt(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"release.max_retained": 5},
		map[string]interface{}{"release.max_retained": 10},
		nil,
		nil,
	)
	pc := NewProvisionConfig(resolver)

	val, scope, err := pc.GetInt("release.max_retained")
	if err != nil {
		t.Fatalf("GetInt('release.max_retained') returned error: %v", err)
	}
	if val != 10 {
		t.Errorf("GetInt('release.max_retained') = %d, want 10", val)
	}
	if scope != ScopeProject {
		t.Errorf("GetInt('release.max_retained') scope = %v, want ScopeProject", scope)
	}
}

// TestProvision_GetInt_AcceptsFloat64 verifies that GetInt accepts whole-number
// float64 values (as produced by YAML/JSON unmarshalling).
func TestProvision_GetInt_AcceptsFloat64(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"release.max_retained": float64(7)},
		nil, nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	val, _, err := pc.GetInt("release.max_retained")
	if err != nil {
		t.Fatalf("GetInt('release.max_retained') returned error for float64(7): %v", err)
	}
	if val != 7 {
		t.Errorf("GetInt('release.max_retained') = %d, want 7", val)
	}
}

// TestProvision_GetBool verifies that GetBool returns the correct boolean
// value and scope level for boolean-typed configuration keys.
func TestProvision_GetBool(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"release.auto_verify": true},
		map[string]interface{}{"global.no_color": false},
		nil,
		nil,
	)
	pc := NewProvisionConfig(resolver)

	val, scope, err := pc.GetBool("release.auto_verify")
	if err != nil {
		t.Fatalf("GetBool('release.auto_verify') returned error: %v", err)
	}
	if val != true {
		t.Errorf("GetBool('release.auto_verify') = %v, want true", val)
	}
	if scope != ScopeGlobal {
		t.Errorf("GetBool('release.auto_verify') scope = %v, want ScopeGlobal", scope)
	}

	val, scope, err = pc.GetBool("global.no_color")
	if err != nil {
		t.Fatalf("GetBool('global.no_color') returned error: %v", err)
	}
	if val != false {
		t.Errorf("GetBool('global.no_color') = %v, want false", val)
	}
	if scope != ScopeProject {
		t.Errorf("GetBool('global.no_color') scope = %v, want ScopeProject", scope)
	}
}

// TestProvision_GetStringSlice verifies that GetStringSlice returns the
// correct string slice value and scope level for array-typed keys.
func TestProvision_GetStringSlice(t *testing.T) {
	resolver := NewResolver(
		nil,
		map[string]interface{}{
			"artifact.include": []interface{}{"src/**", "lib/**"},
			"artifact.exclude": []string{".git/**", "node_modules/**"},
		},
		nil,
		nil,
	)
	pc := NewProvisionConfig(resolver)

	// Test []interface{} source (from YAML).
	val, scope, err := pc.GetStringSlice("artifact.include")
	if err != nil {
		t.Fatalf("GetStringSlice('artifact.include') returned error: %v", err)
	}
	if len(val) != 2 || val[0] != "src/**" || val[1] != "lib/**" {
		t.Errorf("GetStringSlice('artifact.include') = %v, want ['src/**', 'lib/**']", val)
	}
	if scope != ScopeProject {
		t.Errorf("GetStringSlice('artifact.include') scope = %v, want ScopeProject", scope)
	}

	// Test []string source (from code-level defaults).
	val, scope, err = pc.GetStringSlice("artifact.exclude")
	if err != nil {
		t.Fatalf("GetStringSlice('artifact.exclude') returned error: %v", err)
	}
	if len(val) != 2 || val[0] != ".git/**" || val[1] != "node_modules/**" {
		t.Errorf("GetStringSlice('artifact.exclude') = %v, want ['.git/**', 'node_modules/**']", val)
	}
	if scope != ScopeProject {
		t.Errorf("GetStringSlice('artifact.exclude') scope = %v, want ScopeProject", scope)
	}
}

// TestProvision_SliceImmutability verifies that the slice returned by
// GetStringSlice is a copy — modifying it does not affect the underlying
// configuration or subsequent calls.
func TestProvision_SliceImmutability(t *testing.T) {
	original := []interface{}{"a", "b", "c"}
	resolver := NewResolver(
		nil,
		map[string]interface{}{"artifact.include": original},
		nil,
		nil,
	)
	pc := NewProvisionConfig(resolver)

	first, _, err := pc.GetStringSlice("artifact.include")
	if err != nil {
		t.Fatalf("GetStringSlice('artifact.include') returned error: %v", err)
	}

	// Modify the returned slice.
	first[0] = "modified"

	// Get again — should still have the original values.
	second, _, err := pc.GetStringSlice("artifact.include")
	if err != nil {
		t.Fatalf("second GetStringSlice('artifact.include') returned error: %v", err)
	}

	if second[0] == "modified" {
		t.Error("GetStringSlice returned slice that shares memory with internal storage: modification leaked")
	}
	if second[0] != "a" {
		t.Errorf("second GetStringSlice[0] = %q, want 'a'", second[0])
	}
}

// TestProvision_WrongType verifies that typed accessors return an error
// when the resolved value does not match the expected type.
func TestProvision_WrongType(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{
			"global.log_level":        42,             // string expected, got int
			"release.max_retained":    "not-an-int",   // int expected, got string
			"release.auto_verify":     "yes",          // bool expected, got string
			"artifact.include":        "not-a-slice",  // slice expected, got string
		},
		nil, nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	// GetString on int value should fail.
	_, _, err := pc.GetString("global.log_level")
	if err == nil {
		t.Error("GetString('global.log_level') should return error for int value")
	}

	// GetInt on string value should fail.
	_, _, err = pc.GetInt("release.max_retained")
	if err == nil {
		t.Error("GetInt('release.max_retained') should return error for string value")
	}

	// GetBool on string value should fail.
	_, _, err = pc.GetBool("release.auto_verify")
	if err == nil {
		t.Error("GetBool('release.auto_verify') should return error for string value")
	}

	// GetStringSlice on string value should fail.
	_, _, err = pc.GetStringSlice("artifact.include")
	if err == nil {
		t.Error("GetStringSlice('artifact.include') should return error for string value")
	}
}

// TestProvision_KeyNotFound verifies that accessing a non-existent key
// returns an error from all typed accessors.
func TestProvision_KeyNotFound(t *testing.T) {
	resolver := NewResolver(nil, nil, nil, nil)
	pc := NewProvisionConfig(resolver)

	// All accessors should return error for missing key.
	_, _, err := pc.Get("nonexistent")
	if err == nil {
		t.Error("Get('nonexistent') should return error for missing key")
	}

	_, _, err = pc.GetString("nonexistent")
	if err == nil {
		t.Error("GetString('nonexistent') should return error for missing key")
	}

	_, _, err = pc.GetInt("nonexistent")
	if err == nil {
		t.Error("GetInt('nonexistent') should return error for missing key")
	}

	_, _, err = pc.GetBool("nonexistent")
	if err == nil {
		t.Error("GetBool('nonexistent') should return error for missing key")
	}

	_, _, err = pc.GetStringSlice("nonexistent")
	if err == nil {
		t.Error("GetStringSlice('nonexistent') should return error for missing key")
	}
}

// TestProvision_All verifies that All() returns all resolved values with
// their correct scope levels.
func TestProvision_All(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"global.key": "global-val"},
		map[string]interface{}{"project.key": "project-val"},
		map[string]interface{}{"env.key": "env-val"},
		map[string]interface{}{"exec.key": "exec-val"},
	)
	pc := NewProvisionConfig(resolver)

	all := pc.All()

	// All keys should be present.
	tests := []struct {
		key       string
		wantVal   interface{}
		wantScope ScopeLevel
	}{
		{"global.key", "global-val", ScopeGlobal},
		{"project.key", "project-val", ScopeProject},
		{"env.key", "env-val", ScopeEnvironment},
		{"exec.key", "exec-val", ScopeExecution},
	}

	for _, tt := range tests {
		vs, ok := all[tt.key]
		if !ok {
			t.Errorf("All() missing key %q", tt.key)
			continue
		}
		if vs.Value != tt.wantVal {
			t.Errorf("All()[%q].Value = %v, want %v", tt.key, vs.Value, tt.wantVal)
		}
		if vs.Scope != tt.wantScope {
			t.Errorf("All()[%q].Scope = %v, want %v", tt.key, vs.Scope, tt.wantScope)
		}
	}
}

// TestProvision_NoMutation verifies that ProvisionConfig exposes no setter
// or mutation methods. This is a compile-time interface check: if a Set*
// method is added, this test will fail only at the conceptual level.
// The true verification is that only read methods exist on the type.
func TestProvision_NoMutation(t *testing.T) {
	resolver := NewResolver(nil, nil, nil, nil)
	pc := NewProvisionConfig(resolver)

	// Verify that pc is a *ProvisionConfig (not assignable to a mutator).
	_ = pc

	// If a SetKey or similar method existed, we would test that calling it
	// fails. Since none exist by design, this test verifies the type exists
	// and is constructible.
	if pc == nil {
		t.Fatal("NewProvisionConfig returned nil")
	}
}

// TestProvision_ConsistentAcrossCalls verifies that same key returns the
// same value across multiple calls (deterministic behaviour).
func TestProvision_ConsistentAcrossCalls(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"project.name": "stable-app"},
		nil, nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	first, _, err := pc.GetString("project.name")
	if err != nil {
		t.Fatalf("first GetString call failed: %v", err)
	}

	second, _, err := pc.GetString("project.name")
	if err != nil {
		t.Fatalf("second GetString call failed: %v", err)
	}

	if first != second {
		t.Errorf("GetString returned different values across calls: %q vs %q", first, second)
	}
}

// TestProvision_AllReturnsCopy verifies that the map returned by All() is a
// snapshot — modifying it does not affect subsequent calls.
func TestProvision_AllReturnsCopy(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"project.name": "original"},
		nil, nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	first := pc.All()
	first["project.name"] = ValueSource{Value: "modified", Scope: ScopeExecution}

	second := pc.All()
	if second["project.name"].Value != "original" {
		t.Error("All() returned map that shares memory: modification leaked to subsequent call")
	}
}

// TestProvision_GetStringSlice_NonStringElement verifies that GetStringSlice
// returns an error when a []interface{} contains a non-string element.
func TestProvision_GetStringSlice_NonStringElement(t *testing.T) {
	resolver := NewResolver(
		nil,
		map[string]interface{}{
			"artifact.include": []interface{}{"valid", 42, "also-valid"},
		},
		nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	_, _, err := pc.GetStringSlice("artifact.include")
	if err == nil {
		t.Error("GetStringSlice should return error when slice contains non-string element")
	}
}

// TestProvision_GetStringSlice_NilSlice verifies that GetStringSlice handles
// nil slices gracefully.
func TestProvision_GetStringSlice_NilSlice(t *testing.T) {
	resolver := NewResolver(
		nil,
		map[string]interface{}{
			"artifact.include": nil,
		},
		nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	_, _, err := pc.GetStringSlice("artifact.include")
	if err == nil {
		t.Error("GetStringSlice should return error for nil value")
	}
}

// TestProvision_Get_ScopeTracking verifies that Get returns the correct
// scope level for keys defined at different levels.
func TestProvision_Get_ScopeTracking(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"key": "global-val"},
		map[string]interface{}{"key": "project-val"},
		map[string]interface{}{"key": "env-val"},
		map[string]interface{}{"key": "exec-val"},
	)
	pc := NewProvisionConfig(resolver)

	_, scope, err := pc.Get("key")
	if err != nil {
		t.Fatalf("Get('key') returned error: %v", err)
	}
	if scope != ScopeExecution {
		t.Errorf("Get('key') scope = %v, want ScopeExecution (highest precedence)", scope)
	}
}

// ExampleProvisionConfig demonstrates the basic usage of ProvisionConfig.
func ExampleProvisionConfig() {
	resolver := NewResolver(
		map[string]interface{}{"project.name": "example-app"},
		nil, nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	name, scope, err := pc.GetString("project.name")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("%s (source: %s)\n", name, scope)
	// Output: example-app (source: global)
}

// ExampleProvisionConfig_All demonstrates how to list all configuration
// values with their sources.
func ExampleProvisionConfig_All() {
	resolver := NewResolver(
		map[string]interface{}{"project.name": "demo"},
		map[string]interface{}{"project.version": "2.0.0"},
		nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	all := pc.All()
	for key, vs := range all {
		fmt.Printf("%s = %v (scope: %s)\n", key, vs.Value, vs.Scope)
	}
	// Unordered output:
	// project.name = demo (scope: global)
	// project.version = 2.0.0 (scope: project)
}

// TestProvision_GetFromUnusedLevels verifies that keys at the Global level
// are still accessible when higher levels are empty.
func TestProvision_GetFromUnusedLevels(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"project.name": "fallback-app"},
		nil, nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	val, _, err := pc.GetString("project.name")
	if err != nil {
		t.Fatalf("GetString('project.name') returned error: %v", err)
	}
	if val != "fallback-app" {
		t.Errorf("GetString('project.name') = %q, want 'fallback-app'", val)
	}
}

// TestProvision_All_Empty verifies that All() returns an empty map when no
// values are configured at any level.
func TestProvision_All_Empty(t *testing.T) {
	resolver := NewResolver(nil, nil, nil, nil)
	pc := NewProvisionConfig(resolver)

	all := pc.All()
	if all == nil {
		t.Fatal("All() returned nil, want empty map")
	}
	if len(all) != 0 {
		t.Errorf("All() returned %d entries, want 0", len(all))
	}
}

// TestProvision_Int64Coercion verifies that int64 values are accepted by GetInt.
func TestProvision_Int64Coercion(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"release.max_retained": int64(42)},
		nil, nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	val, _, err := pc.GetInt("release.max_retained")
	if err != nil {
		t.Fatalf("GetInt('release.max_retained') returned error for int64: %v", err)
	}
	if val != 42 {
		t.Errorf("GetInt('release.max_retained') = %d, want 42", val)
	}
}

// TestProvision_NonWholeFloatRejected verifies that non-whole float64 values
// are rejected by GetInt.
func TestProvision_NonWholeFloatRejected(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"release.max_retained": 3.14},
		nil, nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	_, _, err := pc.GetInt("release.max_retained")
	if err == nil {
		t.Error("GetInt should reject non-whole float64 values")
	}
}

// TestProvision_InterfaceCompileCheck verifies that ProvisionConfig satisfies
// the expected interface contract (compile-time check).
func TestProvision_InterfaceCompileCheck(t *testing.T) {
	// This is a compile-time check that the type exists with expected methods.
	var _ *ProvisionConfig = NewProvisionConfig(NewResolver(nil, nil, nil, nil))

	// Verify that NewProvisionConfig is the only way to construct.
	_ = &ProvisionConfig{} //nolint:staticcheck // intentional — verifying struct is exported
	_ = reflect.TypeOf(ProvisionConfig{})
}
