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
// The env parameter selects environment-specific configuration overrides
// (e.g., "development", "production").
func (c *BuildCommand) Execute(ctx context.Context, def *PipelineDefinition, env string) *ExecutionReport {
	return c.engine.Execute(ctx, def, env)
}

// LookupBuildDefinition locates and loads .anvil/pipelines/build.yaml from the
// given project root directory. It returns the parsed and validated definition,
// or an error if the file is missing, unreadable, or invalid.
//
// The returned error includes a user-friendly message suggesting to run
// 'anvil init' when the file does not exist.
func LookupBuildDefinition(projectRoot string) (*PipelineDefinition, error) {
	path := filepath.Join(projectRoot, ".anvil", "pipelines", "build.yaml")

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("build pipeline definition not found at %s: run 'anvil init' to generate it", path)
		}
		return nil, fmt.Errorf("checking build pipeline file: %w", err)
	}

	// Use engine.Load for consistent parsing and validation.
	engine := NewPipelineEngine(nil)
	return engine.Load(path)
}
