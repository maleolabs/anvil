// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010, TS-P8-02, TS-P8-03
package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// requiredGroups lists all command groups that must exist at the top level
// of the CLI hierarchy. Each entry represents a parent-only group that
// serves as a namespace for domain-specific subcommands.
//
// Reference: ADR-010 §6
var requiredGroups = []struct {
	use         string // command Use value (the subcommand name)
	description string // what the group represents
}{
	{"project", "Project configuration and metadata"},
	{"artifact", "Artifact management"},
	{"runtime", "Runtime instance management"},
	{"pipeline", "Pipeline workflow execution"},
	{"config", "Configuration inspection"},
	{"server", "Server Runtime management"},
	{"deployment", "Deployment target and transport management (EPIC-010)"},
	{"adapter", "Framework adapter management (EPIC-007)"},
	{"system", "System-level operations"},
	{"help", "Help about any command"}, // cobra built-in help command (BUG-008)
}

// TestCommandHierarchy_AllGroupsExist verifies that all required command
// groups are registered under the root command.
//
// AC: Command groups exist for: project, artifact, runtime, pipeline,
// config, server, deployment, adapter, system, help.
func TestCommandHierarchy_AllGroupsExist(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd must not be nil")
	}

	// Build a lookup from registered commands. Key by Name() (not Use):
	// cobra's built-in help command has Use "help [command]" (BUG-008).
	registered := make(map[string]*cobra.Command)
	for _, sub := range rootCmd.Commands() {
		registered[sub.Name()] = sub
	}

	for _, g := range requiredGroups {
		cmd, ok := registered[g.use]
		if !ok {
			t.Errorf("required command group %q (%s) is not registered under root",
				g.use, g.description)
			continue
		}
		if cmd == nil {
			t.Errorf("command group %q is nil", g.use)
		}
	}
}

// TestCommandHierarchy_GroupNaming verifies that command group names use
// product terminology — lowercase, single-word names with no underscores,
// hyphens only where required by product naming conventions.
//
// AC: Command group names use product terminology, not implementation
// terminology.
func TestCommandHierarchy_GroupNaming(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd must not be nil")
	}

	for _, sub := range rootCmd.Commands() {
		name := sub.Name()

		// Names must not contain spaces (Name() should always return a single word).
		if strings.Contains(name, " ") {
			t.Errorf("command group %q contains spaces in Name(); use should be a single word", name)
		}

		// Names must not contain uppercase letters.
		for _, r := range name {
			if r >= 'A' && r <= 'Z' {
				t.Errorf("command group %q contains uppercase characters; use lowercase product terminology", name)
				break
			}
		}

		// Verify Short description is non-empty and uses product terminology.
		if sub.Short == "" {
			t.Errorf("command group %q has an empty Short description", name)
		}
	}
}

// TestCommandHierarchy_GroupsAreParents verifies that command groups
// are parent-only — they do not have RunE set (no operation logic at the
// group level).
//
// AC: Each group is a parent that contains domain-specific subcommands.
//
// NOTE: The "help" group is excluded from the RunE/Run check because
// cobra's built-in help command has Run set automatically. Our extended
// help group (ADR-010 §6.9) is defined alongside cobra's built-in.
func TestCommandHierarchy_GroupsAreParents(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd must not be nil")
	}

	// Commands that are permitted to have Run, RunE, or an Args validator
	// set (cobra built-ins or groups with intentional UX).
	skipRunCheck := map[string]bool{
		"help":       true, // cobra built-in help command has Run set
		"completion": true, // cobra built-in completion command has RunE set
		"system":     true, // NoArgsWithSuggestions rejects unknown subcommands (BUG-012, like serverReleaseCmd)
	}

	for _, g := range requiredGroups {
		cmd, _, err := rootCmd.Find([]string{g.use})
		if err != nil {
			t.Errorf("could not find command group %q: %v", g.use, err)
			continue
		}

		// Parent groups MUST NOT have RunE set — they should only serve
		// as namespaces for subcommands.
		if !skipRunCheck[g.use] && cmd.RunE != nil {
			t.Errorf("command group %q has RunE set; parent groups should not have execution logic", g.use)
		}

		// Run should also not be set (the older cobra pattern).
		if !skipRunCheck[g.use] && cmd.Run != nil {
			t.Errorf("command group %q has Run set; parent groups should not have execution logic", g.use)
		}

		// Verify the command has no Args validator (parents don't need one),
		// except for groups with intentional UX (see skipRunCheck).
		if !skipRunCheck[g.use] && cmd.Args != nil {
			t.Errorf("command group %q has custom Args validator; parent groups should not", g.use)
		}
	}
}

// ── Convention Enforcement (TS-P8-03) ────────────────────────────────
//
// The following tests verify that all commands follow the conventions
// defined in ADR-010 §3.4 and TS-P8-03.

// collectAllCommands recursively collects every command in the tree
// starting from rootCmd (including rootCmd itself).
func collectAllCommands() []*cobra.Command {
	var all []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		all = append(all, cmd)
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
	return all
}

// isKebabCase checks that s is lowercase-alphanumeric with hyphens only
// between words (no leading/trailing hyphens, no underscores, no uppercase).
func isKebabCase(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
			if i == 0 || i == len(s)-1 {
				return false // leading or trailing hyphen
			}
		default:
			return false // uppercase, underscore, or any other character
		}
	}
	return true
}

// TestCommandConventions_FlagNaming enforces that all flags throughout
// the command tree follow ADR-010 naming conventions:
//   - Long flags use descriptive kebab-case (e.g., --server-root)
//   - Short flags are single lowercase letters (e.g., -o)
//
// AC: Flag naming conventions enforced (short form = single letter,
// long form = descriptive kebab-case).
func TestCommandConventions_FlagNaming(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd must not be nil")
	}

	for _, cmd := range collectAllCommands() {
		cmdPath := cmd.CommandPath()
		cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
			// Long flag name must be kebab-case.
			if !isKebabCase(f.Name) {
				t.Errorf("command %q: long flag %q violates kebab-case convention (use lowercase, hyphens, no underscores)",
					cmdPath, f.Name)
			}

			// Short flag (shorthand) must be a single lowercase letter when present.
			if f.Shorthand != "" {
				if len(f.Shorthand) != 1 {
					t.Errorf("command %q: short flag %q for %q must be a single character",
						cmdPath, f.Shorthand, f.Name)
				} else if f.Shorthand[0] < 'a' || f.Shorthand[0] > 'z' {
					t.Errorf("command %q: short flag %q for %q must be a lowercase letter",
						cmdPath, f.Shorthand, f.Name)
				}
			}
		})
	}
}

// TestCommandConventions_NoRunWithoutRunE verifies that no command uses
// the older cobra.Run field; all action commands must use RunE for proper
// error handling.
//
// AC: No command uses Run where RunE would be appropriate.
func TestCommandConventions_NoRunWithoutRunE(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd must not be nil")
	}

	// Known exceptions that use cobra's built-in Run (not our commands).
	skip := map[string]bool{
		"help":    true, // cobra built-in help command
		"anvil":   true, // root command displays help; uses Run intentionally per ST-P8-01
		"release": true, // serverReleaseCmd displays group help when invoked directly (ST-P4-01 UX)
		"system":  true, // systemCmd displays group help when invoked directly (BUG-012)
	}

	for _, cmd := range collectAllCommands() {
		if skip[cmd.Name()] {
			continue
		}

		if cmd.Run != nil && cmd.RunE == nil {
			t.Errorf("command %q uses Run instead of RunE; use RunE for proper error handling",
				cmd.CommandPath())
		}
	}
}

// TestCommandConventions_ArgsValidators verifies that commands with
// positional arguments in their Use line have an Args validator set.
//
// AC: Argument handling consistency — positional args for resource
// identifiers, flags for options. Positional args must be validated.
func TestCommandConventions_ArgsValidators(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd must not be nil")
	}

	for _, cmd := range collectAllCommands() {
		// Skip root and known parent groups that intentionally lack RunE.
		if cmd == rootCmd || cmd.RunE == nil {
			continue
		}

		// If the Use line contains <...> (positional arg placeholders),
		// the command must have an Args validator.
		if hasPositionalArgs(cmd.Use) && cmd.Args == nil {
			t.Errorf("command %q has positional args in Use %q but no Args validator",
				cmd.CommandPath(), cmd.Use)
		}
	}
}

// hasPositionalArgs reports whether the Use string contains positional
// argument placeholders (e.g., "<name>", "<project-id>").
func hasPositionalArgs(use string) bool {
	return strings.Contains(use, "<")
}

// TestCommandConventions_LeafCommandsUseRunE verifies that leaf commands
// (commands with no subcommands) have RunE set so they can execute logic.
//
// AC: All executable commands use RunE for execution.
func TestCommandConventions_LeafCommandsUseRunE(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd must not be nil")
	}

	// Commands that are intentionally stubs or cobra built-ins.
	skip := map[string]bool{
		"help":       true, // cobra built-in help
		"completion": true, // cobra built-in completion
	}

	for _, cmd := range collectAllCommands() {
		if skip[cmd.Name()] {
			continue
		}

		// Skip root itself (it displays help).
		if cmd == rootCmd {
			continue
		}

		// Only check commands with no children (leaf commands).
		if len(cmd.Commands()) > 0 {
			continue
		}

		// If the command name matches a known parent group pattern, skip.
		// e.g., "serverProjectCmd" has no children but is a known parent.
		if cmd.RunE == nil {
			t.Errorf("leaf command %q has no subcommands and no RunE; executable leaf commands must implement RunE",
				cmd.CommandPath())
		}
	}
}

// TestCommandConventions_ParentGroupsNoRunE verifies that parent-only
// groups (namespaces for subcommands) do not have RunE, Run, or custom
// Args set. This extends the existing TestCommandHierarchy_GroupsAreParents
// to cover ALL parent groups in the tree, not just top-level ones.
//
// AC: Parent groups don't have RunE/Args — they serve only as namespaces.
func TestCommandConventions_ParentGroupsNoRunE(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd must not be nil")
	}

	skip := map[string]bool{
		"help":       true, // cobra built-in help command
		"completion": true, // cobra built-in completion
		"version":    true, // hybrid: displays version AND has subcommands
		"release":    true, // serverReleaseCmd uses NoArgsWithSuggestions for UX
		"system":     true, // systemCmd uses NoArgsWithSuggestions to reject ghost subcommands (BUG-012)
	}

	for _, cmd := range collectAllCommands() {
		if skip[cmd.Name()] || cmd == rootCmd {
			continue
		}

		// Only check commands that have at least one subcommand (parents).
		if len(cmd.Commands()) == 0 {
			continue
		}

		// Parent-only groups must not have RunE.
		if cmd.RunE != nil {
			t.Errorf("parent command %q has RunE set; parent groups should not have execution logic",
				cmd.CommandPath())
		}

		// Parent-only groups must not have Run.
		if cmd.Run != nil {
			t.Errorf("parent command %q has Run set; parent groups should not have execution logic",
				cmd.CommandPath())
		}

		// Parent-only groups must not have custom Args.
		if cmd.Args != nil {
			t.Errorf("parent command %q has custom Args validator; parent groups should not",
				cmd.CommandPath())
		}
	}
}

// TestCommandHelp_ExampleFields verifies that every leaf command
// has usage examples — either via Cobra's Example field or embedded
// in the Long description.
//
// AC: Command help includes examples (ST-P8-02 AC4).
func TestCommandHelp_ExampleFields(t *testing.T) {
	// Cobra built-in commands that don't need our examples.
	skipExamples := map[string]bool{
		"help":       true, // cobra built-in
		"completion": true, // cobra built-in group
	}

	// Cobra built-in completion subcommands.
	isCompletion := func(path string) bool {
		return strings.Contains(path, "completion")
	}

	for _, cmd := range collectAllCommands() {
		if skipExamples[cmd.Name()] {
			continue
		}

		// Only check commands with RunE (executable leaf commands).
		if cmd.RunE == nil {
			// Parent groups with subcommands but no RunE are OK.
			if len(cmd.Commands()) > 0 {
				continue
			}
		}

		// Skip cobra built-in completion subcommands.
		if isCompletion(cmd.CommandPath()) {
			continue
		}

		// Skip versionCmd (hybrid parent + display command).
		if cmd == versionCmd {
			continue
		}

		// Check for examples: either in Example field or Long description.
		hasExample := cmd.Example != ""
		if !hasExample {
			// Fall back: check Long for "Example" heading (various formats).
			hasExample = strings.Contains(cmd.Long, "Examples:\n") ||
				strings.Contains(cmd.Long, "Example:\n") ||
				strings.Contains(cmd.Long, "\nExample: ") ||
				strings.Contains(cmd.Long, "\nExamples: ")
		}

		if !hasExample {
			t.Errorf("command %q has no usage examples; add examples via Example field or Long description for ST-P8-02 compliance",
				cmd.CommandPath())
		}
	}
}

// TestCommandHierarchy_ReleaseGroupRemoved verifies that the ghost
// top-level "release" group has been removed (BUG-012): invoking
// "anvil release" (or any of its documented ghost subcommands such as
// "anvil release list") produces an "unknown command" error with a
// non-zero exit code instead of printing the group's help with exit 0.
//
// The functional release lifecycle surface ("anvil server release")
// must remain unaffected.
//
// AC: `anvil release list` no longer exits 0 with help output (BUG-012 DoD).
func TestCommandHierarchy_ReleaseGroupRemoved(t *testing.T) {
	// The top-level "release" group must not be registered at all.
	if _, _, err := rootCmd.Find([]string{"release"}); err == nil {
		t.Error("top-level 'release' group must not be registered (ghost surface removed)")
	}

	// "anvil release" and its documented ghost subcommands must produce
	// a clear "unknown command" error (non-zero exit), never help + exit 0.
	for _, args := range [][]string{
		{"release"},
		{"release", "list"},
		{"release", "show", "abc123"},
	} {
		_, stdout, stderr, err := executeCommand(args...)
		if err == nil {
			t.Errorf("executeCommand(%q) should return an error (unknown command), got nil", args)
		}
		if strings.Contains(stdout, "Usage:") {
			t.Errorf("executeCommand(%q) should not print help to stdout, got:\n%s", args, stdout)
		}
		if !strings.Contains(stderr, "unknown command") {
			t.Errorf("executeCommand(%q) stderr should indicate an unknown command, got: %q", args, stderr)
		}
	}

	// The functional release lifecycle surface is unaffected.
	serverRelease, _, err := rootCmd.Find([]string{"server", "release"})
	if err != nil {
		t.Fatalf("could not find 'server release' command: %v", err)
	}
	if serverRelease != serverReleaseCmd {
		t.Error("'server release' command must remain registered as serverReleaseCmd")
	}
}

// TestSystemHelp_DocumentsOnlyRegisteredCommands verifies that
// "anvil system --help" documents only the commands that exist
// (health, diagnose, inspect) and no longer advertises the ghost
// "anvil system version" / "anvil system info" examples (BUG-012).
//
// AC: anvil system --help examples match only health, diagnose, and
// inspect (BUG-012 Validation step 3).
func TestSystemHelp_DocumentsOnlyRegisteredCommands(t *testing.T) {
	_, stdout, stderr, err := executeCommand("system", "--help")
	if err != nil {
		t.Fatalf("executeCommand('system', '--help') returned unexpected error: %v (stderr: %q)", err, stderr)
	}

	// The help must document the real subcommands.
	for _, want := range []string{"health", "diagnose", "inspect"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("system --help should document the %q subcommand, got:\n%s", want, stdout)
		}
	}

	// The help must not advertise ghost subcommands.
	for _, ghost := range []string{"anvil system version", "anvil system info"} {
		if strings.Contains(stdout, ghost) {
			t.Errorf("system --help must not document ghost command %q (BUG-012), got:\n%s", ghost, stdout)
		}
	}

	// The ghost subcommands themselves must produce an unknown-command error.
	for _, args := range [][]string{{"system", "version"}, {"system", "info"}} {
		_, _, ghostStderr, ghostErr := executeCommand(args...)
		if ghostErr == nil {
			t.Errorf("executeCommand(%q) should return an error (unknown command), got nil", args)
		}
		if !strings.Contains(ghostStderr, "unknown command") {
			t.Errorf("executeCommand(%q) stderr should indicate an unknown command, got: %q", args, ghostStderr)
		}
	}
}

// TestCommandHelp_ExamplesResolveToRegisteredCommands verifies that
// every usage example documented in command help (Example field or Long
// text) begins with a registered command path.
//
// Scope note: cobra's Find() resolves the longest registered command
// prefix and treats any remaining words as arguments, so this test
// cannot detect ghost subcommands under a registered parent group
// (pre-fix, "anvil release list" resolved to the registered "release"
// group and passed). Its real scope is guarding examples that point at
// unregistered top-level commands — and, after the BUG-012 removal, any
// example that re-introduces "anvil release ...", since the "release"
// group itself is no longer registered.
//
// Deliberately not strengthened to validate leftover words as
// subcommands: examples legitimately contain positional args and flags,
// so that check would require modeling each command's arg semantics and
// would be brittle. Ghost-subcommand coverage lives in the dedicated
// tests (TestCommandHierarchy_ReleaseGroupRemoved,
// TestSystemHelp_DocumentsOnlyRegisteredCommands).
//
// AC: Help examples begin with a registered command path (BUG-012 DoD).
func TestCommandHelp_ExamplesResolveToRegisteredCommands(t *testing.T) {
	// The built-in help command's Long text is cobra's generic template
	// ("Simply type anvil help [path to command] ..."), which is not a
	// concrete usage example; skip that group. The custom "help" group
	// whose Long text advertised the dead quickstart/configuration
	// topics was removed in BUG-008 (T-013).
	skipPath := "anvil help"

	for _, cmd := range collectAllCommands() {
		if cmd == rootCmd || cmd.CommandPath() == skipPath {
			continue
		}

		text := cmd.Example + "\n" + cmd.Long
		for _, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "anvil ") {
				continue
			}

			// Resolve the longest command prefix of the example. Trailing
			// words are positional args/flags for that command, e.g.
			// "anvil server release install my-project --json" resolves
			// to "anvil server release install".
			words := strings.Fields(trimmed)
			resolved := false
			for i := len(words); i >= 2; i-- {
				sub, _, err := rootCmd.Find(words[1:i])
				if err == nil && sub != nil {
					resolved = true
					break
				}
			}
			if !resolved {
				t.Errorf("command %q documents ghost example %q that does not resolve to a registered command (BUG-012)",
					cmd.CommandPath(), trimmed)
			}
		}
	}
}
