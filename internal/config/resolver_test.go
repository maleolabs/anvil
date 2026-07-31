// Package config provides configuration resolution engine tests for Anvil
// projects. Tests cover deterministic precedence, partial overrides, level
// isolation, and edge cases.
//
// Reference: TS-P2-06, ADR-005 §7.5
package config

import (
	"reflect"
	"testing"
)

// TestResolver_ExecutionOverridesAll verifies that values at the Execution
// level override values at all other levels (Environment, Project, Global).
//
// Covers AC 1: Execution-level values override all other levels.
func TestResolver_ExecutionOverridesAll(t *testing.T) {
	global := map[string]interface{}{"key": "global-val"}
	project := map[string]interface{}{"key": "project-val"}
	environment := map[string]interface{}{"key": "env-val"}
	execution := map[string]interface{}{"key": "exec-val"}

	r := NewResolver(global, project, environment, execution)

	val, level := r.Resolve("key")
	if val != "exec-val" {
		t.Errorf("Resolve('key') = %v, want 'exec-val'", val)
	}
	if level != ScopeExecution {
		t.Errorf("Resolve('key') level = %v, want ScopeExecution", level)
	}

	merged := r.ResolveAll()
	if merged["key"] != "exec-val" {
		t.Errorf("ResolveAll()['key'] = %v, want 'exec-val'", merged["key"])
	}
}

// TestResolver_EnvironmentOverridesProjectAndGlobal verifies that values at
// the Environment level override Project and Global values, but are themselves
// overridden by Execution values.
//
// Covers AC 2: Environment-level values override Project and Global.
func TestResolver_EnvironmentOverridesProjectAndGlobal(t *testing.T) {
	global := map[string]interface{}{"key": "global-val"}
	project := map[string]interface{}{"key": "project-val"}
	environment := map[string]interface{}{"key": "env-val"}
	execution := map[string]interface{}{} // empty — no execution override

	r := NewResolver(global, project, environment, execution)

	val, level := r.Resolve("key")
	if val != "env-val" {
		t.Errorf("Resolve('key') = %v, want 'env-val'", val)
	}
	if level != ScopeEnvironment {
		t.Errorf("Resolve('key') level = %v, want ScopeEnvironment", level)
	}
}

// TestResolver_ProjectOverridesGlobal verifies that values at the Project
// level override Global defaults.
//
// Covers AC 3: Project-level values override Global defaults.
func TestResolver_ProjectOverridesGlobal(t *testing.T) {
	global := map[string]interface{}{"key": "global-val"}
	project := map[string]interface{}{"key": "project-val"}
	environment := map[string]interface{}{} // empty
	execution := map[string]interface{}{}   // empty

	r := NewResolver(global, project, environment, execution)

	val, level := r.Resolve("key")
	if val != "project-val" {
		t.Errorf("Resolve('key') = %v, want 'project-val'", val)
	}
	if level != ScopeProject {
		t.Errorf("Resolve('key') level = %v, want ScopeProject", level)
	}
}

// TestResolver_GlobalDefaultsApplied verifies that when no higher priority
// level provides a value, the Global level value (default) is returned.
//
// Covers AC 4: Global defaults apply when no higher priority level provides value.
func TestResolver_GlobalDefaultsApplied(t *testing.T) {
	global := map[string]interface{}{"key": "default-val"}
	project := map[string]interface{}{} // empty
	environment := map[string]interface{}{}
	execution := map[string]interface{}{}

	r := NewResolver(global, project, environment, execution)

	val, level := r.Resolve("key")
	if val != "default-val" {
		t.Errorf("Resolve('key') = %v, want 'default-val'", val)
	}
	if level != ScopeGlobal {
		t.Errorf("Resolve('key') level = %v, want ScopeGlobal", level)
	}
}

// TestResolver_PartialOverrides verifies that a higher-priority level with
// only a subset of keys overrides only those keys; other keys fall through
// to lower levels.
//
// Covers AC 5: Partial overrides supported — env with 1 key overrides only that key.
func TestResolver_PartialOverrides(t *testing.T) {
	global := map[string]interface{}{
		"key-a": "global-a",
		"key-b": "global-b",
		"key-c": "global-c",
	}
	project := map[string]interface{}{
		"key-a": "project-a", // overrides key-a globally
	}
	environment := map[string]interface{}{
		"key-b": "env-b", // overrides key-b only; key-a already project, key-c stays global
	}
	execution := map[string]interface{}{} // no execution overrides

	r := NewResolver(global, project, environment, execution)

	merged := r.ResolveAll()

	if merged["key-a"] != "project-a" {
		t.Errorf("ResolveAll()['key-a'] = %v, want 'project-a' (project overrides global)", merged["key-a"])
	}
	if merged["key-b"] != "env-b" {
		t.Errorf("ResolveAll()['key-b'] = %v, want 'env-b' (env overrides project)", merged["key-b"])
	}
	if merged["key-c"] != "global-c" {
		t.Errorf("ResolveAll()['key-c'] = %v, want 'global-c' (falls through to global)", merged["key-c"])
	}
}

// TestResolver_Deterministic verifies that the same inputs always produce
// the same output. The test runs ResolveAll twice and compares results.
//
// Covers AC 6: Same sources always produce same result (run twice, compare).
func TestResolver_Deterministic(t *testing.T) {
	global := map[string]interface{}{
		"global.a": 1,
		"global.b": "two",
	}
	project := map[string]interface{}{
		"project.a": "override",
	}
	environment := map[string]interface{}{
		"env.a": true,
	}
	execution := map[string]interface{}{
		"exec.a": 42,
	}

	r := NewResolver(global, project, environment, execution)

	first := r.ResolveAll()
	second := r.ResolveAll()

	if !reflect.DeepEqual(first, second) {
		t.Errorf("ResolveAll() results differ between calls\nfirst:  %v\nsecond: %v", first, second)
	}
}

// TestResolver_LevelIsolation verifies that a value stored at one level
// does not appear when querying a different level. Each level map is
// independent.
//
// Covers AC 7: Values at one level do not leak into another level.
func TestResolver_LevelIsolation(t *testing.T) {
	global := map[string]interface{}{"key": "global"}
	project := map[string]interface{}{"key": "project"}
	environment := map[string]interface{}{"key": "environment"}
	execution := map[string]interface{}{"key": "execution"}

	r := NewResolver(global, project, environment, execution)

	// LevelMap should return only the values for that specific level.
	if v := r.LevelMap(ScopeGlobal)["key"]; v != "global" {
		t.Errorf("LevelMap(ScopeGlobal)['key'] = %v, want 'global'", v)
	}
	if v := r.LevelMap(ScopeProject)["key"]; v != "project" {
		t.Errorf("LevelMap(ScopeProject)['key'] = %v, want 'project'", v)
	}
	if v := r.LevelMap(ScopeEnvironment)["key"]; v != "environment" {
		t.Errorf("LevelMap(ScopeEnvironment)['key'] = %v, want 'environment'", v)
	}
	if v := r.LevelMap(ScopeExecution)["key"]; v != "execution" {
		t.Errorf("LevelMap(ScopeExecution)['key'] = %v, want 'execution'", v)
	}
}

// TestResolver_NoValues verifies that when no values exist at any level,
// Resolve returns (nil, -1) and ResolveAll returns an empty map.
//
// Covers edge case: no values at any level.
func TestResolver_NoValues(t *testing.T) {
	r := NewResolver(nil, nil, nil, nil)

	val, level := r.Resolve("anything")
	if val != nil {
		t.Errorf("Resolve('anything') value = %v, want nil", val)
	}
	if level != -1 {
		t.Errorf("Resolve('anything') level = %v, want -1", level)
	}

	merged := r.ResolveAll()
	if len(merged) != 0 {
		t.Errorf("ResolveAll() returned %d entries, want 0", len(merged))
	}
}

// TestResolver_EmptyLevels verifies that empty maps at some levels don't
// cause issues — values from non-empty levels are still resolved correctly.
//
// Covers edge case: some levels have empty maps.
func TestResolver_EmptyLevels(t *testing.T) {
	global := map[string]interface{}{"key": "global"}
	project := map[string]interface{}{}   // empty
	environment := map[string]interface{}{} // empty
	execution := map[string]interface{}{}   // empty

	r := NewResolver(global, project, environment, execution)

	val, level := r.Resolve("key")
	if val != "global" {
		t.Errorf("Resolve('key') = %v, want 'global'", val)
	}
	if level != ScopeGlobal {
		t.Errorf("Resolve('key') level = %v, want ScopeGlobal", level)
	}

	// Verify unknown key returns nil/-1 even when levels are partially empty.
	val, level = r.Resolve("nonexistent")
	if val != nil {
		t.Errorf("Resolve('nonexistent') value = %v, want nil", val)
	}
	if level != -1 {
		t.Errorf("Resolve('nonexistent') level = %v, want -1", level)
	}
}

// TestResolver_ResolveReturnsCorrectLevel verifies that Resolve returns the
// correct ScopeLevel for each precedence level when a key exists at multiple
// levels.
//
// Covers: Verify Resolve() returns the correct ScopeLevel.
func TestResolver_ResolveReturnsCorrectLevel(t *testing.T) {
	tests := []struct {
		name      string
		global    map[string]interface{}
		project   map[string]interface{}
		env       map[string]interface{}
		exec      map[string]interface{}
		key       string
		wantVal   interface{}
		wantLevel ScopeLevel
	}{
		{
			name:      "key only in global",
			global:    map[string]interface{}{"k": "g"},
			project:   nil,
			env:       nil,
			exec:      nil,
			key:       "k",
			wantVal:   "g",
			wantLevel: ScopeGlobal,
		},
		{
			name:      "key in global and project returns project",
			global:    map[string]interface{}{"k": "g"},
			project:   map[string]interface{}{"k": "p"},
			env:       nil,
			exec:      nil,
			key:       "k",
			wantVal:   "p",
			wantLevel: ScopeProject,
		},
		{
			name:      "key in global, project, and env returns env",
			global:    map[string]interface{}{"k": "g"},
			project:   map[string]interface{}{"k": "p"},
			env:       map[string]interface{}{"k": "e"},
			exec:      nil,
			key:       "k",
			wantVal:   "e",
			wantLevel: ScopeEnvironment,
		},
		{
			name:      "key in all four returns execution",
			global:    map[string]interface{}{"k": "g"},
			project:   map[string]interface{}{"k": "p"},
			env:       map[string]interface{}{"k": "e"},
			exec:      map[string]interface{}{"k": "x"},
			key:       "k",
			wantVal:   "x",
			wantLevel: ScopeExecution,
		},
		{
			name:      "key not in any level returns -1",
			global:    map[string]interface{}{"a": "1"},
			project:   map[string]interface{}{"b": "2"},
			env:       map[string]interface{}{"c": "3"},
			exec:      map[string]interface{}{"d": "4"},
			key:       "nonexistent",
			wantVal:   nil,
			wantLevel: -1,
		},
		{
			name:      "key in project but not in global returns project",
			global:    map[string]interface{}{},
			project:   map[string]interface{}{"k": "p"},
			env:       nil,
			exec:      nil,
			key:       "k",
			wantVal:   "p",
			wantLevel: ScopeProject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewResolver(tt.global, tt.project, tt.env, tt.exec)
			val, level := r.Resolve(tt.key)

			if val != tt.wantVal {
				t.Errorf("Resolve(%q) value = %v, want %v", tt.key, val, tt.wantVal)
			}
			if level != tt.wantLevel {
				t.Errorf("Resolve(%q) level = %v, want %v", tt.key, level, tt.wantLevel)
			}
		})
	}
}

// TestResolver_NilMapsAreSafe verifies that nil maps passed to NewResolver
// do not cause panics and behave identically to empty maps.
//
// Covers edge case: nil maps at any level.
func TestResolver_NilMapsAreSafe(t *testing.T) {
	r := NewResolver(nil, nil, nil, nil)

	// Should not panic.
	val, level := r.Resolve("anything")
	if val != nil {
		t.Errorf("with nil maps, Resolve() value = %v, want nil", val)
	}
	if level != -1 {
		t.Errorf("with nil maps, Resolve() level = %v, want -1", level)
	}

	merged := r.ResolveAll()
	if merged == nil || len(merged) != 0 {
		t.Errorf("with nil maps, ResolveAll() = %v, want empty map", merged)
	}

	// LevelMap should return non-nil maps.
	if r.LevelMap(ScopeGlobal) == nil {
		t.Error("LevelMap(ScopeGlobal) returned nil for nil input map")
	}
	if r.LevelMap(ScopeProject) == nil {
		t.Error("LevelMap(ScopeProject) returned nil for nil input map")
	}
	if r.LevelMap(ScopeEnvironment) == nil {
		t.Error("LevelMap(ScopeEnvironment) returned nil for nil input map")
	}
	if r.LevelMap(ScopeExecution) == nil {
		t.Error("LevelMap(ScopeExecution) returned nil for nil input map")
	}
}

// TestResolver_ResolveAllPreservesAllKeys verifies that ResolveAll includes
// keys from all levels in the merged result, not just the highest-priority ones.
//
// Covers: All keys from all levels appear in ResolveAll output.
func TestResolver_ResolveAllPreservesAllKeys(t *testing.T) {
	global := map[string]interface{}{
		"global.only": "g-only",
		"shared":      "g-shared",
	}
	project := map[string]interface{}{
		"project.only": "p-only",
		"shared":       "p-shared", // overrides global
	}
	environment := map[string]interface{}{
		"env.only": "e-only",
	}
	execution := map[string]interface{}{
		"exec.only": "x-only",
		"shared":    "x-shared", // overrides project
	}

	r := NewResolver(global, project, environment, execution)
	merged := r.ResolveAll()

	// All unique keys should be present.
	if merged["global.only"] != "g-only" {
		t.Errorf("merged['global.only'] = %v, want 'g-only'", merged["global.only"])
	}
	if merged["project.only"] != "p-only" {
		t.Errorf("merged['project.only'] = %v, want 'p-only'", merged["project.only"])
	}
	if merged["env.only"] != "e-only" {
		t.Errorf("merged['env.only'] = %v, want 'e-only'", merged["env.only"])
	}
	if merged["exec.only"] != "x-only" {
		t.Errorf("merged['exec.only'] = %v, want 'x-only'", merged["exec.only"])
	}

	// Conflicting key should have highest precedence value.
	if merged["shared"] != "x-shared" {
		t.Errorf("merged['shared'] = %v, want 'x-shared' (execution wins)", merged["shared"])
	}
}

// TestResolver_ResolveAllDoesNotMutateInputs verifies that ResolveAll does
// not modify the original level maps.
//
// Covers: Input immutability during resolution.
func TestResolver_ResolveAllDoesNotMutateInputs(t *testing.T) {
	global := map[string]interface{}{"key": "global"}
	project := map[string]interface{}{"key": "project"}
	environment := map[string]interface{}{"key": "env"}
	execution := map[string]interface{}{"key": "exec"}

	// Capture copies before resolution.
	globalBefore := copyMap(global)
	projectBefore := copyMap(project)
	envBefore := copyMap(environment)
	execBefore := copyMap(execution)

	r := NewResolver(global, project, environment, execution)
	_ = r.ResolveAll()

	if !reflect.DeepEqual(global, globalBefore) {
		t.Error("global map was modified after ResolveAll()")
	}
	if !reflect.DeepEqual(project, projectBefore) {
		t.Error("project map was modified after ResolveAll()")
	}
	if !reflect.DeepEqual(environment, envBefore) {
		t.Error("environment map was modified after ResolveAll()")
	}
	if !reflect.DeepEqual(execution, execBefore) {
		t.Error("execution map was modified after ResolveAll()")
	}
}

// TestResolver_ResolveAllDeterministicWithMultipleKeys verifies that
// ResolveAll produces consistent results even with many keys across levels.
//
// Covers: Deterministic behavior with complex inputs.
func TestResolver_ResolveAllDeterministicWithMultipleKeys(t *testing.T) {
	global := map[string]interface{}{
		"alpha":   1,
		"beta":    2,
		"gamma":   3,
		"delta":   4,
		"epsilon": 5,
	}
	project := map[string]interface{}{
		"alpha":  10,
		"beta":   20,
		"zeta":   7,
		"eta":    8,
	}
	environment := map[string]interface{}{
		"alpha":  100,
		"theta":  9,
	}
	execution := map[string]interface{}{
		"alpha": 1000,
		"iota":  10,
	}

	r := NewResolver(global, project, environment, execution)

	// Run ResolveAll three times and compare.
	first := r.ResolveAll()
	second := r.ResolveAll()
	third := r.ResolveAll()

	if !reflect.DeepEqual(first, second) {
		t.Errorf("first and second ResolveAll() results differ")
	}
	if !reflect.DeepEqual(second, third) {
		t.Errorf("second and third ResolveAll() results differ")
	}

	// Verify specific values.
	if first["alpha"] != 1000 {
		t.Errorf("merged['alpha'] = %v, want 1000 (execution)", first["alpha"])
	}
	if first["beta"] != 20 {
		t.Errorf("merged['beta'] = %v, want 20 (project)", first["beta"])
	}
	if first["theta"] != 9 {
		t.Errorf("merged['theta'] = %v, want 9 (environment)", first["theta"])
	}
	if first["gamma"] != 3 {
		t.Errorf("merged['gamma'] = %v, want 3 (global)", first["gamma"])
	}
}

// copyMap returns a shallow copy of the input map.
func copyMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
