package conformance

import (
	"strings"
	"testing"
)

// brokenActivateRuntime is a deliberately non-conforming runtime: it
// accepts activation of a Release that is not in Ready — violating the
// graph-validated transition rule (lifecycle-model.md §6.2 R2: illegal
// transitions are rejected, not advised against). It is used to prove
// the harness detects non-conformance and reports actionable
// diagnostics; it is not part of the conformance evidence.
type brokenActivateRuntime struct {
	*AnvilRuntime
}

// Activate skips the stage check for non-Ready Releases, accepting the
// illegal Active → Activating transition.
func (b *brokenActivateRuntime) Activate(releaseID string) error {
	stage, err := b.AnvilRuntime.StageOf(releaseID)
	if err != nil {
		return err
	}
	if stage == StageReady {
		return b.AnvilRuntime.Activate(releaseID)
	}
	return nil // illegal transition accepted
}

// brokenVerifyRuntime is a deliberately non-conforming runtime: its
// verification reports success unconditionally — the evidence is never
// recomputed (verification-contract.md §5.1 E1: a claim is not
// evidence; artifact-manifest.md §5.1: the verification operation
// recomputes the hash and compares).
type brokenVerifyRuntime struct {
	*AnvilRuntime
}

// Verify reports every artifact as verified without recomputing the
// content hash.
func (b *brokenVerifyRuntime) Verify(_ string) (VerificationReport, error) {
	return VerificationReport{Passed: true}, nil
}

// panickingRuntime is a runtime whose query surface panics — the
// failure mode a crashing runtime under test exhibits. The harness must
// record the panic as a check failure instead of aborting the report.
type panickingRuntime struct {
	*AnvilRuntime
}

// StageOf panics instead of returning the persisted stage.
func (p *panickingRuntime) StageOf(_ string) (Stage, error) {
	panic("conformance test runtime: query surface crashed")
}

// TestHarnessReportsActionableDiagnostics proves the harness's failure
// diagnostics: when the runtime violates a contract rule, the report
// identifies the contract, the check, the expected behavior (from the
// contract), and the observed deviation (TS-013-05-03 §4 DoD). A
// harness that cannot fail is vacuous; this test pins the failure mode.
func TestHarnessReportsActionableDiagnostics(t *testing.T) {
	t.Run("illegal transition accepted", func(t *testing.T) {
		factory := func() (Runtime, error) {
			base, err := NewAnvilRuntime(t.TempDir())
			if err != nil {
				return nil, err
			}
			return &brokenActivateRuntime{AnvilRuntime: base}, nil
		}
		ws := &tempWorkspace{root: t.TempDir()}

		report := NewHarness(factory).Run(ws)
		if report.Passed() {
			t.Fatal("harness passed against a runtime that accepts illegal transitions — the harness must detect non-conformance")
		}

		found := findFailure(report, "lifecycle-model", "L-03")
		if found == nil {
			t.Fatalf("expected a failure for lifecycle-model L-03 (illegal transitions are rejected); failures: %s", summarize(report))
		}
		assertDiagnostic(t, *found)
	})

	t.Run("verification never fails", func(t *testing.T) {
		factory := func() (Runtime, error) {
			base, err := NewAnvilRuntime(t.TempDir())
			if err != nil {
				return nil, err
			}
			return &brokenVerifyRuntime{AnvilRuntime: base}, nil
		}
		ws := &tempWorkspace{root: t.TempDir()}

		report := NewHarness(factory).Run(ws)
		if report.Passed() {
			t.Fatal("harness passed against a runtime whose verification never fails — the harness must detect non-conformance")
		}

		found := findFailure(report, "artifact-manifest", "A-06")
		if found == nil {
			t.Fatalf("expected a failure for artifact-manifest A-06 (failed verification rejects the operation); failures: %s", summarize(report))
		}
		assertDiagnostic(t, *found)
	})

	t.Run("runtime panic is recorded as a failure", func(t *testing.T) {
		factory := func() (Runtime, error) {
			base, err := NewAnvilRuntime(t.TempDir())
			if err != nil {
				return nil, err
			}
			return &panickingRuntime{AnvilRuntime: base}, nil
		}
		ws := &tempWorkspace{root: t.TempDir()}

		report := NewHarness(factory).Run(ws)
		if report.Passed() {
			t.Fatal("harness passed against a runtime that panics — the panic must be recorded as a check failure")
		}

		found := findFailure(report, "lifecycle-model", "L-01")
		if found == nil {
			t.Fatalf("expected a failure for lifecycle-model L-01 (the panicking check); failures: %s", summarize(report))
		}
		if !strings.Contains(found.Observed, "runtime panicked") {
			t.Errorf("failure observed = %q, want it to record the runtime panic", found.Observed)
		}
	})
}

// assertDiagnostic checks that a failure carries the actionable
// diagnostic triad: contract, expected behavior, observed deviation.
func assertDiagnostic(t *testing.T, failure CheckResult) {
	t.Helper()
	if failure.Contract == "" {
		t.Error("failure diagnostic is missing the contract")
	}
	if failure.Requirement == "" {
		t.Error("failure diagnostic is missing the contract requirement reference")
	}
	if strings.TrimSpace(failure.Expected) == "" {
		t.Error("failure diagnostic is missing the expected behavior (from the contract)")
	}
	if strings.TrimSpace(failure.Observed) == "" {
		t.Error("failure diagnostic is missing the observed deviation")
	}
}

// findFailure returns the first failed check matching the contract and
// check ID, or nil.
func findFailure(report *Report, contract, id string) *CheckResult {
	for _, c := range report.Contracts {
		if c.Name != contract {
			continue
		}
		for _, chk := range c.Checks {
			if chk.ID == id && !chk.Passed {
				out := chk
				return &out
			}
		}
	}
	return nil
}

// summarize renders the failed check IDs of the report for diagnostics.
func summarize(report *Report) string {
	var ids []string
	for _, f := range report.Failures() {
		ids = append(ids, f.Contract+"/"+f.ID)
	}
	return strings.Join(ids, ", ")
}
