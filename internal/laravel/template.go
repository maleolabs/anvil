// Adapter-owned pipeline template of the Laravel adapter (TS-007-038).
//
// The build pipeline definition the adapter owns is returned through the
// `template` command (contracts.CommandTemplate) and written by the Core
// to .anvil/pipelines/build.yaml at generation time, replacing the
// Core-embedded template function execution.LaravelBuildPipeline (ADR-020
// §1: framework knowledge moves OUT of the Core binary INTO the adapter
// binaries).
//
// The definition mirrors the commands the adapter's build pipeline
// executes (internal/laravel/build.go — the single source of build
// knowledge): composer install --no-dev --optimize-autoloader (phase
// composer), npm run build (phase npm), and the artisan optimization
// caches config:cache / route:cache / view:cache (phases config_cache /
// route_cache / view_cache). The stage/task layout keeps the structure of
// the pre-ADR-020 Core template (stages dependencies / assets / optimize)
// so existing projects' build.yaml stays unchanged and the generated YAML
// passes the pipeline loader validation (execution.ParsePipeline).
//
// The CI definition mirrors the Core's generic CI scaffold
// (execution.DefaultCIPipeline — build + test placeholder stages): the CI
// pipeline is generic placeholder data, not framework knowledge, and
// keeping the adapter copy structurally identical preserves the ci.yaml
// output of framework initializations. The Core falls back to its own
// default CI pipeline when the adapter omits the CI definition (old
// adapters, ADR-020 §1).
//
// Reference: TS-007-038, ADR-020 §1, MVP-002 §3.5
package laravel

import (
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
)

// Template returns the pipeline definitions the Laravel adapter owns:
// the build pipeline and the CI scaffold. The Core validates them
// through the pipeline loader and writes them to .anvil/pipelines/ at
// generation time (ADR-020 §1).
//
// Reference: TS-007-038, ADR-020 §1
func Template() contracts.TemplateResult {
	return contracts.TemplateResult{
		Build: &execution.PipelineDefinition{
			Pipeline: execution.Pipeline{
				Name: "build",
				Stages: []execution.PipelineStage{
					{
						Name: "dependencies",
						Tasks: []execution.Task{
							{
								Name:    "composer-install",
								Command: "composer",
								Args:    []string{"install", "--no-dev", "--optimize-autoloader"},
							},
						},
					},
					{
						Name: "assets",
						Tasks: []execution.Task{
							{
								Name:    "npm-build",
								Command: "npm",
								Args:    []string{"run", "build"},
							},
						},
					},
					{
						Name: "optimize",
						Tasks: []execution.Task{
							{
								Name:    "cache-config",
								Command: "php",
								Args:    []string{"artisan", "config:cache"},
							},
							{
								Name:    "cache-route",
								Command: "php",
								Args:    []string{"artisan", "route:cache"},
							},
							{
								Name:    "cache-view",
								Command: "php",
								Args:    []string{"artisan", "view:cache"},
							},
						},
					},
				},
			},
		},
		CI: &execution.PipelineDefinition{
			Pipeline: execution.Pipeline{
				Name: "ci",
				Stages: []execution.PipelineStage{
					{
						Name: "build",
						Tasks: []execution.Task{
							{
								Name:    "build",
								Command: "echo",
								Args:    []string{"building..."},
							},
						},
					},
					{
						Name: "test",
						Tasks: []execution.Task{
							{
								Name:    "unit-tests",
								Command: "echo",
								Args:    []string{"running unit tests..."},
							},
							{
								Name:    "static-analysis",
								Command: "echo",
								Args:    []string{"running static analysis..."},
							},
							{
								Name:    "linting",
								Command: "echo",
								Args:    []string{"running linter..."},
							},
						},
					},
				},
			},
		},
	}
}
