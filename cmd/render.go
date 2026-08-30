package cmd

import (
	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
)

const flagVerbose = "verbose"
const flagVersion = "version"

func styleFor(cmd *cobra.Command) *output.Style {
	verbose, err := cmd.Flags().GetBool(flagVerbose)
	if err != nil {
		verbose = false
	}
	if cmd.Root() != nil {
		if v, err := cmd.Root().PersistentFlags().GetBool(flagVerbose); err == nil {
			verbose = v
		}
	}
	return output.NewStyle(cmd.OutOrStdout(), verbose)
}
