// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P9-05, ADR-010 §6.8
package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/inspection"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

// systemHealthCmd represents the "anvil system health" command that
// performs a comprehensive health check of the Server Runtime.
//
// It runs all system inspectors and produces a readiness assessment
// with actionable blocker descriptions when the system is not ready.
//
// Reference: ST-P9-05, ADR-010 §6.8
var systemHealthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check Server Runtime health and readiness",
	Long: `Perform a comprehensive health check of the Anvil Server Runtime.

This command runs all system inspectors and produces a readiness
assessment. If the system is not ready, actionable blocker descriptions
are listed.

Exit codes:
  0 - System is ready
  1 - System is not ready
  2 - CLI error (invalid flags, etc.)`,
	Example: `  anvil system health
  anvil system health --server-root /etc/anvil
  anvil system health --json`,
	SilenceUsage: true,
	RunE:         runSystemHealth,
}

func init() {
	systemCmd.AddCommand(systemHealthCmd)

	systemHealthCmd.Flags().String(
		"server-root",
		"",
		"override the server root directory (default: ANVIL_SERVER_ROOT or /etc/anvil)",
	)

	systemHealthCmd.Flags().Bool(
		"json",
		false,
		"output result as JSON",
	)
}

// runSystemHealth executes the system health check.
//
// It resolves the server root, creates all inspectors, runs the
// readiness coordinator, and formats the output. Exit code is 0 when
// ready, 1 when not ready.
//
// Reference: ST-P9-05
func runSystemHealth(cmd *cobra.Command, args []string) error {
	// Resolve server root.
	serverRoot := resolveHealthServerRoot(cmd)

	// Create runtime config from server root.
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = serverRoot

	// Create all inspectors.
	runtimeInspector := inspection.NewRuntimeInspector(cfg)
	releaseInspector := inspection.NewReleaseInspector(cfg)
	serverReadinessInspector := inspection.NewServerReadinessInspector(serverRoot)
	registryInspector := inspection.NewRegistryInspector(serverRoot)

	// Create verification engine.
	engine := inspection.NewVerificationEngine(
		runtimeInspector,
		nil, // config inspector not needed for health check
		releaseInspector,
		serverReadinessInspector,
		registryInspector,
	)

	// Create readiness coordinator.
	coordinator := inspection.NewReadinessCoordinator(engine)
	result := coordinator.CheckReadiness(serverRoot, nil)

	// Check for --json flag.
	jsonOutput, _ := cmd.Flags().GetBool("json")

	if jsonOutput {
		return outputJSON(cmd, result)
	}

	return outputTable(cmd, result)
}

// resolveHealthServerRoot determines the server root path from flags,
// environment variable, or default.
func resolveHealthServerRoot(cmd *cobra.Command) string {
	// Check flag first.
	if serverRoot, _ := cmd.Flags().GetString("server-root"); serverRoot != "" {
		return serverRoot
	}

	// Check environment variable.
	if root := os.Getenv(server.EnvServerRoot); root != "" {
		return root
	}

	// Use default.
	return server.DefaultConfigRoot
}

// outputJSON formats the result as JSON and writes it to stdout.
func outputJSON(cmd *cobra.Command, result inspection.ReadinessCoordinatorResult) error {
	// Build a structured JSON response using proper marshaling.
	response := struct {
		Ready      bool                          `json:"ready"`
		Summary    string                        `json:"summary"`
		Components []inspection.InspectionResult `json:"components"`
	}{
		Ready:      result.Ready,
		Summary:    result.Summary,
		Components: result.Components,
	}

	data, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), string(data))

	if !result.Ready {
		return fmt.Errorf("system is not ready")
	}
	return nil
}

// outputTable formats the result as a human-readable table and writes it
// to stdout.
func outputTable(cmd *cobra.Command, result inspection.ReadinessCoordinatorResult) error {
	w := cmd.OutOrStdout()

	// Header.
	if result.Ready {
		fmt.Fprintln(w, "System Health: READY")
	} else {
		fmt.Fprintln(w, "System Health: NOT READY")
	}
	fmt.Fprintln(w)

	// Component table.
	fmt.Fprintf(w, "%-25s %-8s %s\n", "Component", "Status", "Details")
	fmt.Fprintf(w, "%-25s %-8s %s\n", "─────────────────────────", "────────", "─────────────────────────")

	for _, comp := range result.Components {
		statusIcon := output.StatusPass
		if !comp.Passed {
			statusIcon = output.StatusFail
		}

		// Build details summary.
		details := componentDetails(comp)

		fmt.Fprintf(w, "%-25s [%s] %s\n", comp.Component, string(statusIcon), details)
	}

	// Blockers section (if not ready).
	if !result.Ready && len(result.Blockers) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Blockers (%d):\n", len(result.Blockers))
		for i, blocker := range result.Blockers {
			fmt.Fprintf(w, "  %d. %s\n", i+1, blocker)
		}
	}

	// Summary.
	fmt.Fprintln(w)
	fmt.Fprintln(w, result.Summary)

	if !result.Ready {
		return fmt.Errorf("system is not ready")
	}
	return nil
}

// componentDetails generates a brief details string for a component.
func componentDetails(comp inspection.InspectionResult) string {
	if comp.Passed {
		return "all checks passed"
	}

	failedCount := 0
	for _, c := range comp.Checks {
		if !c.Passed {
			failedCount++
		}
	}
	return fmt.Sprintf("%d check(s) failed", failedCount)
}
