package skillbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Rooted, security-hardened bundle extraction (TS-021-01; ADR-037 D4;
// skill-bundle-format.md §6).
//
// Extract is the only content-materialization entry point for skill
// bundles: it reads a validated archive (manifest.json first, then the
// content tree declared by the manifest's files[]) and writes the content
// tree under a caller-supplied root directory, enforcing the security
// rules of ADR-037 D4:
//
//   - rooted extraction: every entry path must be a safe relative path
//     (validateEntryPath) and every written path must resolve inside the
//     resolved extraction root (pathWithinRoot) — ".." components,
//     absolute paths, backslash separators, and drive-letter prefixes are
//     rejected before any filesystem operation;
//   - no symlink escape: symlink, hardlink, and device entries are
//     rejected outright, and the extraction root is resolved with
//     EvalSymlinks before any write, so no entry can redirect a write
//     outside the root;
//   - mode 0644: content files are written with the executable bit
//     stripped (0644), directories 0755;
//   - caps enforced during extraction: per-asset 10 MiB, total-content
//     64 MiB, 512 files, path length 256, path depth 16 — each checked
//     before and during the copy, so a hostile archive cannot exhaust
//     memory or disk beyond the caps;
//   - inventory exactness: the archive must carry exactly the manifest's
//     files[] (no extra entry, no missing file), manifest.json exactly
//     once and first, no extended headers (PAX/GNU), one gzip member with
//     bounded drain (mirroring the registry bundle, TS-014-05-01);
//   - no overwrite: content files are created with O_EXCL, so a duplicate
//     entry, a pre-existing file, or a dir/file conflict is a hard error,
//     never a silent overwrite.
//
// On failure the extractor removes every path it created (best effort),
// leaving the destination as it found it — a failed extraction never
// leaves a partial skill tree behind for a caller to misuse.
//
// The caller supplies an empty or fresh destination directory (e.g. a
// staging dir); materialization into the agent scope is the caller's
// step (ST-021-02/ST-021-03).

// Extraction caps (ADR-037 D4; skill-bundle-format.md §6.2). All caps
// are enforced while the archive is being read, not after.
const (
	// MaxAssetSize caps one extracted content file at 10 MiB (ADR-037 D4).
	MaxAssetSize = 10 << 20

	// MaxTotalSize caps the total uncompressed content of one bundle at
	// 64 MiB. Skills are markdown; the cap is generous yet bounded.
	MaxTotalSize = 64 << 20

	// MaxFileCount caps the number of content files in one bundle.
	MaxFileCount = 512

	// MaxTotalEntries caps the number of archive entries in one bundle
	// (manifest.json plus every content file and directory entry). The
	// directory-entry count is bounded separately from MaxFileCount so a
	// hostile archive padded with millions of directory entries cannot
	// drive unbounded MkdirAll/Lstat work or unbounded rollback tracking.
	MaxTotalEntries = 2 * MaxFileCount

	// MaxPathDepth caps the depth (path components) of one content path.
	MaxPathDepth = 16

	// drainBudget bounds the decompression work spent looking for
	// trailing data after the tar stream, mirroring the registry bundle
	// (1 MiB is far beyond any legitimate remainder — tar block
	// alignment is 512 bytes — and small enough to bound hostile work).
	drainBudget = 1 << 20
)

// ErrorKind classifies a rejected skill bundle into the failure classes
// of TS-021-01.
type ErrorKind string

const (
	// ErrorKindStructure marks an archive that is not a valid skill
	// bundle: not a tar.gz, violating the pinned layout, an unsafe entry
	// path (traversal, absolute, backslash), a symlink/hardlink/device
	// entry, an extended header, an entry outside the content root, or an
	// entry not declared in the manifest.
	ErrorKindStructure ErrorKind = "structure"

	// ErrorKindIntegrity marks a corrupt or truncated archive stream, or
	// trailing input beyond the single gzip member.
	ErrorKindIntegrity ErrorKind = "integrity"

	// ErrorKindManifest marks a missing, unparseable, or invalid
	// manifest.json document.
	ErrorKindManifest ErrorKind = "manifest"

	// ErrorKindFrontmatter marks an invalid SKILL.md frontmatter (or a
	// frontmatter name that does not match the manifest).
	ErrorKindFrontmatter ErrorKind = "frontmatter"

	// ErrorKindLimits marks a bundle that exceeds an extraction cap
	// (per-asset, total, file-count, path length, or depth).
	ErrorKindLimits ErrorKind = "limits"
)

// BundleError reports that a skill bundle was rejected. Kind classifies
// the failure, Field names the archive entry or component it concerns
// ("bundle" for archive-level problems), and Message is human-readable
// and actionable.
//
// When the manifest or the SKILL.md frontmatter fails validation, Cause
// wraps the *ManifestError or *FrontmatterError so callers can inspect
// the document-level problems with errors.As.
type BundleError struct {
	// Kind classifies the failure.
	Kind ErrorKind

	// Field is the archive entry or component the failure concerns.
	Field string

	// Message is a human-readable, actionable explanation.
	Message string

	// Cause is the underlying error, when one exists.
	Cause error
}

// Error implements the error interface.
func (e *BundleError) Error() string {
	field := e.Field
	if field == "" {
		field = "bundle"
	}
	if e.Cause != nil {
		return fmt.Sprintf("skill bundle %s: %s: %v (kind %s)", field, e.Message, e.Cause, e.Kind)
	}
	return fmt.Sprintf("skill bundle %s: %s (kind %s)", field, e.Message, e.Kind)
}

// Unwrap exposes the underlying error for errors.As matching.
func (e *BundleError) Unwrap() error {
	return e.Cause
}

// bundleError builds a BundleError without a cause.
func bundleError(kind ErrorKind, field, message string) *BundleError {
	return &BundleError{Kind: kind, Field: field, Message: message}
}

// bundleErrorCause builds a BundleError wrapping an underlying error.
func bundleErrorCause(kind ErrorKind, field, message string, cause error) *BundleError {
	return &BundleError{Kind: kind, Field: field, Message: message, Cause: cause}
}

// Extraction is the outcome of a successful Extract: the validated
// manifest, the validated SKILL.md frontmatter, and the content files
// written under the extraction root (relative paths, in archive order).
type Extraction struct {
	// Manifest is the validated bundle manifest.
	Manifest Manifest

	// Frontmatter is the validated SKILL.md frontmatter of the extracted
	// skill (its name equals Manifest.Name).
	Frontmatter Frontmatter

	// Files lists the extracted content paths, relative to the
	// extraction root, in archive order.
	Files []string

	// Dest is the resolved extraction root directory.
	Dest string
}

// Extract validates and extracts one skill bundle archive into dest.
// dest must be an empty or fresh directory; the extractor resolves it
// (EvalSymlinks) and writes the skill content tree under it.
//
// The validation pipeline (skill-bundle-format.md §6.3):
//
//  1. archive structure — a single-member gzip-compressed tar with
//     manifest.json first (bounded), no extended headers, safe entry
//     paths, no symlinks/hardlinks/devices, entries within the content
//     root and exactly matching the manifest inventory;
//  2. manifest — ParseManifest (required fields, patterns, inventory
//     rules);
//  3. caps — per-asset 10 MiB, total 64 MiB, 512 files, path length
//     256, depth 16, all enforced during extraction;
//  4. frontmatter — ParseFrontmatter on the extracted SKILL.md (portable
//     fields only) plus the name match against the manifest;
//  5. provenance — the provenance header "# source: <source> <version>"
//     is injected into the extracted SKILL.md (ADR-037 D10), so the
//     installed copy carries it while the archive copy stays author-form.
//
// On any failure the extractor removes everything it created under dest
// (best effort) and returns a *BundleError; errors.As can reach the
// *ManifestError or *FrontmatterError through Cause.
func Extract(data []byte, dest string) (*Extraction, error) {
	// The extraction root must exist and be a real directory, resolved
	// through any symlinked parents so containment is checked against the
	// final location (no write can be redirected outside it).
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, bundleErrorCause(ErrorKindStructure, "bundle", fmt.Sprintf("cannot create the extraction root %s", dest), err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(dest)
	if err != nil {
		return nil, bundleErrorCause(ErrorKindStructure, "bundle", fmt.Sprintf("cannot resolve the extraction root %s", dest), err)
	}
	fi, err := os.Stat(resolvedRoot)
	if err != nil {
		return nil, bundleErrorCause(ErrorKindStructure, "bundle", fmt.Sprintf("cannot stat the extraction root %s", resolvedRoot), err)
	}
	if !fi.IsDir() {
		return nil, bundleError(ErrorKindStructure, "bundle", fmt.Sprintf("the extraction root %s is not a directory", resolvedRoot))
	}

	// Track what this extraction created so a failure can remove exactly
	// that (never pre-existing content).
	tracked := &extractionTracker{}

	// fail aborts the extraction and rolls back everything created so
	// far. The rollback is best-effort; the returned error always carries
	// the original rejection.
	fail := func(err error) (*Extraction, error) {
		tracked.rollback()
		return nil, err
	}

	cr := &countingReader{r: bytes.NewReader(data)}
	gz, err := gzip.NewReader(cr)
	if err != nil {
		return fail(bundleError(ErrorKindStructure, "bundle",
			fmt.Sprintf("not a skill bundle: the input is not a gzip-compressed tar archive (%v). A skill bundle is a single .tar.gz archive carrying manifest.json first, then the content tree (skill-bundle-format.md §2).", err)))
	}
	defer gz.Close()
	// Single-member gzip only: a second member is never decompressed and
	// is rejected by the exact-consumption check at the end.
	gz.Multistream(false)

	tr := tar.NewReader(gz)

	// Entry 1: manifest.json — the identity card, first and exactly once.
	hdr, err := tr.Next()
	if err != nil {
		return fail(nextEntryError(err))
	}
	if hdr.Name != ManifestFileName || !isRegularFile(hdr) {
		return fail(entryShapeError(1, hdr))
	}
	if be := rejectExtendedHeader(hdr); be != nil {
		return fail(be)
	}
	if hdr.Size > MaxManifestSize {
		return fail(bundleError(ErrorKindLimits, ManifestFileName,
			fmt.Sprintf("is %d bytes, exceeding the %d-byte manifest cap", hdr.Size, MaxManifestSize)))
	}
	manifestData, err := io.ReadAll(io.LimitReader(tr, MaxManifestSize+1))
	if err != nil {
		return fail(bundleErrorCause(ErrorKindIntegrity, ManifestFileName,
			fmt.Sprintf("the manifest data is unreadable: %v", err), err))
	}
	if int64(len(manifestData)) > MaxManifestSize {
		return fail(bundleError(ErrorKindLimits, ManifestFileName,
			fmt.Sprintf("is %d bytes, exceeding the %d-byte manifest cap", len(manifestData), MaxManifestSize)))
	}

	manifest, err := ParseManifest(manifestData)
	if err != nil {
		return fail(bundleErrorCause(ErrorKindManifest, ManifestFileName,
			"the bundled manifest is rejected by the strict manifest parse (skillbundle.ParseManifest) — fix the manifest document or obtain a fresh bundle from the publisher", err))
	}
	contentRoot := manifest.SkillRoot() + "/"

	// Content entries: the archive must carry exactly manifest.Files.
	expected := make(map[string]bool, len(manifest.Files))
	for _, f := range manifest.Files {
		expected[f] = true
	}
	extractedFiles := make([]string, 0, len(manifest.Files))
	var totalBytes int64
	// manifest.json is the first entry; every further entry (file or
	// directory) counts toward the total-entry cap, so a directory-entry
	// bomb is bounded like any other hostile archive.
	entryCount := 1

	for {
		hdr, err = tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(nextEntryError(err))
		}
		if be := rejectExtendedHeader(hdr); be != nil {
			return fail(be)
		}

		name := hdr.Name
		// Conventional tar directory entries carry a trailing '/'; it is
		// stripped for validation and containment (a file named with a
		// trailing '/' stays rejected by validateEntryPath).
		if hdr.Typeflag == tar.TypeDir && strings.HasSuffix(name, "/") {
			name = strings.TrimSuffix(name, "/")
		}
		entryCount++
		if entryCount > MaxTotalEntries {
			return fail(bundleError(ErrorKindLimits, name,
				fmt.Sprintf("would be archive entry %d, exceeding the %d-entry cap — including directory entries (skill-bundle-format.md §6.2)", entryCount, MaxTotalEntries)))
		}
		if err := validateEntryPath(name); err != nil {
			return fail(bundleError(ErrorKindStructure, name, err.Error()))
		}
		if len(name) > MaxFilePathLength {
			return fail(bundleError(ErrorKindLimits, name,
				fmt.Sprintf("is %d bytes, exceeding the %d-byte path length cap", len(name), MaxFilePathLength)))
		}
		if depth := pathDepth(name); depth > MaxPathDepth {
			return fail(bundleError(ErrorKindLimits, name,
				fmt.Sprintf("is %d components deep, exceeding the %d-component depth cap", depth, MaxPathDepth)))
		}
		// Every content entry must live under the content root; the root
		// directory entry itself ("<name>/", stripped of its trailing
		// slash) is the one allowed exception.
		if name != manifest.Name && !strings.HasPrefix(name, contentRoot) {
			return fail(bundleError(ErrorKindStructure, name,
				fmt.Sprintf("lies outside the skill content root %q — every content entry must live under <name>/ (skill-bundle-format.md §4.4)", contentRoot)))
		}

		target, err := safeJoin(resolvedRoot, name)
		if err != nil {
			return fail(bundleError(ErrorKindStructure, name, err.Error()))
		}

		switch {
		case hdr.Typeflag == tar.TypeDir:
			if written, ok := tracked.fileAt(name); ok {
				return fail(bundleError(ErrorKindStructure, name,
					fmt.Sprintf("is a directory but %q was already extracted as a file — the archive is inconsistent (dir/file conflict)", written)))
			}
			if err := tracked.ensureDirs(resolvedRoot, name, target); err != nil {
				return fail(bundleErrorCause(ErrorKindStructure, name, "cannot create the directory", err))
			}
		case isRegularFile(hdr):
			if !expected[name] {
				return fail(bundleError(ErrorKindStructure, name,
					fmt.Sprintf("is not declared in the manifest's files[] — a skill bundle's archive must carry exactly the declared inventory (skill-bundle-format.md §4.4)")))
			}
			if hdr.Size > MaxAssetSize {
				return fail(bundleError(ErrorKindLimits, name,
					fmt.Sprintf("is %d bytes, exceeding the %d-byte per-asset cap (10 MiB; ADR-037 D4)", hdr.Size, MaxAssetSize)))
			}
			if totalBytes+hdr.Size > MaxTotalSize {
				return fail(bundleError(ErrorKindLimits, name,
					fmt.Sprintf("would bring the total extracted content to %d bytes, exceeding the %d-byte total cap (skill-bundle-format.md §6.2)", totalBytes+hdr.Size, MaxTotalSize)))
			}
			if len(extractedFiles) >= MaxFileCount {
				return fail(bundleError(ErrorKindLimits, name,
					fmt.Sprintf("would be content file %d, exceeding the %d-file cap", len(extractedFiles)+1, MaxFileCount)))
			}
			if err := writeContentFile(tr, hdr, resolvedRoot, name, target, tracked, &totalBytes); err != nil {
				return fail(err)
			}
			extractedFiles = append(extractedFiles, name)
		default:
			return fail(bundleError(ErrorKindStructure, name,
				fmt.Sprintf("is a %s entry — symlink, hardlink, and device entries are not allowed in a skill bundle (no symlink escape; ADR-037 D4)", typeflagName(hdr.Typeflag))))
		}
	}

	// Inventory exactness: every declared file must have been extracted.
	for _, f := range manifest.Files {
		if _, ok := tracked.fileAt(f); !ok {
			return fail(bundleError(ErrorKindStructure, f,
				fmt.Sprintf("is declared in the manifest's files[] but missing from the archive — a skill bundle's archive must carry exactly the declared inventory (skill-bundle-format.md §4.4)")))
		}
	}

	// Bounded drain + exact consumption (single gzip member), mirroring
	// the registry bundle (TS-014-05-01).
	if err := drainRemainder(gz); err != nil {
		return fail(err)
	}
	if remaining := int64(len(data)) - cr.n; remaining != 0 {
		return fail(bundleError(ErrorKindStructure, "bundle",
			fmt.Sprintf("trailing input after the bundle's gzip stream (%d bytes) — a skill bundle is exactly one gzip member; obtain a fresh copy of the bundle.", remaining)))
	}

	// Frontmatter: portable fields only, and the name must match the
	// manifest (skill-bundle-format.md §5).
	skillPath := filepath.Join(resolvedRoot, manifest.SkillMarkdownPath())
	skillMD, err := os.ReadFile(skillPath)
	if err != nil {
		return fail(bundleErrorCause(ErrorKindStructure, manifest.SkillMarkdownPath(),
			"the extracted SKILL.md is unreadable", err))
	}
	fm, err := ParseFrontmatter(skillMD)
	if err != nil {
		return fail(bundleErrorCause(ErrorKindFrontmatter, manifest.SkillMarkdownPath(),
			"the SKILL.md frontmatter is rejected by the portable-field validation (agentskills.io; ADR-037 D1) — fix the frontmatter or obtain a fresh bundle from the publisher", err))
	}
	if fm.Name != manifest.Name {
		return fail(bundleError(ErrorKindFrontmatter, manifest.SkillMarkdownPath(),
			fmt.Sprintf("the frontmatter name %q does not match the manifest name %q — the skill's identity must be consistent (skill-bundle-format.md §5.1)", fm.Name, manifest.Name)))
	}

	// Provenance header injection (ADR-037 D10; skill-bundle-format.md
	// §5.4): the installed copy carries "source: <standard-id> <version>".
	injected, err := InjectProvenance(skillMD, manifest.Source, manifest.Version)
	if err != nil {
		return fail(bundleErrorCause(ErrorKindFrontmatter, manifest.SkillMarkdownPath(),
			"cannot inject the provenance header into the extracted SKILL.md", err))
	}
	if err := os.WriteFile(skillPath, injected, 0o644); err != nil {
		return fail(bundleErrorCause(ErrorKindStructure, manifest.SkillMarkdownPath(),
			"cannot write the provenance-injected SKILL.md", err))
	}

	return &Extraction{
		Manifest:    *manifest,
		Frontmatter: *fm,
		Files:       extractedFiles,
		Dest:        resolvedRoot,
	}, nil
}

// writeContentFile streams one regular-file entry to disk: the parent
// directories are created, the file is written with O_EXCL and mode 0644
// (exec bit stripped; ADR-037 D4), and the copy is bounded so an entry
// that lies about its size cannot exceed the per-asset cap. It returns a
// *BundleError on any failure.
func writeContentFile(tr *tar.Reader, hdr *tar.Header, root, name, target string, tracked *extractionTracker, totalBytes *int64) error {
	if written, ok := tracked.fileAt(name); ok {
		return bundleError(ErrorKindStructure, name,
			fmt.Sprintf("duplicate archive entry — %q was already extracted (a skill bundle carries each file exactly once)", written))
	}
	if err := tracked.ensureDirs(root, name, target); err != nil {
		return bundleErrorCause(ErrorKindStructure, name, "cannot create the parent directories", err)
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return bundleErrorCause(ErrorKindStructure, name,
			fmt.Sprintf("cannot create the file (O_EXCL: a pre-existing file or a dir/file conflict is a hard error, never a silent overwrite): %v", err), err)
	}
	// Record the file immediately, before the copy: any failure from here
	// on (mid-copy stream error, close error, size mismatch) must roll the
	// file back — a partial file must never survive to block a retry with
	// O_EXCL or leave a corrupt tree behind.
	tracked.recordFile(name, target)
	written, err := io.Copy(f, io.LimitReader(tr, MaxAssetSize+1))
	closeErr := f.Close()
	switch {
	case err != nil:
		return bundleErrorCause(ErrorKindIntegrity, name, "the entry data is unreadable", err)
	case closeErr != nil:
		return bundleErrorCause(ErrorKindIntegrity, name, "cannot close the file", closeErr)
	case written > MaxAssetSize:
		return bundleError(ErrorKindLimits, name,
			fmt.Sprintf("declares %d bytes but carries more than the %d-byte per-asset cap — the archive lies about its size (ADR-037 D4)", hdr.Size, MaxAssetSize))
	case written != hdr.Size:
		return bundleError(ErrorKindIntegrity, name,
			fmt.Sprintf("declares %d bytes but carries %d — the entry is truncated or the archive lies about its size", hdr.Size, written))
	}
	*totalBytes += written
	return nil
}

// safeJoin joins root and a validated relative entry name and verifies
// the result stays inside root (the final containment check).
func safeJoin(root, name string) (string, error) {
	target := filepath.Join(root, filepath.FromSlash(name))
	cleaned := filepath.Clean(target)
	if !pathWithinRoot(root, cleaned) {
		return "", fmt.Errorf("entry path %q resolves outside the extraction root — rejected by the containment check", name)
	}
	return cleaned, nil
}

// extractionTracker records every path created by an extraction so a
// failure can remove exactly those paths (never pre-existing content),
// and detects dir/file conflicts.
type extractionTracker struct {
	// files maps relative content paths to their absolute targets.
	files map[string]string
	// dirs lists created directories, deepest first, for rollback.
	dirs []string
}

func (t *extractionTracker) init() {
	if t.files == nil {
		t.files = make(map[string]string)
	}
}

// fileAt returns the absolute target of a previously extracted file.
func (t *extractionTracker) fileAt(name string) (string, bool) {
	t.init()
	target, ok := t.files[name]
	return target, ok
}

// recordFile records an extracted file.
func (t *extractionTracker) recordFile(name, target string) {
	t.init()
	t.files[name] = target
}

// ensureDirs creates every missing parent directory of the entry (mode
// 0755), recording the ones it created, and fails if an ancestor was
// already extracted as a file (dir/file conflict).
func (t *extractionTracker) ensureDirs(root, name, target string) error {
	t.init()
	parent := filepath.Dir(target)
	rel := filepath.Dir(filepath.FromSlash(name))
	var stack []string
	for {
		if rel == "." {
			break
		}
		if _, ok := t.files[filepath.ToSlash(rel)]; ok {
			return fmt.Errorf("directory %q conflicts with a previously extracted file at the same path", rel)
		}
		stack = append(stack, parent)
		next := filepath.Dir(rel)
		if next == rel {
			break
		}
		rel = next
		parent = filepath.Dir(parent)
	}
	// Create deepest-first, recording only the directories this extraction
	// actually created (and each only once): a pre-existing directory must
	// survive a rollback, and a duplicate record would double-remove.
	for i := len(stack) - 1; i >= 0; i-- {
		dir := stack[i]
		if _, err := os.Lstat(dir); err == nil {
			// Pre-existing: verify it is a real directory (symlink
			// defense) but do not record it for rollback.
			if fi, err := os.Lstat(dir); err != nil {
				return err
			} else if fi.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("directory %q is a symlink — the extraction tree must be plain directories (no symlink escape; ADR-037 D4)", dir)
			}
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		// Defense in depth: MkdirAll follows symlinks, so a pre-existing
		// symlink at a directory path inside the extraction tree could
		// redirect writes outside the root. Every directory the tree is
		// built from must be a real directory (the extractor never
		// creates links); a symlink here is a hard error.
		if fi, err := os.Lstat(dir); err != nil {
			return err
		} else if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("directory %q is a symlink — the extraction tree must be plain directories (no symlink escape; ADR-037 D4)", dir)
		}
		if !slices.Contains(t.dirs, dir) {
			t.dirs = append(t.dirs, dir)
		}
	}
	return nil
}

// rollback removes every path this extraction created (best effort),
// files first then directories deepest-first, stopping at the extraction
// root. Pre-existing content is never touched.
func (t *extractionTracker) rollback() {
	t.init()
	for _, target := range t.files {
		_ = os.Remove(target)
	}
	for i := len(t.dirs) - 1; i >= 0; i-- {
		_ = os.Remove(t.dirs[i])
	}
	t.files = map[string]string{}
	t.dirs = nil
}

// isRegularFile reports whether the header describes a regular file
// (TypeReg '0' or the legacy TypeRegA '\x00').
func isRegularFile(hdr *tar.Header) bool {
	return hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA
}

// typeflagName renders a tar type flag for error messages.
func typeflagName(flag byte) string {
	switch flag {
	case tar.TypeSymlink:
		return "symbolic link"
	case tar.TypeLink:
		return "hard link"
	case tar.TypeChar:
		return "character device"
	case tar.TypeBlock:
		return "block device"
	case tar.TypeFifo:
		return "fifo"
	default:
		return fmt.Sprintf("unsupported type (flag %q)", string(rune(flag)))
	}
}

// nextEntryError renders an error from tar.Next: unexpected EOF is a
// truncated archive, any other error is a corrupt stream.
func nextEntryError(err error) *BundleError {
	return bundleError(ErrorKindIntegrity, "bundle",
		fmt.Sprintf("the archive is corrupt or truncated (%v) — the bundle is damaged or incomplete; obtain a fresh copy of the bundle.", err))
}

// entryShapeError renders the pinned-layout violation for an unexpected
// first entry.
func entryShapeError(position int, hdr *tar.Header) *BundleError {
	return bundleError(ErrorKindStructure, hdr.Name,
		fmt.Sprintf("is not the expected entry at position %d: a skill bundle carries manifest.json first, then the content tree declared by files[] (skill-bundle-format.md §2); obtain a fresh copy of the bundle.", position))
}

// rejectExtendedHeader rejects tar entries encoded with PAX or GNU
// extended headers (hdr.Format reports the encoding the reader had to
// parse): an extended record could alias entry names or smuggle per-entry
// metadata into the pinned layout — the layout is exact, not extensible
// (mirroring the registry bundle, TS-014-05-01).
func rejectExtendedHeader(hdr *tar.Header) *BundleError {
	if hdr.Format != tar.FormatPAX && hdr.Format != tar.FormatGNU {
		return nil
	}
	return bundleError(ErrorKindStructure, hdr.Name,
		fmt.Sprintf("is encoded with a %s extended header — the skill bundle format is pinned to plain tar headers and does not support extended headers; obtain a fresh copy of the bundle.", hdr.Format))
}

// drainRemainder reads the rest of the gzip stream within the bounded
// drainBudget and rejects the bundle if any decompressed bytes remain
// after the tar end-of-archive markers. Reading the remainder also
// validates the gzip trailer (CRC and size) when the stream is intact,
// so a corrupt compressed layer is rejected here.
func drainRemainder(gz *gzip.Reader) error {
	n, err := io.Copy(io.Discard, io.LimitReader(gz, drainBudget+1))
	if err != nil {
		return bundleError(ErrorKindIntegrity, "bundle",
			fmt.Sprintf("the archive is corrupt: the gzip stream fails validation (%v). The bundle is corrupt or was modified; obtain a fresh copy of the bundle.", err))
	}
	if n > drainBudget {
		return bundleError(ErrorKindStructure, "bundle",
			fmt.Sprintf("the bundle carries more than %d bytes of trailing data after its stream, exceeding the %d-byte drain budget — a skill bundle ends at its end-of-archive markers; obtain a fresh copy of the bundle.", drainBudget, drainBudget))
	}
	if n > 0 {
		return bundleError(ErrorKindStructure, "bundle",
			fmt.Sprintf("trailing data after the bundle stream (%d bytes) — a skill bundle ends at its end-of-archive markers; obtain a fresh copy of the bundle.", n))
	}
	return nil
}

// countingReader tracks exactly how many input bytes the gzip reader
// consumes. It implements flate.Reader (io.Reader + io.ByteReader), so
// compress/gzip uses it directly instead of wrapping it in a buffered
// reader — the count is exact, and any input bytes left after the
// bundle's gzip stream has ended are trailing input (a second gzip
// member, or garbage of any length), which the pinned single-member
// format rejects.
type countingReader struct {
	r *bytes.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *countingReader) ReadByte() (byte, error) {
	b, err := c.r.ReadByte()
	if err == nil {
		c.n++
	}
	return b, err
}
