// Configuration extension of the Laravel adapter (TS-P7-12).
//
// The adapter declares its framework-specific configuration keys under
// the "framework.laravel." namespace (ADR-005 §4.4) through the
// `extension` command, and validates provided values through the
// `validate` command (TS-P7-03). The Core enforces namespace isolation
// when registering the extension; the adapter owns the value validation
// rules.
package laravel

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"maleolabs.com/anvil/internal/contracts"
)

// Configuration extension keys declared by the Laravel adapter. All keys
// are fully-qualified under the adapter namespace "framework.laravel."
// (ADR-005 §4.4, MVP-002 §3.2).
//
// Reference: TS-P7-12 AC-1
const (
	// KeyMigrationsPath is the relative path to the Laravel migration
	// files (e.g. "database/migrations").
	KeyMigrationsPath = "framework.laravel.migrations.path"

	// KeyCacheStore is the Laravel cache store driver (e.g. "file").
	KeyCacheStore = "framework.laravel.cache.store"

	// KeyVersion is the Laravel framework version constraint, a SemVer
	// MAJOR.MINOR.PATCH version (e.g. "11.0.0").
	KeyVersion = "framework.laravel.version"
)

// cacheStoreDrivers is the known Laravel cache store driver set (the
// drivers shipped with Laravel's config/cache.php).
//
// Reference: TS-P7-12 AC-3
var cacheStoreDrivers = []string{
	"apc", "array", "database", "file", "memcached", "redis", "dynamodb",
}

// ConfigExtension returns the Laravel adapter's declared configuration
// extension: the keys it adds to the canonical schema, isolated under the
// "framework.laravel." namespace (TS-P7-12 AC-1, AC-2, AC-5).
//
// Reference: TS-P7-12, TS-P7-03
func ConfigExtension() contracts.ConfigExtensionResult {
	return contracts.ConfigExtensionResult{
		Extension: contracts.ConfigExtension{
			Framework: Framework,
			Keys: []contracts.ConfigKey{
				{
					Name:        KeyMigrationsPath,
					Description: "Relative path to the Laravel migration files",
					Default:     "database/migrations",
				},
				{
					Name:        KeyCacheStore,
					Description: "Laravel cache store driver (one of: " + strings.Join(cacheStoreDrivers, ", ") + ")",
					Default:     "file",
				},
				{
					Name:        KeyVersion,
					Description: "Laravel framework version constraint (SemVer MAJOR.MINOR.PATCH, e.g. \"11.0.0\")",
				},
			},
		},
	}
}

// ValidateConfigValues validates extended configuration values against
// the Laravel adapter's rules (TS-P7-12 AC-3): the migrations path must
// be a non-empty relative path, the cache store must be a known driver,
// and the version must be SemVer-compatible. Unknown keys are rejected.
// The Core enforces namespace isolation before values reach the adapter
// (TS-P7-03 AC-4); the adapter validates the values themselves.
//
// Reference: TS-P7-12 AC-3, AC-5
func ValidateConfigValues(req contracts.ConfigValidationRequest) contracts.ConfigValidationResult {
	var errs []string
	for _, value := range req.Values {
		if err := validateConfigValue(value); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return contracts.ConfigValidationResult{Valid: false, Errors: errs}
	}
	return contracts.ConfigValidationResult{Valid: true}
}

// validateConfigValue validates one extended key/value pair.
func validateConfigValue(value contracts.ConfigValue) error {
	switch value.Key {
	case KeyMigrationsPath:
		return validateMigrationsPath(value.Value)
	case KeyCacheStore:
		return validateCacheStore(value.Value)
	case KeyVersion:
		return validateVersion(value.Value)
	default:
		return fmt.Errorf("%s: unknown configuration key", value.Key)
	}
}

// validateMigrationsPath enforces the migrations path rule: non-empty
// and a relative path. Absolute paths are rejected outright; traversal is
// detected after filepath.Clean — a cleaned path equal to ".." or starting
// with ".." + the platform separator escapes the release directory
// (cross-platform: separators are resolved per GOOS).
func validateMigrationsPath(value string) error {
	if value == "" {
		return fmt.Errorf("%s: must not be empty (e.g. \"database/migrations\")", KeyMigrationsPath)
	}
	if filepath.IsAbs(value) {
		return fmt.Errorf("%s: must be a relative path, got absolute path %q", KeyMigrationsPath, value)
	}
	cleaned := filepath.Clean(value)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s: must not contain \"..\" traversal segments, got %q", KeyMigrationsPath, value)
	}
	return nil
}

// validateCacheStore enforces the cache store rule: the value must be
// one of the known Laravel cache drivers.
func validateCacheStore(value string) error {
	if value == "" {
		return fmt.Errorf("%s: must not be empty (e.g. \"file\")", KeyCacheStore)
	}
	for _, driver := range cacheStoreDrivers {
		if value == driver {
			return nil
		}
	}
	return fmt.Errorf(
		"%s: %q is not a known Laravel cache store; expected one of: %s",
		KeyCacheStore, value, strings.Join(cacheStoreDrivers, ", "),
	)
}

// semverPattern matches a basic MAJOR.MINOR.PATCH SemVer 2.0.0 version.
// It mirrors internal/config/validation.go — no semver dependency is
// introduced for the adapter.
var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// validateVersion enforces the version rule: SemVer-compatible
// MAJOR.MINOR.PATCH (e.g. "11.0.0").
func validateVersion(value string) error {
	if value == "" {
		return fmt.Errorf("%s: must not be empty (e.g. \"11.0.0\")", KeyVersion)
	}
	if !semverPattern.MatchString(value) {
		return fmt.Errorf("%s: version %q is not valid SemVer (expected MAJOR.MINOR.PATCH, e.g. \"11.0.0\")", KeyVersion, value)
	}
	return nil
}
