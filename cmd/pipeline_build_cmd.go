package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/project"
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
so pipeline tasks can reference it via $ANVIL_OUTPUT_DIR.

The --target flag restricts execution to the named build targets (comma-separated,
e.g. "web,apk"). A task runs when its metadata target (or its task name when no
metadata target is declared) matches a requested target. Unknown target names
are rejected before execution. Without --target, all compatible targets build.

The --strict flag turns unsupported build targets into failures: instead of
skipping a target that is not supported on the current platform with a warning,
the pipeline fails (ADR-018).`,
	Example: `  anvil pipeline build
  anvil pipeline build --env production
  anvil pipeline build --output dist/binaries
  anvil pipeline build -o dist/binaries --env production
  anvil pipeline build --target web,apk
  anvil pipeline build --target ios --strict`,
	RunE: runPipelineBuild,
}

func init() {
	pipelineCmd.AddCommand(pipelineBuildCmd)
	pipelineBuildCmd.Flags().String("env", "development", "Environment (development, production)")
	pipelineBuildCmd.Flags().StringP("output", "o", "", "Output directory for build artifacts (sets ANVIL_OUTPUT_DIR)")
	pipelineBuildCmd.Flags().String("target", "", `Comma-separated build targets to execute (e.g. "web,apk"); empty runs all compatible targets`)
	pipelineBuildCmd.Flags().Bool("strict", false, "Fail instead of skipping build targets unsupported on the current platform")
}

func runPipelineBuild(cmd *cobra.Command, _ []string) error {
	env, _ := cmd.Flags().GetString("env")
	outputDir, _ := cmd.Flags().GetString("output")
	targetFlag, _ := cmd.Flags().GetString("target")
	strict, _ := cmd.Flags().GetBool("strict")

	// Resolve the project root via project discovery so the pipeline
	// definition and relative --output paths anchor to the project root —
	// not the current working directory — when the command runs from a
	// project subdirectory (TD-005). This mirrors the discovery pattern
	// used by artifact packaging (BUG-005) and project status.
	projectRoot, err := project.Discover()
	if err != nil {
		return err
	}

	// Load pipeline definition.
	def, err := execution.LookupBuildDefinition(projectRoot)
	if err != nil {
		return err
	}

	// Parse the --target flag and validate the requested targets against
	// the pipeline definition before any task executes (TS-P7-24).
	targets := parseTargetList(targetFlag)
	if err := execution.ValidateTargets(def, targets); err != nil {
		return err
	}

	// Build pipeline-level environment variables from flags.
	pipelineEnv := buildPipelineEnv(projectRoot, outputDir)

	// Create progress reporter — auto-detects terminal capabilities.
	// Interactive terminals get colors + spinner; piped output gets plain text.
	reporter := output.NewProgressReporter(cmd.OutOrStdout())

	// Create engine with progress reporter, warning sink for skipped
	// targets (ADR-018), and build command.
	runner := execution.NewRunner()
	engine := execution.NewPipelineEngine(runner,
		execution.WithProgressReporter(reporter),
		execution.WithWarningWriter(cmd.ErrOrStderr()),
	)
	buildCmd := execution.NewBuildCommand(engine)

	// Execute.
	ctx := cmd.Context()
	report := buildCmd.ExecuteWithOptions(ctx, def, env, pipelineEnv, execution.ExecuteOptions{
		Targets: targets,
		Strict:  strict,
	})

	// Display report.
	printPipelineReport(cmd, report)

	if report.Status == "failure" {
		return fmt.Errorf("build pipeline failed")
	}
	return nil
}

// parseTargetList splits a comma-separated --target value into a trimmed
// list of target names. Empty or whitespace-only input yields nil
// (no filtering).
func parseTargetList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	targets := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			targets = append(targets, p)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	return targets
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
