// Package project provides user-facing error formatting for missing
// Anvil projects. When no project is found in the current working directory
// or any parent directory, FormatMissingProjectError produces a message
// that tells the user what happened, where the search looked, and what
// to do next.
//
// Reference: ST-P1-06
package project

import "fmt"

// FormatMissingProjectError returns a user-facing error message for when
// no Anvil project is found in the searched directories.
//
// The message includes:
//   - A clear statement that no project was found
//   - The list of directories that were searched (from CWD upward)
//   - Guidance on creating a new project with "anvil init"
//   - Guidance on navigating to a directory that contains a project
//
// Parameters:
//   - searched: the ordered list of directories searched (from CWD to root)
//
// Returns:
//   - A formatted multi-line string suitable for printing to stderr
//
// Reference: ST-P1-06
func FormatMissingProjectError(searched []string) string {
	msg := "Error: no Anvil project found.\n\nSearched directories:\n"
	for _, dir := range searched {
		msg += fmt.Sprintf("  %s\n", dir)
	}
	msg += "\nTo create a new project, run:\n"
	msg += "  anvil init <project-name>\n\n"
	msg += "Or navigate to a directory that contains an Anvil project."
	return msg
}
