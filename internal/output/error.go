// Package output provides shared formatters for consistent CLI output
// across all Anvil commands.
//
// ── Error Presentation (TS-P8-06, ADR-010 §5.2, §7.4) ───────────────
//
// When a command fails, the error message explains:
//
//   - What went wrong in terms the user understands
//   - Why it went wrong (the root cause, not the symptom)
//   - What the user can do to fix it
//
// Errors are visually distinct from normal output. The error message is
// the first thing the user sees.
//
// Reference: TS-P8-06, ADR-010 §5.2, §7.4
package output

import (
	"fmt"
	"io"
	"strings"
)

// ── AppError ─────────────────────────────────────────────────────────

// AppError represents a structured user-facing error with actionable
// guidance. It implements the error interface and carries three parts:
//
//   - Message:   what went wrong (always rendered as "Error: ...")
//   - Reason:    why it went wrong (rendered as "Reason: ..." when non-empty)
//   - Resolution: what the user can do to fix it (rendered as "Resolution: ..." when non-empty)
//
// When Err is non-nil, it preserves the underlying error for error
// wrapping and unwrapping.
//
// AppError implements the ExitCoder interface. The exit code determines
// the process exit code when main() encounters this error. If ExitCodeValue
// is zero (unset), ExitCode() returns ExitCodeGeneral (1) as the default.
//
// Reference: TS-P8-06, TS-P8-07, ADR-010 §5.2, §8.1
type AppError struct {
	// Message describes what went wrong. This is always rendered as the
	// first "Error:" line. Required.
	Message string

	// Reason describes why it went wrong (the root cause). When empty,
	// the "Reason:" line is omitted from output.
	Reason string

	// Resolution describes what the user can do to fix the error. When
	// empty, the "Resolution:" line is omitted from output.
	Resolution string

	// Err is the underlying error, if any. Used for error wrapping and
	// unwrapping via errors.Is / errors.As.
	Err error

	// ExitCodeValue determines the process exit code. When zero (unset),
	// ExitCode() returns ExitCodeGeneral (1). Set this to one of the
	// ExitCode* constants for deterministic exit codes.
	//
	// Reference: TS-P8-07, ADR-010 §8.1
	ExitCodeValue int
}

// Error returns a single-line error string for the standard error
// interface. This is not the user-facing presentation format; use
// FormatAppError or WriteAppError for the three-part display.
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap returns the underlying error, enabling errors.Is and errors.As
// to traverse the error chain.
func (e *AppError) Unwrap() error {
	return e.Err
}

// ExitCode returns the deterministic exit code for this error. When the
// ExitCodeValue field is zero (unset), it defaults to ExitCodeGeneral (1).
//
// This implements the ExitCoder interface so that main() can extract
// the correct process exit code from the error chain.
//
// Reference: TS-P8-07, ADR-010 §8.1
func (e *AppError) ExitCode() int {
	if e.ExitCodeValue == 0 {
		return ExitCodeGeneral
	}
	return e.ExitCodeValue
}

// ── Error Formatting ─────────────────────────────────────────────────

// FormatAppError renders a structured error message in the Anvil CLI
// three-part format. Only non-empty parts are rendered.
//
// Output shape:
//
//	Error: <message>
//	Reason: <reason>
//	Resolution: <resolution>
//
// When Reason or Resolution are empty, those lines are omitted. This
// ensures plain errors produce a clean single-line output while
// structured errors deliver full actionable guidance.
//
// Reference: TS-P8-06 §7
func FormatAppError(e *AppError) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Error: %s.\n", e.Message)
	if e.Reason != "" {
		fmt.Fprintf(&sb, "Reason: %s.\n", e.Reason)
	}
	if e.Resolution != "" {
		fmt.Fprintf(&sb, "Resolution: %s.\n", e.Resolution)
	}
	return sb.String()
}

// FormatPlainError renders a plain error in the standard one-line
// format. This is the fallback when the error is not an AppError.
//
// Output shape:
//
//	Error: <message>.
//
// Reference: TS-P8-06 §7
func FormatPlainError(message string) string {
	return fmt.Sprintf("Error: %s.\n", message)
}

// FormatPlainErrorf renders a formatted plain error in the standard
// one-line format, applying fmt.Sprintf to the message.
//
// Output shape:
//
//	Error: <formatted message>.
//
// Reference: TS-P8-06 §7
func FormatPlainErrorf(format string, args ...interface{}) string {
	return fmt.Sprintf("Error: %s.\n", fmt.Sprintf(format, args...))
}

// ── Writer Helpers ───────────────────────────────────────────────────

// WriteAppError renders a structured error to the given writer.
//
// When e is nil, no output is produced.
//
// Reference: TS-P8-06, ADR-010 §5.2
func WriteAppError(w io.Writer, e *AppError) {
	if e == nil {
		return
	}
	fmt.Fprint(w, FormatAppError(e))
}

// WritePlainError renders a plain error message to the given writer.
//
// Reference: TS-P8-06, ADR-010 §5.2
func WritePlainError(w io.Writer, message string) {
	fmt.Fprint(w, FormatPlainError(message))
}
