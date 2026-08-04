package cmd

import (
	"github.com/spf13/cobra"
)

// runtimeDeprecationNotice is the cobra deprecation message shared by the
// legacy "runtime" command group and its subcommands.
//
// The "server" command group is the canonical Server Runtime surface
// (ADR-014 migration table, ADR-015 CLI contract, decision 021). The
// "runtime" group is retained for backward compatibility (ST-007-006) and
// will be removed in a future release; deprecation must not change command
// behavior or exit codes.
const runtimeDeprecationNotice = `use "anvil server" commands instead; this group is retained for backward compatibility and will be removed in a future release (see docs/migration-guide-v1.5.md)`

// runtimeCmd represents the "anvil runtime" parent command for managing
// Anvil Runtime instances. It does not perform any action by itself — it
// serves as a namespace for subcommands such as "anvil runtime readiness".
//
// DEPRECATED: the "runtime" group is the legacy Server Runtime surface.
// "anvil server" is the canonical surface (ADR-014, ADR-015).
//
// Reference: ST-P5-02
var runtimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Manage Anvil Runtime instances (deprecated)",
	Long: `Inspect and manage Runtime instances.

DEPRECATED: The "runtime" command group is deprecated. The "server" command
group is the canonical Server Runtime surface (ADR-014, ADR-015). This group
is retained for backward compatibility and will be removed in a future
release — command behavior and exit codes are unchanged.

Migration path (see docs/migration-guide-v1.5.md):
  anvil runtime provision     -> anvil server init
  anvil runtime readiness     -> anvil server readiness (signature differs:
                                 legacy is a zero-argument local filesystem
                                 check; server readiness requires
                                 <project-id> <release-id> and evaluates
                                 pre-activation release readiness, ST-P9-02)
  anvil runtime status        -> anvil server status (signature differs:
                                 legacy takes no arguments; server status
                                 accepts an optional <project-id>)
  anvil runtime list          -> anvil server status (known gap: the legacy
                                 multi-runtime registry has no server-side
                                 equivalent; tracked as follow-up)
  anvil runtime verify-shared -> anvil server doctor

Runtime commands allow operators to observe the condition of Anvil Runtime
environments, check readiness, and manage Runtime metadata.`,
	Deprecated: runtimeDeprecationNotice,
}

func init() {
	rootCmd.AddCommand(runtimeCmd)
}
