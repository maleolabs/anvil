package cmd

import (
	"strings"
	"testing"
)

// ═══════════════════════════════════════════════════════════════════
// ST-P8-05: Exit Code Documentation
// ═══════════════════════════════════════════════════════════════════

// TestHelpExitCodesCommand_RegistersUnderHelp verifies that the
// exit-codes topic command is registered under the help command.
//
// AC: Exit code conventions are documented and accessible through the CLI.
func TestHelpExitCodesCommand_RegistersUnderHelp(t *testing.T) {
	helpSub, _, err := rootCmd.Find([]string{"help", "exit-codes"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"help\", \"exit-codes\"]) returned error: %v", err)
	}
	if helpSub == nil {
		t.Fatal("rootCmd.Find([\"help\", \"exit-codes\"]) returned nil command")
	}
	if helpSub.Use != "exit-codes" {
		t.Errorf("command Use = %q, want %q", helpSub.Use, "exit-codes")
	}
	if helpSub.Short == "" {
		t.Error("exit-codes command should have a Short description")
	}
	if helpSub.Long == "" {
		t.Error("exit-codes command should have a Long description")
	}
}

// TestHelpExitCodesCommand_Output verifies that "anvil help exit-codes"
// prints all five exit code conventions.
//
// AC: Exit code conventions are documented and accessible through the CLI.
func TestHelpExitCodesCommand_Output(t *testing.T) {
	_, stdout, stderr, err := executeCommand("help", "exit-codes")
	if err != nil {
		t.Fatalf("executeCommand('help', 'exit-codes') returned unexpected error: %v", err)
	}
	if stderr != "" {
		t.Errorf("executeCommand('help', 'exit-codes') produced unexpected stderr: %q", stderr)
	}

	expected := []string{
		"0 - Success",
		"1 - General error",
		"2 - Configuration error",
		"3 - Runtime error",
		"4 - Precondition error",
		"Exit code conventions",
	}
	for _, want := range expected {
		if !strings.Contains(stdout, want) {
			t.Errorf("exit-codes output should contain %q, got:\n%s", want, stdout)
		}
	}
}

// TestHelpExitCodesCommand_ArgsRejected verifies that the exit-codes
// topic command rejects positional arguments.
//
// AC: Commands reject unexpected arguments with an error.
func TestHelpExitCodesCommand_ArgsRejected(t *testing.T) {
	_, _, _, err := executeCommand("help", "exit-codes", "extra")
	if err == nil {
		t.Error("executeCommand('help', 'exit-codes', 'extra') should return an error")
	}
}

// TestTopLevelHelp_ExitCodesSection verifies that the root help output
// includes the exit codes summary section.
//
// AC: Exit code conventions are documented and accessible through the CLI.
func TestTopLevelHelp_ExitCodesSection(t *testing.T) {
	_, stdout, _, err := executeCommand()
	if err != nil {
		t.Fatalf("executeCommand() returned unexpected error: %v", err)
	}

	if !strings.Contains(stdout, "Exit Codes:") {
		t.Errorf("root help should contain 'Exit Codes:' section, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "0 - Success") {
		t.Errorf("root help should contain exit code 0 convention, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "4 - Precondition error") {
		t.Errorf("root help should contain exit code 4 convention, got:\n%s", stdout)
	}
}
