// Package project defines the Anvil project lifecycle stage management.
//
// Reference: TS-P1-07
package project

import (
	"fmt"
	"os"
)

// ErrProjectNotRemoved is a sentinel error returned when a project removal
// operation fails partway through cleanup. The error message includes which
// cleanup step failed.
type ErrProjectNotRemoved struct {
	Msg string
}

func (e *ErrProjectNotRemoved) Error() string {
	return e.Msg
}

// RemoveProject removes the Anvil project at the given root directory.
// It performs the following steps in order:
//  1. Transitions the lifecycle state to Removed (if state machine is operational)
//  2. Removes the project directory and all its contents
//
// If the project does not exist (no anvil.yaml), RemoveProject returns nil
// (idempotent — already removed or never existed).
//
// Reference: ST-P1-07
func RemoveProject(root string) error {
	s := NewStructure(root)

	// Check if project exists.
	if _, err := os.Stat(s.ConfigFile); os.IsNotExist(err) {
		// Project already doesn't exist — idempotent.
		return nil
	}

	// Transition lifecycle to Removed.
	sm := NewStateMachine(StageCreated)
	if err := sm.Load(s.LifecycleStateFilePath()); err == nil {
		// Only transition if we could load the state machine.
		// If no state file exists, we still proceed with removal.
		_ = sm.Transition(StageRemoved) // Transition is best-effort here
		_ = sm.Save(s.LifecycleStateFilePath())
	}

	// Remove entire project directory.
	if err := os.RemoveAll(root); err != nil {
		return &ErrProjectNotRemoved{
			Msg: fmt.Sprintf("removing project directory %s: %v", root, err),
		}
	}

	return nil
}
