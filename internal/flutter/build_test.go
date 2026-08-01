// Tests for the Flutter adapter build targets (TS-P7-21): the
// declarative target table (content, order, platform metadata) and the
// build pipeline behavior. A fake command runner is injected in place of
// the production `flutter` execution, so no Flutter toolchain is
// required on the test host.
package flutter

import (
	"context"
	"errors"
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

// buildRequest builds a BuildRequest carrying workingDir.
func buildRequest(workingDir string) contracts.BuildRequest {
	return contracts.BuildRequest{WorkingDir: workingDir}
}

// TestBuildTargets_TableContent verifies the declarative target table
// content: each target's name, flutter command arguments, and platform
// metadata (TS-P7-21 AC-1..AC-4).
func TestBuildTargets_TableContent(t *testing.T) {
	want := []BuildTarget{
		{
			Name:      TargetWeb,
			Args:      []string{"build", "web"},
			Platforms: []string{PlatformLinux, PlatformDarwin, PlatformWindows},
		},
		{
			Name:      TargetApk,
			Args:      []string{"build", "apk", "--release"},
			Platforms: []string{PlatformLinux, PlatformDarwin, PlatformWindows},
		},
		{
			Name:      TargetIos,
			Args:      []string{"build", "ios", "--release"},
			Platforms: []string{PlatformDarwin},
		},
	}
	if !reflect.DeepEqual(buildTargets, want) {
		t.Errorf("buildTargets = %#v, want %#v", buildTargets, want)
	}
}

// TestBuildTargets_WebSupportedEverywhere verifies the web target is
// supported on linux, darwin, and windows (TS-P7-21 AC-1).
func TestBuildTargets_WebSupportedEverywhere(t *testing.T) {
	want := []string{PlatformLinux, PlatformDarwin, PlatformWindows}
	if !reflect.DeepEqual(buildTargets[0].Platforms, want) {
		t.Errorf("web Platforms = %v, want %v", buildTargets[0].Platforms, want)
	}
}

// TestBuildTargets_ApkSupportedEverywhere verifies the apk target is
// supported on linux, darwin, and windows (TS-P7-21 AC-2).
func TestBuildTargets_ApkSupportedEverywhere(t *testing.T) {
	want := []string{PlatformLinux, PlatformDarwin, PlatformWindows}
	if !reflect.DeepEqual(buildTargets[1].Platforms, want) {
		t.Errorf("apk Platforms = %v, want %v", buildTargets[1].Platforms, want)
	}
}

// TestBuildTargets_IosDarwinOnly verifies the ios target is supported on
// darwin (macOS) only — iOS builds require Xcode (TS-P7-21 AC-3,
// ADR-018).
func TestBuildTargets_IosDarwinOnly(t *testing.T) {
	want := []string{PlatformDarwin}
	if !reflect.DeepEqual(buildTargets[2].Platforms, want) {
		t.Errorf("ios Platforms = %v, want %v", buildTargets[2].Platforms, want)
	}
}

// TestBuildTargets_TableOrder verifies the build target table order is
// the documented execution order: web, apk, ios (TS-P7-21).
func TestBuildTargets_TableOrder(t *testing.T) {
	want := []string{TargetWeb, TargetApk, TargetIos}
	got := make([]string, 0, len(buildTargets))
	for _, target := range buildTargets {
		got = append(got, target.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("build target table order = %v, want %v", got, want)
	}
}

// TestRunBuild_AllTargetsSucceedInOrder verifies that the build pipeline
// executes every target exactly once, in table order, with the correct
// flutter arguments per target and the working directory passed through
// (TS-P7-21).
func TestRunBuild_AllTargetsSucceedInOrder(t *testing.T) {
	runner := &fakeRunner{output: "ok"}
	workingDir := "/var/lib/anvil/projects/acme-app/builds/rel-1"

	result := RunBuild(context.Background(), runner.run, buildRequest(workingDir))

	if !result.Success {
		t.Fatalf("Success = false, want true (result: %#v)", result)
	}
	if len(result.Phases) != len(buildTargets) {
		t.Fatalf("Phases length = %d, want %d (result: %#v)", len(result.Phases), len(buildTargets), result)
	}

	wantArgs := [][]string{
		{"build", "web"},
		{"build", "apk", "--release"},
		{"build", "ios", "--release"},
	}
	if len(runner.args) != len(wantArgs) {
		t.Fatalf("runner invoked %d time(s), want %d", len(runner.args), len(wantArgs))
	}
	for i, target := range buildTargets {
		if result.Phases[i].Phase != target.Name {
			t.Errorf("Phases[%d].Phase = %q, want %q", i, result.Phases[i].Phase, target.Name)
		}
		if !result.Phases[i].Success {
			t.Errorf("Phases[%d] (%s) Success = false, want true", i, target.Name)
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
// passed through untouched — the targets run in the adapter's current
// directory.
func TestRunBuild_NoWorkingDir(t *testing.T) {
	runner := &fakeRunner{output: "ok"}

	result := RunBuild(context.Background(), runner.run, buildRequest(""))

	if !result.Success {
		t.Fatalf("Success = false, want true (result: %#v)", result)
	}
	if len(runner.dirs) != len(buildTargets) {
		t.Fatalf("runner invoked %d time(s), want %d", len(runner.dirs), len(buildTargets))
	}
	for i, dir := range runner.dirs {
		if dir != "" {
			t.Errorf("runner working dir[%d] = %q, want empty", i, dir)
		}
	}
}

// TestRunBuild_FailureStopsExecution verifies that a failing target
// stops the pipeline: targets after the failure are not executed, the
// failing target reports Success=false with its output and error details,
// and the build result reports failure (TS-P7-21).
func TestRunBuild_FailureStopsExecution(t *testing.T) {
	var calls [][]string
	runner := func(_ context.Context, _ string, args ...string) (string, error) {
		calls = append(calls, args)
		if reflect.DeepEqual(args, []string{"build", "apk", "--release"}) {
			return "apk output so far", errors.New("flutter build apk --release failed: Gradle task assembleRelease failed")
		}
		return "ok", nil
	}

	result := RunBuild(context.Background(), runner, buildRequest("/var/lib/anvil/projects/acme-app"))

	if result.Success {
		t.Fatal("Success = true, want false")
	}
	if len(calls) != 2 {
		t.Fatalf("runner invoked %d time(s), want 2 — targets after the failure must not run (calls: %v)", len(calls), calls)
	}
	if !reflect.DeepEqual(calls[0], []string{"build", "web"}) {
		t.Errorf("calls[0] = %v, want the web target", calls[0])
	}
	if !reflect.DeepEqual(calls[1], []string{"build", "apk", "--release"}) {
		t.Errorf("calls[1] = %v, want the apk target", calls[1])
	}

	if len(result.Phases) != 2 {
		t.Fatalf("Phases length = %d, want 2 (result: %#v)", len(result.Phases), result)
	}
	if !result.Phases[0].Success {
		t.Errorf("Phases[0] (%s) Success = false, want true", result.Phases[0].Phase)
	}
	if result.Phases[0].Phase != TargetWeb {
		t.Errorf("Phases[0].Phase = %q, want %q", result.Phases[0].Phase, TargetWeb)
	}
	failed := result.Phases[1]
	if failed.Phase != TargetApk {
		t.Errorf("failing phase = %q, want %q", failed.Phase, TargetApk)
	}
	if failed.Success {
		t.Error("failing phase Success = true, want false")
	}
	if failed.Output != "apk output so far" {
		t.Errorf("failing phase Output = %q, want the partial runner output", failed.Output)
	}
	if !strings.Contains(failed.Error, "Gradle task assembleRelease failed") {
		t.Errorf("failing phase Error = %q, want it to carry the failure detail", failed.Error)
	}
}

// TestRunBuild_FirstTargetFailure verifies that a failure in the very
// first target stops the pipeline immediately: only one target runs and
// the result reports the failure with details.
func TestRunBuild_FirstTargetFailure(t *testing.T) {
	var calls [][]string
	runner := func(_ context.Context, _ string, args ...string) (string, error) {
		calls = append(calls, args)
		return "", fmt.Errorf("flutter build web failed: no pubspec.yaml found")
	}

	result := RunBuild(context.Background(), runner, buildRequest("/var/lib/anvil/projects/acme-app"))

	if result.Success {
		t.Fatal("Success = true, want false")
	}
	if len(calls) != 1 {
		t.Fatalf("runner invoked %d time(s), want 1", len(calls))
	}
	if len(result.Phases) != 1 {
		t.Fatalf("Phases length = %d, want 1 (result: %#v)", len(result.Phases), result)
	}
	if result.Phases[0].Phase != TargetWeb {
		t.Errorf("failing phase = %q, want %q", result.Phases[0].Phase, TargetWeb)
	}
	if result.Phases[0].Success {
		t.Error("failing phase Success = true, want false")
	}
	if !strings.Contains(result.Phases[0].Error, "no pubspec.yaml found") {
		t.Errorf("failing phase Error = %q, want it to carry the failure detail", result.Phases[0].Error)
	}
}

// TestRunBuild_NilRunnerUsesProductionRunner verifies that a nil runner
// falls back to the production runner (runFlutter — os/exec): the
// pipeline attempts the real `flutter` executable and reports the
// execution failure through the build contract. The working directory is
// a nonexistent path so the production run fails deterministically
// without a Flutter toolchain on the test host.
func TestRunBuild_NilRunnerUsesProductionRunner(t *testing.T) {
	result := RunBuild(context.Background(), nil, buildRequest("/nonexistent"))
	if result.Success {
		t.Fatalf("Success = true, want false — flutter is not installed in CI (result: %#v)", result)
	}
	if len(result.Phases) != 1 {
		t.Fatalf("Phases length = %d, want 1 — first target must fail and stop", len(result.Phases))
	}
}
