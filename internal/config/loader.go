// Package config provides the multi-source configuration loader for Anvil
// projects. The loader builds a complete configuration view by combining
// values from compiled defaults, global configuration files, project
// configuration files, and environment variables — in that precedence order.
//
// Reference: TS-P2-04, ADR-005 §7.5, §10.2, §12.5
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnvPrefix is the required prefix for all Anvil configuration environment
// variables. Environment variables without this prefix are ignored.
//
// It is exported so that consumers (e.g. diagnostic commands detecting
// configuration sources) can reference the canonical prefix.
//
// Example: ANVIL_CFG_PROJECT_NAME → config key "project.name"
const EnvPrefix = "ANVIL_CFG_"

// ErrConfigValidation is wrapped around LoadConfig's failure when the
// resolved configuration violates the canonical schema. It lets the
// command surface classify an invalid configuration as the config
// category (exit 2) instead of a general error (TS-019-03-02, F-03).
var ErrConfigValidation = errors.New("configuration is invalid")

// ErrConfigMalformed is wrapped around a configuration source parse
// failure (malformed YAML). Per the TS-019-03-02 mapping (D-04),
// malformed configuration is "invalid" per the global contract table and
// exits 2; genuinely unresolvable I/O failures (unreadable files) remain
// general errors (exit 1) and carry no sentinel.
var ErrConfigMalformed = errors.New("configuration is malformed")

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

	// Resolve the configuration from all sources (shared resolution path).
	config, resolver, err := resolveConfig(schema)
	if err != nil {
		return nil, err
	}

	// Validate the combined configuration (schema + server targets, AC1-AC2, framework-free AC4).
	_, errs := ValidateConfig(schema, config)
	serverErrs := ValidateServerTargetsInFlat(config)
	errs = append(errs, serverErrs...)
	if len(errs) > 0 {
		return nil, fmt.Errorf("configuration validation failed:\n%s: %w", FormatValidationErrors(errs), ErrConfigValidation)
	}

	// Wrap the resolver in a read-only ProvisionConfig.
	return NewProvisionConfig(resolver), nil
}

// ResolveAndValidate resolves the project configuration from all sources
// using the exact same path as LoadConfig and validates it against the
// canonical schema.
//
// Unlike LoadConfig — which collapses validation failures into a single
// formatted error string — this function returns the raw validation errors
// so callers can present them individually. It is the explicit-validation
// counterpart of load-time validation (TS-P2-05): "anvil config validate"
// uses it so that implicit (load-time) and explicit validation never
// diverge.
//
// Returns:
//   - nil errors + nil error when the resolved configuration is valid
//   - a non-empty error slice + nil error when validation fails
//   - nil errors + a non-nil error when the configuration cannot be
//     resolved from its sources (e.g. unreadable or malformed files)
//
// The function is read-only and does not modify any state.
//
// Reference: TS-P2-04, TS-P2-05, TS-012-001
func ResolveAndValidate() ([]ValidationError, error) {
	_, errs, err := ResolveAndValidateConfig()
	return errs, err
}

// ResolveAndValidateConfig resolves the project configuration from all
// sources using the exact same path as LoadConfig, validates it against
// the canonical schema, and returns the resolved flat configuration
// together with any validation errors.
//
// It is ResolveAndValidate's resolved-configuration-returning counterpart
// (TS-015-03-02): "anvil config validate" uses it so that the resolved
// values — including the framework declaration and the framework section
// merged from the installed standard (TS-015-03-01) — are available to
// the standard-driven framework validation that runs alongside the
// canonical schema validation, keeping implicit (load-time) and explicit
// validation aligned.
//
// Returns:
//   - resolved configuration + nil errors + nil error when valid
//   - resolved configuration + a non-empty error slice + nil error when
//     validation fails (the resolved values are still returned so every
//     violation can be collected, non-fail-fast)
//   - nil configuration + nil errors + a non-nil error when the
//     configuration cannot be resolved from its sources (e.g. unreadable
//     or malformed files)
//
// The function is read-only and does not modify any state.
//
// Reference: TS-P2-04, TS-P2-05, TS-012-001, TS-015-03-02
func ResolveAndValidateConfig() (map[string]interface{}, []ValidationError, error) {
	schema := GetSchema()

	resolved, _, err := resolveConfig(schema)
	if err != nil {
		return nil, nil, err
	}

	_, errs := ValidateConfig(schema, resolved)
	serverErrs := ValidateServerTargetsInFlat(resolved)
	errs = append(errs, serverErrs...)
	return resolved, errs, nil
}

// resolveConfig builds the resolved configuration from all sources. It is
// the shared resolution path used by both LoadConfig (implicit load-time
// validation) and ResolveAndValidate (explicit validation), guaranteeing
// that both always validate the exact same resolved values.
//
// Resolution order (highest precedence last):
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
// It returns the resolved flat map together with the resolver used to
// produce it. The function does not validate the resolved values — that is
// the caller's responsibility (ValidateConfig / ResolveAndValidate).
func resolveConfig(schema Schema) (map[string]interface{}, *Resolver, error) {
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
	//    A file is global only when it is actually inside the global config
	//    directory; the comparison respects path-component boundaries so that
	//    sibling directories sharing the global dir name as a string prefix
	//    (e.g. ~/.config/anvil-projects/...) are not misclassified.
	var globalFiles, projectFiles []string
	for _, p := range discoveredPaths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if isPathInsideDir(absPath, globalDir) {
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
			return nil, nil, fmt.Errorf("read global config %s: %w", path, err)
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
			return nil, nil, fmt.Errorf("read project config %s: %w", path, err)
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
				return nil, nil, err
			}
			environmentMap = envCfg
		}
	}

	// 9. Resolve all levels with deterministic precedence:
	//    Execution > Environment > Project > Global.
	resolver := NewResolver(globalMap, projectMap, environmentMap, executionMap)
	config := resolver.ResolveAll()

	// Synthesize installer.forms object from fragmented keys if needed (v3 fallback)
	if _, ok := config["installer.forms"]; !ok {
		if forms, err := ParseInstallerFormsFromFlat(config); err == nil && forms != nil {
			// Convert to generic map for storage as TypeObject
			generic := make(map[string]interface{}, len(forms))
			for k, v := range forms {
				generic[k] = v
			}
			config["installer.forms"] = generic
		}
	}

	return config, resolver, nil
}

// isPathInsideDir reports whether path is located inside dir, respecting
// path-component boundaries. A sibling directory that shares dir's name as
// a string prefix (e.g. /home/u/.config/anvil-projects/... when dir is
// /home/u/.config/anvil) is NOT considered inside.
//
// It returns false when dir is empty, when either path cannot be resolved
// relative to the other, or when the relative path escapes dir. It returns
// true when path equals dir itself or lies beneath it.
func isPathInsideDir(path, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
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
		// Malformed YAML is wrapped with ErrConfigMalformed so the
		// command surface can classify it as invalid configuration
		// (exit 2, TS-019-03-02 D-04); unreadable files (os.ReadFile
		// failures) carry no sentinel and remain general errors (exit 1).
		return nil, fmt.Errorf("%w: %v", ErrConfigMalformed, err)
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
		// Keep installer.forms as object to preserve nested structure for forms schema (v3)
		if fullKey == "installer.forms" {
			result[fullKey] = value
			continue
		}
		if prefix == "installer.forms" {
			// Should not happen because we keep installer.forms as object above,
			// but handle fragmented fallback by still flattening one level deep?
			result[fullKey] = value
			continue
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
		if !strings.HasPrefix(env, EnvPrefix) {
			continue
		}

		eqIdx := strings.IndexByte(env, '=')
		if eqIdx < 0 {
			continue
		}

		envName := env[:eqIdx]
		envValue := env[eqIdx+1:]

		// Strip the ANVIL_CFG_ prefix and convert to lowercase.
		key := strings.ToLower(strings.TrimPrefix(envName, EnvPrefix))

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
