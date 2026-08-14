// Package cmd implements the Anvil CLI commands.
//
// ── Argument Validators (ST-P8-04) ─────────────────────────────────
//
// These validators replace cobra.ExactArgs, cobra.MaximumNArgs, etc.
// with error messages that include the command usage and a concrete
// example, making it clear what the user should type.
//
// When validation fails:
//   - Cobra prints: "Error: <our message>"
//   - Cobra then prints the command's Usage line automatically
//
// The output includes an Example line drawn from the validator so that
// users see concrete correct syntax immediately.
//
// Reference: ST-P8-04, ADR-010 §3.4, ADR-010 §4.3
package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// ── Positional-Argument Validators ─────────────────────────────────

// ExactArgsWithUsage returns cobra.ExactArgs with a custom error
// message that includes the argument names from the Use line and a
// concrete usage example.
//
// example should be a complete command invocation such as:
//
//	"anvil artifact status abc123def456"
//
// argNames describes each positional argument (e.g., "identity").
// When empty the validator extracts them from the command's Use string.
func ExactArgsWithUsage(n int, example string, argNames ...string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			names := extractArgNames(cmd.Use, n, argNames)
			return fmt.Errorf("the %q command requires %d argument(s): %s\nExample: %s",
				cmd.CommandPath(), n, names, example)
		}
		return nil
	}
}

// MinimumNArgsWithUsage returns cobra.MinimumNArgs with a custom error
// message that includes the argument names and a usage example.
//
// example should be a complete command invocation.
// argNames describes each positional argument.
func MinimumNArgsWithUsage(n int, example string, argNames ...string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < n {
			names := extractArgNames(cmd.Use, n, argNames)
			return fmt.Errorf("the %q command requires at least %d argument(s): %s\nExample: %s",
				cmd.CommandPath(), n, names, example)
		}
		return nil
	}
}

// MaximumNArgsWithUsage returns cobra.MaximumNArgs with a custom error
// message that includes the argument names and a usage example.
//
// example should be a complete command invocation.
// argNames describes each positional argument.
func MaximumNArgsWithUsage(n int, example string, argNames ...string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > n {
			names := extractArgNames(cmd.Use, n, argNames)
			return fmt.Errorf("the %q command accepts at most %d argument(s): %s\nExample: %s",
				cmd.CommandPath(), n, names, example)
		}
		return nil
	}
}

// RangeArgsWithUsage returns cobra.RangeArgs with a custom error
// message that includes the argument names and a usage example.
//
// example should be a complete command invocation.
// argNames describes each positional argument.
func RangeArgsWithUsage(min, max int, example string, argNames ...string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < min || len(args) > max {
			names := extractArgNames(cmd.Use, max, argNames)
			return fmt.Errorf("the %q command requires between %d and %d argument(s): %s\nExample: %s",
				cmd.CommandPath(), min, max, names, example)
		}
		return nil
	}
}

// NoArgsWithSuggestions returns a cobra.PositionalArgs validator that
// rejects any positional arguments and, when args are present, generates
// suggestions using the command's subcommand names.
//
// This is intended for parent (namespace) commands that should not accept
// positional arguments. When a user types an unknown subcommand name,
// the error message includes "Did you mean ...?" suggestions.
//
// Example:
//
//	anvil server release activ8
//	→ Error: unknown command "activ8" for "anvil server release"
//	  Did you mean this?
//	      activate
func NoArgsWithSuggestions() cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			arg := args[0]
			// Check if it looks like a flag (not a mistyped command).
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("unknown flag: %q", arg)
			}
			// Generate suggestions using Cobra's built-in mechanism.
			suggestions := cmd.SuggestionsFor(arg)
			if len(suggestions) > 0 {
				var sb strings.Builder
				fmt.Fprintf(&sb, "unknown command %q for %q", arg, cmd.CommandPath())
				sb.WriteString("\n\nDid you mean this?")
				for _, s := range suggestions {
					fmt.Fprintf(&sb, "\n\t%s", s)
				}
				return errors.New(sb.String())
			}
			return fmt.Errorf("unknown command %q for %q", arg, cmd.CommandPath())
		}
		return nil
	}
}

// ── Helpers ────────────────────────────────────────────────────────

// extractArgNames extracts argument names from the Use string
// (content inside angle brackets like "<name>"), falling back to
// the provided names when parsing fails.
func extractArgNames(use string, n int, provided []string) string {
	if len(provided) > 0 {
		// Concatenate provided arg names.
		result := ""
		for i, name := range provided {
			if i > 0 {
				result += " "
			}
			result += "<" + name + ">"
		}
		return result
	}

	// Fall back to extracting from the Use string.
	names := extractAngleBracketArgs(use)
	if len(names) > 0 {
		return names
	}

	// Last resort: generic placeholder.
	return "<arg>"
}

// extractAngleBracketArgs extracts text inside angle brackets from
// the Use string and returns them space-separated with brackets.
//
// Example: "set <key> <value>" → "<key> <value>"
func extractAngleBracketArgs(use string) string {
	result := ""
	inBracket := false
	for _, r := range use {
		if r == '<' {
			inBracket = true
			if result != "" {
				result += " "
			}
			result += "<"
		} else if r == '>' {
			inBracket = false
			result += ">"
		} else if inBracket {
			result += string(r)
		}
	}
	return result
}
