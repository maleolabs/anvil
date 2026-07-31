// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P3-07, ADR-004 §8.1/§8.3, EPIC-003
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/output"
)

// verifyImmutabilityCmd represents the 'anvil artifact verify-immutability'
// subcommand.
var verifyImmutabilityCmd = &cobra.Command{
	Use:   "verify-immutability <artifact-path>",
	Short: "Verify artifact immutability",
	Long: `Verify that an artifact has not been modified since creation.

Immutability verification recomputes the artifact's checksum and compares
it to the checksum stored in the artifact's manifest. If they match, the
artifact is unchanged since packaging.

Examples:
  anvil artifact verify-immutability path/to/artifact.tar.gz`,
	Args: ExactArgsWithUsage(1, "anvil artifact verify-immutability path/to/artifact.tar.gz"),
	RunE: runVerifyImmutability,
}

func init() {
	artifactCmd.AddCommand(verifyImmutabilityCmd)
}

// runVerifyImmutability executes the artifact immutability verification command.
func runVerifyImmutability(cmd *cobra.Command, args []string) error {
	artifactPath := args[0]

	result, err := artifact.VerifyImmutability(artifactPath)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: could not verify immutability: %v.\n", err)
		return err
	}

	if result.Passed {
		fmt.Fprintln(cmd.OutOrStdout(), "Immutability verification: PASSED")
		output.PrintStatus(cmd.OutOrStdout(), output.StatusPass, "Checksum: "+result.CurrentChecksum)
		if result.Details != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", result.Details)
		}
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Immutability verification: FAILED")
	output.PrintStatus(cmd.OutOrStdout(), output.StatusFail, "Original checksum: "+result.OriginalChecksum)
	output.PrintStatus(cmd.OutOrStdout(), output.StatusFail, "Current checksum:  "+result.CurrentChecksum)
	if result.Details != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", result.Details)
	}

	return fmt.Errorf("immutability verification failed")
}
