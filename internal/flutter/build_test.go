// Tests for the Flutter adapter build targets (TS-P7-21, TS-007-041):
// the declarative target table (content, order, platform metadata) and
// the build pipeline behavior — target selection, platform-aware skip /
// strict handling (ADR-018). A fake command runner is injected in place
// of the production `flutter` execution, so no Flutter toolchain is
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

// withPlatform sets the platformDetector seam for the test and restores
// it on cleanup, so platform-aware behavior is deterministic on any
// host (TS-007-041).
func withPlatform(t *testing.T, platform string) {
	t.Helper()
	orig := platformDetector
	platformDetector = func() string { return platform }
	t.Cleanup(func() { platformDetector = orig })
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
// (TS-P7-21). The platform seam is set to darwin so all three targets
// are supported and run (TS-007-041).
func TestRunBuild_AllTargetsSucceedInOrder(t *testing.T) {
	withPlatform(t, PlatformDarwin)
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
// directory. The platform seam is set to darwin so all three targets run.
func TestRunBuild_NoWorkingDir(t *testing.T) {
	withPlatform(t, PlatformDarwin)
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
// and the build result reports failure (TS-P7-21). The platform seam is
// set to darwin so platform filtering does not interfere with the
// fail-fast semantics.
func TestRunBuild_FailureStopsExecution(t *testing.T) {
	withPlatform(t, PlatformDarwin)
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
// without a Flutter toolchain on the test host. The web target is
// supported on every platform, so the first target always runs.
func TestRunBuild_NilRunnerUsesProductionRunner(t *testing.T) {
	result := RunBuild(context.Background(), nil, buildRequest("/nonexistent"))
	if result.Success {
		t.Fatalf("Success = true, want false — flutter is not installed in CI (result: %#v)", result)
	}
	if len(result.Phases) != 1 {
		t.Fatalf("Phases length = %d, want 1 — first target must fail and stop", len(result.Phases))
	}
}

// TestRunBuild_TargetSelection runs only the requested targets, in table
// order (TS-007-041, ADR-018 --target parity).
func TestRunBuild_TargetSelection(t *testing.T) {
	withPlatform(t, PlatformDarwin)

	t.Run("single_target", func(t *testing.T) {
		runner := &fakeRunner{output: "ok"}
		result := RunBuild(context.Background(), runner.run, contracts.BuildRequest{
			WorkingDir: "/var/lib/anvil/projects/acme-app",
			Targets:    []string{TargetApk},
		})

		if !result.Success {
			t.Fatalf("Success = false, want true (result: %#v)", result)
		}
		if len(result.Phases) != 1 || result.Phases[0].Phase != TargetApk {
			t.Fatalf("Phases = %#v, want only the apk target", result.Phases)
		}
		if len(runner.args) != 1 || !reflect.DeepEqual(runner.args[0], []string{"build", "apk", "--release"}) {
			t.Errorf("runner args = %v, want only the apk invocation", runner.args)
		}
	})

	t.Run("subset_preserves_table_order", func(t *testing.T) {
		runner := &fakeRunner{output: "ok"}
		result := RunBuild(context.Background(), runner.run, contracts.BuildRequest{
			Targets: []string{TargetIos, TargetWeb},
		})

		if !result.Success {
			t.Fatalf("Success = false, want true (result: %#v)", result)
		}
		var got []string
		for _, p := range result.Phases {
			got = append(got, p.Phase)
		}
		// Table order wins over request order: web, then ios.
		if !reflect.DeepEqual(got, []string{TargetWeb, TargetIos}) {
			t.Errorf("Phases = %v, want [web ios] in table order", got)
		}
		if len(runner.args) != 2 {
			t.Fatalf("runner invoked %d time(s), want 2", len(runner.args))
		}
	})

	t.Run("unknown_target_is_noop", func(t *testing.T) {
		runner := &fakeRunner{output: "ok"}
		result := RunBuild(context.Background(), runner.run, contracts.BuildRequest{
			Targets: []string{"windows"},
		})

		// No target selected: a graceful no-op build (ADR-009 §9.7).
		if !result.Success {
			t.Fatalf("Success = false, want true (result: %#v)", result)
		}
		if len(result.Phases) != 0 {
			t.Errorf("Phases = %#v, want none for an unknown target", result.Phases)
		}
		if len(runner.args) != 0 {
			t.Errorf("runner invoked %d time(s), want 0", len(runner.args))
		}
	})
}

// TestRunBuild_SkipUnsupportedWithWarning verifies platform filtering:
// on linux, the ios target (darwin only) is skipped with Skipped=true
// and a Warning, the skip does not fail the build, and the pipeline
// continues (TS-007-041, ADR-018 — graceful degradation).
func TestRunBuild_SkipUnsupportedWithWarning(t *testing.T) {
	withPlatform(t, PlatformLinux)
	runner := &fakeRunner{output: "ok"}

	result := RunBuild(context.Background(), runner.run, buildRequest("/var/lib/anvil/projects/acme-app"))

	if !result.Success {
		t.Fatalf("Success = false, want true — a graceful skip must not fail the build (result: %#v)", result)
	}
	if len(result.Phases) != 3 {
		t.Fatalf("Phases length = %d, want 3 (web runs, apk runs, ios skipped)", len(result.Phases))
	}

	web, apk, ios := result.Phases[0], result.Phases[1], result.Phases[2]
	if web.Phase != TargetWeb || !web.Success || web.Skipped {
		t.Errorf("web phase = %#v, want an executed successful phase", web)
	}
	if apk.Phase != TargetApk || !apk.Success || apk.Skipped {
		t.Errorf("apk phase = %#v, want an executed successful phase", apk)
	}
	if ios.Phase != TargetIos {
		t.Fatalf("third phase = %q, want %q", ios.Phase, TargetIos)
	}
	if !ios.Skipped {
		t.Errorf("ios phase Skipped = false, want true (result: %#v)", ios)
	}
	if !ios.Success {
		t.Errorf("ios phase Success = false, want true — a skip is not a failure (result: %#v)", ios)
	}
	if ios.Warning == "" {
		t.Error("ios phase Warning = empty, want the unsupported-platform reason")
	}
	wantWarning := fmt.Sprintf("target %q is not supported on platform %q (supported platforms: %s)",
		TargetIos, PlatformLinux, PlatformDarwin)
	if ios.Warning != wantWarning {
		t.Errorf("ios phase Warning = %q, want %q", ios.Warning, wantWarning)
	}

	// The skipped target must not have been executed: only web and apk
	// reached the runner.
	if len(runner.args) != 2 {
		t.Errorf("runner invoked %d time(s), want 2 — the skipped target must not run", len(runner.args))
	}
}

// TestRunBuild_StrictFailsUnsupported verifies strict mode: an
// unsupported target fails the build instead of being skipped, and the
// strict failure stops the pipeline (TS-007-041, ADR-018 --strict
// parity).
func TestRunBuild_StrictFailsUnsupported(t *testing.T) {
	withPlatform(t, PlatformLinux)

	t.Run("strict_stops_pipeline", func(t *testing.T) {
		runner := &fakeRunner{output: "ok"}
		result := RunBuild(context.Background(), runner.run, contracts.BuildRequest{
			WorkingDir: "/var/lib/anvil/projects/acme-app",
			Strict:     true,
		})

		if result.Success {
			t.Fatal("Success = true, want false in strict mode")
		}
		// web runs, apk runs, ios fails in strict mode and stops.
		if len(result.Phases) != 3 {
			t.Fatalf("Phases length = %d, want 3 (result: %#v)", len(result.Phases), result)
		}
		failed := result.Phases[2]
		if failed.Phase != TargetIos {
			t.Errorf("failing phase = %q, want %q", failed.Phase, TargetIos)
		}
		if failed.Success {
			t.Error("failing phase Success = true, want false")
		}
		if failed.Skipped {
			t.Error("failing phase Skipped = true, want false — strict mode fails, it does not skip")
		}
		if !strings.Contains(failed.Error, "(strict mode)") {
			t.Errorf("failing phase Error = %q, want it to mention strict mode", failed.Error)
		}
		if !strings.Contains(failed.Error, TargetIos) {
			t.Errorf("failing phase Error = %q, want it to name the unsupported target", failed.Error)
		}
		// web and apk still ran.
		if len(runner.args) != 2 {
			t.Errorf("runner invoked %d time(s), want 2 (calls: %v)", len(runner.args), runner.args)
		}
	})

	t.Run("strict_single_unsupported_target", func(t *testing.T) {
		runner := &fakeRunner{output: "ok"}
		result := RunBuild(context.Background(), runner.run, contracts.BuildRequest{
			Targets: []string{TargetIos},
			Strict:  true,
		})

		if result.Success {
			t.Fatal("Success = true, want false in strict mode")
		}
		if len(result.Phases) != 1 {
			t.Fatalf("Phases length = %d, want 1 (result: %#v)", len(result.Phases), result)
		}
		if result.Phases[0].Phase != TargetIos || result.Phases[0].Success {
			t.Errorf("Phases[0] = %#v, want the failed ios phase", result.Phases[0])
		}
		if len(runner.args) != 0 {
			t.Errorf("runner invoked %d time(s), want 0 — the strict failure must not execute", len(runner.args))
		}
	})
}

// TestRunBuild_StrictDoesNotAffectSupportedTargets verifies that strict
// mode leaves supported targets untouched — it fails only unsupported
// ones (ADR-018).
func TestRunBuild_StrictDoesNotAffectSupportedTargets(t *testing.T) {
	withPlatform(t, PlatformDarwin)
	runner := &fakeRunner{output: "ok"}

	result := RunBuild(context.Background(), runner.run, contracts.BuildRequest{
		WorkingDir: "/var/lib/anvil/projects/acme-app",
		Strict:     true,
	})

	if !result.Success {
		t.Fatalf("Success = false, want true — every target is supported on darwin (result: %#v)", result)
	}
	if len(result.Phases) != 3 {
		t.Fatalf("Phases length = %d, want 3", len(result.Phases))
	}
	for _, p := range result.Phases {
		if !p.Success || p.Skipped || p.Warning != "" {
			t.Errorf("phase %#v: want an executed successful phase without skip details", p)
		}
	}
}
