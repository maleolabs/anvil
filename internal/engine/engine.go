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
	"maleolabs.com/anvil/internal/registry"
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
	standard  *registry.InstalledStandardRecord
}

// InitOption configures project initialization behavior.
type InitOption func(*initOptions)

// WithFramework selects the application framework declared by the project,
// used for adapter-driven pipeline template generation during
// initialization (e.g. "laravel"). The value is stored in anvil.yaml as a
// user declaration; the Core does not validate or interpret it against a
// built-in whitelist. Framework config keys, defaults, and template content
// come from the installed delivery lifecycle standard (TS-015-03-01,
// ADR-026 decision 1) — resolution and standard-missing semantics are
// standard-driven (TS-015-02-01, TS-015-02-02).
func WithFramework(framework string) InitOption {
	return func(o *initOptions) {
		o.framework = framework
	}
}

// WithFrameworkStandard records the RESOLVED installed delivery lifecycle
// standard for a framework-declared initialization (TS-015-02-01): the
// result of resolving the declared framework name against the
// installed-standard records (registry.ResolveFrameworkStandard). Passing
// the resolution explicitly makes it recorded at the initialization
// boundary — initialization never re-derives or guesses the standard, and
// downstream generation (TS-015-02-03) consumes the same resolved record.
//
// Coherence is validated inside Initialize: the record's id must match
// the standard id of the declared framework (registry.StandardIDForFramework),
// and the option must not be used without a framework declaration. A
// mismatch is a caller defect and fails fast with an actionable error.
func WithFrameworkStandard(standard registry.InstalledStandardRecord) InitOption {
	return func(o *initOptions) {
		o.standard = &standard
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
// Initialize creates the project configuration file (anvil.yaml) and,
// when a framework is declared, pipeline configuration files
// (.anvil/pipelines/build.yaml and .anvil/pipelines/ci.yaml). The pipeline
// content is distribution content supplied by delivery lifecycle
// standards, never engine content (A10, ADR-026 decision 1): with the
// resolved standard (WithFrameworkStandard, TS-015-02-01) the pipeline
// files are generated from the installed standard's template content
// (TS-015-02-03) — validated through the pipeline loader before any
// filesystem work, so a broken standard record fails initialization
// before anything is written. A standard that declares no template
// content hands off to the interim adapter-driven path (ADR-020): the
// installed adapter's template command supplies the definitions; when
// the adapter is unavailable too, no pipeline files are written — the
// Core no longer owns or writes generic pipeline template data
// (TS-015-01-02).
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

	// Coherence of the resolved standard (TS-015-02-01): a resolved
	// standard is only meaningful for a framework declaration, and its id
	// must be the standard id of the declared framework (ADR-021 §3.1).
	// Both are caller defects — resolution happens before Initialize in
	// the CLI layer — and fail fast with actionable errors before any
	// filesystem work.
	if o.framework == "" && o.standard != nil {
		return ResultCreated, fmt.Errorf(
			"a resolved delivery lifecycle standard (%q) was provided without a framework declaration — resolve standards only for framework-declared initialization (TS-015-02-01)",
			o.standard.ID)
	}
	if o.standard != nil {
		want := registry.StandardIDForFramework(o.framework)
		if o.standard.ID != want {
			return ResultCreated, fmt.Errorf(
				"resolved delivery lifecycle standard %q does not match the declared framework %q (expected standard %q) — the resolution must come from the installed-standard records (TS-015-02-01)",
				o.standard.ID, o.framework, want)
		}
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

	// Build the project config before touching the filesystem. The config
	// is built from framework-agnostic compiled defaults (TS-015-01-03):
	// the Core owns no framework config defaults or whitelist (ADR-026
	// decision 1). The declared framework is stored verbatim as a user
	// declaration; framework config keys and defaults are resolved from the
	// installed delivery lifecycle standard (TS-015-03-01, TS-015-02-01).
	cfg := config.NewProjectConfig(name)
	cfg.Project.Framework = o.framework

	// TS-015-03-01: framework config keys and defaults resolve from the
	// installed standard's configuration extension content (ADR-026
	// decision 2) and merge into the project configuration under the
	// framework's own namespace (framework.<name>.<key> = default,
	// ADR-005 §4.4) — never from runtime knowledge. A resolved standard
	// without config extension content is a valid state (a standard may
	// declare nothing in a category, command-contract §4.1): the merge is
	// skipped with an explicit warning — the same hand-off/warning pattern
	// T-004 established for a missing standard; the hard-fail semantics of
	// TS-015-02-02 are not implemented here. A namespace violation inside
	// the record is a real failure, never a silent pass-through.
	if o.standard != nil {
		content, err := o.standard.ConfigExtensionContent(o.framework)
		switch {
		case err == nil:
			cfg.Framework = frameworkConfigDefaults(o.framework, content)
		case errors.Is(err, registry.ErrConfigExtensionMissing):
			warnConfigExtensionMissing(o.framework, o.standard.ID)
		default:
			return ResultCreated, err
		}
	}

	// TS-015-02-03: pipeline template generation is standard-driven —
	// the pipeline files the project receives come from the installed
	// standard's template content, never from runtime knowledge. The
	// standard's content is resolved and validated THROUGH THE PIPELINE
	// LOADER here, before any filesystem work: a broken template in the
	// installed record (content that fails the same loader used at
	// execution time, or a template id the runtime has no pipeline
	// position for) fails initialization with an actionable error and
	// never leaves a partially generated project (no config file, no
	// pipelines directory — the same no-partial-generation property the
	// standard-missing hard-fail of TS-015-02-02 guarantees for the
	// resolution gate). A standard that declares no template content is
	// a valid state (command-contract §4.1): generation hands off to the
	// interim adapter-driven path (ADR-020) with an explicit warning —
	// the same hand-off/warning pattern T-004 established; the standard
	// remains the authoritative source when it supplies content.
	var standardPipelineFiles []pipelineFile
	if o.standard != nil {
		content, err := o.standard.TemplateContent(o.framework)
		switch {
		case err == nil:
			files, verr := pipelineFilesFromStandardContent(o.framework, content)
			if verr != nil {
				return ResultCreated, verr
			}
			standardPipelineFiles = files
		case errors.Is(err, registry.ErrTemplateContentMissing):
			warnTemplateContentMissing(o.framework, o.standard.ID)
		default:
			return ResultCreated, err
		}
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

	// Generate pipeline configuration files: from the installed
	// standard's template content when the resolved standard supplies it
	// (TS-015-02-03), from the installed adapter's template command
	// otherwise (interim path, no-op without a framework declaration —
	// the Core owns no pipeline template content, TS-015-01-02).
	if err := generatePipelineConfigs(s, o.framework, standardPipelineFiles); err != nil {
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

// GenerateFrameworkPipelineConfigs generates the pipeline YAML files
// (.anvil/pipelines/build.yaml and .anvil/pipelines/ci.yaml) for the
// project rooted at root, using the adapter-owned pipeline definitions
// fetched from the framework's adapter binary (ADR-020 §1). Existing
// pipeline files are skipped (non-destructive behaviour, TS-P7-28 AC-1).
// It is the public entry point for callers that need to materialize
// pipeline templates after initialization — e.g. the "anvil adapter use"
// command (TS-007-033) when a project selects a framework whose build
// template is missing. When the adapter is unavailable, no pipeline files
// are generated: the Core owns no generic template data to fall back to
// (TS-015-01-02, ADR-026 decision 1).
//
// Note: this entry point regenerates from the adapter (the interim
// distribution path, ADR-020). Initialization itself generates from the
// installed standard's template content when the resolved standard
// supplies it (TS-015-02-03) and falls back to this adapter path
// otherwise.
//
// Reference: TS-007-038, TS-007-033, ADR-020 §1, TS-P7-28
func GenerateFrameworkPipelineConfigs(root, framework string) error {
	s := project.NewStructure(root)
	return generatePipelineConfigs(s, framework, nil)
}

// fetchAdapterTemplate resolves the framework's adapter executable and
// invokes its `template` command (contracts.CommandTemplate), returning
// the adapter-owned pipeline definitions (ADR-020 §1). It is a
// package-level seam: production resolves anvil-adapter-<framework> via
// exec.LookPath and dispatches through the adapter Coordinator; tests
// replace it to exercise the engine without a real adapter binary on
// PATH. An error means the adapter is missing, does not implement the
// command ("unknown command" exit per 005 §10.2), or failed — the caller
// skips pipeline generation with a warning.
var fetchAdapterTemplate = func(ctx context.Context, framework string) (contracts.TemplateResult, error) {
	executable, err := exec.LookPath("anvil-adapter-" + framework)
	if err != nil {
		return contracts.TemplateResult{}, fmt.Errorf("adapter executable %q not found on PATH: %w", "anvil-adapter-"+framework, err)
	}

	coordinator := adapter.NewCoordinator(execution.NewRunner(), adapter.NewCapabilityRegistry())
	return coordinator.InvokeTemplate(ctx, framework, executable, contracts.TemplateRequest{Framework: framework})
}

// pipelineFile is one pipeline file to generate: the file name within
// the .anvil/pipelines/ directory and the validated content to write.
type pipelineFile struct {
	name string
	data []byte
}

// Pipeline template ids. The installed standard declares its templates
// by stable identifier (command-contract.schema.json templateDeclaration,
// 005 §5.7); the runtime maps the ids of the pipeline positions it owns
// (ADR-007 — the build and CI pipeline positions are Core contract
// knowledge) to the pipeline file names of the project structure. A
// template id outside these positions is undeclared capability from the
// runtime's perspective — rejected, never patched (C7).
const (
	// TemplateIDBuild is the template id of the build pipeline
	// position ("build" → .anvil/pipelines/build.yaml).
	TemplateIDBuild = "build"

	// TemplateIDCI is the template id of the CI pipeline position
	// ("ci" → .anvil/pipelines/ci.yaml).
	TemplateIDCI = "ci"
)

// pipelineFileNameForTemplateID maps a template id declared by the
// installed standard to the pipeline file name the runtime owns:
// "build" → build.yaml and "ci" → ci.yaml (ADR-007 — the pipeline
// positions are Core contract knowledge; command-contract.schema.json
// templateDeclaration ids, 005 §5.7). A template id the runtime has no
// pipeline position for is a record inconsistency: the installed
// standard declares content the runtime cannot generate into any
// pipeline position — an actionable error (reinstall the standard),
// never a silent skip and never an invented position (C7 — a standard
// that violates the contract is rejected, not patched).
func pipelineFileNameForTemplateID(id string) (string, error) {
	switch id {
	case TemplateIDBuild:
		return project.PipelineBuildFileName, nil
	case TemplateIDCI:
		return project.PipelineCIFileName, nil
	}
	return "", fmt.Errorf(
		"installed standard template id %q is not a pipeline position the runtime owns (supported template ids: %s, %s — the build and CI pipeline positions, ADR-007); the installed-standard record is inconsistent with the standard it belongs to; re-install the standard to re-establish the record",
		id, TemplateIDBuild, TemplateIDCI)
}

// pipelineFilesFromStandardContent maps the resolved standard's template
// content to the validated pipeline files initialization writes
// (TS-015-02-03): every declared template maps to the pipeline file of
// its position and is validated THROUGH THE PIPELINE LOADER — the same
// loader used at execution time (ADR-007 — never write unvalidated
// content; the written file must be executable by the generic engine,
// the parity guarantee that keeps lifecycle behavior green). The content
// is written as the standard supplies it: the runtime generates from the
// standard, it does not rewrite the standard's content. A template whose
// content fails validation is a broken installed-standard record — an
// actionable error, never a silent skip and never a runtime fallback
// (C7; the runtime owns no template content to fall back to,
// TS-015-01-02).
func pipelineFilesFromStandardContent(framework string, content registry.TemplateContent) ([]pipelineFile, error) {
	var files []pipelineFile
	for _, tf := range content.Templates {
		name, err := pipelineFileNameForTemplateID(tf.ID)
		if err != nil {
			return nil, err
		}
		data, verr := validateStandardTemplateContent(framework, tf)
		if verr != nil {
			return nil, verr
		}
		files = append(files, pipelineFile{name: name, data: data})
	}
	return files, nil
}

// validateStandardTemplateContent validates one pipeline template file
// of the installed standard's template content through the pipeline
// loader (execution.ParsePipeline) and returns the bytes to write. The
// content is written as the standard supplies it; the validation
// guarantees the written file passes the same loader used at execution
// time (ADR-007 — never write unvalidated content). Invalid content is a
// broken installed-standard record, reported with the standard's id and
// the reinstall remediation.
func validateStandardTemplateContent(framework string, tf registry.TemplateFile) ([]byte, error) {
	data := []byte(tf.Content)
	if _, err := execution.ParsePipeline(data); err != nil {
		return nil, fmt.Errorf(
			"template %q of the installed delivery lifecycle standard for framework %q fails the pipeline loader validation (%v); the installed-standard record is inconsistent with the standard it belongs to; re-install the standard to re-establish the record",
			tf.ID, framework, err)
	}
	return data, nil
}

// generatePipelineConfigs creates pipeline YAML files in the
// .anvil/pipelines/ directory. The content comes from two distribution
// sources, never from the runtime (TS-015-01-02, ADR-026 decision 1):
//
//   - the resolved installed standard's template content (TS-015-02-03),
//     passed pre-validated as standardFiles — the authoritative source
//     when the standard supplies content;
//   - the interim adapter-driven path (ADR-020): the installed adapter's
//     `template` command, validated through the pipeline loader
//     (never write unvalidated adapter output). This path covers
//     standard-missing declarations at the engine boundary (the CLI
//     hard-fails before the engine, TS-015-02-02) and standards that
//     declare no template content.
//
// If a pipeline file already exists it is skipped (non-destructive
// behaviour). The .anvil/pipelines/ directory is created only when at
// least one pipeline file is written. When neither source supplies
// content, no pipeline files are written and no directory is created.
func generatePipelineConfigs(s project.Structure, framework string, standardFiles []pipelineFile) error {
	// Without a framework declaration there is no standard content to
	// generate from: no pipeline files are written.
	if framework == "" {
		return nil
	}

	// Standard-driven generation (TS-015-02-03): the resolved standard's
	// template content is authoritative and already validated — write it.
	if len(standardFiles) > 0 {
		return writePipelineFiles(s, standardFiles)
	}

	// Interim adapter-driven generation (ADR-020): fetch the definitions
	// from the installed adapter's template command.
	result, err := fetchAdapterTemplate(context.Background(), framework)
	if err != nil {
		warnPipelineTemplateSkipped(framework, err)
		return nil
	}

	var files []pipelineFile
	if result.Build != nil {
		if data, verr := validateAdapterDefinition(result.Build); verr != nil {
			warnPipelineTemplateSkipped(framework, verr)
		} else {
			files = append(files, pipelineFile{name: project.PipelineBuildFileName, data: data})
		}
	}
	if result.CI != nil {
		if data, verr := validateAdapterDefinition(result.CI); verr != nil {
			warnPipelineTemplateSkipped(framework, verr)
		} else {
			files = append(files, pipelineFile{name: project.PipelineCIFileName, data: data})
		}
	}

	return writePipelineFiles(s, files)
}

// writePipelineFiles writes the given pipeline files under
// .anvil/pipelines/, skipping files that already exist (non-destructive
// behaviour, TS-P7-28 AC-1). The pipelines directory is created only
// when at least one pipeline file is written.
func writePipelineFiles(s project.Structure, files []pipelineFile) error {
	if len(files) == 0 {
		return nil
	}

	// Create the pipelines directory only when a pipeline file will be
	// written.
	if err := os.MkdirAll(s.PipelinesDir, 0755); err != nil {
		return fmt.Errorf("create pipelines dir: %w", err)
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

// warnPipelineTemplateSkipped prints a warning to stderr in the CLI
// warning format ("Warning: ...") when a pipeline template could not be
// generated from the adapter, directing the user to install the adapter
// and regenerate with 'anvil adapter use <framework>'. The pipeline file
// is skipped — the Core no longer falls back to Core-owned generic
// template data (TS-015-01-02, ADR-026 decision 1). A missing or failing
// adapter never fails project initialization — adapters are optional
// (ADR-009 §9.7).
func warnPipelineTemplateSkipped(framework string, err error) {
	fmt.Fprintf(os.Stderr, "Warning: could not generate pipeline template from adapter for framework %q: %v; no pipeline file was generated. Install the adapter and run 'anvil adapter use %s' to generate it.\n", framework, err, framework)
}

// warnTemplateContentMissing prints a warning to stderr in the CLI
// warning format ("Warning: ...") when the resolved delivery lifecycle
// standard declares no template content (TS-015-02-03): pipeline
// templates cannot be generated from the installed standard, so
// generation hands off to the interim adapter-driven path (ADR-020) —
// the installed adapter's template command supplies the definitions when
// available. The hand-off is explicit, never silent (ADR-026 §4 /
// Manifesto §3.10) and follows the same hand-off/warning pattern T-004
// established for a missing standard and T-007 for missing config
// extension content; the hard-fail semantics of TS-015-02-02 are not
// implemented here (a standard that declares nothing in a category is
// valid, command-contract §4.1).
func warnTemplateContentMissing(framework, standardID string) {
	fmt.Fprintf(os.Stderr, "Warning: the delivery lifecycle standard %s resolved for framework %q declares no template content; pipeline templates will be generated from the installed adapter's template command (interim path — a standard release that supplies template content generates pipeline templates from the standard, TS-015-02-03).\n", standardID, framework)
}

// frameworkConfigDefaults builds the framework configuration extension
// section of the project configuration from the installed standard's
// configuration extension content (TS-015-03-01): every declared key with
// a default becomes framework.<name>.<key> = default — the fully-qualified
// key form under the framework's own namespace (ADR-005 §4.4). Keys
// without a declared default are not written: they are user-provided
// values, and validation of extended values is the standard's own flow
// (TS-015-03-02). Returns nil when no key carries a default — the section
// is omitted from anvil.yaml (omitempty). The values come from the
// installed standard, never from the runtime (ADR-026 decision 1).
func frameworkConfigDefaults(framework string, content registry.ConfigExtensionContent) map[string]map[string]string {
	defaults := make(map[string]string)
	for _, key := range content.Keys {
		if key.Default != "" {
			defaults[key.Name] = key.Default
		}
	}
	if len(defaults) == 0 {
		return nil
	}
	return map[string]map[string]string{framework: defaults}
}

// warnConfigExtensionMissing prints a warning to stderr in the CLI warning
// format ("Warning: ...") when the resolved delivery lifecycle standard
// declares no configuration extension content (TS-015-03-01): framework
// config keys and defaults cannot be merged from the installed standard,
// so the project configuration carries none. The merge is skipped — the
// Core owns no framework config defaults to fall back to (TS-015-01-03,
// ADR-026 decision 1) — and the omission is explicit, never silent
// (ADR-026 §4 / Manifesto §3.10). Missing-extension handling follows the
// same hand-off/warning pattern T-004 established for a missing standard;
// the hard-fail semantics of TS-015-02-02 are not implemented here.
func warnConfigExtensionMissing(framework, standardID string) {
	fmt.Fprintf(os.Stderr, "Warning: the delivery lifecycle standard %s resolved for framework %q declares no configuration extension content; no framework config keys or defaults were merged into the project configuration. The standard may declare nothing in a category; a standard release that supplies configuration extension content resolves framework config defaults (TS-015-03-01).\n", standardID, framework)
}

// projectExists checks whether the target directory already contains
// an Anvil project by looking for the anvil.yaml marker file.
func projectExists(s project.Structure) bool {
	_, err := os.Stat(s.ConfigFile)
	return err == nil
}
