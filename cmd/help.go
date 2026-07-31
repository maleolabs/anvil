// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010 §6.9, TS-P8-02
package cmd

import (
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
  anvil help release-lifecycle`,
}

func init() {
	rootCmd.AddCommand(helpCmd)
}
