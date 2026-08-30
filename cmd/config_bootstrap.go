package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/registry"
)

// ensureDefaultConfigLayout bootstraps the first-run configuration
// layout so a fresh install is not blocked by "file not found" errors:
// it creates the global config directory, the default registry index
// directory (ADR-030 — a decentralized, static index with no bundled or
// canonical hosted directory) and the trust anchors allowlist file
// (D-07 — an explicit empty allowlist means no anchors are configured,
// anchors.go). Creating them up front turns first-run errors into
// actionable "not configured yet" states and makes the intended layout
// visible to the operator.
//
// The operation is best-effort: when the layout cannot be created
// (read-only home, permission errors) the individual commands surface
// their own errors with the same guidance.
func ensureDefaultConfigLayout() error {
	dir, err := config.GlobalConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "registry"), 0o755); err != nil {
		return fmt.Errorf("create default registry index directory: %w", err)
	}
	anchors := filepath.Join(dir, registry.DefaultTrustAnchorsFileName)
	if _, err := os.Stat(anchors); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect trust anchors file: %w", err)
	}
	if err := os.WriteFile(anchors, []byte("{\n  \"publishers\": {}\n}\n"), 0o644); err != nil {
		return fmt.Errorf("create trust anchors file: %w", err)
	}
	return nil
}

// registryIndexConfigured reports whether the registry index directory
// exists and holds at least one .json document — the distinction
// between "not set up yet" (fresh install: keep the setup hint) and
// "configured" (documents present, even when none resolve). The index
// layout is <index>/<standard-id>/<version>.json, so documents live in
// subdirectories; the scan walks the whole tree (mirroring LoadIndex).
func registryIndexConfigured(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: keep scanning the rest
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".json") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// loadStandardIndex loads the registry index for the standard and skill
// commands. When the DEFAULT index directory (no --index flag, no
// ANVIL_REGISTRY_INDEX) exists but holds no documents — the state left
// by the first-run bootstrap — it is reported as ErrIndexNotFound so
// first-run guidance (setup hint, exit 3) stays accurate. Explicit
// --index / environment directories keep their existing semantics: an
// explicitly configured empty directory is a resolved-but-empty index.
func loadStandardIndex(cmd *cobra.Command) (*registry.Index, error) {
	flagSet := FlagIsSet(cmd, "index")
	flagValue, _ := cmd.Flags().GetString("index")
	indexPath, source, err := resolveStandardIndex(flagValue, flagSet, os.Getenv)
	if err != nil {
		return nil, err
	}
	if source == standardIndexDefault && !registryIndexConfigured(indexPath) {
		return nil, fmt.Errorf("%w (or empty): %s", registry.ErrIndexNotFound, indexPath)
	}
	return registry.LoadIndex(indexPath)
}

// loadTrustAnchorsConfigured loads the trust anchors allowlist for the
// pre-fetch gate, treating an explicitly empty allowlist — the state
// left by the first-run bootstrap — the same as a missing file:
// verification would fail after the fetch, but the gate exists to avoid
// fetching at all when no publisher is trusted (ADR-022 fail-fast
// posture, no first-use acceptance).
func loadTrustAnchorsConfigured(path string) (*registry.TrustAnchors, error) {
	anchors, err := registry.LoadTrustAnchors(path)
	if err != nil {
		return nil, err
	}
	if anchors.Len() == 0 {
		return nil, fmt.Errorf("%w: %s", registry.ErrTrustAnchorsNotFound, path)
	}
	return anchors, nil
}
