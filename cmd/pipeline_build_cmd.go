package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/execution"
)

var pipelineBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Execute the build pipeline",
	Long: `Execute the build pipeline defined in .anvil/pipelines/build.yaml.

Build steps typically include vendor dependency installation, asset compilation,
and code generation.

The --env flag selects environment-specific configuration (development/production).
Default environment is development.`,
	Example: `  anvil pipeline build
  anvil pipeline build --env production`,
	RunE: runPipelineBuild,
}

func init() {
	pipelineCmd.AddCommand(pipelineBuildCmd)
	pipelineBuildCmd.Flags().String("env", "development", "Environment (development, production)")
}

func runPipelineBuild(cmd *cobra.Command, _ []string) error {
	env, _ := cmd.Flags().GetString("env")

	// Find project root (current directory for now).
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	// Load pipeline definition.
	def, err := execution.LookupBuildDefinition(projectRoot)
	if err != nil {
		return err
	}

	// Create engine and build command.
	runner := execution.NewRunner()
	engine := execution.NewPipelineEngine(runner)
	buildCmd := execution.NewBuildCommand(engine)

	// Execute.
	ctx := cmd.Context()
	report := buildCmd.Execute(ctx, def, env)

	// Display report.
	printPipelineReport(cmd, report)

	if report.Status == "failure" {
		return fmt.Errorf("build pipeline failed")
	}
	return nil
}
