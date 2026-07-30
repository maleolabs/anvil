package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/config"
)

// configCmd represents the "anvil config" command — the parent for all
// configuration inspection subcommands. These commands are read-only and
// must not modify any state.
//
// Reference: ST-P2-05
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration inspection commands",
	Long: `Inspect the resolved Anvil configuration values and their sources.

These commands display the effective configuration after merging values
from all scope levels (global, project, environment, execution) with
their respective precedence. No state is modified.`,
}

// configGetCmd represents the "anvil config get <key>" command that displays
// the resolved value of a single configuration key together with its source
// scope level.
//
// Reference: ST-P2-05
var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Display the resolved value of a configuration key",
	Long: `Show the resolved value of a specific configuration key and the
scope level from which it was sourced.

The value is resolved across all scope levels (global, project,
environment, execution) with deterministic precedence.

Examples:
  anvil config get project.name
  anvil config get global.log_level
  anvil config get release.max_retained`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigGet,
}

// configListCmd represents the "anvil config list" command that displays
// all resolved configuration values with their source scope levels.
//
// Reference: ST-P2-05
var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all resolved configuration values with their sources",
	Long: `Show all resolved configuration values together with the scope
level each value was sourced from.

Values are displayed in a sorted key-value listing with source
annotations. No state is modified.

Examples:
  anvil config list`,
	Args: cobra.NoArgs,
	RunE: runConfigList,
}

func init() {
	configCmd.AddCommand(configGetCmd, configListCmd)
	rootCmd.AddCommand(configCmd)
}

// runConfigGet implements the "anvil config get" command.
func runConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	// Check whether the key exists in the canonical schema.
	schema := config.GetSchema()
	entry, ok := schema.Entries[key]
	if !ok {
		return fmt.Errorf("key '%s' is not defined in the canonical schema", key)
	}

	// Load the configuration.
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	// Use the typed accessor that matches the schema type.
	var value interface{}
	var scope config.ScopeLevel

	switch entry.Type {
	case config.TypeString:
		v, s, err := cfg.GetString(key)
		if err != nil {
			return fmt.Errorf("key '%s' has no resolved value (not set at any level)", key)
		}
		value, scope = v, s

	case config.TypeInteger:
		v, s, err := cfg.GetInt(key)
		if err != nil {
			return fmt.Errorf("key '%s' has no resolved value (not set at any level)", key)
		}
		value, scope = v, s

	case config.TypeBoolean:
		v, s, err := cfg.GetBool(key)
		if err != nil {
			return fmt.Errorf("key '%s' has no resolved value (not set at any level)", key)
		}
		value, scope = v, s

	case config.TypeArray:
		v, s, err := cfg.GetStringSlice(key)
		if err != nil {
			return fmt.Errorf("key '%s' has no resolved value (not set at any level)", key)
		}
		value, scope = v, s

	default:
		// Fall back to generic Get for unknown types.
		v, s, err := cfg.Get(key)
		if err != nil {
			return fmt.Errorf("key '%s' has no resolved value (not set at any level)", key)
		}
		value, scope = v, s
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s: %v (source: %s)\n", key, value, scope)
	return nil
}

// runConfigList implements the "anvil config list" command.
func runConfigList(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	all := cfg.All()

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		vs := all[key]
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %v (source: %s)\n", key, vs.Value, vs.Scope)
	}

	return nil
}
