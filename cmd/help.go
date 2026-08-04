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
      errors that do not fall into a more specific category
  2 - Configuration error — project configuration is invalid, missing,
      or conflicting (e.g. duplicate project ID)
  3 - Runtime error — a runtime resource is unavailable or not found
      (e.g. project not found in the server registry)
  4 - Precondition error — a required prerequisite is missing
      (e.g. the server runtime is not initialized)

Behavioural guarantees:

  - A non-zero exit code is always accompanied by an error message
    on stderr.
  - Exit codes are stable across versions. Automation consumers may
    rely on the category codes without parsing output text.
  - New commands must use the established conventions.

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
