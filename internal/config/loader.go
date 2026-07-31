// Package config provides the multi-source configuration loader for Anvil
// projects. The loader builds a complete configuration view by combining
// values from compiled defaults, global configuration files, project
// configuration files, and environment variables — in that precedence order.
//
// Reference: TS-P2-04, ADR-005 §7.5, §10.2, §12.5
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// envPrefix is the required prefix for all Anvil configuration environment
// variables. Environment variables without this prefix are ignored.
//
// Example: ANVIL_CFG_PROJECT_NAME → config key "project.name"
const envPrefix = "ANVIL_CFG_"

// LoadConfig builds a complete configuration view by combining values from
// all sources in the following resolution order (highest precedence last):
//
//  1. Compiled defaults — every schema key that has a Default value is
//     populated from the canonical schema (GetSchema) (Global level).
//  2. Global configuration files — YAML files discovered in the global
//     config directory (GlobalConfigDir) (Global level).
//  3. Project configuration files — YAML files discovered in the project
//     root directory (via discoverProjectRoot) (Project level).
//  4. Environment configuration files — YAML files from
//     <project>/config/environments/<ANVIL_ENV>.yaml, loaded when the
//     ANVIL_ENV environment variable is set (Environment level).
//  5. Environment variables — variables with the ANVIL_CFG_ prefix,
//     converted to dot-notation keys and coerced to the schema type
//     (Execution level).
//
// The level-specific maps are resolved via Resolver, which provides
// deterministic precedence: Execution > Environment > Project > Global.
// Global files override compiled defaults at the same Global level.
//
// After resolving all levels, the result is validated via ValidateConfig.
// If validation fails, an error is returned with all validation errors
// formatted for human readability.
//
// On success, a *ProvisionConfig is returned. ProvisionConfig wraps the
// resolver and exposes only read-only typed accessors — consumers cannot
// mutate configuration through this interface.
//
// The function is deterministic: given the same sources, it always produces
// the same configuration.
//
// Reference: TS-P2-04, TS-P2-06, TS-P2-07, TS-P2-08, ST-P2-07,
// ADR-005 §7.5, §8.4, ADR-005 §10.2
func LoadConfig() (*ProvisionConfig, error) {
	schema := GetSchema()

	// 1. Build the Global level map: compiled defaults overlaid with
	//    global config file values (same scope level, files win).
	globalMap := make(map[string]interface{}, len(schema.Entries))
	for key, entry := range schema.Entries {
		if entry.Default != nil {
			// Array-type defaults ([]string) are normalised to []interface{}
			// so that the validation engine can process them correctly.
			globalMap[key] = normalizeValue(entry.Default)
		}
	}

	// 2. Get discovered configuration file paths.
	discoveredPaths := DiscoverConfigFiles()

	// 3. Determine the global config directory for path classification.
	globalDir := ""
	if gd, err := GlobalConfigDir(); err == nil {
		globalDir = gd
	}

	// 4. Separate discovered paths into global and project files.
	var globalFiles, projectFiles []string
	for _, p := range discoveredPaths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if globalDir != "" && strings.HasPrefix(absPath, globalDir) {
			globalFiles = append(globalFiles, absPath)
		} else {
			projectFiles = append(projectFiles, absPath)
		}
	}

	// 5. Overlay global file values onto the Global level map (files
	//    override compiled defaults within the same Global scope).
	for _, path := range globalFiles {
		fileConfig, err := loadYAMLFile(path)
		if err != nil {
			return nil, fmt.Errorf("read global config %s: %w", path, err)
		}
		for k, v := range fileConfig {
			globalMap[k] = v
		}
	}

	// 6. Build the Project level map from project configuration files.
	projectMap := make(map[string]interface{})
	for _, path := range projectFiles {
		fileConfig, err := loadYAMLFile(path)
		if err != nil {
			return nil, fmt.Errorf("read project config %s: %w", path, err)
		}
		for k, v := range fileConfig {
			projectMap[k] = v
		}
	}

	// 7. Build the Execution level map from environment variables.
	//    Values are coerced from string to the schema-expected type.
	executionMap := loadEnvVars(schema)

	// 8. Load environment-level configuration from the active environment
	//    (if ANVIL_ENV is set). The environment config file lives at:
	//    <project_root>/config/environments/<ANVIL_ENV>.yaml
	//    If the file does not exist, it is silently skipped.
	var environmentMap map[string]interface{}
	if envName := os.Getenv("ANVIL_ENV"); envName != "" {
		// Discover the project root so we can locate the environments dir.
		if root, err := discoverProjectRoot(); err == nil {
			envCfg, err := LoadEnvironmentConfig(root, envName)
			if err != nil {
				return nil, err
			}
			environmentMap = envCfg
		}
	}

	// 9. Resolve all levels with deterministic precedence:
	//    Execution > Environment > Project > Global.
	resolver := NewResolver(globalMap, projectMap, environmentMap, executionMap)
	config := resolver.ResolveAll()

	// 10. Validate the combined configuration.
	_, errs := ValidateConfig(schema, config)
	if len(errs) > 0 {
		return nil, fmt.Errorf("configuration validation failed:\n%s", FormatValidationErrors(errs))
	}

	// 11. Wrap the resolver in a read-only ProvisionConfig.
	return NewProvisionConfig(resolver), nil
}

// loadYAMLFile reads a YAML configuration file and flattens its contents
// into a flat dot-notation map.
//
// For example, the following YAML:
//
//	project:
//	  name: my-app
//	  version: 2.0.0
//
// produces:
//
//	{"project.name": "my-app", "project.version": "2.0.0"}
//
// Empty files produce an empty map. The function does not validate the
// contents against the schema — that is done by LoadConfig after all
// sources are combined.
func loadYAMLFile(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return make(map[string]interface{}), nil
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}

	if parsed == nil {
		return make(map[string]interface{}), nil
	}

	return flattenYAML(parsed, ""), nil
}

// flattenYAML converts a nested map[string]interface{} (as produced by
// YAML unmarshalling) into a flat dot-notation map.
//
// Nested maps are recursively flattened with keys joined by ".". Non-map
// values (strings, integers, booleans, slices) are stored as-is at their
// fully qualified key path.
func flattenYAML(data map[string]interface{}, prefix string) map[string]interface{} {
	result := make(map[string]interface{})
	for key, value := range data {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}
		switch v := value.(type) {
		case map[string]interface{}:
			for k, val := range flattenYAML(v, fullKey) {
				result[k] = val
			}
		default:
			result[fullKey] = v
		}
	}
	return result
}

// loadEnvVars reads environment variables with the ANVIL_CFG_ prefix and
// converts them into a flat dot-notation configuration map. Values are
// coerced from strings to the expected schema type where possible.
//
// Convention (ST-P2-02, ADR-005 §10.2):
//   - Environment variables must start with "ANVIL_CFG_" to be recognised.
//   - After stripping the prefix, the remainder is lowercased.
//   - The first underscore is replaced with a dot to separate the
//     configuration category from the key name.
//   - Values are coerced from string to the schema-expected type (int, bool).
//
// Examples:
//
//	ANVIL_CFG_PROJECT_NAME       → "project.name" = "env-app"
//	ANVIL_CFG_GLOBAL_LOG_LEVEL   → "global.log_level" = "debug"
//	ANVIL_CFG_RELEASE_MAX_RETAINED → "release.max_retained" = 7  (int)
func loadEnvVars(schema Schema) map[string]interface{} {
	result := make(map[string]interface{})

	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, envPrefix) {
			continue
		}

		eqIdx := strings.IndexByte(env, '=')
		if eqIdx < 0 {
			continue
		}

		envName := env[:eqIdx]
		envValue := env[eqIdx+1:]

		// Strip the ANVIL_CFG_ prefix and convert to lowercase.
		key := strings.ToLower(strings.TrimPrefix(envName, envPrefix))

		// Replace the first underscore with a dot to separate category
		// from key name. All schema keys are exactly two levels deep,
		// so the first underscore is the category/key boundary.
		key = strings.Replace(key, "_", ".", 1)

		// Coerce the string value to the expected schema type.
		result[key] = coerceStringValue(key, envValue, schema)
	}

	return result
}

// normalizeValue ensures that a schema default value has the correct Go
// type for validation. In particular, []string array defaults are converted
// to []interface{} since the validation engine expects that representation.
func normalizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case []string:
		normalized := make([]interface{}, len(val))
		for i, s := range val {
			normalized[i] = s
		}
		return normalized
	default:
		return val
	}
}

// coerceStringValue attempts to convert a string value to the Go type
// expected by the schema entry for the given key. If the key is not in the
// schema, or if conversion fails, the original string value is returned so
// that validation can produce a meaningful error.
func coerceStringValue(key, value string, schema Schema) interface{} {
	entry, ok := schema.Entries[key]
	if !ok {
		return value
	}

	switch entry.Type {
	case TypeInteger:
		// Attempt to parse as integer. Accept both int and int64.
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
		// Return the original string; validation will catch the type error.
		return value

	case TypeBoolean:
		switch strings.ToLower(value) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
		// Return the original string; validation will catch the type error.
		return value

	default:
		return value
	}
}
