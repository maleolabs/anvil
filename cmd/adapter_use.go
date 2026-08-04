// Package cmd implements the Anvil CLI commands.
//
// ── Adapter Use (TS-007-033) ──────────────────────────────────────────
//
// "anvil adapter use <name>" sets the project's active framework in
// anvil.yaml (project.framework) with validation:
//
//   - anvil.yaml must exist — the command requires a project context;
//   - an adapter that is not installed and probe-validated on this
//     system is rejected before any change, listing the discovered
//     adapters (PATH-based discovery, TS-007-039);
//   - when the framework is already set to the requested adapter, the
//     command reports "Adapter <name> is already active" and succeeds
//     without writing anything;
//   - when the framework is already set to a different adapter, the
//     command rejects with "Adapter <other> is already configured. Use
//     --force to override." unless --force is given;
//   - after setting the framework, the build pipeline template is
//     generated when missing (non-destructive, TS-P7-28 AC-1).
//
// The anvil.yaml update follows the map-based pattern of
// cmd/project_version.go (bumpVersion): the file is parsed as a generic
// map, project.framework is mutated in place, and the document is written
// back — preserving all custom fields. config.WriteConfig is never used
// here because it writes a minimal config and would clobber user fields.
// Note that project.framework is not part of the canonical config schema;
// project.Load silently ignores unknown keys, so the written value does
// not break config validation.
//
// Reference: TS-007-033, TS-P7-33
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
}

func init() {
	adapterUseCmd.Flags().Bool("force", false, "Override an already configured framework")
}

// runAdapterUse executes the use command.
//
// Reference: TS-007-033
func runAdapterUse(cmd *cobra.Command, args []string) error {
	name := args[0]
	force, _ := cmd.Flags().GetBool("force")

	// The adapter must be installed and probe-validated on this system:
	// PATH-based discovery (TS-007-039) replaces the closed known-set
	// gate, so a name without a working adapter binary is unknown even
	// when it is in engine.KnownFrameworks() — the known list is only a
	// display fallback for the error hint (AC-5).
	adapters, err := resolveAdapterSet(cmd.Context())
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not discover adapters: %v", err)
	}
	if _, ok := adapters[name]; !ok {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("unknown adapter %q", name),
			Reason:     discoveredAdapterHint(adapters),
			Resolution: "Run 'anvil adapter list' to see available adapters",
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
// as cmd/project_version.go bumpVersion). The project section is created
// when missing.
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
