// Package server provides models and utilities for managing Anvil Server
// Runtime configuration and coordinates installation and activation of
// Runtime Releases using registered project metadata.
//
// Reference: TS-P4-11, ST-P4-13, ST-P4-14, EPIC-005
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"maleolabs.com/anvil/internal/adapter"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
)

// ServerReleaseCoordinator coordinates Runtime installation and activation
// using a registered project ID and Runtime configuration without repository
// discovery.
//
// Reference: TS-P4-11, ST-P4-13, ST-P4-14
type ServerReleaseCoordinator struct {
	// serverRoot is the config root path (default: /etc/anvil) that holds
	// the project registry YAML files.
	serverRoot string

	// adapterRunner is the Process Runner used to invoke adapter
	// executables (TS-P7-08). Nil means the real runner
	// (execution.NewRunner()) is used; tests inject a runner here.
	adapterRunner execution.Runner

	// adapterExecutable resolves the adapter executable path for a
	// framework. Nil means the convention
	// exec.LookPath("anvil-adapter-<framework>") is used; tests inject a
	// resolver here.
	adapterExecutable func(framework string) (string, error)

	// storedArtifactVerifier re-verifies the artifact copy after it is
	// stored in the artifact store, before the release is created
	// (TD-009). Nil means the default implementation
	// (verifyStoredArtifact) is used; tests inject a verifier here to
	// simulate a TOCTOU between verification and copy.
	storedArtifactVerifier func(storeArtifactPath string, expected *artifact.Manifest) error

	// warnWriter is the destination for deprecation warnings emitted when
	// a project declares the legacy project.adapter key (TS-019-02-02).
	// Nil means os.Stderr; the CLI injects the command's stderr writer
	// via WithWarningWriter so warnings never pollute machine-readable
	// stdout (T-003/T-005 precedent).
	warnWriter io.Writer
}

// ServerReleaseCoordinatorOption configures a ServerReleaseCoordinator.
type ServerReleaseCoordinatorOption func(*ServerReleaseCoordinator)

// NewServerReleaseCoordinator creates a coordinator that uses the given
// server root to resolve project registries and coordinate release operations.
//
// The serverRoot must point to a directory containing the projects registry
// subdirectory (projects/). Use RootPath() or resolveServerRoot() to resolve
// the effective path including ANVIL_SERVER_ROOT overrides.
//
// Optional options configure behavior; see WithWarningWriter.
func NewServerReleaseCoordinator(serverRoot string, opts ...ServerReleaseCoordinatorOption) *ServerReleaseCoordinator {
	c := &ServerReleaseCoordinator{serverRoot: serverRoot}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithWarningWriter sets the writer used for deprecation warnings (the
// legacy project.adapter alias, TS-019-02-02). The CLI passes the
// command's stderr writer so warnings go to stderr; nil means
// os.Stderr.
func WithWarningWriter(w io.Writer) ServerReleaseCoordinatorOption {
	return func(c *ServerReleaseCoordinator) { c.warnWriter = w }
}

// warningWriter returns the configured warning writer, defaulting to
// os.Stderr when none was injected.
func (c *ServerReleaseCoordinator) warningWriter() io.Writer {
	if c.warnWriter != nil {
		return c.warnWriter
	}
	return os.Stderr
}

// Install validates an artifact, stores it in the Runtime Artifact Store,
// creates a Runtime Release in Ready stage, creates the release directory,
// and registers it in the Runtime State.
//
// The installation sequence:
//  1. Load the project registry to resolve the install root.
//  2. Validate the artifact file exists on disk.
//  3. Verify artifact integrity (checksum, manifest, archive).
//  4. Read the artifact manifest and validate project identity.
//  5. Generate a unique Release identity.
//  6. Build RuntimeConfig from the registry's InstallRoot.
//  7. Create the Runtime directory structure (artifacts, state, shared, tmp).
//  8. Copy the artifact into the artifact store.
//     8a. Re-verify the stored copy against the verified source manifest
//     (TD-009) so the release is never created from a payload whose
//     stored form was not proven intact.
//  9. Create the release directory (releases/rel-<identity>).
//  10. Create the Release struct with Ready stage.
//  11. Persist the Release JSON to disk (.anvil/state/releases/<id>.json).
//  12. Initialize the Runtime State store.
//
// Returns the created Release or an error describing the failure.
//
// Reference: ST-P4-13, TD-009
func (c *ServerReleaseCoordinator) Install(projectID, artifactPath string) (*release.Release, error) {
	// Step 1: Load project registry to resolve the install root.
	registryStore := NewRegistryStore(c.serverRoot)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		return nil, fmt.Errorf("load project registry: %w", err)
	}
	installRoot := reg.Project.InstallRoot

	// Lifecycle operations on one server are serialized across processes by
	// the operation lock (ADR-031 §3 keep list: locking; ADR-014 baseline
	// safety: reject concurrent activation/rollback operations for the same
	// project; TS-015-04-03). The runtime is invoked per command, so each
	// Install runs in a fresh process — an in-process mutex cannot serialize
	// concurrent commands, and the read-modify-write of runtime-state.json
	// needs cross-process atomicity. The flock on runtime-state.lock guards
	// the whole operation; a concurrent lifecycle operation is safely
	// rejected with a descriptive error instead of racing state mutations.
	// The kernel releases the lock if this process dies, so an interrupted
	// operation never wedges the server. Locking never depends on
	// diagnostics (ADR-036 §3).
	opLock := runtime.NewOperationLock(installRoot)
	if err := opLock.Acquire("install"); err != nil {
		return nil, err
	}
	defer opLock.Release() // best-effort on failure paths; the kernel lock always drops

	// Step 2: Validate artifact exists.
	if _, err := os.Stat(artifactPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("artifact not found: %s", artifactPath)
		}
		return nil, fmt.Errorf("access artifact: %w", err)
	}

	// Step 3: Verify artifact integrity.
	if err := artifact.RequireVerified(artifactPath); err != nil {
		return nil, fmt.Errorf("artifact must be verified first: %w", err)
	}

	// Step 3a: Adapter verification checks (TS-P7-11). When the project
	// selects a framework adapter, the adapter's declared verification
	// checks run alongside the generic checks and must pass before the
	// artifact is installed. Framework-agnostic: the framework name and
	// the declared checks come from project configuration and the
	// adapter's capability declaration (ADR-009 §8.1, §9.6).
	if err := c.runAdapterVerification(context.Background(), reg, artifactPath); err != nil {
		return nil, err
	}

	// Step 4: Read manifest and validate project identity.
	manifest, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("read artifact manifest: %w", err)
	}

	if manifest.ProjectID != projectID {
		return nil, fmt.Errorf(
			"artifact project ID %q does not match registered project ID %q",
			manifest.ProjectID, projectID,
		)
	}

	// Step 5: Build RuntimeConfig from registry's InstallRoot.
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot

	// Step 6: Check if this ArtifactID is already installed (idempotency).
	// The same manifest-defined Artifact must not create a duplicate Runtime Release.
	// The state directory is the same one internal/release reads
	// (<installRoot>/.anvil/state/releases — BUG-002).
	releasesStateDir := releaseStateDir(installRoot)
	if existingRelease := findReleaseByArtifactID(releasesStateDir, manifest.ArtifactID); existingRelease != nil {
		return nil, fmt.Errorf("artifact %q is already installed as release %s", manifest.ArtifactID, existingRelease.ID)
	}
	// Back-compat (BUG-002): server roots provisioned before the layout was
	// unified keep Releases in <installRoot>/state/releases. Honor the
	// idempotency check there too, so a legacy root cannot double-install
	// the same artifact. Read-only — the coordinator never writes there.
	if existingRelease := findReleaseByArtifactID(legacyReleaseStateDir(installRoot), manifest.ArtifactID); existingRelease != nil {
		return nil, fmt.Errorf("artifact %q is already installed as release %s", manifest.ArtifactID, existingRelease.ID)
	}

	// Step 7: Generate release identity.
	id, err := release.GenerateReleaseID()
	if err != nil {
		return nil, fmt.Errorf("generate release id: %w", err)
	}

	// Step 8: Create all runtime directories (idempotent).
	// This ensures artifacts, state, shared resources, and tmp all exist
	// so that subsequent activation can succeed without missing directories.
	dirs := []string{
		filepath.Join(installRoot, "artifacts"),
		releaseStateDir(installRoot),
		runtimeCfg.ReleasesDirPath(),
		runtimeCfg.SharedConfigDirPath(),
		runtimeCfg.SharedStorageDirPath(),
		runtimeCfg.LogsDirPath(),
		runtimeCfg.TempDirPath(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("create directory %s: %w", d, err)
		}
	}

	// Step 9: Copy artifact file to the artifact store.
	artifactStoreDir := filepath.Join(installRoot, "artifacts")
	storeArtifactPath := filepath.Join(artifactStoreDir, id.String()+".tar.gz")
	if err := copyFile(artifactPath, storeArtifactPath); err != nil {
		return nil, fmt.Errorf("copy artifact to store: %w", err)
	}

	// Step 9a: Re-verify the stored copy before the release is created
	// (TD-009). The source is verified in step 3 and copied here, leaving a
	// TOCTOU window: if the source changed between verification and copy, or
	// the copy is corrupt (interrupted write, filesystem error io.Copy does
	// not surface), the stored bytes differ from the verified bytes. The
	// stored copy must pass full verification AND carry the same manifest
	// the release record is built from; otherwise no release is created and
	// the unverified copy is removed so the artifact store only ever holds
	// verified payloads (ADR-017).
	verifyStored := c.storedArtifactVerifier
	if verifyStored == nil {
		verifyStored = verifyStoredArtifact
	}
	if err := verifyStored(storeArtifactPath, manifest); err != nil {
		// Best-effort cleanup: the artifact store's contract is to hold
		// verified artifacts only, and no release references this copy.
		_ = os.Remove(storeArtifactPath)
		return nil, fmt.Errorf("verify stored artifact: %w", err)
	}

	// Step 10: Create the release directory (empty; extraction happens during activate).
	releaseDir, err := runtime.CreateReleaseDir(runtimeCfg.ReleasesDirPath(), id.String())
	if err != nil {
		return nil, fmt.Errorf("create release directory: %w", err)
	}
	_ = releaseDir // release directory ready for artifact extraction during activate

	// Step 11: Create Release struct.
	now := time.Now().UTC()
	rel := &release.Release{
		ID:           id,
		ArtifactID:   manifest.ArtifactID,
		Version:      manifest.Version,
		Source:       manifest.Source,
		ArtifactPath: storeArtifactPath,
		RuntimePath:  installRoot,
		Stage:        release.StageReady,
		CreatedAt:    now.Format(time.RFC3339),
		Transitions:  []release.TransitionRecord{},
	}

	// Step 12: Persist the Release JSON to disk.
	releasePath := filepath.Join(releasesStateDir, id.String()+".json")
	if err := rel.Save(releasePath); err != nil {
		return nil, fmt.Errorf("save release: %w", err)
	}

	// Step 13: Refresh the Runtime State store without clobbering existing
	// operational state (ADR-031 §3, §6: state survives crashes and restarts,
	// install must never reset it). A state file written by a previous
	// install or activation is loaded first so the recorded active release
	// survives the install; a missing file (first install) initializes the
	// default state. An existing but unreadable state file is a hard error —
	// install never overwrites state it cannot read, so a corrupt state file
	// is preserved for recovery instead of being silently discarded.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	if _, err := os.Stat(statePath); err == nil {
		if err := stateStore.Load(); err != nil {
			return nil, fmt.Errorf("load existing runtime state: %w", err)
		}
	}
	if err := stateStore.Save(); err != nil {
		return nil, fmt.Errorf("save runtime state: %w", err)
	}

	return rel, nil
}

// Activate resolves the Release from Runtime State and executes the activation
// phase sequence (Prepare → Configure → Promote).
//
// The activation sequence:
//  1. Load the project registry to resolve the install root.
//  2. Build RuntimeConfig from the registry's InstallRoot.
//  3. Load the Release from its persisted JSON.
//  4. Extract the stored artifact into the release directory.
//  5. Create activation dependencies (SharedResourceManager, PromoteRunner,
//     ActiveReleaseInvariant — BUG-003).
//  6. Execute the activation phase sequence via ActivationEngine.
//  7. Persist the updated Release (stage transitions).
//  8. Update RuntimeState with the active release ID.
//
// Persistence ordering (TD-002 crash-recovery invariant):
//
//  1. The ActiveReleaseInvariant archives the previously Active Release
//     (atomic persist) before promotion.
//  2. PromoteRunner switches the active symlink atomically — this is the
//     activation commit point (TS-P5-08).
//  3. The Release JSON is persisted atomically (temp file + fsync + rename,
//     fsutil.WriteFileAtomic), recording the Active stage.
//  4. RuntimeState is persisted atomically, recording active_release_id.
//
// Every state file is written atomically, so a crash at any point leaves
// either the complete previous file or the complete new file — never a
// truncated one. Because the symlink is switched before the state persists,
// a crash in the window between steps 2 and 4 leaves the symlink ahead of
// the persisted stage (the Release JSON still records the pre-activation
// stage, ready — nothing persisted Activating; the last persisted stage of
// the new Release comes from Install). That is a consistent, recoverable
// combination: the Release JSON is the authoritative stage record (TS-P4-09
// — GetActiveRelease reads stages from the persisted JSON, not the symlink),
// and re-running activation (ADR-003 §6.3) or rollback reconciliation
// (BUG-004) converges the system.
//
// Returns nil on success or an error describing the failure.
//
// Reference: ST-P4-14, TD-002
func (c *ServerReleaseCoordinator) Activate(projectID, releaseID string) error {
	// Step 1: Load project registry.
	registryStore := NewRegistryStore(c.serverRoot)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		return fmt.Errorf("load project registry: %w", err)
	}
	installRoot := reg.Project.InstallRoot

	// Serialize this operation against concurrent lifecycle operations on
	// the same server (see Install; TS-015-04-03, ADR-014). The lock guards
	// the activation state handling — release extraction, symlink
	// promotion, and the runtime-state read-modify-write — so two
	// concurrent activations cannot interleave. Rejected contenders fail
	// fast with a descriptive error.
	opLock := runtime.NewOperationLock(installRoot)
	if err := opLock.Acquire("activate"); err != nil {
		return err
	}
	defer opLock.Release()

	// Step 2: Build RuntimeConfig.
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot

	// Step 3: Load Release from the unified runtime state directory
	// (.anvil/state/releases — the same directory internal/release reads;
	// BUG-002). releasePath is always the canonical save target, so any
	// later Save migrates a legacy-loaded Release to the unified layout.
	releasePath := filepath.Join(releaseStateDir(installRoot), releaseID+".json")
	loadPath := releasePath
	if _, err := os.Stat(loadPath); err != nil {
		if os.IsNotExist(err) {
			// Back-compat (BUG-002): server roots provisioned before the
			// layout was unified keep Releases in
			// <installRoot>/state/releases. Read from the legacy location
			// so existing roots remain readable. Read-only fallback — the
			// legacy directory is never written.
			legacyPath := filepath.Join(legacyReleaseStateDir(installRoot), releaseID+".json")
			if _, err := os.Stat(legacyPath); err == nil {
				loadPath = legacyPath
			}
		}
	}
	rel, err := release.Load(loadPath)
	if err != nil {
		return fmt.Errorf("load release: %w", err)
	}

	// Migration (BUG-002 back-compat): when the Release was loaded from the
	// legacy directory, the canonical state directory may not exist on a
	// pure legacy root — Release.Save does not create parent directories.
	// Create it before any Save so the post-activation Save and the
	// best-effort failure-path Saves can persist the migrated Release.
	if loadPath != releasePath {
		if err := os.MkdirAll(filepath.Dir(releasePath), 0755); err != nil {
			return fmt.Errorf("create release state directory %s: %w", filepath.Dir(releasePath), err)
		}
	}

	// Step 4: Extract the stored artifact into the release directory.
	// This must happen before the activation engine runs so that the release
	// directory contains deployable files when the symlink is switched.
	releaseDir := runtime.ReleaseDirPath(runtimeCfg.ReleasesDirPath(), releaseID)
	if err := artifact.ExtractArtifact(rel.ArtifactPath, releaseDir); err != nil {
		return fmt.Errorf("extract artifact for activation: %w", err)
	}

	// Step 5: Create activation dependencies.
	sharedMgr := runtime.NewSharedResourceManager(runtimeCfg)
	switcher := runtime.NewSymlinkSwitcher(runtimeCfg)
	promoteRunner := release.NewPromoteRunner(switcher, runtimeCfg.ReleasesDirPath())
	// The ActiveReleaseInvariant archives the previously Active Release
	// before promotion, so exactly one Release is Active at any time
	// (TS-P4-10, ADR-003 §9.1). It is wired here — the production
	// activation path (BUG-003); before the fix the invariant was nil and
	// no Release ever transitioned Active → Archived, leaving rollback
	// without a target.
	engine := release.NewActivationEngine(
		sharedMgr,
		promoteRunner,
		release.NewActiveReleaseInvariant(installRoot),
	)

	// Step 6: Execute activation phase sequence.
	if err := engine.Activate(rel); err != nil {
		// Save the Release with any recorded transitions (best-effort).
		_ = rel.Save(releasePath)
		return fmt.Errorf("activation failed: %w", err)
	}

	// Step 6.5: Invoke the adapter's declared activation phases
	// (TS-P7-09). When the project selects a framework adapter, each
	// declared phase runs in the release directory via the Process
	// Runner; a failing phase fails the activation. Framework-agnostic:
	// phases come from the adapter's capability declaration (ADR-009
	// §8.1, §9.6).
	if err := c.runAdapterActivation(context.Background(), reg, releaseID, releaseDir); err != nil {
		// Save the Release with any recorded transitions (best-effort),
		// matching the engine failure path.
		_ = rel.Save(releasePath)
		return err
	}

	// Step 7: Save updated Release state.
	if err := rel.Save(releasePath); err != nil {
		return fmt.Errorf("save release state: %w", err)
	}

	// Migration (BUG-002 back-compat): the Release is now persisted in the
	// canonical directory; remove the stale legacy copy so no dual source of
	// truth remains. Best-effort — a leftover legacy file is harmless (the
	// read path is canonical-only).
	if loadPath != releasePath {
		_ = os.Remove(loadPath)
	}

	// Step 8: Update RuntimeState with active release. The existing state
	// is loaded first so unrelated operational state (runtime condition,
	// shared resource status) survives the activation — only the active
	// release record changes (ADR-031 §3, §6: state is authoritative and
	// survives; decisions derive from state). An unreadable state file is a
	// hard error, never silently overwritten.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	if _, err := os.Stat(statePath); err == nil {
		if err := stateStore.Load(); err != nil {
			return fmt.Errorf("load existing runtime state: %w", err)
		}
	}
	stateStore.SetActiveRelease(rel.ID.String())
	if err := stateStore.Save(); err != nil {
		return fmt.Errorf("save runtime state: %w", err)
	}

	return nil
}

// Rollback executes the rollback phase sequence for the currently Active
// Release in the given project. It identifies the rollback target (the
// previously Active Release), reverses configuration changes, and promotes
// the target back to Active.
//
// The rollback sequence:
//  1. Load the project registry to resolve the install root.
//  2. Build RuntimeConfig from the registry's InstallRoot.
//  3. Create rollback dependencies (SharedResourceManager, SymlinkSwitcher).
//  4. Execute engine.Rollback() to identify and restore the target Release.
//  5. Update RuntimeState with the restored release as active.
//
// Unlike Activate, Rollback does NOT take a releaseID — the RollbackEngine
// identifies the current Active Release and the rollback target automatically.
//
// Returns the RollbackResult describing both rolled-back and restored Releases,
// or an error describing the failure.
//
// Reference: ST-P4-07
func (c *ServerReleaseCoordinator) Rollback(projectID string) (*release.RollbackResult, error) {
	// Step 1: Load project registry.
	registryStore := NewRegistryStore(c.serverRoot)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		return nil, fmt.Errorf("load project registry: %w", err)
	}
	installRoot := reg.Project.InstallRoot

	// Serialize this operation against concurrent lifecycle operations on
	// the same server (see Install; TS-015-04-03, ADR-014). The lock guards
	// the rollback state handling — stage reconciliation, symlink
	// restoration, and the runtime-state read-modify-write — so two
	// concurrent rollbacks cannot interleave. Rejected contenders fail fast
	// with a descriptive error.
	opLock := runtime.NewOperationLock(installRoot)
	if err := opLock.Acquire("rollback"); err != nil {
		return nil, err
	}
	defer opLock.Release()

	// Step 2: Build RuntimeConfig.
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot

	// Step 3: Create rollback dependencies.
	sharedMgr := runtime.NewSharedResourceManager(runtimeCfg)
	switcher := runtime.NewSymlinkSwitcher(runtimeCfg)
	engine := release.NewRollbackEngine(installRoot, sharedMgr, switcher, runtimeCfg.ReleasesDirPath())

	// Step 3a: Reconcile any interrupted rollback operations before proceeding.
	// If a previous rollback was interrupted (e.g., process crash), Releases
	// stuck in RollingBack stage must be transitioned to RolledBack before
	// another rollback is attempted.
	if _, err := engine.ReconcileInterruptedRollback(); err != nil {
		return nil, fmt.Errorf("reconcile interrupted rollback: %w", err)
	}

	// Step 4: Execute rollback phase sequence.
	result, err := engine.Rollback()
	if err != nil {
		return nil, fmt.Errorf("rollback failed: %w", err)
	}

	// Step 4.5: Invoke the adapter's declared rollback operations
	// (TS-P7-10). When the project selects a framework adapter, each
	// declared phase receives the rollback operation in the restored
	// release directory. Irreversible phases do not fail the rollback —
	// the adapter reports an informational success for them (TS-P7-10
	// AC-2).
	if err := c.runAdapterRollback(context.Background(), reg, result); err != nil {
		return nil, err
	}

	// Step 5: Update RuntimeState with the restored release as active. The
	// existing state is loaded first so unrelated operational state survives
	// the rollback — only the active release record changes (ADR-031 §3, §6).
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	if _, err := os.Stat(statePath); err == nil {
		if err := stateStore.Load(); err != nil {
			return nil, fmt.Errorf("load existing runtime state: %w", err)
		}
	}
	stateStore.SetActiveRelease(result.RestoredRelease.ID.String())
	if err := stateStore.Save(); err != nil {
		return nil, fmt.Errorf("save runtime state: %w", err)
	}

	return result, nil
}

// releaseMeta is a minimal Release representation for scanning releases without
// loading the full Release struct (which may contain large mutable fields).
type releaseMeta struct {
	ArtifactID string `json:"artifact_id"`
	ID         string `json:"id"`
}

// releaseStateDir returns the canonical directory where Release JSON files
// are persisted. It resolves through project.NewStructure, the single source
// of truth for the project layout, so the coordinator writes and reads the
// same directory the internal/release package reads:
// <installRoot>/.anvil/state/releases/.
//
// Reference: BUG-002, TS-P4-10
func releaseStateDir(installRoot string) string {
	return filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
}

// legacyReleaseStateDir returns the pre-BUG-002 release state directory
// (<installRoot>/state/releases). It is used only as a read-only back-compat
// fallback for server roots provisioned before the layout was unified; the
// coordinator never writes to it.
//
// Reference: BUG-002
func legacyReleaseStateDir(installRoot string) string {
	return filepath.Join(installRoot, "state", "releases")
}

// findReleaseByArtifactID scans the releases state directory for a Release
// with the given ArtifactID. Returns the first matching Release (as releaseMeta)
// or nil if no match is found. If the releases directory does not exist, it
// returns nil without error.
func findReleaseByArtifactID(releasesDir, artifactID string) *releaseMeta {
	entries, err := os.ReadDir(releasesDir)
	if err != nil {
		// Directory doesn't exist yet — no releases installed.
		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(releasesDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var meta releaseMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}

		if meta.ArtifactID == artifactID {
			return &meta
		}
	}

	return nil
}

// copyFile copies a file from src to dst efficiently. The destination file
// is created with 0644 permissions. If the destination already exists, it is
// truncated.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}
	return out.Close()
}

// verifyStoredArtifact re-verifies the artifact copy stored in the artifact
// store before the release is created (TD-009). It closes the TOCTOU gap
// between verifying the artifact at its source path and copying it into the
// store: the release must never be created from a payload whose stored form
// was not proven intact.
//
// The check has two parts:
//  1. Full verification of the stored copy (artifact.RequireVerified) —
//     catches a corrupt copy (interrupted write, bit rot, filesystem error)
//     and a source that changed to an inconsistent artifact between
//     verification and copy.
//  2. Manifest equality between the stored copy and the source manifest the
//     release record is built from — catches a source that was swapped for a
//     different, equally valid artifact between verification and copy.
//
// The manifest's declared checksum covers the deployable content (TS-P3-06),
// not the archive bytes, so the stored copy cannot be byte-compared against
// that value; comparing the stored copy's manifest against the verified
// source manifest is instead the content-equality proof that the release
// record describes the stored payload.
//
// Reference: TD-009, ADR-004 §8.9, ADR-017
func verifyStoredArtifact(storeArtifactPath string, expected *artifact.Manifest) error {
	if err := artifact.RequireVerified(storeArtifactPath); err != nil {
		return fmt.Errorf("stored artifact failed verification: %w", err)
	}

	stored, err := artifact.ReadManifest(storeArtifactPath)
	if err != nil {
		return fmt.Errorf("read stored artifact manifest: %w", err)
	}

	if !reflect.DeepEqual(stored, expected) {
		return fmt.Errorf(
			"stored artifact manifest does not match the verified source artifact (stored artifact_id %q, verified artifact_id %q)",
			stored.ArtifactID, expected.ArtifactID,
		)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Adapter wiring (TS-P7-09, TS-P7-10, TS-P7-11, TS-P7-12)
// ---------------------------------------------------------------------------

// runAdapterActivation invokes the project adapter's declared activation
// phases after the ActivationEngine completes. Each phase runs via the
// Process Runner with the release directory as working dir (TS-P7-09).
// A failing phase — process failure or a phase result with Success=false
// — fails the activation. A project without an adapter, or an adapter
// declaring no phases, is a no-op (ADR-009 §9.7).
//
// The wiring is framework-agnostic: the framework name comes from the
// project registry (canonical key project.standard; the legacy
// project.adapter key is honored during the deprecation window per
// ADR-032 — see ProjectSection.StandardName), the phases from the
// adapter's capability declaration, and the executable from the resolver
// convention. No framework-specific value exists in the Core (ADR-009
// §8.1, §9.6).
func (c *ServerReleaseCoordinator) runAdapterActivation(ctx context.Context, reg *ProjectRegistry, releaseID, releaseDir string) error {
	framework := reg.Project.StandardName()
	// The legacy project.adapter key is read as an alias during the
	// deprecation window: every read emits a deprecation warning naming
	// project.standard (TS-019-02-02, ADR-032). The warning goes to
	// stderr (warnWriter), never stdout.
	reg.Project.WarnIfLegacyAdapter(c.warningWriter())
	if framework == "" {
		return nil
	}

	coord, executable, err := c.adapterCoordinator(ctx, framework)
	if err != nil {
		return fmt.Errorf("adapter activation failed: %w", err)
	}
	phases, ok := coord.ActivationPhases(framework)
	if !ok || len(phases) == 0 {
		return nil
	}

	for _, phase := range phases {
		req := contracts.ActivationRequest{
			Phase:     phase,
			Operation: contracts.PhaseOperationActivate,
			Release: contracts.ReleaseContext{
				ProjectID:  reg.Project.ID,
				ReleaseID:  releaseID,
				WorkingDir: releaseDir,
			},
		}
		phaseResult, err := coord.InvokeActivation(ctx, framework, executable, req)
		if err != nil {
			return fmt.Errorf("adapter activation failed: activation of phase %q: %w", phase, err)
		}
		if !phaseResult.Success {
			return fmt.Errorf("adapter activation failed: activation of phase %q: %s", phase, phaseResult.Error)
		}
	}
	return nil
}

// runAdapterRollback invokes the project adapter's declared rollback
// operations after the RollbackEngine completes. Each declared phase
// receives the rollback operation with the restored release directory as
// working dir (TS-P7-10); the release context carries the rolled-back
// release ID — the release whose activation is being reversed. Phases
// that cannot be reversed report an informational success instead of
// failing, so irreversible operations never block the rollback (TS-P7-10
// AC-2).
func (c *ServerReleaseCoordinator) runAdapterRollback(ctx context.Context, reg *ProjectRegistry, result *release.RollbackResult) error {
	framework := reg.Project.StandardName()
	// Legacy project.adapter reads warn during the deprecation window
	// (TS-019-02-02, ADR-032) — stderr, never stdout.
	reg.Project.WarnIfLegacyAdapter(c.warningWriter())
	if framework == "" {
		return nil
	}

	coord, executable, err := c.adapterCoordinator(ctx, framework)
	if err != nil {
		return fmt.Errorf("adapter rollback failed: %w", err)
	}
	phases, ok := coord.ActivationPhases(framework)
	if !ok || len(phases) == 0 {
		return nil
	}

	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = reg.Project.InstallRoot

	rolledBackID := ""
	if result.RolledBackRelease != nil {
		rolledBackID = result.RolledBackRelease.ID.String()
	}
	restoredDir := ""
	if result.RestoredRelease != nil {
		restoredDir = runtime.ReleaseDirPath(runtimeCfg.ReleasesDirPath(), result.RestoredRelease.ID.String())
	}

	for _, phase := range phases {
		req := contracts.ActivationRequest{
			Phase:     phase,
			Operation: contracts.PhaseOperationRollback,
			Release: contracts.ReleaseContext{
				ProjectID:  reg.Project.ID,
				ReleaseID:  rolledBackID,
				WorkingDir: restoredDir,
			},
		}
		phaseResult, err := coord.InvokeActivation(ctx, framework, executable, req)
		if err != nil {
			return fmt.Errorf("adapter rollback failed: rollback of phase %q: %w", phase, err)
		}
		if !phaseResult.Success {
			return fmt.Errorf("adapter rollback failed: rollback of phase %q: %s", phase, phaseResult.Error)
		}
	}
	return nil
}

// runAdapterVerification invokes the project adapter's declared
// verification checks against the artifact during installation
// (TS-P7-11). The checks run after the generic artifact verification
// passed; every declared check must pass for the install to proceed.
// Framework-agnostic: checks come from the adapter's capability
// declaration.
func (c *ServerReleaseCoordinator) runAdapterVerification(ctx context.Context, reg *ProjectRegistry, artifactPath string) error {
	framework := reg.Project.StandardName()
	// Legacy project.adapter reads warn during the deprecation window
	// (TS-019-02-02, ADR-032) — stderr, never stdout.
	reg.Project.WarnIfLegacyAdapter(c.warningWriter())
	if framework == "" {
		return nil
	}

	coord, executable, err := c.adapterCoordinator(ctx, framework)
	if err != nil {
		return fmt.Errorf("adapter verification failed: %w", err)
	}
	checks, ok := coord.VerificationChecks(framework)
	if !ok || len(checks) == 0 {
		return nil
	}

	var failures []string
	for _, check := range checks {
		outcome, err := coord.InvokeVerification(ctx, framework, executable, contracts.VerificationRequest{
			Check:        check.Name,
			ArtifactPath: artifactPath,
		})
		if err != nil {
			return fmt.Errorf("adapter verification failed: check %q: %w", check.Name, err)
		}
		if !outcome.Passed {
			msg := outcome.Name
			if outcome.Details != "" {
				msg += ": " + outcome.Details
			}
			failures = append(failures, msg)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("adapter verification failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

// adapterCoordinator builds a Coordinator wired to invoke the adapter
// executable registered for the given framework. It resolves the
// executable path, registers the adapter's declared capabilities and
// configuration extension through the generic registration helper (so
// namespace isolation and declaration rules are enforced), and returns
// the coordinator together with the resolved executable path.
//
// The registries are fresh per call: adapters are stateless (ADR-009
// §9.8) and the Core keeps no cross-invocation adapter state.
func (c *ServerReleaseCoordinator) adapterCoordinator(ctx context.Context, framework string) (*adapter.Coordinator, string, error) {
	executable, err := c.resolveAdapterExecutable(framework)
	if err != nil {
		return nil, "", err
	}

	runner := c.adapterRunner
	if runner == nil {
		runner = execution.NewRunner()
	}

	capabilities := adapter.NewCapabilityRegistry()
	extensions := adapter.NewConfigExtensionRegistry()
	if err := adapter.RegisterAdapterExecutable(ctx, runner, capabilities, extensions, framework, executable); err != nil {
		return nil, "", fmt.Errorf("register adapter %q: %w", framework, err)
	}

	return adapter.NewCoordinator(runner, capabilities), executable, nil
}

// resolveAdapterExecutable resolves the adapter executable path for the
// given framework. The documented resolution convention is the
// executable name "anvil-adapter-<framework>" looked up on PATH via
// exec.LookPath (005-adapter-command-contract §10); a missing executable
// yields a descriptive error.
func (c *ServerReleaseCoordinator) resolveAdapterExecutable(framework string) (string, error) {
	if c.adapterExecutable != nil {
		return c.adapterExecutable(framework)
	}
	name := "anvil-adapter-" + framework
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("adapter executable %q not found on PATH: %w (install the adapter binary or configure its path)", name, err)
	}
	return path, nil
}

// ---------------------------------------------------------------------------
// Build wiring (TS-007-040)
// ---------------------------------------------------------------------------

// BuildReleaseOptions carries the per-invocation build policy for a
// server release build (TS-007-040). The working directory is not part
// of the options: the build always runs in the project's install root —
// the server-side counterpart of the local project root and the parent
// of every release working directory — so no new server-side directories
// are introduced.
type BuildReleaseOptions struct {
	// Targets restricts the adapter build pipeline to the named
	// targets/phases (TS-007-041). Empty runs all declared phases.
	Targets []string

	// Strict fails the build when a requested target is unsupported on
	// the current platform instead of skipping it with a warning
	// (TS-007-041).
	Strict bool
}

// BuildReleaseResult reports the outcome of a server release build
// (TS-007-040). Success mirrors the adapter's computed build outcome;
// Phases carries every phase the adapter reported, in execution order —
// including skipped phases (Success=true + Skipped=true + Warning) so
// the full pipeline is observable.
type BuildReleaseResult struct {
	// Success reports whether every build phase succeeded.
	Success bool `json:"success"`

	// Phases report each build phase's outcome, in execution order.
	Phases []contracts.BuildPhaseResult `json:"phases"`
}

// BuildRelease invokes the project's framework adapter build pipeline in
// the project install root — the server release build path (TS-007-040,
// ADR-020 §4: the adapter-owned build phases are the single source of
// build knowledge at server release time).
//
// The build sequence:
//  1. Load the project registry to resolve the framework adapter (the
//     canonical project.standard key, with the legacy project.adapter
//     key honored during the deprecation window — ADR-032) and the
//     install root.
//  2. Resolve the adapter executable; a project that selects no adapter
//     or whose adapter executable is missing fails with a descriptive
//     error — there is no silent fallback to a generic build.
//  3. Build the adapter coordinator via adapterCoordinator (reuses the
//     executable resolution and capability registration machinery of the
//     activation/rollback/verification paths).
//  4. Invoke the adapter `build` command with the project install root
//     as working directory; the 15-minute timeout bound is enforced
//     inside InvokeBuild (buildTimeout).
//  5. Map the adapter's BuildResult to BuildReleaseResult; a failing
//     build returns an error naming the first failing phase with its
//     actionable output details (TS-P7-14 AC-7).
//
// The returned report is populated even when the build fails, so the
// caller can surface every phase — including skipped/warned phases — for
// observability.
//
// Reference: TS-007-040, ADR-020 §4, ADR-017
func (c *ServerReleaseCoordinator) BuildRelease(ctx context.Context, projectID string, opts BuildReleaseOptions) (*BuildReleaseResult, error) {
	// Step 1: Load the project registry.
	registryStore := NewRegistryStore(c.serverRoot)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		return nil, fmt.Errorf("load project registry: %w", err)
	}
	framework := reg.Project.StandardName()
	// The legacy project.adapter key is read as an alias during the
	// deprecation window: every read emits a deprecation warning naming
	// project.standard (TS-019-02-02, ADR-032) — stderr, never stdout.
	reg.Project.WarnIfLegacyAdapter(c.warningWriter())

	// Step 2: A project without an adapter cannot run an adapter build —
	// fail with a descriptive error instead of silently building nothing.
	if framework == "" {
		return nil, fmt.Errorf(
			"adapter build failed: project %q selects no framework adapter; set project.standard in the project registry to build through an adapter (the legacy project.adapter key is still honored during the deprecation window, ADR-032)",
			projectID,
		)
	}

	// Step 3: Reuse the adapter coordinator machinery (executable
	// resolution + capability registration).
	coord, executable, err := c.adapterCoordinator(ctx, framework)
	if err != nil {
		return nil, fmt.Errorf("adapter build failed: %w", err)
	}

	// Step 4: Invoke the adapter build pipeline. InvokeBuild enforces the
	// 15-minute build timeout bound (buildTimeout); the working directory
	// is the project install root — the server-side project root hosting
	// every release working directory (same semantics as local builds).
	result, err := coord.InvokeBuild(ctx, framework, executable, contracts.BuildRequest{
		WorkingDir: reg.Project.InstallRoot,
		Targets:    opts.Targets,
		Strict:     opts.Strict,
	})
	if err != nil {
		return nil, fmt.Errorf("adapter build failed: %w", err)
	}

	// Step 5: Map the outcome; a failing build stops here with the first
	// failing phase's actionable details.
	report := &BuildReleaseResult{Success: result.Success, Phases: result.Phases}
	if !result.Success {
		return report, fmt.Errorf("adapter build failed: %s", firstBuildFailure(result.Phases))
	}
	return report, nil
}

// firstBuildFailure returns the actionable failure detail of the first
// failing build phase — the phase name plus its Error, or an excerpt of
// the phase output when the adapter reported no structured error — so a
// failing build reports its output details (TS-P7-14 AC-7).
func firstBuildFailure(phases []contracts.BuildPhaseResult) string {
	for _, phase := range phases {
		if phase.Success {
			continue
		}
		detail := phase.Error
		if detail == "" {
			detail = buildOutputExcerpt(phase.Output)
		}
		return fmt.Sprintf("phase %q: %s", phase.Phase, detail)
	}
	return "no failing phase reported"
}

// buildOutputExcerpt returns a trimmed, bounded excerpt of a build
// phase's output so failure details stay actionable without flooding the
// error message.
func buildOutputExcerpt(output string) string {
	out := strings.TrimSpace(output)
	if out == "" {
		return "no output or error reported for the phase"
	}
	const maxExcerpt = 1000
	if len(out) > maxExcerpt {
		out = out[:maxExcerpt] + "..."
	}
	return out
}
