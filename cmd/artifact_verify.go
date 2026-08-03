// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P3-05, ADR-010, ADR-012, EPIC-003
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/output"
)

// verifyCmd represents the 'anvil artifact verify' subcommand.
var verifyCmd = &cobra.Command{
	Use:   "verify <artifact-path>",
	Short: "Verify an artifact's integrity",
	Long: `Run integrity verification checks on a distributable artifact.

Verification performs four sequential checks:
  1. Archive validity — confirms the tar.gz file is readable and valid
  2. Manifest presence — confirms manifest.json exists in the artifact
  3. Manifest content — validates all required manifest fields are present
  4. Checksum match — recomputes checksum over deployable content and
     compares to the manifest value

When the active project declares a framework (project.framework), the
framework adapter's declared verification checks
(005-adapter-command-contract §4, TS-P7-11) run after the generic
checks: the adapter executable "anvil-adapter-<framework>" is resolved
on PATH and every declared check is invoked against the artifact. The
adapter is optional (ADR-009 §9.7): without a framework, or without the
adapter executable installed, verification runs the generic checks only
(a warning is printed when the executable is missing).

If all checks pass the command exits with status 0. If any check fails,
details are printed and the command exits with a non-zero status.

Examples:
  anvil artifact verify path/to/artifact.tar.gz
  anvil artifact verify --help`,
	Args: ExactArgsWithUsage(1, "anvil artifact verify path/to/artifact.tar.gz"),
	RunE: runVerify,
}

func init() {
	artifactCmd.AddCommand(verifyCmd)
}

// runVerify executes the artifact verification command.
func runVerify(cmd *cobra.Command, args []string) error {
	artifactPath := args[0]

	result, err := artifact.VerifyArtifact(artifactPath)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not verify artifact: %v", err)
	}

	if result.Passed {
		fmt.Fprintln(cmd.OutOrStdout(), "Artifact verification: PASSED")
		for _, check := range result.Checks {
			output.PrintStatus(cmd.OutOrStdout(), output.StatusPass, check.Name+": "+check.Details)
		}
		// Framework checks run after the generic checks passed
		// (ST-007-004). The adapter is optional (ADR-009 §9.7): without a
		// framework or without the adapter executable, the generic PASS
		// result stands.
		return runFrameworkVerification(cmd, artifactPath)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Artifact verification: FAILED")
	for _, check := range result.Checks {
		if !check.Passed {
			details := check.Details
			if details == "" {
				details = "failed"
			}
			output.PrintStatus(cmd.OutOrStdout(), output.StatusFail, check.Name+": "+details)
		}
	}

	// Return an error to signal non-zero exit to Cobra.
	return fmt.Errorf("artifact verification failed")
}

// runFrameworkVerification invokes the active framework adapter's
// declared verification checks against the artifact after the generic
// integrity checks passed (ST-007-004 — the CLI counterpart of the
// server install flow's runAdapterVerification, 005-adapter-command-
// contract §4, TS-P7-11). Framework-agnostic: the checks come from the
// adapter's capability declaration.
//
// The adapter is OPTIONAL (ADR-009 §9.7): a project without a framework
// skips the checks (pre-existing behavior), and a missing adapter
// executable degrades to a warning while the generic PASS result
// stands. An adapter that is present but cannot be prepared (capabilities
// command failure) or whose declared check fails fails the verification —
// the same semantics as the server install flow.
//
// Output format: a "Framework verification:" section header followed by
// one status line per declared check ("[PASS]/[FAIL] <name>: <details>")
// after the generic check lines. On any failure the command returns
// "artifact verification failed" (the same error as the generic failure
// path) so Cobra exits non-zero.
func runFrameworkVerification(cmd *cobra.Command, artifactPath string) error {
	framework := activeProjectFramework()
	if framework == "" {
		return nil
	}

	executable, err := adapterExecutableLookup("anvil-adapter-" + framework)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), FmtWarning("adapter executable %q for framework %q not found; skipping framework verification checks"), "anvil-adapter-"+framework, framework)
		return nil
	}

	coord, err := adapterVerificationCoordinator(cmd.Context(), framework, executable)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not prepare framework verification for %q: %v", framework, err)
	}

	checks, ok := coord.VerificationChecks(framework)
	if !ok || len(checks) == 0 {
		// No declared checks — nothing additional to verify or print.
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Framework verification:")
	failed := false
	for _, check := range checks {
		outcome, err := coord.InvokeVerification(cmd.Context(), framework, executable, contracts.VerificationRequest{
			Check:        check.Name,
			ArtifactPath: artifactPath,
		})
		if err != nil {
			return ReportPlainErrorf(cmd, err, "framework verification check %q could not run: %v", check.Name, err)
		}

		name := outcome.Name
		if name == "" {
			name = check.Name
		}
		details := outcome.Details
		if details == "" {
			if outcome.Passed {
				details = "passed"
			} else {
				details = "failed"
			}
		}

		if outcome.Passed {
			output.PrintStatus(cmd.OutOrStdout(), output.StatusPass, name+": "+details)
			continue
		}
		output.PrintStatus(cmd.OutOrStdout(), output.StatusFail, name+": "+details)
		failed = true
	}

	if failed {
		// Consistent with the generic failure path: the error signals
		// non-zero exit to Cobra.
		return fmt.Errorf("artifact verification failed")
	}
	return nil
}
