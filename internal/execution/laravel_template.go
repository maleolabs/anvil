package execution

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// LaravelBuildPipeline returns the default build pipeline definition for
// Laravel projects, as specified by MVP-002 §3.5 (Pipeline Templates).
//
// The pipeline is named "build" and contains three stages, in order:
//   - dependencies: install Composer dependencies without dev packages and
//     generate an optimized autoloader ("composer install --no-dev
//     --optimize-autoloader").
//   - assets: build frontend assets via npm ("npm run build").
//   - optimize: run the Laravel artisan cache commands that prepare the
//     application for production ("config:cache", "route:cache",
//     "view:cache").
//
// The stage and task layout mirrors the shape of the Go build pipeline
// produced by DefaultBuildPipeline/DefaultCIPipeline, so the definition can
// be marshalled into .anvil/pipelines/build.yaml and loaded through the
// standard pipeline loader.
//
// This definition is Core-owned template data (like DefaultBuildPipeline);
// it must NOT import internal/laravel (ADR-009 §8.1). The concrete Laravel
// values are template assets, not adapter logic.
//
// Consumers (both out of scope for TS-P7-19):
//   - TS-P7-28: the pipeline template generator writes this definition to
//     .anvil/pipelines/build.yaml during project initialization.
//   - TS-P7-29: 'anvil init --framework laravel' selects this template.
//
// Reference: TS-P7-19, MVP-002 §3.5
func LaravelBuildPipeline() PipelineDefinition {
	return PipelineDefinition{
		Pipeline: Pipeline{
			Name: "build",
			Stages: []PipelineStage{
				{
					Name: "dependencies",
					Tasks: []Task{
						{
							Name:    "composer-install",
							Command: "composer",
							Args:    []string{"install", "--no-dev", "--optimize-autoloader"},
						},
					},
				},
				{
					Name: "assets",
					Tasks: []Task{
						{
							Name:    "npm-build",
							Command: "npm",
							Args:    []string{"run", "build"},
						},
					},
				},
				{
					Name: "optimize",
					Tasks: []Task{
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
	}
}

// MarshalPipeline serializes a PipelineDefinition to YAML bytes.
//
// It is the production counterpart of the marshaling already used in
// engine.go's generatePipelineConfigs, exposed so template definitions
// (e.g. LaravelBuildPipeline) can be written to .anvil/pipelines/build.yaml
// by the pipeline template generator (TS-P7-28) and 'anvil init --framework'
// (TS-P7-29), both out of scope for TS-P7-19.
//
// Reference: TS-P7-19, MVP-002 §3.5
func MarshalPipeline(def PipelineDefinition) ([]byte, error) {
	data, err := yaml.Marshal(&def)
	if err != nil {
		return nil, fmt.Errorf("marshaling pipeline definition: %w", err)
	}
	return data, nil
}
