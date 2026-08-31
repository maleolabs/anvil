// Package project defines the Anvil project directory structure and
// lifecycle stage management.
//
// Reference: TS-P1-07
package project

// LoadLifecycleState reads and returns the current lifecycle stage for
// the project at the given root path. If no state file exists (e.g., for
// projects created before lifecycle tracking was added), it returns
// StageActive as a safe default.
//
// Reference: TS-P1-07, ST-P1-08
func LoadLifecycleState(root string) (Stage, error) {
	s := NewStructure(root)
	sm := NewStateMachine(StageCreated)
	if err := sm.Load(s.LifecycleStateFilePath()); err != nil {
		// If the file doesn't exist, treat as Active (backward-compatible).
		return StageActive, nil
	}
	return sm.Stage(), nil
}
