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
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"maleolabs.com/anvil/internal/adapter"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
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
}

// NewServerReleaseCoordinator creates a coordinator that uses the given
// server root to resolve project registries and coordinate release operations.
//
// The serverRoot must point to a directory containing the projects registry
// subdirectory (projects/). Use RootPath() or resolveServerRoot() to resolve
// the effective path including ANVIL_SERVER_ROOT overrides.
func NewServerReleaseCoordinator(serverRoot string) *ServerReleaseCoordinator {
	return &ServerReleaseCoordinator{serverRoot: serverRoot}
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
//  9. Create the release directory (releases/rel-<identity>).
//  10. Create the Release struct with Ready stage.
//  11. Persist the Release JSON to disk (state/releases/<id>.json).
//  12. Initialize the Runtime State store.
//
// Returns the created Release or an error describing the failure.
//
// Reference: ST-P4-13
func (c *ServerReleaseCoordinator) Install(projectID, artifactPath string) (*release.Release, error) {
	// Step 1: Load project registry to resolve the install root.
	registryStore := NewRegistryStore(c.serverRoot)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		return nil, fmt.Errorf("load project registry: %w", err)
	}
	installRoot := reg.Project.InstallRoot
	installOwner := reg.Project.Owner
	installGroup := reg.Project.Group

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
	releasesStateDir := filepath.Join(installRoot, "state", "releases")
	if existingRelease := findReleaseByArtifactID(releasesStateDir, manifest.ArtifactID); existingRelease != nil {
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
		releasesStateDir,
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

	// Step 13: Initialize Runtime State store.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	if err := stateStore.Save(); err != nil {
		return nil, fmt.Errorf("save runtime state: %w", err)
	}

	// Step 14: Apply owner/group from project registry to all installed files.
	// This ensures the release directory, artifact store, and shared resources
	// have the correct ownership for the runtime user (e.g., www-data).
	if installOwner != "" || installGroup != "" {
		if err := ApplyFileOwnership(installRoot, installOwner, installGroup); err != nil {
			return nil, fmt.Errorf("apply file ownership: %w", err)
		}
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
//  5. Apply owner/group from project registry to extracted files.
//  6. Apply shared links — symlink shared resources into release dir.
//  7. Create activation dependencies (SharedResourceManager, PromoteRunner).
//  8. Execute the activation phase sequence via ActivationEngine.
//  9. Persist the updated Release (stage transitions).
//  8. Update RuntimeState with the active release ID.
//
// Returns nil on success or an error describing the failure.
//
// Reference: ST-P4-14
func (c *ServerReleaseCoordinator) Activate(projectID, releaseID string) error {
	// Step 1: Load project registry.
	registryStore := NewRegistryStore(c.serverRoot)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		return fmt.Errorf("load project registry: %w", err)
	}
	installRoot := reg.Project.InstallRoot

	// Step 2: Build RuntimeConfig.
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot

	// Step 3: Load Release from runtime state directory (without .anvil/ prefix).
	releasePath := filepath.Join(installRoot, "state", "releases", releaseID+".json")
	rel, err := release.Load(releasePath)
	if err != nil {
		return fmt.Errorf("load release: %w", err)
	}

	// Step 4: Extract the stored artifact into the release directory.
	// This must happen before the activation engine runs so that the release
	// directory contains deployable files when the symlink is switched.
	releaseDir := runtime.ReleaseDirPath(runtimeCfg.ReleasesDirPath(), releaseID)
	if err := artifact.ExtractArtifact(rel.ArtifactPath, releaseDir); err != nil {
		return fmt.Errorf("extract artifact for activation: %w", err)
	}

	// Step 5: Apply owner/group from project registry to extracted release files.
	if reg.Project.Owner != "" || reg.Project.Group != "" {
		if err := ApplyFileOwnership(releaseDir, reg.Project.Owner, reg.Project.Group); err != nil {
			return fmt.Errorf("apply release directory ownership: %w", err)
		}
	}

	// Step 6: Apply shared links — symlink shared resources into the release
	// directory (e.g., shared/config/.env → .env, shared/storage → storage).
	// This must happen before the activation engine runs so that the release
	// directory is complete before the symlink switch.
	if len(reg.Project.SharedLinks) > 0 {
		if err := applySharedLinks(installRoot, releaseDir, reg.Project.SharedLinks); err != nil {
			return fmt.Errorf("apply shared links: %w", err)
		}
	}

	// Step 7: Create activation dependencies.
	sharedMgr := runtime.NewSharedResourceManager(runtimeCfg)
	switcher := runtime.NewSymlinkSwitcher(runtimeCfg)
	promoteRunner := release.NewPromoteRunner(switcher, runtimeCfg.ReleasesDirPath())
	engine := release.NewActivationEngine(sharedMgr, promoteRunner, nil)

	// Step 8: Execute activation phase sequence.
	if err := engine.Activate(rel); err != nil {
		// Save the Release with any recorded transitions (best-effort).
		_ = rel.Save(releasePath)
		return fmt.Errorf("activation failed: %w", err)
	}

	// Step 8.5: Invoke the adapter's declared activation phases
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

	// Step 9: Save updated Release state.
	if err := rel.Save(releasePath); err != nil {
		return fmt.Errorf("save release state: %w", err)
	}

	// Step 10: Update RuntimeState with active release.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
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

	// Step 5: Update RuntimeState with the restored release as active.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
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

// ProvisionProjectDir creates the project directory structure under the given
// installRoot and applies the specified owner:group to all created directories.
//
// The created structure includes:
//   - <installRoot>/artifacts/
//   - <installRoot>/releases/
//   - <installRoot>/state/releases/
//   - <installRoot>/shared/config/
//   - <installRoot>/shared/storage/
//   - <installRoot>/shared/logs/
//   - <installRoot>/tmp/
//
// If owner or group is empty, ownership is not changed (no-op for ownership).
// When chown fails due to insufficient privileges, the operation continues
// with a warning — this allows non-root users to provision projects without
// ownership changes.
//
// Returns an error if directory creation fails, user/group lookup fails, or
// chown fails for reasons other than permission denied.
func ProvisionProjectDir(installRoot, owner, group string) error {
	// Create the install root directory first.
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		return fmt.Errorf("create install root %s: %w", installRoot, err)
	}

	// Use default runtime directory names (releases, shared, etc.).
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot

	dirs := []string{
		filepath.Join(installRoot, "artifacts"),
		filepath.Join(installRoot, "state", "releases"),
		runtimeCfg.ReleasesDirPath(),
		runtimeCfg.SharedConfigDirPath(),
		runtimeCfg.SharedStorageDirPath(),
		runtimeCfg.LogsDirPath(),
		runtimeCfg.TempDirPath(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", d, err)
		}
	}

	// Apply ownership if specified.
	if owner != "" || group != "" {
		if err := ApplyFileOwnership(installRoot, owner, group); err != nil {
			return fmt.Errorf("set project directory ownership: %w", err)
		}
	}

	return nil
}

// ApplyFileOwnership recursively changes the ownership of all files and
// directories under rootPath to the specified owner and group.
//
// Both owner and group are system user/group names (e.g., "www-data").
// When chown fails with permission denied (not running as root), the
// operation logs a warning and continues — this allows non-root users
// to install releases without ownership changes.
//
// Returns an error if the user or group name cannot be resolved, or if
// chown fails for reasons other than insufficient privileges.
// Returns nil if both owner and group are empty (no-op).
func ApplyFileOwnership(rootPath, owner, group string) error {
	if owner == "" && group == "" {
		return nil
	}

	var uid, gid int

	if owner != "" {
		u, err := user.Lookup(owner)
		if err != nil {
			return fmt.Errorf("lookup user %q: %w", owner, err)
		}
		uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			return fmt.Errorf("parse uid %q: %w", u.Uid, err)
		}
	}
	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			return fmt.Errorf("lookup group %q: %w", group, err)
		}
		gid, err = strconv.Atoi(g.Gid)
		if err != nil {
			return fmt.Errorf("parse gid %q: %w", g.Gid, err)
		}
	}

	return filepath.Walk(rootPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err := os.Lchown(path, uid, gid); err != nil {
			if os.IsPermission(err) {
				// Not running as root — warn and continue.
				// This allows non-root users to install releases without
				// ownership changes, which is the common development case.
				fmt.Fprintf(os.Stderr, "Warning: could not chown %s (requires root): %v\n", path, err)
				return nil
			}
			return fmt.Errorf("change ownership of %s: %w", path, err)
		}
		return nil
	})
}

// applySharedLinks creates symlinks from shared resources into the release
// directory based on the project's SharedLinks configuration.
//
// For each link config:
//  1. Resolve the source path (from) relative to installRoot
//  2. Resolve the target path (to) relative to releaseDir
//  3. If target exists and is NOT a symlink: remove it (artifact content)
//  4. If target exists and IS a symlink: skip (already linked)
//  5. Create relative symlink from target to source
//
// This ensures shared resources (config, storage) are available inside the
// release directory without duplicating their content across releases.
//
// Reference: EPIC-005 §11.5, §9.2
func applySharedLinks(installRoot, releaseDir string, links []SharedLink) error {
	for _, link := range links {
		if link.From == "" || link.To == "" {
			continue
		}

		sourcePath := filepath.Join(installRoot, link.From)
		targetPath := filepath.Join(releaseDir, link.To)

		// Check if target already exists.
		if fi, err := os.Lstat(targetPath); err == nil {
			// If it's already a symlink, skip (idempotent).
			if fi.Mode()&os.ModeSymlink != 0 {
				continue
			}
			// It exists as a regular file or directory from the artifact.
			// Remove it to replace with a symlink to shared resource.
			if err := os.RemoveAll(targetPath); err != nil {
				return fmt.Errorf("remove %s for shared link: %w", targetPath, err)
			}
		}

		// Ensure parent directory of target exists inside release dir.
		targetParent := filepath.Dir(targetPath)
		if err := os.MkdirAll(targetParent, 0755); err != nil {
			return fmt.Errorf("create parent directory for shared link %s: %w", targetPath, err)
		}

		// Calculate relative path from target to source for a portable symlink.
		relPath, err := filepath.Rel(targetParent, sourcePath)
		if err != nil {
			return fmt.Errorf("calculate relative path for shared link %s -> %s: %w", targetPath, sourcePath, err)
		}

		if err := os.Symlink(relPath, targetPath); err != nil {
			return fmt.Errorf("create shared link %s -> %s: %w", targetPath, relPath, err)
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
// project registry, the phases from the adapter's capability declaration,
// and the executable from the resolver convention. No framework-specific
// value exists in the Core (ADR-009 §8.1, §9.6).
func (c *ServerReleaseCoordinator) runAdapterActivation(ctx context.Context, reg *ProjectRegistry, releaseID, releaseDir string) error {
	framework := reg.Project.Adapter
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
	framework := reg.Project.Adapter
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
	framework := reg.Project.Adapter
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
