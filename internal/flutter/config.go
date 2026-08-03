// Configuration extension of the Flutter adapter (TS-P7-26).
//
// The adapter declares its framework-specific configuration keys under
// the "framework.flutter." namespace (ADR-005 §4.4) through the
// `extension` command, and validates provided values through the
// `validate` command (TS-P7-03). The Core enforces namespace isolation
// when registering the extension; the adapter owns the value validation
// rules.
//
// Reference: TS-P7-26, TS-P7-20 AC-4, TS-P7-03
package flutter

import (
	"fmt"
	"strings"

	"maleolabs.com/anvil/internal/contracts"
)

// Configuration extension keys declared by the Flutter adapter. All keys
// are fully-qualified under the adapter namespace "framework.flutter."
// (ADR-005 §4.4, MVP-002 §3.2).
//
// Reference: TS-P7-26 AC-1, AC-2
const (
	// KeyTargets is the comma-separated list of Flutter build targets
	// to execute (e.g. "web,apk"). The contracts.ConfigKey.Default
	// field is a string, so the array default ["web","apk"] is
	// represented as the comma-separated "web,apk" — see the key's
	// Description for the convention.
	KeyTargets = "framework.flutter.targets"

	// KeyBuildArgs is the additional `flutter build` arguments for the
	// Flutter build (e.g. "--release --split-per-abi"). Optional: empty
	// means no extra arguments.
	KeyBuildArgs = "framework.flutter.build_args"
)

// knownTargets is the known Flutter build target set. It mirrors the
// build target names (TargetWeb/TargetApk/TargetIos in build.go) so the
// validated targets can always be executed by the build pipeline.
//
// Reference: TS-P7-26 AC-3, TS-P7-21
var knownTargets = []string{TargetWeb, TargetApk, TargetIos}

// ConfigExtension returns the Flutter adapter's declared configuration
// extension: the keys it adds to the canonical schema, isolated under the
// "framework.flutter." namespace (TS-P7-26 AC-1, AC-2; TS-P7-20 AC-4).
// The targets key declares the default "web,apk" — the array default
// ["web","apk"] represented as a comma-separated string, the convention
// documented in its Description. build_args declares no default — it is
// an optional key, so omitting it must not break basic operation.
//
// Reference: TS-P7-26, TS-P7-20 AC-4, TS-P7-03
func ConfigExtension() contracts.ConfigExtensionResult {
	return contracts.ConfigExtensionResult{
		Extension: contracts.ConfigExtension{
			Framework: Framework,
			Keys: []contracts.ConfigKey{
				{
					Name:        KeyTargets,
					Description: "Comma-separated list of Flutter build targets to execute (known targets: web, apk, ios; e.g. \"web,apk\")",
					Default:     "web,apk",
				},
				{
					Name:        KeyBuildArgs,
					Description: "Additional flutter build arguments (whitespace-separated, no shell metacharacters); optional",
				},
			},
		},
	}
}

// ValidateConfigValues validates extended configuration values against
// the Flutter adapter's rules (TS-P7-26 AC-3): the targets must be a
// non-empty comma-separated list of known target names, and build_args
// must be a safe argument string when present. Unknown keys are rejected.
// The Core enforces namespace isolation before values reach the adapter
// (TS-P7-03 AC-4); the adapter validates the values themselves.
//
// Reference: TS-P7-26 AC-3, TS-P7-03 AC-4
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
	case KeyTargets:
		return validateTargets(value.Value)
	case KeyBuildArgs:
		return validateBuildArgs(value.Value)
	default:
		return fmt.Errorf("%s: unknown configuration key", value.Key)
	}
}

// validateTargets enforces the targets rule: the value must be a
// non-empty comma-separated list of known Flutter target names (web, apk,
// ios). Empty tokens — from lists like ",web", "web," or "web,,apk" —
// are rejected as malformed, and unknown names are rejected outright.
// Tokens are not trimmed: a token like " apk" is not a known target and
// is rejected, keeping the rule deterministic.
func validateTargets(value string) error {
	if value == "" {
		return fmt.Errorf("%s: must not be empty (comma-separated known targets, e.g. \"web,apk\")", KeyTargets)
	}
	for _, token := range strings.Split(value, ",") {
		if token == "" {
			return fmt.Errorf(
				"%s: %q is not a valid target list: empty target name (comma-separated known targets, e.g. \"web,apk\")",
				KeyTargets, value,
			)
		}
		if !isKnownTarget(token) {
			return fmt.Errorf(
				"%s: %q is not a known Flutter target; expected one of: %s",
				KeyTargets, token, strings.Join(knownTargets, ", "),
			)
		}
	}
	return nil
}

// isKnownTarget reports whether name is one of the known Flutter build
// targets (web, apk, ios).
func isKnownTarget(name string) bool {
	for _, target := range knownTargets {
		if name == target {
			return true
		}
	}
	return false
}

// shellMetacharacters are the characters that would let an argument
// string be interpreted by a shell: command separators and pipes (; & |),
// output redirection (< >), variable/command substitution ($ `), quoting
// (\" '), grouping and globbing (() {} [] * ?), comment start (#), plus
// backslash escapes, home expansion (~) and history expansion (!). The
// value is intended to be appended to a `flutter build` command line;
// rejecting these characters keeps it a plain whitespace-separated
// argument list.
const shellMetacharacters = ";&|<>$\\`\"'(){}[]*?~!#"

// validateBuildArgs enforces the build_args rule: when non-empty, the
// value must be a safe, whitespace-separated list of flutter build
// arguments with no shell metacharacters (see shellMetacharacters) — the
// value is appended to a command line and must never be interpreted by a
// shell. Empty is valid — the key is optional.
func validateBuildArgs(value string) error {
	if value == "" {
		return nil
	}
	if strings.ContainsAny(value, shellMetacharacters) {
		return fmt.Errorf(
			"%s: %q contains shell metacharacters; use whitespace-separated build arguments only (e.g. \"--release --split-per-abi\")",
			KeyBuildArgs, value,
		)
	}
	return nil
}
