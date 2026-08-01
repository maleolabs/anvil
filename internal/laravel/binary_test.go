// End-to-end test of the compiled Laravel adapter executable. The
// dispatcher is exercised in-process by command_test.go; this test builds
// the actual binary (cmd/laravel-adapter) with `go build` and invokes it
// as a subprocess, proving the executable entrypoint, the JSON I/O, and
// the exit-code convention work end to end (004-review-resolutions D1).
package laravel

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

	bin := filepath.Join(t.TempDir(), "anvil-adapter-laravel")
	cmd := exec.Command("go", "build", "-o", bin, "maleolabs.com/anvil/cmd/laravel-adapter")
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
// verify, extension, and validate produce their JSON results with exit 0,
// and an unknown command exits non-zero.
func TestBinary_EndToEnd(t *testing.T) {
	bin := buildAdapterBinary(t)

	t.Run("capabilities", func(t *testing.T) {
		code, stdout, stderr := runBinary(t, bin, contracts.CommandCapabilities, `{"framework":"laravel"}`)
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
		}
		var result contracts.CapabilityResult
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("stdout %q is not valid JSON: %v", stdout, err)
		}
		if len(result.Declaration.ActivationPhases) != 4 {
			t.Errorf("ActivationPhases length = %d, want 4", len(result.Declaration.ActivationPhases))
		}
	})

	t.Run("verify", func(t *testing.T) {
		artifactDir := writeArtifactDir(t, "vendor/autoload.php", "bootstrap/app.php", "config/app.php", ".env.example")
		payload, err := json.Marshal(contracts.VerificationRequest{
			Check:        CheckConfigFiles,
			ArtifactPath: artifactDir,
		})
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		code, stdout, stderr := runBinary(t, bin, contracts.CommandVerification, string(payload))
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
		}
		var outcome contracts.VerificationOutcome
		if err := json.Unmarshal([]byte(stdout), &outcome); err != nil {
			t.Fatalf("stdout %q is not valid JSON: %v", stdout, err)
		}
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
	})

	t.Run("extension", func(t *testing.T) {
		code, stdout, stderr := runBinary(t, bin, contracts.CommandConfigExtension, `{"framework":"laravel"}`)
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
		}
		var result contracts.ConfigExtensionResult
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("stdout %q is not valid JSON: %v", stdout, err)
		}
		if len(result.Extension.Keys) != 3 {
			t.Errorf("Extension.Keys length = %d, want 3", len(result.Extension.Keys))
		}
	})

	t.Run("validate", func(t *testing.T) {
		code, stdout, stderr := runBinary(t, bin, contracts.CommandConfigValidation,
			`{"values":[{"key":"framework.laravel.cache.store","value":"nope"}]}`)
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
		}
		var result contracts.ConfigValidationResult
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("stdout %q is not valid JSON: %v", stdout, err)
		}
		if result.Valid {
			t.Error("Valid = true, want false for an unknown cache store")
		}
	})

	t.Run("unknown_command", func(t *testing.T) {
		code, _, _ := runBinary(t, bin, "frobnicate", `{}`)
		if code == ExitOK {
			t.Fatal("exit code = 0, want non-zero for an unknown command")
		}
	})

	t.Run("malformed_json", func(t *testing.T) {
		code, _, _ := runBinary(t, bin, contracts.CommandActivation, "{not-json")
		if code == ExitOK {
			t.Fatal("exit code = 0, want non-zero for a malformed payload")
		}
	})
}
