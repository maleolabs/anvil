package output

import (
	"fmt"
	"io"
)

// ── Status Indicator ──────────────────────────────────────────────────

// PrintStatus writes a status indicator line to w in the format:
//
//	[PASS] message
//	[FAIL] message
//
// This follows the existing pattern from cmd/runtime_readiness.go.
//
// The status badge is colored green for StatusPass and red for
// StatusFail when w is an interactive terminal and colors are enabled;
// the badge is plain otherwise. Other statuses (WARN, RUN, SKIP) always
// render plain.
//
// Usage:
//
//	output.PrintStatus(cmd.OutOrStdout(), output.StatusPass, "All checks passed")
//
// Reference: TS-008-009
func PrintStatus(w io.Writer, status Status, message string) {
	fmt.Fprintf(w, "[%s] %s\n", statusBadge(w, status), message)
}

// statusBadge returns the status badge, colored when colors are enabled
// for w. PASS renders green, FAIL renders red, everything else plain.
func statusBadge(w io.Writer, status Status) string {
	switch status {
	case StatusPass:
		return Green(w, string(status))
	case StatusFail:
		return Red(w, string(status))
	default:
		return string(status)
	}
}
