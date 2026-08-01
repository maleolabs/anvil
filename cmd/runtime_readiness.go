package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/runtime"
)

// readinessCmd represents the "anvil runtime readiness" command that
// checks whether the Runtime environment is ready to accept releases.
//
// Unlike status.go, this command does NOT require a project context —
// it operates on a default Runtime configuration and checks local
// filesystem readiness.
//
// Reference: ST-P5-02
var readinessCmd = &cobra.Command{
	Use:   "readiness",
	Short: "Check Runtime environment readiness",
	Long: `Check whether the Anvil Runtime environment is ready.

This command uses default Runtime configuration to verify that all
required directories exist and that configuration values are valid.
It does not require an Anvil project context.`,
	Example: `  anvil runtime readiness
  anvil runtime readiness --install-root /opt/anvil`,
	SilenceUsage: true,
	RunE:         runReadiness,
}

func init() {
	runtimeCmd.AddCommand(readinessCmd)

	// Allow overriding the install root for testing purposes.
	readinessCmd.Flags().String(
		"install-root",
		runtime.DefaultInstallRoot,
		"override the default Runtime install root directory",
	)
}

// runReadiness executes the readiness check.
//
// It displays all individual checks (both passing and failing) and then
// reports whether the Runtime is ready. Exit code is 0 when ready, 1
// when not ready.
func runReadiness(cmd *cobra.Command, args []string) error {
	cfg := runtime.DefaultRuntimeConfig()

	if installRoot, _ := cmd.Flags().GetString("install-root"); installRoot != "" {
		cfg.InstallRoot = installRoot
	}

	checker := runtime.NewReadinessChecker(cfg)
	result := checker.Check()

	// Display all checks.
	for _, check := range result.Checks {
		if check.Passed {
			output.PrintStatus(cmd.OutOrStdout(), output.StatusPass, check.Name)
		} else {
			output.PrintStatus(cmd.OutOrStdout(), output.StatusFail, check.Name)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", check.Details)
	}

	// Summary.
	fmt.Fprintln(cmd.OutOrStdout(), "")
	if result.Ready {
		PrintSuccess(cmd, "Runtime is ready to accept releases.")
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Runtime is not ready:")
	for _, check := range result.Checks {
		if !check.Passed {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s: %s\n", check.Name, check.Details)
		}
	}
	return fmt.Errorf("runtime is not ready")
}
