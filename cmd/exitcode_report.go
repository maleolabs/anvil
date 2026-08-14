// Package cmd implements the Anvil CLI commands.
//
// ── Truthful Exit-Code Helpers (TS-019-03-02) ────────────────────────
//
// Shared error renderers for the truthful exit-code mapping: every lookup
// of a registered resource (project, release, standard) that comes up
// empty exits with the runtime category (3 — "a runtime resource is
// unavailable or not found", TS-P8-07 / ADR-010 §8.1) instead of the
// general default (1) (audit findings F-02, contract §9.4). Informational
// absence per the D-03 carve-out (status/readiness commands report absent
// resources with 0) is handled by the commands themselves and is not
// reclassified here.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
)

// reportProjectNotFoundError renders a project-not-found lookup failure
// with the runtime exit-code category (3): no project with the given ID
// is registered in the Server Runtime Registry.
func reportProjectNotFoundError(cmd *cobra.Command, projectID string, err error) error {
	return ReportErrorWithCode(cmd, &output.AppError{
		Message:    fmt.Sprintf("project %q not found in the Server Runtime Registry", projectID),
		Reason:     "No project with the given ID is registered",
		Resolution: "Check the project ID, or register it with 'anvil server project register'",
		Err:        err,
	}, output.ExitCodeRuntime)
}

// reportReleaseNotFoundError renders a release-not-found lookup failure
// with the runtime exit-code category (3): no Release with the given
// identity exists for the project.
func reportReleaseNotFoundError(cmd *cobra.Command, projectID, releaseID string, err error) error {
	return ReportErrorWithCode(cmd, &output.AppError{
		Message:    fmt.Sprintf("release %q not found for project %q", releaseID, projectID),
		Reason:     "No Release with the given identity exists for the project",
		Resolution: "Check the release ID, or install a Release with 'anvil server release install'",
		Err:        err,
	}, output.ExitCodeRuntime)
}
