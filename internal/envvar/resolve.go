// Package envvar resolves ${VAR} environment variable references in
// configuration values.
//
// Resolution follows ADR-019: only whole-value references — a value
// that is exactly "${VAR}" — are substituted with the value of the VAR
// environment variable. Any value that contains "${" but is not a
// whole-value reference is rejected with an explicit error so that
// secrets or placeholders never leak silently. Nested or partial
// references are not supported (EPIC-011 §11.3, single-level only).
//
// Reference: TS-P11-02, TS-011-002, EPIC-011 §11.3, ADR-019
package envvar

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Resolve resolves a single configuration value.
//
// A value that is exactly "${VAR}" is replaced by the value of the VAR
// environment variable. The variable is read with os.LookupEnv so that
// "unset" and "set-but-empty" are distinguishable: an unset variable
// produces an explicit error, while a set-but-empty variable resolves
// to "" (explicit operator intent). Values without placeholders are
// returned unchanged.
//
// Any other value containing "${" (for example "${}", "${VAR",
// "${VAR}extra", or nested references) is rejected with an explicit
// error naming the original value.
//
// Reference: TS-P11-02 AC-1, AC-2, AC-3, ADR-019
func Resolve(value string) (string, error) {
	if !strings.Contains(value, "${") {
		return value, nil
	}
	if !strings.HasPrefix(value, "${") || !strings.HasSuffix(value, "}") {
		return "", unsupportedReference(value)
	}
	name := value[2 : len(value)-1]
	if name == "" || strings.ContainsAny(name, "${}") {
		return "", unsupportedReference(value)
	}
	resolved, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment variable %q is not set", name)
	}
	return resolved, nil
}

// ResolveAll resolves every value in a map of configuration values.
//
// Each value is resolved with Resolve. Resolution fails — and nil is
// returned — if any single value fails to resolve, so callers never
// receive a partially resolved map. The input map is not modified; a
// fresh map is returned only when every value resolved successfully.
// Keys are processed in sorted order so that the reported error is
// deterministic when multiple values fail.
//
// Reference: TS-P11-02 AC-1, AC-2, AC-3, ADR-019
func ResolveAll(values map[string]string) (map[string]string, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	resolved := make(map[string]string, len(values))
	for _, key := range keys {
		value, err := Resolve(values[key])
		if err != nil {
			return nil, fmt.Errorf("cannot resolve value for key %q: %w", key, err)
		}
		resolved[key] = value
	}
	return resolved, nil
}

// unsupportedReference builds the explicit error for values that
// contain "${" but are not a whole-value "${VAR}" reference. The
// original value is included so operators can locate the offending
// configuration without secrets leaking silently.
func unsupportedReference(value string) error {
	return fmt.Errorf(
		"value %q contains a partial or unsupported environment variable reference (only whole-value ${VAR} substitution is supported)",
		value,
	)
}
