// Package contracts defines the stable command contract exchanged between
// the Anvil Core and Framework Adapters via subprocess invocation.
//
// Per 004-review-resolutions D1, Core↔Adapter integration is a CLI
// subprocess command contract. Adapters are standalone executables in any
// language; the Core invokes them through the Process Runner (ADR-008)
// with structured JSON input, and adapters return structured JSON output
// with a defined exit code convention (ADR-010 §8.1). No Go interfaces,
// no in-process calls, and no plugin mechanism are used.
//
// The types in this package are data payloads only. They carry the
// contract between Core and adapters; they do not define behavior.
//
// Reference: TS-P7-01, TS-P7-02, EPIC-007 C-1, ADR-009, ADR-016,
// 004-review-resolutions D1
package contracts

// VerificationCheck describes a verification check that an adapter
// provides. The description explains what the check validates.
//
// Reference: TS-P7-02 AC-1
type VerificationCheck struct {
	// Name identifies the check (e.g. "vendor_present"). Check names
	// are defined by the adapter and discovered by the Core.
	Name string `json:"name"`

	// Description explains what the check validates.
	Description string `json:"description"`
}

// VerificationRequest is the structured JSON payload the Core sends to
// an adapter to execute one verification check during artifact
// verification (EPIC-003 §7.5).
//
// Reference: TS-P7-02 AC-1, EPIC-007 §8.2
type VerificationRequest struct {
	// Check names the check to execute.
	Check string `json:"check"`

	// ArtifactPath is the path to the artifact archive to validate.
	ArtifactPath string `json:"artifact_path"`
}

// VerificationOutcome reports the pass/fail result of a single
// verification check. The shape is aligned with artifact.CheckResult
// (internal/artifact) so adapter outcomes can be merged into the Core's
// verification report without transformation.
//
// Reference: TS-P7-02 AC-2, EPIC-003 §7.5
type VerificationOutcome struct {
	// Name identifies the check that produced this outcome.
	Name string `json:"name"`

	// Passed reports whether the check passed.
	Passed bool `json:"passed"`

	// Details describes what was validated and, on failure, what failed.
	// Empty when the check passed without remarks.
	Details string `json:"details,omitempty"`
}
