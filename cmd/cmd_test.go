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
	{"release", "Release lifecycle management"},
	{"artifact", "Artifact management"},
	{"runtime", "Runtime instance management"},
	{"pipeline", "Pipeline workflow execution"},
	{"config", "Configuration inspection"},
	{"server", "Server Runtime management"},
	{"deployment", "Deployment target and transport management (EPIC-010)"},
	{"adapter", "Framework adapter management (EPIC-007)"},
	{"system", "System-level operations"},
	{"help", "Extended help and documentation"},
}

// TestCommandHierarchy_AllGroupsExist verifies that all required command
// groups are registered under the root command.
//
// AC: Command groups exist for: project, release, artifact, runtime,
// pipeline, config, adapter, system, help.
func TestCommandHierarchy_AllGroupsExist(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd must not be nil")
	}

	// Build a lookup from registered commands.
	registered := make(map[string]*cobra.Command)
	for _, sub := range rootCmd.Commands() {
		registered[sub.Use] = sub
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

	// Commands that are permitted to have Run or RunE set (cobra built-ins
	// or stub parent groups that display help when invoked directly).
	skipRunCheck := map[string]bool{
		"help":       true, // cobra built-in help command has Run set
		"completion": true, // cobra built-in completion command has RunE set
		"release":    true, // stub parent — displays help until subcommands exist
		"adapter":    true, // stub parent — EPIC-007 not yet implemented
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

		// Verify the command has no Args validator (parents don't need one).
		if cmd.Args != nil {
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
		"release": true, // stub parent — displays help until subcommands exist
		"adapter": true, // stub parent — EPIC-007 not yet implemented
		"system":  true, // stub parent — subcommands not yet implemented
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

	// Known parent stubs that have no subcommands yet but are not leaf
	// commands — they serve as extension points for future EPICs.
	isStubParent := map[string]bool{
		"adapter": true, // EPIC-007 not yet implemented
		"release": true, // release subcommands not yet implemented
		"system":  true, // system subcommands not yet implemented
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

		// Skip known stub parent commands.
		if isStubParent[cmd.Name()] {
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

		// Skip adapter (stub parent group, EPIC-007).
		if cmd.Name() == "adapter" {
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

// TestCommandHierarchy_ReleaseGroupDistinct verifies that the top-level
// "release" group is distinct from "anvil server release" (serverReleaseCmd).
//
// AC: Top-level "anvil release" and "anvil server release" are separate
// commands with separate parent hierarchies.
func TestCommandHierarchy_ReleaseGroupDistinct(t *testing.T) {
	// Find the top-level release command.
	release, _, err := rootCmd.Find([]string{"release"})
	if err != nil {
		t.Fatalf("could not find top-level 'release' command: %v", err)
	}

	// Verify it is registered directly under root.
	if release.Parent() != rootCmd {
		t.Errorf("top-level release command's parent is %v, want rootCmd", release.Parent())
	}

	// Verify the top-level release is NOT the same command as serverReleaseCmd.
	if release == serverReleaseCmd {
		t.Error("top-level release command and serverReleaseCmd are the same; they must be distinct")
	}

	// Verify serverReleaseCmd exists and is a child of serverCmd.
	if serverReleaseCmd == nil {
		t.Fatal("serverReleaseCmd must not be nil")
	}

	// Verify serverReleaseCmd is registered under serverCmd.
	server, _, err := rootCmd.Find([]string{"server"})
	if err != nil {
		t.Fatalf("could not find 'server' command: %v", err)
	}

	found := false
	for _, sub := range server.Commands() {
		if sub == serverReleaseCmd {
			found = true
			break
		}
	}
	if !found {
		t.Error("serverReleaseCmd is not registered as a child of the server command")
	}
}
