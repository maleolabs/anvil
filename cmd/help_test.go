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

// ═══════════════════════════════════════════════════════════════════
// BUG-008: Built-in Help Command
// ═══════════════════════════════════════════════════════════════════

// TestHelpCommand_DelegatesToCommandHelp verifies that "anvil help <command>"
// produces the same output as "<command> --help" for both leaf commands
// and command groups. The custom help group no longer shadows cobra's
// built-in help command, so arbitrary command lookups resolve to the
// requested command's help (BUG-008).
//
// AC: anvil help <command> output equals <command> --help output (BUG-008 DoD).
func TestHelpCommand_DelegatesToCommandHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string // arguments to "anvil help <args...>"
	}{
		{"leaf init", []string{"init"}},
		{"leaf status", []string{"status"}},
		{"group server", []string{"server"}},
		{"group pipeline", []string{"pipeline"}},
		{"nested pipeline build", []string{"pipeline", "build"}},
		{"nested server release", []string{"server", "release"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helpArgs := append([]string{"help"}, tt.args...)
			_, helpOut, helpErrOut, helpErr := executeCommand(helpArgs...)
			if helpErr != nil {
				t.Fatalf("executeCommand(%q) returned unexpected error: %v", helpArgs, helpErr)
			}
			if helpErrOut != "" {
				t.Errorf("executeCommand(%q) produced unexpected stderr: %q", helpArgs, helpErrOut)
			}

			flagArgs := append(tt.args, "--help")
			_, flagOut, flagErrOut, flagErr := executeCommand(flagArgs...)
			if flagErr != nil {
				t.Fatalf("executeCommand(%q) returned unexpected error: %v", flagArgs, flagErr)
			}
			if flagErrOut != "" {
				t.Errorf("executeCommand(%q) produced unexpected stderr: %q", flagArgs, flagErrOut)
			}

			if strings.TrimSpace(helpOut) != strings.TrimSpace(flagOut) {
				t.Errorf("anvil help %v output should match anvil %v output:\n--- help ---\n%s\n--- --help ---\n%s",
					tt.args, flagArgs, helpOut, flagOut)
			}
		})
	}
}

// TestHelpCommand_BareDelegatesToRootHelp verifies that bare "anvil help"
// shows the root domain help, identical to running "anvil" with no
// arguments (BUG-008).
func TestHelpCommand_BareDelegatesToRootHelp(t *testing.T) {
	_, helpOut, helpErrOut, helpErr := executeCommand("help")
	if helpErr != nil {
		t.Fatalf("executeCommand('help') returned unexpected error: %v", helpErr)
	}
	if helpErrOut != "" {
		t.Errorf("executeCommand('help') produced unexpected stderr: %q", helpErrOut)
	}

	_, rootOut, rootErrOut, rootErr := executeCommand()
	if rootErr != nil {
		t.Fatalf("executeCommand() returned unexpected error: %v", rootErr)
	}
	if rootErrOut != "" {
		t.Errorf("executeCommand() produced unexpected stderr: %q", rootErrOut)
	}

	if strings.TrimSpace(helpOut) != strings.TrimSpace(rootOut) {
		t.Errorf("anvil help output should match bare anvil output:\n--- help ---\n%s\n--- bare ---\n%s",
			helpOut, rootOut)
	}
}

// TestHelpCommand_HelpFlag verifies that "anvil help --help" shows the
// built-in help command's own help without errors (BUG-008).
func TestHelpCommand_HelpFlag(t *testing.T) {
	_, stdout, stderr, err := executeCommand("help", "--help")
	if err != nil {
		t.Fatalf("executeCommand('help', '--help') returned unexpected error: %v", err)
	}
	if stderr != "" {
		t.Errorf("executeCommand('help', '--help') produced unexpected stderr: %q", stderr)
	}
	if !strings.Contains(stdout, "Help provides help for any command") {
		t.Errorf("anvil help --help should show the built-in help command's help, got:\n%s", stdout)
	}
}

// TestHelpCommand_UnknownTopicIsReported verifies that requesting a
// non-existent help topic ("anvil help quickstart") is reported as an
// unknown topic instead of silently printing the help group's own Long
// text (BUG-008).
//
// Cobra prints the "Unknown help topic" notice via c.Printf, which
// writes through OutOrStderr — the informational-output stream, not the
// error stream. OutOrStderr resolves through the out-writer chain: the
// help command has no writer of its own, so in this test harness it
// inherits the root's stdout writer (root.SetOut in executeCommand) and
// the notice lands on the stdout buffer; in the real binary it falls
// back to os.Stderr. Accept either stream, and lock in the fail-closed
// behavior by requiring the removed custom group's Long text to never
// appear on stdout.
//
// AC: anvil help <dead topic> no longer falls back to the group Long
// text (BUG-008 Validation step 4).
func TestHelpCommand_UnknownTopicIsReported(t *testing.T) {
	_, stdout, stderr, err := executeCommand("help", "quickstart")
	if err != nil {
		t.Fatalf("executeCommand('help', 'quickstart') returned unexpected error: %v", err)
	}

	if !strings.Contains(stdout+stderr, "Unknown help topic") {
		t.Errorf("anvil help quickstart should report an unknown help topic, got stdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}

	// The old custom help group printed its own Long text on stdout for
	// unknown topics; that fallback must never return.
	if strings.Contains(stdout, "Access extended documentation") {
		t.Errorf("anvil help quickstart must not print the removed help group Long text to stdout (BUG-008), got:\n%s",
			stdout)
	}
}

// TestHelpGroup_DoesNotAdvertiseDeadTopics verifies that the help group
// no longer advertises documentation topics that do not exist
// ("quickstart", "configuration") in its Long text (BUG-008).
//
// AC: the help group's Long text lists only real topics (BUG-008 DoD).
func TestHelpGroup_DoesNotAdvertiseDeadTopics(t *testing.T) {
	help, _, err := rootCmd.Find([]string{"help"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"help\"]) returned error: %v", err)
	}
	if help == nil || help == rootCmd {
		t.Fatal("rootCmd.Find([\"help\"]) did not resolve to the built-in help command")
	}

	for _, ghost := range []string{"anvil help quickstart", "anvil help configuration"} {
		if strings.Contains(help.Long, ghost) {
			t.Errorf("help group Long text must not advertise non-existent topic %q (BUG-008), got:\n%s",
				ghost, help.Long)
		}
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
