package execution

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// CICommand executes the CI pipeline defined in .anvil/pipelines/ci.yaml.
//
// It wraps a PipelineEngine and provides a focused API for CI pipeline
// execution. CI pipelines typically include build and test stages and do
// not use environment-specific overrides.
type CICommand struct {
	engine *PipelineEngine
}

// NewCICommand creates a CICommand with the given pipeline engine.
func NewCICommand(engine *PipelineEngine) *CICommand {
	return &CICommand{engine: engine}
}

// Execute runs the provided CI pipeline definition and returns the report.
// CI pipelines execute without environment-specific overrides or pipeline-level
// environment variables.
func (c *CICommand) Execute(ctx context.Context, def *PipelineDefinition) *ExecutionReport {
	return c.engine.Execute(ctx, def, "", nil)
}

// LookupCIDefinition locates and loads .anvil/pipelines/ci.yaml from the
// given project root directory. It returns the parsed and validated definition,
// or an error if the file is missing, unreadable, or invalid.
//
// The returned error includes a user-friendly message suggesting to run
// 'anvil init' when the file does not exist.
func LookupCIDefinition(projectRoot string) (*PipelineDefinition, error) {
	path := filepath.Join(projectRoot, ".anvil", "pipelines", "ci.yaml")

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("CI pipeline definition not found at %s: run 'anvil init' to generate it", path)
		}
		return nil, fmt.Errorf("checking CI pipeline file: %w", err)
	}

	// Use engine.Load for consistent parsing and validation.
	engine := NewPipelineEngine(nil)
	return engine.Load(path)
}
