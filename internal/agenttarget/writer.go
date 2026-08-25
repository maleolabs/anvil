package agenttarget

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"maleolabs.com/anvil/internal/fsutil"
)

// WriterOptions controls how a ResolvedSet is materialized.
type WriterOptions struct {
	// Force allows replacing a native-location path occupied by content
	// that is not ours (a user's same-name skill at the target location):
	// the occupant is removed before the symlink/copy is published
	// (ADR-037 D7 `--force` escape). Without Force, any non-owned
	// occupation is an error — never a silent overwrite.
	Force bool

	// ForceCopy forces the copy fallback for native locations even when
	// symlinks are possible. It exists for the Windows path (no symlink
	// privilege) and for tests; production POSIX installs leave it false.
	ForceCopy bool
}

// installMarkerName is the ownership marker file written into every skill
// directory this package creates (master, copy fallback, lone native). Its
// presence with a matching skill name marks the directory as ours, so
// re-runs and updates are idempotent instead of false conflicts (M-1).
const installMarkerName = ".anvil-install"

// installMarker is the content of the ownership marker. It identifies the
// install so a later run can recognize its own directory regardless of how
// it was materialized (master / symlink / copy).
type installMarker struct {
	// Format is the marker format version (1). Bumped only on breaking
	// marker changes.
	Format int `json:"format"`

	// Skill is the skill name this directory carries.
	Skill string `json:"skill"`
}

// currentMarkerFormat is the marker format version written today.
const currentMarkerFormat = 1

// markerBytes returns the encoded marker for a skill.
func markerBytes(skillName string) []byte {
	data, _ := json.Marshal(installMarker{Format: currentMarkerFormat, Skill: skillName})
	return data
}

// readMarker returns the skill name recorded in a directory's ownership
// marker, and whether a valid marker was found. A marker for a DIFFERENT
// skill still counts as "found" (it is an ownership marker, just not
// ours) — readMarker returns found=true with that skill name so the caller
// can distinguish "no marker" from "someone else's install".
func readMarker(dir string) (skill string, found bool) {
	data, err := os.ReadFile(filepath.Join(dir, installMarkerName))
	if err != nil {
		return "", false
	}
	var m installMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return "", false // corrupt marker → not a valid ownership claim
	}
	if m.Format != currentMarkerFormat || m.Skill == "" {
		return "", false
	}
	return m.Skill, true
}

// markerMatches reports whether dir carries OUR ownership marker for skill.
func markerMatches(dir, skill string) bool {
	got, found := readMarker(dir)
	return found && got == skill
}

// writeTracker tracks every directory and symlink created by one
// WriteMaterializes call, so a failure can roll back exactly what this
// call created (and nothing the user owned before it).
type writeTracker struct {
	dirs  map[string]bool
	links map[string]bool
}

func newWriteTracker() *writeTracker {
	return &writeTracker{dirs: map[string]bool{}, links: map[string]bool{}}
}

// WriteMaterializes writes a resolved set to disk:
//
//  1. Pre-flight: every target is validated BEFORE anything is written
//     (all-or-nothing). Without Force, any target path occupied by content
//     that is not ours blocks the whole install with an aggregated error;
//     with Force, occupied paths are marked for replacement. This keeps
//     the gate atomic: a blocked install writes nothing, and a forced
//     install replaces every occupant in one pass.
//  2. The master copy at `<base>/.agents/skills/<name>/` — every file
//     lands atomically (fsutil.WriteFileAtomic), so a crash mid-install
//     never leaves a truncated skill. An ownership marker is written with
//     the tree.
//  3. Each native target: a symlink to the master (POSIX) or a copy of
//     the master tree (Windows / no privilege / ForceCopy). Copy targets
//     carry the ownership marker too, so re-runs are idempotent.
//
// files maps skill-relative paths (for example "SKILL.md",
// "references/REFERENCE.md") to their content, as produced by the bundle
// extractor (TS-021-01). Paths must stay inside the skill directory —
// the caller's extractor already enforces containment; this writer
// re-checks the rel-path shape before touching the filesystem.
//
// All writes are fail-fast with cleanup: on failure, every directory AND
// symlink created by this call is removed (no dangling symlinks), and the
// pre-existing filesystem is untouched.
func WriteMaterializes(set *ResolvedSet, files map[string][]byte, opts WriterOptions) (err error) {
	if set == nil {
		return fmt.Errorf("write skill targets: no resolved set")
	}
	if len(files) == 0 {
		return fmt.Errorf("write skill targets: no files to write for skill %q", set.SkillName)
	}
	for rel := range files {
		if err := validateSkillRelPath(rel); err != nil {
			return fmt.Errorf("write skill targets: %w", err)
		}
	}

	// Pre-flight: validate every target before writing anything.
	replace, problems := preflightTargets(set, opts)
	if len(problems) > 0 {
		return &WriteBlockedError{Problems: problems}
	}

	tracker := newWriteTracker()

	defer func() {
		if err != nil {
			rollback(tracker)
		}
	}()

	// 1. Master copy.
	if set.Master != "" {
		if err = writeMaster(set.Master, files, set.SkillName, replace[set.Master], tracker); err != nil {
			return err
		}
	}

	// 2. Native targets.
	for _, t := range set.Targets {
		if t.Kind == TargetKindMaster {
			continue
		}
		if err = writeNativeTarget(t, set.Master, set.SkillName, files, opts, replace[t.Path], tracker); err != nil {
			return err
		}
	}

	return nil
}

// preflightTargets validates every target path before any write. It
// returns the set of paths to replace (only populated when Force is set)
// and every blocking problem found.
//
// The occupation rules are the writer-side mirror of CheckConflicts:
//   - our own previous install (native symlink to our master, or an
//     ownership marker for copy/lone/reader installs) is NOT a problem —
//     re-run/update is idempotent;
//   - any other occupant is a problem without Force, and a replace target
//     with Force.
func preflightTargets(set *ResolvedSet, opts WriterOptions) (map[string]bool, []error) {
	replace := map[string]bool{}
	var problems []error

	// Master (only when it exists and is not ours).
	if set.Master != "" && !masterOwned(set) {
		if existing := describeExisting(set.Master); existing != "" {
			if opts.Force {
				replace[set.Master] = true
			} else {
				problems = append(problems, &ConflictError{Path: set.Master, Existing: existing})
			}
		}
	}

	// Native targets.
	for _, t := range set.Targets {
		if t.Kind == TargetKindMaster {
			continue
		}
		occupied, ours := targetOccupation(t, set.Master, set.SkillName)
		if !occupied || ours {
			continue
		}
		if opts.Force {
			replace[t.Path] = true
		} else {
			problems = append(problems, &ConflictError{Path: t.Path, Existing: describeExisting(t.Path)})
		}
	}
	return replace, problems
}

// targetOccupation reports whether a native target path is occupied, and
// whether the occupant is ours. A symlink to our master is ours; a
// directory carrying our ownership marker (copy fallback, lone native) is
// ours; everything else is not.
func targetOccupation(t Target, master, skillName string) (occupied, ours bool) {
	info, err := os.Lstat(t.Path)
	if err != nil {
		return false, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if master != "" {
			if target, rerr := os.Readlink(t.Path); rerr == nil && filepath.Clean(target) == filepath.Clean(master) {
				return true, true
			}
		}
		return true, false
	}
	if info.IsDir() && markerMatches(t.Path, skillName) {
		return true, true
	}
	return true, false
}

// masterOwned reports whether the master directory carries our ownership
// marker for this skill.
func masterOwned(set *ResolvedSet) bool {
	return markerMatches(set.Master, set.SkillName)
}

// writeMaster writes the master copy tree: the skill directory, every
// file, and the ownership marker — atomically, with mode 0644 (the
// extraction security rule from TS-021-01 — skills never carry exec bits).
// When replace is true, an existing non-ours occupant at master is removed
// first (Force).
func writeMaster(master string, files map[string][]byte, skillName string, replace bool, tracker *writeTracker) error {
	if replace {
		if err := os.RemoveAll(master); err != nil {
			return fmt.Errorf("write master copy: --force: remove existing %s: %w", master, err)
		}
	}
	if err := mkdirTracked(master, tracker); err != nil {
		return fmt.Errorf("write master copy: %w", err)
	}
	for rel := range files {
		path := filepath.Join(master, rel)
		if err := mkdirTracked(filepath.Dir(path), tracker); err != nil {
			return fmt.Errorf("write master copy: %w", err)
		}
		if err := fsutil.WriteFileAtomic(path, files[rel], 0o644); err != nil {
			return fmt.Errorf("write master copy: %s: %w", rel, err)
		}
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(master, installMarkerName), markerBytes(skillName), 0o644); err != nil {
		return fmt.Errorf("write master copy: ownership marker: %w", err)
	}
	return nil
}

// writeNativeTarget materializes one native target: a symlink to the
// master (POSIX default) or a copy of the master tree (Windows fallback /
// ForceCopy). replace=true (Force) removes a non-ours occupant first.
//
// A lone native-only install (--agent claude-code / --agent cursor) has no
// master at all: the target receives the skill files directly as a real
// copy (with the ownership marker), so other agents never see the content
// in `.agents/skills` (ADR-037 D6 "so other agents never see it").
func writeNativeTarget(t Target, master, skillName string, files map[string][]byte, opts WriterOptions, replace bool, tracker *writeTracker) error {
	if master == "" && t.Kind == TargetKindSymlink {
		return fmt.Errorf("write target for agent %s: native target %s has no master to link to", t.Agent, t.Path)
	}

	switch {
	case master == "" && t.Kind == TargetKindCopy:
		// Lone native-only install: write the files directly at the
		// native location (no master exists). The occupant removal for
		// Force happened in the pre-flight replace pass; writeMaster with
		// replace=false never removes again.
		if replace {
			if err := os.RemoveAll(t.Path); err != nil {
				return fmt.Errorf("write target for agent %s: --force: remove existing %s: %w", t.Agent, t.Path, err)
			}
		}
		return writeMaster(t.Path, files, skillName, false, tracker)
	case t.Kind == TargetKindCopy || opts.ForceCopy || preferWindowsCopy():
		return copyMasterTo(t.Path, master, replace, tracker)
	default:
		return symlinkMasterTo(t.Path, master, replace, tracker)
	}
}

// symlinkMasterTo creates `<native>/<name>` as a symlink to the master
// directory, atomically: a temp symlink in the same directory is renamed
// over the final path (the runtime SymlinkSwitcher pattern, TS-P5-08).
// The parent directory is created first.
//
// No-overwrite contract: the final path must not exist, or be a symlink
// of ours pointing at the same master (idempotent re-run). With Force, a
// non-ours occupant is removed first; without Force it is an error. The
// conflict gate normally catches this earlier; the check here makes the
// writer safe even when called directly.
//
// If the symlink cannot be created (no privilege, unsupported filesystem),
// the caller's intent is preserved via the copy fallback: this function
// returns the underlying error and writeNativeTarget does NOT retry — the
// fallback decision belongs to the writer options, not the filesystem.
// Production callers on POSIX keep the default; Windows always copies.
func symlinkMasterTo(path, master string, replace bool, tracker *writeTracker) error {
	if err := mkdirTracked(filepath.Dir(path), tracker); err != nil {
		return fmt.Errorf("symlink %s: %w", path, err)
	}

	// The final path must not exist, or be our own symlink to master.
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if target, rerr := os.Readlink(path); rerr == nil && filepath.Clean(target) == filepath.Clean(master) {
				return nil // idempotent re-run
			}
		}
		if !replace {
			return fmt.Errorf("symlink %s: refusing to overwrite existing %s — remove it or run with --force", path, describeExisting(path))
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("symlink %s: --force: remove existing: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("symlink %s: stat: %w", path, err)
	}

	tmp := path + ".tmp-link"
	_ = os.Remove(tmp) // stale temp from a crashed previous attempt

	if err := os.Symlink(master, tmp); err != nil {
		return fmt.Errorf("create symlink %s -> %s: %w", tmp, master, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publish symlink %s: %w", path, err)
	}
	tracker.links[path] = true
	return nil
}

// copyMasterTo copies the master tree into the native location: the
// destination skill directory receives a full copy of the master's files
// (including the ownership marker) with the same atomic per-file write
// (crash-safe) and mode 0644. replace=true (Force) removes a non-ours
// occupant first.
func copyMasterTo(path, master string, replace bool, tracker *writeTracker) error {
	if replace {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("copy %s: --force: remove existing: %w", path, err)
		}
	}
	if err := mkdirTracked(filepath.Dir(path), tracker); err != nil {
		return fmt.Errorf("copy %s: %w", path, err)
	}
	if err := mkdirTracked(path, tracker); err != nil {
		return fmt.Errorf("copy %s: %w", path, err)
	}

	entries, err := os.ReadDir(master)
	if err != nil {
		return fmt.Errorf("copy %s: read master %s: %w", path, master, err)
	}
	for _, e := range entries {
		src := filepath.Join(master, e.Name())
		dst := filepath.Join(path, e.Name())
		if e.IsDir() {
			if err := copyTree(src, dst, tracker); err != nil {
				return fmt.Errorf("copy %s: %w", path, err)
			}
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			// Skills extracted by TS-021-01 never contain symlinks, but a
			// defensive copy must not follow one from the master either.
			return fmt.Errorf("copy %s: master contains a symlink %s — refusing to copy", path, src)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("copy %s: read %s: %w", path, src, err)
		}
		if err := fsutil.WriteFileAtomic(dst, data, 0o644); err != nil {
			return fmt.Errorf("copy %s: write %s: %w", path, dst, err)
		}
	}
	return nil
}

// copyTree recursively copies a directory subtree (mode 0644 files, 0755
// dirs), tracking created directories for rollback.
func copyTree(src, dst string, tracker *writeTracker) error {
	if err := mkdirTracked(dst, tracker); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		childSrc := filepath.Join(src, e.Name())
		childDst := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyTree(childSrc, childDst, tracker); err != nil {
				return err
			}
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to copy symlink %s", childSrc)
		}
		data, err := os.ReadFile(childSrc)
		if err != nil {
			return err
		}
		if err := fsutil.WriteFileAtomic(childDst, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// mkdirTracked creates a directory (0755) and records it so a failed
// install can roll it back. Pre-existing directories are not recorded.
//
// Symlink safety: every existing component along the path is checked with
// Lstat and refused when it is a symlink or reparse point — os.MkdirAll
// would silently follow an intermediate symlink (e.g. `.agents` → /etc)
// and redirect the install elsewhere. This is the same rule the bundle
// extractor enforces for extraction roots (TS-021-01 symlink-escape fix).
func mkdirTracked(dir string, tracker *writeTracker) error {
	if err := checkNoSymlinkPath(dir); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err == nil {
		if isSymlinkOrReparse(info, dir) {
			return fmt.Errorf("create directory %s: refusing to follow a symlink or reparse point at this path", dir)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	tracker.dirs[dir] = true
	return nil
}

// checkNoSymlinkPath walks every existing component of dir from the
// filesystem root downward and refuses the path when any component is a
// symlink or reparse point. Only components that exist are inspected (the
// ones MkdirAll will create cannot be symlinks yet). The final component
// is not inspected here — mkdirTracked handles it after the walk.
func checkNoSymlinkPath(dir string) error {
	vol := filepath.VolumeName(dir)
	current := filepath.FromSlash(vol + string(filepath.Separator))
	rest, err := filepath.Rel(current, dir)
	if err != nil {
		return fmt.Errorf("create directory %s: cannot inspect path: %w", dir, err)
	}
	for _, comp := range strings.Split(rest, string(filepath.Separator)) {
		if comp == "" || comp == "." {
			continue
		}
		current = filepath.Join(current, comp)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil // remaining components will be created fresh
			}
			return fmt.Errorf("stat %s: %w", current, err)
		}
		if isSymlinkOrReparse(info, current) {
			return fmt.Errorf("create directory %s: refusing to follow symlink or reparse point at %s", dir, current)
		}
	}
	return nil
}

// isSymlinkOrReparse reports whether an existing path is a symlink (Lstat
// ModeSymlink) or a Windows reparse point such as a junction (M2).
//
// Windows junctions report as plain directories to os.Lstat, so the Lstat
// check alone is insufficient. The portable fallback compares the
// EvalSymlinks-resolved path with the original: a junction or symlink
// resolves to a different path, a real directory does not. On Windows the
// FILE_ATTRIBUTE_REPARSE_POINT attribute is read first (via the syscall
// layer through gosysinfo if available); the EvalSymlinks comparison is
// the platform-independent backstop and is what the tests exercise.
func isSymlinkOrReparse(info os.FileInfo, path string) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	// Windows reparse point (junction) — os.Lstat reports a directory.
	if isWindowsReparsePoint(info) {
		return true
	}
	// Portable backstop: a resolved path different from the original means
	// the path traverses a link (symlink or junction).
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		// Unresolvable (e.g. dangling) — treat as not-a-real-dir and let
		// the caller's stat/MkdirAll surface the concrete error.
		return false
	}
	return filepath.Clean(resolved) != filepath.Clean(path)
}

// rollback removes every directory and symlink created by a failed call:
// symlinks first (they may live inside directories being removed), then
// created directories deepest-first. User-owned content is never touched.
func rollback(tracker *writeTracker) {
	for link := range tracker.links {
		_ = os.Remove(link)
	}
	dirs := make([]string, 0, len(tracker.dirs))
	for d := range tracker.dirs {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
}

// validateSkillRelPath enforces the rel-path shape for skill files: no
// absolute path, no traversal, no backslash, and no empty components —
// the same character-level rules the bundle extractor enforces (TS-021-01),
// re-checked here because this writer touches the filesystem.
func validateSkillRelPath(rel string) error {
	if rel == "" {
		return fmt.Errorf("empty skill file path")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return fmt.Errorf("skill file path %q is absolute", rel)
	}
	if strings.Contains(rel, "\\") {
		return fmt.Errorf("skill file path %q contains a backslash", rel)
	}
	for _, comp := range strings.Split(rel, "/") {
		if comp == "" || comp == "." || comp == ".." {
			return fmt.Errorf("skill file path %q contains an invalid component", rel)
		}
	}
	return nil
}

// ReadAllTargets returns the distinct absolute paths that a successful
// materialization leaves on disk (used by uninstall). Symlink and copy
// targets point at the same content but are separate filesystem entries.
func ReadAllTargets(set *ResolvedSet) []string {
	seen := map[string]bool{}
	var out []string
	if set.Master != "" {
		out = append(out, set.Master)
		seen[set.Master] = true
	}
	for _, t := range set.Targets {
		if t.Kind == TargetKindMaster {
			continue
		}
		if !seen[t.Path] {
			out = append(out, t.Path)
			seen[t.Path] = true
		}
	}
	return out
}

// WriteBlockedError aggregates every occupation problem found during the
// writer pre-flight. It mirrors InstallBlockedError for callers that use
// the writer directly.
type WriteBlockedError struct {
	Problems []error
}

func (e *WriteBlockedError) Error() string {
	if len(e.Problems) == 0 {
		return "skill write blocked"
	}
	out := fmt.Sprintf("skill write blocked by %d problem(s):", len(e.Problems))
	for _, p := range e.Problems {
		out += "\n  - " + p.Error()
	}
	out += "\nRun with --force to override."
	return out
}
