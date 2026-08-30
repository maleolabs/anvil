// Package project defines the Anvil project directory structure.
//
// All Anvil capabilities reference these well-known paths rather than
// constructing ad-hoc paths. The structure definition is the single source
// of truth for project layout.
//
// Reference: TS-001-008, ADR-002 §3.3, ADR-005 §7
package project

import "path/filepath"

const (
	// ConfigFileName is the name of the project configuration file
	// at the root of every Anvil project.
	ConfigFileName = "anvil.yaml"

	// AnvilDirName is the name of the hidden metadata directory
	// that stores Anvil runtime state and configuration.
	AnvilDirName = ".anvil"

	// StateDirName is the name of the directory inside .anvil/
	// that stores project lifecycle state.
	StateDirName = "state"

	// ReleasesDirName is the name of the directory inside .anvil/
	// that stores versioned release directories.
	ReleasesDirName = "releases"

	// SharedDirName is the name of the directory inside .anvil/
	// that stores shared resources shared across releases.
	SharedDirName = "shared"

	// SharedConfigDirName is the name of the directory inside shared/
	// for environment-specific configuration.
	SharedConfigDirName = "config"

	// SharedStorageDirName is the name of the directory inside shared/
	// for persistent storage (uploads, generated files, etc.).
	SharedStorageDirName = "storage"

	// IdentityFileName is the name of the file inside .anvil/ that stores
	// the immutable project identity (established during initialization).
	//
	// This file is written once during anvil init and checked on every
	// subsequent config load to detect project name changes. It is not
	// intended to be edited manually.
	//
	// Reference: ST-P1-03
	IdentityFileName = "project-identity.json"

	// PipelinesDirName is the name of the directory inside .anvil/
	// that stores pipeline configuration files.
	PipelinesDirName = "pipelines"

	// PipelineBuildFileName is the name of the build pipeline file
	// inside .anvil/pipelines/.
	PipelineBuildFileName = "build.yaml"

	// PipelineCIFileName is the name of the CI pipeline file
	// inside .anvil/pipelines/.
	PipelineCIFileName = "ci.yaml"

	// LifecycleStateFileName is the name of the file inside .anvil/state/
	// that stores the project lifecycle state machine data.
	//
	// Reference: TS-P1-07
	LifecycleStateFileName = "lifecycle.json"
)

// Structure holds the resolved paths for an Anvil project.
type Structure struct {
	// Root is the absolute path to the project root directory.
	Root string

	// ConfigFile is the absolute path to anvil.yaml.
	ConfigFile string

	// AnvilDir is the absolute path to .anvil/.
	AnvilDir string

	// StateDir is the absolute path to .anvil/state/.
	StateDir string

	// ReleasesDir is the absolute path to .anvil/releases/.
	ReleasesDir string

	// SharedDir is the absolute path to .anvil/shared/.
	SharedDir string

	// SharedConfigDir is the absolute path to .anvil/shared/config/.
	SharedConfigDir string

	// SharedStorageDir is the absolute path to .anvil/shared/storage/.
	SharedStorageDir string

	// PipelinesDir is the absolute path to .anvil/pipelines/.
	PipelinesDir string
}

// NewStructure resolves all project paths relative to the given root directory.
// root should be an absolute path.
func NewStructure(root string) Structure {
	return Structure{
		Root:             root,
		ConfigFile:       filepath.Join(root, ConfigFileName),
		AnvilDir:         filepath.Join(root, AnvilDirName),
		StateDir:         filepath.Join(root, AnvilDirName, StateDirName),
		ReleasesDir:      filepath.Join(root, AnvilDirName, ReleasesDirName),
		SharedDir:        filepath.Join(root, AnvilDirName, SharedDirName),
		SharedConfigDir:  filepath.Join(root, AnvilDirName, SharedDirName, SharedConfigDirName),
		SharedStorageDir: filepath.Join(root, AnvilDirName, SharedDirName, SharedStorageDirName),
		PipelinesDir:     filepath.Join(root, AnvilDirName, PipelinesDirName),
	}
}

// IdentityFilePath returns the absolute path to the project identity file,
// which stores the immutable project name set during initialization.
//
// Reference: ST-P1-03
func (s Structure) IdentityFilePath() string {
	return filepath.Join(s.AnvilDir, IdentityFileName)
}

// LifecycleStateFilePath returns the absolute path to the project lifecycle
// state file, which stores the current lifecycle stage and transition history.
//
// Reference: TS-P1-07
func (s Structure) LifecycleStateFilePath() string {
	return filepath.Join(s.StateDir, LifecycleStateFileName)
}

// Dirs returns all directories that should be created during project
// initialization, ordered from parent to child.
func (s Structure) Dirs() []string {
	return []string{
		s.AnvilDir,
		s.StateDir,
		s.ReleasesDir,
		s.SharedDir,
		s.SharedConfigDir,
		s.SharedStorageDir,
	}
}
