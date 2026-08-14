// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P1-01, ADR-010, ADR-012, ST-P8-01, ST-P8-02
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/output"
)

// CliVersion is the Anvil CLI version, set at build time via ldflags.
// When not overridden, it defaults to "0.0.0-dev".
var CliVersion = "0.0.0-dev"

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "anvil",
	Short: "Release lifecycle engine for single-server deployments",
	Long: `Anvil is a release lifecycle engine for single-server deployments.

It provides a standardized, framework-agnostic toolkit for initializing
projects, packaging releases, activating deployments, and rolling back
when needed.

Complete documentation is available at https://maleolabs.com/anvil`,
	// PersistentPreRunE bootstraps the first-run configuration layout
	// (registry index directory + trust anchors file) before any command
	// executes; see ensureDefaultConfigLayout.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return ensureDefaultConfigLayout()
	},
}

// propagateSuggestionsOnce is set to true after we've propagated
// the SuggestionsMinimumDistance to all parent commands.
var propagateSuggestionsOnce bool

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once.
func Execute() error {
	// Propagate SuggestionsMinimumDistance to every parent command in the
	// tree so that suggestions work at ALL hierarchy levels.
	// We do this here (not in init) because Go's init ordering means
	// not all commands are registered when root.go's init runs.
	if !propagateSuggestionsOnce {
		walkCommands(func(cmd *cobra.Command) {
			if cmd != rootCmd && len(cmd.Commands()) > 0 {
				cmd.SuggestionsMinimumDistance = 2
			}
		})
		propagateSuggestionsOnce = true
	}

	// Honor the global.no_color configuration key (TS-008-009). NO_COLOR env is
	// handled inside internal/output.
	if cfg, err := config.LoadConfig(); err == nil {
		if v, _, err := cfg.GetBool("global.no_color"); err == nil && v {
			output.SetNoColor(true)
		}
	}
	return rootCmd.Execute()
}

// ── Domain Groups (ST-P8-01) ──────────────────────────────────────
//
// domainGroup defines a product domain section in the top-level help.
type domainGroup struct {
	Name     string
	Commands []string // command names (Use field) belonging to this domain
}

// rootDomainGroups organises all top-level commands by product domain
// for the custom help display.
//
// The "adapter" group belongs to Development: "anvil adapter use" is a
// repository-aware action that writes project.framework into anvil.yaml
// (TS-008-008 command context, decision 021). The adapter group is the
// deprecated v1.x surface retained for the dual-run window (ADR-028,
// ADR-032) — "standard" is the canonical group and both stay listed
// until the aliases are removed. The "skill" group is the AI agent
// skills surface (ADR-037 D4): it installs into the current repo or the
// user's agent directories, so it belongs to Development alongside
// standard. The legacy "runtime" group was removed
// at the end of its deprecation window (ADR-032 D12, TS-019-04-01); the
// "server" group is the sole Server Runtime surface (ADR-014).
//
// Reference: ST-P8-01, ADR-010 §6, ADR-037
var rootDomainGroups = []domainGroup{
	{
		Name:     "Development",
		Commands: []string{"init", "status", "project", "config", "artifact", "pipeline", "adapter", "standard", "skill"},
	},
	{
		Name:     "Deployment",
		Commands: []string{"deployment"},
	},
	{
		Name:     "Server Runtime",
		Commands: []string{"server"},
	},
	{
		Name:     "System",
		Commands: []string{"system", "update", "help"},
	},
}

// printDomainHelp prints the top-level help with commands organised
// by product domain. It is used for both bare invocation (rootCmd.Run)
// and the custom help function (rootCmd.SetHelpFunc).
//
// Reference: ST-P8-01
func printDomainHelp(cmd *cobra.Command) {
	// Print the command's description.
	fmt.Fprintln(cmd.OutOrStdout(), cmd.Long)
	fmt.Fprintln(cmd.OutOrStdout())

	fmt.Fprintln(cmd.OutOrStdout(), "Usage:")
	fmt.Fprintf(cmd.OutOrStdout(), "  %s [command]\n", cmd.Name())
	fmt.Fprintln(cmd.OutOrStdout())

	fmt.Fprintln(cmd.OutOrStdout(), "Product Domains:")

	for _, group := range rootDomainGroups {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", group.Name)
		for _, cmdName := range group.Commands {
			sub, _, err := cmd.Find([]string{cmdName})
			if err == nil && sub != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "    %-12s %s\n", sub.Name(), sub.Short)
			}
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}

	fmt.Fprintf(cmd.OutOrStdout(), `Use "%s [command] --help" for more information about a command.`+"\n", cmd.Name())
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), exitCodesSummary)
}

// ── Help / Run Setup (ST-P8-01, ST-P8-02) ─────────────────────────

// defaultHelpFunc captures Cobra's default help function before we
// override it on rootCmd. Subcommands inherit root's HelpFunc, so we
// must delegate back to the default for non-root commands.
var defaultHelpFunc func(*cobra.Command, []string)

func init() {
	// Capture the default help function before overriding.
	defaultHelpFunc = rootCmd.HelpFunc()

	// rootCmd.Run handles bare invocation ("anvil" with no arguments).
	// Cobra calls Run when no subcommand matches and no special flags
	// (--help, --version) are present.
	rootCmd.Run = func(cmd *cobra.Command, args []string) {
		printDomainHelp(cmd)
	}

	// Override the help function so that --help on root also shows the
	// domain-organised output. Subcommands inherit this function but
	// we detect root and delegate to the saved default for others.
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd == rootCmd {
			printDomainHelp(cmd)
		} else {
			defaultHelpFunc(cmd, args)
		}
	})

	// Set version from the CliVersion variable, which may be overridden
	// via ldflags at build time (e.g., go build -ldflags="-X maleolabs.com/anvil/cmd.CliVersion=1.0.0").
	rootCmd.Version = CliVersion

	// ── Invalid Command Suggestions (ST-P8-03) ─────────────────────
	//
	// Enable fuzzy command name matching so that mistyped commands
	// ("pipelne" → "pipeline") produce a "Did you mean ...?" suggestion.
	// Distance 2 catches most single-character typos and transpositions.
	// Root-level suggestions are set here; nested-level suggestions are
	// propagated in Execute() after all commands have been registered.
	rootCmd.SuggestionsMinimumDistance = 2
	rootCmd.SuggestFor = []string{"--help", "--version"}

	rootCmd.AddCommand(initCmd)
}

// ── Command Tree Traversal ────────────────────────────────────────

// walkCommands recursively visits every command in the tree starting
// from root (including root itself), calling fn for each.
func walkCommands(fn func(*cobra.Command)) {
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		fn(cmd)
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}

// ── Extension Points ──────────────────────────────────────────────
//
// EPIC-007 Adapter Commands (implemented):
//
//	The adapter command group is implemented in cmd/adapter.go: a
//	parent-only cobra.Command group (Use: "adapter") registered via
//	rootCmd.AddCommand(adapterCmd) with domain-specific subcommands
//	("anvil adapter list", "anvil adapter inspect", "anvil adapter
//	use") as children (TS-007-031, TS-007-032, TS-007-033).
//
//	Future adapter commands are added as children of adapterCmd in
//	cmd/adapter.go. See ADR-010 §6.7 for the command hierarchy
//	specification.
// ──────────────────────────────────────────────────────────────────
