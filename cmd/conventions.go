// Package cmd implements the Anvil CLI commands.
//
// ── Command Conventions (ADR-010 §3.4, §4.2, §4.3, TS-P8-03) ────────
//
// Structure:
//
//		anvil <capability> <operation> [arguments] [options]
//
//	  - capability: product domain (project, release, artifact, runtime,
//	    pipeline, config, server, system)
//	  - operation:  action within the domain (init, status, package, verify,
//	    get, set, list, build, ci, install, activate, rollback, cleanup)
//
// Flag Naming:
//
//	Short form: single lowercase letter  (-o, -f, -v)
//	Long form:  descriptive kebab-case   (--output, --format, --server-root)
//
//	Examples:
//	  --server-root      (correct — two kebab-case words)
//	  --install-root     (correct)
//	  --non-interactive   (correct)
//	  --server_root      (incorrect — uses underscore)
//	  --serverRoot       (incorrect — uses camelCase)
//	  -o                 (correct — single letter)
//	  -out               (incorrect — multi-letter short flag)
//
// Argument Placement (ADR-010 §4.3):
//
//	Positional arguments identify resources:
//	  anvil init <name>
//	  anvil artifact verify <artifact-path>
//	  anvil server release install <project-id> <artifact-path>
//
//	Flags (options) modify behavior:
//	  anvil artifact package --format tar.gz --output ./dist
//	  anvil pipeline build --env production
//
// Output (ADR-010 §3.4):
//
//	Human-readable: write to cmd.OutOrStdout()
//	Errors/warnings: write to cmd.ErrOrStderr()
//	Machine-readable: use --json flag, write JSON to cmd.OutOrStdout()
//
// Error Reporting:
//
//	User-facing errors must be printed to cmd.ErrOrStderr() before
//	returning the error to Cobra. This ensures consistent error display.
//
//	Format: "Error: <message>.\n"
//	Format: "Warning: <message>\n"
//
// Parent Groups:
//
//	Parent-only groups (namespaces) MUST NOT have RunE, Run, or Args set.
//	They exist solely to organise subcommands by domain.
//
// Reference: ADR-010, TS-P8-03
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
)

// ── Flag Helpers ─────────────────────────────────────────────────────

// AddJSONFlag adds a standard --json flag for machine-readable output.
//
// Usage:
//
//	cmd.Flags().Bool("json", false, "Output in JSON format for machine consumption")
func AddJSONFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Output in JSON format for machine consumption")
}

// ── Output Helpers ───────────────────────────────────────────────────

// FmtError returns an error string formatted according to Anvil CLI
// conventions. Use with cmd.ErrOrStderr() for consistent error display.
//
// Example:
//
//	fmt.Fprintf(cmd.ErrOrStderr(), FmtError("could not load config: %v"), err)
func FmtError(format string) string {
	return "Error: " + format + ".\n"
}

// FmtWarning returns a warning string formatted according to Anvil CLI
// conventions. Use with cmd.ErrOrStderr() for consistent warning display.
//
// Example:
//
//	fmt.Fprintf(cmd.ErrOrStderr(), FmtWarning("using non-default server root %q"), path)
func FmtWarning(format string) string {
	return "Warning: " + format + "\n"
}

// PrintKeyValue writes a formatted key-value pair to the command's stdout
// with standard indentation. This provides consistent output formatting
// across all commands.
//
// Example:
//
//	PrintKeyValue(cmd, "Release ID", rel.ID)
//	// Output: "  Release ID: abc123..."
func PrintKeyValue(cmd *cobra.Command, key string, value interface{}) {
	fmt.Fprintf(cmd.OutOrStdout(), "  %s: %v\n", key, value)
}

// WriteJSON encodes v as a machine-readable JSON envelope to the
// command's stdout. The output is wrapped in the standard envelope
// format (TS-P8-05):
//
//	{"version":"1","status":"success","data":{...}}
//
// Example:
//
//	return WriteJSON(cmd, result)
func WriteJSON(cmd *cobra.Command, v interface{}) error {
	return output.WriteJSON(cmd.OutOrStdout(), v)
}

// WriteJSONError encodes an error response in the machine-readable JSON
// envelope format. The output is wrapped in the standard error envelope:
//
//	{"version":"1","status":"error","error":"..."}
//
// Example:
//
//	return WriteJSONError(cmd, "could not load project")
func WriteJSONError(cmd *cobra.Command, errMsg string) error {
	return output.WriteJSONError(cmd.OutOrStdout(), errMsg)
}

// ── Output Convenience ───────────────────────────────────────────────

// PrintSection prints a section header to stdout with a trailing blank
// line for consistent section-based output formatting.
//
// Example:
//
//	PrintSection(cmd, "Project Registry")
func PrintSection(cmd *cobra.Command, title string) {
	fmt.Fprintln(cmd.OutOrStdout(), title)
	fmt.Fprintln(cmd.OutOrStdout(), "")
}

// PrintTable prints a table with headers and data rows using the
// output.Table formatter. Each row must have the same number of cells
// as headers.
//
// Example:
//
//	PrintTable(cmd, []string{"Name", "Version"}, [][]string{
//	    {"app-a", "1.0.0"},
//	    {"app-b", "2.0.0"},
//	})
func PrintTable(cmd *cobra.Command, headers []string, rows [][]string) {
	t := output.NewTable(headers...)
	for _, row := range rows {
		t.AddRow(row...)
	}
	t.Format(cmd.OutOrStdout())
}

// PrintStatus prints a status indicator line to stdout in the format:
//
//	[PASS] message
//	[FAIL] message
//
// This wraps output.PrintStatus with the command's stdout writer.
//
// Example:
//
//	PrintStatus(cmd, output.StatusPass, "All checks passed")
func PrintStatus(cmd *cobra.Command, status output.Status, message string) {
	output.PrintStatus(cmd.OutOrStdout(), status, message)
}

// PrintList prints a bullet or numbered list to stdout.
//
// Example:
//
//	PrintList(cmd, []string{"item 1", "item 2"}, output.BulletList)
func PrintList(cmd *cobra.Command, items []string, style output.ListStyle) {
	l := output.NewList(style)
	for _, item := range items {
		l.AddItem(item)
	}
	l.Format(cmd.OutOrStdout())
}

// PrintKV prints a single key-value pair. It is a shorthand for
// PrintKeyValue when only one value needs to be displayed.
//
// Deprecated: Prefer PrintKeyValue for consistency.
func PrintKV(cmd *cobra.Command, key string, value interface{}) {
	PrintKeyValue(cmd, key, value)
}

// ── Error Helpers ────────────────────────────────────────────────────

// ExitError formats a user-facing error, writes it to stderr, and returns
// the underlying error. This is the standard pattern for runtime errors
// that should produce a non-zero exit code.
//
// Usage:
//
//	return ExitError(cmd, "could not load project", err)
func ExitError(cmd *cobra.Command, message string, err error) error {
	fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s: %v.\n", message, err)
	return err
}

// FlagIsSet returns true if the named flag was explicitly changed by the
// user (as opposed to using its default value).
func FlagIsSet(cmd *cobra.Command, name string) bool {
	f := cmd.Flags().Lookup(name)
	return f != nil && f.Changed
}
