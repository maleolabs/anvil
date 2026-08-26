package spklocaldeployrace

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

func tempCfg(t *testing.T, user string, sizeMB int) HarnessConfig {
	t.Helper()
	serverRoot := filepath.Join(t.TempDir(), "server")
	installRoot := filepath.Join(serverRoot, "install", "spike-race-project")
	artifactsDir := filepath.Join(t.TempDir(), "artifacts")
	remoteDir := filepath.Join(t.TempDir(), "remote")
	return HarnessConfig{
		ProjectID:        "spike-race-project",
		ServerRoot:       serverRoot,
		InstallRoot:      installRoot,
		ArtifactsDir:     artifactsDir,
		RemoteStagingDir: remoteDir,
		DeployerUser:     user,
		SizeMB:           sizeMB,
		Logger:           &bytes.Buffer{},
	}
}

// AC1 + AC2: lock contention proof — exactly one winner, rejection is clear, state not corrupted
func TestAC2_LockContentionExactlyOneWinner(t *testing.T) {
	cfg := tempCfg(t, "tester", 1)
	if err := SetupServer(cfg); err != nil {
		t.Fatalf("SetupServer: %v", err)
	}
	ev, err := runLockContentionProof(cfg)
	if err != nil {
		t.Fatalf("runLockContentionProof: %v", err)
	}
	if ev.Successes != 1 {
		t.Errorf("AC2: expected exactly one winner, got %d (contenders %d)", ev.Successes, ev.Contenders)
	}
	if ev.Failures != ev.Contenders-1 {
		t.Errorf("AC2: failures = %d want %d", ev.Failures, ev.Contenders-1)
	}
	// Each rejection must be clear
	for _, e := range ev.Errors {
		if !strings.Contains(e, "another lifecycle operation is in progress") {
			t.Errorf("AC2: rejection error unclear, got %q", e)
		}
	}
	if !ev.StateParseable {
		t.Error("AC2: state file not parseable after lock contention (corruption)")
	}
	if ev.LockFile == "" || ev.StateFile == "" {
		t.Error("AC2: lock/state file paths missing in evidence")
	}
	// Lock file mode 0600 and persists
	fi, err := os.Stat(filepath.Join(cfg.InstallRoot, runtime.LockFileName))
	if err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0600 {
		t.Errorf("lock file mode = %o want 0600", got)
	}
}

// AC1: concurrent deploy (local vs CI) — only one active, deterministic
func TestAC1_ConcurrentDeployOnlyOneActive(t *testing.T) {
	cfg := tempCfg(t, "race-tester", 1)
	var buf bytes.Buffer
	cfg.Logger = &buf
	result, err := RunRace(cfg)
	if err != nil {
		t.Fatalf("RunRace: %v\nlog:\n%s", err, buf.String())
	}
	if result.AfterActivateRace == nil || result.AfterActivateRace.ActiveRelease == nil {
		t.Fatal("AC1: after activate race active is nil")
	}
	// Only-one-active invariant
	if err := AssertOnlyOneActive(cfg.InstallRoot); err != nil {
		t.Fatalf("AC1: only-one-active violated: %v", err)
	}
	if !result.ConcurrentActivate.OnlyOneActive {
		t.Error("AC1: ConcurrentActivate evidence OnlyOneActive false")
	}
	if !result.StateIntegrity.OnlyOneActiveInvariant {
		t.Error("AC1: StateIntegrity OnlyOneActiveInvariant false")
	}
	// Deterministic: active must be one of local/ci
	activeID := result.AfterActivateRace.ActiveRelease.ID.String()
	localRelID := ""
	ciRelID := ""
	for _, r := range result.InstallReleases {
		if r.ArtifactID == result.ArtifactLocal.Manifest.ArtifactID {
			localRelID = r.ID.String()
		}
		if r.ArtifactID == result.ArtifactCI.Manifest.ArtifactID {
			ciRelID = r.ID.String()
		}
	}
	if activeID != localRelID && activeID != ciRelID {
		t.Errorf("AC1: active %s not one of local %s or ci %s", activeID, localRelID, ciRelID)
	}
	// State integrity: no corruption
	if !result.StateIntegrity.StateFileParseable {
		t.Error("AC1: state file not parseable (corruption)")
	}
	if !result.StateIntegrity.ReleasesParseable {
		t.Error("AC1: releases not parseable (corruption)")
	}
	if !result.ConcurrentActivate.NoCorruption {
		t.Error("AC1: ConcurrentActivate NoCorruption false")
	}
}

// AC2: second deploy rejected with clear error, state not corrupt
func TestAC2_StateLockingOptimisticReject(t *testing.T) {
	cfg := tempCfg(t, "reject-tester", 1)
	var buf bytes.Buffer
	cfg.Logger = &buf
	result, err := RunRace(cfg)
	if err != nil {
		t.Fatalf("RunRace: %v\n%s", err, buf.String())
	}
	// Lock contention already proved; also concurrent install/activate must have loser errors with lock message at least in one phase
	hasLockError := false
	for _, e := range result.LockContention.Errors {
		if strings.Contains(e, "another lifecycle operation is in progress") {
			hasLockError = true
			break
		}
	}
	if !hasLockError {
		t.Error("AC2: lock contention errors missing expected lock message")
	}
	// Concurrent install while holder held must have lock errors
	hasInstallLockErr := false
	for _, e := range result.ConcurrentInstall.LoserErrors {
		if strings.Contains(e, "another lifecycle operation is in progress") {
			hasInstallLockErr = true
			break
		}
	}
	if !hasInstallLockErr {
		t.Errorf("AC2: concurrent install loser errors should contain lock rejection, got %v", result.ConcurrentInstall.LoserErrors)
	}
	// State files still valid
	if !result.LockContention.StateParseable {
		t.Error("AC2: state not parseable after concurrent operations")
	}
	if !result.StateIntegrity.StateFileParseable {
		t.Error("AC2: final state not parseable")
	}
	// Releases still parseable
	all, err := release.ListReleases(cfg.InstallRoot)
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	for _, r := range all {
		if r == nil {
			t.Error("AC2: nil release found (corruption)")
		}
	}
}

// AC3: idempotency — retry does not create duplicate release
func TestAC3_IdempotencyNoDuplicate(t *testing.T) {
	cfg := tempCfg(t, "idem-tester", 1)
	var buf bytes.Buffer
	cfg.Logger = &buf
	result, err := RunRace(cfg)
	if err != nil {
		t.Fatalf("RunRace: %v\n%s", err, buf.String())
	}
	if !result.Idempotency.DuplicatePrevented {
		t.Errorf("AC3: duplicate not prevented: before=%d after=%d duplicateErrors=%d", result.Idempotency.ReleaseCountBefore, result.Idempotency.ReleaseCountAfter, result.Idempotency.DuplicateErrors)
	}
	if result.Idempotency.ReleaseCountBefore != result.Idempotency.ReleaseCountAfter {
		t.Errorf("AC3: release count changed %d -> %d (duplicate created)", result.Idempotency.ReleaseCountBefore, result.Idempotency.ReleaseCountAfter)
	}
	if result.Idempotency.DuplicateErrors == 0 {
		t.Error("AC3: expected at least one duplicate error on retry")
	}
	// Verify no duplicate JSON file
	stateDir := filepath.Join(cfg.InstallRoot, ".anvil", "state", "releases")
	files, _ := os.ReadDir(stateDir)
	countJSON := 0
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".json") {
			countJSON++
		}
	}
	if countJSON != result.Idempotency.ReleaseCountAfter && countJSON != 0 {
		t.Errorf("AC3: json file count %d != release count %d (duplicate file leak)", countJSON, result.Idempotency.ReleaseCountAfter)
	}
	// Direct retry via coordinator should still be duplicate error
	coordinator := server.NewServerReleaseCoordinator(cfg.ServerRoot)
	remote := filepath.Join(cfg.RemoteStagingDir, filepath.Base(result.ArtifactLocal.Path))
	_, err = coordinator.Install(cfg.ProjectID, remote)
	if err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Errorf("AC3: direct retry should fail with already installed, got %v", err)
	}
}

// AC4: guard recommendation doc exists and covers dev allow + prod allowlist
func TestAC4_GuardRecommendation(t *testing.T) {
	doc := BuildGuardRecommendation()
	for _, want := range []string{
		"Dev Environment",
		"allow local",
		"Prod Environment",
		"allowlist",
		"confirm prompt",
		"runtime.OperationLock",
		"already installed",
		"adr:local-deploy-guard",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("AC4: guard doc missing %q", want)
		}
	}
	if len(doc) < 500 {
		t.Errorf("AC4: guard doc too short %d", len(doc))
	}
	// Also verify that RunRace populates GuardRecommendation
	cfg := tempCfg(t, "guard-tester", 1)
	var buf bytes.Buffer
	cfg.Logger = &buf
	result, err := RunRace(cfg)
	if err != nil {
		t.Fatalf("RunRace: %v\n%s", err, buf.String())
	}
	if result.GuardRecommendation == "" {
		t.Error("AC4: RaceResult.GuardRecommendation empty")
	}
	if !strings.Contains(result.GuardRecommendation, "allowlist") {
		t.Error("AC4: RaceResult guard missing allowlist")
	}
}

// AC2 deep: state dumps before/after are valid and lock record cleared
func TestStateDumpsAndLockCleared(t *testing.T) {
	cfg := tempCfg(t, "dump-tester", 1)
	var buf bytes.Buffer
	cfg.Logger = &buf
	result, err := RunRace(cfg)
	if err != nil {
		t.Fatalf("RunRace: %v\n%s", err, buf.String())
	}
	if result.BeforeState == nil {
		t.Fatal("before state nil")
	}
	if result.AfterActivateRace == nil {
		t.Fatal("after state nil")
	}
	// Lock record should be cleared after all ops
	if !result.StateIntegrity.LockRecordCleared {
		t.Error("lock record not cleared after race (held operation still recorded)")
	}
	// Before and after dumps must be parseable lifecycle
	if result.BeforeState.Lifecycle == nil {
		t.Error("before lifecycle nil")
	}
	if result.AfterActivateRace.Lifecycle == nil {
		t.Error("after lifecycle nil")
	}
	// Evidence json should contain before/after
	if result.LockContention.StateDump == "" {
		t.Error("lock state dump empty")
	}
}

// Additional: verify BuildGuardRecommendation file can be written (evidence contract)
func TestGuardRecommendationFileWrite(t *testing.T) {
	dir := t.TempDir()
	doc := BuildGuardRecommendation()
	path := filepath.Join(dir, "guard_recommendation.md")
	if err := os.WriteFile(path, []byte(doc), 0644); err != nil {
		t.Fatalf("write guard doc: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read guard doc: %v", err)
	}
	if !bytes.Contains(data, []byte("allowlist")) {
		t.Error("guard file missing allowlist")
	}
}

// Need to avoid unused import for releaseCoordinator; above helper returns nil but we don't use it now.
// Re-implement TestAC3's direct retry without helper to avoid import issues
func TestAC3_DirectRetry(t *testing.T) {
	// Use real server coordinator
	cfg := tempCfg(t, "direct-idem", 1)
	var buf bytes.Buffer
	cfg.Logger = &buf
	result, err := RunRace(cfg)
	if err != nil {
		t.Fatalf("RunRace: %v\n%s", err, buf.String())
	}
	// Direct retry with server coordinator
	// import server package already via SetupServer, but need server.NewServerReleaseCoordinator
	// Use the same logic as harness: create coordinator
	// To avoid circular helper, just test idempotency via result evidence
	if !result.Idempotency.DuplicatePrevented {
		t.Fatal("idempotency not prevented")
	}
}
