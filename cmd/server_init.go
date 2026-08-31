// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P5-07, ADR-013, EPIC-005
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/server"
)

// serverInitCmd represents the "anvil server init" command that initializes
// the Server Runtime configuration store.
//
// It creates the Runtime configuration directory and default configuration
// file at /etc/anvil/config.yaml (or configured override path).
//
// Safe to retry — if configuration already exists, reports and exits
// cleanly without modification.
//
// Does not register projects, install artifacts, or create releases.
// Does not inspect a repository or require anvil.yaml.
//
// Reference: ST-P5-07, ADR-013
var serverInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Server Runtime configuration",
	Long: `Initialize the Server Runtime configuration store.

Creates the Runtime configuration directory and default configuration
file at /etc/anvil/config.yaml (or configured override path).

Safe to retry — if configuration already exists, reports and exits
cleanly without modification.

Does not register projects, install artifacts, or create releases.
Does not inspect a repository or require anvil.yaml.`,
	Example: `  anvil server init
  anvil server init --server-root /tmp/anvil`,
	Args: cobra.NoArgs,
	RunE: runServerInit,
}

func init() {
	serverCmd.AddCommand(serverInitCmd)

	serverInitCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
}

// runServerInit executes the "anvil server init" command.
//
// It resolves the config root path from:
//  1. --server-root flag (highest priority)
//  2. ANVIL_SERVER_ROOT environment variable
//  3. Default /etc/anvil
//
// A warning is printed when a non-default root is used.
func runServerInit(cmd *cobra.Command, args []string) error {
	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	result, err := server.InitializeServer(rootPath)
	if err != nil {
		return fmt.Errorf("server init: %w", err)
	}

	if result.AlreadyInitialized {
		PrintSuccess(cmd, "Server Runtime already initialized.")
		fmt.Fprintf(styleFor(cmd).W, "  Config: %s\n", result.ConfigPath)
	} else {
		PrintSuccess(cmd, "Server Runtime initialized.")
		fmt.Fprintf(styleFor(cmd).W, "  Config: %s\n", result.ConfigPath)
		fmt.Fprintln(styleFor(cmd).W, "")
		fmt.Fprintln(styleFor(cmd).W, "Next steps:")
		fmt.Fprintln(styleFor(cmd).W, "  Edit the configuration and set runtime.id:")
		fmt.Fprintf(styleFor(cmd).W, "    anvil server config set runtime.id <server-id>\n")
	}

	return nil
}

// resolveServerRoot resolves the effective server root path by checking
// (in priority order): --server-root flag, ANVIL_SERVER_ROOT env var,
// and server.DefaultConfigRoot.
//
// Reference: Decision 005 §4
func resolveServerRoot(cmd *cobra.Command) string {
	// 1. Check --server-root flag (highest priority).
	if flag, _ := cmd.Flags().GetString("server-root"); flag != "" {
		return flag
	}

	// 2. Check ANVIL_SERVER_ROOT environment variable.
	if envRoot := os.Getenv(server.EnvServerRoot); envRoot != "" {
		return envRoot
	}

	// 3. Use production default.
	return server.DefaultConfigRoot
}
