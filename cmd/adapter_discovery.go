// Package cmd implements the Anvil CLI commands.
//
// ── PATH-Based Adapter Discovery (TS-007-039) ────────────────────────
//
// Adapter discovery migrates from the closed set engine.KnownFrameworks()
// to scanning the system for "anvil-adapter-<name>" executables
// (ADR-020 §2, 005-adapter-command-contract §10): any adapter binary
// installed next to the CLI or on PATH is visible and usable without a
// Core release (ADR-009 §11.1).
//
// Scanning is defensive (TS-007-039 §3): unreadable PATH directories are
// skipped, duplicate PATH entries are deduplicated, relative entries
// resolve against the working directory, and non-executable files with
// the prefix are ignored. A scanned executable is only treated as a
// valid adapter after it answers the capabilities command (TS-007-039
// §7) — foreign or broken "anvil-adapter-*" binaries are filtered out by
// the probe. The engine.KnownFrameworks() list is retained as a
// display-only fallback for the "known adapters" hint during the
// transition period (ADR-020 §2, AC-5).
//
// Reference: TS-007-039, ADR-020 §2, ADR-009 §11.1,
// 005-adapter-command-contract §10
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// scanPathAdapters walks every directory on PATH and returns the adapter
// binaries installed there: name → executable path for every file named
// exactly "anvil-adapter-<name>" (no platform suffix). It is the PATH
// portion of the system scan — the CLI install directory is layered on
// top of it by installedAdaptersFromSystem.
//
// The scan is defensive by design (TS-007-039 §3):
//
//   - an unreadable or missing directory is skipped, never an error;
//   - duplicate PATH entries resolve to one binary (first wins);
//   - relative PATH entries are resolved against the working directory
//     so the returned executable paths are always absolute;
//   - non-executable files with the prefix are skipped — a binary that
//     cannot be executed is not a usable adapter.
//
// The per-directory matching reuses listInstalledAdapters
// (cmd/adapter_binary.go) — the release-asset platform-suffix filtering
// and identifier validation are shared, not duplicated. The error result
// is reserved for scanning-level failures; today every defensive
// condition is skipped per directory, so it is always nil.
//
// Reference: TS-007-039 §3, 005-adapter-command-contract §10
func scanPathAdapters() (map[string]string, error) {
	found := make(map[string]string)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		names, err := listInstalledAdapters(dir)
		if err != nil {
			continue // unreadable or missing directory — nothing to detect there
		}
		for _, name := range names {
			if _, ok := found[name]; ok {
				continue
			}
			executable := filepath.Join(dir, adapterBinaryName(name))
			if !filepath.IsAbs(executable) {
				abs, err := filepath.Abs(executable)
				if err != nil {
					continue // cannot resolve the relative entry — skip gracefully
				}
				executable = abs
			}
			if !isExecutableAdapterBinary(executable) {
				continue
			}
			found[name] = executable
		}
	}
	return found, nil
}

// isExecutableAdapterBinary reports whether path is a regular file with
// the executable bit set. Symlinks to executables qualify (os.Stat
// follows links). Files that merely carry the "anvil-adapter-" prefix —
// fixtures, documentation, a foreign binary without exec permissions —
// are not usable adapters and are skipped by the scan.
func isExecutableAdapterBinary(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}

// resolveAdapterSet returns the probe-validated adapter set: every
// detected binary (CLI install directory + PATH) whose capabilities
// command succeeds, as name → executable path. This is the validation
// gate of PATH-based discovery (TS-007-039 §7): a binary is a valid
// adapter only when it answers the capabilities command, so foreign
// "anvil-adapter-*" executables that happen to carry the prefix but fail
// the probe are excluded — the set never surfaces non-Anvil binaries
// (AC-4). A failing probe excludes the candidate without failing the
// set, so the error result is always nil; it is reserved for discovery
// failures.
//
// Reference: TS-007-039 §7, AC-4
func resolveAdapterSet(ctx context.Context) (map[string]string, error) {
	valid := make(map[string]string)
	for name, executable := range installedAdaptersFromSystem() {
		if _, err := invokeAdapterCapabilities(ctx, name, executable); err != nil {
			continue // probe failed — not a valid Anvil adapter
		}
		valid[name] = executable
	}
	return valid, nil
}

// discoveredAdapterHint builds the "reason" text for unknown-adapter
// errors: the sorted names of the probe-validated adapter set when
// discovery found anything, or the engine.KnownFrameworks() display list
// ("known adapters: ...") when nothing is installed — the display-only
// fallback of the transition period (ADR-020 §2, TS-007-039 AC-5).
func discoveredAdapterHint(adapters map[string]string) string {
	if len(adapters) == 0 {
		return fmt.Sprintf("known adapters: %s", strings.Join(adapterKnownFrameworks(), ", "))
	}
	names := make([]string, 0, len(adapters))
	for name := range adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Sprintf("available adapters: %s", strings.Join(names, ", "))
}
