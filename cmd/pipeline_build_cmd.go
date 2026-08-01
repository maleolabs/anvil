package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/output"
)

var pipelineBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Execute the build pipeline",
	Long: `Execute the build pipeline defined in .anvil/pipelines/build.yaml.

Build steps typically include vendor dependency installation, asset compilation,
and code generation.

The --env flag selects environment-specific configuration (development/production).
Default environment is development.

The --output flag sets the output directory for build artifacts. The resolved
absolute path is injected as ANVIL_OUTPUT_DIR into every task's environment,
so pipeline tasks can reference it via $ANVIL_OUTPUT_DIR.`,
	Example: `  anvil pipeline build
  anvil pipeline build --env production
  anvil pipeline build --output dist/binaries
  anvil pipeline build -o dist/binaries --env production`,
	RunE: runPipelineBuild,
}

func init() {
	pipelineCmd.AddCommand(pipelineBuildCmd)
	pipelineBuildCmd.Flags().String("env", "development", "Environment (development, production)")
	pipelineBuildCmd.Flags().StringP("output", "o", "", "Output directory for build artifacts (sets ANVIL_OUTPUT_DIR)")
}

func runPipelineBuild(cmd *cobra.Command, _ []string) error {
	env, _ := cmd.Flags().GetString("env")
	outputDir, _ := cmd.Flags().GetString("output")

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

	// Build pipeline-level environment variables from flags.
	pipelineEnv := buildPipelineEnv(projectRoot, outputDir)

	// Create progress reporter — auto-detects terminal capabilities.
	// Interactive terminals get colors + spinner; piped output gets plain text.
	reporter := output.NewProgressReporter(cmd.OutOrStdout())

	// Create engine with progress reporter and build command.
	runner := execution.NewRunner()
	engine := execution.NewPipelineEngine(runner, execution.WithProgressReporter(reporter))
	buildCmd := execution.NewBuildCommand(engine)

	// Execute.
	ctx := cmd.Context()
	report := buildCmd.Execute(ctx, def, env, pipelineEnv)

	// Display report.
	printPipelineReport(cmd, report)

	if report.Status == "failure" {
		return fmt.Errorf("build pipeline failed")
	}
	return nil
}

// buildPipelineEnv constructs the pipeline-level environment variables from
// CLI flags. Currently it sets ANVIL_OUTPUT_DIR when --output is provided.
//
// The output directory is resolved to an absolute path relative to the project
// root so that tasks in any working directory can rely on a canonical path.
func buildPipelineEnv(projectRoot, outputDir string) map[string]string {
	if outputDir == "" {
		return nil
	}

	// Resolve to absolute path. If outputDir is already absolute, filepath.Abs
	// returns it unchanged. Otherwise it is resolved relative to projectRoot.
	absPath := outputDir
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(projectRoot, outputDir)
	}
	absPath = filepath.Clean(absPath)

	return map[string]string{
		"ANVIL_OUTPUT_DIR": absPath,
	}
}
