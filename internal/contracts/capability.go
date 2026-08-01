// The capability declaration contract payloads (TS-P7-07) are defined in
// this file. They are data payloads only, consistent with the rest of the
// package: adapters declare the operations they support — activation
// phases, verification checks, and diagnostic commands — through the
// command contract, and the Core queries the declaration to determine
// what framework-specific behavior is available without inspecting
// adapter internals (ADR-009 §4.1, §7.3). The Core-side storage and
// enforcement mechanism lives in internal/adapter.
//
// Reference: TS-P7-07, ADR-009 §4.1, §7.3
package contracts

// CapabilityDeclaration lists the operations an adapter supports: which
// activation phases, verification checks, and diagnostic commands it
// provides. The Core uses this declaration to determine what
// framework-specific behavior is available without inspecting adapter
// internals. An empty declaration — no phases, no checks, no commands —
// is valid: the Core then proceeds with generic operations only
// (ADR-009 §9.7).
//
// Reference: TS-P7-07 AC-1, AC-2, AC-4
type CapabilityDeclaration struct {
	// ActivationPhases are the phase names the adapter supports
	// (e.g. "migrate", "config_cache"). The Core invokes only declared
	// phases.
	ActivationPhases []string `json:"activation_phases,omitempty"`

	// VerificationChecks are the verification checks the adapter
	// provides. The Core invokes only declared checks.
	VerificationChecks []VerificationCheck `json:"verification_checks,omitempty"`

	// DiagnosticCommands are the command names the adapter supports for
	// diagnostics, reserved for future use by the Core.
	DiagnosticCommands []string `json:"diagnostic_commands,omitempty"`
}

// CapabilityRequest is the structured JSON payload the Core sends to an
// adapter to request its declared capabilities. The payload is generic —
// it selects the adapter at invocation time and contains no
// framework-specific structure.
//
// Reference: TS-P7-07 AC-1
type CapabilityRequest struct {
	// Framework names the adapter whose declared capabilities are
	// requested (e.g. "laravel").
	Framework string `json:"framework"`
}

// CapabilityResult is the structured JSON payload the adapter returns
// with its declared capabilities.
//
// Reference: TS-P7-07 AC-1, AC-2
type CapabilityResult struct {
	// Declaration carries the adapter's declared capabilities.
	Declaration CapabilityDeclaration `json:"capabilities"`
}
