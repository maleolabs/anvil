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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"maleolabs.com/anvil/internal/adapter"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/contracts"
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

// initOptions holds optional initialization parameters.
type initOptions struct {
	framework string
}

// InitOption configures project initialization behavior.
type InitOption func(*initOptions)

// WithFramework selects the application framework used for pipeline template
// generation during initialization (e.g. "laravel"). The value is validated
// by config.NewFrameworkProjectConfig (TS-P7-29, TS-P7-28).
func WithFramework(framework string) InitOption {
	return func(o *initOptions) {
		o.framework = framework
	}
}

// Initialize creates a new Anvil project at the specified path.
//
// name is the project name (required, non-empty).
// path is the target directory for the project (must exist or be creatable).
// opts optionally configure initialization (e.g. WithFramework for pipeline
// template selection).
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
func Initialize(name string, path string, opts ...InitOption) (Result, error) {
	if name == "" {
		return ResultCreated, ErrNameRequired
	}

	// Apply optional initialization parameters.
	o := &initOptions{}
	for _, opt := range opts {
		opt(o)
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

	// Build the project config before touching the filesystem so an invalid
	// framework fails before any file or directory is created (TS-P7-28 AC-4).
	cfg, err := config.NewFrameworkProjectConfig(name, o.framework)
	if err != nil {
		return ResultCreated, fmt.Errorf("create project config: %w", err)
	}

	// Ensure project root directory exists.
	if err := os.MkdirAll(absPath, 0755); err != nil {
		return ResultCreated, fmt.Errorf("create project root %s: %w", absPath, err)
	}

	// Generate and write configuration file.
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
	if err := generatePipelineConfigs(s, o.framework); err != nil {
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

// knownFrameworks is the static list of framework names the Core
// recognizes. It replaces the frameworkBuildTemplates map removed in
// TS-007-038: the pipeline templates are no longer Core-owned — they
// moved into the adapter binaries (ADR-020 §1) — so the Core keeps only
// the display list of "known but not installed" frameworks during the
// transition period. PATH-based adapter discovery (TS-007-039) will
// supersede this list; it is kept so the CLI adapter commands
// (cmd/adapter_shared.go) keep enumerating the known frameworks.
var knownFrameworks = []string{"laravel", "flutter"}

// KnownFrameworks returns the sorted list of framework names the Core
// knows about (currently "laravel" and "flutter"). It is the display
// list for the CLI adapter commands during the transition period
// (TS-007-031, TS-007-033) — the "known but not installed" set — and
// is accepted by config.NewFrameworkProjectConfig. PATH-based adapter
// discovery (TS-007-039) supersedes this list; the engine no longer
// selects pipeline templates from it (ADR-020 §1 — templates are
// adapter-owned).
//
// Reference: TS-007-038, ADR-020 §1, TS-007-039
func KnownFrameworks() []string {
	return append([]string(nil), knownFrameworks...)
}

// GenerateFrameworkPipelineConfigs generates the pipeline YAML files
// (.anvil/pipelines/build.yaml and .anvil/pipelines/ci.yaml) for the
// project rooted at root, using the adapter-owned pipeline definitions
// fetched from the framework's adapter binary (ADR-020 §1) when it is
// available. Existing pipeline files are skipped (non-destructive
// behaviour, TS-P7-28 AC-1). It is the public entry point for callers
// that need to materialize pipeline templates after initialization —
// e.g. the "anvil adapter use" command (TS-007-033) when a project
// selects a framework whose build template is missing.
//
// Reference: TS-007-038, TS-007-033, ADR-020 §1, TS-P7-28
func GenerateFrameworkPipelineConfigs(root, framework string) error {
	s := project.NewStructure(root)
	return generatePipelineConfigs(s, framework)
}

// fetchAdapterTemplate resolves the framework's adapter executable and
// invokes its `template` command (contracts.CommandTemplate), returning
// the adapter-owned pipeline definitions (ADR-020 §1). It is a
// package-level seam: production resolves anvil-adapter-<framework> via
// exec.LookPath and dispatches through the adapter Coordinator; tests
// replace it to exercise the engine without a real adapter binary on
// PATH. An error means the adapter is missing, does not implement the
// command ("unknown command" exit per 005 §10.2), or failed — the
// caller falls back to the generic pipelines.
var fetchAdapterTemplate = func(ctx context.Context, framework string) (contracts.TemplateResult, error) {
	executable, err := exec.LookPath("anvil-adapter-" + framework)
	if err != nil {
		return contracts.TemplateResult{}, fmt.Errorf("adapter executable %q not found on PATH: %w", "anvil-adapter-"+framework, err)
	}

	coordinator := adapter.NewCoordinator(execution.NewRunner(), adapter.NewCapabilityRegistry())
	return coordinator.InvokeTemplate(ctx, framework, executable, contracts.TemplateRequest{Framework: framework})
}

// generatePipelineConfigs creates default pipeline YAML files in the
// .anvil/pipelines/ directory. If a pipeline file already exists it is
// skipped (non-destructive behaviour).
//
// When a framework is selected and its adapter is available, build.yaml
// and ci.yaml are generated from the adapter-owned definitions returned
// by the adapter's `template` command, validated through the pipeline
// loader (ADR-020 §1 — never write unvalidated adapter output).
// Otherwise — no framework, adapter missing, template command failed, or
// invalid adapter output — the generic defaults are used
// (execution.DefaultBuildPipeline / DefaultCIPipeline) with a warning on
// stderr directing the user to install the adapter and regenerate
// (ADR-020 §1 fallback; ADR-009 §9.7 — adapters are optional).
func generatePipelineConfigs(s project.Structure, framework string) error {
	// Create the pipelines directory.
	if err := os.MkdirAll(s.PipelinesDir, 0755); err != nil {
		return fmt.Errorf("create pipelines dir: %w", err)
	}

	buildData, ciData, err := defaultPipelineData()
	if err != nil {
		return err
	}

	if framework != "" {
		result, err := fetchAdapterTemplate(context.Background(), framework)
		if err != nil {
			warnPipelineTemplateFallback(framework, err)
		} else {
			if result.Build != nil {
				if data, verr := validateAdapterDefinition(result.Build); verr != nil {
					warnPipelineTemplateFallback(framework, verr)
				} else {
					buildData = data
				}
			}
			if result.CI != nil {
				if data, verr := validateAdapterDefinition(result.CI); verr != nil {
					warnPipelineTemplateFallback(framework, verr)
				} else {
					ciData = data
				}
			}
		}
	}

	// Define the pipeline files to generate with their marshaled content.
	type pipelineFile struct {
		name string
		data []byte
	}

	files := []pipelineFile{
		{name: project.PipelineBuildFileName, data: buildData},
		{name: project.PipelineCIFileName, data: ciData},
	}

	for _, pf := range files {
		path := filepath.Join(s.PipelinesDir, pf.name)

		// Skip if file already exists (non-destructive).
		if _, err := os.Stat(path); err == nil {
			continue
		}

		if err := os.WriteFile(path, pf.data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", pf.name, err)
		}
	}

	return nil
}

// defaultPipelineData marshals the generic default build and CI
// pipelines to YAML bytes. The defaults are Core-owned template data, so
// they are trusted without loader validation (unlike adapter output).
func defaultPipelineData() (buildData, ciData []byte, err error) {
	buildDef := execution.DefaultBuildPipeline()
	buildData, err = yaml.Marshal(&buildDef)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal %s: %w", project.PipelineBuildFileName, err)
	}

	ciDef := execution.DefaultCIPipeline()
	ciData, err = yaml.Marshal(&ciDef)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal %s: %w", project.PipelineCIFileName, err)
	}
	return buildData, ciData, nil
}

// validateAdapterDefinition marshals an adapter-owned pipeline
// definition to YAML and validates the bytes through the pipeline loader
// (execution.ParsePipeline). The returned bytes are what the engine
// writes; validating them guarantees the written file passes the same
// loader used at execution time (ADR-020 §1 — never write unvalidated
// adapter output).
func validateAdapterDefinition(def *execution.PipelineDefinition) ([]byte, error) {
	data, err := yaml.Marshal(def)
	if err != nil {
		return nil, fmt.Errorf("marshaling adapter pipeline definition: %w", err)
	}
	if _, err := execution.ParsePipeline(data); err != nil {
		return nil, fmt.Errorf("adapter pipeline definition failed validation: %w", err)
	}
	return data, nil
}

// warnPipelineTemplateFallback prints the ADR-020 §1 fallback warning to
// stderr in the CLI warning format ("Warning: ..."), directing the user
// to install the adapter and regenerate the pipeline with
// 'anvil adapter use <framework>'. A missing or failing adapter never
// fails project initialization — adapters are optional (ADR-009 §9.7).
func warnPipelineTemplateFallback(framework string, err error) {
	fmt.Fprintf(os.Stderr, "Warning: could not generate pipeline template from adapter for framework %q: %v; generating the generic pipeline instead. Install the adapter and run 'anvil adapter use %s' to regenerate.\n", framework, err, framework)
}

// projectExists checks whether the target directory already contains
// an Anvil project by looking for the anvil.yaml marker file.
func projectExists(s project.Structure) bool {
	_, err := os.Stat(s.ConfigFile)
	return err == nil
}
