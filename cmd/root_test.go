// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P1-01, ADR-010, ADR-012, ST-P8-01, ST-P8-02
package cmd

import (
	"strings"
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
		"artifact",
		"pipeline",
		"config",
		"server",
		"deployment", // ST-P10-01, ST-P10-02, EPIC-010
		"adapter",    // EPIC-007 adapter commands
		"system",     // system operations
		"update",     // self-update CLI binary
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

// ═══════════════════════════════════════════════════════════════════
// ST-P8-01: Top-Level Help Overview
// ═══════════════════════════════════════════════════════════════════

// TestTopLevelHelp_NoArgsDisplaysDomainGroups verifies that running
// "anvil" with no arguments displays commands organized by product domain.
//
// AC: Running anvil with no arguments displays a list of available command groups.
// AC: Each command group is displayed with a one-line description.
// AC: Command groups are organized by product domain.
// AC: Exit code is 0 when displaying help.
func TestTopLevelHelp_NoArgsDisplaysDomainGroups(t *testing.T) {
	_, stdout, stderr, err := executeCommand()

	if err != nil {
		t.Errorf("executeCommand() returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("executeCommand() produced unexpected stderr: %q", stderr)
	}

	// Must contain the "Product Domains" section header.
	if !strings.Contains(stdout, "Product Domains:") {
		t.Errorf("output should contain 'Product Domains:' section header, got:\n%s", stdout)
	}

	// Must contain expected domain group names.
	for _, group := range rootDomainGroups {
		if !strings.Contains(stdout, group.Name) {
			t.Errorf("output should contain domain group %q, got:\n%s", group.Name, stdout)
		}
	}

	// Must contain all registered command names with their Short descriptions.
	for _, group := range rootDomainGroups {
		for _, cmdName := range group.Commands {
			sub, _, err := rootCmd.Find([]string{cmdName})
			if err != nil {
				continue
			}
			if !strings.Contains(stdout, sub.Name()) || !strings.Contains(stdout, sub.Short) {
				t.Errorf("output should contain command %q with description %q, got:\n%s",
					sub.Name(), sub.Short, stdout)
			}
		}
	}

	// Must contain the help hint at the bottom.
	if !strings.Contains(stdout, `Use "anvil [command] --help"`) {
		t.Errorf("output should contain help hint, got:\n%s", stdout)
	}
}

// TestTopLevelHelp_HelpFlagDisplaysSameContent verifies that
// "anvil --help" produces the same domain-grouped output as
// running "anvil" with no arguments.
//
// AC: Running anvil --help displays the same output as running anvil with no args.
func TestTopLevelHelp_HelpFlagDisplaysSameContent(t *testing.T) {
	_, stdoutNoArgs, _, errNoArgs := executeCommand()
	if errNoArgs != nil {
		t.Fatalf("executeCommand() returned error: %v", errNoArgs)
	}

	_, stdoutHelp, _, errHelp := executeCommand("--help")
	if errHelp != nil {
		t.Fatalf("executeCommand('--help') returned error: %v", errHelp)
	}

	// Normalize trailing whitespace for comparison.
	if strings.TrimSpace(stdoutNoArgs) != strings.TrimSpace(stdoutHelp) {
		t.Errorf("output with no args and --help should match:\n--- no args ---\n%s\n--- --help ---\n%s",
			stdoutNoArgs, stdoutHelp)
	}
}

// TestTopLevelHelp_VersionFlag verifies that "anvil --version"
// displays the version string without help output.
//
// AC: Running anvil --version displays the version string without help output.
func TestTopLevelHelp_VersionFlag(t *testing.T) {
	_, stdout, stderr, err := executeCommand("--version")

	if err != nil {
		t.Errorf("executeCommand('--version') returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("executeCommand('--version') produced unexpected stderr: %q", stderr)
	}

	// Output should contain version, not help text.
	if !strings.Contains(stdout, CliVersion) {
		t.Errorf("version output should contain %q, got: %q", CliVersion, stdout)
	}

	// Should NOT contain domain groupings (that's help output).
	if strings.Contains(stdout, "Product Domains:") {
		t.Errorf("version output should not contain help text, got: %q", stdout)
	}
}

// TestTopLevelHelp_NoErrorOrWarning verifies that running "anvil"
// with no arguments produces no error or warning.
//
// AC: No error or warning is produced when running anvil with no arguments.
func TestTopLevelHelp_NoErrorOrWarning(t *testing.T) {
	_, _, stderr, err := executeCommand()

	if err != nil {
		t.Errorf("executeCommand() should not produce an error: %v", err)
	}

	if stderr != "" {
		t.Errorf("executeCommand() should not produce warnings or errors on stderr, got: %q", stderr)
	}
}

// TestTopLevelHelp_ExitCode verifies that displaying help produces
// exit code 0.
//
// AC: Exit code is 0 when displaying help.
func TestTopLevelHelp_ExitCode(t *testing.T) {
	// Execute with no args should return nil error (exit 0).
	_, _, _, err := executeCommand()
	if err != nil {
		t.Errorf("executeCommand() should return nil error (exit 0), got: %v", err)
	}

	// Execute with --help should also return nil error.
	_, _, _, err = executeCommand("--help")
	if err != nil {
		t.Errorf("executeCommand('--help') should return nil error (exit 0), got: %v", err)
	}

	// Execute with valid subcommand --help should return nil error.
	_, _, _, err = executeCommand("init", "--help")
	if err != nil {
		t.Errorf("executeCommand('init', '--help') should return nil error (exit 0), got: %v", err)
	}
}

// ═══════════════════════════════════════════════════════════════════
// ST-P8-02: Group-Level and Command-Level Help Consistency
// ═══════════════════════════════════════════════════════════════════

// TestCommandHelp_ShortDescriptions verifies that every command in
// the tree has a non-empty Short description.
//
// AC: Every command has a Short description explaining its purpose.
func TestCommandHelp_ShortDescriptions(t *testing.T) {
	for _, cmd := range collectAllCommands() {
		if cmd.Short == "" {
			t.Errorf("command %q has empty Short description", cmd.CommandPath())
		}
	}
}

// TestCommandHelp_LongDescriptions verifies that every command in
// the tree has a non-empty Long description (or at minimum matches Short).
//
// AC: Every command has a detailed Long description.
func TestCommandHelp_LongDescriptions(t *testing.T) {
	for _, cmd := range collectAllCommands() {
		if cmd.Long == "" {
			t.Errorf("command %q has empty Long description", cmd.CommandPath())
		}
	}
}

// TestCommandHelp_HelpExitCodeZero verifies that --help for any
// command returns exit code 0.
//
// AC: Exit code is 0 for valid help requests.
func TestCommandHelp_HelpExitCodeZero(t *testing.T) {
	// Test a representative sample of commands across the tree.
	// Each entry is the full args including --help.
	type helpTest struct {
		args []string
		path string // display name for error messages
	}

	tests := []helpTest{
		{[]string{"init", "--help"}, "init --help"},
		{[]string{"status", "--help"}, "status --help"},
		{[]string{"project", "--help"}, "project --help"},
		{[]string{"project", "status", "--help"}, "project status --help"},
		{[]string{"project", "remove", "--help"}, "project remove --help"},
		{[]string{"project", "version", "--help"}, "project version --help"},
		{[]string{"artifact", "--help"}, "artifact --help"},
		{[]string{"artifact", "package", "--help"}, "artifact package --help"},
		{[]string{"artifact", "verify", "--help"}, "artifact verify --help"},
		{[]string{"pipeline", "--help"}, "pipeline --help"},
		{[]string{"pipeline", "build", "--help"}, "pipeline build --help"},
		{[]string{"pipeline", "ci", "--help"}, "pipeline ci --help"},
		{[]string{"config", "--help"}, "config --help"},
		{[]string{"config", "get", "--help"}, "config get --help"},
		{[]string{"config", "levels", "--help"}, "config levels --help"},
		{[]string{"config", "list", "--help"}, "config list --help"},
		{[]string{"server", "--help"}, "server --help"},
		{[]string{"server", "init", "--help"}, "server init --help"},
		{[]string{"server", "status", "--help"}, "server status --help"},
		{[]string{"server", "config", "--help"}, "server config --help"},
		{[]string{"server", "config", "get", "--help"}, "server config get --help"},
		{[]string{"server", "config", "set", "--help"}, "server config set --help"},
		{[]string{"server", "project", "--help"}, "server project --help"},
		{[]string{"server", "project", "get", "--help"}, "server project get --help"},
		{[]string{"server", "project", "register", "--help"}, "server project register --help"},
		{[]string{"server", "release", "--help"}, "server release --help"},
		{[]string{"server", "release", "install", "--help"}, "server release install --help"},
		{[]string{"server", "release", "activate", "--help"}, "server release activate --help"},
		{[]string{"server", "release", "rollback", "--help"}, "server release rollback --help"},
		{[]string{"server", "release", "cleanup", "--help"}, "server release cleanup --help"},
		{[]string{"server", "release", "history", "--help"}, "server release history --help"},
		{[]string{"server", "release", "active", "--help"}, "server release active --help"},
		{[]string{"server", "release", "status", "--help"}, "server release status --help"},
		{[]string{"deployment", "--help"}, "deployment --help"},
		{[]string{"deployment", "info", "--help"}, "deployment info --help"},
		{[]string{"deployment", "upload", "--help"}, "deployment upload --help"},
		{[]string{"deployment", "install", "--help"}, "deployment install --help"},
		{[]string{"deployment", "activate", "--help"}, "deployment activate --help"},
		{[]string{"deployment", "rollback", "--help"}, "deployment rollback --help"},
		{[]string{"system", "--help"}, "system --help"},
		{[]string{"adapter", "--help"}, "adapter --help"},
		{[]string{"adapter", "list", "--help"}, "adapter list --help"},
		{[]string{"adapter", "inspect", "--help"}, "adapter inspect --help"},
		{[]string{"adapter", "use", "--help"}, "adapter use --help"},
		{[]string{"adapter", "install", "--help"}, "adapter install --help"},
		{[]string{"adapter", "uninstall", "--help"}, "adapter uninstall --help"},
		{[]string{"help", "--help"}, "help --help"},
	}

	for _, tt := range tests {
		_, _, stderr, err := executeCommand(tt.args...)
		if err != nil {
			t.Errorf("--help for %q returned error: %v (stderr: %q)", tt.path, err, stderr)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════
// ST-P8-03: Invalid Command Suggestions
// ═══════════════════════════════════════════════════════════════════

// TestSuggestion_RootLevel_MistypedCommand verifies that a mistyped
// root-level command produces a suggestion.
//
// AC: anvil pipelne → "Did you mean `pipeline`?"
// AC: Exit code is non-zero for invalid commands.
func TestSuggestion_RootLevel_MistypedCommand(t *testing.T) {
	_, _, stderr, err := executeCommand("pipelne")
	if err == nil {
		t.Fatal("expected error for invalid command, got nil")
	}

	if !contains(stderr, "pipelne") {
		t.Errorf("stderr should mention the invalid command name, got: %s", stderr)
	}

	// Should contain "pipeline" as a suggestion.
	if !contains(stderr, "pipeline") {
		t.Errorf("stderr should suggest 'pipeline', got: %s", stderr)
	}
}

// TestSuggestion_NestedLevel_MistypedCommand verifies that a mistyped
// command at a nested level produces a suggestion.
//
// AC: anvil server release activee → "Did you mean `active`?"
// Note: Levenshtein distance from "activee" to "active" = 1 (delete 'e'),
// while to "activate" = 4. With threshold 2, only "active" is suggested.
func TestSuggestion_NestedLevel_MistypedCommand(t *testing.T) {
	_, _, stderr, err := executeCommand("server", "release", "activee")
	if err == nil {
		t.Fatal("expected error for invalid command, got nil")
	}

	if !contains(stderr, "server release") {
		t.Errorf("stderr should mention the command context, got: %s", stderr)
	}

	// Should contain "active" as a suggestion.
	if !contains(stderr, "active") {
		t.Errorf("stderr should suggest 'active', got: %s", stderr)
	}
}

// TestSuggestion_ValidCommand_NoSuggestions verifies that a valid
// command executes normally without producing suggestions.
//
// AC: Running a valid command executes normally without suggestions.
func TestSuggestion_ValidCommand_NoSuggestions(t *testing.T) {
	// Use --help on a valid command — should produce help, not a suggestion error.
	_, _, stderr, err := executeCommand("init", "--help")
	if err != nil {
		t.Errorf("valid command 'init --help' should not produce an error, got: %v (stderr: %s)", err, stderr)
	}
}

// TestSuggestion_SettingsApplied verifies that the root command has
// the suggestion mechanism enabled.
//
// AC: SuggestionsMinimumDistance is set on root and parent commands.
func TestSuggestion_SettingsApplied(t *testing.T) {
	if rootCmd.SuggestionsMinimumDistance != 2 {
		t.Errorf("rootCmd.SuggestionsMinimumDistance = %d, want 2", rootCmd.SuggestionsMinimumDistance)
	}

	// Verify a parent command also has the setting.
	server, _, err := rootCmd.Find([]string{"server"})
	if err != nil {
		t.Fatalf("could not find server command: %v", err)
	}
	if server.SuggestionsMinimumDistance != 2 {
		t.Errorf("serverCmd.SuggestionsMinimumDistance = %d, want 2", server.SuggestionsMinimumDistance)
	}

	// Verify a deeply nested parent command has the setting.
	release, _, err := rootCmd.Find([]string{"server", "release"})
	if err != nil {
		t.Fatalf("could not find server release command: %v", err)
	}
	if release.SuggestionsMinimumDistance != 2 {
		t.Errorf("serverReleaseCmd.SuggestionsMinimumDistance = %d, want 2", serverReleaseCmd.SuggestionsMinimumDistance)
	}
}

// ═══════════════════════════════════════════════════════════════════
// ST-P8-04: Missing Argument Guidance
// ═══════════════════════════════════════════════════════════════════

// TestMissingArgGuidance_ShowsUsageExample verifies that running a
// command without a required argument displays the correct usage
// syntax and a concrete example.
//
// AC: Running a command without a required argument displays an error
// identifying the missing argument.
// AC: The error displays the correct usage syntax for that specific command.
// AC: The error includes a concrete example of correct usage.
func TestMissingArgGuidance_ShowsUsageExample(t *testing.T) {
	_, _, stderr, err := executeCommand("artifact", "verify")
	if err == nil {
		t.Fatal("expected error for missing artifact path, got nil")
	}

	// The error should include the command path.
	if !contains(stderr, "artifact verify") {
		t.Errorf("stderr should mention the command path, got: %s", stderr)
	}

	// The error should mention the required argument count.
	if !contains(stderr, "requires 1 argument") {
		t.Errorf("stderr should mention the required argument count, got: %s", stderr)
	}

	// The error should include a concrete example.
	if !contains(stderr, "Example:") {
		t.Errorf("stderr should include an 'Example:' line, got: %s", stderr)
	}

	// The error should include the argument name.
	if !contains(stderr, "<artifact-path>") {
		t.Errorf("stderr should include the argument name <artifact-path>, got: %s", stderr)
	}
}

// TestMissingArgGuidance_ValidCommand_NoGuidance verifies that running
// a command with all required arguments executes normally without
// guidance errors.
//
// AC: Running a command with all required arguments executes normally
// without guidance.
func TestMissingArgGuidance_ValidCommand_NoGuidance(t *testing.T) {
	// artifact verify requires 1 arg. We pass a dummy value; the command
	// may fail later (file not found), but arg validation should pass.
	_, _, stderr, err := executeCommand("artifact", "verify", "dummy.anvil")
	if err == nil {
		t.Fatal("expected error (artifact file missing), got nil")
	}

	// The error should NOT be about missing arguments.
	if contains(stderr, "requires 1 argument") {
		t.Errorf("stderr should not contain arg validation error when args are provided, got: %s", stderr)
	}
}

// TestMissingArgGuidance_MultipleArgs verifies guidance for commands
// that require multiple positional arguments.
//
// AC: Error identifies all missing arguments for multi-arg commands.
func TestMissingArgGuidance_MultipleArgs(t *testing.T) {
	_, _, stderr, err := executeCommand("server", "release", "install")
	if err == nil {
		t.Fatal("expected error for missing args, got nil")
	}

	if !contains(stderr, "requires 2 argument") {
		t.Errorf("stderr should mention 2 required arguments, got: %s", stderr)
	}

	if !contains(stderr, "Example:") {
		t.Errorf("stderr should include an 'Example:' line for multi-arg commands, got: %s", stderr)
	}
}

// TestMissingArgGuidance_OptionalArgNoError verifies that omitting an
// optional argument does not produce an error.
//
// AC: Optional arguments that are omitted do not produce errors.
func TestMissingArgGuidance_OptionalArgNoError(t *testing.T) {
	// server status accepts 0 or 1 arg (optional). Running with 0 args
	// should not produce an arg validation error.
	_, _, stderr, err := executeCommand("server", "status")
	if err == nil {
		// No error is fine — the command runs without args.
		return
	}

	// If there IS an error, it should NOT be about missing args.
	if contains(stderr, "argument") {
		t.Errorf("stderr should not contain argument validation error for optional arg, got: %s", stderr)
	}
}

// TestMissingArgGuidance_ExitCodeNonZero verifies that a missing
// required argument produces a non-zero exit code.
//
// AC: Exit code is non-zero when arguments are missing.
func TestMissingArgGuidance_ExitCodeNonZero(t *testing.T) {
	_, _, _, err := executeCommand("config", "get")
	if err == nil {
		t.Error("expected error for missing config key argument, got nil")
	}

	_, _, _, err = executeCommand("artifact", "verify")
	if err == nil {
		t.Error("expected error for missing artifact path argument, got nil")
	}

	_, _, _, err = executeCommand("server", "release", "active")
	if err == nil {
		t.Error("expected error for missing project-id argument, got nil")
	}
}
