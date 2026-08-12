package cmd

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/engine"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

var (
	// validProjectName matches names containing only alphanumeric characters,
	// hyphens, and underscores.
	validProjectName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	// ErrInvalidProjectName is returned when the project name contains
	// disallowed characters.
	ErrInvalidProjectName = errors.New("project name contains invalid characters")
)

// initFlags holds parsed flags for the init command.
type initFlags struct {
	path string
}

var initCmd = &cobra.Command{
	Use:   "init <name>",
	Short: "Initialize a new Anvil project",
	Long: `Create a new Anvil project with a valid configuration file
containing sensible defaults and the expected directory structure.

The project name must contain only letters, numbers, hyphens,
and underscores.

Examples:
  anvil init my-project
  anvil init my-project --path /var/www/my-app
  anvil init my-app --framework laravel`,
	Args: cobra.MatchAll(
		MaximumNArgsWithUsage(1, "anvil init my-project"),
		func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] == "" {
				return fmt.Errorf("the %q command requires 1 argument: <name>\nExample: anvil init my-project",
					cmd.CommandPath())
			}
			return nil
		},
	),
	RunE: runInit,
}

func init() {
	initCmd.Flags().String("path", ".", "Target directory for the project")
	initCmd.Flags().String("framework", "", "Framework declaration; resolves to the installed delivery lifecycle standard and hard-fails when it is not installed (e.g. laravel -> anvil-standard-laravel)")
}

func runInit(cmd *cobra.Command, args []string) error {
	name := args[0]
	path, _ := cmd.Flags().GetString("path")
	framework, _ := cmd.Flags().GetString("framework")

	if name == "" {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("the %q command requires 1 argument: <name>", cmd.CommandPath()),
			Resolution: "Provide a project name, e.g.: anvil init my-project",
		})
	}

	if !validProjectName.MatchString(name) {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("invalid project name '%s'", name),
			Reason:     "Project names may only contain letters, numbers, hyphens (-), and underscores (_)",
			Resolution: "Choose a name using only allowed characters, e.g.: anvil init my-project",
		})
	}

	// Framework-declared initialization resolves the declared framework to
	// the installed delivery lifecycle standard (TS-015-02-01, ADR-026
	// decision 2): the resolution reads the installed-standard records
	// (EPIC-014, TS-014-03-03) — never runtime knowledge. The resolved
	// record is passed explicitly into the engine and reported on
	// success. A no-match hard-fails initialization with actionable
	// remediation (TS-015-02-02, ADR-026 decision 3): an explicit
	// framework declaration requires the installed standard, and there is
	// no graceful fallback to a generic lifecycle (§4 — silent
	// degradation hides a missing distribution step). The failure
	// happens before any filesystem work, so no project files are
	// created.
	opts := []engine.InitOption{engine.WithFramework(framework)}
	var resolved *registry.InstalledStandardRecord
	if framework != "" {
		// TS-017-01-02 (T-004): adoption-time installed-adapter
		// recognition and migration (ADR-028 §3, §12.3). When an
		// installed v1.x adapter is recognized for the declared
		// framework, the runtime maps it to the corresponding standard
		// via the authoritative mapping table and records the migration
		// outcome — explicit, never silent (A2). Recognition is
		// additive: it never changes the resolution semantics below
		// (standard-missing hard-fail, ADR-026 decision 3) and never
		// modifies project state. Contract-version validation at
		// migration is TS-017-01-03 (T-007, Wave 3).
		recognizeAndMigrateInstalledAdapterAtAdoption(cmd, cmd.Context(), framework)
		rec, err := resolveInitFrameworkStandard(framework)
		switch {
		case err == nil:
			resolved = &rec
			opts = append(opts, engine.WithFrameworkStandard(rec))
		case errors.Is(err, registry.ErrStandardNotInstalled):
			// Standard-missing hard-fail (TS-015-02-02, ADR-026 decision
			// 3): an explicit framework declaration with no installed
			// standard fails initialization with an actionable message
			// stating what is missing and how to resolve it — never a
			// graceful fallback and never a silent no-match. A framework
			// name reaching this branch has already passed recordIDPattern
			// (Get validates the derived standard id before reading),
			// which incidentally enforces the same name safety the adapter
			// LookPath provides: no path separators or traversal.
			id := registry.StandardIDForFramework(framework)
			return ReportError(cmd, &output.AppError{
				Message: fmt.Sprintf(
					"the delivery lifecycle standard for the declared framework %q (%s) is not installed",
					framework, id),
				Reason: "framework-declared initialization requires the standard recorded in the installed-standard registry; the declaration cannot be resolved without it (ADR-026 decision 3)",
				Resolution: fmt.Sprintf(
					"install the standard with 'anvil standard install %s <version>' (e.g. 'anvil standard install %s 1.0.0'), then re-run 'anvil init %s --framework %s'",
					id, id, name, framework),
				Err: err,
				// Precondition category (TS-019-03-02, D-02): the
				// installed standard is a required prerequisite of the
				// framework-declared initialization.
				ExitCodeValue: output.ExitCodePrecondition,
			})
		default:
			// The store cannot answer: a corrupt record or an unreadable
			// store is a real failure, never a silent no-match. An
			// invalid derived standard id (the framework name cannot form
			// a standard id) is reported with user-facing context — the
			// raw store message is store-internal.
			if errors.Is(err, registry.ErrRecordInvalid) {
				return ReportError(cmd, &output.AppError{
					Message:    fmt.Sprintf("framework name %q is not a valid standard name", framework),
					Reason:     err.Error(),
					Resolution: "Declare a framework name that forms a valid standard id (lowercase letters, digits, and hyphens), e.g. laravel",
					Err:        err,
				})
			}
			return ReportPlainErrorf(cmd, err, "could not resolve the delivery lifecycle standard for framework %q: %v", framework, err)
		}
	}

	result, err := engine.Initialize(name, path, opts...)
	if err != nil {
		if errors.Is(err, engine.ErrProjectAlreadyExists) {
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("project already exists in '%s'", path),
				Reason:     "The target directory already contains an Anvil project configuration",
				Resolution: "Use a different directory or remove the existing project first",
				Err:        err,
			})
		}
		return ReportPlainErrorf(cmd, err, "could not create project: %v", err)
	}

	switch result {
	case engine.ResultCreated:
		if framework != "" {
			// With the standard-missing hard-fail (TS-015-02-02), a
			// framework declaration that reaches this point has always
			// resolved to an installed standard — the resolution claim is
			// explicit and recorded (TS-015-02-01), never a pass-through.
			PrintSuccessf(cmd, "Project '%s' created with '%s' framework (resolved delivery lifecycle standard %s %s). Ready for use.",
				name, framework, resolved.ID, resolved.Version)
		} else {
			PrintSuccessf(cmd, "Project '%s' created. Ready for use.", name)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Next steps:")
		fmt.Fprintf(cmd.OutOrStdout(), "  cd %s && anvil config list\n", path)
	}

	return nil
}

// resolveInitFrameworkStandard resolves the declared framework name
// against the installed-standard record store (TS-015-02-01): the
// standard id follows the identity convention (registry.StandardIDForFramework)
// and the installed-standard record is the resolution result. A no-match
// returns the wrapped registry.ErrStandardNotInstalled hand-off signal
// (TS-015-02-02); store failures (corrupt record, unreadable store) are
// real failures returned as-is.
func resolveInitFrameworkStandard(framework string) (registry.InstalledStandardRecord, error) {
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		return registry.InstalledStandardRecord{}, err
	}
	store := registry.NewInstalledStandardStore(dir)
	return store.ResolveFrameworkStandard(framework)
}
