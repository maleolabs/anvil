package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ProvisionResult contains the outcome of a Runtime provisioning operation.
//
// Reference: ST-P5-01
type ProvisionResult struct {
	Metadata  RuntimeMetadata
	Lifecycle *Lifecycle
}

// Provisioner creates and manages Runtime instances.
//
// Reference: ST-P5-01, TS-P5-10
type Provisioner struct {
	config   RuntimeConfig
	registry *RuntimeRegistry // optional — nil means no registry tracking
}

// NewProvisioner creates a Provisioner with the given RuntimeConfig.
// The Provisioner does not use a RuntimeRegistry.
//
// Reference: ST-P5-01
func NewProvisioner(cfg RuntimeConfig) *Provisioner {
	return &Provisioner{config: cfg}
}

// NewProvisionerWithRegistry creates a Provisioner with both a RuntimeConfig
// and a RuntimeRegistry. When a registry is set, Provision() automatically
// registers the newly created Runtime.
//
// Reference: TS-P5-10
func NewProvisionerWithRegistry(cfg RuntimeConfig, registry *RuntimeRegistry) *Provisioner {
	return &Provisioner{
		config:   cfg,
		registry: registry,
	}
}

// Provision creates a new Runtime with a unique identity, metadata,
// directory structure, and initial lifecycle stage (Provisioned).
//
// It persists the Runtime metadata to <installPath>/runtime.json and
// the lifecycle stage to <installPath>/lifecycle.json so that the
// provisioned state survives process restarts.
//
// If the Provisioner has an associated RuntimeRegistry, the new Runtime
// is automatically registered after successful provisioning.
//
// Reference: ST-P5-01 AC-1, AC-2, AC-3, TS-P5-10 AC-1
func (p *Provisioner) Provision(name string, env EnvironmentType, installPath string) (*ProvisionResult, error) {
	if name == "" {
		return nil, fmt.Errorf("runtime name is required")
	}
	if !IsValidEnvironmentType(env) {
		return nil, fmt.Errorf("invalid environment type: %s", env)
	}
	if installPath == "" {
		return nil, fmt.Errorf("install path is required")
	}

	metadata := NewRuntimeMetadata(name, env, installPath)
	lifecycle := NewLifecycle()

	// Create directory structure based on config paths under installPath.
	cfg := p.config
	cfg.InstallRoot = installPath
	for _, dir := range cfg.AllDirs() {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	// Persist Runtime metadata to <installPath>/runtime.json.
	metadataPath := filepath.Join(installPath, "runtime.json")
	metadataData, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal runtime metadata: %w", err)
	}
	if err := os.WriteFile(metadataPath, metadataData, 0644); err != nil {
		return nil, fmt.Errorf("write runtime metadata to %s: %w", metadataPath, err)
	}

	// Persist Lifecycle stage to <installPath>/lifecycle.json.
	lifecyclePath := filepath.Join(installPath, "lifecycle.json")
	if err := lifecycle.Save(lifecyclePath); err != nil {
		return nil, fmt.Errorf("persist lifecycle: %w", err)
	}

	// Register with the registry if one is configured.
	if p.registry != nil {
		entry := RuntimeEntry{
			ID:          metadata.ID,
			Name:        metadata.Name,
			Environment: metadata.Environment,
			InstallPath: metadata.InstallPath,
			Status:      metadata.Status,
		}
		if err := p.registry.Register(entry); err != nil {
			return nil, fmt.Errorf("register runtime in registry: %w", err)
		}
	}

	return &ProvisionResult{
		Metadata:  metadata,
		Lifecycle: lifecycle,
	}, nil
}

// Retire transitions a Runtime's lifecycle to Retired.
// Returns an error if the transition is not allowed by the lifecycle state machine.
//
// Reference: ST-P5-01 AC-4
func (p *Provisioner) Retire(lifecycle *Lifecycle) error {
	return lifecycle.Transition(StageRetired)
}

// ListRuntimes returns all registered Runtimes from the associated registry.
// Returns nil if the Provisioner does not have a registry.
//
// Reference: TS-P5-10
func (p *Provisioner) ListRuntimes() []RuntimeEntry {
	if p.registry == nil {
		return nil
	}
	return p.registry.ListAll()
}
