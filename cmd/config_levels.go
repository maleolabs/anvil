package cmd

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/registry"
)

// configLevelsCmd represents the "anvil config levels" command that
// displays configuration organized by scope level (Global, Project,
// Environment, Execution) showing which values are defined at each level
// and which level provides the winning value for every key.
//
// Reference: ST-P2-08
var configLevelsCmd = &cobra.Command{
	Use:   "levels",
	Short: "Display configuration values organized by scope level",
	Long: `Show all configuration values organized by their source scope level.

Each scope level (Global, Project, Environment, Execution) is displayed
with its keys and values. Empty levels are shown as "(not configured)".
When an environment is active (ANVIL_ENV is set), the Environment level
shows values from that environment configuration file.

The "Resolved Values" section shows every key with the winning value
and which level provided it.

This command is read-only and does not modify any files or state.

Examples:
  anvil config levels`,
	Args: cobra.NoArgs,
	RunE: runConfigLevels,
}

func init() {
	configCmd.AddCommand(configLevelsCmd)
}

// runConfigLevels implements the "anvil config levels" command.
func runConfigLevels(cmd *cobra.Command, args []string) error {
	s := styleFor(cmd)
	cfg, err := config.LoadConfig()
	if err != nil {
		return classifyConfigLoadError(cmd, err)
	}

	// TS-015-03-02: standard-driven framework validation on the load path
	// (see runConfigGet — implicit and explicit validation never diverge).
	// Invalid framework configuration exits 2; a declared framework
	// without an installed standard exits 4 (D-02).
	flat := flatResolvedConfig(cfg)
	frameworkErrs, ferr := validateFrameworkConfig(flat)
	if ferr != nil {
		if errors.Is(ferr, registry.ErrStandardNotInstalled) {
			return standardMissingError(flat, ferr)
		}
		return ferr
	}
	if len(frameworkErrs) > 0 {
		return configInvalidError(cmd, fmt.Errorf("configuration validation failed:\n%s", config.FormatValidationErrors(frameworkErrs)))
	}

	// Determine the active environment name for display.
	envName := os.Getenv("ANVIL_ENV")

	// Define the levels to display in order (Global → Project → Environment → Execution).
	type levelInfo struct {
		level     config.ScopeLevel
		label     string
		extraInfo string // optional suffix like "(production)"
	}
	levels := []levelInfo{
		{config.ScopeGlobal, "Global Level", ""},
		{config.ScopeProject, "Project Level", ""},
		{config.ScopeEnvironment, "Environment Level", envName},
		{config.ScopeExecution, "Execution Level", ""},
	}

	for _, li := range levels {
		data := cfg.LevelMap(li.level)
		label := li.label
		if li.extraInfo != "" {
			label = fmt.Sprintf("%s (%s)", label, li.extraInfo)
		}
		fmt.Fprintf(s.W, "%s:\n", label)
		if len(data) == 0 {
			fmt.Fprintf(s.W, "  (not configured)\n\n")
			continue
		}
		// Sort keys for deterministic output.
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(s.W, "  %s: %v\n", k, data[k])
		}
		fmt.Fprintln(s.W)
	}

	// Show resolved values with their source levels.
	fmt.Fprintln(s.W, "Resolved Values:")
	all := cfg.All()
	if len(all) == 0 {
		fmt.Fprintln(s.W, "  (no values)")
		return nil
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vs := all[k]
		fmt.Fprintf(s.W, "  %s: %v (%s)\n", k, vs.Value, vs.Scope)
	}

	return nil
}
