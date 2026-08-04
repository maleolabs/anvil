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
// Reference: TS-P7-12 AC-1, TS-P7-18 AC-1
const (
	// KeyMigrationsPath is the relative path to the Laravel migration
	// files (e.g. "database/migrations").
	KeyMigrationsPath = "framework.laravel.migrations.path"

	// KeyCacheStore is the Laravel cache store driver (e.g. "file").
	KeyCacheStore = "framework.laravel.cache.store"

	// KeyVersion is the Laravel framework version constraint, a SemVer
	// MAJOR.MINOR.PATCH version (e.g. "11.0.0").
	KeyVersion = "framework.laravel.version"

	// KeyPHPVersion is the PHP version constraint for the Laravel
	// application (e.g. "8.3.0"). Optional: empty means no constraint.
	KeyPHPVersion = "framework.laravel.php_version"

	// KeyComposerFlags is the additional composer install flags for the
	// Laravel build (e.g. "--prefer-dist --no-interaction"). Optional:
	// empty means no extra flags.
	KeyComposerFlags = "framework.laravel.composer_flags"
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
// "framework.laravel." namespace (TS-P7-12 AC-1, AC-2, AC-5; TS-P7-18
// AC-1, AC-2). php_version and composer_flags declare no default — they
// are optional keys, so omitting them must not break basic operation.
//
// Reference: TS-P7-12, TS-P7-18, TS-P7-03
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
				{
					Name:        KeyPHPVersion,
					Description: "PHP version constraint (SemVer MAJOR.MINOR.PATCH, e.g. \"8.3.0\"); optional",
				},
				{
					Name:        KeyComposerFlags,
					Description: "Additional composer install flags (whitespace-separated, no shell metacharacters, no --no-dev); optional",
				},
			},
		},
	}
}

// ValidateConfigValues validates extended configuration values against
// the Laravel adapter's rules (TS-P7-12 AC-3, TS-P7-18 AC-3): the
// migrations path must be a non-empty relative path, the cache store must
// be a known driver, the version must be SemVer-compatible, php_version
// must be SemVer-compatible when present, and composer_flags must be a
// safe flag string when present. Unknown keys are rejected. The Core
// enforces namespace isolation before values reach the adapter (TS-P7-03
// AC-4); the adapter validates the values themselves.
//
// Reference: TS-P7-12 AC-3, AC-5, TS-P7-18 AC-2, AC-3
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
	case KeyPHPVersion:
		return validatePHPVersion(value.Value)
	case KeyComposerFlags:
		return validateComposerFlags(value.Value)
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

// validatePHPVersion enforces the php_version rule: when non-empty, the
// value must be a SemVer-compatible MAJOR.MINOR.PATCH version (e.g.
// "8.3.0"). Empty is valid — the key is optional.
func validatePHPVersion(value string) error {
	if value == "" {
		return nil
	}
	if !semverPattern.MatchString(value) {
		return fmt.Errorf("%s: version %q is not valid SemVer (expected MAJOR.MINOR.PATCH, e.g. \"8.3.0\")", KeyPHPVersion, value)
	}
	return nil
}

// shellMetacharacters are the characters that would let a flag string be
// interpreted by a shell: command separators and pipes (; & |), output
// redirection (< >), variable/command substitution ($ `), quoting
// (\" '), grouping and globbing (() {} [] * ?), comment start (#), plus
// backslash escapes, home expansion (~) and history expansion (!). The
// value is intended to be appended to a composer install command line;
// rejecting these characters keeps it a plain whitespace-separated flag
// list.
const shellMetacharacters = ";&|<>$\\`\"'(){}[]*?~!#"

// validateComposerFlags enforces the composer_flags rule: the value must
// be a safe, whitespace-separated list of composer flags. Two rules
// apply, both deterministic:
//
//  1. No shell metacharacters (see shellMetacharacters) — the value is
//     appended to a command line and must never be interpreted by a
//     shell.
//  2. No "--no-dev" — the build phase already installs without dev
//     dependencies (fixed flags), so allowing it here would create two
//     sources of truth for the flag.
//
// Empty is valid — the key is optional.
func validateComposerFlags(value string) error {
	if value == "" {
		return nil
	}
	if strings.ContainsAny(value, shellMetacharacters) {
		return fmt.Errorf(
			"%s: %q contains shell metacharacters; use whitespace-separated composer flags only (e.g. \"--prefer-dist --no-interaction\")",
			KeyComposerFlags, value,
		)
	}
	if strings.Contains(value, "--no-dev") {
		return fmt.Errorf(
			"%s: %q must not contain --no-dev (the build phase already excludes dev dependencies)",
			KeyComposerFlags, value,
		)
	}
	return nil
}
