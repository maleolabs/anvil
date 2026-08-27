// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// The structure definitions are the single source of truth for artifact
// layout — all packaging, verification, and lifecycle components reference
// these constants rather than constructing ad-hoc paths.
//
// Reference: TS-P3-02, ADR-004, EPIC-003
package artifact

const (
	// DeployableContentDir is the directory within the artifact containing
	// the application's deployable files. All source files selected by the
	// filtering engine are placed under this directory in the archive.
	DeployableContentDir = "app"

	// ManifestFile is the path to the artifact manifest relative to the
	// artifact root. The manifest contains metadata describing the artifact's
	// contents, identity, and provenance.
	ManifestFile = "manifest.json"
)
