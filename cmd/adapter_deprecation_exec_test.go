package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ── TS-019-01-02: deprecation warning stream contract (exec-level) ───
//
// The in-process harness (executeCommand) wires SetOut on the root
// command, so cobra's deprecation notice — printed through
// OutOrStderr(), which prefers the SetOut writer — lands in the stdout
// buffer there. In the REAL binary no writer is set, OutOrStderr falls
// back to os.Stderr, and the warning must land on STDERR while stdout
// stays clean — otherwise machine-readable --json output would be
// polluted. This test builds the actual CLI binary and runs it, the
// exec-level insurance against a future SetOut/SetErr wiring change
// (product review note b).
//
// Reference: TS-019-01-02, TS-019-01-01, ADR-032

// buildAnvilTestBinary builds the real CLI binary into a temp dir and
// returns its path: a self-contained executable with no in-process
// seams — the exact artifact users run.
func buildAnvilTestBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "anvil")
	cmd := exec.Command("go", "build", "-o", bin, "maleolabs.com/anvil")
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build anvil binary: %v\n%s", err, out)
	}
	return bin
}

// emptyPathEnv returns the process environment with every PATH entry
// dropped and a single empty "PATH=" appended — explicit isolation of
// the child's executable search path — plus an isolated XDG_CONFIG_HOME
// pointing at a fresh temp dir, so the registry-driven surfaces
// (installed-standard records) never read the developer machine's real
// config state (TS-017-02-02: the post-gate installed view is
// registry-driven). os/exec's last-wins deduplication of duplicate keys
// is a hidden mechanism; filtering first makes the empty-PATH contract
// explicit (and case-insensitive, as Windows treats variable names
// case-insensitively).
func emptyPathEnv(t *testing.T) []string {
	t.Helper()
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if ok && strings.EqualFold(key, "PATH") {
			continue
		}
		if ok && strings.EqualFold(key, "XDG_CONFIG_HOME") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "PATH=", "XDG_CONFIG_HOME="+t.TempDir())
}

// runAnvilBinary runs the built binary with args in a fresh temp working
// directory and returns stdout, stderr, and the exit code. PATH is
// emptied and the config home isolated (emptyPathEnv) so adapter
// resolution never touches the developer machine's installed adapters
// or installed-standard records: the run is deterministic — nothing
// installed, nothing recorded.
func runAnvilBinary(t *testing.T, bin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = t.TempDir()
	cmd.Env = emptyPathEnv(t)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %v: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

// TestDeprecationWarning_RealBinaryWritesStderr is the exec-level guard
// for the deprecation warning stream contract: invoking a deprecated
// alias in the real binary prints the cobra notice on STDERR, stdout
// carries only the command's own output — and for --json only the
// machine-readable envelope (TS-019-01-02 note b).
func TestDeprecationWarning_RealBinaryWritesStderr(t *testing.T) {
	bin := buildAnvilTestBinary(t)

	// Machine-readable path: stdout must be exactly the JSON envelope.
	stdout, stderr, code := runAnvilBinary(t, bin, "adapter", "list", "--json")
	if code != 0 {
		t.Fatalf("'adapter list --json' must exit 0: %d (stderr: %s)", code, stderr)
	}
	if strings.Contains(stdout, `Command "list" is deprecated`) {
		t.Errorf("--json stdout must not carry the deprecation warning, got: %s", stdout)
	}
	if !strings.Contains(stderr, `Command "list" is deprecated`) {
		t.Errorf("deprecation warning must land on stderr, got: %s", stderr)
	}
	if !strings.Contains(stderr, "anvil standard list") {
		t.Errorf("stderr warning must name the replacement, got: %s", stderr)
	}
	var envelope struct {
		Version string `json:"version"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &envelope); err != nil {
		t.Fatalf("stdout must be the pure JSON envelope, got: %q (%v)", stdout, err)
	}
	if envelope.Version != "1" || envelope.Status != "success" {
		t.Errorf("envelope must be the TS-P8-05 shape, got: %s", stdout)
	}

	// Bare group invocation: group help on stdout, warning on stderr.
	stdout, stderr, code = runAnvilBinary(t, bin, "adapter")
	if code != 0 {
		t.Fatalf("bare 'adapter' must exit 0: %d (stderr: %s)", code, stderr)
	}
	if strings.Contains(stdout, `Command "adapter" is deprecated`) {
		t.Errorf("group help stdout must not carry the deprecation warning, got: %s", stdout)
	}
	if !strings.Contains(stdout, "DEPRECATED") {
		t.Errorf("group help must announce the deprecation, got: %s", stdout)
	}
	if !strings.Contains(stderr, `Command "adapter" is deprecated`) {
		t.Errorf("group deprecation warning must land on stderr, got: %s", stderr)
	}

	// Human-readable path: command output on stdout, warning on stderr.
	stdout, stderr, code = runAnvilBinary(t, bin, "adapter", "list")
	if code != 0 {
		t.Fatalf("'adapter list' must exit 0: %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "No adapters installed.") {
		t.Errorf("list output must stay on stdout, got: %s", stdout)
	}
	if strings.Contains(stdout, `Command "list" is deprecated`) {
		t.Errorf("plain stdout must not carry the deprecation warning, got: %s", stdout)
	}
	if !strings.Contains(stderr, `Command "list" is deprecated`) {
		t.Errorf("deprecation warning must land on stderr, got: %s", stderr)
	}
}
