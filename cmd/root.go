// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P1-01, ADR-010, ADR-012, ST-P8-01, ST-P8-02
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
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
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once.
func Execute() error {
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
// Reference: ST-P8-01, ADR-010 §6
var rootDomainGroups = []domainGroup{
	{
		Name:     "Development",
		Commands: []string{"init", "status", "project", "config", "artifact", "pipeline"},
	},
	{
		Name:     "Deployment",
		Commands: []string{"release", "deployment"},
	},
	{
		Name:     "Server Runtime",
		Commands: []string{"server", "runtime"},
	},
	{
		Name:     "System",
		Commands: []string{"system", "update", "help", "adapter"},
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

	rootCmd.AddCommand(initCmd)
}

// ── Extension Points ──────────────────────────────────────────────
//
// EPIC-007 Adapter Commands (not yet implemented):
//
//	When EPIC-007 is implemented, adapter commands should be registered
//	by creating cmd/adapter.go with an init() function that calls:
//
//	    rootCmd.AddCommand(adapterCmd)
//
//	The adapterCmd variable should be defined as a parent-only cobra.Command
//	group (Use: "adapter") in that file. Domain-specific subcommands
//	(e.g., "anvil adapter list", "anvil adapter inspect") should be added
//	as children of adapterCmd.
//
//	See ADR-010 §6.7 for the command hierarchy specification.
// ──────────────────────────────────────────────────────────────────
