// Package server provides models and utilities for managing Anvil Server
// Runtime configuration — global Runtime metadata persistence, YAML schema
// definition, defaults, and validation, as well as per-project Registry
// metadata.
//
// Reference: TS-P5-11, TS-P5-12, ADR-013, Decision 005, EPIC-005
package server

import (
	"errors"
	"fmt"
	"io"
)

var (
	// ErrProjectIDRequired is returned when project.id is empty.
	//
	// Reference: TS-P5-12, ADR-013
	ErrProjectIDRequired = errors.New("project.id is required and must not be empty")

	// ErrInstallRootRequired is returned when project.install_root is empty.
	//
	// Reference: TS-P5-12, ADR-013
	ErrInstallRootRequired = errors.New("project.install_root is required and must not be empty")

	// ErrStandardAdapterConflict is returned when a project declares both
	// the canonical project.standard key and the legacy project.adapter
	// key. The rename policy (ADR-032) is explicit: the standard
	// declaration is project.standard; declaring both keys is ambiguous
	// and rejected instead of silently preferring one. The error names
	// the canonical key and points at the migration guide — no governance
	// internals in user-facing text.
	//
	// Reference: TS-019-02-01, ADR-032
	ErrStandardAdapterConflict = errors.New("project.standard and the legacy project.adapter are mutually exclusive; declare project.standard only (see docs/migration-guide-v2.md)")

	// StandardAdapterAliasWarning is the deprecation warning emitted when
	// a project declares the legacy project.adapter key (TS-019-02-02).
	// The text names the canonical replacement key (project.standard) and
	// points at the v2 migration guide — no governance internals in
	// user-facing text (T-005 style). Every read of the legacy key emits
	// this warning on stderr; the alias value itself keeps mapping to
	// project.standard semantics during the deprecation window.
	//
	// REMOVAL (end of the deprecation window, ADR-032 §7): the legacy
	// project.adapter key is removed per the announced schedule
	// (docs/migration-guide-v2.md §6; ADR-028 §3 — removal happens only
	// after the window closes and the migration path is exercised). The
	// removal is a governed, explicit change, never silent: the removal
	// condition is the single gate in internal/deprecation (TS-017-04-02 —
	// window closed AND migration-path evidence, T-021). When the gate
	// holds, delete this constant, WarnIfLegacyAdapter, the Adapter field,
	// and every emission point (coordinator + cmd read sites), then flip
	// the window-behavior tests to post-removal expectations (the
	// phantom-target-id removal precedent, TS-019-04-03). Until then the
	// alias must keep working and keep warning.
	//
	// Reference: TS-019-02-02, ADR-032, ADR-028, docs/migration-guide-v2.md
	StandardAdapterAliasWarning = `project.adapter is deprecated; declare project.standard instead (see docs/migration-guide-v2.md)`
)

// ProjectRegistry represents one registered project's declarative metadata.
//
// Each registered project is stored as a separate YAML file at
// <configRoot>/projects/<project-id>.yaml.
//
// Registry ownership semantics (owner/group) and the shared-links model
// (shared_links) were demoted per ADR-031 §3: they are not part of the v2
// runtime, are no longer written by Anvil, and no longer govern runtime
// behavior. Legacy registry files carrying those keys remain readable —
// unknown keys are ignored on load.
//
// Reference: TS-P5-12, ADR-013, ADR-031
type ProjectRegistry struct {
	Project ProjectSection `yaml:"project"`
}

// ProjectSection holds the identity and metadata keys for a registered
// project within a Server Runtime.
type ProjectSection struct {
	// ID is the unique project identifier (required, immutable).
	ID string `yaml:"id"`

	// DisplayName is an optional human-readable name for the project.
	DisplayName string `yaml:"display_name,omitempty"`

	// InstallRoot is the absolute filesystem path where the project is
	// installed (required).
	InstallRoot string `yaml:"install_root"`

	// Standard is the canonical declaration of the delivery lifecycle
	// standard the project uses (project.standard, ADR-032). The project's
	// lifecycle resolves to an installed standard through this key.
	// Optional; when empty the legacy Adapter key is honored during the
	// deprecation window (each read emitting a deprecation warning,
	// TS-019-02-02).
	//
	// Reference: TS-019-02-01, TS-019-02-02, ADR-032
	Standard string `yaml:"standard,omitempty"`

	// Adapter is the legacy declaration of the deployment adapter
	// (project.adapter). It remains readable and honored as an alias
	// during the deprecation window so existing projects keep working;
	// every read emits a deprecation warning naming project.standard
	// (WarnIfLegacyAdapter, TS-019-02-02). Declaring both Adapter and
	// Standard is rejected by ValidateProjectRegistry. Removed at window
	// end as a governed, explicit change (ADR-032 §7).
	//
	// Reference: TS-019-02-01, TS-019-02-02, ADR-032
	Adapter string `yaml:"adapter,omitempty"`
}

// DefaultProjectRegistry returns a ProjectRegistry with compiled-in defaults.
//
// All fields are empty by default; the caller must provide at least ID and
// InstallRoot before the registry is considered valid.
func DefaultProjectRegistry() ProjectRegistry {
	return ProjectRegistry{
		Project: ProjectSection{
			ID:          "",
			DisplayName: "",
			InstallRoot: "",
			Standard:    "",
			Adapter:     "",
		},
	}
}

// StandardName resolves the delivery lifecycle standard a project
// declares, honoring the canonical project.standard key when present and
// falling back to the legacy project.adapter key during the deprecation
// window (ADR-032). The legacy fallback keeps projects that declare
// project.adapter working unchanged; every read of the legacy key emits
// a deprecation warning via WarnIfLegacyAdapter (TS-019-02-02).
//
// When both keys are present the canonical key wins deterministically
// (registration validation rejects the combination, so this only matters
// for hand-edited registry files).
//
// Reference: TS-019-02-01, TS-019-02-02, ADR-032
func (p ProjectSection) StandardName() string {
	if p.Standard != "" {
		return p.Standard
	}
	return p.Adapter
}

// WarnIfLegacyAdapter writes the project.adapter deprecation warning to
// w when the project declares the legacy key — alone or alongside the
// canonical project.standard key (TS-019-02-02). During the deprecation
// window the alias value stays honored and maps to project.standard
// semantics; the warning is the governed notification that the key is
// removed at window end (ADR-032 §7), directing users to the canonical
// key and the v2 migration guide.
//
// Warnings are the caller's channel decision: the CLI passes the
// command's stderr writer (machine-readable stdout stays unpolluted,
// T-003/T-005 precedent); the coordinator defaults to os.Stderr.
//
// It is a no-op for projects declaring only project.standard — the
// canonical path never warns, so the removal does not touch canonical
// users. Removal of this helper and its emission points is a governed
// window-end change (see StandardAdapterAliasWarning).
//
// Reference: TS-019-02-02, ADR-032, ADR-028
func (p ProjectSection) WarnIfLegacyAdapter(w io.Writer) {
	if p.Adapter == "" {
		return
	}
	fmt.Fprintf(w, "Warning: %s\n", StandardAdapterAliasWarning)
}

// ValidateProjectRegistry validates the required fields of a ProjectRegistry.
//
// Validation rules:
//   - project.id must be non-empty (required)
//   - project.install_root must be non-empty (required)
//   - project.display_name is optional
//   - project.standard (canonical) and the legacy project.adapter are
//     mutually exclusive — declaring both is rejected (ADR-032: the
//     rename policy is explicit, never a silent preference)
//   - other fields are optional
//
// Returns nil if the config is valid, or an error describing the first
// validation failure.
//
// Reference: TS-P5-12, ADR-013, TS-019-02-01, ADR-032
func ValidateProjectRegistry(cfg ProjectRegistry) error {
	if cfg.Project.ID == "" {
		return ErrProjectIDRequired
	}
	if cfg.Project.InstallRoot == "" {
		return ErrInstallRootRequired
	}
	if cfg.Project.Standard != "" && cfg.Project.Adapter != "" {
		return ErrStandardAdapterConflict
	}
	return nil
}

// Validate validates the ProjectRegistry against the canonical schema.
// It is a convenience method wrapping ValidateProjectRegistry.
func (c ProjectRegistry) Validate() error {
	return ValidateProjectRegistry(c)
}

// String returns a human-readable summary of the ProjectRegistry.
func (c ProjectRegistry) String() string {
	display := c.Project.DisplayName
	if display == "" {
		display = c.Project.ID
	}
	return fmt.Sprintf("ProjectRegistry{id=%q, display_name=%q, install_root=%q, standard=%q, adapter=%q}",
		c.Project.ID, display, c.Project.InstallRoot, c.Project.Standard, c.Project.Adapter)
}
