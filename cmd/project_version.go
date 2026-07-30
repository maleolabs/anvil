// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010, ADR-015, EPIC-001
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"maleolabs.com/anvil/internal/project"
)

// versionCmd represents the 'anvil project version' command.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display or manage project version",
	Long: `Display the current project version or manage version bumps.

Version is read from and written to the project's anvil.yaml file.
The version follows Semantic Versioning (MAJOR.MINOR.PATCH).

Use 'bump:patch', 'bump:minor', or 'bump:major' to increment.
Use 'generate' to produce a VERSION file for runtime consumption.`,
}

// versionSetCmd represents 'anvil project version set <version>'.
var versionSetCmd = &cobra.Command{
	Use:   "set <version>",
	Short: "Set version to a specific value",
	Args:  cobra.ExactArgs(1),
	Long: `Set the project version to an exact SemVer string.

Use this command to synchronize the project version with an external
source such as a git tag. The version must follow MAJOR.MINOR.PATCH
format.

Example: anvil project version set 1.2.3`,
	RunE: runVersionSet,
}

// versionBumpPatchCmd represents 'anvil project version bump:patch'.
var versionBumpPatchCmd = &cobra.Command{
	Use:   "bump:patch",
	Short: "Bump patch version (x.y.Z)",
	Args:  cobra.NoArgs,
	Long: `Increment the patch version number.

Patch releases are for backward-compatible bug fixes.
Example: 1.0.0 -> 1.0.1`,
	Example: `  anvil project version bump:patch`,
	RunE:    runVersionBumpPatch,
}

// versionBumpMinorCmd represents 'anvil project version bump:minor'.
var versionBumpMinorCmd = &cobra.Command{
	Use:   "bump:minor",
	Short: "Bump minor version (x.Y.z)",
	Args:  cobra.NoArgs,
	Long: `Increment the minor version number and reset patch to 0.

Minor releases are for backward-compatible new features.
Example: 1.0.1 -> 1.1.0`,
	Example: `  anvil project version bump:minor`,
	RunE:    runVersionBumpMinor,
}

// versionBumpMajorCmd represents 'anvil project version bump:major'.
var versionBumpMajorCmd = &cobra.Command{
	Use:   "bump:major",
	Short: "Bump major version (X.y.z)",
	Args:  cobra.NoArgs,
	Long: `Increment the major version number and reset minor and patch to 0.

Major releases are for backward-incompatible changes.
Example: 1.2.3 -> 2.0.0`,
	Example: `  anvil project version bump:major`,
	RunE:    runVersionBumpMajor,
}

// versionGenerateCmd represents 'anvil project version generate'.
var versionGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate VERSION file for runtime consumption",
	Args:  cobra.NoArgs,
	Long: `Generate a VERSION file from the project section of anvil.yaml.

All fields under the 'project' key are written as KEY=VALUE pairs
with uppercase keys. If no timestamp field exists, TIMESTAMP is
automatically added with the current time.

The VERSION file is typically used by web applications at runtime
to display version metadata without parsing YAML.`,
	Example: `  anvil project version generate`,
	RunE:    runVersionGenerate,
}

func init() {
	projectCmd.AddCommand(versionCmd)

	// Register subcommands for set, bump, and generate.
	versionCmd.AddCommand(versionSetCmd)
	versionCmd.AddCommand(versionBumpPatchCmd)
	versionCmd.AddCommand(versionBumpMinorCmd)
	versionCmd.AddCommand(versionBumpMajorCmd)
	versionCmd.AddCommand(versionGenerateCmd)

	// When no subcommand is given, display the current version.
	versionCmd.RunE = runVersionDisplay
}

func runVersionDisplay(cmd *cobra.Command, args []string) error {
	cfg, err := RequireProject(cmd)
	if err != nil {
		return err
	}

	version := "0.0.0"
	if cfg.Project != nil && cfg.Project.Version != "" {
		version = cfg.Project.Version
	}

	fmt.Fprintf(cmd.OutOrStdout(), "v%s\n", version)
	return nil
}

// getConfigDir returns the directory containing anvil.yaml.
func getConfigDir() (string, error) {
	root, err := project.Discover()
	if err != nil {
		return "", fmt.Errorf("no project found: %w", err)
	}
	return root, nil
}

// parseSemVer parses a SemVer string into its components.
// Returns 0.0.0 on error.
func parseSemVer(version string) (major, minor, patch int) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 3 {
		return 0, 0, 0
	}

	major, _ = strconv.Atoi(parts[0])
	minor, _ = strconv.Atoi(parts[1])
	patch, _ = strconv.Atoi(parts[2])

	return major, minor, patch
}

// formatSemVer formats version components back into a SemVer string.
func formatSemVer(major, minor, patch int) string {
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

// bumpVersion reads the current version, applies a bump function, and writes
// the result back to anvil.yaml while preserving all custom fields.
func bumpVersion(cmd *cobra.Command, bumpFn func(major, minor, patch int) (int, int, int)) error {
	root, err := getConfigDir()
	if err != nil {
		return err
	}

	configPath := filepath.Join(root, project.ConfigFileName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", configPath, err)
	}

	// Parse as generic map to preserve custom fields during write-back.
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", configPath, err)
	}

	// Navigate to project.version.
	proj, ok := doc["project"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid project section in %s", configPath)
	}

	oldVersion, _ := proj["version"].(string)
	if oldVersion == "" {
		oldVersion = "0.0.0"
	}

	major, minor, patch := parseSemVer(oldVersion)
	newMajor, newMinor, newPatch := bumpFn(major, minor, patch)
	newVersion := formatSemVer(newMajor, newMinor, newPatch)

	proj["version"] = newVersion

	// Write back preserving all original formatting and custom fields.
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, out, 0644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", newVersion)
	return nil
}

func runVersionBumpPatch(cmd *cobra.Command, args []string) error {
	return bumpVersion(cmd, func(major, minor, patch int) (int, int, int) {
		return major, minor, patch + 1
	})
}

func runVersionBumpMinor(cmd *cobra.Command, args []string) error {
	return bumpVersion(cmd, func(major, minor, patch int) (int, int, int) {
		return major, minor + 1, 0
	})
}

func runVersionBumpMajor(cmd *cobra.Command, args []string) error {
	return bumpVersion(cmd, func(major, minor, patch int) (int, int, int) {
		return major + 1, 0, 0
	})
}

func runVersionSet(cmd *cobra.Command, args []string) error {
	newVersion := args[0]

	// Basic validation: ensure it looks like a SemVer string.
	major, minor, patch := parseSemVer(newVersion)
	if major == 0 && minor == 0 && patch == 0 && newVersion != "0.0.0" {
		return fmt.Errorf("invalid version %q — expected MAJOR.MINOR.PATCH format (e.g., 1.2.3)", newVersion)
	}
	_ = minor
	_ = patch

	return bumpVersion(cmd, func(_, _, _ int) (int, int, int) {
		return major, minor, patch
	})
}

func runVersionGenerate(cmd *cobra.Command, args []string) error {
	// Load project to validate we are in a project context.
	cfg, err := RequireProject(cmd)
	if err != nil {
		return err
	}

	// Determine project root for the VERSION file output.
	root, err := project.Discover()
	if err != nil {
		return err
	}

	versionFilePath := filepath.Join(root, "VERSION")

	// Build VERSION content from project section.
	// We marshal the project section to YAML-like key-value pairs.
	// All fields under 'project' in anvil.yaml become KEY=VALUE entries,
	// with keys transformed to UPPERCASE.
	var lines []string

	if cfg.Project != nil {
		if cfg.Project.Name != "" {
			lines = append(lines, fmt.Sprintf("NAME=%s", cfg.Project.Name))
		}
		if cfg.Project.Version != "" {
			lines = append(lines, fmt.Sprintf("VERSION=%s", cfg.Project.Version))
		}
		if cfg.Project.Description != "" {
			lines = append(lines, fmt.Sprintf("DESCRIPTION=%s", cfg.Project.Description))
		}
	}

	// Check for custom fields by re-reading raw YAML to capture
	// any extra fields the user added under 'project'.
	// These are fields that don't exist in the Go struct but are present
	// in the YAML file (e.g., release_codename, status, minimum_php).
	hasTimestamp := false
	rawData, err := os.ReadFile(filepath.Join(root, project.ConfigFileName))
	if err == nil {
		var rawDoc map[string]interface{}
		if yaml.Unmarshal(rawData, &rawDoc) == nil {
			if proj, ok := rawDoc["project"]; ok {
				if projMap, ok := proj.(map[string]interface{}); ok {
					for k := range projMap {
						upper := strings.ToUpper(k)
						if upper == "TIMESTAMP" || upper == "RELEASE_DATE" || upper == "DATE" {
							hasTimestamp = true
						}
						// Add all fields that are not already handled above.
						upperKey := strings.ToUpper(k)
						if upperKey != "NAME" && upperKey != "VERSION" && upperKey != "DESCRIPTION" {
							val := fmt.Sprintf("%v", projMap[k])
							lines = append(lines, fmt.Sprintf("%s=%s", upperKey, val))
						}
					}
				}
			}
		}
	}

	// Add TIMESTAMP if no timestamp/release_date/date field was provided.
	if !hasTimestamp {
		lines = append(lines, fmt.Sprintf("TIMESTAMP=%s", time.Now().UTC().Format(time.RFC3339)))
	}

	content := strings.Join(lines, "\n") + "\n"

	if err := os.WriteFile(versionFilePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write VERSION file: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "VERSION file created: %s\n", versionFilePath)
	return nil
}
