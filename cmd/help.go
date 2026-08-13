// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010 §6.9, TS-P8-02
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// init registers cobra's built-in help command and attaches the
// extended documentation topics to it.
//
// The help group is cobra's built-in "help" command: "anvil help <command>"
// resolves arbitrary command lookups and prints the requested command's
// help, identical to "<command> --help". The extended documentation
// topics (e.g. "exit-codes") are attached as subcommands of the built-in
// command so "anvil help <topic>" keeps working.
//
// This file previously registered a custom command literally named
// "help". Cobra's findNext returns the first name match, so the custom
// command shadowed the built-in one and "anvil help <command>" printed
// the group's own Long text instead of the command's help (BUG-008).
//
// Init-order dependency: InitDefaultHelpCmd early-returns when the root
// command has no subcommands yet, so this init() relies on the lexical
// file order of this package — the other cmd/*.go init()s (adapter.go,
// artifact.go, config.go, ...) register root commands before help.go
// runs. The dependency is guarded by
// TestHelpExitCodesCommand_RegistersUnderHelp, which fails loudly if
// the topic is ever missing from the tree.
//
// Reference: ADR-010 §6.9
func init() {
	rootCmd.InitDefaultHelpCmd()

	help, _, err := rootCmd.Find([]string{"help"})
	if err == nil && help != nil && help != rootCmd {
		help.AddCommand(helpExitCodesCmd)
	}
}

// exitCodesSummary is the concise exit code overview printed in the
// root help. It summarises the general conventions and points to the
// full topic for details.
//
// Reference: ST-P8-05, ADR-010 §8.1
const exitCodesSummary = `Exit Codes:
  0 - Success — the command completed successfully
  1 - General error — an unspecified error occurred
  2 - Configuration error — project configuration is invalid or missing
  3 - Runtime error — the runtime environment is unavailable or misconfigured
  4 - Precondition error — a required prerequisite is missing

Run "anvil help exit-codes" for the complete conventions.`

// exitCodesDetail is the complete exit code conventions reference
// shown by "anvil help exit-codes".
//
// The general categories apply to every command. Non-zero exit codes
// are always accompanied by an error message on stderr. Codes are
// deterministic and stable across versions, so automation consumers
// can rely on them without parsing human-readable output.
//
// Reference: ST-P8-05, ADR-010 §8.1/§9.6
const exitCodesDetail = `Exit code conventions

Every Anvil command exits with a deterministic exit code. Zero
indicates success; non-zero codes identify specific failure categories.
The conventions apply consistently across all commands — the same
failure category always produces the same exit code.

General conventions:

  0 - Success — the command completed successfully
  1 - General error — an unspecified error occurred; the default for
      errors that do not fall into a more specific category. This
      includes network/transport failures, validation errors, and
      commands invoked outside a project context (the absence of a
      project context is a context error, not invalid configuration)
  2 - Configuration error — project configuration is invalid, missing,
      or conflicting (e.g. duplicate project ID, malformed config
      files, a different version of a standard already installed)
  3 - Runtime error — a runtime resource is unavailable or not found
      (e.g. project not found in the server registry, release not
      found, standard not found in the registry index). The scope of
      "not found" is repository/registry lookups — the deprecated
      v1.x adapter surface (ADR-032) emits 3 for index lookups during
      the dual-run window and is excluded from enforcement (D-09);
      network failures are general errors (1)
  4 - Precondition error — a required prerequisite is missing
      (e.g. the server runtime is not initialized, the delivery
      lifecycle standard for a declared framework is not installed,
      SSH credentials for deployment upload are not configured)

Carve-outs (TS-019-03-02):

  - Informational and status commands report absent resources without
    gating: "anvil server status", "anvil server doctor", "anvil
    server release active" (no active release), and "anvil adapter
    uninstall" (not installed) exit 0. Lookup commands (e.g. "anvil
    server project get", "anvil server release status") gate with 3.
  - "anvil server readiness" exits 0 whenever it runs successfully
    (findings never gate, ADR-036); input-resolution failures
    (unreadable registry/release/registration-index inputs) exit 1.

Behavioural guarantees:

  - A non-zero exit code is always accompanied by an error message
    on stderr.
  - Exit codes are stable across versions. Automation consumers may
    rely on the category codes without parsing output text.
  - New commands must use the established conventions.

Command-specific notes:

  - "anvil system inspect {environment, runtime, config, release,
    deps}" exits 0 when all checks pass — informational output
  - "anvil system inspect {environment, runtime, config, release}"
    exits 1 when a component inspection found failed check(s) —
    with --json the failed checks are reported per check in the
    envelope ("passed": false) and the process exits 0
  - "anvil system inspect release" exits 3 when the Release is not
    found — no Release with the given identity exists (runtime);
    the not-found lookup gates with 3 per D-03 (demoted
    diagnostics, ADR-036), while inspection findings themselves
    never gate lifecycle operations
  - "anvil system inspect deps" always exits 0 — missing external
    tools are reported as informational, never gated
  - "anvil skill list" exits 0 on success — including listings that
    surface stale entries or unreadable records (reported, never
    silently dropped); 1 when the embedded core set or a skill store
    (installed-skills / installed-standards) cannot be read
  - "anvil skill install <name>" exits 0 on success — including an
    idempotent re-install of the same version; 3 when the skill or
    its source standard is not found; 2 on a conflict or version
    conflict (install never changes versions — update is the
    explicit event); 4 when a precondition is missing (no selectable
    agent detected, repo scope without an Anvil project/git root);
    1 for other errors (invalid release, gate failure, digest
    mismatch, fetch/extract failure)
  - "anvil skill update <name>" exits 0 on success; 3 when the skill
    is not installed or its source is not found; 2 on a conflict; 4
    on a precondition; 1 for other errors (deprecated/retired
    source — the no-updates rule, gate failure, digest mismatch,
    fetch/extract failure)
  - "anvil skill uninstall <name>" exits 0 on success — including a
    graceful uninstall of a skill that is not installed (the desired
    end state already holds); 1 on errors (unreadable record,
    removal failure, containment rejection, filter matching only
    shared targets); 3 when the --agent/
    --scope filter matches no recorded target; 4 when the recorded
    scope base cannot be resolved (e.g. repo scope outside the
    project)

Command-specific notes are documented in the command help and in
docs/operations/exit-codes.md.`

// helpExitCodesCmd is the "anvil help exit-codes" topic command that
// documents the exit code conventions for all commands.
//
// Reference: ST-P8-05, ADR-010 §8.1/§9.6
var helpExitCodesCmd = &cobra.Command{
	Use:     "exit-codes",
	Short:   "Explain the exit code conventions for all commands",
	Long:    exitCodesDetail,
	Example: `  anvil help exit-codes`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), exitCodesDetail)
		return err
	},
}
