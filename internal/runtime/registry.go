// Package runtime provides models and utilities for managing Anvil Runtime
// instances — their configuration, lifecycle state machines, readiness
// assessment, runtime identity, runtime state tracking, multi-Runtime
// isolation, and shared resource preservation.
//
// Reference: CH-P5-01, TS-P5-01, TS-P5-02, TS-P5-03, TS-P5-04, TS-P5-05,
// TS-P5-06, TS-P5-07, TS-P5-08, TS-P5-09, TS-P5-10, EPIC-005, ADR-003 §8.5
package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
)

// RuntimeEntry represents a single Runtime in the Registry.
// It contains only the information needed to discover and manage Runtimes.
//
// Reference: TS-P5-10
type RuntimeEntry struct {
	ID          RuntimeID         `json:"id"`
	Name        string            `json:"name"`
	Environment EnvironmentType   `json:"environment"`
	InstallPath string            `json:"install_path"`
	Status      OperationalStatus `json:"status"`
}

// RuntimeRegistry provides centralized tracking of all Runtimes in an Anvil
// installation. It is backed by a JSON file for persistence and is thread-safe.
//
// Reference: TS-P5-10
type RuntimeRegistry struct {
	mu      sync.Mutex
	entries map[RuntimeID]RuntimeEntry
	path    string
}

// NewRuntimeRegistry creates a RuntimeRegistry that persists entries to the
// given file path. The registry starts with an empty entries map.
//
// Reference: TS-P5-10
func NewRuntimeRegistry(path string) *RuntimeRegistry {
	return &RuntimeRegistry{
		entries: make(map[RuntimeID]RuntimeEntry),
		path:    path,
	}
}

// Register adds an entry to the registry. Returns an error if an entry with
// the same ID already exists.
//
// Reference: TS-P5-10 AC-1
func (r *RuntimeRegistry) Register(entry RuntimeEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entries[entry.ID]; exists {
		return fmt.Errorf("runtime %q is already registered", entry.ID)
	}

	r.entries[entry.ID] = entry
	return nil
}

// Unregister removes the entry with the given ID from the registry. Returns
// an error if the ID is not found.
//
// Reference: TS-P5-10 AC-2
func (r *RuntimeRegistry) Unregister(id RuntimeID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entries[id]; !exists {
		return fmt.Errorf("runtime %q is not registered", id)
	}

	delete(r.entries, id)
	return nil
}

// Get returns the entry for the given ID. Returns an error if the ID is not
// found.
//
// Reference: TS-P5-10 AC-3
func (r *RuntimeRegistry) Get(id RuntimeID) (RuntimeEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[id]
	if !exists {
		return RuntimeEntry{}, fmt.Errorf("runtime %q not found", id)
	}

	return entry, nil
}

// ListAll returns all registered entries as a slice sorted by Name.
//
// Reference: TS-P5-10 AC-4
func (r *RuntimeRegistry) ListAll() []RuntimeEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]RuntimeEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		result = append(result, entry)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// Len returns the number of registered entries.
//
// Reference: TS-P5-10
func (r *RuntimeRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

// registryFile is a serializable wrapper for JSON persistence.
type registryFile struct {
	Entries []RuntimeEntry `json:"entries"`
}

// Save persists all registry entries as JSON to the configured file path.
//
// Reference: TS-P5-10 AC-5
func (r *RuntimeRegistry) Save() error {
	r.mu.Lock()
	entries := make([]RuntimeEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		entries = append(entries, entry)
	}
	r.mu.Unlock()

	rf := registryFile{Entries: entries}
	data, err := json.Marshal(rf)
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}

	if err := os.WriteFile(r.path, data, 0644); err != nil {
		return fmt.Errorf("write registry to %s: %w", r.path, err)
	}

	return nil
}

// Load restores registry entries from the configured JSON file path. Returns
// an error if the file does not exist or cannot be decoded.
//
// Reference: TS-P5-10 AC-6
func (r *RuntimeRegistry) Load() error {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("registry file not found: %s", r.path)
		}
		return fmt.Errorf("read registry from %s: %w", r.path, err)
	}

	var rf registryFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return fmt.Errorf("unmarshal registry: %w", err)
	}

	r.mu.Lock()
	r.entries = make(map[RuntimeID]RuntimeEntry, len(rf.Entries))
	for _, entry := range rf.Entries {
		r.entries[entry.ID] = entry
	}
	r.mu.Unlock()

	return nil
}

// DefaultRegistryPath returns the default path for the Runtime registry file:
// $ANVIL_HOME/runtimes.json, where ANVIL_HOME defaults to DefaultInstallRoot.
//
// Reference: TS-P5-10
func DefaultRegistryPath() string {
	return DefaultInstallRoot + "/runtimes.json"
}
