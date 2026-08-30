package conformance

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"maleolabs.com/anvil/internal/adapter"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

// AnvilRuntime binds the conformance harness's contract-level Runtime
// surface to the Anvil Runtime engine's public API (EPIC-015). It is
// the only file in this package that imports engine packages: the
// checks in this package validate behavior through the Runtime
// interface and never see engine internals (ADR-029 §3; Transition Plan
// §5.10).
//
// The binding drives the engine through its public operations — the
// server release coordinator (Install/Activate/Rollback), the artifact
// packaging/verification primitives, the release state queries, and the
// adapter coordinator at the runtime–standard exchange boundary — and
// maps the engine's results onto contract-level facts (stages, identity,
// evidence, declared capability).
type AnvilRuntime struct {
	// serverRoot is the config root the binding provisions the
	// conformance project under.
	serverRoot string

	// installRoot is the registered project's install root — the
	// runtime under test.
	installRoot string

	// projectID is the registered project identity (conformanceProjectID).
	projectID string

	// coordinator drives the engine's lifecycle operations.
	coordinator *server.ServerReleaseCoordinator

	// registry and exchange implement the runtime–standard exchange
	// boundary: declared capability storage and the subprocess
	// coordinator.
	registry *adapter.CapabilityRegistry
	exchange *adapter.Coordinator

	// recorder observes the subprocess boundary.
	recorder *recordingRunner
}

// NewAnvilRuntime provisions a fresh runtime under test rooted at
// serverRoot: a server config, a registered project with its install
// root, and the runtime–standard exchange surface. Each call creates an
// isolated runtime, so checks never share state.
func NewAnvilRuntime(serverRoot string) (*AnvilRuntime, error) {
	projectID := conformanceProjectID
	installRoot := filepath.Join(serverRoot, "install-root")

	// Provision the server config store and register the project.
	configStore := server.NewConfigStore(serverRoot)
	cfg := server.DefaultServerConfig()
	cfg.Runtime.ID = "conformance-runtime"
	if err := configStore.Save(cfg); err != nil {
		return nil, fmt.Errorf("save server config: %w", err)
	}

	reg := server.DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "Conformance Project"
	registryStore := server.NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		return nil, fmt.Errorf("register conformance project: %w", err)
	}

	// Shared env gate (coordinator Activate requires valid shared/.env):
	// provision minimal shared/.env so conformance activations pass
	// without requiring console DB provision (EKA mode).
	sharedEnvPath := filepath.Join(installRoot, "shared", ".env")
	if err := os.MkdirAll(filepath.Dir(sharedEnvPath), 0o755); err != nil {
		return nil, fmt.Errorf("create shared env dir: %w", err)
	}
	if err := os.WriteFile(sharedEnvPath, []byte("APP_ENV=production\nAPP_KEY=base64:testkey1234567890\n"), 0644); err != nil {
		return nil, fmt.Errorf("write shared env: %w", err)
	}

	recorder := newRecordingRunner()
	registry := adapter.NewCapabilityRegistry()
	exchange := adapter.NewCoordinator(recorder, registry)

	return &AnvilRuntime{
		serverRoot:  serverRoot,
		installRoot: installRoot,
		projectID:   projectID,
		coordinator: server.NewServerReleaseCoordinator(serverRoot),
		registry:    registry,
		exchange:    exchange,
		recorder:    recorder,
	}, nil
}

// --- artifact-manifest contract surface -----------------------------------

// Package produces an artifact through the engine's packaging operation
// (artifact.Package): content-derived identity, embedded manifest,
// deterministic output.
func (r *AnvilRuntime) Package(in PackageInput) (ArtifactInfo, error) {
	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: in.SourceDir,
		OutputDir: in.OutputDir,
		Version:   in.Version,
		Source:    in.Source,
		ProjectID: in.ProjectID,
	})
	if err != nil {
		return ArtifactInfo{}, err
	}
	return ArtifactInfo{
		Path:     result.ArtifactPath,
		Manifest: mapManifest(*result.Manifest),
	}, nil
}

// ReadManifest reads the manifest embedded in the artifact.
func (r *AnvilRuntime) ReadManifest(artifactPath string) (ManifestInfo, error) {
	manifest, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		return ManifestInfo{}, err
	}
	return mapManifest(*manifest), nil
}

// Verify runs the engine's integrity verification (artifact.VerifyArtifact).
func (r *AnvilRuntime) Verify(artifactPath string) (VerificationReport, error) {
	result, err := artifact.VerifyArtifact(artifactPath)
	if err != nil {
		return VerificationReport{}, err
	}
	report := VerificationReport{Passed: result.Passed}
	for _, check := range result.Checks {
		report.Checks = append(report.Checks, CheckOutcome{
			Name:    check.Name,
			Passed:  check.Passed,
			Details: check.Details,
		})
	}
	return report, nil
}

// RequireVerified enforces the mandatory integrity gate.
func (r *AnvilRuntime) RequireVerified(artifactPath string) error {
	return artifact.RequireVerified(artifactPath)
}

// TamperPayload builds a copy of the artifact whose deployable payload
// content differs from the content its embedded manifest declares — the
// state of an artifact whose content was altered after packaging
// (artifact-manifest.md §5.1). The embedded manifest (the claim) is
// preserved unchanged, so the deviation is detectable only by
// recomputation.
//
// The engine artifact layout — a gzip-compressed tar archive with the
// manifest at the archive root and the deployable payload under the
// "app/" prefix (internal/artifact) — is the binding's own knowledge:
// the harness interface defines no layout, and an independent runtime
// implements this fixture using its own. This is a fixture hook for the
// integrity-gate checks, not a lifecycle operation.
func (r *AnvilRuntime) TamperPayload(artifactPath string) (string, error) {
	dir, err := os.MkdirTemp(r.serverRoot, "tamper-")
	if err != nil {
		return "", err
	}

	// Extract the archive: deployable content under app/, manifest at
	// the root.
	var manifestBytes []byte
	var entries []struct {
		name string
		data []byte
	}
	f, err := os.Open(artifactPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return "", err
		}
		if hdr.Name == artifact.ManifestFile {
			manifestBytes = data
			continue
		}
		if strings.HasPrefix(hdr.Name, artifact.DeployableContentDir+"/") {
			entries = append(entries, struct {
				name string
				data []byte
			}{name: hdr.Name, data: data})
		}
	}

	// Alter one deployable file's content.
	for i := range entries {
		if strings.HasSuffix(entries[i].name, "index.php") {
			entries[i].data = []byte("<?php\n// ALTERED after packaging\n")
		}
	}

	// Repack with the SAME manifest: the recomputed hash now deviates
	// from the manifest declaration.
	outPath := filepath.Join(dir, "tampered.tar.gz")
	out, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer out.Close()
	gzw := gzip.NewWriter(out)
	tw := tar.NewWriter(gzw)
	for _, e := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.data)), Typeflag: tar.TypeReg}); err != nil {
			return "", err
		}
		if _, err := tw.Write(e.data); err != nil {
			return "", err
		}
	}
	if len(manifestBytes) > 0 {
		if err := tw.WriteHeader(&tar.Header{Name: artifact.ManifestFile, Mode: 0o644, Size: int64(len(manifestBytes)), Typeflag: tar.TypeReg}); err != nil {
			return "", err
		}
		if _, err := tw.Write(manifestBytes); err != nil {
			return "", err
		}
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gzw.Close(); err != nil {
		return "", err
	}
	return outPath, nil
}

// --- lifecycle-model contract surface -------------------------------------

// Install adopts a verified artifact and creates a Release in Ready via
// the server release coordinator.
func (r *AnvilRuntime) Install(artifactPath string) (ReleaseInfo, error) {
	rel, err := r.coordinator.Install(r.projectID, artifactPath)
	if err != nil {
		return ReleaseInfo{}, err
	}
	return mapRelease(rel), nil
}

// Activate executes the activation phase sequence via the coordinator.
func (r *AnvilRuntime) Activate(releaseID string) error {
	return r.coordinator.Activate(r.projectID, releaseID)
}

// Rollback restores the previously Active Release via the coordinator.
func (r *AnvilRuntime) Rollback() (RollbackOutcome, error) {
	result, err := r.coordinator.Rollback(r.projectID)
	if err != nil {
		return RollbackOutcome{}, err
	}
	outcome := RollbackOutcome{}
	if result.RolledBackRelease != nil {
		rel := mapRelease(result.RolledBackRelease)
		outcome.RolledBackRelease = &rel
	}
	if result.RestoredRelease != nil {
		rel := mapRelease(result.RestoredRelease)
		outcome.RestoredRelease = &rel
	}
	return outcome, nil
}

// ReconcileInterruptedRollback reconciles Releases stuck in Rolling Back
// through the engine's rollback reconciliation.
func (r *AnvilRuntime) ReconcileInterruptedRollback() ([]string, error) {
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = r.installRoot
	engine := release.NewRollbackEngine(
		r.installRoot,
		runtime.NewSharedResourceManager(cfg),
		runtime.NewSymlinkSwitcher(cfg),
		cfg.ReleasesDirPath(),
	)
	reconciled, err := engine.ReconcileInterruptedRollback()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(reconciled))
	for _, id := range reconciled {
		ids = append(ids, id.String())
	}
	return ids, nil
}

// PersistInterruptedRollback persists a Release in the Rolling Back
// transitional stage — the crash state a mid-rollback leaves behind
// (lifecycle-model.md §6.5) — through the engine's own public release
// persistence.
func (r *AnvilRuntime) PersistInterruptedRollback(releaseID string) error {
	rel, err := release.LookupByID(r.installRoot, release.ReleaseID(releaseID))
	if err != nil {
		return fmt.Errorf("lookup release %s: %w", releaseID, err)
	}
	if err := rel.Transition(release.StageRollingBack); err != nil {
		return fmt.Errorf("persist interrupted rollback: %w", err)
	}
	if err := rel.Save(rel.SavePath(r.installRoot)); err != nil {
		return fmt.Errorf("persist interrupted rollback: %w", err)
	}
	return nil
}

// StageOf returns the persisted stage of a Release.
func (r *AnvilRuntime) StageOf(releaseID string) (Stage, error) {
	stage, err := release.GetReleaseState(r.installRoot, release.ReleaseID(releaseID))
	if err != nil {
		return "", err
	}
	return mapStage(stage), nil
}

// ReleasesIn lists the Releases in the given stage.
func (r *AnvilRuntime) ReleasesIn(stage Stage) ([]ReleaseInfo, error) {
	rels, err := release.ListReleasesByStage(r.installRoot, mapStageBack(stage))
	if err != nil {
		return nil, err
	}
	out := make([]ReleaseInfo, 0, len(rels))
	for _, rel := range rels {
		out = append(out, mapRelease(rel))
	}
	return out, nil
}

// Active returns the Release currently in the Active stage, or nil.
func (r *AnvilRuntime) Active() (*ReleaseInfo, error) {
	rel, err := release.GetActiveRelease(r.installRoot)
	if err != nil {
		return nil, err
	}
	if rel == nil {
		return nil, nil
	}
	mapped := mapRelease(rel)
	return &mapped, nil
}

// HistoryOf returns the recorded transition history of a Release.
func (r *AnvilRuntime) HistoryOf(releaseID string) ([]Transition, error) {
	history, err := release.GetReleaseHistory(r.installRoot, release.ReleaseID(releaseID))
	if err != nil {
		return nil, err
	}
	out := make([]Transition, 0, len(history))
	for _, tr := range history {
		out = append(out, Transition{
			Timestamp: tr.Timestamp,
			From:      mapStage(tr.From),
			To:        mapStage(tr.To),
			Outcome:   tr.Outcome,
		})
	}
	return out, nil
}

// --- verification-contract evidence surface --------------------------------

// RegisterVerified records a verified artifact's outcome as lifecycle
// evidence through the engine's artifact registration store.
func (r *AnvilRuntime) RegisterVerified(artifactPath string) error {
	if err := artifact.RequireVerified(artifactPath); err != nil {
		return fmt.Errorf("artifact not verified: %w", err)
	}
	manifest, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.registryPath()), 0o755); err != nil {
		return fmt.Errorf("create registration evidence directory: %w", err)
	}
	store := artifact.NewRegistrationStore(r.registryPath())
	if _, err := store.Register(manifest, "passed"); err != nil {
		return err
	}
	return store.Save()
}

// RegistrationEvidence returns the recorded lifecycle evidence for an
// artifact identity, if recorded.
func (r *AnvilRuntime) RegistrationEvidence(artifactID string) (RegistrationInfo, bool) {
	store := artifact.NewRegistrationStore(r.registryPath())
	_ = store.Load()
	record, ok := store.Lookup(artifactID)
	if !ok {
		return RegistrationInfo{}, false
	}
	return RegistrationInfo{
		ArtifactID:         record.ArtifactID,
		Version:            record.Version,
		VerificationResult: record.VerificationResult,
		RegisteredAt:       record.RegisteredAt,
	}, true
}

// registryPath is where the binding persists the artifact registration
// evidence within the runtime under test.
func (r *AnvilRuntime) registryPath() string {
	return filepath.Join(r.installRoot, ".anvil", "state", "artifact-registry.json")
}

// --- command-contract exchange surface -------------------------------------

// RegisterCapability records a standard's declared capability surface in
// the engine's capability registry. The engine's v1.x adapter
// declaration carries no per-phase irreversibility field: irreversible
// phases are handled at rollback time — the adapter reports an
// informational success and irreversibility never blocks the rollback
// (TS-P7-10 AC-2), matching the contract's C4 clause. The binding
// therefore accepts the declaration's IrreversiblePhases documentation
// and keeps the invocation path unchanged: declared phases are invoked
// for rollback regardless.
func (r *AnvilRuntime) RegisterCapability(framework string, decl CapabilityDeclaration) error {
	checks := make([]contracts.VerificationCheck, 0, len(decl.VerificationChecks))
	for _, name := range decl.VerificationChecks {
		checks = append(checks, contracts.VerificationCheck{Name: name, Description: "conformance declared check"})
	}
	return r.registry.Register(framework, contracts.CapabilityDeclaration{
		ActivationPhases:   append([]string(nil), decl.ActivationPhases...),
		VerificationChecks: checks,
	})
}

// DeclaredPhases returns the declared activation phases in declared
// order.
func (r *AnvilRuntime) DeclaredPhases(framework string) ([]string, bool) {
	return r.exchange.ActivationPhases(framework)
}

// InvokePhase invokes one activation phase operation through the engine's
// subprocess coordinator. Undeclared capability is rejected before any
// subprocess invocation (C1).
func (r *AnvilRuntime) InvokePhase(framework, phase, operation string) error {
	req := contracts.ActivationRequest{
		Phase:     phase,
		Operation: contracts.PhaseOperation(operation),
		Release: contracts.ReleaseContext{
			ProjectID: r.projectID,
		},
	}
	_, err := r.exchange.InvokeActivation(context.Background(), framework, "conformance-stub-adapter", req)
	return err
}

// InvokeCheck invokes one verification check through the engine's
// subprocess coordinator. Undeclared checks are rejected (C1).
func (r *AnvilRuntime) InvokeCheck(framework, check string) error {
	req := contracts.VerificationRequest{
		Check:        check,
		ArtifactPath: "conformance-fixture",
	}
	_, err := r.exchange.InvokeVerification(context.Background(), framework, "conformance-stub-adapter", req)
	return err
}

// SubprocessCalls returns the observed invocation log at the subprocess
// boundary.
func (r *AnvilRuntime) SubprocessCalls() []SubprocessCall {
	return r.recorder.snapshot()
}

// --- engine mapping helpers ------------------------------------------------

// mapRelease maps the engine's Release onto the contract-level Release
// fact.
func mapRelease(rel *release.Release) ReleaseInfo {
	return ReleaseInfo{
		ID:           rel.ID.String(),
		ArtifactID:   rel.ArtifactID,
		Version:      rel.Version,
		Stage:        mapStage(rel.Stage),
		ArtifactPath: rel.ArtifactPath,
	}
}

// mapStage maps the engine's stage onto the contract stage name
// (lifecycle-model.md §6.1).
func mapStage(s release.Stage) Stage {
	switch s {
	case release.StageReady:
		return StageReady
	case release.StageActivating:
		return StageActivating
	case release.StageActive:
		return StageActive
	case release.StageRollingBack:
		return StageRollingBack
	case release.StageRolledBack:
		return StageRolledBack
	case release.StageArchived:
		return StageArchived
	case release.StageFailed:
		return StageFailed
	case release.StageRemoved:
		return StageRemoved
	default:
		return Stage(s.String())
	}
}

// mapStageBack maps the contract stage name onto the engine's stage.
func mapStageBack(s Stage) release.Stage {
	switch s {
	case StageReady:
		return release.StageReady
	case StageActivating:
		return release.StageActivating
	case StageActive:
		return release.StageActive
	case StageRollingBack:
		return release.StageRollingBack
	case StageRolledBack:
		return release.StageRolledBack
	case StageArchived:
		return release.StageArchived
	case StageFailed:
		return release.StageFailed
	case StageRemoved:
		return release.StageRemoved
	default:
		return release.Stage(0)
	}
}

// mapManifest maps the engine's manifest onto the contract-level
// manifest fact.
func mapManifest(m artifact.Manifest) ManifestInfo {
	return ManifestInfo{
		ArtifactID:   m.ArtifactID,
		Version:      m.Version,
		CreatedAt:    m.CreatedAt,
		Source:       m.Source,
		Checksum:     m.Checksum,
		ChecksumType: m.ChecksumType,
		ProjectID:    m.ProjectID,
	}
}

// recordingRunner is a stub subprocess runner at the runtime–standard
// boundary: it observes every invocation the runtime would make (the
// command name, the JSON payload, the JSON result) and answers with the
// canned JSON document a conforming standard would return. It is the
// observable subprocess boundary the command-contract checks assert on;
// it never asserts anything itself.
type recordingRunner struct {
	mu    sync.Mutex
	calls []SubprocessCall
}

// newRecordingRunner creates an empty recording runner.
func newRecordingRunner() *recordingRunner {
	return &recordingRunner{}
}

// Execute records the invocation and returns the stub standard's JSON
// result on stdout (execution.StatusSuccess), mirroring the exchange
// shape of the command contract (command-contract.md §5).
func (r *recordingRunner) Execute(_ context.Context, req execution.ExecutionRequest) execution.Result {
	stdout := stubStdout(req.Args)

	r.mu.Lock()
	r.calls = append(r.calls, SubprocessCall{
		Command: commandOf(req.Args),
		Args:    append([]string(nil), req.Args...),
		Stdout:  stdout,
	})
	r.mu.Unlock()

	return execution.Result{
		Status:   execution.StatusSuccess,
		ExitCode: 0,
		Stdout:   stdout,
	}
}

// snapshot returns a copy of the observed invocation log.
func (r *recordingRunner) snapshot() []SubprocessCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SubprocessCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// commandOf returns the contract command name of an invocation — the
// first argument the runtime passes to the subprocess
// (<executable> <command> [<json-payload>]).
func commandOf(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// stubStdout builds the JSON document a conforming standard returns for
// the invocation: a success result for activation phases, a passing
// outcome for verification checks. The document is derived from the
// request payload, mirroring the exchange.
func stubStdout(args []string) string {
	if len(args) < 2 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(args[1]), &payload); err != nil {
		return ""
	}
	switch args[0] {
	case contracts.CommandActivation:
		data, _ := json.Marshal(contracts.ActivationResult{Success: true, Output: "conformance stub phase"})
		return string(data)
	case contracts.CommandVerification:
		check, _ := payload["check"].(string)
		data, _ := json.Marshal(contracts.VerificationOutcome{Name: check, Passed: true, Details: "conformance stub check"})
		return string(data)
	default:
		return ""
	}
}
