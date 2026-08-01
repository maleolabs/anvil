// Package output provides shared formatters for consistent CLI output
// across all Anvil commands.
//
// Four formatter types are provided:
//   - Table:   aligned columns with headers and data rows
//   - List:    bullet or numbered item lists
//   - Summary: key-value pair displays
//   - Status:  [PASS]/[FAIL]/[WARN]/[RUN]/[SKIP] indicators
//
// All formatters write to an io.Writer, making them compatible with
// Cobra's cmd.OutOrStdout() and cmd.ErrOrStderr().
//
// Reference: TS-P8-04
package output

// ── ListStyle ─────────────────────────────────────────────────────────

// ListStyle controls whether a list renders as bullet points or numbered
// items.
type ListStyle int

const (
	// BulletList renders items prefixed with "-".
	BulletList ListStyle = iota

	// NumberedList renders items prefixed with "1.", "2.", etc.
	NumberedList
)

// ── Status ────────────────────────────────────────────────────────────

// Status represents a check or operation outcome.
type Status string

const (
	StatusPass    Status = "PASS"
	StatusFail    Status = "FAIL"
	StatusWarn    Status = "WARN"
	StatusRunning Status = "RUN"
	StatusSkipped Status = "SKIP"
)
