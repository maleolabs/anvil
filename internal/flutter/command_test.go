// Tests for the Flutter adapter command dispatcher: each supported
// command, unknown commands (including the absent activate), malformed
// JSON, and argument errors — all exercised in-process on the dispatch
// function (the executable entrypoint is a thin os.Exit wrapper around
// it).
package flutter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// runDispatch invokes the dispatcher with a fake runner wired as the
// build runner and returns the exit code, stdout, and stderr.
func runDispatch(t *testing.T, runner commandRunner, args ...string) (int, string, string) {
	t.Helper()
	adapter := &Adapter{buildRunner: runner}
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
// declared capabilities JSON and exits 0: hybrid deployment model, no
// activation phases, and the three build targets (TS-P7-20).
func TestRun_Capabilities(t *testing.T) {
	code, stdout, stderr := runDispatch(t, nil,
		contracts.CommandCapabilities, `{"framework":"flutter"}`)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}

	var result contracts.CapabilityResult
	decodeStdout(t, stdout, &result)

	if result.Declaration.DeploymentModel != string(contracts.DeploymentModelHybrid) {
		t.Errorf("DeploymentModel = %q, want %q", result.Declaration.DeploymentModel, contracts.DeploymentModelHybrid)
	}
	if len(result.Declaration.ActivationPhases) != 0 {
		t.Errorf("ActivationPhases = %v, want none (TS-P7-20 AC-5)", result.Declaration.ActivationPhases)
	}
	wantBuildPhases := []string{TargetWeb, TargetApk, TargetIos}
	if !reflectEqual(result.Declaration.BuildPhases, wantBuildPhases) {
		t.Errorf("BuildPhases = %v, want %v", result.Declaration.BuildPhases, wantBuildPhases)
	}
}

// TestRun_Build verifies the build command parses the working directory,
// runs the build pipeline through the runner, and prints the BuildResult
// JSON with exit 0 (TS-P7-21). The platform seam is set to darwin so all
// three targets are supported and run (TS-007-041).
func TestRun_Build(t *testing.T) {
	withPlatform(t, PlatformDarwin)
	runner := &fakeRunner{output: "ok"}
	workingDir := "/var/lib/anvil/projects/acme-app/builds/rel-1"

	payload, err := json.Marshal(contracts.BuildRequest{WorkingDir: workingDir})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	code, stdout, stderr := runDispatch(t, runner.run, contracts.CommandBuild, string(payload))
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}

	var result contracts.BuildResult
	decodeStdout(t, stdout, &result)

	if !result.Success {
		t.Fatalf("Success = false, want true (result: %#v)", result)
	}
	if len(result.Phases) != len(buildTargets) {
		t.Fatalf("Phases length = %d, want %d (result: %#v)", len(result.Phases), len(buildTargets), result)
	}
	if len(runner.args) != len(buildTargets) {
		t.Fatalf("runner invoked %d time(s), want %d", len(runner.args), len(buildTargets))
	}
	for i, target := range buildTargets {
		if result.Phases[i].Phase != target.Name {
			t.Errorf("Phases[%d].Phase = %q, want %q", i, result.Phases[i].Phase, target.Name)
		}
	}
	if len(runner.dirs) != len(buildTargets) || runner.dirs[0] != workingDir {
		t.Errorf("runner dirs = %v, want all %q", runner.dirs, workingDir)
	}
}

// TestRun_BuildFailureExitsZero verifies the exit-code semantics for
// build: a build with a failing target still exits 0 because a valid
// JSON result (with Success=false) was produced — the JSON result is
// authoritative for the build outcome (005-adapter-command-contract §7).
func TestRun_BuildFailureExitsZero(t *testing.T) {
	runner := &fakeRunner{err: fmt.Errorf("flutter build web failed: boom")}
	payload, err := json.Marshal(contracts.BuildRequest{WorkingDir: "/var/lib/anvil/projects/acme-app"})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	code, stdout, stderr := runDispatch(t, runner.run, contracts.CommandBuild, string(payload))
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d — a produced JSON result exits 0 (stderr: %s)", code, ExitOK, stderr)
	}
	var result contracts.BuildResult
	decodeStdout(t, stdout, &result)
	if result.Success {
		t.Error("Success = true, want false")
	}
	if len(result.Phases) != 1 || result.Phases[0].Error == "" {
		t.Errorf("Phases = %#v, want one failing target with error details", result.Phases)
	}
}

// TestRun_Extension verifies the extension command prints the declared
// configuration extension JSON — the two Flutter keys under the
// "framework.flutter." namespace (TS-P7-26) — and exits 0. The command
// must succeed because the Core registration path requires it (TS-P7-20
// AC-4, TS-P7-12).
func TestRun_Extension(t *testing.T) {
	code, stdout, stderr := runDispatch(t, nil,
		contracts.CommandConfigExtension, `{"framework":"flutter"}`)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}

	var result contracts.ConfigExtensionResult
	decodeStdout(t, stdout, &result)

	if result.Extension.Framework != Framework {
		t.Errorf("Extension.Framework = %q, want %q", result.Extension.Framework, Framework)
	}
	wantKeys := []string{KeyTargets, KeyBuildArgs}
	if len(result.Extension.Keys) != len(wantKeys) {
		t.Fatalf("Extension.Keys = %v, want %v (TS-P7-26)", result.Extension.Keys, wantKeys)
	}
	for i, key := range result.Extension.Keys {
		if key.Name != wantKeys[i] {
			t.Errorf("Extension.Keys[%d].Name = %q, want %q", i, key.Name, wantKeys[i])
		}
	}
}

// TestRun_ActivateNotSupported verifies the activate command is NOT
// supported: the hybrid deployment model has no server activation, so
// the dispatcher reports an unknown command with exit 2 (TS-P7-20 AC-5,
// EPIC-007 §7.3).
func TestRun_ActivateNotSupported(t *testing.T) {
	code, stdout, stderr := runDispatch(t, nil, contracts.CommandActivation, `{}`)
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitUsage, stderr)
	}
	if !strings.Contains(stderr, `unknown command "activate"`) {
		t.Errorf("stderr = %q, want mention of the unsupported activate command", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty for a failed dispatch", stdout)
	}
}

// TestRun_Verify verifies the verify command parses the check and
// artifact path, runs the verification check, and prints the
// VerificationOutcome JSON with exit 0 (TS-P7-25).
func TestRun_Verify(t *testing.T) {
	artifactDir := writeArtifactDir(t, "pubspec.yaml")

	payload, err := json.Marshal(contracts.VerificationRequest{
		Check:        CheckPubspecYaml,
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
	if outcome.Name != CheckPubspecYaml {
		t.Errorf("Name = %q, want %q", outcome.Name, CheckPubspecYaml)
	}
}

// TestRun_Validate verifies the validate command prints the validation
// result JSON and exits 0 — for both valid and invalid values (TS-P7-26).
func TestRun_Validate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		code, stdout, stderr := runDispatch(t, nil,
			contracts.CommandConfigValidation,
			`{"values":[{"key":"framework.flutter.targets","value":"web,apk"}]}`)
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
			`{"values":[{"key":"framework.flutter.targets","value":"winphone"}]}`)
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

// TestRun_UnknownCommand verifies that an unknown command exits with the
// usage code and a diagnostic on stderr (ADR-010 §8.1,
// 005-adapter-command-contract §7).
func TestRun_UnknownCommand(t *testing.T) {
	code, stdout, stderr := runDispatch(t, nil, "frobnicate", `{}`)
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
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
		contracts.CommandConfigExtension,
		contracts.CommandVerification,
		contracts.CommandConfigValidation,
		contracts.CommandBuild,
		contracts.CommandTemplate,
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
	code, _, stderr := runDispatch(t, nil, contracts.CommandBuild)
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

// TestAdapter_RunnerRequiredForBuild verifies that the injectable
// runner is what the dispatcher uses — a runner recording invocations
// proves the wiring end to end. The platform seam is set to darwin so
// all three targets are supported and run (TS-007-041).
func TestAdapter_RunnerRequiredForBuild(t *testing.T) {
	withPlatform(t, PlatformDarwin)
	calls := 0
	runner := func(_ context.Context, _ string, _ ...string) (string, error) {
		calls++
		return "ok", nil
	}

	payload, err := json.Marshal(contracts.BuildRequest{WorkingDir: "/var/lib/anvil/projects/acme-app"})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	code, stdout, _ := runDispatch(t, runner, contracts.CommandBuild, string(payload))
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	if calls != len(buildTargets) {
		t.Errorf("runner calls = %d, want %d", calls, len(buildTargets))
	}
	var result contracts.BuildResult
	decodeStdout(t, stdout, &result)
	if !result.Success {
		t.Errorf("Success = false, want true (result: %#v)", result)
	}
}

// TestRun_ManifestEmpty verifies the manifest command returns an EMPTY
// result: the hybrid deployment model has no server activation, so there
// are no activation or rollback commands to store in the artifact
// manifest (TS-P7-20 AC-5, ADR-016, 005-adapter-command-contract
// §10.10). The packaging layer omits the empty slices from the manifest.
func TestRun_ManifestEmpty(t *testing.T) {
	code, stdout, stderr := runDispatch(t, nil, contracts.CommandManifest)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}

	var result contracts.ManifestCommandResult
	decodeStdout(t, stdout, &result)

	if len(result.ActivationCommands) != 0 {
		t.Errorf("ActivationCommands = %v, want none", result.ActivationCommands)
	}
	if len(result.RollbackCommands) != 0 {
		t.Errorf("RollbackCommands = %v, want none", result.RollbackCommands)
	}
}

// TestRun_Template verifies the template command prints the adapter-owned
// pipeline definitions (build + ci) as valid JSON and exits 0 (TS-007-038,
// ADR-020 §1).
func TestRun_Template(t *testing.T) {
	code, stdout, stderr := runDispatch(t, nil,
		contracts.CommandTemplate, `{"framework":"flutter"}`)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}

	var result contracts.TemplateResult
	decodeStdout(t, stdout, &result)

	if result.Build == nil {
		t.Fatal("Build = nil, want the Flutter build definition")
	}
	if result.Build.Pipeline.Name != "build" {
		t.Errorf("Build.Pipeline.Name = %q, want %q", result.Build.Pipeline.Name, "build")
	}
	if len(result.Build.Pipeline.Stages) != 1 {
		t.Fatalf("Build stages = %d, want 1", len(result.Build.Pipeline.Stages))
	}
	tasks := result.Build.Pipeline.Stages[0].Tasks
	if len(tasks) != 3 {
		t.Fatalf("Build tasks = %d, want 3 (web, apk, ios)", len(tasks))
	}
	// The template must preserve the ADR-018 platform metadata.
	for _, task := range tasks {
		if task.Metadata == nil {
			t.Errorf("task %q Metadata = nil, want platform metadata (ADR-018)", task.Name)
		}
	}
	if result.CI == nil {
		t.Fatal("CI = nil, want the CI scaffold")
	}
	if result.CI.Pipeline.Name != "ci" {
		t.Errorf("CI.Pipeline.Name = %q, want %q", result.CI.Pipeline.Name, "ci")
	}
	// The returned definitions must pass the pipeline loader validation.
	if err := result.Build.Validate(); err != nil {
		t.Errorf("Build definition failed pipeline validation: %v", err)
	}
	if err := result.CI.Validate(); err != nil {
		t.Errorf("CI definition failed pipeline validation: %v", err)
	}
}

// TestRun_TemplateBuildMatchesBuildTargets verifies the single-source
// requirement (TS-007-038): the template's tasks mirror the commands of
// the adapter's build target table — the same framework knowledge must
// not drift between the template and the executed build phases.
func TestRun_TemplateBuildMatchesBuildTargets(t *testing.T) {
	code, stdout, stderr := runDispatch(t, nil,
		contracts.CommandTemplate, `{"framework":"flutter"}`)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}

	var result contracts.TemplateResult
	decodeStdout(t, stdout, &result)

	tasks := result.Build.Pipeline.Stages[0].Tasks
	if len(tasks) != len(buildTargets) {
		t.Fatalf("template tasks = %d, want %d", len(tasks), len(buildTargets))
	}
	for i, target := range buildTargets {
		task := tasks[i]
		if task.Name != "flutter-"+target.Name {
			t.Errorf("task %d name = %q, want %q", i, task.Name, "flutter-"+target.Name)
		}
		if task.Command != "flutter" {
			t.Errorf("task %q Command = %q, want %q", task.Name, task.Command, "flutter")
		}
		if !reflectEqual(task.Args, target.Args) {
			t.Errorf("task %q Args = %v, want the build target args %v", task.Name, task.Args, target.Args)
		}
		if task.Metadata == nil || task.Metadata.Target != target.Name {
			t.Errorf("task %q Metadata = %#v, want target %q", task.Name, task.Metadata, target.Name)
		}
		if task.Metadata == nil || !reflectEqual(task.Metadata.Platforms, target.Platforms) {
			t.Errorf("task %q Platforms = %v, want %v", task.Name, task.Metadata.Platforms, target.Platforms)
		}
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
