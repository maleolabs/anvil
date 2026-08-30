package spklocaldeployrace

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/server"
)

// StatusSnapshot captures observable state (state-before-assumptions) via authoritative read paths.
type StatusSnapshot struct {
	AfterAction   string                  `json:"after_action"` // e.g. "activate1", "activate2", "rollback"
	Lifecycle     *server.LifecycleStatus `json:"lifecycle"`
	ActiveRelease *release.Release        `json:"active_release,omitempty"`
	InstalledList []*release.Release      `json:"installed_list"`
}

// QueryStatus builds a StatusSnapshot using server.QueryLifecycleStatus + release queries (read-only).
func QueryStatus(serverRoot, projectID, installRoot, afterAction string) (*StatusSnapshot, error) {
	lc, err := server.QueryLifecycleStatus(serverRoot, projectID)
	if err != nil {
		return nil, fmt.Errorf("query lifecycle status: %w", err)
	}
	active, err := release.GetActiveRelease(installRoot)
	if err != nil {
		return nil, fmt.Errorf("get active release: %w", err)
	}
	installed, err := release.ListReleases(installRoot)
	if err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}
	return &StatusSnapshot{
		AfterAction:   afterAction,
		Lifecycle:     lc,
		ActiveRelease: active,
		InstalledList: installed,
	}, nil
}

// WriteStatusLog writes status JSON to w and optionally to file.
func WriteStatusLog(w io.Writer, snap *StatusSnapshot) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, "=== Status after %s ===\n", snap.AfterAction)
	if snap.Lifecycle != nil {
		fmt.Fprintf(w, "Project: %s\n", snap.Lifecycle.ProjectID)
		if snap.Lifecycle.Active != nil {
			fmt.Fprintf(w, "Active: %s (version=%s stage=%s)\n", snap.Lifecycle.Active.ReleaseID, snap.Lifecycle.Active.Version, snap.Lifecycle.Active.Stage)
		} else {
			fmt.Fprintf(w, "Active: <none>\n")
		}
		fmt.Fprintf(w, "Installed: %d\n", len(snap.Lifecycle.Installed))
		for _, r := range snap.Lifecycle.Installed {
			fmt.Fprintf(w, "  - %s version=%s stage=%s\n", r.ReleaseID, r.Version, r.Stage)
		}
		fmt.Fprintf(w, "Rollback eligible=%v", snap.Lifecycle.Rollback.Eligible)
		if snap.Lifecycle.Rollback.Reason != "" {
			fmt.Fprintf(w, " reason=%s", snap.Lifecycle.Rollback.Reason)
		}
		if snap.Lifecycle.Rollback.TargetReleaseID != "" {
			fmt.Fprintf(w, " target=%s active=%s", snap.Lifecycle.Rollback.TargetReleaseID, snap.Lifecycle.Rollback.ActiveReleaseID)
		}
		fmt.Fprintf(w, "\n")
		fmt.Fprintf(w, "RuntimeState recorded=%v active=%s condition=%s shared=%s\n", snap.Lifecycle.RuntimeState.Recorded, snap.Lifecycle.RuntimeState.ActiveReleaseID, snap.Lifecycle.RuntimeState.RuntimeCondition, snap.Lifecycle.RuntimeState.SharedResource)
	}
	if snap.ActiveRelease != nil {
		fmt.Fprintf(w, "Direct GetActiveRelease: %s stage=%s version=%s\n", snap.ActiveRelease.ID.String(), snap.ActiveRelease.Stage.String(), snap.ActiveRelease.Version)
	} else {
		fmt.Fprintf(w, "Direct GetActiveRelease: <none>\n")
	}
	fmt.Fprintf(w, "ListReleases total=%d\n", len(snap.InstalledList))
	for _, r := range snap.InstalledList {
		fmt.Fprintf(w, "  * %s stage=%s artifact=%s\n", r.ID.String(), r.Stage.String(), r.ArtifactID)
	}
	fmt.Fprintf(w, "\n")
}

// SaveStatusJSON saves snapshot as JSON file for evidence.
func SaveStatusJSON(dir, afterAction string, snap *StatusSnapshot) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, fmt.Sprintf("status_%s.json", afterAction))
	return os.WriteFile(path, data, 0644)
}

// AssertOnlyOneActive verifies only-one-active invariant (ADR-003).
func AssertOnlyOneActive(installRoot string) error {
	active, err := release.ListReleasesByStage(installRoot, release.StageActive)
	if err != nil {
		return fmt.Errorf("list active releases: %w", err)
	}
	if len(active) > 1 {
		return fmt.Errorf("invariant violated: only-one-active: found %d Active releases", len(active))
	}
	return nil
}
