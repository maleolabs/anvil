// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P1-01, ADR-010, ADR-012, TS-P8-01
package cmd

import (
	"testing"
)

// TestRootCommand_Initialization verifies that the root command is
// properly initialized with the correct Use, Short, and Long fields.
//
// AC: CLI application initializes with a root command.
func TestRootCommand_Initialization(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd must not be nil")
	}

	if rootCmd.Use != "anvil" {
		t.Errorf("rootCmd.Use = %q, want %q", rootCmd.Use, "anvil")
	}

	if rootCmd.Short == "" {
		t.Error("rootCmd.Short must not be empty")
	}

	if rootCmd.Long == "" {
		t.Error("rootCmd.Long must not be empty")
	}
}

// TestRootCommand_Execute verifies that the Execute function is exported
// correctly and delegates to cobra.Command.Execute.
//
// AC: CLI application can be executed (returns "anvil" help when no args).
func TestRootCommand_Execute(t *testing.T) {
	// Execute with --help should return nil (help is not an error).
	_, _, _, err := executeCommand("--help")
	if err != nil {
		t.Errorf("executeCommand('--help') returned unexpected error: %v", err)
	}

	// Execute with no args should return nil (root help is displayed).
	_, _, _, err = executeCommand()
	if err != nil {
		t.Errorf("executeCommand() with no args returned unexpected error: %v", err)
	}
}

// TestRootCommand_HasSubcommands verifies that all expected command groups
// and standalone commands are registered under the root command.
//
// AC: Commands can be registered through the command registration mechanism.
// AC: Adapter commands from EPIC-007 can be registered (extension point).
func TestRootCommand_HasSubcommands(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd must not be nil")
	}

	expected := []string{
		"init",
		"status",
		"project",
		"release",
		"artifact",
		"runtime",
		"pipeline",
		"config",
		"server",
		"deployment", // ST-P10-01, ST-P10-02, EPIC-010
		"adapter",    // EPIC-007 extension point (stub)
		"system",     // system operations
		"help",       // additional help / cobra built-in
	}

	registered := make(map[string]bool)
	for _, sub := range rootCmd.Commands() {
		// Use Name() to extract the bare command name (first word of Use).
		registered[sub.Name()] = true
	}

	for _, name := range expected {
		if !registered[name] {
			t.Errorf("expected subcommand %q to be registered under root, but it was not found", name)
		}
	}
}
