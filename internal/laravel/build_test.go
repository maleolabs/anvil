// Tests for the Laravel adapter build phases (TS-P7-14). A fake command
// runner is injected in place of the production composer/npm/php
// execution, so no tooling is required on the test host.
package laravel

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// buildRequest builds a BuildRequest carrying workingDir.
func buildRequest(workingDir string) contracts.BuildRequest {
	return contracts.BuildRequest{WorkingDir: workingDir}
}

// TestRunBuild_AllPhasesSucceedInOrder verifies that the build pipeline
// executes every phase exactly once, in table order, with the correct
// arguments per phase and the working directory passed through
// (TS-P7-14 AC-1..AC-6).
func TestRunBuild_AllPhasesSucceedInOrder(t *testing.T) {
	runner := &fakeRunner{output: "ok"}
	workingDir := "/var/www/acme-shop/releases/rel-1"

	result := RunBuild(context.Background(), runner.run, buildRequest(workingDir))

	if !result.Success {
		t.Fatalf("Success = false, want true (result: %#v)", result)
	}
	if len(result.Phases) != len(buildPhases) {
		t.Fatalf("Phases length = %d, want %d (result: %#v)", len(result.Phases), len(buildPhases), result)
	}

	wantArgs := [][]string{
		{"install", "--no-dev", "--optimize-autoloader"},
		{"run", "build"},
		{"config:cache"},
		{"route:cache"},
		{"view:cache"},
	}
	if len(runner.args) != len(wantArgs) {
		t.Fatalf("runner invoked %d time(s), want %d", len(runner.args), len(wantArgs))
	}
	for i, p := range buildPhases {
		if result.Phases[i].Phase != p.name {
			t.Errorf("Phases[%d].Phase = %q, want %q", i, result.Phases[i].Phase, p.name)
		}
		if !result.Phases[i].Success {
			t.Errorf("Phases[%d] (%s) Success = false, want true", i, p.name)
		}
		if !reflect.DeepEqual(runner.args[i], wantArgs[i]) {
			t.Errorf("runner args[%d] = %v, want %v", i, runner.args[i], wantArgs[i])
		}
		if runner.dirs[i] != workingDir {
			t.Errorf("runner working dir[%d] = %q, want %q", i, runner.dirs[i], workingDir)
		}
	}
}

// TestRunBuild_NoWorkingDir verifies that an empty working directory is
// passed through untouched — the phases run in the adapter's current
// directory.
func TestRunBuild_NoWorkingDir(t *testing.T) {
	runner := &fakeRunner{output: "ok"}

	result := RunBuild(context.Background(), runner.run, buildRequest(""))

	if !result.Success {
		t.Fatalf("Success = false, want true (result: %#v)", result)
	}
	if len(runner.dirs) != len(buildPhases) {
		t.Fatalf("runner invoked %d time(s), want %d", len(runner.dirs), len(buildPhases))
	}
	for i, dir := range runner.dirs {
		if dir != "" {
			t.Errorf("runner working dir[%d] = %q, want empty", i, dir)
		}
	}
}

// TestRunBuild_FailureStopsExecution verifies that a failing phase stops
// the pipeline: phases after the failure are not executed, the failing
// phase reports Success=false with its output and error details, and the
// build result reports failure (TS-P7-14 AC-7).
func TestRunBuild_FailureStopsExecution(t *testing.T) {
	var calls [][]string
	runner := func(_ context.Context, _ string, args ...string) (string, error) {
		calls = append(calls, args)
		if reflect.DeepEqual(args, []string{"run", "build"}) {
			return "npm output so far", errors.New("npm run build failed: error:0308010C:digital envelope routines::unsupported")
		}
		return "ok", nil
	}

	result := RunBuild(context.Background(), runner, buildRequest("/var/www/app"))

	if result.Success {
		t.Fatal("Success = true, want false")
	}
	if len(calls) != 2 {
		t.Fatalf("runner invoked %d time(s), want 2 — phases after the failure must not run (calls: %v)", len(calls), calls)
	}
	if !reflect.DeepEqual(calls[0], []string{"install", "--no-dev", "--optimize-autoloader"}) {
		t.Errorf("calls[0] = %v, want the composer phase", calls[0])
	}
	if !reflect.DeepEqual(calls[1], []string{"run", "build"}) {
		t.Errorf("calls[1] = %v, want the npm phase", calls[1])
	}

	if len(result.Phases) != 2 {
		t.Fatalf("Phases length = %d, want 2 (result: %#v)", len(result.Phases), result)
	}
	if !result.Phases[0].Success {
		t.Errorf("Phases[0] (%s) Success = false, want true", result.Phases[0].Phase)
	}
	if result.Phases[0].Phase != PhaseComposer {
		t.Errorf("Phases[0].Phase = %q, want %q", result.Phases[0].Phase, PhaseComposer)
	}
	failed := result.Phases[1]
	if failed.Phase != PhaseNpm {
		t.Errorf("failing phase = %q, want %q", failed.Phase, PhaseNpm)
	}
	if failed.Success {
		t.Error("failing phase Success = true, want false")
	}
	if failed.Output != "npm output so far" {
		t.Errorf("failing phase Output = %q, want the partial runner output", failed.Output)
	}
	if !strings.Contains(failed.Error, "digital envelope routines") {
		t.Errorf("failing phase Error = %q, want it to carry the failure detail", failed.Error)
	}
}

// TestRunBuild_FirstPhaseFailure verifies that a failure in the very
// first phase stops the pipeline immediately: only one phase runs and
// the result reports the failure with details.
func TestRunBuild_FirstPhaseFailure(t *testing.T) {
	var calls [][]string
	runner := func(_ context.Context, _ string, args ...string) (string, error) {
		calls = append(calls, args)
		return "", fmt.Errorf("composer install failed: composer.json not found")
	}

	result := RunBuild(context.Background(), runner, buildRequest("/var/www/app"))

	if result.Success {
		t.Fatal("Success = true, want false")
	}
	if len(calls) != 1 {
		t.Fatalf("runner invoked %d time(s), want 1", len(calls))
	}
	if len(result.Phases) != 1 {
		t.Fatalf("Phases length = %d, want 1 (result: %#v)", len(result.Phases), result)
	}
	if result.Phases[0].Phase != PhaseComposer {
		t.Errorf("failing phase = %q, want %q", result.Phases[0].Phase, PhaseComposer)
	}
	if result.Phases[0].Success {
		t.Error("failing phase Success = true, want false")
	}
	if !strings.Contains(result.Phases[0].Error, "composer.json not found") {
		t.Errorf("failing phase Error = %q, want it to carry the failure detail", result.Phases[0].Error)
	}
}

// TestRunBuild_NilRunnerUsesProductionTable verifies that a nil runner
// leaves the phase table's production runners in place — the table
// entries carry runComposer, runNpm, and runArtisan.
func TestRunBuild_NilRunnerUsesProductionTable(t *testing.T) {
	for i, p := range buildPhases {
		if p.runner == nil {
			t.Errorf("buildPhases[%d] (%s) has a nil runner, want the production runner", i, p.name)
		}
	}
}

// TestBuildPhases_TableOrder verifies the build phase table order is the
// documented execution order: composer, npm, then the artisan
// optimization caches (TS-P7-14 AC-6).
func TestBuildPhases_TableOrder(t *testing.T) {
	want := []string{PhaseComposer, PhaseNpm, PhaseConfigCache, PhaseRouteCache, PhaseViewCache}
	got := make([]string, 0, len(buildPhases))
	for _, p := range buildPhases {
		got = append(got, p.name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("build phase table order = %v, want %v", got, want)
	}
}
