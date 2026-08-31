// Package cmd implements the Anvil CLI commands.
//
// ── Standard Group (TS-014-02-02) ────────────────────────────────────
//
// "anvil standard" is the parent-only namespace for registry-driven
// discovery and adoption of delivery lifecycle standards (ADR-032
// vocabulary: adapter → standard; ADR-023 dual-run window; ADR-030
// static index). The discovery subcommands read the static registry
// index — they never modify anything; the install subcommand is the
// explicit adoption flow (TS-014-03-01) and the update subcommand is
// the explicit update flow (TS-014-03-02) — both fetch and verify the
// release content and record the installed version; the install-bundle
// subcommand is the offline adoption flow (TS-014-05-02) — it verifies
// and records the bundled install material with no network access.
// Updates never happen implicitly: the update command is the only
// surface that changes an installed version.
//
// Reference: TS-014-02-02, TS-014-03-01, TS-014-03-02, TS-014-05-02,
// ADR-023, ADR-030, ADR-032
package cmd

import (
	"github.com/spf13/cobra"
)

// standardCmd represents the "anvil standard" parent command group for
// discovering and adopting delivery lifecycle standards from the static
// registry index.
//
// The group is a parent-only namespace (ADR-010 §6.7): it has no RunE,
// Run, or Args — running "anvil standard" displays the group help
// listing the subcommands below.
//
// Reference: TS-014-02-02, TS-014-03-01, TS-014-03-02, TS-014-05-02,
// ADR-010 §6.7, ADR-023, ADR-030, ADR-032
var standardCmd = &cobra.Command{
	Use:   "standard",
	Short: "Discover, install, and update delivery lifecycle standards",
	Long: `Discover, inspect, install, and update delivery lifecycle standards
from the static registry index.

The standard registry is a decentralized, static index of metadata
documents (ADR-030): one document per standard release, laid out as
<index>/<standard-id>/<version>.json. The discovery and inspect
commands are read-only — they never modify the index; install is the
explicit adoption flow (TS-014-03-01) and update is the explicit
update flow (TS-014-03-02): both validate, verify, and record the
installed version — installation and updates never happen implicitly;
install-bundle is the offline adoption flow (TS-014-05-02): it
installs from bundled install material with no network access.

This group was formerly named "adapter" in v1.x; the legacy
"anvil adapter" commands still resolve as deprecated aliases (see
docs/migration-guide-v2.md).

Subcommands:
  list            List standards offered for adoption from the index
  inspect         Inspect a standard's versions and lifecycle state
  install         Install a standard release (explicit adoption)
  install-bundle  Install a standard release from bundled material (offline)
  update          Update an installed standard release (explicit adoption)

Examples:
  anvil standard list
  anvil standard list --json
  anvil standard inspect anvil-standard-laravel
  anvil standard inspect anvil-standard-laravel 1.2.3
  anvil standard install anvil-standard-laravel 1.2.3
  anvil standard install-bundle ./anvil-standard-laravel-1.2.3.bundle.tar.gz
  anvil standard update anvil-standard-laravel 1.3.0`,
}

func init() {
	rootCmd.AddCommand(standardCmd)
	standardCmd.AddCommand(standardListCmd, standardInspectCmd, standardInstallCmd, standardInstallBundleCmd, standardUpdateCmd)
}
