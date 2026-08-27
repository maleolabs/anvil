// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P5-07, ADR-013, EPIC-005
package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/server"
)

// serverConfigSetCmd represents the "anvil server config set <key> <value>"
// command that sets a Server Runtime configuration value.
//
// Reference: ST-P5-07, ADR-013
var serverConfigSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a Server Runtime configuration value",
	Long: `Set a Server Runtime configuration value.

Supported keys:
  runtime.id             Server Runtime unique identifier (required)
  runtime.display_name   Human-readable name for this Runtime (optional)
  runtime.schema_version Configuration schema version (default: 1)

The configuration is stored at /etc/anvil/config.yaml
(or configured override path).

Examples:
  anvil server config set runtime.id production-server-01
  anvil server config set runtime.display_name "Production Server"
  anvil server config set runtime.schema_version 1`,
	Args: ExactArgsWithUsage(2, "anvil server config set runtime.id my-server"),
	RunE: runServerConfigSet,
}

func init() {
	serverConfigCmd.AddCommand(serverConfigSetCmd)

	serverConfigSetCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
}

// runServerConfigSet executes the "anvil server config set" command.
//
// It resolves the config root path, loads the existing configuration,
// applies the requested change, validates, and persists.
func runServerConfigSet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]
	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	store := server.NewConfigStore(rootPath)

	// Load or initialize config.
	var cfg *server.ServerConfig
	if store.Exists() {
		var err error
		cfg, err = store.Load()
		if err != nil {
			return fmt.Errorf("load server config: %w", err)
		}
	} else {
		def := server.DefaultServerConfig()
		cfg = &def
	}

	// Apply the change based on key.
	switch key {
	case "runtime.id":
		if value == "" {
			return fmt.Errorf("runtime.id must not be empty")
		}
		cfg.Runtime.ID = value

	case "runtime.display_name":
		cfg.Runtime.DisplayName = value

	case "runtime.schema_version":
		ver, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("runtime.schema_version must be an integer: %w", err)
		}
		if ver != 1 {
			return fmt.Errorf("runtime.schema_version must be 1")
		}
		cfg.Runtime.SchemaVersion = ver

	default:
		return fmt.Errorf("unsupported key %q", key)
	}

	// Validate the updated config.
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration after update: %w", err)
	}

	// Persist.
	if err := store.Save(*cfg); err != nil {
		return fmt.Errorf("save server config: %w", err)
	}

	PrintSuccessf(cmd, "Set %s = %s", key, value)
	return nil
}
