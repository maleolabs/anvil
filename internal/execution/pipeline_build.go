package execution

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// BuildCommand executes the build pipeline defined in .anvil/pipelines/build.yaml.
//
// It wraps a PipelineEngine and provides a focused API for build pipeline
// execution. Designed for both CLI and programmatic (CI command) invocation.
type BuildCommand struct {
	engine *PipelineEngine
}

// NewBuildCommand creates a BuildCommand with the given pipeline engine.
func NewBuildCommand(engine *PipelineEngine) *BuildCommand {
	return &BuildCommand{engine: engine}
}

// Execute runs the provided build pipeline definition and returns the report.
//
// The env parameter selects environment-specific configuration overrides
// (e.g., "development", "production").
//
// The pipelineEnv parameter specifies pipeline-level environment variables
// that are injected into every task's environment. Task-level env vars take
// precedence over pipeline-level vars when keys conflict. Pass nil if no
// pipeline-level environment is needed.
//
// Execute applies the default execution policy (all targets, no strict
// mode); use ExecuteWithOptions for target selection and strict mode.
func (c *BuildCommand) Execute(ctx context.Context, def *PipelineDefinition, env string, pipelineEnv map[string]string) *ExecutionReport {
	return c.ExecuteWithOptions(ctx, def, env, pipelineEnv, ExecuteOptions{})
}

// ExecuteWithOptions runs the provided build pipeline definition with
// per-execution policy options — target selection (--target) and strict
// mode (--strict, ADR-018). See Execute for the base semantics.
//
// Reference: TS-P7-24, ADR-018
func (c *BuildCommand) ExecuteWithOptions(ctx context.Context, def *PipelineDefinition, env string, pipelineEnv map[string]string, opts ExecuteOptions) *ExecutionReport {
	return c.engine.ExecuteWithOptions(ctx, def, env, pipelineEnv, opts)
}

// LookupBuildDefinition locates and loads .anvil/pipelines/build.yaml from the
// given project root directory. It returns the parsed and validated definition,
// or an error if the file is missing, unreadable, or invalid.
//
// The returned error includes a user-friendly message suggesting to generate
// the pipeline from the installed adapter when the file does not exist
// ('anvil init --framework <name>' or 'anvil adapter use <name>'; the Core
// owns no pipeline template content, TS-015-01-02).
func LookupBuildDefinition(projectRoot string) (*PipelineDefinition, error) {
	path := filepath.Join(projectRoot, ".anvil", "pipelines", "build.yaml")

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("build pipeline definition not found at %s: run 'anvil init --framework <name>' or 'anvil adapter use <name>' to generate it", path)
		}
		return nil, fmt.Errorf("checking build pipeline file: %w", err)
	}

	// Use engine.Load for consistent parsing and validation.
	engine := NewPipelineEngine(nil)
	return engine.Load(path)
}
