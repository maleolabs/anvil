// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P1-01, ADR-010, ADR-012, ST-P8-01, ST-P8-02
package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/output"
)

// CliVersion is the Anvil CLI version, set at build time via ldflags.
// When not overridden, it defaults to "0.0.0-dev".
var CliVersion = "0.0.0-dev"

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:           "anvil",
	Short:         "Release lifecycle engine for single-server deployments",
	SilenceErrors: true,
	SilenceUsage:  true,
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
	err := rootCmd.Execute()
	if err != nil {
		var rep *reportedError
		if !errors.As(err, &rep) {
			renderStyledError(rootCmd, err)
		}
	}
	return err
}

// renderStyledError prints a cobra-level error (unknown command, unknown
// flag, argument validation) through the platform theme: the first line in
// Error color, the "Did you mean this?" header and the usage hint in Dim,
// each suggestion in Info — all wrapped in the output container (uniform
// left margin + leading/trailing blank lines) on stderr, so error output
// never sticks to the terminal corner. Non-TTY output is plain text,
// byte-deterministic. Command-level errors already reported through
// ReportError/WriteAppError are printed once here from their returned
// error, removing the previous double echo.
func renderStyledError(root *cobra.Command, err error) {
	es := output.NewStyle(root.ErrOrStderr(), false)
	msg := err.Error()
	var b strings.Builder
	for i, line := range strings.Split(msg, "\n") {
		switch {
		case i == 0:
			b.WriteString(es.Error("Error: " + line))
		case strings.TrimSpace(line) == "Did you mean this?":
			b.WriteString(es.Dim(line))
		case strings.HasPrefix(line, "\t"):
			b.WriteString(es.Info("  " + strings.TrimSpace(line)))
		default:
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	if strings.Contains(msg, "unknown command") || strings.Contains(msg, "unknown flag") {
		if !strings.HasSuffix(b.String(), "\n\n") {
			b.WriteString("\n")
		}
		b.WriteString(es.Dim(fmt.Sprintf("Run '%s --help' for more information about a command.", root.Name())))
		b.WriteString("\n")
	}
	output.Container(es, b.String())
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
		Commands: []string{"init", "status", "project", "config", "artifact", "pipeline", "installer", "adapter", "standard", "skill"},
	},
	{
		Name:     "Deployment",
		Commands: []string{"deploy", "deployment"},
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
	s := styleFor(cmd)
	var b bytes.Buffer
	writeDomainHelp(s, &b, cmd)
	output.Container(s, b.String())
}

// writeDomainHelp renders the top-level help (domain-grouped command
// overview) into w. Shared by the bare invocation and the help surfaces
// (SetHelpFunc routes root here) so the two never drift apart.
func writeDomainHelp(s *output.Style, w io.Writer, cmd *cobra.Command) {
	fmt.Fprintln(w, cmd.Long)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  %s [command]\n", cmd.Name())
	fmt.Fprintln(w)
	fmt.Fprintln(w, s.Accent("Product Domains:"))
	for _, group := range rootDomainGroups {
		fmt.Fprintf(w, "  %s\n", s.Accent(group.Name))
		for _, cmdName := range group.Commands {
			sub, _, err := cmd.Find([]string{cmdName})
			if err == nil && sub != nil {
				fmt.Fprintf(w, "    %s %s\n", s.Info(fmt.Sprintf("%-12s", sub.Name())), sub.Short)
			}
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "%s\n", s.Dim(fmt.Sprintf(`Use "%s [command] --help" for more information about a command.`, cmd.Name())))
	fmt.Fprintln(w)
	fmt.Fprintln(w, s.Dim(exitCodesSummary))
}

// ── Help / Run Setup (ST-P8-01, ST-P8-02) ─────────────────────────

// defaultHelpFunc captures Cobra's default help function before we
// override it on rootCmd. Subcommands inherit root's HelpFunc, so we
// must delegate back to the default for non-root commands.
var defaultHelpFunc func(*cobra.Command, []string)

func init() {
	// Persistent verbose flag for Style (presentation-only)
	rootCmd.PersistentFlags().Bool(flagVerbose, false, "verbose output")
	// Capture the default help function before overriding.
	defaultHelpFunc = rootCmd.HelpFunc()

	// rootCmd.Run handles bare invocation ("anvil" with no arguments).
	rootCmd.Run = func(cmd *cobra.Command, args []string) {
		printDomainHelp(cmd)
	}

	// Unified help with Style+Container (never sticks to corner)
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		// Render into a private buffer and emit through the style's raw
		// writer. Deliberately NO cmd.SetOut swap here: pinning the
		// resolved writer back onto a child command would break writer
		// inheritance for every later execution (tests re-point root's
		// output per call; a pinned child keeps writing to the stale
		// buffer — the v2.4.0 release-CI failure). renderHelp takes an
		// explicit io.Writer, so the swap is unnecessary.
		s := styleFor(cmd)
		var buf bytes.Buffer
		if cmd == rootCmd {
			// Root surfaces (bare anvil, --help, anvil help) share ONE
			// renderer so the three never drift apart.
			writeDomainHelp(s, &buf, cmd)
		} else {
			renderHelp(cmd, s, &buf)
		}
		output.Container(s, buf.String())
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
