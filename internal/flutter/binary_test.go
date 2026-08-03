// End-to-end test of the compiled Flutter adapter executable. The
// dispatcher is exercised in-process by command_test.go; this test builds
// the actual binary (cmd/flutter-adapter) with `go build` and invokes it
// as a subprocess, proving the executable entrypoint, the JSON I/O, and
// the exit-code convention work end to end (004-review-resolutions D1).
// The binary name mirrors the convention `anvil-adapter-<framework>`
// (005-adapter-command-contract §10).
package flutter

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// buildAdapterBinary compiles the adapter executable into a temp dir and
// returns its path. The module root is located by walking up from this
// test file to the go.mod.
func buildAdapterBinary(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test file")
		}
		dir = parent
	}

	bin := filepath.Join(t.TempDir(), "anvil-adapter-flutter")
	cmd := exec.Command("go", "build", "-o", bin, "maleolabs.com/anvil/cmd/flutter-adapter")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build adapter binary: %v\n%s", err, out)
	}
	return bin
}

// runBinary invokes the built adapter executable with the given arguments
// and returns its exit code, stdout, and stderr.
func runBinary(t *testing.T, bin string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run adapter binary: %v", err)
		}
	}
	return code, stdout.String(), stderr.String()
}

// TestBinary_EndToEnd verifies the compiled executable: capabilities,
// extension, and build produce their JSON results with exit 0, activate
// is rejected (hybrid model — no server activation), and an unknown
// command exits non-zero.
func TestBinary_EndToEnd(t *testing.T) {
	bin := buildAdapterBinary(t)

	t.Run("capabilities", func(t *testing.T) {
		code, stdout, stderr := runBinary(t, bin, contracts.CommandCapabilities, `{"framework":"flutter"}`)
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
		}
		var result contracts.CapabilityResult
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("stdout %q is not valid JSON: %v", stdout, err)
		}
		if result.Declaration.DeploymentModel != string(contracts.DeploymentModelHybrid) {
			t.Errorf("DeploymentModel = %q, want %q", result.Declaration.DeploymentModel, contracts.DeploymentModelHybrid)
		}
		if len(result.Declaration.ActivationPhases) != 0 {
			t.Errorf("ActivationPhases = %v, want none (TS-P7-20 AC-5)", result.Declaration.ActivationPhases)
		}
		if len(result.Declaration.BuildPhases) != len(buildTargets) {
			t.Errorf("BuildPhases length = %d, want %d", len(result.Declaration.BuildPhases), len(buildTargets))
		}
	})

	t.Run("extension", func(t *testing.T) {
		code, stdout, stderr := runBinary(t, bin, contracts.CommandConfigExtension, `{"framework":"flutter"}`)
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
		}
		var result contracts.ConfigExtensionResult
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("stdout %q is not valid JSON: %v", stdout, err)
		}
		if result.Extension.Framework != "flutter" {
			t.Errorf("Extension.Framework = %q, want %q", result.Extension.Framework, "flutter")
		}
		if len(result.Extension.Keys) != 2 {
			t.Errorf("Extension.Keys = %v, want the two TS-P7-26 keys (framework.flutter.targets, framework.flutter.build_args)", result.Extension.Keys)
		}
	})

	t.Run("build", func(t *testing.T) {
		// The build runs the production flutter runner; the working
		// directory is a nonexistent path so the child process fails
		// at start — deterministically, without a Flutter toolchain or
		// a Flutter project on the test host. The process still exits
		// 0 with a valid JSON result — the JSON result is
		// authoritative (005-adapter-command-contract §7).
		code, stdout, stderr := runBinary(t, bin, contracts.CommandBuild, `{"working_dir":"/nonexistent"}`)
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
		}
		var result contracts.BuildResult
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("stdout %q is not valid JSON: %v", stdout, err)
		}
		if len(result.Phases) != 1 {
			t.Errorf("Phases length = %d, want 1 — the pipeline stops at the first failing target", len(result.Phases))
		}
		if result.Phases[0].Phase != TargetWeb {
			t.Errorf("Phases[0].Phase = %q, want %q", result.Phases[0].Phase, TargetWeb)
		}
	})

	t.Run("activate_rejected", func(t *testing.T) {
		code, stdout, _ := runBinary(t, bin, contracts.CommandActivation, `{}`)
		if code != ExitUsage {
			t.Errorf("exit code = %d, want %d — activate is not supported (TS-P7-20 AC-5)", code, ExitUsage)
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want empty for a failed dispatch", stdout)
		}
	})

	t.Run("unknown_command", func(t *testing.T) {
		code, _, _ := runBinary(t, bin, "frobnicate", `{}`)
		if code == ExitOK {
			t.Fatal("exit code = 0, want non-zero for an unknown command")
		}
	})

	t.Run("malformed_json", func(t *testing.T) {
		code, _, _ := runBinary(t, bin, contracts.CommandCapabilities, "{not-json")
		if code == ExitOK {
			t.Fatal("exit code = 0, want non-zero for a malformed payload")
		}
	})
}
