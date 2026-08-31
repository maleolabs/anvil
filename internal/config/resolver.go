// Package config provides the configuration resolution engine for Anvil
// projects. The Resolver merges configuration from multiple scope levels
// (Global, Project, Environment, Execution) with deterministic precedence.
//
// Reference: TS-P2-06, ADR-005
package config

// Resolver merges configuration values from four scope levels with
// deterministic precedence: Execution (highest) > Environment > Project >
// Global (lowest).
//
// Each level is an independent map. Values from higher-precedence levels
// override those from lower levels. A value defined only at the Global level
// is still accessible — it does not need to be redefined at higher levels.
//
// The resolver is fully deterministic: given the same inputs, Resolve and
// ResolveAll always produce the same output. Level maps are not modified
// during resolution.
//
// Reference: TS-P2-06, ADR-005 §7.5
type Resolver struct {
	global      map[string]interface{}
	project     map[string]interface{}
	environment map[string]interface{}
	execution   map[string]interface{}
}

// NewResolver creates a new Resolver with the provided level-specific
// configuration maps. Nil maps are treated as empty for safety.
//
// Parameters:
//   - global: compiled defaults and global config file values (lowest precedence)
//   - project: project-level config file values
//   - environment: environment-specific overrides
//   - execution: single-command overrides (highest precedence)
func NewResolver(global, project, environment, execution map[string]interface{}) *Resolver {
	return &Resolver{
		global:      safeMap(global),
		project:     safeMap(project),
		environment: safeMap(environment),
		execution:   safeMap(execution),
	}
}

// safeMap returns an empty map for nil inputs, ensuring the resolver never
// panics from map access on nil maps.
func safeMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return make(map[string]interface{})
	}
	return m
}

// Resolve looks up the given key across all scope levels in precedence order
// (Execution highest, then Environment, Project, Global). It returns the
// first value found and the level at which it was found, or (nil, -1) if
// the key is not present at any level.
//
// The lookup is deterministic: the levels are always checked in the same
// order (Execution → Environment → Project → Global).
//
// Reference: TS-P2-06, ADR-005 §7.5
func (r *Resolver) Resolve(key string) (interface{}, ScopeLevel) {
	// Check from highest precedence to lowest.
	if v, ok := r.execution[key]; ok {
		return v, ScopeExecution
	}
	if v, ok := r.environment[key]; ok {
		return v, ScopeEnvironment
	}
	if v, ok := r.project[key]; ok {
		return v, ScopeProject
	}
	if v, ok := r.global[key]; ok {
		return v, ScopeGlobal
	}
	return nil, -1
}

// ResolveAll merges all four scope levels into a single map, with higher
// precedence levels overriding lower ones. The merge order is:
//
//  1. Global (lowest precedence)
//  2. Project (overrides Global)
//  3. Environment (overrides Project)
//  4. Execution (overrides Environment, highest precedence)
//
// The result is a new map allocated per call; the original level maps are
// never modified. Keys present only in lower levels appear in the result —
// partial overrides are fully supported.
//
// Reference: TS-P2-06, ADR-005 §7.5
func (r *Resolver) ResolveAll() map[string]interface{} {
	// Pre-allocate with total capacity across all levels.
	totalLen := len(r.global) + len(r.project) + len(r.environment) + len(r.execution)
	result := make(map[string]interface{}, totalLen)

	// Merge in precedence order (lowest first, so highest wins on conflict).
	for k, v := range r.global {
		result[k] = v
	}
	for k, v := range r.project {
		result[k] = v
	}
	for k, v := range r.environment {
		result[k] = v
	}
	for k, v := range r.execution {
		result[k] = v
	}

	return result
}

// LevelMap returns the configuration map for the specified scope level.
// This is primarily useful for testing and debugging. The returned map is
// a direct reference — callers should not modify it.
func (r *Resolver) LevelMap(level ScopeLevel) map[string]interface{} {
	switch level {
	case ScopeGlobal:
		return r.global
	case ScopeProject:
		return r.project
	case ScopeEnvironment:
		return r.environment
	case ScopeExecution:
		return r.execution
	default:
		return nil
	}
}
