package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/config"
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
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
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
		fmt.Fprintf(cmd.OutOrStdout(), "%s:\n", label)
		if len(data) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "  (not configured)\n\n")
			continue
		}
		// Sort keys for deterministic output.
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s: %v\n", k, data[k])
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}

	// Show resolved values with their source levels.
	fmt.Fprintln(cmd.OutOrStdout(), "Resolved Values:")
	all := cfg.All()
	if len(all) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "  (no values)")
		return nil
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vs := all[k]
		fmt.Fprintf(cmd.OutOrStdout(), "  %s: %v (%s)\n", k, vs.Value, vs.Scope)
	}

	return nil
}
