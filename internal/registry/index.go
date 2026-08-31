package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Static index client for the delivery lifecycle standard registry
// (TS-014-02-01).
//
// Per ADR-030 the registry is a decentralized/static index: metadata
// documents live in repositories and are published/fetched as a derived,
// static artifact — there is no Core-operated registry service in v2. The
// index is distribution metadata, not content hosting: this client resolves
// metadata (standard identity, version, declared contract version,
// capability declaration, distribution location, lifecycle state, trust
// fields) and never content.
//
// Index layout. The static index is a local directory of metadata
// documents, one file per standard release, laid out as
//
//	<index-dir>/
//	  <standard-id>/
//	    <version>.json
//
// for example:
//
//	index/
//	  anvil-standard-laravel/
//	    1.0.0.json
//	    1.2.3.json
//	  anvil-standard-flutter/
//	    2.0.0.json
//
// This layout is consistent with a static index fetched/published via git:
// each release adds exactly one new file (mirroring the tag-driven
// GitHub-releases channel of ADR-030 §3, §5), so publishing is add-only,
// diffs stay small, and publishers adding releases in parallel do not
// rewrite shared files. The client does not depend on the layout: entry
// identity comes from the document content (the declared id and version),
// not from the file path — identity from content, not location
// (Manifesto §3.4). The layout is the canonical convention for publishing;
// the client's robustness does not rely on it.
//
// The index tree must contain only entry documents: every *.json file
// under the directory is treated as an entry document, so any other JSON
// file (a README.json, a copy of the schema, a stray dump) fails load by
// design. Only non-JSON files and hidden entries are ignored. The index
// directory and tree must be a plain directory tree: symlinks are rejected
// (the index is a fetched/published artifact, not a link farm — see
// LoadIndex).
//
// Parsing. Strict schema validation (required fields, semver patterns,
// enum bounds, if/then constraints, format annotations) is the registry
// client's parsing responsibility (TS-014-01-02, parse.go, planned). This
// client performs structural decoding only — encoding/json into the
// Metadata mirror — so it works against the format independent of parse
// progress; when the strict parse lands, its exports replace the inline
// decode below (the single decode site is loadDocument). Documents that
// cannot be indexed at all — unreadable, structurally malformed, or
// lacking the identity fields that form the index key — fail index load
// with an actionable error naming the offending document. Each document is
// read through a size cap (MaxIndexDocumentSize): metadata documents are
// small, and an oversized document fails load with an error naming the
// file — the index must not be a vehicle for unbounded memory use.
//
// Resolution semantics. Resolve requires an exact id and version pair:
// wildcard or "latest" resolution is out of scope — adoptions pin explicit
// versions (ADR-022 §3). Missing versions produce an error listing the
// available versions of that standard. Duplicate entries (two documents
// declaring the same id and version) fail index load: in a static, derived
// index a duplicate release is a publishing error, and failing loudly at
// load prevents silent divergence between index copies (deterministic
// first-wins was rejected because it hides exactly the corruption the
// index client is meant to surface).
//
// Reference: TS-014-02-01, ADR-022, ADR-023, ADR-030
type Index struct {
	// dir is the local index directory the index was loaded from.
	dir string

	// entries maps standard id -> release version -> resolved entry.
	entries map[string]map[string]Entry
}

// Entry is one resolved index entry: the full metadata of one standard
// release plus the index document it was resolved from.
//
// The Metadata fields are promoted, so the resolved entry carries the
// declared contract version (ContractVersion), the capability declaration
// (Capability), the distribution location (Distribution), and the
// lifecycle state (Lifecycle) directly, alongside identity (ID, Version)
// and the trust fields (Trust).
type Entry struct {
	Metadata

	// Source is the path of the index document this entry was resolved
	// from, for diagnostics and actionable error messages.
	Source string
}

// Sentinel errors. Consumers match them with errors.Is on the wrapped
// errors returned by LoadIndex and Index.Resolve.
var (
	// ErrIndexNotFound reports that the index directory does not exist.
	ErrIndexNotFound = errors.New("registry index not found")

	// ErrEntryNotFound reports that no index entry declares the
	// requested standard id and version.
	ErrEntryNotFound = errors.New("registry index entry not found")

	// ErrDuplicateEntry reports that two index documents declare the
	// same standard id and version.
	ErrDuplicateEntry = errors.New("registry index contains duplicate entries")
)

// MaxIndexDocumentSize caps the size of a single index document (1 MiB).
// Registry metadata documents are small (kilobytes at most — one release,
// a handful of digests); the cap bounds memory use when reading an index
// and turns an accidentally committed large file into a precise,
// actionable load error instead of a decode failure.
const MaxIndexDocumentSize = 1 << 20

// LoadIndex reads the static index from dir: every *.json document under
// the directory is decoded and indexed by the id and version it declares.
//
// The whole index is validated at load, not lazily: an index that cannot
// be fully read is a broken derived artifact and should fail early.
// Failure cases, each producing an actionable error:
//
//   - the index directory does not exist: wrapped ErrIndexNotFound;
//   - an index document is unreadable: the wrapped error names the file;
//   - an index document exceeds MaxIndexDocumentSize: the error names the
//     file and the cap;
//   - an index document is not decodable JSON: the wrapped error names
//     the file (parse diagnostics land with TS-014-01-02, parse.go,
//     planned);
//   - an index document lacks a non-empty id or version: the identity
//     fields are the index key, so the document cannot be indexed
//     (structural requirement, not schema validation);
//   - two documents declare the same id and version: wrapped
//     ErrDuplicateEntry naming both documents;
//   - the tree contains a symlink (file or directory): the error names
//     the path — symlinks are not supported, the index is a plain
//     directory tree.
//
// Hidden entries (names starting with "." — e.g. .git) and non-.json
// files are ignored; an index directory containing no documents loads as
// an empty index.
func LoadIndex(dir string) (*Index, error) {
	ix := &Index{
		dir:     dir,
		entries: make(map[string]map[string]Entry),
	}

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrIndexNotFound, dir)
		}
		return nil, fmt.Errorf("registry index: stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("registry index: %s is not a directory", dir)
	}

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("registry index: walk %s: %w", path, err)
		}
		// Symlinks are rejected outright, before any type handling: the
		// index is a fetched/published plain-tree artifact, and a link
		// could point outside the index directory (a broken or hostile
		// index must not be able to read arbitrary paths).
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("registry index: %s: symlinks are not supported in the index tree", path)
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		return ix.addDocument(path)
	})
	if err != nil {
		return nil, err
	}

	return ix, nil
}

// loadDocument is the single decode site for index documents. It performs
// structural decoding into the Metadata mirror only; strict schema
// validation is the parse responsibility (TS-014-01-02, parse.go, planned)
// and plugs in here when it lands.
//
// Documents are read through io.LimitReader capped at
// MaxIndexDocumentSize: at most cap+1 bytes are read, so a document that
// exceeds the cap is reported precisely (naming the file) instead of
// surfacing as a truncated-JSON decode error.
func loadDocument(path string) (Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("registry index: read %s: %w", path, err)
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, MaxIndexDocumentSize+1))
	if err != nil {
		return Metadata{}, fmt.Errorf("registry index: read %s: %w", path, err)
	}
	if len(raw) > MaxIndexDocumentSize {
		return Metadata{}, fmt.Errorf(
			"registry index: %s: document exceeds the %d-byte size cap",
			path, MaxIndexDocumentSize,
		)
	}

	var m Metadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return Metadata{}, fmt.Errorf("registry index: decode %s: %w", path, err)
	}
	return m, nil
}

// addDocument decodes the document at path and indexes it by the id and
// version it declares, rejecting duplicates (TS-014-02-01).
func (ix *Index) addDocument(path string) error {
	m, err := loadDocument(path)
	if err != nil {
		return err
	}
	if m.ID == "" || m.Version == "" {
		return fmt.Errorf(
			"registry index: %s: document must declare a non-empty id and version (identity is the index key)",
			path,
		)
	}

	versions, ok := ix.entries[m.ID]
	if !ok {
		versions = make(map[string]Entry)
		ix.entries[m.ID] = versions
	}
	if existing, ok := versions[m.Version]; ok {
		return fmt.Errorf(
			"%w: standard %q version %q declared by %s and %s",
			ErrDuplicateEntry, m.ID, m.Version, existing.Source, path,
		)
	}
	versions[m.Version] = Entry{Metadata: m, Source: path}
	return nil
}

// Resolve returns the index entry for the standard id at the exact
// version, or an actionable error when it is not in the index.
//
// Resolution is exact-pin only: wildcard or "latest" resolution is out of
// scope, adoptions pin explicit versions (ADR-022 §3; TS-014-02-01). A
// missing entry returns a wrapped ErrEntryNotFound that lists the
// available versions of the standard. id and version must be non-empty.
func (ix *Index) Resolve(id, version string) (Entry, error) {
	if id == "" || version == "" {
		return Entry{}, fmt.Errorf("registry index: resolve: id and version must not be empty")
	}

	versions, ok := ix.entries[id]
	if !ok {
		return Entry{}, fmt.Errorf(
			"%w: standard %q at version %q is not in the index at %s",
			ErrEntryNotFound, id, version, ix.dir,
		)
	}
	entry, ok := versions[version]
	if !ok {
		return Entry{}, fmt.Errorf(
			"%w: standard %q at version %q is not in the index at %s (available versions: %s)",
			ErrEntryNotFound, id, version, ix.dir, strings.Join(ix.Versions(id), ", "),
		)
	}
	return entry, nil
}

// Standards returns the standard IDs present in the index, sorted
// ascending — the discovery surface for listing available standards
// (T-005). The error return is reserved: with the load-time-validated
// index, enumeration cannot fail and always returns nil; the signature
// keeps the enumeration contract uniform for future index sources that
// may enumerate lazily.
func (ix *Index) Standards() ([]string, error) {
	out := make([]string, 0, len(ix.entries))
	for id := range ix.entries {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// Versions returns the versions of the standard id present in the index,
// sorted ascending in lexical order (semantic version ordering is out of
// scope for the index client). It returns nil when the standard is not in
// the index.
func (ix *Index) Versions(id string) []string {
	versions, ok := ix.entries[id]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(versions))
	for version := range versions {
		out = append(out, version)
	}
	sort.Strings(out)
	return out
}
