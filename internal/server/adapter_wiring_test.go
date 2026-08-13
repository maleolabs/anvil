// Tests for the server coordinator's adapter wiring (TS-P7-09, TS-P7-10,
// TS-P7-11, TS-P7-12): activation phases, rollback operations, and
// verification checks are invoked through the generic adapter coordinator
// against a stub adapter executable, while projects without an adapter
// preserve the existing framework-agnostic behavior (ADR-009 §8.1, §9.6).
package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
)

// writeServerStubAdapter writes a stub adapter executable that appends
// one line per invocation to logPath and implements the adapter command
// contract. The stub declares two activation phases (migrate,
// config_cache) and two verification checks (vendor_present,
// config_files). failCheck names the verification check that reports
// failure (empty = all checks pass); failPhase names the activation phase
// whose activate operation reports failure (empty = all phases pass).
func writeServerStubAdapter(t *testing.T, logPath, failCheck, failPhase string) string {
	t.Helper()

	script := `#!/bin/sh
# Stub adapter for server wiring tests. Logs every invocation.
command="$1"
payload="$2"
log="` + logPath + `"
fail_check="` + failCheck + `"
fail_phase="` + failPhase + `"

json_field() {
  printf '%s' "$payload" | grep -o "\"$1\":\"[^\"]*\"" | head -1 | cut -d'"' -f4
}

echo "$command|$payload" >> "$log"

case "$command" in
  "capabilities")
    echo '{"capabilities":{"activation_phases":["migrate","config_cache"],"verification_checks":[{"name":"vendor_present","description":"vendor present"},{"name":"config_files","description":"config files present"}]}}'
    ;;
  "extension")
    echo '{"extension":{"framework":"laravel","keys":[{"name":"framework.laravel.migrations.path","description":"migration path","default":"database/migrations"},{"name":"framework.laravel.cache.store","description":"cache driver","default":"file"},{"name":"framework.laravel.version","description":"version constraint"}]}}'
    ;;
  "activate")
    phase=$(json_field phase)
    operation=$(json_field operation)
    if [ "$operation" = "activate" ] && [ "$phase" = "$fail_phase" ]; then
      echo '{"success":false,"error":"phase failed on purpose"}'
    else
      echo '{"success":true,"output":"stub phase ok"}'
    fi
    ;;
  "verify")
    check=$(json_field check)
    if [ "$check" = "$fail_check" ]; then
      echo "{\"name\":\"$check\",\"passed\":false,\"details\":\"check failed on purpose\"}"
    else
      echo "{\"name\":\"$check\",\"passed\":true,\"details\":\"stub check ok\"}"
    fi
    ;;
  *)
    echo "unknown command $command" >&2
    exit 2
    ;;
esac
exit 0
`

	path := filepath.Join(t.TempDir(), "server-stub-adapter.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	return path
}

// logLines returns the recorded stub invocations as "command|payload"
// lines, or nil when the log does not exist.
func logLines(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read stub log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// adapterCoordinatorSeams wires the coordinator under test to the stub
// adapter executable and the real Process Runner.
func adapterCoordinatorSeams(coord *ServerReleaseCoordinator, executable string) {
	coord.adapterRunner = execution.NewRunner()
	coord.adapterExecutable = func(framework string) (string, error) {
		return executable, nil
	}
}

// setupAdapterActivateEnvironment registers a project that selects
// adapterName and creates a complete activation environment (runtime
// directories, artifact in the store, Ready Release JSON). It returns the
// project ID and the release directory path.
func setupAdapterActivateEnvironment(t *testing.T, serverRoot, releaseID, adapterName string) (projectID, releaseDir string) {
	t.Helper()

	projectID = "test-project"
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Initialize server config.
	configStore := NewConfigStore(serverRoot)
	cfg := DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	// Register the project with the adapter selected.
	reg := DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "Test Project"
	reg.Project.Adapter = adapterName
	registryStore := NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot

	// Runtime directories + project state dirs.
	for _, d := range runtimeCfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	// Release state dir: unified layout the coordinator reads/writes
	// (<installRoot>/.anvil/state/releases — BUG-002).
	releasesStateDir := filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
	artifactStoreDir := filepath.Join(installRoot, "artifacts")
	for _, d := range []string{releasesStateDir, artifactStoreDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Release directory (as Install would create it).
	releasesDir := runtimeCfg.ReleasesDirPath()
	if _, err := runtime.CreateReleaseDir(releasesDir, releaseID); err != nil {
		t.Fatalf("create release dir: %v", err)
	}
	releaseDir = runtime.ReleaseDirPath(releasesDir, releaseID)

	// Artifact in the store + Ready Release JSON.
	sourceArtifact := createTestArtifact(t, projectID)
	storeArtifactPath := filepath.Join(artifactStoreDir, releaseID+".tar.gz")
	if err := copyFile(sourceArtifact, storeArtifactPath); err != nil {
		t.Fatalf("copy artifact to store: %v", err)
	}
	rel := &release.Release{
		ID:           release.ReleaseID(releaseID),
		ArtifactPath: storeArtifactPath,
		Stage:        release.StageReady,
		Transitions:  []release.TransitionRecord{},
	}
	releaseFilePath := filepath.Join(releasesStateDir, releaseID+".json")
	if err := rel.Save(releaseFilePath); err != nil {
		t.Fatalf("save release JSON: %v", err)
	}

	return projectID, releaseDir
}

// setupAdapterRollbackEnvironment registers a project that selects
// adapterName and creates a rollback environment: an Archived target
// Release (rollback target), an Active Release, both release
// directories, the active symlink, and runtime state. It returns the
// project ID, the active release ID, and the target release ID.
func setupAdapterRollbackEnvironment(t *testing.T, serverRoot, targetReleaseID, activeReleaseID, adapterName string) (projectID string) {
	t.Helper()

	projectID, installRoot := func() (string, string) {
		// Reuse the activation setup to register the project + runtime
		// dirs, then override the release fixtures below.
		p := "test-project"
		r := filepath.Join(serverRoot, "projects", p)
		return p, r
	}()

	// Register the project with the adapter selected (mirroring
	// setupAdapterActivateEnvironment's first half).
	configStore := NewConfigStore(serverRoot)
	cfg := DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}
	reg := DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "Test Project"
	reg.Project.Adapter = adapterName
	registryStore := NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot
	for _, d := range runtimeCfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	s := project.NewStructure(installRoot)
	releasesStateDir := filepath.Join(s.StateDir, "releases")
	if err := os.MkdirAll(releasesStateDir, 0755); err != nil {
		t.Fatalf("mkdir releases state dir: %v", err)
	}
	releasesDir := runtimeCfg.ReleasesDirPath()

	// Archived target Release (was previously Active).
	targetRel := &release.Release{
		ID:          release.ReleaseID(targetReleaseID),
		Stage:       release.StageArchived,
		Transitions: []release.TransitionRecord{},
	}
	targetRel.Transitions = append(targetRel.Transitions, release.TransitionRecord{
		Timestamp: "2026-07-28T10:00:00Z",
		From:      release.StageActive,
		To:        release.StageArchived,
		Outcome:   "success",
	})
	if err := targetRel.Save(filepath.Join(releasesStateDir, targetReleaseID+".json")); err != nil {
		t.Fatalf("save target release: %v", err)
	}
	targetDir := runtime.ReleaseDirPath(releasesDir, targetReleaseID)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("mkdir target release dir: %v", err)
	}

	// Active Release (the one that will be rolled back).
	activeRel := &release.Release{
		ID:          release.ReleaseID(activeReleaseID),
		Stage:       release.StageActive,
		Transitions: []release.TransitionRecord{},
	}
	if err := activeRel.Save(filepath.Join(releasesStateDir, activeReleaseID+".json")); err != nil {
		t.Fatalf("save active release: %v", err)
	}
	activeDir := runtime.ReleaseDirPath(releasesDir, activeReleaseID)
	if err := os.MkdirAll(activeDir, 0755); err != nil {
		t.Fatalf("mkdir active release dir: %v", err)
	}

	// Symlink points at the Active Release (SwitchForRollback needs it).
	switcher := runtime.NewSymlinkSwitcher(runtimeCfg)
	if err := switcher.SwitchTo(activeDir); err != nil {
		t.Fatalf("switch symlink to active release: %v", err)
	}

	// Runtime state names the Active Release.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	stateStore.SetActiveRelease(activeReleaseID)
	stateStore.SetRuntimeCondition(runtime.ConditionNormal)
	stateStore.SetSharedResourceStatus(runtime.ResourceAccessible)
	if err := stateStore.Save(); err != nil {
		t.Fatalf("save runtime state: %v", err)
	}

	return projectID
}

// ---------------------------------------------------------------------------
// Activation wiring (TS-P7-09)
// ---------------------------------------------------------------------------

// TestActivate_AdapterPhasesInvoked verifies that activation invokes each
// declared adapter phase via the Process Runner with the release
// directory as working dir, and succeeds when all phases pass.
func TestActivate_AdapterPhasesInvoked(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-adapter-activate"
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerStubAdapter(t, logPath, "", "")

	projectID, releaseDir := setupAdapterActivateEnvironment(t, serverRoot, releaseID, "laravel")

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	if err := coord.Activate(projectID, releaseID); err != nil {
		t.Fatalf("Activate returned unexpected error: %v", err)
	}

	lines := logLines(t, logPath)
	if len(lines) == 0 {
		t.Fatal("stub adapter was never invoked")
	}
	var activatePhases []string
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 || parts[0] != "activate" {
			continue
		}
		if !strings.Contains(parts[1], `"operation":"activate"`) {
			t.Errorf("activate invocation %q is not an activate operation", line)
		}
		if !strings.Contains(parts[1], `"working_dir":"`+releaseDir+`"`) {
			t.Errorf("activate invocation %q does not carry working_dir %q", line, releaseDir)
		}
		if !strings.Contains(parts[1], `"release_id":"`+releaseID+`"`) {
			t.Errorf("activate invocation %q does not carry release_id %q", line, releaseID)
		}
		activatePhases = append(activatePhases, phaseField(t, parts[1]))
	}

	// Both declared phases were invoked, in declaration order.
	want := []string{"migrate", "config_cache"}
	if strings.Join(activatePhases, ",") != strings.Join(want, ",") {
		t.Errorf("invoked phases = %v, want %v", activatePhases, want)
	}
}

// TestActivate_AdapterPhaseFailureFailsActivation verifies that a phase
// reporting failure fails the activation with a descriptive error.
func TestActivate_AdapterPhaseFailureFailsActivation(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-adapter-fail"
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerStubAdapter(t, logPath, "", "migrate")

	projectID, _ := setupAdapterActivateEnvironment(t, serverRoot, releaseID, "laravel")

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	err := coord.Activate(projectID, releaseID)
	if err == nil {
		t.Fatal("Activate succeeded, want error for a failing phase")
	}
	if !strings.Contains(err.Error(), "adapter activation failed") {
		t.Errorf("error %q does not mention the adapter activation failure", err)
	}
	if !strings.Contains(err.Error(), `phase "migrate"`) {
		t.Errorf("error %q does not mention the failing phase", err)
	}
}

// TestActivate_NoAdapterPreservesBehavior verifies that a project without
// an adapter never invokes the adapter — existing behavior is preserved
// (ADR-009 §9.7). The stub would fail loudly if invoked.
func TestActivate_NoAdapterPreservesBehavior(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-no-adapter"
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerStubAdapter(t, logPath, "", "")

	projectID, _ := setupAdapterActivateEnvironment(t, serverRoot, releaseID, "")

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	if err := coord.Activate(projectID, releaseID); err != nil {
		t.Fatalf("Activate returned unexpected error: %v", err)
	}
	if lines := logLines(t, logPath); lines != nil {
		t.Errorf("stub adapter was invoked for a project without an adapter: %v", lines)
	}
}

// TestActivate_AdapterExecutableMissing verifies that a project selecting
// an adapter whose executable cannot be resolved fails activation with a
// descriptive error.
func TestActivate_AdapterExecutableMissing(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-missing-exe"

	projectID, _ := setupAdapterActivateEnvironment(t, serverRoot, releaseID, "no-such-framework-xyz")

	// No seams: the default convention resolver
	// (exec.LookPath("anvil-adapter-no-such-framework-xyz")) fails.
	coord := NewServerReleaseCoordinator(serverRoot)

	err := coord.Activate(projectID, releaseID)
	if err == nil {
		t.Fatal("Activate succeeded, want error for a missing adapter executable")
	}
	if !strings.Contains(err.Error(), `anvil-adapter-no-such-framework-xyz`) {
		t.Errorf("error %q does not mention the missing executable name", err)
	}
}

// ---------------------------------------------------------------------------
// Rollback wiring (TS-P7-10)
// ---------------------------------------------------------------------------

// TestRollback_AdapterRollbackInvoked verifies that rollback invokes each
// declared phase with the rollback operation, the rolled-back release ID,
// and the restored release directory as working dir, and succeeds.
func TestRollback_AdapterRollbackInvoked(t *testing.T) {
	serverRoot := t.TempDir()
	targetReleaseID := "rel-target-001"
	activeReleaseID := "rel-active-002"
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerStubAdapter(t, logPath, "", "")

	projectID := setupAdapterRollbackEnvironment(t, serverRoot, targetReleaseID, activeReleaseID, "laravel")

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	if _, err := coord.Rollback(projectID); err != nil {
		t.Fatalf("Rollback returned unexpected error: %v", err)
	}

	lines := logLines(t, logPath)
	if len(lines) == 0 {
		t.Fatal("stub adapter was never invoked")
	}

	// Restored release directory = the rollback target's directory.
	reg, err := NewRegistryStore(serverRoot).Load(projectID)
	if err != nil {
		t.Fatalf("load project registry: %v", err)
	}
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = reg.Project.InstallRoot
	restoredDir := runtime.ReleaseDirPath(runtimeCfg.ReleasesDirPath(), targetReleaseID)

	var rollbackPhases []string
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 || parts[0] != "activate" {
			continue
		}
		if !strings.Contains(parts[1], `"operation":"rollback"`) {
			t.Errorf("invocation %q is not a rollback operation", line)
		}
		if !strings.Contains(parts[1], `"working_dir":"`+restoredDir+`"`) {
			t.Errorf("rollback invocation %q does not carry working_dir %q", line, restoredDir)
		}
		if !strings.Contains(parts[1], `"release_id":"`+activeReleaseID+`"`) {
			t.Errorf("rollback invocation %q does not carry the rolled-back release_id %q", line, activeReleaseID)
		}
		rollbackPhases = append(rollbackPhases, phaseField(t, parts[1]))
	}

	want := []string{"migrate", "config_cache"}
	if strings.Join(rollbackPhases, ",") != strings.Join(want, ",") {
		t.Errorf("rollback phases = %v, want %v", rollbackPhases, want)
	}
}

// TestRollback_AdapterPhaseRollbackFailureFailsRollback verifies that a
// rollback operation reporting failure fails the rollback (only
// irreversible phases may report success without undoing — see the
// Laravel standard's activation tests, now in the anvil-standard-laravel
// repository, for the irreversibility handling).
func TestRollback_AdapterPhaseRollbackFailureFailsRollback(t *testing.T) {
	serverRoot := t.TempDir()
	targetReleaseID := "rel-target-001"
	activeReleaseID := "rel-active-002"
	logPath := filepath.Join(t.TempDir(), "stub.log")

	// Dedicated stub: declares a single phase (migrate) and fails every
	// rollback operation.
	failingStub := `#!/bin/sh
command="$1"
payload="$2"
log="` + logPath + `"
echo "$command|$payload" >> "$log"
case "$command" in
  "capabilities") echo '{"capabilities":{"activation_phases":["migrate"]}}' ;;
  "extension") echo '{"extension":{"framework":"laravel","keys":[]}}' ;;
  "activate")
    case "$(printf '%s' "$payload" | grep -o '"operation":"[^"]*"' | cut -d'"' -f4)" in
      "rollback") echo '{"success":false,"error":"migrate rollback failed on purpose"}' ;;
      *) echo '{"success":true,"output":"ok"}' ;;
    esac
    ;;
  *) exit 2 ;;
esac
exit 0
`
	executable := filepath.Join(t.TempDir(), "failing-rollback-stub.sh")
	if err := os.WriteFile(executable, []byte(failingStub), 0o755); err != nil {
		t.Fatalf("write failing stub: %v", err)
	}

	projectID := setupAdapterRollbackEnvironment(t, serverRoot, targetReleaseID, activeReleaseID, "laravel")

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	_, err := coord.Rollback(projectID)
	if err == nil {
		t.Fatal("Rollback succeeded, want error for a failing rollback phase")
	}
	if !strings.Contains(err.Error(), "adapter rollback failed") {
		t.Errorf("error %q does not mention the adapter rollback failure", err)
	}
}

// ---------------------------------------------------------------------------
// Verification wiring (TS-P7-11)
// ---------------------------------------------------------------------------

// TestInstall_AdapterVerificationChecks verifies that the adapter's
// declared verification checks run during installation and pass.
func TestInstall_AdapterVerificationChecks(t *testing.T) {
	serverRoot := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerStubAdapter(t, logPath, "", "")

	projectID, _ := setupAdapterActivateEnvironment(t, serverRoot, "rel-install-verify", "laravel")
	artifactPath := createTestArtifact(t, projectID)

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	if _, err := coord.Install(projectID, artifactPath); err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}

	lines := logLines(t, logPath)
	var checks []string
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 && parts[0] == "verify" {
			checks = append(checks, phaseField(t, parts[1]))
		}
	}
	want := []string{"vendor_present", "config_files"}
	if strings.Join(checks, ",") != strings.Join(want, ",") {
		t.Errorf("invoked verification checks = %v, want %v", checks, want)
	}
}

// TestInstall_AdapterVerificationFailure verifies that a failing adapter
// verification check blocks the installation with a descriptive error.
func TestInstall_AdapterVerificationFailure(t *testing.T) {
	serverRoot := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerStubAdapter(t, logPath, "config_files", "")

	projectID, _ := setupAdapterActivateEnvironment(t, serverRoot, "rel-install-fail", "laravel")
	artifactPath := createTestArtifact(t, projectID)

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	_, err := coord.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("Install succeeded, want error for a failing adapter check")
	}
	if !strings.Contains(err.Error(), "adapter verification failed") {
		t.Errorf("error %q does not mention the adapter verification failure", err)
	}
	if !strings.Contains(err.Error(), "config_files") {
		t.Errorf("error %q does not mention the failing check", err)
	}
}

// TestInstall_NoAdapterNoAdapterChecks verifies that installation without
// an adapter never invokes the adapter — existing behavior is preserved.
func TestInstall_NoAdapterNoAdapterChecks(t *testing.T) {
	serverRoot := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerStubAdapter(t, logPath, "", "")

	projectID, _ := setupAdapterActivateEnvironment(t, serverRoot, "rel-install-none", "")
	artifactPath := createTestArtifact(t, projectID)

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	if _, err := coord.Install(projectID, artifactPath); err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}
	if lines := logLines(t, logPath); lines != nil {
		t.Errorf("stub adapter was invoked for a project without an adapter: %v", lines)
	}
}

// phaseField extracts the "phase"/"check" value from a stub payload line.
func phaseField(t *testing.T, payload string) string {
	t.Helper()
	idx := strings.Index(payload, `"phase":"`)
	if idx >= 0 {
		rest := payload[idx+len(`"phase":"`):]
		end := strings.Index(rest, `"`)
		if end < 0 {
			t.Fatalf("cannot parse phase from payload %q", payload)
		}
		return rest[:end]
	}
	idx = strings.Index(payload, `"check":"`)
	if idx >= 0 {
		rest := payload[idx+len(`"check":"`):]
		end := strings.Index(rest, `"`)
		if end < 0 {
			t.Fatalf("cannot parse check from payload %q", payload)
		}
		return rest[:end]
	}
	t.Fatalf("payload %q carries neither phase nor check", payload)
	return ""
}

// ---------------------------------------------------------------------------
// Build wiring (TS-007-040)
// ---------------------------------------------------------------------------

// writeServerBuildStubAdapter writes a stub adapter executable that
// appends one line per invocation to logPath and implements the build
// portion of the adapter command contract. The stub declares two build
// phases (composer, npm). failPhase selects the failing variant: the npm
// phase reports failure with output details (empty = all phases pass).
// skipPhase selects the skipping variant: the npm phase reports a
// graceful skip with a warning (empty = no phase skipped). failPhase
// takes precedence over skipPhase.
func writeServerBuildStubAdapter(t *testing.T, logPath, failPhase, skipPhase string) string {
	t.Helper()

	var buildVariant string
	switch {
	case failPhase != "":
		buildVariant = `echo '{"phases":[{"phase":"composer","success":true,"output":"composer ok"},{"phase":"npm","success":false,"error":"npm build failed on purpose","output":"npm error output"}],"success":false}'`
	case skipPhase != "":
		buildVariant = `echo '{"phases":[{"phase":"composer","success":true,"output":"composer ok"},{"phase":"npm","success":true,"skipped":true,"warning":"target \"apk\" is not supported on platform \"linux\""}],"success":true}'`
	default:
		buildVariant = `echo '{"phases":[{"phase":"composer","success":true,"output":"composer ok"},{"phase":"npm","success":true,"output":"npm ok"}],"success":true}'`
	}

	script := `#!/bin/sh
# Stub adapter for server build wiring tests. Logs every invocation.
command="$1"
payload="$2"
log="` + logPath + `"
echo "$command|$payload" >> "$log"
case "$command" in
  "capabilities")
    echo '{"capabilities":{"build_phases":["composer","npm"]}}'
    ;;
  "extension")
    echo '{"extension":{"framework":"laravel","keys":[]}}'
    ;;
  "build")
    ` + buildVariant + `
    ;;
  *)
    echo "unknown command $command" >&2
    exit 2
    ;;
esac
exit 0
`

	path := filepath.Join(t.TempDir(), "server-build-stub-adapter.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	return path
}

// setupAdapterBuildEnvironment registers a project that selects
// adapterName with its runtime directory structure and returns the
// project ID and install root.
// setupAdapterBuildEnvironment registers a project that selects
// adapterName and creates a complete build environment. It returns the
// project ID and the install root.
func setupAdapterBuildEnvironment(t *testing.T, serverRoot, adapterName string) (projectID, installRoot string) {
	t.Helper()

	projectID = "test-project"
	installRoot = filepath.Join(serverRoot, "projects", projectID)

	// Initialize server config.
	configStore := NewConfigStore(serverRoot)
	cfg := DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	// Register the project with the adapter selected.
	reg := DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "Test Project"
	reg.Project.Adapter = adapterName
	registryStore := NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	// Runtime directories (the build runs in the install root).
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot
	for _, d := range runtimeCfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	return projectID, installRoot
}

// setupStandardBuildEnvironment registers a project declaring the
// canonical project.standard key (TS-019-02-01, ADR-032) and creates a
// complete build environment. It returns the project ID and the install
// root.
func setupStandardBuildEnvironment(t *testing.T, serverRoot, standardName string) (projectID, installRoot string) {
	t.Helper()

	projectID = "test-project"
	installRoot = filepath.Join(serverRoot, "projects", projectID)

	// Initialize server config.
	configStore := NewConfigStore(serverRoot)
	cfg := DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	// Register the project with the canonical standard key.
	reg := DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "Test Project"
	reg.Project.Standard = standardName
	registryStore := NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	// Runtime directories (the build runs in the install root).
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot
	for _, d := range runtimeCfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	return projectID, installRoot
}

// buildInvocation returns the payload of the first "build" invocation in
// the stub log, or "" when the stub never received a build command.
func buildInvocation(t *testing.T, logPath string) string {
	t.Helper()
	for _, line := range logLines(t, logPath) {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 && parts[0] == "build" {
			return parts[1]
		}
	}
	return ""
}

// TestBuildRelease_InvokesAdapterBuild verifies that a server release
// build invokes the adapter `build` command with the project install
// root as working directory, passes Targets/Strict through to the
// request, and reports every phase.
func TestBuildRelease_InvokesAdapterBuild(t *testing.T) {
	serverRoot := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerBuildStubAdapter(t, logPath, "", "")

	projectID, installRoot := setupAdapterBuildEnvironment(t, serverRoot, "laravel")

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	result, err := coord.BuildRelease(context.Background(), projectID, BuildReleaseOptions{
		Targets: []string{"npm"},
		Strict:  true,
	})
	if err != nil {
		t.Fatalf("BuildRelease returned unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("BuildRelease.Success = false, want true")
	}
	if len(result.Phases) != 2 {
		t.Fatalf("BuildRelease phases = %d, want 2", len(result.Phases))
	}

	payload := buildInvocation(t, logPath)
	if payload == "" {
		t.Fatal("stub adapter build command was never invoked")
	}
	if !strings.Contains(payload, `"working_dir":"`+installRoot+`"`) {
		t.Errorf("build invocation %q does not carry working_dir %q", payload, installRoot)
	}
	if !strings.Contains(payload, `"targets":["npm"]`) {
		t.Errorf("build invocation %q does not carry targets [npm]", payload)
	}
	if !strings.Contains(payload, `"strict":true`) {
		t.Errorf("build invocation %q does not carry strict=true", payload)
	}
}

// TestBuildRelease_CanonicalStandardKeyHonored verifies that a server
// release build honors the canonical project.standard key when present
// (TS-019-02-01, ADR-032): a project declaring project.standard resolves
// to the declared standard and the adapter build pipeline is invoked.
func TestBuildRelease_CanonicalStandardKeyHonored(t *testing.T) {
	serverRoot := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerBuildStubAdapter(t, logPath, "", "")

	projectID, installRoot := setupStandardBuildEnvironment(t, serverRoot, "laravel")

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	result, err := coord.BuildRelease(context.Background(), projectID, BuildReleaseOptions{})
	if err != nil {
		t.Fatalf("BuildRelease returned unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("BuildRelease.Success = false, want true")
	}
	if len(result.Phases) != 2 {
		t.Fatalf("BuildRelease phases = %d, want 2", len(result.Phases))
	}

	payload := buildInvocation(t, logPath)
	if payload == "" {
		t.Fatal("stub adapter build command was never invoked for a project.standard declaration")
	}
	if !strings.Contains(payload, `"working_dir":"`+installRoot+`"`) {
		t.Errorf("build invocation %q does not carry working_dir %q", payload, installRoot)
	}
}

// TestBuildRelease_LegacyAdapterKeyStillHonored verifies backward
// compatibility (TS-019-02-01): a project that still declares the legacy
// project.adapter key resolves to the same adapter build pipeline — the
// canonical-key adoption must not break existing projects.
func TestBuildRelease_LegacyAdapterKeyStillHonored(t *testing.T) {
	serverRoot := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerBuildStubAdapter(t, logPath, "", "")

	projectID, _ := setupAdapterBuildEnvironment(t, serverRoot, "laravel")

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	result, err := coord.BuildRelease(context.Background(), projectID, BuildReleaseOptions{})
	if err != nil {
		t.Fatalf("BuildRelease returned unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("BuildRelease.Success = false, want true")
	}
	if payload := buildInvocation(t, logPath); payload == "" {
		t.Fatal("stub adapter build command was never invoked for a project.adapter declaration")
	}
}

// TestBuildRelease_NoAdapterDescriptiveError verifies that a project
// without a framework adapter fails with a descriptive error — the
// adapter build never silently falls back to a generic build.
func TestBuildRelease_NoAdapterDescriptiveError(t *testing.T) {
	serverRoot := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerBuildStubAdapter(t, logPath, "", "")

	projectID, _ := setupAdapterBuildEnvironment(t, serverRoot, "")

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	_, err := coord.BuildRelease(context.Background(), projectID, BuildReleaseOptions{})
	if err == nil {
		t.Fatal("BuildRelease succeeded, want error for a project without an adapter")
	}
	if !strings.Contains(err.Error(), "selects no framework adapter") {
		t.Errorf("error %q does not mention the missing framework adapter", err)
	}
	if lines := logLines(t, logPath); lines != nil {
		t.Errorf("stub adapter was invoked for a project without an adapter: %v", lines)
	}
}

// TestBuildRelease_AdapterExecutableMissing verifies that a project
// selecting an adapter whose executable cannot be resolved fails the
// build with a descriptive error naming the missing binary.
func TestBuildRelease_AdapterExecutableMissing(t *testing.T) {
	serverRoot := t.TempDir()

	projectID, _ := setupAdapterBuildEnvironment(t, serverRoot, "no-such-framework-xyz")

	// No seams: the default convention resolver
	// (exec.LookPath("anvil-adapter-no-such-framework-xyz")) fails.
	coord := NewServerReleaseCoordinator(serverRoot)

	_, err := coord.BuildRelease(context.Background(), projectID, BuildReleaseOptions{})
	if err == nil {
		t.Fatal("BuildRelease succeeded, want error for a missing adapter executable")
	}
	if !strings.Contains(err.Error(), `anvil-adapter-no-such-framework-xyz`) {
		t.Errorf("error %q does not mention the missing executable name", err)
	}
	if !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("error %q does not mention the PATH resolution failure", err)
	}
}

// TestBuildRelease_FailingPhaseStopsBuild verifies that a failing build
// phase fails the build with an error naming the first failing phase and
// its actionable output details, while the returned report still carries
// every phase for observability.
func TestBuildRelease_FailingPhaseStopsBuild(t *testing.T) {
	serverRoot := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerBuildStubAdapter(t, logPath, "npm", "")

	projectID, _ := setupAdapterBuildEnvironment(t, serverRoot, "laravel")

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	result, err := coord.BuildRelease(context.Background(), projectID, BuildReleaseOptions{})
	if err == nil {
		t.Fatal("BuildRelease succeeded, want error for a failing build phase")
	}
	if !strings.Contains(err.Error(), "adapter build failed") {
		t.Errorf("error %q does not mention the adapter build failure", err)
	}
	if !strings.Contains(err.Error(), `phase "npm"`) {
		t.Errorf("error %q does not mention the first failing phase", err)
	}
	if !strings.Contains(err.Error(), "npm build failed on purpose") {
		t.Errorf("error %q does not carry the failing phase's output details", err)
	}
	// The report is populated even on failure.
	if result == nil || result.Success {
		t.Fatalf("BuildRelease report = %+v, want Success=false", result)
	}
	if len(result.Phases) != 2 {
		t.Errorf("BuildRelease phases = %d, want 2 (all phases reported)", len(result.Phases))
	}
}

// TestBuildRelease_SkippedPhaseReported verifies that skipped phases are
// reported in the result payload: a skipped phase keeps the build
// successful and carries its warning.
func TestBuildRelease_SkippedPhaseReported(t *testing.T) {
	serverRoot := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerBuildStubAdapter(t, logPath, "", "npm")

	projectID, _ := setupAdapterBuildEnvironment(t, serverRoot, "laravel")

	coord := NewServerReleaseCoordinator(serverRoot)
	adapterCoordinatorSeams(coord, executable)

	result, err := coord.BuildRelease(context.Background(), projectID, BuildReleaseOptions{})
	if err != nil {
		t.Fatalf("BuildRelease returned unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("BuildRelease.Success = false, want true (a skip is not a failure)")
	}

	var skipped *contracts.BuildPhaseResult
	for i := range result.Phases {
		if result.Phases[i].Skipped {
			skipped = &result.Phases[i]
			break
		}
	}
	if skipped == nil {
		t.Fatal("BuildRelease report does not include a skipped phase")
	}
	if skipped.Phase != "npm" {
		t.Errorf("skipped phase = %q, want %q", skipped.Phase, "npm")
	}
	if !skipped.Success {
		t.Error("skipped phase Success = false, want true")
	}
	if skipped.Warning == "" {
		t.Error("skipped phase carries no warning")
	}
}
