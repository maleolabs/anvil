// Package cmd implements the Anvil CLI commands.
//
// ── Targeted Component Inspection (ST-P9-04) ───────────────────────────
//
// "anvil system inspect <component>" inspects a single platform component
// independently — environment, runtime, configuration, releases, or
// dependencies — enabling targeted troubleshooting without running a full
// system verification.
//
// Component-to-engine mapping (ST-009-004, TS-009-05/06/07):
//
//   - environment → RuntimeInspector.InspectActiveSymlink + InspectReleaseDirectories:
//     Active Release (symlink target), symlink correctness, directory
//     structure integrity.
//   - runtime → RuntimeInspector.InspectActiveSymlink + InspectSharedResources
//   - InspectRuntimeConfig: Active Release, shared resource status, and
//     operational status (runtime config presence). The RuntimeInspector
//     does not separate environment from runtime, so each subcommand uses
//     the subset of checks that matches its scope.
//   - config → ConfigInspector.Inspect: completeness (all required keys
//     present), validity (values conform to the schema), and resolution
//     (cross-level conflicts) with specific keys, values, and sources.
//     Configuration inspection operates on the project configuration
//     discovered from the current directory — the --server-root flag does
//     not apply to this component.
//   - release → ReleaseInspector (release directory, artifact presence,
//     shared links): release infrastructure. Per-release lifecycle stage
//     and history are already exposed by "anvil server release history"
//     (ST-P4-10) and "anvil server release active" (ST-P4-11), which
//     require Release identity — targeted component inspection reports
//     the release infrastructure condition instead.
//   - deps → ReleaseInspector.InspectExternalTools: availability of
//     required external tools (php, node, composer, npm, git) with
//     installation guidance for missing tools.
//
// All inspections are read-only — no platform state or configuration is
// modified. When a component does not exist (e.g. no project configured),
// the inspection reports that the component is not available.
//
// Exit codes (all subcommands):
//
//	0 - Component inspection passed (or component not available)
//	1 - Component inspection found failed check(s)
//
// Reference: ST-P9-04, ST-009-004, ADR-010 §6.8
package cmd

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/inspection"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

// systemInspectCmd represents the "anvil system inspect" parent command
// group for targeted component inspection.
//
// Reference: ST-P9-04
var systemInspectCmd = &cobra.Command{
	Use:   "inspect",
	Short: "Inspect a specific platform component",
	Long: `Inspect a single platform component in detail.

Targeted inspection reports the condition of one component with
diagnostic detail, enabling troubleshooting without running a full
system verification.

Components:
  environment   Active Release, symlink status, directory structure
  runtime       Active Release, shared resources, operational status
  config        Configuration completeness, validity, resolution
  release       Lifecycle stage and history for a Release (by identity)
                plus release infrastructure
  deps          Required external tool availability

All inspections are read-only and never modify platform state.

Examples:
  anvil system inspect runtime
  anvil system inspect config
  anvil system inspect release my-project abc123def456
  anvil system inspect deps --server-root /etc/anvil`,
}

// inspectJSONOutput is the machine-readable output shape for targeted
// inspection commands, wrapped in the standard OutputEnvelope (TS-P8-05).
type inspectJSONOutput struct {
	Component string                       `json:"component"`
	Available bool                         `json:"available"`
	Passed    bool                         `json:"passed,omitempty"`
	Checks    []inspection.InspectionCheck `json:"checks,omitempty"`
	Message   string                       `json:"message,omitempty"`
}

// inspectReleaseJSONOutput is the machine-readable output shape for the
// release inspection: the component checks plus the lifecycle stage and
// transition history of the inspected Release (EPIC-004 state).
type inspectReleaseJSONOutput struct {
	Component   string                       `json:"component"`
	Available   bool                         `json:"available"`
	Passed      bool                         `json:"passed,omitempty"`
	Checks      []inspection.InspectionCheck `json:"checks,omitempty"`
	ReleaseID   string                       `json:"release_id"`
	Stage       string                       `json:"stage"`
	Transitions []release.TransitionRecord   `json:"transitions,omitempty"`
}

// inspectEnvironmentCmd represents the "anvil system inspect environment"
// command that reports the Active Release, symlink status, and directory
// structure integrity.
//
// Reference: ST-P9-04, ST-009-004 §3
var inspectEnvironmentCmd = &cobra.Command{
	Use:   "environment",
	Short: "Inspect the Runtime environment (symlinks, directories)",
	Long: `Inspect the Server Runtime environment: the Active Release,
symlink status, and directory structure integrity.

The Active Release is reported through the active symlink target; the
symlink check reports correctness, and the directory check reports the
release directory structure integrity.

This command is read-only and does not modify any state.

Examples:
  anvil system inspect environment
  anvil system inspect environment --server-root /etc/anvil
  anvil system inspect environment --json`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runInspectEnvironment,
}

// inspectRuntimeCmd represents the "anvil system inspect runtime" command
// that reports the Active Release, shared resource status, and operational
// status.
//
// Reference: ST-P9-04, ST-009-004 §3
var inspectRuntimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Inspect the Runtime condition",
	Long: `Inspect the Server Runtime condition: the Active Release, shared
resource status, and operational status.

The Active Release is reported through the active symlink; the shared
resource check verifies all shared directories (config, storage, logs,
temp); the runtime config check verifies that the Runtime configuration
exists, which indicates the Runtime is operational.

This command is read-only and does not modify any state.

Examples:
  anvil system inspect runtime
  anvil system inspect runtime --server-root /etc/anvil
  anvil system inspect runtime --json`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runInspectRuntime,
}

// inspectConfigCmd represents the "anvil system inspect config" command
// that reports configuration completeness, validity, and resolution
// conflicts with specific keys, values, and sources.
//
// Reference: ST-P9-04, ST-009-004 §3
var inspectConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect configuration completeness, validity, resolution",
	Long: `Inspect the project configuration: completeness (all required keys
present), validity (values conform to the canonical schema), and
resolution (conflicts across hierarchy levels).

Issues are reported with specific keys, values, sources, and expected
formats. Configuration inspection operates on the project configuration
discovered from the current directory (anvil.yaml and ANVIL_CFG_*
environment variables); the --server-root flag does not apply.

When no project configuration exists, the component is reported as not
available.

This command is read-only and does not modify any state.

Examples:
  anvil system inspect config
  anvil system inspect config --json`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runInspectConfig,
}

// inspectReleaseCmd represents the "anvil system inspect release" command
// that reports the lifecycle stage and history for a specified Release,
// together with the release infrastructure condition (release directory,
// artifact presence, and shared links).
//
// Reference: ST-P9-04, ST-009-004 §3
var inspectReleaseCmd = &cobra.Command{
	Use:   "release <project-id> <release-id>",
	Short: "Inspect a Release: lifecycle stage, history, infrastructure",
	Long: `Inspect a Runtime Release: the lifecycle stage and transition
history for the specified Release, plus the release infrastructure
condition — release directory structure, artifact presence, and shared
link integrity.

The lifecycle stage and history are consumed from EPIC-004 release
state (read-only). Artifact presence is consumed from the Artifact
contract (EPIC-003) — this command never re-verifies artifacts.

This command is read-only and does not modify any state.

Examples:
  anvil system inspect release my-project abc123def456
  anvil system inspect release my-project --server-root /etc/anvil
  anvil system inspect release my-project abc123def456 --json`,
	Args:         ExactArgsWithUsage(2, "anvil system inspect release <project-id> <release-id>"),
	SilenceUsage: true,
	RunE:         runInspectRelease,
}

// inspectDepsCmd represents the "anvil system inspect deps" command that
// reports the availability of required external tools.
//
// Reference: ST-P9-04, ST-009-004 §3
var inspectDepsCmd = &cobra.Command{
	Use:   "deps",
	Short: "Inspect required external tool availability",
	Long: `Inspect the availability of required external tools and commands
(php, node, composer, npm, git) on the system PATH.

Each tool is reported with its location when found, or as missing with
installation guidance when not found.

This command is read-only and does not modify any state.

Examples:
  anvil system inspect deps
  anvil system inspect deps --server-root /etc/anvil
  anvil system inspect deps --json`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runInspectDeps,
}

func init() {
	systemCmd.AddCommand(systemInspectCmd)
	systemInspectCmd.AddCommand(
		inspectEnvironmentCmd,
		inspectRuntimeCmd,
		inspectConfigCmd,
		inspectReleaseCmd,
		inspectDepsCmd,
	)

	for _, cmd := range []*cobra.Command{
		inspectEnvironmentCmd,
		inspectRuntimeCmd,
		inspectReleaseCmd,
		inspectDepsCmd,
	} {
		cmd.Flags().String(
			"server-root",
			"",
			"override the server root directory (default: ANVIL_SERVER_ROOT or /etc/anvil)",
		)
	}

	for _, cmd := range []*cobra.Command{
		inspectEnvironmentCmd,
		inspectRuntimeCmd,
		inspectConfigCmd,
		inspectReleaseCmd,
		inspectDepsCmd,
	} {
		cmd.Flags().Bool(
			"json",
			false,
			"output result as JSON",
		)
	}
}

// runInspectEnvironment executes the environment inspection.
//
// Reference: ST-P9-04
func runInspectEnvironment(cmd *cobra.Command, args []string) error {
	inspector := inspection.NewRuntimeInspector(inspectRuntimeConfig(cmd))

	result := inspection.NewInspectionResult("environment")
	for _, check := range []inspection.InspectionCheck{
		inspector.InspectActiveSymlink(),
		inspector.InspectReleaseDirectories(),
	} {
		result.AddCheck(check.Name, check.Passed, check.Details)
	}

	return renderInspection(cmd, *result)
}

// runInspectRuntime executes the runtime inspection.
//
// Reference: ST-P9-04
func runInspectRuntime(cmd *cobra.Command, args []string) error {
	inspector := inspection.NewRuntimeInspector(inspectRuntimeConfig(cmd))

	result := inspection.NewInspectionResult("runtime")
	for _, check := range []inspection.InspectionCheck{
		inspector.InspectActiveSymlink(),
		inspector.InspectSharedResources(),
		inspector.InspectRuntimeConfig(),
	} {
		result.AddCheck(check.Name, check.Passed, check.Details)
	}

	return renderInspection(cmd, *result)
}

// runInspectConfig executes the configuration inspection.
//
// When no project configuration exists, the component is reported as not
// available (exit code 0) instead of failing. When configuration exists
// but cannot be loaded, the component is reported with a failing
// config_load check.
//
// Reference: ST-P9-04
func runInspectConfig(cmd *cobra.Command, args []string) error {
	resolver, err := loadConfigResolver()
	if err != nil {
		if !hasConfigSources() {
			return renderInspectionUnavailable(cmd, "config",
				"no project configuration found (anvil.yaml or ANVIL_CFG_* environment variables); run 'anvil init' to create a project")
		}
		return renderInspection(cmd, failingConfigComponent(err))
	}

	result := inspection.NewDefaultConfigInspector().Inspect(resolver)
	return renderInspection(cmd, result)
}

// runInspectRelease executes the release inspection: the lifecycle stage
// and transition history for the Release identified by project and release
// identity, plus the release infrastructure condition (release directory,
// artifact presence).
//
// The stage and history checks are informational — they report the
// recorded EPIC-004 state and never fail. Infrastructure checks may fail
// and produce a non-zero exit.
//
// Reference: ST-P9-04, ST-009-004 AC4
func runInspectRelease(cmd *cobra.Command, args []string) error {
	projectID := args[0]
	releaseID := args[1]

	serverRoot := resolveServerRoot(cmd)

	// Load the project registry to resolve the install root (ADR-013).
	registryStore := server.NewRegistryStore(serverRoot)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not load project registry: %v", err)
	}
	installRoot := reg.Project.InstallRoot

	// Load the Runtime Release by identity (EPIC-004 state). A missing
	// Release is a runtime not-found (exit 3, TS-019-03-02 F-02).
	rel, err := release.LookupByID(installRoot, release.ReleaseID(releaseID))
	if err != nil {
		if errors.Is(err, release.ErrReleaseNotFound) {
			return reportReleaseNotFoundError(cmd, projectID, releaseID, err)
		}
		return ReportPlainErrorf(cmd, err, "Release %q not found: %v", releaseID, err)
	}

	// Infrastructure checks (existing behavior).
	inspector := inspection.NewReleaseInspector(inspectRuntimeConfig(cmd))
	result := inspection.NewInspectionResult("release")
	checks := []inspection.InspectionCheck{
		inspector.InspectReleaseDirectory(),
		inspector.InspectArtifactPresence(),
	}
	for _, check := range checks {
		result.AddCheck(check.Name, check.Passed, check.Details)
	}

	// Lifecycle stage + history checks (EPIC-004 state, informational).
	history := rel.History()
	result.AddCheck("release_stage", true,
		fmt.Sprintf("release %s is in stage %s", rel.ID, rel.Stage))
	if len(history) == 0 {
		result.AddCheck("release_history", true, "no transitions recorded")
	} else {
		result.AddCheck("release_history", true,
			fmt.Sprintf("%d transition(s) recorded", len(history)))
	}

	// Machine-readable output: include stage and history explicitly.
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		out := inspectReleaseJSONOutput{
			Component:   result.Component,
			Available:   true,
			Passed:      result.Passed,
			Checks:      result.Checks,
			ReleaseID:   rel.ID.String(),
			Stage:       rel.Stage.String(),
			Transitions: history,
		}
		return output.WriteJSON(cmd.OutOrStdout(), out)
	}

	// Human-readable output.
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s Inspection: %s\n", componentTitle(result.Component), rel.ID)
	fmt.Fprintln(w)
	renderChecks(w, *result)

	// Transition history listing.
	if len(history) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Transitions:")
		for i, tr := range history {
			fmt.Fprintf(w, "  %d. %s  %s \u2192 %s  %s\n",
				i+1, tr.Timestamp, tr.From, tr.To, tr.Outcome)
		}
	}

	failedCount := failedChecks(*result)
	fmt.Fprintln(w)
	if failedCount == 0 {
		fmt.Fprintf(w, "%s component: all checks passed\n", componentTitle(result.Component))
		return nil
	}
	fmt.Fprintf(w, "%s component: %d check(s) failed\n", componentTitle(result.Component), failedCount)

	appErr := &output.AppError{
		Message:    fmt.Sprintf("%s inspection found %d failed check(s)", result.Component, failedCount),
		Reason:     "The component condition does not meet the required standard",
		Resolution: "Review the failed checks above and resolve each one.",
	}
	output.WriteAppError(cmd.ErrOrStderr(), appErr)
	return appErr
}

// runInspectDeps executes the external tool availability inspection.
//
// Tool availability checks are informational in the inspection engine
// (they always pass and report location or absence in the details). For
// missing tools the command adds installation guidance.
//
// Reference: ST-P9-04
func runInspectDeps(cmd *cobra.Command, args []string) error {
	inspector := inspection.NewReleaseInspector(inspectRuntimeConfig(cmd))

	result := inspection.NewInspectionResult("deps")
	var missing []string
	for _, check := range inspector.InspectExternalTools() {
		result.AddCheck(check.Name, check.Passed, check.Details)
		if isToolMissing(check) {
			missing = append(missing, strings.TrimPrefix(check.Name, "tool_"))
		}
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		return writeInspectionJSON(cmd, *result)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "Dependency Inspection")
	fmt.Fprintln(w)
	renderChecks(w, *result)
	for _, check := range result.Checks {
		if isToolMissing(check) {
			fmt.Fprintf(w, "  Install %s or ensure it is available in PATH\n", strings.TrimPrefix(check.Name, "tool_"))
		}
	}
	fmt.Fprintln(w)
	if len(missing) > 0 {
		fmt.Fprintf(w, "Missing tools: %s\n", strings.Join(missing, ", "))
	} else {
		fmt.Fprintln(w, "All required external tools are available.")
	}

	return nil
}

// isToolMissing reports whether a tool availability check indicates the
// tool is absent. The inspector's informational checks use the stable
// detail format "<tool> not found in PATH" for missing tools.
func isToolMissing(check inspection.InspectionCheck) bool {
	return strings.HasSuffix(check.Details, "not found in PATH")
}

// inspectRuntimeConfig builds the Runtime configuration from the
// --server-root flag (or environment/default), shared by the inspection
// subcommands that operate on the Runtime filesystem.
func inspectRuntimeConfig(cmd *cobra.Command) runtime.RuntimeConfig {
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = resolveServerRoot(cmd)
	return cfg
}

// componentTitle returns the display title for a component name.
func componentTitle(component string) string {
	if component == "" {
		return component
	}
	return strings.ToUpper(component[:1]) + component[1:]
}

// renderChecks writes each check with its status indicator and details.
// Shared by renderInspection and the component-specific renderers.
func renderChecks(w io.Writer, result inspection.InspectionResult) {
	for _, check := range result.Checks {
		status := output.StatusPass
		if !check.Passed {
			status = output.StatusFail
		}
		output.PrintStatus(w, status, check.Name)
		fmt.Fprintf(w, "  %s\n", check.Details)
	}
}

// renderInspection writes the human-readable or JSON inspection output and
// returns the command error for failed inspections.
func renderInspection(cmd *cobra.Command, result inspection.InspectionResult) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		return writeInspectionJSON(cmd, result)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s Inspection\n", componentTitle(result.Component))
	fmt.Fprintln(w)
	renderChecks(w, result)

	failedCount := failedChecks(result)
	fmt.Fprintln(w)
	if failedCount == 0 {
		fmt.Fprintf(w, "%s component: all checks passed\n", componentTitle(result.Component))
		return nil
	}
	fmt.Fprintf(w, "%s component: %d check(s) failed\n", componentTitle(result.Component), failedCount)

	appErr := &output.AppError{
		Message:    fmt.Sprintf("%s inspection found %d failed check(s)", result.Component, failedCount),
		Reason:     "The component condition does not meet the required standard",
		Resolution: "Review the failed checks above and resolve each one.",
	}
	output.WriteAppError(cmd.ErrOrStderr(), appErr)
	return appErr
}

// renderInspectionUnavailable reports that the target component does not
// exist. This is a successful outcome (exit 0) — the inspection ran and
// determined the component is not available.
func renderInspectionUnavailable(cmd *cobra.Command, component, message string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		out := inspectJSONOutput{
			Component: component,
			Available: false,
			Message:   message,
		}
		return output.WriteJSON(cmd.OutOrStdout(), out)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s component not available: %s\n", componentTitle(component), message)
	return nil
}

// writeInspectionJSON writes the inspection result as a machine-readable
// envelope.
func writeInspectionJSON(cmd *cobra.Command, result inspection.InspectionResult) error {
	out := inspectJSONOutput{
		Component: result.Component,
		Available: true,
		Passed:    result.Passed,
		Checks:    result.Checks,
	}
	return output.WriteJSON(cmd.OutOrStdout(), out)
}

// failedChecks counts the failed checks in an inspection result.
func failedChecks(result inspection.InspectionResult) int {
	count := 0
	for _, check := range result.Checks {
		if !check.Passed {
			count++
		}
	}
	return count
}
