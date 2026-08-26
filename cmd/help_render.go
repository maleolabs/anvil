package cmd

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"unicode"

	"maleolabs.com/anvil/internal/output"
	"github.com/spf13/cobra"
)

const helpCommandName = "help"

func renderHelp(cmd *cobra.Command, s *output.Style, w io.Writer) {
	text := cmd.Long
	if text == "" {
		text = cmd.Short
	}
	text = strings.TrimRightFunc(text, unicode.IsSpace)
	if text != "" {
		fmt.Fprintln(w, text)
		fmt.Fprintln(w)
	}
	if cmd.Runnable() || cmd.HasSubCommands() {
		renderUsage(cmd, s, w)
	}
}

func renderUsage(cmd *cobra.Command, s *output.Style, w io.Writer) {
	fmt.Fprint(w, "Usage:")
	if cmd.Runnable() {
		fmt.Fprintf(w, "\n  %s", cmd.UseLine())
	}
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintf(w, "\n  %s [command]", cmd.CommandPath())
	}
	if len(cmd.Aliases) > 0 {
		fmt.Fprintf(w, "\n\nAliases:\n  %s", cmd.NameAndAliases())
	}
	if cmd.HasExample() {
		fmt.Fprintf(w, "\n\nExamples:\n%s", cmd.Example)
	}
	if cmd.HasAvailableSubCommands() {
		cmds := cmd.Commands()
		if len(cmd.Groups()) == 0 {
			// Use domain groups for root, flat for subcommands
			if cmd == rootCmd {
				renderDomainGroups(s, w, cmds)
			} else {
				fmt.Fprint(w, "\n\nAvailable Commands:")
				for _, sub := range cmds {
					if sub.IsAvailableCommand() || sub.Name() == helpCommandName {
						fmt.Fprintf(w, "\n  %s %s",
							s.Info(rpad(sub.Name(), sub.NamePadding())), sub.Short)
					}
				}
			}
		} else {
			// Generic grouped rendering if Groups defined
			for _, g := range cmd.Groups() {
				fmt.Fprintf(w, "\n\n%s", s.Accent(g.Title))
				for _, sub := range cmds {
					if sub.GroupID != g.ID {
						continue
					}
					if sub.IsAvailableCommand() || sub.Name() == helpCommandName {
						fmt.Fprintf(w, "\n  %s %s",
							s.Info(rpad(sub.Name(), sub.NamePadding())), sub.Short)
					}
				}
			}
		}
	}
	if cmd.HasAvailableLocalFlags() {
		fmt.Fprint(w, "\n\nFlags:\n")
		renderFlagUsages(s, w, strings.TrimRightFunc(cmd.LocalFlags().FlagUsages(), unicode.IsSpace))
	}
	if cmd.HasAvailableInheritedFlags() {
		fmt.Fprint(w, "\n\nGlobal Flags:\n")
		renderFlagUsages(s, w, strings.TrimRightFunc(cmd.InheritedFlags().FlagUsages(), unicode.IsSpace))
	}
	if cmd.HasHelpSubCommands() {
		fmt.Fprint(w, "\n\nAdditional help topics:")
		for _, sub := range cmd.Commands() {
			if sub.IsAdditionalHelpTopicCommand() {
				fmt.Fprintf(w, "\n  %s %s",
					rpad(sub.CommandPath(), sub.CommandPathPadding()), sub.Short)
			}
		}
	}
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintf(w, "\n\n%s", s.Dim(fmt.Sprintf(
			"Use %q for more information about a command.",
			cmd.CommandPath()+" [command] --help")))
	}
	fmt.Fprintln(w)
}

func renderDomainGroups(s *output.Style, w io.Writer, cmds []*cobra.Command) {
	for _, group := range rootDomainGroups {
		fmt.Fprintf(w, "\n\n%s", s.Accent(group.Name))
		for _, cmdName := range group.Commands {
			sub, _, err := rootCmd.Find([]string{cmdName})
			if err == nil && sub != nil && (sub.IsAvailableCommand() || sub.Name() == helpCommandName) {
				fmt.Fprintf(w, "\n  %s %s",
					s.Info(rpad(sub.Name(), sub.NamePadding())), sub.Short)
			}
		}
	}
}

func renderFlagUsages(s *output.Style, w io.Writer, usages string) {
	if !s.Color {
		io.WriteString(w, usages)
		return
	}
	for _, line := range strings.SplitAfter(usages, "\n") {
		if i := flagNameEnd(line); i > 0 {
			io.WriteString(w, s.Dim(line[:i]))
			io.WriteString(w, line[i:])
		} else {
			io.WriteString(w, line)
		}
	}
}

func flagNameEnd(line string) int {
	for i := 1; i+1 < len(line); i++ {
		if line[i] == ' ' && line[i+1] == ' ' && line[i-1] != ' ' {
			return i
		}
	}
	return -1
}

func rpad(s string, width int) string {
	return fmt.Sprintf("%-*s", width, s)
}

// ensure help_render uses bytes import
var _ = bytes.Buffer{}
