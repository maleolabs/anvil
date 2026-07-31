package cmd

import (
	"github.com/spf13/cobra"
)

// pipelineCmd represents the "anvil pipeline" parent command.
var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Execute pipeline workflows",
	Long: `Run build and CI pipelines defined in .anvil/pipelines/.

Pipeline workflows automate common development tasks such as dependency
installation, compilation, testing, and static analysis.

Examples:
  anvil pipeline build
  anvil pipeline build --env production
  anvil pipeline ci`,
}

func init() {
	rootCmd.AddCommand(pipelineCmd)
}
