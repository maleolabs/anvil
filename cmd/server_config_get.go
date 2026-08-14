// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P5-07, ADR-013, EPIC-005
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/server"
)

// serverConfigGetCmd represents the "anvil server config get <key>" command
// that displays a Server Runtime configuration value.
//
// Reference: ST-P5-07, ADR-013
var serverConfigGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a Server Runtime configuration value",
	Long: `Display a Server Runtime configuration value.

Supported keys:
  runtime.id             Server Runtime unique identifier
  runtime.display_name   Human-readable name for this Runtime
  runtime.schema_version Configuration schema version

The configuration is read from /etc/anvil/config.yaml
(or configured override path).

Examples:
  anvil server config get runtime.id
  anvil server config get runtime.display_name`,
	Args: ExactArgsWithUsage(1, "anvil server config get runtime.id"),
	RunE: runServerConfigGet,
}

func init() {
	serverConfigCmd.AddCommand(serverConfigGetCmd)

	serverConfigGetCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
}

// runServerConfigGet executes the "anvil server config get" command.
//
// It resolves the config root path, validates the Runtime is initialized
// (precondition, exit 4 — TS-019-03-02 F-01), loads the configuration,
// and displays the requested value.
func runServerConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]
	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// The initialized Server Runtime is a prerequisite of every server
	// command: an uninitialized Runtime gates with 4 (precondition),
	// never a general error.
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		return err
	}

	store := server.NewConfigStore(rootPath)

	cfg, err := store.Load()
	if err != nil {
		return fmt.Errorf("load server config: %w", err)
	}

	var value string
	switch key {
	case "runtime.id":
		value = cfg.Runtime.ID
	case "runtime.display_name":
		value = cfg.Runtime.DisplayName
	case "runtime.schema_version":
		value = fmt.Sprintf("%d", cfg.Runtime.SchemaVersion)
	default:
		return fmt.Errorf("unsupported key %q", key)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", value)
	return nil
}
