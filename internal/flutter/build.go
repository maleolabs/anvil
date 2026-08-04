// Build targets of the Flutter adapter (TS-P7-21, TS-007-041).
//
// The Flutter build pipeline executes the framework build targets — web,
// APK, and iOS — in table order from the release's working directory,
// and reports the outcome of each target through the build contract
// payloads (contracts.BuildResult). The pipeline stops at the first
// failing target and reports that target's failure with its output
// details, mirroring the Laravel build pipeline (TS-P7-14).
//
// The target table is declarative (TS-P7-21 AC-4): each entry carries
// the target name, the flutter command arguments, and the platform
// metadata — the platforms that support the target (ADR-018). The build
// pipeline applies ADR-018 platform-aware execution (TS-007-041):
// target selection (req.Targets), platform filtering (unsupported
// targets are skipped with a warning; strict mode fails them), matching
// the local engine's --target/--strict semantics.
//
// The targets run through the injectable commandRunner: the production
// runFlutter runner executes `flutter build ...` via os/exec, and tests
// inject a fake runner so no Flutter toolchain is required on the test
// host.
//
// Reference: TS-P7-21, TS-P7-14, TS-007-041, ADR-018
package flutter

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"maleolabs.com/anvil/internal/contracts"
)

// Build target names. The names are declared in the capability
// declaration (Capabilities().BuildPhases) in the same order as the
// target table (TS-P7-21 AC-4, TS-P7-20).
//
// Reference: TS-P7-21
const (
	// TargetWeb builds the Flutter web application
	// (`flutter build web`).
	TargetWeb = "web"

	// TargetApk builds the Android APK
	// (`flutter build apk --release`).
	TargetApk = "apk"

	// TargetIos builds the iOS application
	// (`flutter build ios --release`). It requires macOS/Xcode and is
	// therefore supported on the darwin platform only (ADR-018).
	TargetIos = "ios"
)

// BuildTarget declares one Flutter build target: its name, the flutter
// command arguments that build it, and the platforms that support it
// (ADR-018 platform metadata). The declaration is data, not logic —
// platform filtering (TS-P7-23) consumes Platforms without changing the
// target definitions.
//
// Reference: TS-P7-21 AC-1..AC-4, ADR-018
type BuildTarget struct {
	// Name is the target identifier (Target* constants). It is also
	// the phase name reported in the build result.
	Name string

	// Args are the flutter command arguments (without the program
	// prefix — runFlutter prepends "flutter"). E.g. TargetApk is
	// ["build", "apk", "--release"].
	Args []string

	// Platforms lists the platforms (Platform* constants, ADR-018
	// values) that support this target. E.g. TargetIos supports
	// [PlatformDarwin] only.
	Platforms []string
}

// buildTargets is the adapter's build target table, in execution order:
// web, APK, then iOS (TS-P7-21). The order is the fixed order both the
// build pipeline and the capability declaration's BuildPhases use.
//
// Reference: TS-P7-21 AC-1..AC-4
var buildTargets = []BuildTarget{
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

// commandRunner executes one flutter command: `flutter <args...>` in dir
// (empty dir = current working directory) and returns the command
// output. The runner is a function so tests can inject a fake
// implementation without requiring the Flutter toolchain on the host
// (TS-P7-21).
//
// Reference: TS-P7-21, 004-review-resolutions D1
type commandRunner func(ctx context.Context, dir string, args ...string) (output string, err error)

// runFlutter is the production command runner: it executes
// `flutter <args...>` via os/exec with the environment inherited and the
// working directory set when dir is non-empty. The adapter is a
// standalone executable (004-review-resolutions D1) — it uses os/exec
// directly, not the Core's Process Runner, which exists only on the Core
// side.
//
// On failure the error carries the flutter stderr (or the exit error
// when stderr is empty) so build failures report actionable details
// (TS-P7-21).
//
// Reference: TS-P7-21, 004-review-resolutions D1
func runFlutter(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "flutter", args...)
	if dir != "" {
		cmd.Dir = dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return stdout.String(), fmt.Errorf("flutter %s failed: %s", strings.Join(args, " "), detail)
	}
	return stdout.String(), nil
}

// RunBuild executes the adapter's build pipeline: each target in build
// target table order, stopping at the first failing target and reporting
// that target's failure with its output details (TS-P7-21). Each target
// reports its outcome in the returned BuildResult.Phases; the result's
// Success is computed from the target outcomes.
//
// Platform-aware execution (TS-007-041, ADR-018) — the adapter-side
// counterpart of the local engine's --target/--strict semantics:
//
//   - Target selection: when req.Targets is non-empty, only the targets
//     whose name is in the list run, in table order; targets not
//     selected are not executed. When Targets is empty, all targets are
//     considered selected — the pre-TS-007-041 behavior for supported
//     platforms (additive compatibility, 005 §8). Unknown target names
//     select no targets; the result then reports no phases with
//     Success=true — a graceful no-op build (ADR-009 §9.7).
//   - Platform filtering: a selected target whose Platforms do not
//     include the current platform (flutter.Current) is skipped with
//     Skipped=true and a Warning describing the unsupported platform.
//     The skip is graceful — it does not fail the build and does not
//     stop the pipeline.
//   - Strict mode: under req.Strict, an unsupported target fails
//     instead of being skipped (Success=false, Error describing strict
//     mode) and stops the pipeline, matching the local engine.
//
// runner is the injectable command runner: when non-nil it replaces the
// production runner of every target (tests inject a fake so no Flutter
// toolchain is required on the test host); when nil each target uses its
// production runner from the table (runFlutter).
//
// Reference: TS-P7-21, TS-P7-14, TS-007-041, ADR-018
func RunBuild(ctx context.Context, runner commandRunner, req contracts.BuildRequest) contracts.BuildResult {
	results := make([]contracts.BuildPhaseResult, 0, len(buildTargets))
	current := platformDetector()
	for _, target := range buildTargets {
		if !targetSelected(req.Targets, target.Name) {
			continue
		}

		if !targetSupported(target.Platforms, current) {
			reason := targetSkipReason(target.Name, target.Platforms, current)
			if req.Strict {
				// Strict mode: an unsupported target is a failure and
				// stops the pipeline (ADR-018 --strict).
				results = append(results, contracts.BuildPhaseResult{
					Phase:   target.Name,
					Success: false,
					Error:   reason + " (strict mode)",
				})
				break
			}
			// Graceful degradation: skip with a warning; a skip does
			// not stop the pipeline and does not fail the build
			// (ADR-018).
			results = append(results, contracts.BuildPhaseResult{
				Phase:   target.Name,
				Success: true,
				Skipped: true,
				Warning: reason,
			})
			continue
		}

		r := runFlutter
		if runner != nil {
			r = runner
		}

		output, err := r(ctx, req.WorkingDir, target.Args...)
		phaseResult := contracts.BuildPhaseResult{
			Phase:   target.Name,
			Success: err == nil,
			Output:  output,
		}
		if err != nil {
			phaseResult.Error = err.Error()
		}
		results = append(results, phaseResult)
		if err != nil {
			break
		}
	}

	return contracts.BuildResult{
		Phases:  results,
		Success: buildSucceeded(results),
	}
}

// platformDetector returns the platform identifier used for
// platform-aware target filtering (ADR-018). It defaults to Current —
// runtime.GOOS detection — and is a package-level seam so tests can
// exercise every platform deterministically regardless of the host
// (the same seam pattern as the engine's WithPlatformDetector).
//
// Reference: TS-007-041, ADR-018
var platformDetector = Current

// targetSelected reports whether the target runs under target
// selection: an empty target list selects every target (no filtering);
// a non-empty list selects only targets named in it (TS-007-041).
func targetSelected(targets []string, name string) bool {
	if len(targets) == 0 {
		return true
	}
	return slices.Contains(targets, name)
}

// targetSupported reports whether the target can run on the current
// platform: the platform is listed in the target's supported platforms
// (ADR-018). A target with no platform metadata is supported
// everywhere.
func targetSupported(platforms []string, current string) bool {
	if len(platforms) == 0 {
		return true
	}
	return slices.Contains(platforms, current)
}

// targetSkipReason describes why the target cannot run on the current
// platform, in the same shape as the local engine's skip reason
// (internal/execution platformSkipReason), e.g.:
//
//	target "ios" is not supported on platform "linux" (supported platforms: darwin)
//
// Reference: TS-007-041, ADR-018
func targetSkipReason(name string, platforms []string, current string) string {
	return fmt.Sprintf("target %q is not supported on platform %q (supported platforms: %s)",
		name, current, strings.Join(platforms, ", "))
}

// buildSucceeded reports whether every executed build target succeeded.
// A build with no executed targets — an empty table or a build that ran
// no targets — is vacuously successful (a graceful no-op build, ADR-009
// §9.7).
func buildSucceeded(results []contracts.BuildPhaseResult) bool {
	for _, r := range results {
		if !r.Success {
			return false
		}
	}
	return true
}
