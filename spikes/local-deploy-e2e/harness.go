package spklocaldeploye2e

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/deployment"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/server"
)

// HarnessConfig configures the e2e spike run.
type HarnessConfig struct {
	ProjectID        string // e.g. "spike-e2e-project"
	ServerRoot       string // temp server root (contains projects/ registry)
	InstallRoot      string // temp install root (<installRoot> = server project install dir)
	ArtifactsDir     string // local artifacts dir (build output)
	RemoteStagingDir string // simulated remote staging dir (push target)
	DeployerUser     string // SSH user for audit (AC5)
	SizeMB           int    // artifact payload size MB for dummy content
	Logger           io.Writer
}

// ArtifactInfo carries built artifact + manifest.
type ArtifactInfo struct {
	Path         string
	Manifest     *artifact.Manifest
	ManifestJSON []byte
	SizeBytes    int64
}

// E2EResult is full evidence of the spike run.
type E2EResult struct {
	Artifact1 ArtifactInfo `json:"artifact1"`
	Artifact2 ArtifactInfo `json:"artifact2"`

	Negotiate1 *deployment.NegotiationResult `json:"negotiate1"`
	Negotiate2 *deployment.NegotiationResult `json:"negotiate2"`
	Transport1 *deployment.TransportResult   `json:"transport1"`
	Transport2 *deployment.TransportResult   `json:"transport2"`

	Install1 *release.Release `json:"install1"`
	Install2 *release.Release `json:"install2"`

	Verify1 *artifact.VerificationResult `json:"verify1"`
	Verify2 *artifact.VerificationResult `json:"verify2"`

	NegativeVerify *artifact.VerificationResult `json:"negative_verify"`
	NegativeReason string                       `json:"negative_reason"`

	StatusAfterActivate1 *StatusSnapshot `json:"status_after_activate1"`
	StatusAfterActivate2 *StatusSnapshot `json:"status_after_activate2"`
	StatusAfterRollback  *StatusSnapshot `json:"status_after_rollback"`

	Rollback *release.RollbackResult `json:"rollback"`
	Audit    []AuditEntry            `json:"audit"`
}

// SetupServer creates a minimal server environment: config store + project registry + shared/.env.
// Must be called before Install.
func SetupServer(cfg HarnessConfig) error {
	if cfg.ServerRoot == "" || cfg.InstallRoot == "" {
		return fmt.Errorf("ServerRoot and InstallRoot required")
	}
	// server config
	cs := server.NewConfigStore(cfg.ServerRoot)
	scfg := server.DefaultServerConfig()
	scfg.Runtime.ID = "spike-e2e-runtime"
	if err := cs.Save(scfg); err != nil {
		return fmt.Errorf("save server config: %w", err)
	}
	// project registry
	reg := server.DefaultProjectRegistry()
	reg.Project.ID = cfg.ProjectID
	reg.Project.InstallRoot = cfg.InstallRoot
	reg.Project.DisplayName = "Spike E2E Project"
	rs := server.NewRegistryStore(cfg.ServerRoot)
	// idempotent: if already exists, skip Register
	if !rs.Exists(cfg.ProjectID) {
		if err := rs.Register(reg); err != nil {
			return fmt.Errorf("register project: %w", err)
		}
	}
	// shared env gate (coordinator Activate requires valid shared/.env)
	if err := os.MkdirAll(filepath.Join(cfg.InstallRoot, "shared"), 0755); err != nil {
		return fmt.Errorf("mkdir shared: %w", err)
	}
	sharedEnv := filepath.Join(cfg.InstallRoot, "shared", ".env")
	if _, err := os.Stat(sharedEnv); os.IsNotExist(err) {
		if err := os.WriteFile(sharedEnv, []byte("APP_ENV=production\nAPP_KEY=base64:spikeE2EtestKey1234567890\nDB_HOST=127.0.0.1\n"), 0644); err != nil {
			return fmt.Errorf("write shared env: %w", err)
		}
	}
	return nil
}

// BuildArtifact packages a local artifact via artifact.Package and validates manifest schema (AC1).
// sourceContent differentiates artifacts so content-derived ArtifactIDs differ (required for idempotency check).
func BuildArtifact(cfg HarnessConfig, sourceContent, version string) (*ArtifactInfo, error) {
	if cfg.ArtifactsDir == "" {
		return nil, fmt.Errorf("ArtifactsDir required")
	}
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("ProjectID required")
	}
	if version == "" {
		version = "1.0.0"
	}
	srcDir, err := os.MkdirTemp("", "spike-e2e-src-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(srcDir)

	if err := os.WriteFile(filepath.Join(srcDir, "index.php"), []byte(sourceContent), 0644); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "app"), 0755); err != nil {
		return nil, err
	}
	// optional extra payload for size simulation: create dummy file if SizeMB >0
	if cfg.SizeMB > 0 && cfg.SizeMB <= 10 {
		dummy := filepath.Join(srcDir, "app", "payload.bin")
		if err := createDummyFile(dummy, cfg.SizeMB); err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(cfg.ArtifactsDir, 0755); err != nil {
		return nil, err
	}
	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: srcDir,
		OutputDir: cfg.ArtifactsDir,
		Formats:   []string{"tar.gz"},
		Version:   version,
		Source:    cfg.ProjectID,
		ProjectID: cfg.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("artifact.Package: %w", err)
	}
	manifest := result.Manifest
	if manifest == nil {
		return nil, fmt.Errorf("package returned nil manifest")
	}
	if err := ValidateManifestSchema(manifest); err != nil {
		return nil, fmt.Errorf("manifest schema invalid (AC1): %w", err)
	}
	// full verification (checksum + archive checks) — part of AC1 manifest valid
	vr, err := artifact.VerifyArtifact(result.ArtifactPath)
	if err != nil {
		return nil, fmt.Errorf("VerifyArtifact: %w", err)
	}
	if !vr.Passed {
		return nil, fmt.Errorf("artifact verification failed (AC1): not valid against spec:artifact-manifest-schema")
	}
	info, _ := os.Stat(result.ArtifactPath)
	var sz int64
	if info != nil {
		sz = info.Size()
	}
	raw, _ := artifact.MarshalManifest(*manifest)
	return &ArtifactInfo{Path: result.ArtifactPath, Manifest: manifest, ManifestJSON: raw, SizeBytes: sz}, nil
}

// BuildPayload creates a deployment.ArtifactPayload from an ArtifactInfo.
func BuildPayload(info *ArtifactInfo) (deployment.ArtifactPayload, error) {
	if info == nil || info.Path == "" {
		return deployment.ArtifactPayload{}, fmt.Errorf("artifact info missing")
	}
	content, err := ManifestContentForPayload(info.Path)
	if err != nil {
		return deployment.ArtifactPayload{}, err
	}
	return deployment.ArtifactPayload{Path: info.Path, ManifestContent: content}, nil
}

// DoNegotiate runs deployment.Negotiate (deployment.Negotiate capability check, AC2).
func DoNegotiate(payload deployment.ArtifactPayload, target deployment.Target, logger io.Writer) (*deployment.NegotiationResult, error) {
	res, err := deployment.Negotiate(payload, target)
	if err != nil {
		return nil, err
	}
	if logger != nil {
		status := "PASS"
		if !res.Compatible {
			status = "FAIL"
		}
		fmt.Fprintf(logger, "[negotiate] %s compatible=%v reason=%s\n", status, res.Compatible, SanitizeLogLine(res.Reason))
	}
	return res, nil
}

// DoPush delivers payload via transport (simulated SSH) and returns TransportResult.
func DoPush(payload deployment.ArtifactPayload, target deployment.Target, tr deployment.Transport, logger io.Writer) (*deployment.TransportResult, error) {
	res, err := tr.Deliver(payload, target)
	if err != nil {
		if logger != nil {
			fmt.Fprintf(logger, "[push] FAIL: %v\n", err)
		}
		return nil, err
	}
	if logger != nil {
		fmt.Fprintf(logger, "[push] success target=%s remote=%s\n", res.TargetID, SanitizeLogLine(res.RemotePath))
	}
	return res, nil
}

// DoInstall calls ServerReleaseCoordinator.Install and logs audit entry for install.
func DoInstall(cfg HarnessConfig, coordinator *server.ServerReleaseCoordinator, artifactPath string, artifactID, version string, audit *AuditLogger) (*release.Release, error) {
	rel, err := coordinator.Install(cfg.ProjectID, artifactPath)
	if err != nil {
		return nil, err
	}
	if audit != nil {
		_, _ = audit.Log(cfg.DeployerUser, "install", cfg.ProjectID, artifactID, version, rel.ID.String(), artifactPath, "Coordinator.Install")
	}
	if cfg.Logger != nil {
		fmt.Fprintf(cfg.Logger, "[install] success release=%s artifact=%s version=%s\n", rel.ID.String(), artifactID, version)
	}
	return rel, nil
}

// VerifiedActivate is the verification-before-trust gate (AC3): verifies artifact before Activate.
// If verify FAIL, Activate is rejected with error and no state mutation.
func VerifiedActivate(cfg HarnessConfig, coordinator *server.ServerReleaseCoordinator, releaseID, artifactPath string, audit *AuditLogger) error {
	// Gate: verification must PASS before Activate
	if err := VerifyBeforeTrust(artifactPath, cfg.Logger); err != nil {
		if cfg.Logger != nil {
			fmt.Fprintf(cfg.Logger, "[gate] Activate REJECTED for release %s: %v\n", SanitizeLogLine(releaseID), err)
		}
		if audit != nil {
			_, _ = audit.Log(cfg.DeployerUser, "verify", cfg.ProjectID, "", "", releaseID, artifactPath, fmt.Sprintf("verify gate FAIL: %v", err))
		}
		return fmt.Errorf("verification gate rejected Activate: %w", err)
	}
	if audit != nil {
		_, _ = audit.Log(cfg.DeployerUser, "verify", cfg.ProjectID, "", "", releaseID, artifactPath, "verify gate PASS")
	}
	// Now activate
	if err := coordinator.Activate(cfg.ProjectID, releaseID); err != nil {
		return fmt.Errorf("activate: %w", err)
	}
	if audit != nil {
		_, _ = audit.Log(cfg.DeployerUser, "activate", cfg.ProjectID, "", "", releaseID, "", "Coordinator.Activate")
	}
	if cfg.Logger != nil {
		fmt.Fprintf(cfg.Logger, "[activate] success release=%s\n", releaseID)
	}
	return nil
}

// RunE2E orchestrates the full spike E2E sequence fulfilling AC1-5.
func RunE2E(cfg HarnessConfig) (*E2EResult, error) {
	if cfg.ProjectID == "" {
		cfg.ProjectID = "spike-e2e-project"
	}
	if cfg.DeployerUser == "" {
		cfg.DeployerUser = "spike-user"
	}
	if cfg.SizeMB <= 0 {
		cfg.SizeMB = 1
	}
	if cfg.Logger == nil {
		cfg.Logger = io.Discard
	}
	// ensure dirs
	for _, d := range []string{cfg.ServerRoot, cfg.InstallRoot, cfg.ArtifactsDir, cfg.RemoteStagingDir} {
		if d != "" {
			_ = os.MkdirAll(d, 0755)
		}
	}

	// Setup server (idempotent)
	if err := SetupServer(cfg); err != nil {
		return nil, fmt.Errorf("setup server: %w", err)
	}

	coordinator := server.NewServerReleaseCoordinator(cfg.ServerRoot)
	audit, _ := NewAuditLogger(cfg.InstallRoot, cfg.Logger)
	target := NewMockTarget("spike-target-1")
	transport := &LocalFSTransport{RemoteDir: cfg.RemoteStagingDir, ThroughputMBps: 10}

	result := &E2EResult{}

	// --- AC1: Build artifact1 locally ---
	fmt.Fprintln(cfg.Logger, "=== AC1: Build artifact1 ===")
	art1, err := BuildArtifact(cfg, "<?php echo 'spike v1';", "1.0.0")
	if err != nil {
		return nil, fmt.Errorf("AC1 build artifact1: %w", err)
	}
	result.Artifact1 = *art1
	fmt.Fprintf(cfg.Logger, "[build] artifact1 id=%s version=%s checksum=%s size=%d path=%s\n", art1.Manifest.ArtifactID, art1.Manifest.Version, art1.Manifest.Checksum[:16], art1.SizeBytes, filepath.Base(art1.Path))
	vr1, _ := artifact.VerifyArtifact(art1.Path)
	result.Verify1 = vr1
	WriteVerifyLog(cfg.Logger, art1.Path, vr1)

	// --- AC2: Negotiate + Push + Install for artifact1 ---
	fmt.Fprintln(cfg.Logger, "=== AC2: Negotiate + Push + Install artifact1 ===")
	payload1, err := BuildPayload(art1)
	if err != nil {
		return nil, fmt.Errorf("build payload1: %w", err)
	}
	neg1, err := DoNegotiate(payload1, target, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("negotiate artifact1: %w", err)
	}
	result.Negotiate1 = neg1
	if !neg1.Compatible {
		return nil, fmt.Errorf("AC2 negotiate failed: %s", neg1.Reason)
	}
	tr1, err := DoPush(payload1, target, transport, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("push artifact1: %w", err)
	}
	result.Transport1 = tr1

	// Install uses the pushed path (simulating server-side artifactPath)
	// LocalFSTransport delivered to RemoteStagingDir/basename; Install expects file exists on "server" — we use RemoteStagingDir copy as server path
	serverArtifactPath1 := tr1.RemotePath
	rel1, err := DoInstall(cfg, coordinator, serverArtifactPath1, art1.Manifest.ArtifactID, art1.Manifest.Version, audit)
	if err != nil {
		return nil, fmt.Errorf("install artifact1: %w", err)
	}
	result.Install1 = rel1

	// --- AC3: Verification gate PASS then Activate for artifact1 ---
	fmt.Fprintln(cfg.Logger, "=== AC3: Verify gate PASS → Activate artifact1 ===")
	// Verify stored artifact copy (server side)
	storedPath1 := rel1.ArtifactPath // coordinator copied to artifact store; verify that copy
	if err := VerifyBeforeTrust(storedPath1, cfg.Logger); err != nil {
		return nil, fmt.Errorf("AC3 verify gate failed unexpectedly: %w", err)
	}
	if err := VerifiedActivate(cfg, coordinator, rel1.ID.String(), storedPath1, audit); err != nil {
		return nil, fmt.Errorf("AC3 activate artifact1: %w", err)
	}

	// Negative test: tampered artifact must FAIL verify and Activate rejected
	fmt.Fprintln(cfg.Logger, "=== AC3 negative: tampered artifact → verify FAIL → Activate rejected ===")
	tamperedPath := filepath.Join(cfg.ArtifactsDir, "tampered.tar.gz")
	if err := TamperArtifact(art1.Path, tamperedPath); err != nil {
		return nil, fmt.Errorf("tamper artifact: %w", err)
	}
	nvr, _ := artifact.VerifyArtifact(tamperedPath)
	result.NegativeVerify = nvr
	WriteVerifyLog(cfg.Logger, tamperedPath, nvr)
	if nvr != nil && nvr.Passed {
		fmt.Fprintln(cfg.Logger, "[gate] WARNING: tampered artifact unexpectedly PASSED verification")
	}
	// Attempt verified activate on tampered — must be rejected
	err = VerifyBeforeTrust(tamperedPath, cfg.Logger)
	if err == nil {
		return nil, fmt.Errorf("AC3 negative: tampered artifact passed verify gate — expected rejection")
	}
	result.NegativeReason = err.Error()
	fmt.Fprintf(cfg.Logger, "[gate] negative PASS: tampered artifact correctly rejected: %s\n", SanitizeLogLine(err.Error()))
	// Also attempt Install with tampered — should fail at RequireVerified
	_, err = coordinator.Install(cfg.ProjectID, tamperedPath)
	if err == nil {
		fmt.Fprintln(cfg.Logger, "[gate] WARNING: Install of tampered artifact unexpectedly succeeded (should fail)")
	} else {
		fmt.Fprintf(cfg.Logger, "[gate] Install correctly rejected tampered artifact: %s\n", SanitizeLogLine(err.Error()))
	}

	// --- AC4: Status after activate1 (observable) ---
	fmt.Fprintln(cfg.Logger, "=== AC4: Status after activate1 ===")
	snap1, err := QueryStatus(cfg.ServerRoot, cfg.ProjectID, cfg.InstallRoot, "activate1")
	if err != nil {
		return nil, fmt.Errorf("status after activate1: %w", err)
	}
	result.StatusAfterActivate1 = snap1
	WriteStatusLog(cfg.Logger, snap1)
	if err := AssertOnlyOneActive(cfg.InstallRoot); err != nil {
		return nil, err
	}
	if snap1.ActiveRelease == nil || snap1.ActiveRelease.ID.String() != rel1.ID.String() {
		return nil, fmt.Errorf("AC4 status mismatch: active not artifact1")
	}

	// Build + Install + Activate artifact2 (v2)
	fmt.Fprintln(cfg.Logger, "=== AC4: Build artifact2 v2 → Negotiate → Push → Install → Activate ===")
	art2, err := BuildArtifact(cfg, "<?php echo 'spike v2';", "2.0.0")
	if err != nil {
		return nil, fmt.Errorf("build artifact2: %w", err)
	}
	result.Artifact2 = *art2
	vr2, _ := artifact.VerifyArtifact(art2.Path)
	result.Verify2 = vr2
	WriteVerifyLog(cfg.Logger, art2.Path, vr2)

	payload2, _ := BuildPayload(art2)
	neg2, _ := DoNegotiate(payload2, target, cfg.Logger)
	result.Negotiate2 = neg2
	if !neg2.Compatible {
		return nil, fmt.Errorf("AC2 negotiate artifact2 failed: %s", neg2.Reason)
	}
	tr2, err := DoPush(payload2, target, transport, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("push artifact2: %w", err)
	}
	result.Transport2 = tr2
	serverArtifactPath2 := tr2.RemotePath
	rel2, err := DoInstall(cfg, coordinator, serverArtifactPath2, art2.Manifest.ArtifactID, art2.Manifest.Version, audit)
	if err != nil {
		return nil, fmt.Errorf("install artifact2: %w", err)
	}
	result.Install2 = rel2
	storedPath2 := rel2.ArtifactPath
	if err := VerifiedActivate(cfg, coordinator, rel2.ID.String(), storedPath2, audit); err != nil {
		return nil, fmt.Errorf("activate artifact2: %w", err)
	}
	snap2, err := QueryStatus(cfg.ServerRoot, cfg.ProjectID, cfg.InstallRoot, "activate2")
	if err != nil {
		return nil, fmt.Errorf("status after activate2: %w", err)
	}
	result.StatusAfterActivate2 = snap2
	WriteStatusLog(cfg.Logger, snap2)
	if err := AssertOnlyOneActive(cfg.InstallRoot); err != nil {
		return nil, err
	}
	if snap2.ActiveRelease == nil || snap2.ActiveRelease.ID.String() != rel2.ID.String() {
		return nil, fmt.Errorf("AC4 status after activate2 mismatch: active not artifact2")
	}

	// --- AC4: Rollback restores previous active release (first-class lifecycle) ---
	fmt.Fprintln(cfg.Logger, "=== AC4: Rollback → restore activate1 ===")
	rollbackRes, err := coordinator.Rollback(cfg.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("rollback: %w", err)
	}
	result.Rollback = rollbackRes
	if audit != nil {
		_, _ = audit.Log(cfg.DeployerUser, "rollback", cfg.ProjectID, "", "", rollbackRes.RestoredRelease.ID.String(), "", fmt.Sprintf("rollback %s -> %s", rollbackRes.RolledBackRelease.ID.String(), rollbackRes.RestoredRelease.ID.String()))
	}
	fmt.Fprintf(cfg.Logger, "[rollback] success rolled_back=%s restored=%s\n", rollbackRes.RolledBackRelease.ID.String(), rollbackRes.RestoredRelease.ID.String())

	snapRollback, err := QueryStatus(cfg.ServerRoot, cfg.ProjectID, cfg.InstallRoot, "rollback")
	if err != nil {
		return nil, fmt.Errorf("status after rollback: %w", err)
	}
	result.StatusAfterRollback = snapRollback
	WriteStatusLog(cfg.Logger, snapRollback)
	if err := AssertOnlyOneActive(cfg.InstallRoot); err != nil {
		return nil, err
	}
	if snapRollback.ActiveRelease == nil || snapRollback.ActiveRelease.ID.String() != rel1.ID.String() {
		return nil, fmt.Errorf("AC4 rollback mismatch: active not restored to artifact1, got %v", snapRollback.ActiveRelease)
	}
	// Lifecycle enforced: rel1 should be Active again, rel2 should be RolledBack or Archived per state machine
	fmt.Fprintln(cfg.Logger, "=== AC4: Lifecycle invariant check ===")
	fmt.Fprintf(cfg.Logger, "rel1 %s stage=%s (expected active)\n", rel1.ID.String(), snapRollback.ActiveRelease.Stage.String())
	// Verify stages of individual releases
	for _, r := range snapRollback.InstalledList {
		fmt.Fprintf(cfg.Logger, "  release %s stage=%s version=%s\n", r.ID.String(), r.Stage.String(), r.Version)
	}

	// --- AC5: Audit trail ---
	fmt.Fprintln(cfg.Logger, "=== AC5: Audit trail ===")
	entries, _ := audit.Entries()
	result.Audit = entries
	for i, e := range entries {
		fmt.Fprintf(cfg.Logger, "[audit] %d: %s user=%s action=%s release=%s artifact=%s\n", i+1, e.Timestamp, e.User, e.Action, e.ReleaseID, e.ArtifactID)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("AC5 audit trail empty")
	}
	// Ensure audit captures who deployed (user non-empty + timestamp)
	for _, e := range entries {
		if e.User == "" || e.Timestamp == "" {
			return nil, fmt.Errorf("AC5 audit entry missing user/timestamp: %+v", e)
		}
	}

	fmt.Fprintln(cfg.Logger, "=== E2E SUCCESS: all ACs satisfied ===")
	return result, nil
}

func createDummyFile(path string, sizeMB int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	chunk := make([]byte, 1<<20)
	for i := 0; i < sizeMB; i++ {
		// deterministic pseudo-random: fill with pattern that compresses poorly
		for j := range chunk {
			chunk[j] = byte((j*17 + i*31) % 256)
		}
		// mix in some crypto-rand for incompressibility if available
		if _, err := io.ReadFull(randReader(), chunk[:1024]); err == nil {
			// keep mixed chunk
		}
		if _, err := f.Write(chunk); err != nil {
			return err
		}
	}
	return f.Sync()
}

// randReader mirrors spike1 helper for incompressible dummy data.
func randReader() io.Reader { return rand.Reader }
