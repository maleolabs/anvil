package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/runtime"
)

// provisionCmd represents the "anvil runtime provision" command that
// provisions a new Runtime instance with a unique identity, metadata,
// and directory structure.
//
// Reference: ST-P5-01
var provisionCmd = &cobra.Command{
	Use:   "provision",
	Short: "Provision a new Runtime instance",
	Long: `Create a new Runtime with a unique identity, metadata, and directory structure.

The Runtime is initialized in the Provisioned lifecycle stage and is ready
for configuration and Release activation.

Required:
  --name        Runtime name (unique identifier for this Runtime instance)

Optional:
  --environment Deployment environment (default: production)
  --install-path  Installation path (default: /opt/anvil)`,
	Example: `  anvil runtime provision --name production-server
  anvil runtime provision --name staging-server --environment staging
  anvil runtime provision --name dev-server --install-path /opt/anvil-dev`,
	RunE: runProvision,
}

func init() {
	runtimeCmd.AddCommand(provisionCmd)

	provisionCmd.Flags().String("name", "", "Runtime name (required)")
	provisionCmd.Flags().String("environment", "production", "Environment type (development, staging, production)")
	provisionCmd.Flags().String("install-path", runtime.DefaultInstallRoot, "Runtime installation path")

	_ = provisionCmd.MarkFlagRequired("name")
}

// runProvision executes the Runtime provisioning command.
//
// It validates flags, provisions the Runtime, and displays the result
// including the unique Runtime ID, name, environment, and install path.
func runProvision(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	envStr, _ := cmd.Flags().GetString("environment")
	installPath, _ := cmd.Flags().GetString("install-path")

	env := runtime.EnvironmentType(envStr)

	cfg := runtime.DefaultRuntimeConfig()
	provisioner := runtime.NewProvisioner(cfg)

	result, err := provisioner.Provision(name, env, installPath)
	if err != nil {
		return fmt.Errorf("provision runtime: %w", err)
	}

	PrintSuccess(cmd, "Runtime provisioned successfully:")
	fmt.Fprintf(cmd.OutOrStdout(), "  ID:          %s\n", result.Metadata.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Name:        %s\n", result.Metadata.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "  Environment: %s\n", result.Metadata.Environment)
	fmt.Fprintf(cmd.OutOrStdout(), "  Path:        %s\n", result.Metadata.InstallPath)
	fmt.Fprintf(cmd.OutOrStdout(), "  Status:      %s\n", result.Metadata.Status)

	return nil
}
