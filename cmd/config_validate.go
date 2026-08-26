// Package cmd implements the Anvil CLI commands.
//
// ── Config Validate (TS-012-001) ─────────────────────────────────────
//
// "anvil config validate" resolves the project configuration from all
// scope levels and validates it against the canonical schema using the
// same engine exercised at load time by every other command (TS-P2-05).
// Framework configuration is additionally validated against the declared
// framework's installed delivery lifecycle standard: the standard's
// config extension rules (EPIC-013) are enforced on the framework section
// (framework.<name>.<key>, ADR-005 §4.4) — no hardcoded framework rules
// exist in the runtime (TS-015-03-02, ADR-026). Implicit (load-time) and
// explicit validation therefore never diverge.
//
// Validation errors are grouped by category (required, type, allowed,
// format) so operators can act on the output directly. The command
// resolves and validates configuration without modifying project state;
// the one best-effort side effect is the adoption-time adapter
// recognition of TS-017-01-02 (an installed v1.x adapter is recognized
// through the authoritative mapping and the migration outcome is
// recorded under the global config directory, anvil/adapter-migrations/)
// and the command exits non-zero when the resolved configuration is
// invalid or cannot be resolved.
//
// Exit codes (truthful mapping, TS-019-03-02 D-04/D-02):
//
//	0 - Configuration is valid
//	1 - Configuration could not be resolved (unreadable source files)
//	2 - Configuration is invalid (validation errors found, or malformed
//	    source files)
//	4 - A declared framework without an installed standard (precondition)
//
// Reference: TS-012-001, TS-P2-05, MVP-001 AC 9.2, TS-P8-07, ADR-010 §8.1
package cmd

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
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

When the project declares a framework, the framework configuration
section is additionally validated against the installed delivery
lifecycle standard's config extension rules (standard-driven, TS-015-03-02):
the standard supplies the validation surface, the runtime enforces it.

Validation errors are grouped by category so each problem can be
addressed directly:
  required  missing required values
  type      values that do not match the expected schema type
  allowed   values outside the schema's allowed set
  format    values that do not match the required format

Adoption-time adapter recognition (TS-017-01-02): when the project
declares a framework and an installed v1.x adapter is detected on this
system, the command recognizes it through the authoritative
adapter-to-standard mapping and records the migration outcome as a
best-effort side effect (a record under the global config directory,
anvil/adapter-migrations/; nothing is written otherwise, and project
state is never modified). The validation result itself is unaffected.

Exit codes:
  0 - Configuration is valid
  1 - Configuration could not be resolved (unreadable source files)
  2 - Configuration is invalid (validation errors found, or malformed
      source files)
  4 - A declared framework without an installed standard (precondition)

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
	s := styleFor(cmd)
	jsonOutput, _ := cmd.Flags().GetBool("json")

	// Resolve and validate the canonical schema using the same path as
	// load-time validation. The resolved values are returned alongside
	// the validation errors so the standard-driven framework validation
	// (TS-015-03-02) runs over the exact same resolved configuration —
	// implicit (load-time) and explicit validation never diverge.
	resolved, errs, err := config.ResolveAndValidateConfig()
	if err != nil {
		// The configuration could not be resolved from its sources.
		// Malformed configuration is "invalid" per the global contract
		// table → exit 2 (TS-019-03-02, D-04); genuinely unresolvable
		// I/O failures (unreadable files) stay general → exit 1.
		// ReportErrorWithCode handles the --json envelope.
		if errors.Is(err, config.ErrConfigMalformed) {
			return configInvalidError(cmd, err)
		}
		if jsonOutput {
			_ = output.WriteJSONError(s.W, err.Error())
			return err
		}
		return ReportPlainError(cmd, err, fmt.Sprintf("could not resolve configuration: %v", err))
	}

	// TS-015-03-02: standard-driven framework validation — the declared
	// framework's config extension rules come from the installed delivery
	// lifecycle standard (ADR-026 decision 2), never from the runtime.
	// TS-017-01-02 (T-004): adoption-time installed-adapter recognition
	// and migration (ADR-028 §3, §12.3) — when an installed v1.x adapter
	// is recognized for the project's declared framework, the runtime
	// maps it to the corresponding standard via the authoritative
	// mapping table and records the migration outcome (explicit, never
	// silent — A2). Recognition is additive: it never changes the
	// framework validation semantics below (standard-missing hard-fail,
	// ADR-026 decision 3) and never modifies project state.
	// Contract-version validation at migration is TS-017-01-03 (T-007,
	// Wave 3).
	recognizeAndMigrateInstalledAdapterAtAdoption(cmd, cmd.Context(), frameworkDeclaration(resolved))
	frameworkErrs, err := validateFrameworkConfig(resolved)
	if err != nil {
		if errors.Is(err, registry.ErrStandardNotInstalled) {
			// Standard-missing hard-fail (ADR-026 decision 3, the
			// failure semantics of TS-015-02-02): a declared framework
			// without an installed standard cannot be validated against
			// the standard's rules — fail with an actionable message
			// stating what is missing and how to resolve it, never a
			// silent pass-through.
			appErr := standardMissingError(resolved, err)
			if jsonOutput {
				_ = output.WriteJSONError(s.W, appErr.Error())
			}
			return appErr
		}
		// The standard store cannot answer: a corrupt record or an
		// unreadable store is a real failure, never a silent no-match.
		if jsonOutput {
			_ = output.WriteJSONError(s.W, err.Error())
			return err
		}
		return ReportPlainError(cmd, err, fmt.Sprintf("could not resolve configuration: %v", err))
	}
	errs = append(errs, frameworkErrs...)

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
