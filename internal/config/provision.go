// Package config provides the immutable configuration provisioning system
// for Anvil projects. ProvisionConfig wraps a Resolver and delivers resolved,
// validated configuration values through typed accessors that enforce
// immutability — no setter or mutation methods are exposed.
//
// Reference: TS-P2-07, ADR-005 §7.5
package config

import "fmt"

// ValueSource pairs a resolved configuration value with the scope level at
// which it was defined. This enables consumers to inspect not just what the
// value is, but also where it came from.
type ValueSource struct {
	// Value is the resolved configuration value.
	Value interface{}

	// Scope is the scope level at which the value was resolved.
	Scope ScopeLevel
}

// ProvisionConfig delivers resolved, validated configuration as immutable
// values. It wraps a *Resolver and exposes only read operations: typed
// accessors (GetString, GetInt, GetBool, GetStringSlice) and an All() method
// that returns all values with their source scope levels.
//
// No setter or mutation methods are exposed. Typed accessors return copies
// of the underlying data (strings are immutable in Go; slices are explicitly
// copied). Consumers cannot modify the configuration through this interface.
//
// Reference: TS-P2-07, ADR-005 §7.5
type ProvisionConfig struct {
	resolver *Resolver
}

// NewProvisionConfig creates a new ProvisionConfig wrapping the provided
// Resolver. The resolver must not be nil.
func NewProvisionConfig(resolver *Resolver) *ProvisionConfig {
	return &ProvisionConfig{resolver: resolver}
}

// Get returns the resolved value for the given key together with its scope
// level. It returns an error if the key is not present at any level.
//
// This is the generic accessor. For type-safe access, use the typed accessors
// (GetString, GetInt, GetBool, GetStringSlice).
func (pc *ProvisionConfig) Get(key string) (interface{}, ScopeLevel, error) {
	value, scope := pc.resolver.Resolve(key)
	if scope == -1 {
		return nil, -1, fmt.Errorf("key %q: no resolved value found at any scope level", key)
	}
	return value, scope, nil
}

// GetString returns the resolved string value for the given key. It returns
// an error if the key is not found or if the value is not a string.
func (pc *ProvisionConfig) GetString(key string) (string, ScopeLevel, error) {
	value, scope, err := pc.Get(key)
	if err != nil {
		return "", -1, err
	}
	str, ok := value.(string)
	if !ok {
		return "", -1, fmt.Errorf("key %q: expected string, got %T", key, value)
	}
	return str, scope, nil
}

// GetInt returns the resolved int value for the given key. It returns an
// error if the key is not found or if the value is not an int.
//
// Note: YAML/JSON unmarshalling may produce int, int64, or float64 values.
// This accessor accepts int and int64; whole-number float64 values are also
// accepted to handle common YAML edge cases.
func (pc *ProvisionConfig) GetInt(key string) (int, ScopeLevel, error) {
	value, scope, err := pc.Get(key)
	if err != nil {
		return 0, -1, err
	}

	switch v := value.(type) {
	case int:
		return v, scope, nil
	case int64:
		return int(v), scope, nil
	case float64:
		// Accept whole-number floats (e.g. 5.0 from YAML).
		if v == float64(int(v)) {
			return int(v), scope, nil
		}
		return 0, -1, fmt.Errorf("key %q: expected integer, got float %v", key, v)
	default:
		return 0, -1, fmt.Errorf("key %q: expected integer, got %T", key, value)
	}
}

// GetBool returns the resolved bool value for the given key. It returns an
// error if the key is not found or if the value is not a bool.
func (pc *ProvisionConfig) GetBool(key string) (bool, ScopeLevel, error) {
	value, scope, err := pc.Get(key)
	if err != nil {
		return false, -1, err
	}
	b, ok := value.(bool)
	if !ok {
		return false, -1, fmt.Errorf("key %q: expected boolean, got %T", key, value)
	}
	return b, scope, nil
}

// GetStringSlice returns the resolved string slice value for the given key.
// It returns an error if the key is not found or if the value is not a slice
// of strings (or a slice of interface{} containing strings).
//
// The returned slice is a COPY of the underlying data, enforcing immutability.
// Modifying the returned slice will not affect the configuration state.
func (pc *ProvisionConfig) GetStringSlice(key string) ([]string, ScopeLevel, error) {
	value, scope, err := pc.Get(key)
	if err != nil {
		return nil, -1, err
	}

	switch v := value.(type) {
	case []string:
		// Return a copy to enforce immutability.
		result := make([]string, len(v))
		copy(result, v)
		return result, scope, nil
	case []interface{}:
		// Convert from []interface{} (as produced by YAML loading or
		// normalizeValue in the loader) to []string.
		result := make([]string, len(v))
		for i, elem := range v {
			s, ok := elem.(string)
			if !ok {
				return nil, -1, fmt.Errorf("key %q: element %d is %T, expected string", key, i, elem)
			}
			result[i] = s
		}
		return result, scope, nil
	default:
		return nil, -1, fmt.Errorf("key %q: expected string slice, got %T", key, value)
	}
}

// All returns all resolved configuration values with their scope levels.
// The returned map is a snapshot — modifying it does not affect the
// underlying configuration.
//
// Reference: TS-P2-07
func (pc *ProvisionConfig) All() map[string]ValueSource {
	merged := pc.resolver.ResolveAll()
	result := make(map[string]ValueSource, len(merged))
	for key, value := range merged {
		_, scope := pc.resolver.Resolve(key)
		result[key] = ValueSource{Value: value, Scope: scope}
	}
	return result
}

// LevelMap returns the configuration map for the specified scope level.
// This enables consumers to inspect configuration values at each level
// independently — useful for debugging, the "config levels" CLI command,
// and testing.
//
// The returned map is a direct reference to the internal resolver's map.
// Callers must not modify it.
//
// Reference: TS-P2-08, ST-P2-08
func (pc *ProvisionConfig) LevelMap(level ScopeLevel) map[string]interface{} {
	return pc.resolver.LevelMap(level)
}

// ProjectMetadata returns the project metadata (name, version) from the
// resolved configuration. It returns an error if the required keys are
// missing or have unexpected types.
//
// Reference: TS-P1-03, ADR-005 §7.2
func (pc *ProvisionConfig) ProjectMetadata() (Metadata, error) {
	name, _, err := pc.GetString("project.name")
	if err != nil {
		return Metadata{}, fmt.Errorf("project metadata: %w", err)
	}
	version, _, err := pc.GetString("project.version")
	if err != nil {
		return Metadata{}, fmt.Errorf("project metadata: %w", err)
	}
	return NewMetadata(name, version), nil
}
