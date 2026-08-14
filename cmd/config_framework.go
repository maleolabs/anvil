// Framework configuration validation at the command surface
// (TS-015-03-02).
//
// Standard-driven enforcement of framework configuration keys: the
// declared framework's installed delivery lifecycle standard supplies the
// validation rules (EPIC-013 config extension contract) and the command
// layer enforces them against the resolved configuration through the
// validation engine (internal/config ValidateFrameworkRules). The runtime
// owns no framework validation rules (TS-015-01-03, ADR-026 decision 1);
// the installed-standard record is the authoritative local record of what
// is installed (TS-014-03-03).
//
// Outcomes (explicit, never silent):
//
//   - no framework declaration: no framework validation runs — projects
//     without framework configuration are unaffected (ADR-026 §12.2);
//   - framework declared, standard installed: the standard's declared
//     rules are enforced — validation errors identify the offending
//     fully-qualified key (framework.<namespace>.<name>, ADR-005 §4.4)
//     and the expected format;
//   - framework declared, standard installed without config extension
//     content: no rules to enforce — the framework section passes
//     through (a standard may declare nothing in a category,
//     command-contract §4.1);
//   - framework declared, standard not installed: wrapped
//     registry.ErrStandardNotInstalled — the caller hard-fails with
//     actionable remediation (ADR-026 decision 3, the standard-missing
//     failure semantics of TS-015-02-02).
//
// Reference: TS-015-03-02, ADR-026 decisions 2-3, EPIC-013,
// command-contract §4.1, §4.5
package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// ── Exit-Code Classification (TS-019-03-02, F-03/D-04) ────────────────

// configInvalidError renders an invalid-configuration failure with the
// config exit-code category (2, TS-P8-07 / ADR-010 §8.1): invalid or
// malformed project configuration is a configuration error per the
// general contract table. The reason preserves the underlying
// validation/parse detail.
func configInvalidError(cmd *cobra.Command, err error) error {
	return ReportErrorWithCode(cmd, &output.AppError{
		Message:    "the project configuration is invalid",
		Reason:     err.Error(),
		Resolution: "Fix the configuration source files, then re-run the command",
		Err:        err,
	}, output.ExitCodeConfig)
}

// classifyConfigLoadError renders a configuration load/resolution failure
// with the truthful exit-code category (TS-019-03-02, D-04):
//
//   - invalid (schema validation) or malformed (unparseable YAML)
//     configuration → 2 (config error) — malformed configuration is
//     "invalid" per the global contract table;
//   - genuinely unresolvable I/O failures (unreadable files) → 1
//     (general, unchanged);
//   - validation failure with NO configuration sources present (no
//     anvil.yaml and no ANVIL_CFG_* variables) → 1 — the missing
//     required values are the missing-project-context case of the D-01
//     carve-out ("commands requiring a project context exit 1 when no
//     project is found"), not invalid user-provided configuration.
//
// The config family (get/list/levels) applies it to LoadConfig failures;
// "anvil config validate" applies it to resolution failures.
func classifyConfigLoadError(cmd *cobra.Command, err error) error {
	if errors.Is(err, config.ErrConfigValidation) && !hasConfigSources() {
		// D-01 carve-out: no project context at all.
		return ReportPlainError(cmd, err, fmt.Sprintf("could not load configuration: %v", err))
	}
	if errors.Is(err, config.ErrConfigValidation) || errors.Is(err, config.ErrConfigMalformed) {
		return configInvalidError(cmd, err)
	}
	return ReportPlainError(cmd, err, fmt.Sprintf("could not load configuration: %v", err))
}

// frameworkDeclaration returns the framework declared by the resolved
// configuration (project.framework), or "" when the project declares
// none. A non-string value is not a declaration.
func frameworkDeclaration(resolved map[string]interface{}) string {
	value, ok := resolved["project.framework"]
	if !ok {
		return ""
	}
	framework, _ := value.(string)
	return framework
}

// flatResolvedConfig converts a ProvisionConfig into the flat resolved
// configuration map (key → value) used by the standard-driven framework
// validation. The values are the same resolved values the config family
// displays — framework validation runs over the exact same view.
func flatResolvedConfig(cfg *config.ProvisionConfig) map[string]interface{} {
	all := cfg.All()
	flat := make(map[string]interface{}, len(all))
	for key, vs := range all {
		flat[key] = vs.Value
	}
	return flat
}

// standardMissingError builds the standard-missing hard-fail error
// (ADR-026 decision 3, the failure semantics of TS-015-02-02) for a
// declared framework without an installed delivery lifecycle standard:
// an explicit framework declaration cannot be validated without the
// standard, and the failure is actionable — it states what is missing and
// how to resolve it. Never a silent pass-through.
func standardMissingError(resolved map[string]interface{}, err error) *output.AppError {
	framework := frameworkDeclaration(resolved)
	id := registry.StandardIDForFramework(framework)
	return &output.AppError{
		Message: fmt.Sprintf(
			"the delivery lifecycle standard for the declared framework %q (%s) is not installed",
			framework, id),
		Reason: "framework configuration keys are validated against the installed standard's config extension rules (ADR-026 decision 2); the declaration cannot be validated without it (ADR-026 decision 3)",
		Resolution: fmt.Sprintf(
			"install the standard with 'anvil standard install %s <version>', then re-run 'anvil config validate'",
			id),
		Err: err,
		// Precondition category (TS-019-03-02, D-02): the installed
		// standard is a required prerequisite of the declaration.
		ExitCodeValue: output.ExitCodePrecondition,
	}
}

// validateFrameworkConfig validates the resolved configuration's
// framework section against the installed delivery lifecycle standard's
// declared config extension rules (TS-015-03-02, ADR-026 decision 2):
//
//   - no framework declaration: no validation (nil errors, nil error) —
//     projects without framework configuration are unaffected
//     (ADR-026 §12.2);
//   - framework declared: the installed-standard store resolves the
//     standard's config extension content and the standard's rules are
//     enforced against the resolved configuration;
//   - framework declared, standard not installed: wrapped
//     registry.ErrStandardNotInstalled — the declaration cannot be
//     validated without the standard; the caller hard-fails with
//     actionable remediation (ADR-026 decision 3).
//
// The function is read-only and does not modify any state.
func validateFrameworkConfig(resolved map[string]interface{}) ([]config.ValidationError, error) {
	framework := frameworkDeclaration(resolved)
	if framework == "" {
		return nil, nil
	}

	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		return nil, err
	}
	store := registry.NewInstalledStandardStore(dir)
	return store.ValidateFrameworkConfig(framework, resolved)
}
