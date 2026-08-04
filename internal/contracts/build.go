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
// generic — it carries the working directory and the per-target /
// strict-mode execution policy only and contains no framework-specific
// structure (ADR-009 §9.6).
//
// Targets and Strict are additive optional fields (005-adapter-command-
// contract §8): an old adapter that does not know them ignores them and
// runs its full pipeline as before — the fields are omitted when unset
// (omitempty), so old and new adapters interoperate during transition
// periods.
//
// Reference: TS-P7-14 AC-1, TS-007-041, ADR-018, 005-adapter-command-
// contract §8
type BuildRequest struct {
	// WorkingDir is the project/release working directory the build
	// phases run in. Empty runs the phases in the adapter's current
	// working directory.
	WorkingDir string `json:"working_dir,omitempty"`

	// Targets restricts the build pipeline to the named targets/phases
	// (e.g. "web", "apk" for Flutter — ADR-018 --target selection). The
	// adapter runs only the phases whose name is in the list, in its
	// table order. Empty runs all phases (current behavior) — additive
	// compatibility per 005 §8.
	Targets []string `json:"targets,omitempty"`

	// Strict fails the build when a requested target is unsupported on
	// the current platform instead of skipping it with a warning
	// (ADR-018 --strict). Adapters with no platform metadata ignore it.
	Strict bool `json:"strict,omitempty"`
}

// BuildPhaseResult reports the outcome of one build phase. The shape
// follows the ActivationResult convention: the phase is always named,
// success is always reported, and the optional output/error fields are
// omitted when empty.
//
// Skipped and Warning are additive optional fields (005-adapter-command-
// contract §8): an old Core that does not know them ignores them and
// treats the phase by its Success flag, so old and new adapters
// interoperate during transition periods.
//
// Reference: TS-P7-14 AC-2, AC-7, TS-007-041, 005-adapter-command-
// contract §8
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

	// Skipped reports that the phase was not executed because it is
	// unsupported on the current platform (ADR-018 platform filtering).
	// A skipped phase still reports Success=true — the skip is a
	// graceful degradation, not a failure — so builds that skip
	// unsupported targets succeed. Omitted when the phase ran.
	Skipped bool `json:"skipped,omitempty"`

	// Warning describes why the phase was skipped (e.g. "target "ios"
	// is not supported on platform "linux""). Present only when Skipped
	// is true.
	Warning string `json:"warning,omitempty"`
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
