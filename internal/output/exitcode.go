// Package output provides shared formatters for consistent CLI output
// across all Anvil commands.
//
// ── Exit Codes (TS-P8-07, ADR-010 §8.1) ────────────────────────────
//
// Exit codes are deterministic and documented. Automation consumers
// (CI/CD pipelines, provisioning tools) rely on these codes to
// distinguish error categories without parsing human-readable messages.
//
// The codes align with the Registration Automation Contract
// (docs/operations/registration-automation-contract.md).
//
// Reference: TS-P8-07, ADR-010 §8.1
package output

// Exit codes for the Anvil CLI. These are stable and may be relied
// upon by automation consumers.
//
// Reference: TS-P8-07, ADR-010 §8.1
const (
	// ExitCodeSuccess indicates successful execution.
	ExitCodeSuccess = 0

	// ExitCodeGeneral indicates a general or validation error.
	// This is the default for errors that do not fall into a
	// more specific category.
	ExitCodeGeneral = 1

	// ExitCodeConfig indicates a configuration error such as
	// duplicate project ID, conflict, or invalid configuration.
	ExitCodeConfig = 2

	// ExitCodeRuntime indicates a runtime error such as resource
	// not found or service unavailable.
	ExitCodeRuntime = 3

	// ExitCodePrecondition indicates a precondition error such as
	// runtime not initialized or missing prerequisite.
	ExitCodePrecondition = 4
)

// ExitCoder is implemented by errors that carry a deterministic exit
// code. When main() encounters an error implementing this interface,
// it uses the exit code instead of the default code 1.
//
// This enables different error categories to produce distinct exit
// codes for automation consumers.
//
// Reference: TS-P8-07, ADR-010 §8.1
type ExitCoder interface {
	ExitCode() int
}
