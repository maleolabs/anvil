// Build phase operations of the Laravel adapter (TS-P7-14).
//
// The build pipeline executes the framework build steps in order —
// composer install, asset build, and the artisan optimization caches —
// from the release's working directory, and reports the outcome of each
// phase through the build contract payloads (contracts.BuildResult). The
// pipeline stops at the first failing phase and reports that phase's
// failure with its output details (TS-P7-14 AC-7).
//
// The phases run through the same injectable commandRunner used by the
// activation phases: artisan phases reuse the production runArtisan
// runner, composer and npm phases use the dedicated runComposer and
// runNpm runners, and tests inject a fake runner for all phases.
package laravel

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"maleolabs.com/anvil/internal/contracts"
)

// Build phase names declared in the capability declaration
// (Capabilities), in build execution order (TS-P7-14 AC-6). The
// config_cache and route_cache build phases reuse the activation phase
// constants — the artisan commands are identical.
//
// Reference: TS-P7-14
const (
	// PhaseComposer installs the production dependencies with composer.
	PhaseComposer = "composer"

	// PhaseNpm builds the frontend assets with npm.
	PhaseNpm = "npm"
)

// buildPhase defines one build phase: the command runner that executes
// it (the production runner, or an injected fake in tests) and the
// arguments passed to the runner.
//
// Reference: TS-P7-14
type buildPhase struct {
	// name is the phase identifier (Phase* constants).
	name string

	// runner executes the phase command. It is the production runner
	// (runComposer, runNpm, or runArtisan); tests replace it with a
	// fake through RunBuild.
	runner commandRunner

	// args are the command arguments (without the program prefix —
	// runComposer/runNpm/runArtisan prepend their own program).
	args []string
}

// buildPhases is the adapter's build phase table, in execution order:
// dependencies (composer), assets (npm), then the artisan optimization
// caches (config, routes, views) — composer -> npm -> artisan
// (TS-P7-14 AC-6).
//
// Reference: TS-P7-14 AC-1..AC-6
var buildPhases = []buildPhase{
	{
		name:   PhaseComposer,
		runner: runComposer,
		args:   []string{"install", "--no-dev", "--optimize-autoloader"},
	},
	{
		name:   PhaseNpm,
		runner: runNpm,
		args:   []string{"run", "build"},
	},
	{
		name:   PhaseConfigCache,
		runner: runArtisan,
		args:   []string{"config:cache"},
	},
	{
		name:   PhaseRouteCache,
		runner: runArtisan,
		args:   []string{"route:cache"},
	},
	{
		name:   PhaseViewCache,
		runner: runArtisan,
		args:   []string{"view:cache"},
	},
}

// runComposer is the production runner for composer commands: it
// executes `composer <args...>` via os/exec with the environment
// inherited and the working directory set when dir is non-empty. The
// adapter is a standalone executable (004-review-resolutions D1) — it
// uses os/exec directly, not the Core's Process Runner. On failure the
// error carries the composer stderr (or the exit error when stderr is
// empty) so build failures report actionable details (TS-P7-14 AC-7).
//
// Reference: TS-P7-14, 004-review-resolutions D1
func runComposer(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "composer", args...)
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
		return stdout.String(), fmt.Errorf("composer %s failed: %s", strings.Join(args, " "), detail)
	}
	return stdout.String(), nil
}

// runNpm is the production runner for npm commands: it executes
// `npm <args...>` via os/exec with the environment inherited and the
// working directory set when dir is non-empty. On failure the error
// carries the npm stderr (or the exit error when stderr is empty) so
// build failures report actionable details (TS-P7-14 AC-7).
//
// Reference: TS-P7-14, 004-review-resolutions D1
func runNpm(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "npm", args...)
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
		return stdout.String(), fmt.Errorf("npm %s failed: %s", strings.Join(args, " "), detail)
	}
	return stdout.String(), nil
}

// RunBuild executes the adapter's build pipeline: each phase in build
// phase table order, stopping at the first failing phase and reporting
// that phase's failure with its output details (TS-P7-14 AC-7). Each
// phase reports its outcome in the returned BuildResult.Phases; the
// result's Success is computed from the phase outcomes.
//
// runner is the injectable command runner: when non-nil it replaces the
// production runner of every phase (tests inject a fake so no
// composer/npm/php is required on the test host); when nil each phase
// uses its production runner from the table (runComposer, runNpm, or
// runArtisan).
//
// Reference: TS-P7-14 AC-1..AC-7
func RunBuild(ctx context.Context, runner commandRunner, req contracts.BuildRequest) contracts.BuildResult {
	results := make([]contracts.BuildPhaseResult, 0, len(buildPhases))
	for _, p := range buildPhases {
		r := p.runner
		if runner != nil {
			r = runner
		}

		output, err := r(ctx, req.WorkingDir, p.args...)
		phaseResult := contracts.BuildPhaseResult{
			Phase:   p.name,
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

// buildSucceeded reports whether every executed build phase succeeded. A
// build with no executed phases — an empty table or a build that ran no
// phases — is vacuously successful (a graceful no-op build, ADR-009
// §9.7).
func buildSucceeded(results []contracts.BuildPhaseResult) bool {
	for _, r := range results {
		if !r.Success {
			return false
		}
	}
	return true
}
