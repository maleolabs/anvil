// The configuration extension contract payloads (TS-P7-03) are defined in
// this file. They are data payloads only, consistent with the rest of the
// package: adapters declare framework-specific configuration keys through
// the command contract, and the Core requests validation of extended
// values. The Core-side enforcement mechanism lives in internal/adapter.
//
// Reference: TS-P7-03, ADR-005 §4.4, ADR-009 §6.3
package contracts

// ConfigKey declares one framework-specific configuration key that an
// adapter extends the canonical schema with. Name is the fully-qualified
// dot-notation key (e.g. "framework.laravel.php_version") and must be
// prefixed with the adapter's namespace ("framework.<adapter-name>.") per
// the configuration extension convention (ADR-005 §4.4, MVP-002 §3.2).
//
// Reference: TS-P7-03 AC-1
type ConfigKey struct {
	// Name is the fully-qualified configuration key (e.g.
	// "framework.laravel.php_version").
	Name string `json:"name"`

	// Description explains what the key configures.
	Description string `json:"description"`

	// Default is the key's default value, when the adapter declares one.
	Default string `json:"default,omitempty"`

	// Required reports whether the key must be provided for the adapter
	// to participate.
	Required bool `json:"required,omitempty"`
}

// ConfigExtension declares the configuration keys a framework adapter adds
// to the canonical schema. Framework is the adapter namespace segment
// (e.g. "laravel"); every declared key must be prefixed with
// "framework.<framework>.".
//
// Reference: TS-P7-03 AC-1, AC-2
type ConfigExtension struct {
	// Framework is the adapter namespace segment (e.g. "laravel").
	Framework string `json:"framework"`

	// Keys are the declared framework-specific configuration keys.
	Keys []ConfigKey `json:"keys"`
}

// ConfigExtensionRequest is the structured JSON payload the Core sends to
// an adapter to request its declared configuration keys. The payload is
// generic — it selects the adapter at invocation time and contains no
// framework-specific structure.
//
// Reference: TS-P7-03 AC-1
type ConfigExtensionRequest struct {
	// Framework names the adapter whose declared keys are requested.
	Framework string `json:"framework"`
}

// ConfigExtensionResult is the structured JSON payload the adapter returns
// with its declared configuration keys.
//
// Reference: TS-P7-03 AC-1
type ConfigExtensionResult struct {
	// Extension carries the adapter's declared configuration keys.
	Extension ConfigExtension `json:"extension"`
}

// ConfigValue carries one extended configuration key/value pair to be
// validated by an adapter.
//
// Reference: TS-P7-03 AC-4
type ConfigValue struct {
	// Key is the fully-qualified configuration key.
	Key string `json:"key"`

	// Value is the configured value for Key.
	Value string `json:"value"`
}

// ConfigValidationRequest is the structured JSON payload the Core sends to
// an adapter to validate extended configuration values. The payload is
// generic — it carries values only, without framework-specific structure.
//
// Reference: TS-P7-03 AC-4
type ConfigValidationRequest struct {
	// Values are the extended configuration values to validate.
	Values []ConfigValue `json:"values"`
}

// ConfigValidationResult is the structured JSON payload the adapter
// returns after validating extended configuration values. The adapter
// validates its own extended values; the Core enforces the namespace
// isolation rules and passes values through.
//
// Reference: TS-P7-03 AC-4
type ConfigValidationResult struct {
	// Valid reports whether all provided values passed validation.
	Valid bool `json:"valid"`

	// Errors describes each validation failure with a clear, actionable
	// message. Empty when Valid is true.
	Errors []string `json:"errors,omitempty"`
}
