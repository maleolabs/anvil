// Package cmd implements the Anvil CLI commands.
//
// ── Context Boundary Validation (TS-P8-08, ADR-015, ADR-013) ────────
//
// Anvil defines three execution contexts:
//
//  1. Development context  — repository-aware, requires anvil.yaml.
//     Commands: init, project, artifact, pipeline
//
//  2. Deployment context   — transports and orchestrates Artifacts through
//     published Runtime commands. Never reads Runtime Registry or
//     filesystem layout. (Not yet implemented.)
//
//  3. Server context       — runtime-aware, repository-independent.
//     Commands: server init, server project, server release, server config,
//     server status
//
// Server commands MUST NOT:
//   - Load or require anvil.yaml
//   - Discover projects from the current working directory
//   - Import or depend on the project discovery mechanism
//   - Require repository context for any operation
//
// Development commands MUST NOT:
//   - Read or modify Runtime Registry state
//   - Depend on Runtime filesystem layout
//
// Deployment commands (future) MUST NOT:
//   - Read Runtime internals (Registry, State, symlinks)
//   - Implement a second lifecycle
//   - Duplicate Runtime State
//
// Reference: ADR-010 §6 (Three CLI Contexts), ADR-013 §2, ADR-015 §4
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/server"
)

// RequireServerInitialized checks that the Server Runtime has been
// initialized at the given root path. If not, it prints a user-facing
// error with guidance to stderr and returns an error.
//
// Server commands should call this early in their RunE to ensure the
// Runtime is configured before attempting any operation.
//
// Returns:
//   - nil when the Runtime is initialized
//   - error when the Runtime has not been initialized
//
// Reference: TS-P8-08, ADR-013
func RequireServerInitialized(cmd *cobra.Command, rootPath string) error {
	configStore := server.NewConfigStore(rootPath)
	if configStore.Exists() {
		return nil
	}

	resolution := "Run 'anvil server init' to initialize the Server Runtime."
	if rootPath != server.DefaultConfigRoot {
		resolution = fmt.Sprintf("Run 'anvil server init --server-root %s' to initialize with a non-default config path.", rootPath)
	}

	return ReportError(cmd, &output.AppError{
		Message:    fmt.Sprintf("Server Runtime not initialized at %s", rootPath),
		Reason:     "The Server Runtime configuration directory does not exist at the expected location",
		Resolution: resolution,
		// Precondition error category (TS-P8-07, ADR-010 §8.1): the
		// initialized Server Runtime is a required prerequisite for every
		// command that operates on it.
		ExitCodeValue: output.ExitCodePrecondition,
	})
}

// ── Context Boundary Error Messages ──────────────────────────────────

// FmtCrossContextError returns an error message for when a command is
// used in the wrong execution context. The message explains which context
// the command belongs to and provides guidance.
//
// Parameters:
//   - command: the full command path (e.g., "anvil server release install")
//   - requiredContext: the context the command requires (e.g., "Server Runtime")
//   - hint: actionable guidance (e.g., "Run 'anvil server init' first")
//
// Reference: TS-P8-08 AC4
func FmtCrossContextError(command, requiredContext, hint string) string {
	return fmt.Sprintf("Error: %s requires %s context.\nResolution: %s.\n", command, requiredContext, hint)
}

// FmtRepoRequiredError returns an error message for when a development
// command requires a project repository context.
//
// Reference: TS-P8-08 AC1
func FmtRepoRequiredError(command string) string {
	return fmt.Sprintf("Error: %s requires an Anvil project context.\nResolution: Run the command from within an Anvil project directory, or use 'anvil init' to create one.\n", command)
}

// FmtServerOnlyError returns an error message for when a command is
// only available in the Server Runtime context.
//
// Reference: TS-P8-08 AC3
func FmtServerOnlyError(command string) string {
	return fmt.Sprintf("Error: %s is a Server Runtime command.\nReason: It does not operate on the current working directory or project repository.\nResolution: Use 'anvil server init' to set up a Server Runtime.\n", command)
}
