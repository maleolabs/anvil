// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P3-01, ADR-010, ADR-012, EPIC-003
package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/project"
)

// artifactCmd represents the 'anvil artifact' parent command.
var artifactCmd = &cobra.Command{
	Use:   "artifact",
	Short: "Manage artifacts",
	Long: `Create and inspect distributable artifacts for Anvil projects.

Artifacts are immutable, self-describing archives that contain
filtered project source files, a manifest with identity and
metadata, and integrity evidence for verification.`,
}

// packageCmd represents the 'anvil artifact package' subcommand.
var packageCmd = &cobra.Command{
	Use:   "package",
	Short: "Package project source into a distributable artifact",
	Args:  cobra.NoArgs,
	Long: `Create a distributable artifact from the project source files.

The packaging process:
  1. Reads file inclusion and exclusion rules from project configuration
  2. Filters project files according to the configured rules
  3. Generates a content-derived identity for the artifact
  4. Computes an integrity checksum
  5. Creates a manifest with identity, version, timestamp, and checksum
  6. Produces archive(s) containing filtered files and the manifest

By default, both tar.gz (primary) and zip (secondary) archives are created.
Use --format to control which formats are produced.

The artifact is written to the specified output directory
(default: .anvil/artifacts).

Examples:
  anvil artifact package
  anvil artifact package --output ./dist
  anvil artifact package -o /tmp/builds
  anvil artifact package --format zip
  anvil artifact package --format tar.gz,zip
  anvil artifact package --output ./dist --json`,
	RunE: runPackage,
}

func init() {
	artifactCmd.AddCommand(packageCmd)
	rootCmd.AddCommand(artifactCmd)

	packageCmd.Flags().StringP("output", "o", "",
		"Output directory for the artifact (default: .anvil/artifacts)")
	packageCmd.Flags().StringP("format", "f", "tar.gz,zip",
		"Archive format(s) to produce (tar.gz, zip, or tar.gz,zip)")
	packageCmd.Flags().Bool("json", false,
		"Output artifact details in JSON format for machine consumption")
}

// runPackage executes the artifact packaging command.
func runPackage(cmd *cobra.Command, args []string) error {
	// Require a valid Anvil project context.
	cfg, err := RequireProject(cmd)
	if err != nil {
		return err
	}

	// Check if JSON output is requested — skip UX enhancements for machine mode.
	jsonFlag, _ := cmd.Flags().GetBool("json")

	// Create step reporter for human-readable mode only.
	var reporter output.StepReporter
	if !jsonFlag {
		reporter = output.NewStepReporter(cmd.OutOrStdout())
		reporter.Start("Package Artifact")
	}

	overallStart := time.Now()

	// Resolve the project root via project discovery. The packaging source
	// and relative --output paths anchor to the project root — not the
	// current working directory — so invoking the command from a project
	// subdirectory still packages the whole project (BUG-005). This mirrors
	// the discovery pattern used by resolveStateDir in artifact_status.go.
	root, err := project.Discover()
	if err != nil {
		return err
	}
	sourceDir := root

	// Resolve output directory with the following precedence:
	//   1. --output / -o flag (highest)
	//   2. artifact.output from project config
	//   3. Hardcoded default: .anvil/artifacts
	outputFlag, _ := cmd.Flags().GetString("output")
	var outputDir string
	if outputFlag != "" {
		outputDir = outputFlag
	} else if cfg.Artifact != nil && cfg.Artifact.Output != "" {
		outputDir = cfg.Artifact.Output
	} else {
		outputDir = ".anvil/artifacts"
	}

	// Clean the path to normalize trailing slashes and resolve dots.
	outputDir = filepath.Clean(outputDir)

	// Ensure the output path is absolute for consistent behavior.
	if !filepath.IsAbs(outputDir) {
		absSource, err := filepath.Abs(sourceDir)
		if err == nil {
			outputDir = filepath.Join(absSource, outputDir)
		}
	}

	// Read filtering rules from configuration.
	var include []string
	var exclude []string

	if cfg.Artifact != nil {
		include = cfg.Artifact.Include
		exclude = cfg.Artifact.Exclude
	}

	// Read project identity and metadata for the manifest.
	identity := cfg.Identity()
	metadata := cfg.Metadata()
	version := metadata.Version()
	source := identity.Name()
	projectID := identity.Name()

	// Resolve the framework's manifest activation/rollback commands
	// (TS-P7-15, TS-P7-16 — deferred wiring, now implemented). The
	// command strings come from the framework adapter executable through
	// the manifest command (005-adapter-command-contract §10.10) — the
	// Core never imports internal/laravel or internal/flutter (ADR-009
	// §8.1). The adapter is OPTIONAL: a project without a framework,
	// without the adapter binary installed, or with a failing manifest
	// invocation keeps the pre-existing behavior and packages without
	// activation/rollback commands (backward compatible — the empty
	// slices are omitted from the manifest by omitempty). Packaging is a
	// core operation and must not be blocked by an optional adapter, so
	// adapter problems degrade to a warning.
	var activationCommands, rollbackCommands []string
	if framework := activeProjectFramework(); framework != "" {
		executable, err := adapterExecutableLookup("anvil-adapter-" + framework)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), FmtWarning("adapter executable %q for framework %q not found; packaging without manifest activation/rollback commands"), "anvil-adapter-"+framework, framework)
		} else {
			manifestResult, err := invokeAdapterManifestCommands(cmd.Context(), framework, executable)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), FmtWarning("could not fetch manifest commands from adapter %q for framework %q: %v; packaging without manifest activation/rollback commands"), executable, framework, err)
			} else {
				activationCommands = manifestResult.ActivationCommands
				rollbackCommands = manifestResult.RollbackCommands
			}
		}
	}

	// Resolve archive formats from flag.
	formatFlag, _ := cmd.Flags().GetString("format")
	formats := parseFormats(formatFlag)

	// Create packaging reporter adapter (bridges StepReporter to PackagingReporter).
	var pkgReporter artifact.PackagingReporter
	if reporter != nil {
		pkgReporter = &packagingReporterAdapter{reporter: reporter}
	}

	// Execute the packaging engine.
	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir:          sourceDir,
		OutputDir:          outputDir,
		Formats:            formats,
		Include:            include,
		Exclude:            exclude,
		Version:            version,
		Source:             source,
		ProjectID:          projectID,
		ActivationCommands: activationCommands,
		RollbackCommands:   rollbackCommands,
		Reporter:           pkgReporter,
	})
	if err != nil {
		if reporter != nil {
			reporter.Failed("Package Artifact", time.Since(overallStart))
		}
		return ReportPlainErrorf(cmd, err, "could not package artifact: %v", err)
	}

	// Complete the reporter.
	if reporter != nil {
		reporter.Complete("Package Artifact", time.Since(overallStart))
	}

	// JSON mode: output machine-readable result.
	if jsonFlag {
		return outputPackageJSON(cmd, result)
	}

	// Display human-readable result.
	manifestID := ""
	if result.Manifest != nil {
		manifestID = result.Manifest.ArtifactID
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nArtifact created:\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Primary:   %s\n", result.ArtifactPath)
	if result.SecondaryPath != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Secondary: %s\n", result.SecondaryPath)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "  Files:     %d\n", result.FileCount)
	if manifestID != "" {
		shortID := manifestID
		if len(shortID) > 16 {
			shortID = shortID[:16] + "..."
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  ID:        %s\n", shortID)
	}

	return nil
}

// packagingReporterAdapter bridges output.StepReporter to artifact.PackagingReporter.
type packagingReporterAdapter struct {
	reporter output.StepReporter
}

func (a *packagingReporterAdapter) StepStart(name string) {
	a.reporter.StepStart(name)
}

func (a *packagingReporterAdapter) StepComplete(name string, duration time.Duration) {
	a.reporter.StepComplete(name, duration)
}

func (a *packagingReporterAdapter) StepFailed(name string, duration time.Duration, err error) {
	a.reporter.StepFailed(name, duration, err)
}

// packageJSONOutput is the machine-readable output format for --json flag.
type packageJSONOutput struct {
	Artifact  artifactJSON `json:"artifact"`
	FileCount int          `json:"file_count"`
}

type artifactJSON struct {
	ID        string `json:"id"`
	Primary   string `json:"primary"`
	Secondary string `json:"secondary,omitempty"`
}

// outputPackageJSON writes the packaging result as JSON to stdout.
func outputPackageJSON(cmd *cobra.Command, result *artifact.PackageResult) error {
	artifactID := ""
	if result.Manifest != nil {
		artifactID = result.Manifest.ArtifactID
	}

	out := packageJSONOutput{
		Artifact: artifactJSON{
			ID:        artifactID,
			Primary:   result.ArtifactPath,
			Secondary: result.SecondaryPath,
		},
		FileCount: result.FileCount,
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode JSON output: %w", err)
	}

	return nil
}

// parseFormats parses the --format flag value into a slice of format strings.
// Accepts comma-separated values like "tar.gz" or "tar.gz,zip".
func parseFormats(raw string) []string {
	if raw == "" {
		return nil
	}

	var formats []string
	for _, f := range strings.Split(raw, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			formats = append(formats, f)
		}
	}
	return formats
}
