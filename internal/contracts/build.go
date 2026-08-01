// The build contract payloads (TS-P7-14) are defined in this file. They
// are data payloads only, consistent with the rest of the package: the
// Core invokes an adapter's build pipeline — the build phases the adapter
// declares in its capability declaration — through the `build` command
// (contracts.CommandBuild), and the adapter reports the outcome of each
// phase through the build contract. The Core-side dispatch mechanism
// lives in internal/adapter; the Laravel adapter's build pipeline lives
// in internal/laravel.
//
// Reference: TS-P7-14, ADR-009 §4.1, §7.3
package contracts

// BuildRequest is the structured JSON payload the Core sends to an
// adapter to execute its build pipeline for a release. The payload is
// generic — it carries the working directory only and contains no
// framework-specific structure (ADR-009 §9.6).
//
// Reference: TS-P7-14 AC-1
type BuildRequest struct {
	// WorkingDir is the project/release working directory the build
	// phases run in. Empty runs the phases in the adapter's current
	// working directory.
	WorkingDir string `json:"working_dir,omitempty"`
}

// BuildPhaseResult reports the outcome of one build phase. The shape
// follows the ActivationResult convention: the phase is always named,
// success is always reported, and the optional output/error fields are
// omitted when empty.
//
// Reference: TS-P7-14 AC-2, AC-7
type BuildPhaseResult struct {
	// Phase names the build phase (e.g. "composer").
	Phase string `json:"phase"`

	// Success reports whether the phase completed successfully.
	Success bool `json:"success"`

	// Output captures the phase's human-readable output. Empty when the
	// phase produced no output.
	Output string `json:"output,omitempty"`

	// Error describes why the phase failed. Present only when Success
	// is false.
	Error string `json:"error,omitempty"`
}

// BuildResult is the structured JSON payload the adapter returns after
// executing its build pipeline. Success is computed: it is true when
// every phase in Phases succeeded. An adapter that declares no build
// phases returns an empty Phases list with Success=true — a graceful
// no-op build (ADR-009 §9.7).
//
// Reference: TS-P7-14 AC-2
type BuildResult struct {
	// Phases report each build phase's outcome, in execution order.
	Phases []BuildPhaseResult `json:"phases"`

	// Success reports whether all build phases succeeded.
	Success bool `json:"success"`
}
