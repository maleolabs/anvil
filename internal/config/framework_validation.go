// Framework configuration validation against standard-supplied rules
// (TS-015-03-02).
//
// Per ADR-026 decision 2, config extension is standard-driven: the
// validation rules for framework configuration keys are supplied by the
// installed delivery lifecycle standard's configuration extension content
// and consumed by the Anvil Runtime (Core) through the specification's
// config extension contract (EPIC-013, command-contract.schema.json
// configKeyDeclaration). The runtime enforces the standard's rules and
// owns no framework validation rules of its own (TS-015-01-03, ADR-026
// decision 1).
//
// The contract deliberately declares no value types
// (command-contract.schema.json configKeyDeclaration: "value types are
// deliberately not extended") — the validation surface derives from what
// the contract DOES carry:
//
//   - required: a key declared required must be present in the resolved
//     configuration;
//   - string-only shape: every declared key's default is a string and no
//     other value type exists, so a provided value must be a string.
//
// This component is the engine-level integration point of the runtime's
// validation path (internal/config/validation.go, TS-P2-02): it produces
// the same ValidationError shape as the canonical schema engine, so
// framework validation errors surface identically — offending key and
// expected format — and are collected non-fail-fast alongside schema
// errors. The rules are supplied by the caller (resolved from the
// installed-standard record by internal/registry, which owns the store);
// this package never resolves standards and never interprets
// framework-specific values (C6: the standard validates its own extended
// values).
//
// Reference: TS-015-03-02, ADR-026 decision 2, EPIC-013,
// command-contract §4.5, ADR-005 §4.4
package config

import "fmt"

// FrameworkConfigKey returns the fully-qualified configuration key of a
// declared framework config key: framework.<namespace>.<name> (ADR-005
// §4.4). The runtime qualifies keys under the framework's own namespace —
// the extension declares the namespace separately, the key is the name
// within it (command-contract.schema.json configExtensionDeclaration).
func FrameworkConfigKey(namespace, name string) string {
	return "framework." + namespace + "." + name
}

// FrameworkConfigRule is one declared framework configuration key and the
// validation rules the installed delivery lifecycle standard supplies for
// it (EPIC-013 config extension contract, command-contract.schema.json
// configKeyDeclaration). The rule shape mirrors the contract: a key must
// be provided when declared required, and its value must be a string (the
// contract declares string-only defaults and deliberately extends no
// other value type). The Core owns no framework config rules
// (TS-015-01-03, ADR-026 decision 1): this struct carries the standard's
// declaration only, never runtime knowledge.
type FrameworkConfigRule struct {
	// Key is the fully-qualified dot-notation key
	// (framework.<namespace>.<name>, FrameworkConfigKey).
	Key string

	// Required reports whether the key must be present in the resolved
	// configuration (the standard's required declaration).
	Required bool
}

// ValidateFrameworkRules validates the resolved configuration's framework
// section against the standard-supplied rules. It returns a slice of
// ValidationError for every violation found.
//
// The function collects ALL errors before returning (non-fail-fast),
// mirroring the canonical schema engine (Validate). If no violations are
// found, it returns an empty slice.
//
// Enforcement derives from what the contract carries (TS-015-03-02):
//
//   - a rule declaring the key required with the key absent from the
//     resolved configuration: a required violation — the error identifies
//     the fully-qualified offending key and the expected value shape;
//   - a provided value that is not a string: the contract declares
//     string-only defaults and deliberately extends no other value type
//     (command-contract.schema.json configKeyDeclaration), so a non-string
//     value violates the declared shape — the error identifies the
//     offending key and the expected type.
//
// Keys declared by the standard but absent from the configuration are
// valid unless declared required; keys present in the configuration but
// undeclared by the standard pass through untouched — the runtime passes
// extended values through to the standard's own validation and never
// interprets framework-specific values (C6, command-contract §4.5).
//
// The function does not modify the rules or the configuration values.
func ValidateFrameworkRules(rules []FrameworkConfigRule, config map[string]interface{}) []ValidationError {
	var errs []ValidationError

	for _, rule := range rules {
		value, present := config[rule.Key]

		// Required key presence (the standard's required declaration).
		if rule.Required && !present {
			errs = append(errs, ValidationError{
				Key:      rule.Key,
				Expected: "required string value (declared required by the installed delivery lifecycle standard)",
				Actual:   nil,
			})
			continue
		}

		// Optional keys that are not present are valid (the standard's
		// default applies).
		if !present {
			continue
		}

		// String-only shape: the config extension contract declares
		// string-only defaults and deliberately extends no other value
		// type (command-contract.schema.json configKeyDeclaration).
		if _, ok := value.(string); !ok {
			errs = append(errs, ValidationError{
				Key:      rule.Key,
				Expected: fmt.Sprintf("string (framework config keys are string-only by the config extension contract — command-contract §4.5)"),
				Actual:   value,
			})
		}
	}

	return errs
}
