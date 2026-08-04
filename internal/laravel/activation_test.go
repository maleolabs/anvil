// Tests for the Laravel adapter activation phases (TS-P7-09) and
// rollback operations (TS-P7-10). A fake command runner is injected in
// place of the production `php artisan` execution, so no PHP is required
// on the test host.
package laravel

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// fakeRunner records the invocations it received and returns the given
// output and error. It implements commandRunner for tests.
type fakeRunner struct {
	dirs   []string
	args   [][]string
	output string
	err    error
}

func (f *fakeRunner) run(_ context.Context, dir string, args ...string) (string, error) {
	f.dirs = append(f.dirs, dir)
	f.args = append(f.args, args)
	return f.output, f.err
}

// request builds an ActivationRequest for the given phase and operation
// with the release context carrying workingDir.
func request(phase string, operation contracts.PhaseOperation, workingDir string) contracts.ActivationRequest {
	return contracts.ActivationRequest{
		Phase:     phase,
		Operation: operation,
		Release: contracts.ReleaseContext{
			ProjectID:  "acme-shop",
			ReleaseID:  "rel-20260801-01",
			WorkingDir: workingDir,
		},
	}
}

// TestRunActivation_ActivateMigrate verifies that the migrate phase runs
// `php artisan migrate --force` in the release working directory and
// reports success with the command output (TS-P7-09 AC-1, AC-5).
func TestRunActivation_ActivateMigrate(t *testing.T) {
	runner := &fakeRunner{output: "Migration table created successfully."}
	workingDir := "/var/www/acme-shop/releases/rel-1"

	result := RunActivation(context.Background(), runner.run, request(PhaseMigrate, contracts.PhaseOperationActivate, workingDir))

	if !result.Success {
		t.Fatalf("Success = false, want true (result: %#v)", result)
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want empty", result.Error)
	}
	if result.Output != "Migration table created successfully." {
		t.Errorf("Output = %q, want the runner output", result.Output)
	}
	if len(runner.args) != 1 {
		t.Fatalf("runner invoked %d time(s), want 1", len(runner.args))
	}
	wantArgs := []string{"migrate", "--force"}
	if !reflect.DeepEqual(runner.args[0], wantArgs) {
		t.Errorf("runner args = %v, want %v", runner.args[0], wantArgs)
	}
	if runner.dirs[0] != workingDir {
		t.Errorf("runner working dir = %q, want %q", runner.dirs[0], workingDir)
	}
}

// TestRunActivation_ActivateAllPhases verifies the artisan command of
// each declared activation phase (TS-P7-09 AC-1..AC-4).
func TestRunActivation_ActivateAllPhases(t *testing.T) {
	tests := []struct {
		phase string
		args  []string
	}{
		{phase: PhaseMigrate, args: []string{"migrate", "--force"}},
		{phase: PhaseConfigCache, args: []string{"config:cache"}},
		{phase: PhaseRouteCache, args: []string{"route:cache"}},
		{phase: PhaseEventCache, args: []string{"event:cache"}},
	}
	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			runner := &fakeRunner{output: "ok"}
			result := RunActivation(context.Background(), runner.run, request(tt.phase, contracts.PhaseOperationActivate, ""))
			if !result.Success {
				t.Fatalf("Success = false, want true (result: %#v)", result)
			}
			if len(runner.args) != 1 || !reflect.DeepEqual(runner.args[0], tt.args) {
				t.Errorf("runner args = %v, want %v", runner.args, tt.args)
			}
		})
	}
}

// TestRunActivation_NoWorkingDir verifies that an empty working directory
// is passed through untouched — the process runs in the adapter's current
// directory.
func TestRunActivation_NoWorkingDir(t *testing.T) {
	runner := &fakeRunner{output: "ok"}
	result := RunActivation(context.Background(), runner.run, request(PhaseMigrate, contracts.PhaseOperationActivate, ""))
	if !result.Success {
		t.Fatalf("Success = false, want true (result: %#v)", result)
	}
	if runner.dirs[0] != "" {
		t.Errorf("runner working dir = %q, want empty", runner.dirs[0])
	}
}

// TestRunActivation_ActivateFailure verifies that a failing artisan
// command reports failure with the error details while preserving any
// partial output (TS-P7-09 AC-6).
func TestRunActivation_ActivateFailure(t *testing.T) {
	runner := &fakeRunner{
		output: "migrations started",
		err:    fmt.Errorf("artisan migrate --force failed: SQLSTATE[42S02]: table users already exists"),
	}

	result := RunActivation(context.Background(), runner.run, request(PhaseMigrate, contracts.PhaseOperationActivate, "/var/www/app"))

	if result.Success {
		t.Fatal("Success = true, want false")
	}
	if result.Output != "migrations started" {
		t.Errorf("Output = %q, want the partial runner output", result.Output)
	}
	if !strings.Contains(result.Error, "table users already exists") {
		t.Errorf("Error = %q, want it to carry the artisan failure detail", result.Error)
	}
}

// TestRunActivation_UnknownPhase verifies that an undeclared phase yields
// a failure result without invoking the runner.
func TestRunActivation_UnknownPhase(t *testing.T) {
	runner := &fakeRunner{}
	result := RunActivation(context.Background(), runner.run, request("unknown_phase", contracts.PhaseOperationActivate, ""))

	if result.Success {
		t.Error("Success = true, want false")
	}
	if !strings.Contains(result.Error, `unknown activation phase "unknown_phase"`) {
		t.Errorf("Error = %q, want mention of the unknown phase", result.Error)
	}
	if len(runner.args) != 0 {
		t.Errorf("runner invoked %d time(s), want 0", len(runner.args))
	}
}

// TestRunActivation_UnknownOperation verifies that an unsupported
// operation value yields a failure result.
func TestRunActivation_UnknownOperation(t *testing.T) {
	runner := &fakeRunner{}
	result := RunActivation(context.Background(), runner.run, request(PhaseMigrate, contracts.PhaseOperation("sideways"), ""))

	if result.Success {
		t.Error("Success = true, want false")
	}
	if !strings.Contains(result.Error, `unknown activation operation "sideways"`) {
		t.Errorf("Error = %q, want mention of the unknown operation", result.Error)
	}
}

// TestRunActivation_RollbackMigrate verifies that the rollback operation
// of the migrate phase runs `php artisan migrate:rollback` in the release
// working directory (TS-P7-10 AC-1).
func TestRunActivation_RollbackMigrate(t *testing.T) {
	runner := &fakeRunner{output: "Rolled back: 2026_07_31_000001"}
	workingDir := "/var/www/acme-shop/releases/rel-0"

	result := RunActivation(context.Background(), runner.run, request(PhaseMigrate, contracts.PhaseOperationRollback, workingDir))

	if !result.Success {
		t.Fatalf("Success = false, want true (result: %#v)", result)
	}
	if len(runner.args) != 1 || !reflect.DeepEqual(runner.args[0], []string{"migrate:rollback"}) {
		t.Errorf("runner args = %v, want [migrate:rollback]", runner.args)
	}
	if runner.dirs[0] != workingDir {
		t.Errorf("runner working dir = %q, want %q", runner.dirs[0], workingDir)
	}
}

// TestRunActivation_RollbackMigrateFailure verifies that a failing
// migrate:rollback reports failure with error details.
func TestRunActivation_RollbackMigrateFailure(t *testing.T) {
	runner := &fakeRunner{err: fmt.Errorf("artisan migrate:rollback failed: connection refused")}
	result := RunActivation(context.Background(), runner.run, request(PhaseMigrate, contracts.PhaseOperationRollback, ""))

	if result.Success {
		t.Fatal("Success = true, want false")
	}
	if !strings.Contains(result.Error, "connection refused") {
		t.Errorf("Error = %q, want it to carry the failure detail", result.Error)
	}
}

// TestRunActivation_RollbackIrreversible verifies that cache phases are
// irreversible: the rollback operation returns an informational success
// that documents the irreversibility and does NOT invoke the runner — the
// rollback is not blocked (TS-P7-10 AC-2).
func TestRunActivation_RollbackIrreversible(t *testing.T) {
	for _, phase := range []string{PhaseConfigCache, PhaseRouteCache, PhaseEventCache} {
		t.Run(phase, func(t *testing.T) {
			runner := &fakeRunner{}
			result := RunActivation(context.Background(), runner.run, request(phase, contracts.PhaseOperationRollback, "/var/www/app"))

			if !result.Success {
				t.Fatalf("Success = false, want true — irreversible phases must not block rollback (result: %#v)", result)
			}
			if !strings.Contains(result.Output, "irreversible") {
				t.Errorf("Output = %q, want it to document the irreversibility", result.Output)
			}
			if len(runner.args) != 0 {
				t.Errorf("runner invoked %d time(s), want 0 — no command may run for an irreversible phase", len(runner.args))
			}
		})
	}
}

// TestCapabilities_DeclaresPhases verifies that the capability declaration
// lists exactly the four activation phases in declaration order — the
// Core invokes only declared phases (TS-P7-09 DoD, TS-P7-10 AC-3).
func TestCapabilities_DeclaresPhases(t *testing.T) {
	result := Capabilities()
	want := []string{PhaseMigrate, PhaseConfigCache, PhaseRouteCache, PhaseEventCache}
	if !reflect.DeepEqual(result.Declaration.ActivationPhases, want) {
		t.Errorf("ActivationPhases = %v, want %v", result.Declaration.ActivationPhases, want)
	}
}

// TestActivation_PhaseTableMatchesDocumentedCommandSet pins the
// executable activation phase table to its documented contract (MVP-002
// §3.2, TS-P7-09): migrate, config:cache, route:cache and event:cache,
// with view:cache excluded because it is not reversible as a rollback
// operation (TS-P7-10). This is the executable surface only — the
// manifest metadata surface (`ActivationCommands`, view:cache form) is
// pinned separately in manifest_commands_test.go; the two surfaces
// deliberately diverge on the cache phase (005-adapter-command-contract
// §10.10, TD-012). The test makes the contract visible so a future
// change to the phase table cannot silently diverge from the document
// again.
func TestActivation_PhaseTableMatchesDocumentedCommandSet(t *testing.T) {
	// The table must contain exactly the four documented activation
	// phases and nothing else.
	names := make([]string, 0, len(phases))
	for _, p := range phases {
		names = append(names, p.name)
	}
	want := []string{PhaseMigrate, PhaseConfigCache, PhaseRouteCache, PhaseEventCache}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("activation phase table = %v, want %v", names, want)
	}

	// view_cache must not resolve to an activation phase — it is a build
	// phase (TS-P7-14), not an activation phase (TD-012).
	if _, ok := lookupPhase(PhaseViewCache); ok {
		t.Error("view_cache found in the activation phase table, want excluded")
	}

	// An activation request for view_cache must fail as an unknown phase
	// without invoking the runner.
	runner := &fakeRunner{}
	result := RunActivation(context.Background(), runner.run, request(PhaseViewCache, contracts.PhaseOperationActivate, ""))
	if result.Success {
		t.Fatal("Success = true, want false — view_cache is not an activation phase")
	}
	if !strings.Contains(result.Error, `unknown activation phase "view_cache"`) {
		t.Errorf("Error = %q, want the unknown-phase failure", result.Error)
	}
	if len(runner.args) != 0 {
		t.Errorf("runner invoked %d time(s), want 0", len(runner.args))
	}
}
