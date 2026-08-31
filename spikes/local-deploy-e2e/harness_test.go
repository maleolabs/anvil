package spklocaldeploye2e

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/deployment"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/server"
)

func tempCfg(t *testing.T, user string, sizeMB int) HarnessConfig {
	t.Helper()
	serverRoot := filepath.Join(t.TempDir(), "server")
	installRoot := filepath.Join(serverRoot, "install", "spike-e2e-project")
	artifactsDir := filepath.Join(t.TempDir(), "artifacts")
	remoteDir := filepath.Join(t.TempDir(), "remote")
	return HarnessConfig{
		ProjectID:        "spike-e2e-project",
		ServerRoot:       serverRoot,
		InstallRoot:      installRoot,
		ArtifactsDir:     artifactsDir,
		RemoteStagingDir: remoteDir,
		DeployerUser:     user,
		SizeMB:           sizeMB,
		Logger:           &bytes.Buffer{},
	}
}

// AC1: build produces artifact with valid manifest schema
func TestAC1_BuildArtifactManifestValid(t *testing.T) {
	cfg := tempCfg(t, "tester", 1)
	art, err := BuildArtifact(cfg, "<?php echo 'ac1';", "1.2.3")
	if err != nil {
		t.Fatalf("BuildArtifact: %v", err)
	}
	if art.Manifest.ArtifactID == "" {
		t.Error("artifact_id empty (identity-from-content violated)")
	}
	if art.Manifest.Version != "1.2.3" {
		t.Errorf("version = %q want 1.2.3", art.Manifest.Version)
	}
	if art.Manifest.Checksum == "" || art.Manifest.ChecksumType == "" {
		t.Error("checksum missing")
	}
	if art.Manifest.ProjectID != cfg.ProjectID {
		t.Errorf("project_id = %q want %q", art.Manifest.ProjectID, cfg.ProjectID)
	}
	vr, err := artifact.VerifyArtifact(art.Path)
	if err != nil {
		t.Fatalf("VerifyArtifact: %v", err)
	}
	if !vr.Passed {
		t.Fatalf("verification FAIL (AC1): %+v", vr.Checks)
	}
	if err := ValidateManifestSchema(art.Manifest); err != nil {
		t.Fatalf("manifest schema invalid: %v", err)
	}
}

// AC2: Negotiate PASS + Push + Install
func TestAC2_NegotiatePushInstall(t *testing.T) {
	cfg := tempCfg(t, "deployer1", 1)
	if err := SetupServer(cfg); err != nil {
		t.Fatalf("SetupServer: %v", err)
	}
	art, err := BuildArtifact(cfg, "<?php echo 'ac2';", "1.0.0")
	if err != nil {
		t.Fatalf("BuildArtifact: %v", err)
	}
	payload, err := BuildPayload(art)
	if err != nil {
		t.Fatalf("BuildPayload: %v", err)
	}
	target := NewMockTarget("t1")
	neg, err := DoNegotiate(payload, target, nil)
	if err != nil {
		t.Fatalf("DoNegotiate: %v", err)
	}
	if !neg.Compatible {
		t.Fatalf("negotiate FAIL want PASS: %s", neg.Reason)
	}
	tr := &LocalFSTransport{RemoteDir: cfg.RemoteStagingDir, ThroughputMBps: 10}
	tres, err := DoPush(payload, target, tr, nil)
	if err != nil {
		t.Fatalf("DoPush: %v", err)
	}
	if !tres.Success || tres.RemotePath == "" {
		t.Fatalf("transport result invalid: %+v", tres)
	}
	coord := server.NewServerReleaseCoordinator(cfg.ServerRoot)
	rel, err := DoInstall(cfg, coord, tres.RemotePath, art.Manifest.ArtifactID, art.Manifest.Version, nil)
	if err != nil {
		t.Fatalf("DoInstall: %v", err)
	}
	if rel.ID.String() == "" {
		t.Error("install returned empty release id")
	}
	if rel.Stage != release.StageReady {
		t.Errorf("install stage = %s want ready", rel.Stage.String())
	}
}

// AC2 negative: Negotiate FAIL when target rejects
func TestAC2_NegotiateFail(t *testing.T) {
	cfg := tempCfg(t, "tester", 1)
	art, err := BuildArtifact(cfg, "<?php echo 'negfail';", "1.0.0")
	if err != nil {
		t.Fatalf("BuildArtifact: %v", err)
	}
	payload, _ := BuildPayload(art)
	target := NewFailingTarget("t-fail", "incompatible runtime")
	neg, err := deployment.Negotiate(payload, target)
	if err != nil {
		t.Fatalf("Negotiate: %v", err)
	}
	if neg.Compatible {
		t.Fatal("expected negotiate incompatible, got compatible")
	}
	// Also via DoNegotiate helper
	target2 := NewMockTarget("t1").AsFailing("not compatible")
	neg2, _ := DoNegotiate(payload, target2, nil)
	if neg2.Compatible {
		t.Fatal("expected DoNegotiate incompatible")
	}
}

// AC3: Verify gate PASS before Activate, and FAIL blocks Activate
func TestAC3_VerificationGate(t *testing.T) {
	cfg := tempCfg(t, "verifier", 1)
	if err := SetupServer(cfg); err != nil {
		t.Fatalf("SetupServer: %v", err)
	}
	art, err := BuildArtifact(cfg, "<?php echo 'ac3';", "1.0.0")
	if err != nil {
		t.Fatalf("BuildArtifact: %v", err)
	}
	payload, _ := BuildPayload(art)
	target := NewMockTarget("t1")
	tr := &LocalFSTransport{RemoteDir: cfg.RemoteStagingDir}
	tres, _ := DoPush(payload, target, tr, nil)
	coord := server.NewServerReleaseCoordinator(cfg.ServerRoot)
	rel, err := DoInstall(cfg, coord, tres.RemotePath, art.Manifest.ArtifactID, art.Manifest.Version, nil)
	if err != nil {
		t.Fatalf("DoInstall: %v", err)
	}
	// Gate PASS
	if err := VerifyBeforeTrust(rel.ArtifactPath, nil); err != nil {
		t.Fatalf("VerifyBeforeTrust PASS expected: %v", err)
	}
	if err := VerifiedActivate(cfg, coord, rel.ID.String(), rel.ArtifactPath, nil); err != nil {
		t.Fatalf("VerifiedActivate PASS expected: %v", err)
	}
	// Negative: tampered artifact must be rejected
	tampered := filepath.Join(t.TempDir(), "tampered.tar.gz")
	if err := TamperArtifact(art.Path, tampered); err != nil {
		t.Fatalf("TamperArtifact: %v", err)
	}
	vr, _ := artifact.VerifyArtifact(tampered)
	if vr != nil && vr.Passed {
		t.Fatal("tampered artifact should FAIL verification")
	}
	if err := VerifyBeforeTrust(tampered, nil); err == nil {
		t.Fatal("VerifyBeforeTrust should reject tampered artifact")
	}
	if err := VerifiedActivate(cfg, coord, rel.ID.String(), tampered, nil); err == nil {
		t.Fatal("VerifiedActivate should reject tampered artifact")
	}
	// Also Install of tampered should fail
	if _, err := coord.Install(cfg.ProjectID, tampered); err == nil {
		t.Fatal("Install should reject tampered artifact")
	}
}

// AC4: Activate sets active, status observable, only-one-active, rollback
func TestAC4_ActivateStatusRollback(t *testing.T) {
	cfg := tempCfg(t, "activator", 1)
	var buf bytes.Buffer
	cfg.Logger = &buf
	result, err := RunE2E(cfg)
	if err != nil {
		t.Fatalf("RunE2E: %v\nlog:\n%s", err, buf.String())
	}
	// After activate1 active is rel1
	if result.StatusAfterActivate1 == nil || result.StatusAfterActivate1.ActiveRelease == nil {
		t.Fatal("status after activate1 missing active")
	}
	if result.StatusAfterActivate1.ActiveRelease.ID.String() != result.Install1.ID.String() {
		t.Errorf("active after activate1 = %s want %s", result.StatusAfterActivate1.ActiveRelease.ID.String(), result.Install1.ID.String())
	}
	// After activate2 active is rel2
	if result.StatusAfterActivate2.ActiveRelease.ID.String() != result.Install2.ID.String() {
		t.Errorf("active after activate2 = %s want %s", result.StatusAfterActivate2.ActiveRelease.ID.String(), result.Install2.ID.String())
	}
	// only-one-active
	if err := AssertOnlyOneActive(cfg.InstallRoot); err != nil {
		t.Fatalf("only-one-active violated: %v", err)
	}
	// Rollback restores rel1
	if result.Rollback == nil {
		t.Fatal("rollback result nil")
	}
	if result.StatusAfterRollback == nil || result.StatusAfterRollback.ActiveRelease == nil {
		t.Fatal("status after rollback missing active")
	}
	if result.StatusAfterRollback.ActiveRelease.ID.String() != result.Install1.ID.String() {
		t.Errorf("active after rollback = %s want %s (restored)", result.StatusAfterRollback.ActiveRelease.ID.String(), result.Install1.ID.String())
	}
	// Lifecycle stages: after rollback, rel2 should not be Active
	activeList, err := release.ListReleasesByStage(cfg.InstallRoot, release.StageActive)
	if err != nil {
		t.Fatalf("ListReleasesByStage active: %v", err)
	}
	if len(activeList) != 1 {
		t.Errorf("active count after rollback = %d want 1", len(activeList))
	}
	if len(activeList) == 1 && activeList[0].ID.String() != result.Install1.ID.String() {
		t.Errorf("active after rollback = %s want %s", activeList[0].ID.String(), result.Install1.ID.String())
	}
	// Status observable via QueryLifecycleStatus
	lc, err := server.QueryLifecycleStatus(cfg.ServerRoot, cfg.ProjectID)
	if err != nil {
		t.Fatalf("QueryLifecycleStatus: %v", err)
	}
	if lc.Active == nil {
		t.Fatal("QueryLifecycleStatus active nil after rollback")
	}
	// Check lifecycle persistence: runtime-state.json holds active
	if lc.RuntimeState.ActiveReleaseID != result.Install1.ID.String() {
		t.Errorf("runtime state active = %s want %s", lc.RuntimeState.ActiveReleaseID, result.Install1.ID.String())
	}
}

// AC5: Audit trail captures who deployed + timestamp
func TestAC5_AuditTrail(t *testing.T) {
	cfg := tempCfg(t, "audit-user-123", 1)
	var buf bytes.Buffer
	cfg.Logger = &buf
	result, err := RunE2E(cfg)
	if err != nil {
		t.Fatalf("RunE2E: %v\n%s", err, buf.String())
	}
	if len(result.Audit) == 0 {
		t.Fatal("audit empty")
	}
	// Check audit entries contain user + timestamp + actions
	foundInstall := false
	foundActivate := false
	foundRollback := false
	for _, e := range result.Audit {
		if e.User == "" || e.Timestamp == "" {
			t.Errorf("audit entry missing user/timestamp: %+v", e)
		}
		if e.User != "audit-user-123" {
			t.Errorf("audit user = %q want audit-user-123", e.User)
		}
		switch e.Action {
		case "install":
			foundInstall = true
		case "activate":
			foundActivate = true
		case "rollback":
			foundRollback = true
		}
	}
	if !foundInstall {
		t.Error("audit missing install action")
	}
	if !foundActivate {
		t.Error("audit missing activate action")
	}
	if !foundRollback {
		t.Error("audit missing rollback action")
	}
	// Also verify log file on disk
	auditPath := filepath.Join(cfg.InstallRoot, "audit.log")
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	if len(data) == 0 {
		t.Error("audit.log empty on disk")
	}
	// Ensure audit log doesn't leak secrets
	content := string(data)
	if bytes.Contains([]byte(content), []byte("DEPLOY_SSH_KEY")) {
		t.Error("audit log leaked DEPLOY_SSH_KEY")
	}
}

// Transport idempotency: retry after partial write still succeeds (AC1 from spike1, reused)
func TestTransport_IdempotentRetry(t *testing.T) {
	cfg := tempCfg(t, "retry-tester", 1)
	art, err := BuildArtifact(cfg, "<?php echo 'retry';", "1.0.0")
	if err != nil {
		t.Fatalf("BuildArtifact: %v", err)
	}
	payload, _ := BuildPayload(art)
	target := NewMockTarget("t1")
	// first attempt with failure injection
	failTr := &LocalFSTransport{RemoteDir: cfg.RemoteStagingDir, FailAtByte: 1024, FailKind: deployment.KindTransferFailed}
	_, err = failTr.Deliver(payload, target)
	if err == nil {
		t.Fatal("expected fail on injected disconnect")
	}
	// remote final file should NOT exist after partial
	if _, err := os.Stat(filepath.Join(cfg.RemoteStagingDir, filepath.Base(art.Path))); err == nil {
		t.Error("final remote should not exist after partial write")
	}
	// retry with success
	okTr := &LocalFSTransport{RemoteDir: cfg.RemoteStagingDir}
	res, err := okTr.Deliver(payload, target)
	if err != nil {
		t.Fatalf("retry Deliver: %v", err)
	}
	if _, err := os.Stat(res.RemotePath); err != nil {
		t.Fatalf("remote after retry not exists: %v", err)
	}
	// checksum verify after retry
	if err := VerifyBeforeTrust(res.RemotePath, nil); err != nil {
		t.Fatalf("verify after retry: %v", err)
	}
}

// Redaction: secrets not logged
func TestRedaction(t *testing.T) {
	if got := RedactSecrets("/home/user/.ssh/id_rsa"); got == "/home/user/.ssh/id_rsa" {
		t.Error("RedactSecrets should redact key path")
	}
	if got := SanitizeLogLine("key is DEPLOY_SSH_KEY value secret123"); !contains(got, "REDACTED") {
		// set env for test
		t.Setenv("DEPLOY_SSH_KEY", "secret123")
		got2 := SanitizeLogLine("key is secret123")
		if !contains(got2, "REDACTED") {
			t.Errorf("SanitizeLogLine should redact env var: %q", got2)
		}
	}
}

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }

// Identity-from-content: same content yields same ArtifactID, different content yields different
// Use SizeMB=0 to avoid random dummy payload that would randomize identity.
func TestIdentityFromContent(t *testing.T) {
	cfg1 := tempCfg(t, "u", 0)
	cfg2 := tempCfg(t, "u", 0)
	cfg1.SizeMB = 0
	cfg2.SizeMB = 0
	// Use same ServerRoot? No, isolated artifacts dirs, but source content identical
	art1, err := BuildArtifact(cfg1, "<?php same content;", "1.0.0")
	if err != nil {
		t.Fatalf("BuildArtifact1: %v", err)
	}
	art2, err := BuildArtifact(cfg2, "<?php same content;", "1.0.0")
	if err != nil {
		t.Fatalf("BuildArtifact2: %v", err)
	}
	if art1.Manifest.ArtifactID != art2.Manifest.ArtifactID {
		t.Errorf("same content should yield same ArtifactID: %q vs %q", art1.Manifest.ArtifactID, art2.Manifest.ArtifactID)
	}
	art3, err := BuildArtifact(cfg2, "<?php different content;", "1.0.0")
	if err != nil {
		t.Fatalf("BuildArtifact3: %v", err)
	}
	if art1.Manifest.ArtifactID == art3.Manifest.ArtifactID {
		t.Error("different content should yield different ArtifactID")
	}
}
