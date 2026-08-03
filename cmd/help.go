// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010 §6.9, TS-P8-02
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// helpCmd represents the "anvil help" parent command group for
// additional help and documentation resources.
//
// Cobra provides a built-in --help flag and "help" command. This
// group extends help functionality with topic-specific documentation
// and guides beyond what the built-in help provides.
//
// Reference: ADR-010 §6.9
var helpCmd = &cobra.Command{
	Use:   "help",
	Short: "Additional help and documentation",
	Long: `Access extended documentation and help resources for Anvil.

In addition to the built-in cobra help system, this command group
provides topic-specific guides, tutorials, configuration references,
and best practices.

Examples:
  anvil help quickstart
  anvil help configuration
  anvil help exit-codes`,
}

func init() {
	rootCmd.AddCommand(helpCmd)
	helpCmd.AddCommand(helpExitCodesCmd)
}

// exitCodesSummary is the concise exit code overview printed in the
// root help. It summarises the general conventions and points to the
// full topic for details.
//
// Reference: ST-P8-05, ADR-010 §8.1
const exitCodesSummary = `Exit Codes:
  0 - Success — the command completed successfully
  1 - General error — an unspecified error occurred
  2 - Configuration error — project configuration is invalid or missing
  3 - Runtime error — the runtime environment is unavailable or misconfigured
  4 - Precondition error — a required prerequisite is missing

Run "anvil help exit-codes" for the complete conventions.`

// exitCodesDetail is the complete exit code conventions reference
// shown by "anvil help exit-codes".
//
// The general categories apply to every command. Non-zero exit codes
// are always accompanied by an error message on stderr. Codes are
// deterministic and stable across versions, so automation consumers
// can rely on them without parsing human-readable output.
//
// Reference: ST-P8-05, ADR-010 §8.1/§9.6
const exitCodesDetail = `Exit code conventions

Every Anvil command exits with a deterministic exit code. Zero
indicates success; non-zero codes identify specific failure categories.
The conventions apply consistently across all commands — the same
failure category always produces the same exit code.

General conventions:

  0 - Success — the command completed successfully
  1 - General error — an unspecified error occurred; the default for
      errors that do not fall into a more specific category
  2 - Configuration error — project configuration is invalid, missing,
      or conflicting (e.g. duplicate project ID)
  3 - Runtime error — a runtime resource is unavailable or not found
      (e.g. project not found in the server registry)
  4 - Precondition error — a required prerequisite is missing
      (e.g. the server runtime is not initialized)

Behavioural guarantees:

  - A non-zero exit code is always accompanied by an error message
    on stderr.
  - Exit codes are stable across versions. Automation consumers may
    rely on the category codes without parsing output text.
  - New commands must use the established conventions.

Command-specific notes are documented in the command help and in
docs/operations/exit-codes.md.`

// helpExitCodesCmd is the "anvil help exit-codes" topic command that
// documents the exit code conventions for all commands.
//
// Reference: ST-P8-05, ADR-010 §8.1/§9.6
var helpExitCodesCmd = &cobra.Command{
	Use:     "exit-codes",
	Short:   "Explain the exit code conventions for all commands",
	Long:    exitCodesDetail,
	Example: `  anvil help exit-codes`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), exitCodesDetail)
		return err
	},
}
