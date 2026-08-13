package conformance

import (
	"os"
	"testing"
)

// tempWorkspace provides isolated scratch space for harness fixtures
// under a test-managed root, so go test cleans it up automatically.
type tempWorkspace struct {
	root string
}

// TempDir creates a fresh scratch directory under the test root.
func (w *tempWorkspace) TempDir(prefix string) (string, error) {
	return os.MkdirTemp(w.root, prefix)
}

// TestConformance runs the conformance harness against the Anvil
// Runtime and fails the build when the runtime deviates from the
// published contracts for the declared contract version
// (TS-013-05-03 §4: the harness runs in CI and passes against the
// runtime for the contracts covered; a failure identifies the contract,
// the expected behavior, and the observed deviation).
//
// Run with -v to print the full conformance report — the re-checkable
// evidence of the runtime's conformance (ADR-029 §3).
func TestConformance(t *testing.T) {
	factory := func() (Runtime, error) {
		return NewAnvilRuntime(t.TempDir())
	}
	ws := &tempWorkspace{root: t.TempDir()}

	report := NewHarness(factory).Run(ws)

	t.Logf("\n%s", report.String())

	for _, failure := range report.Failures() {
		t.Errorf("\n%s", failure.Failure())
	}
}
