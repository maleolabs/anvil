// Activation phase operations of the Laravel adapter (TS-P7-09, TS-P7-10).
//
// Each activation phase runs the corresponding `php artisan` command as a
// subprocess from the release's working directory and reports the outcome
// through the activation contract payloads (contracts.ActivationResult).
// The rollback operation (PhaseOperationRollback) reverses reversible
// phases (migrate → migrate:rollback); irreversible phases — the cache
// phases, whose effects cannot be undone — return an informational
// success that does not block rollback (TS-P7-10 AC-2).
package laravel

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"maleolabs.com/anvil/internal/contracts"
)

// Activation phase names declared in the capability declaration
// (Capabilities). Phase names are part of the adapter's contract surface:
// the Core invokes only declared phases (TS-P7-08 AC-3).
//
// Reference: TS-P7-09, TS-P7-07
const (
	// PhaseMigrate applies database migrations during activation.
	PhaseMigrate = "migrate"

	// PhaseConfigCache caches the application configuration.
	PhaseConfigCache = "config_cache"

	// PhaseRouteCache caches the application routes.
	PhaseRouteCache = "route_cache"

	// PhaseEventCache caches the application events.
	PhaseEventCache = "event_cache"

	// PhaseViewCache caches the application compiled views. It is not
	// part of the executable activation phase table — view:cache is not
	// reversible as a rollback operation, so the activation pipeline
	// cannot include it — but the constant lives with the other artisan
	// cache phases because the build pipeline reuses it (TS-P7-14: the
	// `view:cache` build phase). The executable activation command set —
	// including this exclusion — is the documented contract in MVP-002
	// §3.2 (TD-012); the manifest metadata surface carries `view:cache`
	// (TS-P7-15 AC-3, 005-adapter-command-contract §10.10).
	PhaseViewCache = "view_cache"
)

// phase defines one activation phase: the artisan command it runs during
// activation, its rollback command when reversible, and whether the
// operation is irreversible (no rollback possible — the phase reports an
// informational result instead of blocking rollback).
//
// Reference: TS-P7-09, TS-P7-10
type phase struct {
	// name is the phase identifier (Phase* constants).
	name string

	// activateArgs are the `php artisan` arguments for the activate
	// operation (e.g. []string{"migrate", "--force"}).
	activateArgs []string

	// rollbackArgs are the `php artisan` arguments for the rollback
	// operation. Nil when the phase has no rollback operation.
	rollbackArgs []string

	// irreversible reports that the phase's effects cannot be undone.
	// Rollback requests then return an informational success instead of
	// running a command (TS-P7-10 AC-2).
	irreversible bool
}

// phases is the adapter's phase table, in capability declaration order.
// The cache phases (config_cache, route_cache, event_cache) are
// irreversible: a rollback cannot restore the previous release's caches —
// the previous release's own activation regenerates them from its code.
//
// Reference: TS-P7-09, TS-P7-10
var phases = []phase{
	{
		name:         PhaseMigrate,
		activateArgs: []string{"migrate", "--force"},
		rollbackArgs: []string{"migrate:rollback"},
	},
	{
		name:         PhaseConfigCache,
		activateArgs: []string{"config:cache"},
		irreversible: true,
	},
	{
		name:         PhaseRouteCache,
		activateArgs: []string{"route:cache"},
		irreversible: true,
	},
	{
		name:         PhaseEventCache,
		activateArgs: []string{"event:cache"},
		irreversible: true,
	},
}

// commandRunner executes one artisan command: `php artisan <args...>`
// in dir (empty dir = current working directory) and returns the command
// output. The runner is a function so tests can inject a fake
// implementation without requiring PHP on the host.
//
// Reference: TS-P7-09 §7
type commandRunner func(ctx context.Context, dir string, args ...string) (output string, err error)

// runArtisan is the production command runner: it executes
// `php artisan <args...>` via os/exec with the environment inherited and
// the working directory set when dir is non-empty. The adapter is a
// standalone executable (004-review-resolutions D1) — it uses os/exec
// directly, not the Core's Process Runner, which exists only on the Core
// side.
//
// On failure the error carries the artisan stderr (or the exit error when
// stderr is empty) so phase failures report actionable details
// (TS-P7-09 AC-6).
//
// Reference: TS-P7-09, 004-review-resolutions D1
func runArtisan(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "php", append([]string{"artisan"}, args...)...)
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
		return stdout.String(), fmt.Errorf("artisan %s failed: %s", strings.Join(args, " "), detail)
	}
	return stdout.String(), nil
}

// RunActivation executes one activation phase operation and returns the
// contract result. The JSON result is authoritative for the phase
// outcome: Success=false with Error details reports a phase failure,
// while the process-level exit code reports whether the adapter itself
// produced a result (005-adapter-command-contract §7).
//
// Reference: TS-P7-09 AC-1..AC-6, TS-P7-10 AC-1, AC-2
func RunActivation(ctx context.Context, runner commandRunner, req contracts.ActivationRequest) contracts.ActivationResult {
	p, ok := lookupPhase(req.Phase)
	if !ok {
		return contracts.ActivationResult{
			Success: false,
			Error:   fmt.Sprintf("unknown activation phase %q", req.Phase),
		}
	}

	switch req.Operation {
	case contracts.PhaseOperationActivate:
		return runPhase(ctx, runner, req.Release.WorkingDir, p, p.activateArgs)
	case contracts.PhaseOperationRollback:
		if p.irreversible {
			return irreversibleRollbackResult(p)
		}
		return runPhase(ctx, runner, req.Release.WorkingDir, p, p.rollbackArgs)
	default:
		return contracts.ActivationResult{
			Success: false,
			Error:   fmt.Sprintf("unknown activation operation %q", req.Operation),
		}
	}
}

// lookupPhase returns the phase definition for the given name.
func lookupPhase(name string) (phase, bool) {
	for _, p := range phases {
		if p.name == name {
			return p, true
		}
	}
	return phase{}, false
}

// runPhase executes the given artisan arguments via the runner and maps
// the outcome to an ActivationResult. The working directory from the
// release context is passed through so artisan runs inside the release
// directory (005-adapter-command-contract §3.3).
func runPhase(ctx context.Context, runner commandRunner, dir string, p phase, args []string) contracts.ActivationResult {
	output, err := runner(ctx, dir, args...)
	if err != nil {
		return contracts.ActivationResult{
			Success: false,
			Output:  output,
			Error:   err.Error(),
		}
	}
	return contracts.ActivationResult{
		Success: true,
		Output:  output,
	}
}

// irreversibleRollbackResult reports an informational success for a
// rollback request on an irreversible phase. The operation cannot be
// undone, so the adapter documents the limitation in the result and does
// NOT block the rollback (TS-P7-10 AC-2): the Core treats Success=true as
// "rollback of this phase completed without error".
func irreversibleRollbackResult(p phase) contracts.ActivationResult {
	return contracts.ActivationResult{
		Success: true,
		Output: fmt.Sprintf(
			"phase %q is irreversible: %s cannot be undone; rollback proceeds without undoing this operation",
			p.name, strings.Join(p.activateArgs, " "),
		),
	}
}
