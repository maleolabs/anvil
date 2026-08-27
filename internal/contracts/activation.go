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

// PhaseOperation identifies which lifecycle operation an activation phase
// invocation requests. The contract supports both activation and rollback
// operations so a single adapter phase can reverse its own effects.
//
// Reference: TS-P7-01 AC-3, EPIC-007 §8.2
type PhaseOperation string

const (
	// PhaseOperationActivate requests the phase's execute operation.
	// The Core invokes it during release activation (EPIC-004).
	PhaseOperationActivate PhaseOperation = "activate"

	// PhaseOperationRollback requests the phase's rollback operation.
	// The Core invokes it during release rollback (EPIC-004).
	PhaseOperationRollback PhaseOperation = "rollback"
)

// ReleaseContext carries the release context for an activation phase
// invocation. All fields are optional except ProjectID. The struct is
// generic and intentionally contains no framework-specific fields — the
// same context applies to every framework adapter.
//
// Reference: TS-P7-01 AC-2, TS-P7-01 §7
type ReleaseContext struct {
	// ProjectID identifies the Anvil project the release belongs to.
	// This is the only required field.
	ProjectID string `json:"project_id"`

	// ReleaseID identifies the release being activated or rolled back.
	ReleaseID string `json:"release_id,omitempty"`

	// Environment names the deployment environment (e.g. "production").
	// The set of environment names is defined by the Core.
	Environment string `json:"environment,omitempty"`

	// WorkingDir is the directory the adapter should operate in, when
	// the Core provides one.
	WorkingDir string `json:"working_dir,omitempty"`
}

// ActivationRequest is the structured JSON payload the Core sends to an
// adapter to invoke one activation phase. It selects the phase, the
// operation (activate or rollback), and carries the release context.
//
// Reference: TS-P7-01 AC-1, AC-3
type ActivationRequest struct {
	// Phase is the phase name (e.g. "migrate", "config_cache"). Phase
	// names are defined by the adapter and discovered by the Core.
	Phase string `json:"phase"`

	// Operation selects the phase operation: activate or rollback.
	Operation PhaseOperation `json:"operation"`

	// Release carries the release context for the invocation.
	Release ReleaseContext `json:"release"`
}

// ActivationResult is the structured JSON payload the adapter returns
// after executing an activation phase operation.
//
// Reference: TS-P7-01 §7
type ActivationResult struct {
	// Success reports whether the phase operation completed successfully.
	Success bool `json:"success"`

	// Output captures the phase's human-readable output. Empty when the
	// phase produced no output.
	Output string `json:"output,omitempty"`

	// Error describes why the phase failed. Present only when Success
	// is false.
	Error string `json:"error,omitempty"`
}
