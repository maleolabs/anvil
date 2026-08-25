// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-01, ADR-015, EPIC-010, TD-006
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/server"
)

// deploymentInfoCmd represents the "anvil deployment info" subcommand
// that displays deployment target identity and delivery context.
//
// Per ADR-015, this command reports configured target identity and
// delivery context WITHOUT reading Registry, State, release directories,
// or symlinks. It clearly distinguishes Deployment information from
// Runtime status.
//
// This command is the local target-centric alias of the 'server
// status' surface (install/activate/rollback alias the 'server
// release' operations instead): it runs on the local server runtime
// and requires a locally initialized server. It is NOT an SSH
// transport command (TD-006).
//
// Reference: ST-P10-01, ADR-015, TD-006
var deploymentInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Display deployment target and delivery context",
	Long: `Display deployment target identity and delivery context.

This is a local target-centric view of the server runtime: it runs on
the local server runtime and requires a locally initialized server. It
is NOT an SSH transport command — only 'anvil deployment upload'
transports artifacts to remote targets (TD-006).

This command does NOT read Registry, State, release directories, or
symlinks — it operates independently from Runtime filesystem layout
per ADR-015.

Deployment information is distinct from Runtime status. Use
'anvil server status' to inspect Runtime state.

Use --json to get machine-readable output for CI/CD and automation tools.

Examples:
  anvil deployment info
  anvil deployment info --server-root /tmp/anvil
  anvil deployment info --json`,
	Args: cobra.NoArgs,
	RunE: runDeploymentInfo,
}

func init() {
	deploymentCmd.AddCommand(deploymentInfoCmd)

	deploymentInfoCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
	AddJSONFlag(deploymentInfoCmd)
}

// runDeploymentInfo executes the "anvil deployment info" command.
//
// It resolves the server root, validates the Runtime is initialized,
// loads the deployment target identity from the server config, and
// displays the target information without reading Registry or State.
func runDeploymentInfo(cmd *cobra.Command, args []string) error {
	// Step 1: Resolve the server root.
	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// Step 2: Validate the Runtime is initialized.
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		return err
	}

	// Step 3: Load target information from the server config.
	configStore := server.NewConfigStore(rootPath)
	cfg, err := configStore.Load()
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not load server config: %v", err)
	}

	// Build delivery context info (MVP: runtime.id serves as target identity).
	deliveryInfo := &deploymentInfoOutput{
		TargetName:     cfg.Runtime.DisplayName,
		RuntimeID:      cfg.Runtime.ID,
		ConfigPath:     configStore.ConfigPath(),
		DeliveryStatus: "configured",
	}

	// Step 4: Display the result.
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		return WriteJSON(cmd, deliveryInfo)
	}

	// Human-readable output.
	fmt.Fprintf(cmd.OutOrStdout(), "Deployment Target\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Target Name:   %s\n", deliveryInfo.TargetName)
	fmt.Fprintf(cmd.OutOrStdout(), "  Runtime ID:    %s\n", deliveryInfo.RuntimeID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Config:        %s\n", deliveryInfo.ConfigPath)
	fmt.Fprintf(cmd.OutOrStdout(), "  Delivery:      %s\n", deliveryInfo.DeliveryStatus)
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "Deployment information is distinct from Runtime status.")
	fmt.Fprintln(cmd.OutOrStdout(), "Use 'anvil server status' to inspect Runtime state.")

	return nil
}

// deploymentInfoOutput is the machine-readable output format for --json flag.
type deploymentInfoOutput struct {
	TargetName     string `json:"target_name,omitempty"`
	RuntimeID      string `json:"runtime_id"`
	ConfigPath     string `json:"config_path"`
	DeliveryStatus string `json:"delivery_status"`
}
