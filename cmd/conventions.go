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

// PrintSuccess writes a success confirmation message to the command's stdout
// (green on interactive terminals, plain otherwise).
//
// Example:
//
//	PrintSuccess(cmd, "Release created.")
func PrintSuccess(cmd *cobra.Command, message string) {
	output.PrintSuccess(cmd.OutOrStdout(), message)
}

// PrintSuccessf formats a success confirmation message and writes it to the
// command's stdout (green on interactive terminals, plain otherwise).
//
// Example:
//
//	PrintSuccessf(cmd, "Project '%s' created.", name)
func PrintSuccessf(cmd *cobra.Command, format string, args ...interface{}) {
	output.PrintSuccessf(cmd.OutOrStdout(), format, args...)
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

// PrintRoundedTable prints a table with headers and data rows using the
// modern rounded-corner ASCII box style (output.NewRoundedTable). Each
// row must have the same number of cells as headers.
//
// Example:
//
//	PrintRoundedTable(cmd, []string{"Name", "Version"}, [][]string{
//	    {"app-a", "1.0.0"},
//	    {"app-b", "2.0.0"},
//	})
func PrintRoundedTable(cmd *cobra.Command, headers []string, rows [][]string) {
	t := output.NewRoundedTable(headers...)
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
//
// Deprecated: Prefer ReportError or ReportPlainError for structured
// error presentation with actionable guidance (TS-P8-06).
func ExitError(cmd *cobra.Command, message string, err error) error {
	str := fmt.Sprintf("Error: %s: %v.\n", message, err)
	fmt.Fprint(cmd.ErrOrStderr(), output.Red(cmd.ErrOrStderr(), str))
	return err
}

// ReportError renders a structured error to stderr using the three-part
// error presentation format (TS-P8-06).
//
// When the --json flag is set on the command, the error is written as a
// machine-readable JSON envelope to stdout instead (TS-P8-05).
//
// Format (human-readable):
//
//	Error: <what went wrong>
//	Reason: <why it went wrong>
//	Resolution: <what the user can do>
//
// Only non-empty parts are rendered. When e.Reason or e.Resolution are
// empty, those lines are omitted.
//
// Usage:
//
//	return ReportError(cmd, &output.AppError{
//	    Message:    "could not load project",
//	    Reason:     "anvil.yaml was not found",
//	    Resolution: "Run 'anvil init' to create a project",
//	    Err:        err,
//	})
//
// Reference: TS-P8-06, ADR-010 §5.2, §7.4
func ReportError(cmd *cobra.Command, e *output.AppError) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag {
		return output.WriteJSONError(cmd.OutOrStdout(), e.Error())
	}
	output.WriteAppError(cmd.ErrOrStderr(), e)
	return e
}

// ReportPlainError formats a plain error message and writes it to stderr.
// This is the fallback for error sites that do not yet carry structured
// Reason/Resolution context.
//
// When the --json flag is set on the command, the error is written as a
// machine-readable JSON envelope to stdout instead (TS-P8-05).
//
// Format (human-readable):
//
//	Error: <message>.
//
// Usage:
//
//	return ReportPlainError(cmd, fmt.Errorf("could not load config"), "could not load config")
//
// Reference: TS-P8-06, ADR-010 §5.2
func ReportPlainError(cmd *cobra.Command, err error, message string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag {
		return output.WriteJSONError(cmd.OutOrStdout(), message)
	}
	output.WritePlainError(cmd.ErrOrStderr(), message)
	return err
}

// ReportPlainErrorf formats a plain error message using fmt.Sprintf and
// writes it to stderr. Convenience wrapper around ReportPlainError.
//
// Usage:
//
//	return ReportPlainErrorf(cmd, err, "could not load %s: %v", "config", err)
//
// Reference: TS-P8-06, ADR-010 §5.2
func ReportPlainErrorf(cmd *cobra.Command, err error, format string, args ...interface{}) error {
	message := fmt.Sprintf(format, args...)
	return ReportPlainError(cmd, err, message)
}

// ReportErrorWithCode renders a structured error with an explicit exit
// code to stderr using the three-part error presentation format (TS-P8-06).
//
// This is used when the exit code must be specified explicitly rather
// than derived from the AppError's ExitCode field. The exitCode
// parameter overrides the AppError's ExitCode for the returned error.
//
// When the --json flag is set on the command, the error is written as a
// machine-readable JSON envelope to stdout instead (TS-P8-05).
//
// Usage:
//
//	return ReportErrorWithCode(cmd, &output.AppError{
//	    Message:    "project already registered",
//	    Reason:     "a project with ID 'my-app' already exists",
//	    Resolution: "Use a different project ID",
//	}, output.ExitCodeConfig)
//
// Reference: TS-P8-06, TS-P8-07, ADR-010 §5.2, §8.1
func ReportErrorWithCode(cmd *cobra.Command, e *output.AppError, exitCode int) error {
	e.ExitCodeValue = exitCode
	return ReportError(cmd, e)
}

// FlagIsSet returns true if the named flag was explicitly changed by the
// user (as opposed to using its default value).
func FlagIsSet(cmd *cobra.Command, name string) bool {
	f := cmd.Flags().Lookup(name)
	return f != nil && f.Changed
}
