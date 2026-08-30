// Tests for the Core-side execution coordinator (TS-P7-08). The tests
// use the real Process Runner (internal/execution) and a stub adapter
// executable written to a temp directory as a shell script, exercising
// the subprocess command contract end to end.
package adapter

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
)

// writeStubAdapter writes the stub adapter shell script used by the
// coordinator tests to a temp directory and returns its path. The stub
// implements the adapter command contract: the first argument is the
// command name, the second is the JSON payload; it prints a fixed JSON
// response on stdout (or fails) depending on the command and the
// phase/check named in the payload.
//
// Reference: TS-P7-08
func writeStubAdapter(t *testing.T) string {
	t.Helper()

	script := `#!/bin/sh
# Stub adapter executable for Coordinator tests. Implements the adapter
# command contract: arg 1 = command name, arg 2 = JSON payload.
command="$1"
payload="$2"

json_field() {
  printf '%s' "$payload" | grep -o "\"$1\":\"[^\"]*\"" | head -1 | cut -d'"' -f4
}

case "$command" in
  "activate")
    phase=$(json_field phase)
    case "$phase" in
      "migrate")
        release_id=$(json_field release_id)
        working_dir=$(json_field working_dir)
        echo "{\"success\":true,\"output\":\"migrations applied release_id=$release_id working_dir=$working_dir\"}"
        ;;
      "config_cache") echo '{"success":false,"error":"cache clear failed"}' ;;
      "bogus")        echo 'this is not json' ;;
      "crash")        echo "phase crash exploded" >&2; exit 7 ;;
      *)              echo "unknown phase $phase" >&2; exit 3 ;;
    esac
    ;;
  "verify")
    check=$(json_field check)
    case "$check" in
      "vendor_present") echo '{"name":"vendor_present","passed":true,"details":"vendor directory found"}' ;;
      "artisan_ok")     echo '{"name":"artisan_ok","passed":false,"details":"artisan command failed"}' ;;
      *)                echo "unknown check $check" >&2; exit 3 ;;
    esac
    ;;
  "capabilities")
    framework=$(json_field framework)
    case "$framework" in
      "crash") echo "capabilities exploded" >&2; exit 7 ;;
      "bogus") echo 'this is not json' ;;
      *)       echo '{"capabilities":{"activation_phases":["migrate","config_cache"],"verification_checks":[{"name":"vendor_present","description":"vendor directory present"}],"diagnostic_commands":["routes:list"]}}' ;;
    esac
    ;;
  "manifest")
    echo '{"activation_commands":["php artisan migrate --force","php artisan config:cache"],"rollback_commands":["php artisan migrate:rollback"]}'
    ;;
  "build")
    working_dir=$(json_field working_dir)
    case "$working_dir" in
      *crash*) echo "build exploded" >&2; exit 7 ;;
      *bogus*) echo 'this is not json' ;;
      *fail*)  echo '{"success":false,"phases":[{"phase":"composer","success":false,"error":"composer install failed: no lock file"}]}' ;;
      *)       echo "{\"success\":true,\"phases\":[{\"phase\":\"composer\",\"success\":true,\"output\":\"build ok working_dir=$working_dir\"}]}" ;;
    esac
    ;;
  "template")
    framework=$(json_field framework)
    case "$framework" in
      "crash") echo "template exploded" >&2; exit 7 ;;
      "bogus") echo 'this is not json' ;;
      *)       echo '{"build":{"Pipeline":{"Name":"build","Stages":[{"Name":"build","Tasks":[{"Name":"compile","Command":"echo","Args":["compiling..."]}]}]}},"ci":{"Pipeline":{"Name":"ci","Stages":[{"Name":"test","Tasks":[{"Name":"unit-tests","Command":"echo","Args":["running unit tests..."]}]}]}}}' ;;
    esac
    ;;
  *)
    echo "unknown command $command" >&2
    exit 2
    ;;
esac
exit 0
`

	path := filepath.Join(t.TempDir(), "stub-adapter.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	return path
}

// coordinatorWithLaravel returns a Coordinator wired to the real Process
// Runner and a registry with the Laravel adapter registered with the
// given declaration. The provided executable is the stub adapter.
//
// Reference: TS-P7-08
func coordinatorWithLaravel(t *testing.T, executable string, decl contracts.CapabilityDeclaration) *Coordinator {
	t.Helper()

	registry := NewCapabilityRegistry()
	if err := registry.Register("laravel", decl); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	return NewCoordinator(execution.NewRunner(), registry)
}

// TestCoordinator_InvokeActivationSuccess verifies that a declared
// activation phase is invoked through the Process Runner and the
// adapter's JSON result is parsed and returned. The stub echoes back the
// release context it received, proving the execution context actually
// reaches the subprocess (TS-P7-08 AC-4 end-to-end).
//
// Reference: TS-P7-08 AC-2, AC-3, AC-4, AC-5
func TestCoordinator_InvokeActivationSuccess(t *testing.T) {
	executable := writeStubAdapter(t)
	decl := laravelCapabilities()
	decl.ActivationPhases = []string{"migrate", "config_cache", "bogus", "crash"}
	coord := coordinatorWithLaravel(t, executable, decl)

	workingDir := t.TempDir()
	req := contracts.ActivationRequest{
		Phase:     "migrate",
		Operation: contracts.PhaseOperationActivate,
		Release: contracts.ReleaseContext{
			ProjectID:   "acme-shop",
			ReleaseID:   "rel-20260801-01",
			Environment: "production",
			WorkingDir:  workingDir,
		},
	}

	result, err := coord.InvokeActivation(context.Background(), "laravel", executable, req)
	if err != nil {
		t.Fatalf("InvokeActivation returned error: %v", err)
	}
	if !result.Success {
		t.Error("Success = false, want true")
	}
	for _, want := range []string{"migrations applied", "release_id=rel-20260801-01", "working_dir=" + workingDir} {
		if !strings.Contains(result.Output, want) {
			t.Errorf("Output %q does not contain %q", result.Output, want)
		}
	}
}

// TestCoordinator_InvokeActivationFailureResult verifies that an adapter
// result with Success=false is returned as-is without a Go error — the
// JSON result is authoritative for the phase outcome.
//
// Reference: TS-P7-08 AC-5
func TestCoordinator_InvokeActivationFailureResult(t *testing.T) {
	executable := writeStubAdapter(t)
	decl := laravelCapabilities()
	decl.ActivationPhases = []string{"migrate", "config_cache", "bogus", "crash"}
	coord := coordinatorWithLaravel(t, executable, decl)

	req := contracts.ActivationRequest{
		Phase:     "config_cache",
		Operation: contracts.PhaseOperationActivate,
		Release:   contracts.ReleaseContext{ProjectID: "acme-shop", WorkingDir: t.TempDir()},
	}

	result, err := coord.InvokeActivation(context.Background(), "laravel", executable, req)
	if err != nil {
		t.Fatalf("InvokeActivation returned error: %v", err)
	}
	if result.Success {
		t.Error("Success = true, want false")
	}
	if result.Error != "cache clear failed" {
		t.Errorf("Error = %q, want %q", result.Error, "cache clear failed")
	}
}

// TestCoordinator_InvokeActivationPhaseNotDeclared verifies that an
// undeclared phase is rejected with a Go error before any subprocess is
// invoked — the coordinator reads declarations to determine what to
// invoke.
//
// Reference: TS-P7-08 AC-3
func TestCoordinator_InvokeActivationPhaseNotDeclared(t *testing.T) {
	executable := writeStubAdapter(t)
	decl := laravelCapabilities()
	decl.ActivationPhases = []string{"migrate"}
	coord := coordinatorWithLaravel(t, executable, decl)

	req := contracts.ActivationRequest{
		Phase:     "unknown_phase",
		Operation: contracts.PhaseOperationActivate,
		Release:   contracts.ReleaseContext{ProjectID: "acme-shop", WorkingDir: t.TempDir()},
	}

	_, err := coord.InvokeActivation(context.Background(), "laravel", executable, req)
	if err == nil {
		t.Fatal("InvokeActivation succeeded, want error")
	}
	if !strings.Contains(err.Error(), `does not declare activation phase "unknown_phase"`) {
		t.Errorf("error %q does not mention the undeclared phase", err)
	}
}

// TestCoordinator_InvokeActivationFrameworkNotRegistered verifies that an
// unregistered framework is rejected with a Go error before any
// subprocess is invoked.
//
// Reference: TS-P7-08 AC-3
func TestCoordinator_InvokeActivationFrameworkNotRegistered(t *testing.T) {
	executable := writeStubAdapter(t)
	registry := NewCapabilityRegistry()
	coord := NewCoordinator(execution.NewRunner(), registry)

	req := contracts.ActivationRequest{
		Phase:     "migrate",
		Operation: contracts.PhaseOperationActivate,
		Release:   contracts.ReleaseContext{ProjectID: "acme-shop", WorkingDir: t.TempDir()},
	}

	_, err := coord.InvokeActivation(context.Background(), "rails", executable, req)
	if err == nil {
		t.Fatal("InvokeActivation succeeded, want error")
	}
	if !strings.Contains(err.Error(), `adapter for framework "rails" is not registered`) {
		t.Errorf("error %q does not mention the unregistered framework", err)
	}
}

// TestCoordinator_InvokeActivationProcessFailure verifies that a
// non-zero adapter exit without a JSON result produces a descriptive Go
// error carrying the status, exit code, and stderr.
//
// Reference: TS-P7-08 AC-5
func TestCoordinator_InvokeActivationProcessFailure(t *testing.T) {
	executable := writeStubAdapter(t)
	decl := laravelCapabilities()
	decl.ActivationPhases = []string{"migrate", "config_cache", "bogus", "crash"}
	coord := coordinatorWithLaravel(t, executable, decl)

	req := contracts.ActivationRequest{
		Phase:     "crash",
		Operation: contracts.PhaseOperationActivate,
		Release:   contracts.ReleaseContext{ProjectID: "acme-shop", WorkingDir: t.TempDir()},
	}

	_, err := coord.InvokeActivation(context.Background(), "laravel", executable, req)
	if err == nil {
		t.Fatal("InvokeActivation succeeded, want error")
	}
	for _, want := range []string{"status=failure", "exit_code=7", "phase crash exploded"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestCoordinator_InvokeActivationInvalidJSON verifies that an adapter
// producing invalid JSON on stdout yields a descriptive Go error that
// includes the raw stdout.
//
// Reference: TS-P7-08 AC-5
func TestCoordinator_InvokeActivationInvalidJSON(t *testing.T) {
	executable := writeStubAdapter(t)
	decl := laravelCapabilities()
	decl.ActivationPhases = []string{"migrate", "config_cache", "bogus", "crash"}
	coord := coordinatorWithLaravel(t, executable, decl)

	req := contracts.ActivationRequest{
		Phase:     "bogus",
		Operation: contracts.PhaseOperationActivate,
		Release:   contracts.ReleaseContext{ProjectID: "acme-shop", WorkingDir: t.TempDir()},
	}

	_, err := coord.InvokeActivation(context.Background(), "laravel", executable, req)
	if err == nil {
		t.Fatal("InvokeActivation succeeded, want error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error %q does not mention invalid JSON", err)
	}
	if !strings.Contains(err.Error(), "this is not json") {
		t.Errorf("error %q does not include the raw stdout", err)
	}
}

// TestCoordinator_InvokeVerificationPass verifies that a declared
// verification check is invoked and a passing outcome is parsed and
// returned.
//
// Reference: TS-P7-08 AC-2, AC-3, AC-5
func TestCoordinator_InvokeVerificationPass(t *testing.T) {
	executable := writeStubAdapter(t)
	decl := laravelCapabilities()
	decl.VerificationChecks = []contracts.VerificationCheck{
		{Name: "vendor_present"},
		{Name: "artisan_ok"},
	}
	coord := coordinatorWithLaravel(t, executable, decl)

	req := contracts.VerificationRequest{
		Check:        "vendor_present",
		ArtifactPath: "/var/anvil/artifacts/app-v1.0.0.tar.gz",
	}

	outcome, err := coord.InvokeVerification(context.Background(), "laravel", executable, req)
	if err != nil {
		t.Fatalf("InvokeVerification returned error: %v", err)
	}
	if !outcome.Passed {
		t.Error("Passed = false, want true")
	}
	if outcome.Name != "vendor_present" {
		t.Errorf("Name = %q, want %q", outcome.Name, "vendor_present")
	}
}

// TestCoordinator_InvokeVerificationFail verifies that a failing check
// outcome is parsed and returned with its details, without a Go error.
//
// Reference: TS-P7-08 AC-5
func TestCoordinator_InvokeVerificationFail(t *testing.T) {
	executable := writeStubAdapter(t)
	decl := laravelCapabilities()
	decl.VerificationChecks = []contracts.VerificationCheck{
		{Name: "vendor_present"},
		{Name: "artisan_ok"},
	}
	coord := coordinatorWithLaravel(t, executable, decl)

	req := contracts.VerificationRequest{
		Check:        "artisan_ok",
		ArtifactPath: "/var/anvil/artifacts/app-v1.0.0.tar.gz",
	}

	outcome, err := coord.InvokeVerification(context.Background(), "laravel", executable, req)
	if err != nil {
		t.Fatalf("InvokeVerification returned error: %v", err)
	}
	if outcome.Passed {
		t.Error("Passed = true, want false")
	}
	if outcome.Details != "artisan command failed" {
		t.Errorf("Details = %q, want %q", outcome.Details, "artisan command failed")
	}
}

// TestCoordinator_InvokeVerificationCheckNotDeclared verifies that an
// undeclared check is rejected with a Go error before any subprocess is
// invoked.
//
// Reference: TS-P7-08 AC-3
func TestCoordinator_InvokeVerificationCheckNotDeclared(t *testing.T) {
	executable := writeStubAdapter(t)
	decl := laravelCapabilities()
	decl.VerificationChecks = []contracts.VerificationCheck{{Name: "vendor_present"}}
	coord := coordinatorWithLaravel(t, executable, decl)

	req := contracts.VerificationRequest{
		Check:        "unknown_check",
		ArtifactPath: "/var/anvil/artifacts/app-v1.0.0.tar.gz",
	}

	_, err := coord.InvokeVerification(context.Background(), "laravel", executable, req)
	if err == nil {
		t.Fatal("InvokeVerification succeeded, want error")
	}
	if !strings.Contains(err.Error(), `does not declare verification check "unknown_check"`) {
		t.Errorf("error %q does not mention the undeclared check", err)
	}
}

// TestCoordinator_PlanningHelpers verifies that ActivationPhases and
// VerificationChecks return the declared lists, and ok=false for
// unregistered frameworks.
//
// Reference: TS-P7-07 AC-5, TS-P7-08
func TestCoordinator_PlanningHelpers(t *testing.T) {
	executable := writeStubAdapter(t)
	coord := coordinatorWithLaravel(t, executable, laravelCapabilities())

	phases, ok := coord.ActivationPhases("laravel")
	if !ok {
		t.Fatal("ActivationPhases(\"laravel\") = ok=false, want ok=true")
	}
	wantPhases := []string{"migrate", "config_cache"}
	if len(phases) != len(wantPhases) {
		t.Fatalf("ActivationPhases = %v, want %v", phases, wantPhases)
	}
	for i, phase := range wantPhases {
		if phases[i] != phase {
			t.Errorf("ActivationPhases[%d] = %q, want %q", i, phases[i], phase)
		}
	}

	checks, ok := coord.VerificationChecks("laravel")
	if !ok {
		t.Fatal("VerificationChecks(\"laravel\") = ok=false, want ok=true")
	}
	wantChecks := []string{"vendor_present", "artisan_ok"}
	if len(checks) != len(wantChecks) {
		t.Fatalf("VerificationChecks = %v, want %v", checks, wantChecks)
	}
	for i, check := range wantChecks {
		if checks[i].Name != check {
			t.Errorf("VerificationChecks[%d].Name = %q, want %q", i, checks[i].Name, check)
		}
	}

	if _, ok := coord.ActivationPhases("rails"); ok {
		t.Error("ActivationPhases(\"rails\") = ok=true, want ok=false")
	}
	if _, ok := coord.VerificationChecks("rails"); ok {
		t.Error("VerificationChecks(\"rails\") = ok=true, want ok=false")
	}
}

// TestCoordinator_NilDependencies verifies that missing dependencies
// yield descriptive Go errors instead of panics.
//
// Reference: TS-P7-08
func TestCoordinator_NilDependencies(t *testing.T) {
	req := contracts.ActivationRequest{
		Phase:     "migrate",
		Operation: contracts.PhaseOperationActivate,
		Release:   contracts.ReleaseContext{ProjectID: "acme-shop"},
	}

	// Nil runner.
	coord := NewCoordinator(nil, NewCapabilityRegistry())
	_, err := coord.InvokeActivation(context.Background(), "laravel", "/bin/true", req)
	if err == nil {
		t.Error("InvokeActivation with nil runner succeeded, want error")
	} else if !strings.Contains(err.Error(), "Process Runner is nil") {
		t.Errorf("error %q does not mention the nil Process Runner", err)
	}

	// Nil registry.
	coord = NewCoordinator(execution.NewRunner(), nil)
	_, err = coord.InvokeActivation(context.Background(), "laravel", "/bin/true", req)
	if err == nil {
		t.Error("InvokeActivation with nil registry succeeded, want error")
	} else if !strings.Contains(err.Error(), "capability registry is nil") {
		t.Errorf("error %q does not mention the nil capability registry", err)
	}

	// Nil coordinator receiver.
	var nilCoord *Coordinator
	_, err = nilCoord.InvokeActivation(context.Background(), "laravel", "/bin/true", req)
	if err == nil {
		t.Error("InvokeActivation on nil Coordinator succeeded, want error")
	}
	if _, ok := nilCoord.ActivationPhases("laravel"); ok {
		t.Error("ActivationPhases on nil Coordinator = ok=true, want ok=false")
	}
}

// TestCoordinator_InvokeCapabilitiesSuccess verifies that the
// capabilities command is dispatched through the Process Runner and the
// adapter's CapabilityResult JSON is parsed and returned with all three
// categories intact.
//
// Reference: TS-P7-07, TS-P7-08
func TestCoordinator_InvokeCapabilitiesSuccess(t *testing.T) {
	executable := writeStubAdapter(t)
	coord := coordinatorWithLaravel(t, executable, laravelCapabilities())

	result, err := coord.InvokeCapabilities(context.Background(), "laravel", executable)
	if err != nil {
		t.Fatalf("InvokeCapabilities returned error: %v", err)
	}

	want := contracts.CapabilityDeclaration{
		ActivationPhases: []string{"migrate", "config_cache"},
		VerificationChecks: []contracts.VerificationCheck{
			{Name: "vendor_present", Description: "vendor directory present"},
		},
		DiagnosticCommands: []string{"routes:list"},
	}
	if !reflect.DeepEqual(result.Declaration, want) {
		t.Errorf("Declaration mismatch:\n got: %#v\nwant: %#v", result.Declaration, want)
	}
}

// TestCoordinator_InvokeCapabilitiesProcessFailure verifies that a
// non-zero adapter exit without a JSON result produces a descriptive Go
// error carrying the status, exit code, and stderr.
//
// Reference: TS-P7-07, TS-P7-08
func TestCoordinator_InvokeCapabilitiesProcessFailure(t *testing.T) {
	executable := writeStubAdapter(t)
	coord := coordinatorWithLaravel(t, executable, laravelCapabilities())

	_, err := coord.InvokeCapabilities(context.Background(), "crash", executable)
	if err == nil {
		t.Fatal("InvokeCapabilities succeeded, want error")
	}
	for _, want := range []string{"status=failure", "exit_code=7", "capabilities exploded"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestCoordinator_InvokeCapabilitiesInvalidJSON verifies that an adapter
// producing invalid JSON on stdout yields a descriptive Go error that
// includes the raw stdout.
//
// Reference: TS-P7-07, TS-P7-08
func TestCoordinator_InvokeCapabilitiesInvalidJSON(t *testing.T) {
	executable := writeStubAdapter(t)
	coord := coordinatorWithLaravel(t, executable, laravelCapabilities())

	_, err := coord.InvokeCapabilities(context.Background(), "bogus", executable)
	if err == nil {
		t.Fatal("InvokeCapabilities succeeded, want error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error %q does not mention invalid JSON", err)
	}
	if !strings.Contains(err.Error(), "this is not json") {
		t.Errorf("error %q does not include the raw stdout", err)
	}
}

// TestCoordinator_InvokeCapabilitiesNilDependencies verifies that missing
// dependencies yield descriptive Go errors instead of panics.
//
// Reference: TS-P7-07, TS-P7-08
func TestCoordinator_InvokeCapabilitiesNilDependencies(t *testing.T) {
	// Nil runner.
	coord := NewCoordinator(nil, NewCapabilityRegistry())
	_, err := coord.InvokeCapabilities(context.Background(), "laravel", "/bin/true")
	if err == nil {
		t.Error("InvokeCapabilities with nil runner succeeded, want error")
	} else if !strings.Contains(err.Error(), "Process Runner is nil") {
		t.Errorf("error %q does not mention the nil Process Runner", err)
	}

	// Nil registry.
	coord = NewCoordinator(execution.NewRunner(), nil)
	_, err = coord.InvokeCapabilities(context.Background(), "laravel", "/bin/true")
	if err == nil {
		t.Error("InvokeCapabilities with nil registry succeeded, want error")
	} else if !strings.Contains(err.Error(), "capability registry is nil") {
		t.Errorf("error %q does not mention the nil capability registry", err)
	}

	// Nil coordinator receiver.
	var nilCoord *Coordinator
	_, err = nilCoord.InvokeCapabilities(context.Background(), "laravel", "/bin/true")
	if err == nil {
		t.Error("InvokeCapabilities on nil Coordinator succeeded, want error")
	}
}

// TestCoordinator_InvokeTemplateSuccess verifies that the template
// command is dispatched through the Process Runner and the adapter's
// TemplateResult JSON is parsed and returned with the build and CI
// pipeline definitions intact (TS-007-038, ADR-020 §1).
//
// Reference: TS-007-038, ADR-020 §1
func TestCoordinator_InvokeTemplateSuccess(t *testing.T) {
	executable := writeStubAdapter(t)
	coord := coordinatorWithLaravel(t, executable, laravelCapabilities())

	result, err := coord.InvokeTemplate(context.Background(), "laravel", executable,
		contracts.TemplateRequest{Framework: "laravel"})
	if err != nil {
		t.Fatalf("InvokeTemplate returned error: %v", err)
	}

	if result.Build == nil {
		t.Fatal("Build = nil, want the adapter's build definition")
	}
	if result.Build.Pipeline.Name != "build" {
		t.Errorf("Build.Pipeline.Name = %q, want %q", result.Build.Pipeline.Name, "build")
	}
	if len(result.Build.Pipeline.Stages) != 1 {
		t.Fatalf("Build stages = %d, want 1", len(result.Build.Pipeline.Stages))
	}
	stage := result.Build.Pipeline.Stages[0]
	if len(stage.Tasks) != 1 || stage.Tasks[0].Command != "echo" {
		t.Errorf("Build stage tasks = %#v, want the stub's compile task", stage.Tasks)
	}
	// The returned definitions must pass the pipeline loader validation —
	// the Core validates adapter output before writing it (ADR-020 §1).
	if err := result.Build.Validate(); err != nil {
		t.Errorf("Build definition failed pipeline validation: %v", err)
	}

	if result.CI == nil {
		t.Fatal("CI = nil, want the adapter's CI definition")
	}
	if result.CI.Pipeline.Name != "ci" {
		t.Errorf("CI.Pipeline.Name = %q, want %q", result.CI.Pipeline.Name, "ci")
	}
	if err := result.CI.Validate(); err != nil {
		t.Errorf("CI definition failed pipeline validation: %v", err)
	}
}

// TestCoordinator_InvokeTemplateProcessFailure verifies that a non-zero
// adapter exit without a JSON result produces a descriptive Go error
// carrying the status, exit code, and stderr.
//
// Reference: TS-007-038, ADR-020 §1
func TestCoordinator_InvokeTemplateProcessFailure(t *testing.T) {
	executable := writeStubAdapter(t)
	coord := coordinatorWithLaravel(t, executable, laravelCapabilities())

	_, err := coord.InvokeTemplate(context.Background(), "crash", executable,
		contracts.TemplateRequest{Framework: "crash"})
	if err == nil {
		t.Fatal("InvokeTemplate succeeded, want error")
	}
	for _, want := range []string{"status=failure", "exit_code=7", "template exploded"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestCoordinator_InvokeTemplateInvalidJSON verifies that an adapter
// producing invalid JSON on stdout yields a descriptive Go error that
// includes the raw stdout.
//
// Reference: TS-007-038, ADR-020 §1
func TestCoordinator_InvokeTemplateInvalidJSON(t *testing.T) {
	executable := writeStubAdapter(t)
	coord := coordinatorWithLaravel(t, executable, laravelCapabilities())

	_, err := coord.InvokeTemplate(context.Background(), "bogus", executable,
		contracts.TemplateRequest{Framework: "bogus"})
	if err == nil {
		t.Fatal("InvokeTemplate succeeded, want error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error %q does not mention invalid JSON", err)
	}
	if !strings.Contains(err.Error(), "this is not json") {
		t.Errorf("error %q does not include the raw stdout", err)
	}
}

// TestCoordinator_InvokeTemplateNilDependencies verifies that missing
// dependencies yield descriptive Go errors instead of panics.
//
// Reference: TS-007-038, ADR-020 §1
func TestCoordinator_InvokeTemplateNilDependencies(t *testing.T) {
	// Nil runner.
	coord := NewCoordinator(nil, NewCapabilityRegistry())
	_, err := coord.InvokeTemplate(context.Background(), "laravel", "/bin/true",
		contracts.TemplateRequest{Framework: "laravel"})
	if err == nil {
		t.Error("InvokeTemplate with nil runner succeeded, want error")
	} else if !strings.Contains(err.Error(), "Process Runner is nil") {
		t.Errorf("error %q does not mention the nil Process Runner", err)
	}

	// Nil registry.
	coord = NewCoordinator(execution.NewRunner(), nil)
	_, err = coord.InvokeTemplate(context.Background(), "laravel", "/bin/true",
		contracts.TemplateRequest{Framework: "laravel"})
	if err == nil {
		t.Error("InvokeTemplate with nil registry succeeded, want error")
	} else if !strings.Contains(err.Error(), "capability registry is nil") {
		t.Errorf("error %q does not mention the nil capability registry", err)
	}

	// Nil coordinator receiver.
	var nilCoord *Coordinator
	_, err = nilCoord.InvokeTemplate(context.Background(), "laravel", "/bin/true",
		contracts.TemplateRequest{Framework: "laravel"})
	if err == nil {
		t.Error("InvokeTemplate on nil Coordinator succeeded, want error")
	}
}

// TestCoordinator_InvokeManifestCommandsSuccess verifies that the
// manifest command is dispatched through the Process Runner — without a
// payload argument, since the command carries no request data
// (005-adapter-command-contract §10.10) — and the adapter's
// ManifestCommandResult JSON is parsed and returned.
//
// Reference: TS-P7-15, TS-P7-16, 005-adapter-command-contract §10.10
func TestCoordinator_InvokeManifestCommandsSuccess(t *testing.T) {
	executable := writeStubAdapter(t)
	coord := coordinatorWithLaravel(t, executable, laravelCapabilities())

	result, err := coord.InvokeManifestCommands(context.Background(), "laravel", executable)
	if err != nil {
		t.Fatalf("InvokeManifestCommands returned error: %v", err)
	}

	wantActivation := []string{"php artisan migrate --force", "php artisan config:cache"}
	if !reflect.DeepEqual(result.ActivationCommands, wantActivation) {
		t.Errorf("ActivationCommands = %v, want %v", result.ActivationCommands, wantActivation)
	}
	wantRollback := []string{"php artisan migrate:rollback"}
	if !reflect.DeepEqual(result.RollbackCommands, wantRollback) {
		t.Errorf("RollbackCommands = %v, want %v", result.RollbackCommands, wantRollback)
	}
}

// TestCoordinator_InvokeManifestCommandsNilDependencies verifies that
// missing dependencies yield descriptive Go errors instead of panics.
//
// Reference: TS-P7-15, TS-P7-16
func TestCoordinator_InvokeManifestCommandsNilDependencies(t *testing.T) {
	// Nil runner.
	coord := NewCoordinator(nil, NewCapabilityRegistry())
	_, err := coord.InvokeManifestCommands(context.Background(), "laravel", "/bin/true")
	if err == nil {
		t.Error("InvokeManifestCommands with nil runner succeeded, want error")
	} else if !strings.Contains(err.Error(), "Process Runner is nil") {
		t.Errorf("error %q does not mention the nil Process Runner", err)
	}

	// Nil registry.
	coord = NewCoordinator(execution.NewRunner(), nil)
	_, err = coord.InvokeManifestCommands(context.Background(), "laravel", "/bin/true")
	if err == nil {
		t.Error("InvokeManifestCommands with nil registry succeeded, want error")
	} else if !strings.Contains(err.Error(), "capability registry is nil") {
		t.Errorf("error %q does not mention the nil capability registry", err)
	}

	// Nil coordinator receiver.
	var nilCoord *Coordinator
	_, err = nilCoord.InvokeManifestCommands(context.Background(), "laravel", "/bin/true")
	if err == nil {
		t.Error("InvokeManifestCommands on nil Coordinator succeeded, want error")
	}
}

// TestCoordinator_InvokeBuildSuccess verifies that the build command is
// dispatched through the Process Runner and the adapter's BuildResult
// JSON is parsed and returned. The stub echoes back the working
// directory it received, proving the execution context actually reaches
// the subprocess (TS-P7-14 end-to-end).
//
// Reference: TS-P7-14, TS-P7-08
func TestCoordinator_InvokeBuildSuccess(t *testing.T) {
	executable := writeStubAdapter(t)
	decl := laravelCapabilities()
	decl.BuildPhases = []string{"composer", "npm"}
	coord := coordinatorWithLaravel(t, executable, decl)

	workingDir := t.TempDir()
	req := contracts.BuildRequest{WorkingDir: workingDir}

	result, err := coord.InvokeBuild(context.Background(), "laravel", executable, req)
	if err != nil {
		t.Fatalf("InvokeBuild returned error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true (result: %#v)", result)
	}
	if len(result.Phases) != 1 {
		t.Fatalf("Phases length = %d, want 1 (result: %#v)", len(result.Phases), result)
	}
	if result.Phases[0].Phase != "composer" {
		t.Errorf("Phases[0].Phase = %q, want %q", result.Phases[0].Phase, "composer")
	}
	if !strings.Contains(result.Phases[0].Output, "working_dir="+workingDir) {
		t.Errorf("Phases[0].Output %q does not contain the working dir", result.Phases[0].Output)
	}
}

// TestCoordinator_InvokeBuildFailureResult verifies that an adapter build
// result with Success=false (a failing phase in the JSON result) is
// returned as-is without a Go error — the JSON result is authoritative
// for the build outcome.
//
// Reference: TS-P7-14 AC-7
func TestCoordinator_InvokeBuildFailureResult(t *testing.T) {
	executable := writeStubAdapter(t)
	decl := laravelCapabilities()
	decl.BuildPhases = []string{"composer", "npm"}
	coord := coordinatorWithLaravel(t, executable, decl)

	// The stub reports a failing build when the working dir contains
	// "fail". The directory must exist — the subprocess starts inside it.
	dir := filepath.Join(t.TempDir(), "fail")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll failed: %v", err)
	}

	result, err := coord.InvokeBuild(context.Background(), "laravel", executable, contracts.BuildRequest{WorkingDir: dir})
	if err != nil {
		t.Fatalf("InvokeBuild returned error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false (result: %#v)", result)
	}
	if len(result.Phases) != 1 || result.Phases[0].Success {
		t.Errorf("Phases = %#v, want one failing composer phase", result.Phases)
	}
	if result.Phases[0].Error != "composer install failed: no lock file" {
		t.Errorf("Phases[0].Error = %q, want the stub failure detail", result.Phases[0].Error)
	}
}

// TestCoordinator_InvokeBuildFrameworkNotRegistered verifies that an
// unregistered framework is rejected with a Go error before any
// subprocess is invoked.
//
// Reference: TS-P7-14, TS-P7-08 AC-3
func TestCoordinator_InvokeBuildFrameworkNotRegistered(t *testing.T) {
	executable := writeStubAdapter(t)
	registry := NewCapabilityRegistry()
	coord := NewCoordinator(execution.NewRunner(), registry)

	_, err := coord.InvokeBuild(context.Background(), "rails", executable, contracts.BuildRequest{})
	if err == nil {
		t.Fatal("InvokeBuild succeeded, want error")
	}
	if !strings.Contains(err.Error(), `adapter for framework "rails" is not registered`) {
		t.Errorf("error %q does not mention the unregistered framework", err)
	}
}

// TestCoordinator_InvokeBuildProcessFailure verifies that a non-zero
// adapter exit without a JSON result produces a descriptive Go error
// carrying the status, exit code, and stderr.
//
// Reference: TS-P7-14, TS-P7-08 AC-5
func TestCoordinator_InvokeBuildProcessFailure(t *testing.T) {
	executable := writeStubAdapter(t)
	decl := laravelCapabilities()
	decl.BuildPhases = []string{"composer"}
	coord := coordinatorWithLaravel(t, executable, decl)

	dir := filepath.Join(t.TempDir(), "crash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll failed: %v", err)
	}

	_, err := coord.InvokeBuild(context.Background(), "laravel", executable, contracts.BuildRequest{WorkingDir: dir})
	if err == nil {
		t.Fatal("InvokeBuild succeeded, want error")
	}
	for _, want := range []string{"status=failure", "exit_code=7", "build exploded"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestCoordinator_InvokeBuildInvalidJSON verifies that an adapter
// producing invalid JSON on stdout yields a descriptive Go error that
// includes the raw stdout.
//
// Reference: TS-P7-14, TS-P7-08 AC-5
func TestCoordinator_InvokeBuildInvalidJSON(t *testing.T) {
	executable := writeStubAdapter(t)
	decl := laravelCapabilities()
	decl.BuildPhases = []string{"composer"}
	coord := coordinatorWithLaravel(t, executable, decl)

	dir := filepath.Join(t.TempDir(), "bogus")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll failed: %v", err)
	}

	_, err := coord.InvokeBuild(context.Background(), "laravel", executable, contracts.BuildRequest{WorkingDir: dir})
	if err == nil {
		t.Fatal("InvokeBuild succeeded, want error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error %q does not mention invalid JSON", err)
	}
	if !strings.Contains(err.Error(), "this is not json") {
		t.Errorf("error %q does not include the raw stdout", err)
	}
}

// TestCoordinator_BuildPhasesHelper verifies that BuildPhases returns the
// declared build phases in declaration order, and ok=false for
// unregistered frameworks and nil receivers.
//
// Reference: TS-P7-14
func TestCoordinator_BuildPhasesHelper(t *testing.T) {
	executable := writeStubAdapter(t)
	decl := laravelCapabilities()
	decl.BuildPhases = []string{"composer", "npm", "config_cache", "route_cache", "view_cache"}
	coord := coordinatorWithLaravel(t, executable, decl)

	phases, ok := coord.BuildPhases("laravel")
	if !ok {
		t.Fatal("BuildPhases(\"laravel\") = ok=false, want ok=true")
	}
	want := []string{"composer", "npm", "config_cache", "route_cache", "view_cache"}
	if !reflect.DeepEqual(phases, want) {
		t.Errorf("BuildPhases = %v, want %v", phases, want)
	}

	if _, ok := coord.BuildPhases("rails"); ok {
		t.Error("BuildPhases(\"rails\") = ok=true, want ok=false")
	}

	var nilCoord *Coordinator
	if _, ok := nilCoord.BuildPhases("laravel"); ok {
		t.Error("BuildPhases on nil Coordinator = ok=true, want ok=false")
	}
}
