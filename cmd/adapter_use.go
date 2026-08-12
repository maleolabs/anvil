// Package cmd implements the Anvil CLI commands.
//
// ── Adapter Use (TS-007-033, TS-017-02-02) ────────────────────────────
//
// "anvil adapter use <name>" sets the project's active framework in
// anvil.yaml (project.framework) with validation:
//
//   - anvil.yaml must exist — the command requires a project context;
//   - post-gate (TS-017-02-02, ADR-028 §3, §7), an adapter is valid
//     when its standard (anvil-standard-<name>) is RECORDED — adopted
//     through the registry with the ADR-022 trust validation — and its
//     executable answers the capabilities probe through the executable
//     resolution contract (anvil-adapter-<name> on PATH, ADR-025
//     decision 4). The closed-set binary scan is removed; a bare binary
//     that was never adopted through the registry is rejected before
//     any change;
//   - when the framework is already set to the requested adapter, the
//     command reports "Adapter <name> is already active" and succeeds
//     without writing anything;
//   - when the framework is already set to a different adapter, the
//     command rejects with "Adapter <other> is already configured. Use
//     --force to override." unless --force is given;
//   - after setting the framework, the build pipeline template is
//     generated when missing (non-destructive, TS-P7-28 AC-1).
//
// The anvil.yaml update follows the map-based pattern: the file is
// parsed as a generic map, project.framework is mutated in place, and
// the document is written back — preserving all custom fields. (The
// same pattern was used by the removed "anvil project version"
// commands, TS-019-04-02.) config.WriteConfig is never used
// here because it writes a minimal config and would clobber user fields.
// Note that project.framework is not part of the canonical config schema;
// project.Load silently ignores unknown keys, so the written value does
// not break config validation.
//
// Reference: TS-007-033, TS-P7-33, TS-017-02-02
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"maleolabs.com/anvil/internal/engine"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/project"
)

// adapterUseCmd represents the "anvil adapter use" command that sets the
// active framework in anvil.yaml.
//
// Reference: TS-007-033
var adapterUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Set the active framework for the project",
	Long: `Set the active framework for the Anvil project.

The framework is recorded as project.framework in anvil.yaml and drives
pipeline template generation: the build pipeline template is generated
when .anvil/pipelines/build.yaml does not exist (existing pipeline files
are never overwritten).

When a framework is already configured, the command reports it:
  - same adapter:   "Adapter <name> is already active"
  - other adapter:  "Adapter <other> is already configured. Use --force
    to override."

Examples:
  anvil adapter use laravel
  anvil adapter use flutter --force`,
	Args:         ExactArgsWithUsage(1, "anvil adapter use laravel", "name"),
	SilenceUsage: true,
	RunE:         runAdapterUse,
	Deprecated:   adapterUseDeprecationNotice,
}

func init() {
	adapterUseCmd.Flags().Bool("force", false, "Override an already configured framework")
}

// runAdapterUse executes the use command.
//
// Reference: TS-007-033, TS-017-02-02
func runAdapterUse(cmd *cobra.Command, args []string) error {
	name := args[0]
	force, _ := cmd.Flags().GetBool("force")

	// Post-gate, an adapter is valid when its standard is RECORDED
	// (adopted through the registry with the ADR-022 trust validation)
	// and its executable answers the capabilities probe through the
	// executable resolution contract. The closed-set binary scan is
	// removed (TS-017-02-02); the Core carries no known-framework
	// catalog (ADR-026) — the hint resolves installed delivery lifecycle
	// standards through the registry client. A store that cannot be
	// read is a distinct error, never silently "unknown adapter" (team
	// review F5).
	installed, err := installedAdapterVersions()
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not read the installed-standard store",
			Reason:     err.Error(),
			Resolution: "Fix or remove the corrupt record file(s) in the installed-standard store, or re-adopt the standard with 'anvil adapter install <name> --force' (registry-based, trust-validated)",
			Err:        err,
		})
	}
	if _, ok := installed[name]; !ok {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("unknown adapter %q", name),
			Reason:     adapterResolutionHint(),
			Resolution: "Run 'anvil adapter list' to see the installed adapters (registry records), or 'anvil adapter list --available' to see the adapters offered in the registry index",
		})
	}

	// The executable resolves through the executable resolution
	// contract (anvil-adapter-<name> on PATH, ADR-025 decision 4): the
	// standard is recorded, but the binary may not be installed — the
	// error names the adoption path.
	executable, err := resolveAdapterExecutable(name)
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("adapter %q is installed but its binary is not resolvable", name),
			Reason:     fmt.Sprintf("the standard %q is installed, but the adapter executable %q was not found on PATH", adapterStandardIDForName(name), "anvil-adapter-"+name),
			Resolution: "Run 'anvil adapter install <name>' to install the adapter binary from the standard's release (registry-based, trust-validated)",
			Err:        err,
		})
	}
	if _, err := invokeAdapterCapabilities(cmd.Context(), name, executable); err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("adapter %q is not usable", name),
			Reason:     "the adapter executable does not answer the capabilities command (broken or foreign binary)",
			Resolution: "Run 'anvil adapter install <name> --force' to reinstall the adapter binary from the standard's release (registry-based, trust-validated)",
			Err:        err,
		})
	}

	root, err := project.Discover()
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not set adapter: no Anvil project found",
			Reason:     "no anvil.yaml was found in the current directory or any parent directory",
			Resolution: "Run 'anvil init <name>' to create a project first",
			Err:        err,
		})
	}
	configPath := filepath.Join(root, project.ConfigFileName)

	current, err := readProjectFramework(configPath)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not read %s: %v", configPath, err)
	}

	// Validation matrix (TS-007-033 §7).
	if current == name {
		PrintSuccessf(cmd, "Adapter %s is already active.", name)
		return nil
	}
	if current != "" && !force {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("Adapter %s is already configured. Use --force to override", current),
			Reason:     fmt.Sprintf("project.framework in %s is set to %q", configPath, current),
			Resolution: fmt.Sprintf("Run 'anvil adapter use %s --force' to switch the active framework", name),
		})
	}

	if err := writeProjectFramework(configPath, name); err != nil {
		return ReportPlainErrorf(cmd, err, "could not set adapter %q: %v", name, err)
	}

	// Generate the build pipeline template when missing (non-destructive).
	if err := engine.GenerateFrameworkPipelineConfigs(root, name); err != nil {
		return ReportPlainErrorf(cmd, err, "could not generate pipeline template: %v", err)
	}

	if current == "" {
		PrintSuccessf(cmd, "Adapter %s is now active for this project.", name)
	} else {
		PrintSuccessf(cmd, "Adapter %s is now active for this project (overrode %s).", name, current)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Next steps:")
	fmt.Fprintln(cmd.OutOrStdout(), "  anvil config list")
	return nil
}

// readProjectFramework returns the current project.framework value from
// the project config file, or "" when the key or the project section is
// absent.
func readProjectFramework(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	proj, ok := doc["project"].(map[string]interface{})
	if !ok {
		return "", nil
	}
	framework, _ := proj["framework"].(string)
	return framework, nil
}

// writeProjectFramework updates project.framework in the project config
// file, preserving all other fields (map-based update — the same pattern
// the removed "anvil project version" commands used, TS-019-04-02). The
// project section is created when missing.
func writeProjectFramework(configPath, framework string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	proj, ok := doc["project"].(map[string]interface{})
	if !ok {
		proj = make(map[string]interface{})
		doc["project"] = proj
	}
	proj["framework"] = framework

	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, out, 0644)
}
