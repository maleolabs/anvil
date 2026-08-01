// Command dispatcher of the Laravel adapter executable.
//
// The dispatcher implements the invocation shape of the adapter command
// contract (005-adapter-command-contract §5.2): the first CLI argument is
// the command name, the second is the JSON payload as a single argument.
// It prints the structured JSON result to stdout and uses the exit code
// convention of ADR-010 §8.1.
//
// Exit code semantics (documented in 005-adapter-command-contract §7):
// the adapter exits 0 whenever it produced a valid JSON result on stdout
// — the JSON result is authoritative for the operation outcome (a phase
// that fails reports Success=false in its JSON result, which the Core
// reads as the phase outcome). The adapter exits non-zero only when it
// could NOT produce a JSON result: unknown command, malformed payload,
// or an internal dispatch error. This keeps the exit code and the JSON
// result in agreement (005 §7 — "the exit code is authoritative for
// process-level failure").
package laravel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"maleolabs.com/anvil/internal/contracts"
)

// Exit codes of the adapter executable. Zero indicates success
// (ADR-010 §8.1); non-zero values categorize the failure.
//
// Reference: 005-adapter-command-contract §7
const (
	// ExitOK is returned when the adapter produced a valid JSON result.
	ExitOK = 0

	// ExitError is returned when a command could not be dispatched:
	// malformed JSON payload, invalid arguments, or an internal error.
	ExitError = 1

	// ExitUsage is returned for an unknown command or a missing command
	// name.
	ExitUsage = 2
)

// Adapter is the Laravel adapter executable's command surface. It holds
// the injectable command runners used by the activation phases and the
// build pipeline.
//
// Reference: TS-P7-09, TS-P7-14, 004-review-resolutions D1
type Adapter struct {
	// runner executes activation phases (`php artisan ...`). Tests
	// construct &Adapter{runner: fakeRunner} to execute phases without
	// PHP on the host.
	runner commandRunner

	// buildRunner executes the build phases. A nil buildRunner means
	// each build phase uses its production runner from the build table
	// (runComposer, runNpm, runArtisan); tests set it to a fake to
	// execute the build pipeline without composer/npm/php on the host
	// (TS-P7-14).
	buildRunner commandRunner
}

// New returns an Adapter wired to the production artisan runner
// (runArtisan — os/exec). The build runner is left nil so build phases
// use their production runners. Tests construct &Adapter{runner: f,
// buildRunner: f} to execute phases without PHP on the host.
func New() *Adapter {
	return &Adapter{runner: runArtisan}
}

// ErrUnknownCommand is returned when the adapter receives a command name
// it does not implement. It maps to ExitUsage so unknown commands are
// distinguishable from malformed payloads.
var ErrUnknownCommand = errors.New("unknown command")

// Run handles one adapter invocation and returns the process exit code.
// args[0] is the command name; args[1] is the JSON payload (the Process
// Runner has no stdin channel, so payloads are always passed as a single
// trailing argument). The JSON result is written to stdout; diagnostics
// are written to stderr.
//
// Reference: 005-adapter-command-contract §2, §5.2, §7
func (a *Adapter) Run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "laravel-adapter: usage: laravel-adapter <command> [<json-payload>]")
		return ExitUsage
	}
	if len(args) > 2 {
		fmt.Fprintf(stderr, "laravel-adapter: too many arguments for command %q (expected <command> [<json-payload>])\n", args[0])
		return ExitUsage
	}

	command := args[0]
	var payload []byte
	if len(args) == 2 {
		payload = []byte(args[1])
	}

	result, err := a.handle(context.Background(), command, payload)
	if err != nil {
		fmt.Fprintf(stderr, "laravel-adapter: %v\n", err)
		if errors.Is(err, ErrUnknownCommand) {
			return ExitUsage
		}
		return ExitError
	}

	data, err := json.Marshal(result)
	if err != nil {
		fmt.Fprintf(stderr, "laravel-adapter: marshal result for command %q: %v\n", command, err)
		return ExitError
	}
	fmt.Fprintln(stdout, string(data))
	return ExitOK
}

// handle dispatches one command to its handler and returns the contract
// payload to serialize. An error means no JSON result can be produced —
// the caller maps it to a non-zero exit.
func (a *Adapter) handle(ctx context.Context, command string, payload []byte) (any, error) {
	switch command {
	case contracts.CommandCapabilities:
		var req contracts.CapabilityRequest
		if err := parsePayload(command, payload, &req); err != nil {
			return nil, err
		}
		return Capabilities(), nil

	case contracts.CommandActivation:
		var req contracts.ActivationRequest
		if err := parsePayload(command, payload, &req); err != nil {
			return nil, err
		}
		return RunActivation(ctx, a.runner, req), nil

	case contracts.CommandVerification:
		var req contracts.VerificationRequest
		if err := parsePayload(command, payload, &req); err != nil {
			return nil, err
		}
		return RunVerification(req), nil

	case contracts.CommandConfigExtension:
		var req contracts.ConfigExtensionRequest
		if err := parsePayload(command, payload, &req); err != nil {
			return nil, err
		}
		return ConfigExtension(), nil

	case contracts.CommandConfigValidation:
		var req contracts.ConfigValidationRequest
		if err := parsePayload(command, payload, &req); err != nil {
			return nil, err
		}
		return ValidateConfigValues(req), nil

	case contracts.CommandBuild:
		var req contracts.BuildRequest
		if err := parsePayload(command, payload, &req); err != nil {
			return nil, err
		}
		return RunBuild(ctx, a.buildRunner, req), nil

	default:
		return nil, fmt.Errorf("%w %q", ErrUnknownCommand, command)
	}
}

// parsePayload decodes the single JSON payload argument into req. The
// payload is required — the Core always sends it; a malformed payload is
// a contract violation reported as a process failure (non-zero exit).
func parsePayload(command string, payload []byte, req any) error {
	if len(payload) == 0 {
		return fmt.Errorf("command %q requires a JSON payload argument", command)
	}
	if err := json.Unmarshal(payload, req); err != nil {
		return fmt.Errorf("command %q: invalid JSON payload: %v", command, err)
	}
	return nil
}
