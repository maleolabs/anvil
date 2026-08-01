// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P3-05, ADR-010, ADR-012, EPIC-003
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
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
		return nil
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
