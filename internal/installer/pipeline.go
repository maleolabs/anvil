package installer

import (
	"fmt"
	"path/filepath"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/config"
)

// PipelineConfig for installer build v3.
type PipelineConfig struct {
	SourceDir     string
	OutputDir     string
	Version       string
	Source        string
	ProjectID     string
	ReuseArtifact string // --artifact path, if set verify and skip Package
	Forms         config.InstallerForms
	Include       []string
	Exclude       []string
}

// PipelineResult of installer pipeline.
type PipelineResult struct {
	BundlePath    string
	ArtifactPath  string
	FormsEmbedded bool
	Reused        bool
	Manifest      *artifact.Manifest
}

// Run executes installer pipeline: verify/reuse or package, embed forms.json, template handling.
func Run(cfg PipelineConfig) (*PipelineResult, error) {
	var formsJSON []byte
	var err error
	if cfg.Forms != nil {
		formsJSON, err = config.MarshalFormsJSON(cfg.Forms)
		if err != nil {
			return nil, fmt.Errorf("marshal forms: %w", err)
		}
	}
	opts := artifact.InstallerPayloadOptions{
		SourceDir:     cfg.SourceDir,
		OutputDir:     cfg.OutputDir,
		Version:       cfg.Version,
		Source:        cfg.Source,
		ProjectID:     cfg.ProjectID,
		Include:       cfg.Include,
		Exclude:       cfg.Exclude,
		FormsJSON:     formsJSON,
		ReuseArtifact: cfg.ReuseArtifact,
	}
	res, err := artifact.BuildInstallerPayload(opts)
	if err != nil {
		return nil, err
	}
	return &PipelineResult{
		BundlePath:    res.BundlePath,
		ArtifactPath:  res.ArtifactPath,
		FormsEmbedded: res.FormsEmbedded,
		Reused:        cfg.ReuseArtifact != "",
		Manifest:      res.Manifest,
	}, nil
}

// ResolveSetupEmail resolves superAdmin email templated from forms input with fallback.
func ResolveSetupEmail(templateValue string, formValues map[string]map[string]string, fallback string) string {
	if templateValue == "" {
		if fallback != "" {
			return fallback
		}
		return "admin@example.com"
	}
	resolved := config.ResolveFormsTemplateWithFallback(templateValue, formValues, fallback)
	// if still empty after resolution, fallback
	if resolved == "" && fallback != "" {
		return fallback
	}
	// if resolved still contains placeholder (missing value) and fallback exists, use fallback
	if config.ContainsFormsPlaceholder(resolved) && fallback != "" {
		return fallback
	}
	return resolved
}

// FormsOutputPath helper for bundle naming.
func FormsOutputPath(bundlePath string) string {
	return filepath.Join(filepath.Dir(bundlePath), "forms.json")
}
