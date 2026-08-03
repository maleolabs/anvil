// Package config provides configuration schema defaults and generation
// for Anvil project initialization.
//
// Reference: CH-P1-01, ADR-005, EPIC-001
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ProjectConfig represents the minimal configuration required for a valid
// Anvil project immediately after initialization.
//
// This schema is the minimal set defined by CH-P1-01. The full canonical
// schema (owned by EPIC-002) extends this with keys across all capabilities.
type ProjectConfig struct {
	Project  ProjectConfigSection  `yaml:"project"`
	Artifact ArtifactConfigSection `yaml:"artifact"`
}

// ProjectConfigSection holds project identity and metadata keys.
type ProjectConfigSection struct {
	// Name is the unique project identity (required, user-provided).
	Name string `yaml:"name"`

	// Version is the project version identifier (optional, default "1.0.0").
	Version string `yaml:"version"`

	// Description is an optional human-readable description of the project.
	Description string `yaml:"description"`

	// Framework is the application framework used by the project (e.g. "laravel").
	// It is stored in anvil.yaml (TS-P7-29 AC-4) and drives pipeline template
	// generation during initialization. Empty means no framework was specified.
	Framework string `yaml:"framework,omitempty"`
}

// ArtifactConfigSection holds artifact packaging file filter configuration.
// Include and Exclude are initialized as empty slices so users can directly
// edit them in anvil.yaml without needing to know the schema keys.
type ArtifactConfigSection struct {
	// Include specifies glob patterns for files to include in the artifact.
	// Empty by default — all non-excluded files are included.
	Include []string `yaml:"include"`

	// Exclude specifies glob patterns for files to exclude from the artifact.
	// Empty by default — no files are excluded beyond the compiled defaults.
	Exclude []string `yaml:"exclude"`
}

// DefaultVersion is the compiled default version assigned to a new project
// when the user does not specify one.
const DefaultVersion = "1.0.0"

// DefaultDescription is the compiled default description for a new project.
const DefaultDescription = ""

// NewProjectConfig creates a ProjectConfig with the given project name and
// sensible defaults for all optional keys.
func NewProjectConfig(name string) ProjectConfig {
	return ProjectConfig{
		Project: ProjectConfigSection{
			Name:        name,
			Version:     DefaultVersion,
			Description: DefaultDescription,
		},
		Artifact: ArtifactConfigSection{
			Include: []string{},
			Exclude: []string{},
		},
	}
}

// NewFrameworkProjectConfig creates a ProjectConfig for the given project
// name and framework, applying framework-specific defaults (TS-P7-29).
//
// Framework validation lives in this single constructor so both the engine
// and the CLI benefit from one consistent validation point. Accepted values:
//   - "": plain config (no framework), no error.
//   - "laravel": sets Project.Framework and replaces the compiled default
//     artifact exclude list with the same list minus "vendor/**".
//     Artifact.Include stays empty. An include override is NOT used to keep
//     vendor/ in the artifact: the artifact filter (internal/artifact
//     FilterFiles) treats a non-empty include list as a strict whitelist, so
//     "include: [vendor/**]" would drop every non-vendor file (artisan,
//     app/, config/, routes/, public/, ...) from the artifact. Removing
//     "vendor/**" from the exclude list instead keeps vendor/ in the
//     artifact — runtime-critical for Laravel (ADR-017) — while all other
//     non-excluded files flow in as usual.
//   - "flutter": sets Project.Framework. The artifact include/exclude lists
//     stay at their compiled defaults — Flutter build outputs are produced
//     by the pipeline and the default artifact config already keeps all
//     non-excluded files (TS-P7-27).
//
// Any other value is unknown and rejected.
func NewFrameworkProjectConfig(name, framework string) (ProjectConfig, error) {
	cfg := NewProjectConfig(name)

	switch framework {
	case "":
		return cfg, nil
	case "laravel":
		cfg.Project.Framework = framework
		cfg.Artifact.Exclude = laravelArtifactExcludes()
		return cfg, nil
	case "flutter":
		cfg.Project.Framework = framework
		return cfg, nil
	default:
		return ProjectConfig{}, fmt.Errorf("unknown framework %q", framework)
	}
}

// laravelArtifactExcludes returns the compiled default artifact exclude
// patterns from the canonical schema (key "artifact.exclude") with
// "vendor/**" removed.
//
// The schema default excludes "vendor/**" for generic projects, but vendor/
// is runtime-critical for Laravel (ADR-017): the Composer autoloader and
// all installed packages live there, so every packaged Laravel artifact
// must contain it. Laravel therefore keeps every other compiled exclusion
// and drops only "vendor/**".
//
// The result preserves the schema definition order and contains no
// duplicates. The schema always registers "artifact.exclude" (CoreSchema),
// so the empty fallback is defensive only.
func laravelArtifactExcludes() []string {
	entry, ok := GetSchema().Entries["artifact.exclude"]
	if !ok {
		// Defensive fallback: CoreSchema always registers this key. An empty
		// list keeps the plain default behavior (nothing excluded beyond the
		// compiled defaults).
		return []string{}
	}

	// The schema default is a []string, but handle []interface{} as well so
	// the conversion survives future schema representation changes.
	var patterns []string
	switch v := entry.Default.(type) {
	case []string:
		patterns = v
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				patterns = append(patterns, s)
			}
		}
	}

	excludes := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if p == "vendor/**" {
			continue
		}
		excludes = append(excludes, p)
	}
	return excludes
}

// WriteConfig writes the project configuration as YAML to the specified path.
// The directory containing the path must already exist.
func WriteConfig(cfg ProjectConfig, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config to YAML: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config file %s: %w", path, err)
	}

	return nil
}
