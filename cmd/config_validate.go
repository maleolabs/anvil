// Package cmd implements the Anvil CLI commands.
//
// ── Config Validate (TS-012-001) ─────────────────────────────────────
//
// "anvil config validate" resolves the project configuration from all
// scope levels and validates it against the canonical schema using the
// same engine exercised at load time by every other command (TS-P2-05).
// Implicit (load-time) and explicit validation therefore never diverge.
//
// Validation errors are grouped by category (required, type, allowed,
// format) so operators can act on the output directly. The command is
// read-only and exits non-zero when the resolved configuration is
// invalid or cannot be resolved.
//
// Exit codes:
//
//	0 - Configuration is valid
//	1 - Configuration could not be resolved (e.g. unreadable or malformed files)
//	2 - Configuration is invalid (validation errors found)
//
// Reference: TS-012-001, TS-P2-05, MVP-001 AC 9.2, TS-P8-07, ADR-010 §8.1
package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/output"
)

// configValidateCmd represents the "anvil config validate" command that
// resolves and validates the project configuration against the canonical
// schema.
//
// Reference: TS-012-001
var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the resolved project configuration",
	Long: `Validate the resolved project configuration against the canonical schema.

The configuration is resolved from all scope levels (global, project,
environment, execution) with their respective precedence, then validated
with the same engine exercised at load time by every other command —
implicit and explicit validation never diverge.

Validation errors are grouped by category so each problem can be
addressed directly:
  required  missing required values
  type      values that do not match the expected schema type
  allowed   values outside the schema's allowed set
  format    values that do not match the required format

This command is read-only and does not modify any files or state.

Exit codes:
  0 - Configuration is valid
  1 - Configuration could not be resolved (e.g. unreadable or malformed files)
  2 - Configuration is invalid (validation errors found)

Examples:
  anvil config validate
  anvil config validate --json`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runConfigValidate,
}

func init() {
	configCmd.AddCommand(configValidateCmd)
	AddJSONFlag(configValidateCmd)
}

// ── Validation Error Categories (TS-012-001) ─────────────────────────
//
// The config engine reports every failure as a ValidationError without a
// category field, so the presentation categories below are derived
// deterministically from the engine's Expected description. This is
// presentation logic only — no new validation rules are introduced.

const (
	// validationCategoryRequired groups missing required values.
	validationCategoryRequired = "required"
	// validationCategoryType groups schema type mismatches.
	validationCategoryType = "type"
	// validationCategoryAllowed groups values outside the allowed set.
	validationCategoryAllowed = "allowed"
	// validationCategoryFormat groups format violations (e.g. SemVer).
	validationCategoryFormat = "format"
)

// validationErrorCategoryOrder defines the canonical display order of the
// presentation categories.
var validationErrorCategoryOrder = []string{
	validationCategoryRequired,
	validationCategoryType,
	validationCategoryAllowed,
	validationCategoryFormat,
}

// categorizeValidationError derives the presentation category for a
// validation error from the engine's Expected field (internal/config
// validation.go):
//
//   - "required <type> value"  → required (missing value)
//   - "one of <values>"        → allowed (value outside the allowed set)
//   - "valid SemVer string…"   → format (malformed value)
//   - otherwise                → type (schema type mismatch)
func categorizeValidationError(err config.ValidationError) string {
	switch {
	case strings.HasPrefix(err.Expected, "required "):
		return validationCategoryRequired
	case strings.HasPrefix(err.Expected, "one of "):
		return validationCategoryAllowed
	case strings.Contains(err.Expected, "SemVer"):
		return validationCategoryFormat
	default:
		return validationCategoryType
	}
}

// groupValidationErrorsByCategory groups validation errors by their
// presentation category.
func groupValidationErrorsByCategory(errs []config.ValidationError) map[string][]config.ValidationError {
	grouped := make(map[string][]config.ValidationError)
	for _, err := range errs {
		category := categorizeValidationError(err)
		grouped[category] = append(grouped[category], err)
	}
	return grouped
}

// ── Machine-Readable Result (TS-P8-05) ───────────────────────────────

// configValidationResult is the structured validation result reported via
// --json. Errors are grouped by category, mirroring the human-readable
// output field for field.
type configValidationResult struct {
	Valid      bool                           `json:"valid"`
	ErrorCount int                            `json:"error_count"`
	Errors     map[string][]configErrorRecord `json:"errors"`
}

// configErrorRecord is the machine-readable representation of a single
// validation error.
type configErrorRecord struct {
	Key      string      `json:"key"`
	Expected string      `json:"expected"`
	Actual   interface{} `json:"actual"`
	Source   string      `json:"source,omitempty"`
}

// buildValidationResult converts raw validation errors into the
// categorized machine-readable result. Records within a category are
// sorted by Key so the JSON output is deterministic across runs (the
// engine reports errors in map-iteration order).
func buildValidationResult(errs []config.ValidationError) configValidationResult {
	result := configValidationResult{
		Valid:      len(errs) == 0,
		ErrorCount: len(errs),
		Errors:     make(map[string][]configErrorRecord),
	}
	if len(errs) == 0 {
		return result
	}

	grouped := groupValidationErrorsByCategory(errs)
	for _, category := range validationErrorCategoryOrder {
		categoryErrs, ok := grouped[category]
		if !ok {
			continue
		}
		records := make([]configErrorRecord, 0, len(categoryErrs))
		for _, e := range categoryErrs {
			records = append(records, configErrorRecord{
				Key:      e.Key,
				Expected: e.Expected,
				Actual:   e.Actual,
				Source:   e.Source,
			})
		}
		sort.Slice(records, func(i, j int) bool {
			return records[i].Key < records[j].Key
		})
		result.Errors[category] = records
	}
	return result
}

// ── Command Implementation ───────────────────────────────────────────

// runConfigValidate implements the "anvil config validate" command.
func runConfigValidate(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")

	// Resolve and validate using the same path as load-time validation.
	errs, err := config.ResolveAndValidate()
	if err != nil {
		// The configuration could not be resolved at all (e.g. unreadable
		// or malformed source files) — this is not a validation result.
		if jsonOutput {
			_ = output.WriteJSONError(cmd.OutOrStdout(), err.Error())
			return err
		}
		return ReportPlainError(cmd, err, fmt.Sprintf("could not resolve configuration: %v", err))
	}

	// Valid configuration: report success and exit 0.
	if len(errs) == 0 {
		if jsonOutput {
			return WriteJSON(cmd, buildValidationResult(errs))
		}
		PrintSuccess(cmd, "Configuration is valid.")
		return nil
	}

	// Invalid configuration: report categorized errors and exit with the
	// config error category (exit 2, TS-P8-07 / ADR-010 §8.1).
	if jsonOutput {
		if err := WriteJSON(cmd, buildValidationResult(errs)); err != nil {
			return err
		}
		return &output.AppError{
			Message:       "configuration is invalid",
			Reason:        fmt.Sprintf("%d validation error(s) found", len(errs)),
			Resolution:    "Fix the configuration errors reported in the JSON output, then run 'anvil config validate' again",
			ExitCodeValue: output.ExitCodeConfig,
		}
	}

	appErr := &output.AppError{
		Message:       "configuration is invalid",
		Reason:        fmt.Sprintf("%d validation error(s) found", len(errs)),
		Resolution:    "Fix the configuration errors listed below, then run 'anvil config validate' again",
		ExitCodeValue: output.ExitCodeConfig,
	}
	err = ReportErrorWithCode(cmd, appErr, output.ExitCodeConfig)
	fmt.Fprintln(cmd.ErrOrStderr())
	writeCategorizedErrors(cmd.ErrOrStderr(), errs)
	return err
}

// writeCategorizedErrors renders validation errors grouped by category to
// the provided writer. Errors within a category are listed in
// deterministic key order.
func writeCategorizedErrors(w io.Writer, errs []config.ValidationError) {
	grouped := groupValidationErrorsByCategory(errs)

	fmt.Fprintln(w, "Validation errors by category:")
	for _, category := range validationErrorCategoryOrder {
		categoryErrs, ok := grouped[category]
		if !ok {
			continue
		}
		sort.Slice(categoryErrs, func(i, j int) bool {
			return categoryErrs[i].Key < categoryErrs[j].Key
		})
		fmt.Fprintf(w, "  %s:\n", category)
		for _, e := range categoryErrs {
			fmt.Fprintf(w, "    - %s\n", e.Error())
		}
	}
}
