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
// Usage:
//
//	output.PrintStatus(cmd.OutOrStdout(), output.StatusPass, "All checks passed")
func PrintStatus(w io.Writer, status Status, message string) {
	fmt.Fprintf(w, "[%s] %s\n", string(status), message)
}
