package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/project"
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
	// Resolve the project root via project discovery so the pipeline
	// definition anchors to the project root — not the current working
	// directory — when the command runs from a project subdirectory
	// (TD-005). This mirrors the discovery pattern used by artifact
	// packaging (BUG-005) and project status.
	projectRoot, err := project.Discover()
	if err != nil {
		return err
	}

	// Load pipeline definition.
	def, err := execution.LookupCIDefinition(projectRoot)
	if err != nil {
		return err
	}

	// Create progress reporter — auto-detects terminal capabilities.
	// Interactive terminals get colors + spinner; piped output gets plain text.
	reporter := output.NewProgressReporter(styleFor(cmd).W)

	// Create engine with progress reporter and CI command.
	runner := execution.NewRunner()
	engine := execution.NewPipelineEngine(runner, execution.WithProgressReporter(reporter))
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
