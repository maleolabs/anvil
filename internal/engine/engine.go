// Package engine implements the Anvil project initialization engine.
//
// The engine is responsible for creating a new Anvil project configuration
// file at the specified path. It is invoked by the CLI command layer
// (ST-P1-01) and consumed
// by downstream capabilities (TS-001-002 identity, TS-001-004 validation).
//
// Reference: TS-001-001, EPIC-001, ADR-002 §3.3, ADR-005 §7.2
package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/project"
)

// Result describes the outcome of a project initialization attempt.
type Result int

const (
	// ResultCreated indicates a new project was successfully created.
	ResultCreated Result = iota

	// ResultAlreadyExists indicates initialization was attempted in a
	// directory that already contains an Anvil project.
	ResultAlreadyExists
)

// String returns a human-readable description of the result.
func (r Result) String() string {
	switch r {
	case ResultCreated:
		return "project created"
	case ResultAlreadyExists:
		return "project already exists"
	default:
		return "unknown"
	}
}

var (
	// ErrProjectAlreadyExists is returned when the target directory already
	// contains an Anvil project and initialization refuses to overwrite.
	ErrProjectAlreadyExists = errors.New("project already exists in target directory")

	// ErrNameRequired is returned when an empty project name is provided.
	ErrNameRequired = errors.New("project name is required")
)

// Initialize creates a new Anvil project at the specified path.
//
// name is the project name (required, non-empty).
// path is the target directory for the project (must exist or be creatable).
//
// The function is idempotent: if a project already exists at the target path,
// it returns ResultAlreadyExists and ErrProjectAlreadyExists without
// modifying any files.
//
// Initialize creates the project configuration file (anvil.yaml) and
// default pipeline configuration files (.anvil/pipelines/build.yaml and
// .anvil/pipelines/ci.yaml).
// It does not create runtime state (releases, artifacts, execution history,
// or other hidden metadata directories beyond pipelines). A project is a
// configuration unit, not a runtime entity. Runtime directories are created
// on the server during provisioning (see EPIC-005).
func Initialize(name string, path string) (Result, error) {
	if name == "" {
		return ResultCreated, ErrNameRequired
	}

	// Resolve to absolute path for consistent comparisons.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ResultCreated, fmt.Errorf("resolve path %s: %w", path, err)
	}

	// Check for existing project before making any changes.
	s := project.NewStructure(absPath)
	if projectExists(s) {
		return ResultAlreadyExists, ErrProjectAlreadyExists
	}

	// Ensure project root directory exists.
	if err := os.MkdirAll(absPath, 0755); err != nil {
		return ResultCreated, fmt.Errorf("create project root %s: %w", absPath, err)
	}

	// Generate and write configuration file.
	cfg := config.NewProjectConfig(name)
	if err := config.WriteConfig(cfg, s.ConfigFile); err != nil {
		return ResultCreated, fmt.Errorf("write config: %w", err)
	}

	// Write the immutable project identity file (.anvil/project-identity.json).
	// This file is written once during initialization and is checked on every
	// subsequent config load to detect project name changes. It is not intended
	// to be edited manually.
	//
	// Reference: ST-P1-03
	if err := writeIdentityFile(s, name); err != nil {
		return ResultCreated, fmt.Errorf("write identity file: %w", err)
	}

	// Generate default pipeline configuration files.
	if err := generatePipelineConfigs(s); err != nil {
		return ResultCreated, fmt.Errorf("generate pipeline configs: %w", err)
	}

	// Initialize the project lifecycle state to Created.
	// Ensure the state directory exists before writing the lifecycle file.
	if err := os.MkdirAll(s.StateDir, 0755); err != nil {
		return ResultCreated, fmt.Errorf("create state directory: %w", err)
	}
	lifecycle := project.NewStateMachine(project.StageCreated)
	if err := lifecycle.Save(s.LifecycleStateFilePath()); err != nil {
		return ResultCreated, fmt.Errorf("initialize lifecycle state: %w", err)
	}

	return ResultCreated, nil
}

// writeIdentityFile persists the project identity to .anvil/project-identity.json.
// This file establishes the immutable project name that is checked on subsequent
// config loads to detect name changes.
//
// The file contains a simple JSON object with a single "name" field.
//
// Reference: ST-P1-03
func writeIdentityFile(s project.Structure, name string) error {
	identityDir := s.AnvilDir
	if err := os.MkdirAll(identityDir, 0755); err != nil {
		return fmt.Errorf("create identity directory %s: %w", identityDir, err)
	}

	identity := map[string]string{"name": name}
	data, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("marshal identity: %w", err)
	}

	path := s.IdentityFilePath()
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write identity file %s: %w", path, err)
	}

	return nil
}

// generatePipelineConfigs creates default pipeline YAML files in the
// .anvil/pipelines/ directory. If a pipeline file already exists it is
// skipped (non-destructive behaviour).
func generatePipelineConfigs(s project.Structure) error {
	// Create the pipelines directory.
	if err := os.MkdirAll(s.PipelinesDir, 0755); err != nil {
		return fmt.Errorf("create pipelines dir: %w", err)
	}

	// Define the pipeline files to generate with their default definitions.
	type pipelineFile struct {
		name string
		def  execution.PipelineDefinition
	}

	files := []pipelineFile{
		{name: project.PipelineBuildFileName, def: execution.DefaultBuildPipeline()},
		{name: project.PipelineCIFileName, def: execution.DefaultCIPipeline()},
	}

	for _, pf := range files {
		path := filepath.Join(s.PipelinesDir, pf.name)

		// Skip if file already exists (non-destructive).
		if _, err := os.Stat(path); err == nil {
			continue
		}

		data, err := yaml.Marshal(&pf.def)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", pf.name, err)
		}

		if err := os.WriteFile(path, data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", pf.name, err)
		}
	}

	return nil
}

// projectExists checks whether the target directory already contains
// an Anvil project by looking for the anvil.yaml marker file.
func projectExists(s project.Structure) bool {
	_, err := os.Stat(s.ConfigFile)
	return err == nil
}
