// Tests for the Laravel adapter command dispatcher: each supported
// command, unknown commands, malformed JSON, and argument errors — all
// exercised in-process on the dispatch function (the executable entrypoint
// is a thin os.Exit wrapper around it).
package laravel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// runDispatch invokes the dispatcher with a fake runner and returns the
// exit code, stdout, and stderr.
func runDispatch(t *testing.T, runner commandRunner, args ...string) (int, string, string) {
	t.Helper()
	adapter := &Adapter{runner: runner}
	var stdout, stderr bytes.Buffer
	code := adapter.Run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// decodeStdout unmarshals the dispatcher's stdout into out, failing the
// test when the output is not valid JSON.
func decodeStdout(t *testing.T, stdout string, out any) {
	t.Helper()
	if err := json.Unmarshal([]byte(stdout), out); err != nil {
		t.Fatalf("stdout %q is not valid JSON: %v", stdout, err)
	}
}

// TestRun_Capabilities verifies the capabilities command prints the
// declared capabilities JSON and exits 0.
func TestRun_Capabilities(t *testing.T) {
	code, stdout, stderr := runDispatch(t, nil,
		contracts.CommandCapabilities, `{"framework":"laravel"}`)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}

	var result contracts.CapabilityResult
	decodeStdout(t, stdout, &result)

	if len(result.Declaration.ActivationPhases) != 4 {
		t.Errorf("ActivationPhases length = %d, want 4", len(result.Declaration.ActivationPhases))
	}
	if len(result.Declaration.VerificationChecks) != 3 {
		t.Errorf("VerificationChecks length = %d, want 3", len(result.Declaration.VerificationChecks))
	}
}

// TestRun_Activate verifies the activate command parses the release
// context, runs the phase through the runner, and prints the JSON result
// with exit 0.
func TestRun_Activate(t *testing.T) {
	runner := &fakeRunner{output: "Migration table created successfully."}
	workingDir := "/var/www/app/releases/rel-1"

	payload, err := json.Marshal(contracts.ActivationRequest{
		Phase:     PhaseMigrate,
		Operation: contracts.PhaseOperationActivate,
		Release: contracts.ReleaseContext{
			ProjectID:  "acme-shop",
			ReleaseID:  "rel-20260801-01",
			WorkingDir: workingDir,
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	code, stdout, stderr := runDispatch(t, runner.run, contracts.CommandActivation, string(payload))
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}

	var result contracts.ActivationResult
	decodeStdout(t, stdout, &result)

	if !result.Success {
		t.Fatalf("Success = false, want true (result: %#v)", result)
	}
	if result.Output != "Migration table created successfully." {
		t.Errorf("Output = %q, want the runner output", result.Output)
	}
	if len(runner.args) != 1 || !reflectEqual(runner.args[0], []string{"migrate", "--force"}) {
		t.Errorf("runner args = %v, want [migrate --force]", runner.args)
	}
	if len(runner.dirs) != 1 || runner.dirs[0] != workingDir {
		t.Errorf("runner dirs = %v, want [%s]", runner.dirs, workingDir)
	}
}

// TestRun_Verify verifies the verify command prints the check outcome
// JSON and exits 0.
func TestRun_Verify(t *testing.T) {
	artifactDir := writeArtifactDir(t, "vendor/autoload.php")

	payload, err := json.Marshal(contracts.VerificationRequest{
		Check:        CheckVendorPresent,
		ArtifactPath: artifactDir,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	code, stdout, stderr := runDispatch(t, nil, contracts.CommandVerification, string(payload))
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}

	var outcome contracts.VerificationOutcome
	decodeStdout(t, stdout, &outcome)

	if !outcome.Passed {
		t.Fatalf("Passed = false, want true (outcome: %#v)", outcome)
	}
	if outcome.Name != CheckVendorPresent {
		t.Errorf("Name = %q, want %q", outcome.Name, CheckVendorPresent)
	}
}

// TestRun_Extension verifies the extension command prints the declared
// configuration extension JSON and exits 0.
func TestRun_Extension(t *testing.T) {
	code, stdout, stderr := runDispatch(t, nil,
		contracts.CommandConfigExtension, `{"framework":"laravel"}`)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}

	var result contracts.ConfigExtensionResult
	decodeStdout(t, stdout, &result)

	if result.Extension.Framework != Framework {
		t.Errorf("Extension.Framework = %q, want %q", result.Extension.Framework, Framework)
	}
	if len(result.Extension.Keys) != 3 {
		t.Errorf("Extension.Keys length = %d, want 3", len(result.Extension.Keys))
	}
}

// TestRun_Validate verifies the validate command prints the validation
// result JSON and exits 0 — for both valid and invalid values.
func TestRun_Validate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		code, stdout, stderr := runDispatch(t, nil,
			contracts.CommandConfigValidation,
			`{"values":[{"key":"framework.laravel.version","value":"11.0.0"}]}`)
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
		}
		var result contracts.ConfigValidationResult
		decodeStdout(t, stdout, &result)
		if !result.Valid {
			t.Errorf("Valid = false, want true (errors: %v)", result.Errors)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		code, stdout, stderr := runDispatch(t, nil,
			contracts.CommandConfigValidation,
			`{"values":[{"key":"framework.laravel.version","value":"not-a-version"}]}`)
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
		}
		var result contracts.ConfigValidationResult
		decodeStdout(t, stdout, &result)
		if result.Valid {
			t.Error("Valid = true, want false")
		}
		if len(result.Errors) == 0 {
			t.Error("Errors = empty, want a validation error")
		}
	})
}

// TestRun_UnknownCommand verifies that an unknown command exits non-zero
// with a diagnostic on stderr (ADR-010 §8.1).
func TestRun_UnknownCommand(t *testing.T) {
	code, stdout, stderr := runDispatch(t, nil, "frobnicate", `{}`)
	if code == ExitOK {
		t.Fatal("exit code = 0, want non-zero for an unknown command")
	}
	if code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, `unknown command "frobnicate"`) {
		t.Errorf("stderr = %q, want mention of the unknown command", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty for a failed dispatch", stdout)
	}
}

// TestRun_MalformedJSON verifies that malformed JSON payloads exit
// non-zero for every command that requires a payload (ADR-010 §8.1).
func TestRun_MalformedJSON(t *testing.T) {
	commands := []string{
		contracts.CommandCapabilities,
		contracts.CommandActivation,
		contracts.CommandVerification,
		contracts.CommandConfigExtension,
		contracts.CommandConfigValidation,
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			code, _, stderr := runDispatch(t, nil, command, "{not-json")
			if code == ExitOK {
				t.Fatalf("exit code = 0, want non-zero for malformed JSON (stderr: %s)", stderr)
			}
			if !strings.Contains(stderr, "invalid JSON payload") {
				t.Errorf("stderr = %q, want mention of the invalid JSON payload", stderr)
			}
		})
	}
}

// TestRun_MissingPayload verifies that a missing payload exits non-zero
// with a usage diagnostic.
func TestRun_MissingPayload(t *testing.T) {
	code, _, stderr := runDispatch(t, nil, contracts.CommandActivation)
	if code == ExitOK {
		t.Fatal("exit code = 0, want non-zero for a missing payload")
	}
	if !strings.Contains(stderr, "requires a JSON payload argument") {
		t.Errorf("stderr = %q, want mention of the missing payload", stderr)
	}
}

// TestRun_NoArgs verifies that an invocation without arguments exits
// non-zero with a usage message.
func TestRun_NoArgs(t *testing.T) {
	code, _, stderr := runDispatch(t, nil)
	if code == ExitOK {
		t.Fatal("exit code = 0, want non-zero without a command")
	}
	if !strings.Contains(stderr, "usage") {
		t.Errorf("stderr = %q, want a usage message", stderr)
	}
}

// TestRun_TooManyArgs verifies that more than one payload argument exits
// non-zero.
func TestRun_TooManyArgs(t *testing.T) {
	code, _, stderr := runDispatch(t, nil, contracts.CommandCapabilities, `{}`, `extra`)
	if code == ExitOK {
		t.Fatal("exit code = 0, want non-zero for extra arguments")
	}
	if !strings.Contains(stderr, "too many arguments") {
		t.Errorf("stderr = %q, want mention of the extra arguments", stderr)
	}
}

// TestRun_ActivatePhaseFailureExitsZero verifies the exit-code semantics:
// a phase that FAILS still exits 0 because a valid JSON result (with
// Success=false) was produced — the JSON result is authoritative for the
// phase outcome (005-adapter-command-contract §7).
func TestRun_ActivatePhaseFailureExitsZero(t *testing.T) {
	runner := &fakeRunner{err: fmt.Errorf("artisan migrate --force failed: boom")}
	payload, err := json.Marshal(contracts.ActivationRequest{
		Phase:     PhaseMigrate,
		Operation: contracts.PhaseOperationActivate,
		Release:   contracts.ReleaseContext{ProjectID: "acme-shop"},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	code, stdout, stderr := runDispatch(t, runner.run, contracts.CommandActivation, string(payload))
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d — a produced JSON result exits 0 (stderr: %s)", code, ExitOK, stderr)
	}
	var result contracts.ActivationResult
	decodeStdout(t, stdout, &result)
	if result.Success {
		t.Error("Success = true, want false")
	}
	if result.Error == "" {
		t.Error("Error = empty, want failure details")
	}
}

// TestRun_ActivationUnknownPhase verifies that a phase the adapter does
// not declare still produces a JSON failure result (the adapter reports
// the failure through the contract rather than dying).
func TestRun_ActivationUnknownPhase(t *testing.T) {
	payload, err := json.Marshal(contracts.ActivationRequest{
		Phase:     "unknown_phase",
		Operation: contracts.PhaseOperationActivate,
		Release:   contracts.ReleaseContext{ProjectID: "acme-shop"},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	code, stdout, _ := runDispatch(t, nil, contracts.CommandActivation, string(payload))
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	var result contracts.ActivationResult
	decodeStdout(t, stdout, &result)
	if result.Success {
		t.Error("Success = true, want false")
	}
	if !strings.Contains(result.Error, `unknown activation phase "unknown_phase"`) {
		t.Errorf("Error = %q, want mention of the unknown phase", result.Error)
	}
}

// TestAdapter_RunnerRequiredForActivate verifies that the injectable
// runner is what the dispatcher uses — a runner recording invocations
// proves the wiring end to end.
func TestAdapter_RunnerRequiredForActivate(t *testing.T) {
	calls := 0
	runner := func(_ context.Context, _ string, _ ...string) (string, error) {
		calls++
		return "ok", nil
	}

	payload, err := json.Marshal(contracts.ActivationRequest{
		Phase:     PhaseEventCache,
		Operation: contracts.PhaseOperationActivate,
		Release:   contracts.ReleaseContext{ProjectID: "acme-shop"},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	code, stdout, _ := runDispatch(t, runner, contracts.CommandActivation, string(payload))
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	if calls != 1 {
		t.Errorf("runner calls = %d, want 1", calls)
	}
	var result contracts.ActivationResult
	decodeStdout(t, stdout, &result)
	if !result.Success {
		t.Errorf("Success = false, want true (result: %#v)", result)
	}
}

// reflectEqual is a tiny equality helper avoiding an extra import in the
// dispatcher tests.
func reflectEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
