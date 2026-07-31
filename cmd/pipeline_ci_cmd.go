package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/execution"
)

var pipelineCiCmd = &cobra.Command{
	Use:   "ci",
	Short: "Execute the CI pipeline",
	Long: `Execute the CI pipeline defined in .anvil/pipelines/ci.yaml.

CI pipelines typically run build and test stages to validate code changes
before deployment. The CI pipeline runs without environment-specific overrides.`,
	Example: `  anvil pipeline ci`,
	RunE:    runPipelineCi,
}

func init() {
	pipelineCmd.AddCommand(pipelineCiCmd)
}

func runPipelineCi(cmd *cobra.Command, _ []string) error {
	// Find project root (current directory for now).
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	// Load pipeline definition.
	def, err := execution.LookupCIDefinition(projectRoot)
	if err != nil {
		return err
	}

	// Create engine and CI command.
	runner := execution.NewRunner()
	engine := execution.NewPipelineEngine(runner)
	ciCmd := execution.NewCICommand(engine)

	// Execute.
	ctx := cmd.Context()
	report := ciCmd.Execute(ctx, def)

	// Display report.
	printPipelineReport(cmd, report)

	if report.Status == "failure" {
		return fmt.Errorf("CI pipeline failed")
	}
	return nil
}
