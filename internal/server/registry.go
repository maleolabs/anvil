package server

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	// projectsDirName is the name of the directory within the config root
	// that holds per-project registry YAML files.
	//
	// Reference: TS-P5-12, ADR-013
	projectsDirName = "projects"
)

// ErrProjectAlreadyRegistered is returned when attempting to register a
// project whose ID already exists in the registry.
//
// Reference: TS-P5-12, ADR-013
var ErrProjectAlreadyRegistered = fmt.Errorf("project already registered")

// ErrProjectNotFound is returned when a project registry file does not
// exist for the requested project ID. It lets callers distinguish a
// missing project (runtime error category) from other load failures.
//
// Reference: TS-P5-12, ADR-013, TS-P8-07
var ErrProjectNotFound = fmt.Errorf("project registry not found")

// RegistryStore manages persistence of ProjectRegistry entries to individual
// YAML files on disk, one per project.
//
// Each project is stored at <rootPath>/projects/<project-id>.yaml.
//
// Reference: TS-P5-12, ADR-013
type RegistryStore struct {
	// rootPath is the config root directory (default: /etc/anvil).
	rootPath string

	// projectsDir is the full path to the projects subdirectory.
	projectsDir string
}

// NewRegistryStore creates a RegistryStore that persists project registry
// entries to <rootPath>/projects/.
//
// The root path is used as-is; callers should resolve environment variable
// overrides before calling NewRegistryStore.
func NewRegistryStore(rootPath string) *RegistryStore {
	return &RegistryStore{
		rootPath:    rootPath,
		projectsDir: filepath.Join(rootPath, projectsDirName),
	}
}

// RootPath returns the config root directory path.
func (s *RegistryStore) RootPath() string {
	return s.rootPath
}

// ProjectsDir returns the full path to the projects subdirectory.
func (s *RegistryStore) ProjectsDir() string {
	return s.projectsDir
}

// ProjectPath returns the full path to a project's registry YAML file.
//
// The path is <projectsDir>/<projectID>.yaml.
func (s *RegistryStore) ProjectPath(projectID string) string {
	return filepath.Join(s.projectsDir, projectID+".yaml")
}

// Exists checks whether a project registry file already exists on disk.
func (s *RegistryStore) Exists(projectID string) bool {
	_, err := os.Stat(s.ProjectPath(projectID))
	return err == nil
}

// Register validates a ProjectRegistry, checks uniqueness, and writes it to
// disk as a YAML file.
//
// The project file is created at <projectsDir>/<project.id>.yaml with 0644
// permissions. The projects directory is created if it does not exist.
//
// Returns ErrProjectAlreadyRegistered if a project with the same ID is
// already registered.
//
// Reference: TS-P5-12, ADR-013
func (s *RegistryStore) Register(cfg ProjectRegistry) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	if s.Exists(cfg.Project.ID) {
		return fmt.Errorf("%w: %q", ErrProjectAlreadyRegistered, cfg.Project.ID)
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal project registry to YAML: %w", err)
	}

	if err := os.MkdirAll(s.projectsDir, 0755); err != nil {
		return fmt.Errorf("create projects directory %s: %w", s.projectsDir, err)
	}

	projectPath := s.ProjectPath(cfg.Project.ID)
	if err := os.WriteFile(projectPath, data, 0644); err != nil {
		return fmt.Errorf("write project registry to %s: %w", projectPath, err)
	}

	return nil
}

// List returns all registered project IDs by scanning the projects directory
// for .yaml files. Returns an empty slice if no projects are registered or
// the directory does not exist.
//
// Reference: TS-P5-12, ADR-013
func (s *RegistryStore) List() ([]string, error) {
	entries, err := os.ReadDir(s.projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read projects directory %s: %w", s.projectsDir, err)
	}

	var ids []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".yaml" {
			continue
		}
		ids = append(ids, name[:len(name)-len(".yaml")])
	}
	return ids, nil
}

// Load reads a project registry YAML file and unmarshals it into a
// ProjectRegistry.
//
// Returns an error if the file does not exist, cannot be read, or contains
// invalid YAML.
//
// Reference: TS-P5-12, ADR-013
func (s *RegistryStore) Load(projectID string) (*ProjectRegistry, error) {
	projectPath := s.ProjectPath(projectID)

	data, err := os.ReadFile(projectPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w at %s", ErrProjectNotFound, projectPath)
		}
		return nil, fmt.Errorf("read project registry from %s: %w", projectPath, err)
	}

	var cfg ProjectRegistry
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal project registry from %s: %w", projectPath, err)
	}

	return &cfg, nil
}
