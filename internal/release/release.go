// Package release defines the Release model and persistence for Anvil
// Runtime Releases.
//
// A Release packages a verified artifact together with its target Runtime,
// providing a traceable, immutable record of what was deployed where and
// when. Releases are persisted as JSON files in the project state directory
// and form the backbone of the deployment lifecycle.
//
// Reference: TS-P4-01, EPIC-004, ADR-003 §8.4
package release

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"maleolabs.com/anvil/internal/fsutil"
	"maleolabs.com/anvil/internal/project"
)

// Release represents a Runtime Release — an immutable association between
// a verified artifact and a target Runtime at a point in time.
//
// Every Release is uniquely identified by a ReleaseID and records the
// artifact identity, file system paths, and current stage in the release
// lifecycle. Releases are persisted to disk and loaded on demand.
//
// Reference: TS-P4-01, TS-P4-02, ADR-003 §8.4
type Release struct {
	ID           ReleaseID          `json:"id"`                    // Release identity
	ArtifactID   string             `json:"artifact_id"`           // From manifest
	Version      string             `json:"version"`               // From manifest
	Source       string             `json:"source"`                // Project name (from manifest)
	ArtifactPath string             `json:"artifact_path"`         // Path to artifact file
	RuntimePath  string             `json:"runtime_path"`          // Target runtime (project root)
	Stage        Stage              `json:"stage"`                 // Current lifecycle stage
	CreatedAt    string             `json:"created_at"`            // RFC 3339 timestamp
	Transitions  []TransitionRecord `json:"transitions,omitempty"` // Transition history
}

// Save persists the Release as JSON to the specified path.
// The directory containing the path must already exist.
//
// The write is atomic (temp file + fsync + rename, see fsutil.WriteFileAtomic):
// a crash mid-save leaves either the complete previous release file or the
// complete new one at path — never a truncated or partially-written release
// (TD-002). Release files are the durable source of truth for the release
// lifecycle stage (TS-P4-09), so they must never be observable in a
// partially-written form.
//
// Reference: TS-P4-01 AC-4, TD-002
func (r *Release) Save(path string) error {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal release: %w", err)
	}

	if err := fsutil.WriteFileAtomic(path, data, 0644); err != nil {
		return fmt.Errorf("write release to %s: %w", path, err)
	}

	return nil
}

// SavePath returns the filesystem path where the Release should be persisted
// within the given runtime's state directory.
//
// The path follows the convention: <runtimePath>/.anvil/state/releases/<id>.json
//
// Reference: TS-P4-01, TS-P4-10
func (r *Release) SavePath(runtimePath string) string {
	s := project.NewStructure(runtimePath)
	return filepath.Join(s.StateDir, "releases", r.ID.String()+".json")
}

// Load reads and deserializes a Release from a JSON file at the specified
// path. Returns an error if the file does not exist or cannot be decoded.
//
// Reference: TS-P4-01 AC-5
func Load(path string) (*Release, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("release file not found: %s", path)
		}
		return nil, fmt.Errorf("read release from %s: %w", path, err)
	}

	var r Release
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("unmarshal release: %w", err)
	}

	return &r, nil
}

// LookupByID retrieves a Release by its identity from the runtime's
// releases state directory.
//
// The function constructs the path from project structure and loads
// the Release from its JSON file.
//
// Reference: TS-P4-03 AC-3
func LookupByID(runtimePath string, id ReleaseID) (*Release, error) {
	s := project.NewStructure(runtimePath)
	releasePath := filepath.Join(s.StateDir, "releases", id.String()+".json")
	return Load(releasePath)
}

// Transition attempts to move the Release from its current stage to the
// given target stage. It delegates to StateMachine for transition validation
// and recording. Returns an error if the transition is not allowed.
//
// After a successful transition, both the Stage and Transitions history
// are updated on the Release.
//
// Reference: TS-P4-04, ST-P4-04
func (r *Release) Transition(target Stage) error {
	sm := NewStateMachine(r.Stage)
	sm.history = r.Transitions
	if err := sm.Transition(target); err != nil {
		// Sync history even on failure (failed transition is recorded).
		r.Transitions = sm.history
		return err
	}
	r.Stage = sm.Stage()
	r.Transitions = sm.history
	return nil
}
