// Adoption validation orchestration (TS-014-04-03).
//
// Per ADR-022 §3, validation at adoption is a single, coherent mechanism:
// trust and compatibility are validated together at every install and
// update, results are recorded for auditability, and failures are
// reported with actionable messaging at install time (ADR-023 §3). This
// file orchestrates the two validation engines — compatibility
// (compatibility.go, TS-014-04-01) and trust (trust.go, TS-014-04-02) —
// into that one adoption-time mechanism, used by the install flow
// (TS-014-03-01) and the update flow (TS-014-03-02).
//
// Adoption sequence (preserves the pinned adoption order of TS-014-03-01;
// PM binding decision 2). The flow is explicit, two-phase, and
// side-effect-free — nothing is fetched inside this component; content
// arrives as bytes (ADR-030: the registry is distribution metadata, not
// content hosting):
//
//	parse (caller) → lifecycle gate → compatibility → (content, fetched
//	by the caller) → trust → combined record
//
// The two phases are two entry points:
//
//  1. ValidateAdoptionBeforeFetch — the pre-fetch phase: the lifecycle
//     gate (adoptability; retired releases are not offered for fresh
//     adoption, ADR-027 §3) and compatibility validation (declared
//     contract version against the runtime's supported contract majors,
//     and the capability declaration against the project's framework
//     version; ADR-024 §3.1, §3.4; ADR-021 §3.2). No content is needed.
//     The caller aborts the adoption when this phase fails — content is
//     never fetched (a compatibility failure means zero fetches, the
//     pinned order).
//  2. ValidateAdoptionAfterFetch — the post-fetch phase: trust
//     verification over the fetched content bytes (integrity, publisher
//     attestation, out-of-band trust anchor; ADR-022 §3) and the
//     combined AdoptionResult record. It requires the pre-fetch result:
//     when the pre-fetch phase failed, the adoption already aborted and
//     trust is not evaluated — the aborted result is returned unchanged
//     and can never become Valid through this function.
//
// Both validations always run in a full adoption: every adoption that
// reaches the content executes both phases, and a failure in either
// phase aborts the adoption (DoD TS-014-04-03). The structure makes
// skipping impossible: the combined record's Valid derives exclusively
// from the sub-results, and the post-fetch phase is the only way to
// attach a trust result to the record — there is no path to a Valid
// AdoptionResult that bypasses compatibility or trust.
//
// Result recording (T-009). AdoptionResult is the combined, JSON-
// serializable record of one adoption: metadata identity (id, version —
// self-identifying, T-010 product note G1), the lifecycle outcome, the
// embedded CompatibilityResult and TrustResult, and the overall Valid.
// The installed-standard record persists the compatibility and trust
// results as typed embedded sections; CompatibilityRecord and
// TrustRecord expose the results in exactly that shape.
//
// Actionable failures. AdoptionResult.Errors aggregates every rejection
// reason across the dimensions that ran — lifecycle, compatibility,
// trust — each message stating what failed and how to resolve it (the
// per-dimension engines already produce those messages; the
// orchestration aggregates them, ADR-023 §3).
//
// Supported contract majors (T-010 reviewer finding G4 — must-fix).
// The runtime's supported contract major set is read at runtime from
// the compatibility matrix (docs/specification-corpus/compatibility-
// matrix.json — the corpus reference the declarations are checked
// against; ADR-029 §3: engine-path-independent corpus; ADR-024 §3.4).
// LoadCompatibilityMatrix parses the matrix record; the supported set
// is passed into ValidateAdoptionBeforeFetch (the caller resolves it —
// the caller may override it for tests). A matrix file that cannot be
// read — missing, corrupt, or structurally invalid — is an actionable
// error, never a silent default: supported majors are declared,
// validated, and recorded, never assumed (Transition Plan A2;
// ADR-024 §3.6).
//
// Scope. This component never fetches content, never reads project
// configuration, and never persists records: resolution, framework
// version, and persistence belong to the consuming flows (T-007/T-008).
//
// Reference: TS-014-04-03, ADR-022 §3, ADR-023 §3, ADR-024 §3.1, §3.4,
// ADR-029 §3, ADR-030, PRD-002 §5.7, §5.8
package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

// ── Adoption orchestration ───────────────────────────────────────────

// AdoptionResult is the combined, JSON-serializable record of one
// adoption validation run (TS-014-04-03): the metadata identity (the
// self-identifying half of the record, T-010 product note G1), the
// lifecycle outcome, the embedded compatibility result, the embedded
// trust result, and the overall Valid. The persistence consumer (T-009)
// stores the compatibility and trust results as typed embedded sections
// of the installed-standard record; CompatibilityRecord and TrustRecord
// expose them in that exact shape.
//
// The result is built in two phases: ValidateAdoptionBeforeFetch
// produces it with the lifecycle and compatibility results (Trust is
// nil — the trust phase has not run), and ValidateAdoptionAfterFetch
// completes it with the trust result. Valid is true only when every
// dimension that ran passed: a failure in any dimension aborts the
// adoption (ADR-022 §3; DoD TS-014-04-03).
type AdoptionResult struct {
	// ID is the standard identity of the adoption — the metadata
	// document's id, the identity half of the installation idempotency
	// key (ADR-023 §3).
	ID string `json:"id"`

	// Version is the release version of the adoption, pinned at
	// adoption (ADR-022 §3; the second half of the idempotency key).
	Version string `json:"version"`

	// Lifecycle is the lifecycle state of the release as declared in
	// the metadata document (ADR-023 §3, ADR-027 §3), recorded for
	// auditability.
	Lifecycle Lifecycle `json:"lifecycle"`

	// Adoptable reports the lifecycle gate outcome: whether the release
	// is offered for fresh adoption (published or deprecated). Retired
	// releases — and releases declaring an unknown state — are not
	// adoptable (ADR-027 §3).
	Adoptable bool `json:"adoptable"`

	// Compatibility is the embedded compatibility validation result
	// (TS-014-04-01), always evaluated in the pre-fetch phase.
	Compatibility CompatibilityResult `json:"compatibility"`

	// Trust is the embedded trust validation result (TS-014-04-02),
	// attached by the post-fetch phase. Nil until the trust phase runs.
	Trust *TrustResult `json:"trust,omitempty"`

	// Valid reports whether the adoption may proceed: every dimension
	// that ran passed. When false, Errors carries every rejection
	// reason.
	Valid bool `json:"valid"`

	// Errors lists every rejection reason across the dimensions that
	// ran — lifecycle, compatibility, trust — each actionable: what
	// failed and how to resolve it. Empty when Valid is true.
	Errors []string `json:"errors,omitempty"`
}

// CompatibilityRecord returns the compatibility validation result as the
// pointer shape the installed-standard record persists
// (InstalledStandardRecord.Compatibility, T-009).
func (r AdoptionResult) CompatibilityRecord() *CompatibilityResult {
	return &r.Compatibility
}

// TrustRecord returns the trust validation result as the pointer shape
// the installed-standard record persists (InstalledStandardRecord.Trust,
// T-009). Nil when the trust phase has not run — the pre-fetch result of
// an adoption that never reached the content.
func (r AdoptionResult) TrustRecord() *TrustResult {
	return r.Trust
}

// ValidateAdoptionBeforeFetch runs the pre-fetch phase of adoption
// validation (TS-014-04-03; PM binding decision 2): the lifecycle gate
// and compatibility validation, in the pinned order. No content is
// needed. A release that is not offered for adoption (retired, or an
// unknown state) aborts at the lifecycle gate: compatibility is not
// evaluated for a release that cannot be adopted, and the result carries
// only the lifecycle rejection. Otherwise compatibility runs against
// supportedContractMajors — the runtime's supported contract major set,
// read from the compatibility matrix (LoadCompatibilityMatrix) or
// overridden by the caller — and projectFrameworkVersion (empty for a
// framework-free project: shape-only capability validation, recorded
// explicitly, ADR-026).
//
// The caller aborts the adoption when the result is not Valid — content
// is never fetched after a lifecycle or compatibility failure (the
// pinned adoption order: a compatibility failure means zero fetches).
//
// Reference: TS-014-04-03, TS-014-04-01, ADR-022 §3, ADR-023 §3,
// ADR-024 §3.1, §3.4, ADR-027 §3
func ValidateAdoptionBeforeFetch(md Metadata, supportedContractMajors []int, projectFrameworkVersion string) AdoptionResult {
	result := AdoptionResult{
		ID:        md.ID,
		Version:   md.Version,
		Lifecycle: md.Lifecycle,
	}

	checkLifecycle(&result, md)
	if !result.Adoptable {
		// The adoption aborts at the lifecycle gate: a release that is
		// not offered for adoption is not compatibility-validated —
		// downstream gates do not run after an aborted adoption (the
		// abort semantics of the pinned order).
		result.Valid = false
		return result
	}

	result.Compatibility = ValidateCompatibility(md, supportedContractMajors, projectFrameworkVersion)
	result.Errors = append(result.Errors, result.Compatibility.Errors...)
	result.Valid = result.Compatibility.Valid
	return result
}

// ValidateAdoptionAfterFetch runs the post-fetch phase of adoption
// validation (TS-014-04-03; PM binding decision 2): trust verification
// over the fetched content bytes (integrity, publisher attestation,
// out-of-band trust anchor — VerifyTrust, TS-014-04-02) and the
// combined record. It completes the pre-fetch result: when the pre-fetch
// phase failed, the adoption already aborted before the content was
// fetched — trust is not evaluated, and the aborted result is returned
// unchanged (it can never become Valid through this function, so a
// bypassed compatibility gate can never yield a passing adoption).
//
// The combined record's Valid requires both the pre-fetch phase and
// trust to have passed: a failure in either aborts the adoption
// (ADR-022 §3; DoD TS-014-04-03). The combination itself is the single
// source CompleteAdoption — the same combination the offline/bundled
// path (TS-014-05-02) applies to its Bundle.Verify result.
//
// Reference: TS-014-04-03, TS-014-04-02, ADR-022 §3
func ValidateAdoptionAfterFetch(md Metadata, content []byte, anchors *TrustAnchors, before AdoptionResult) AdoptionResult {
	if !before.Valid {
		// The adoption aborted at the lifecycle or compatibility gate
		// (the pinned order: content is never fetched after such a
		// failure). Trust is not evaluated — the aborted result is the
		// final result.
		return before
	}

	trust := VerifyTrust(md, content, anchors)
	return CompleteAdoption(before, trust)
}

// CompleteAdoption combines a pre-fetch adoption result with the trust
// result of the post-fetch/verification phase into the final combined
// record (TS-014-04-03). It is the single source of the combination
// semantics, shared by every adoption path:
//
//   - the online path — ValidateAdoptionAfterFetch runs VerifyTrust and
//     combines through this helper;
//   - the offline/bundled path (TS-014-05-02) — the offline flow runs
//     Bundle.Verify (the exact same VerifyTrust engine) and combines
//     through this helper instead of re-implementing the combination.
//
// Semantics: Trust is attached to the result, Valid requires BOTH the
// pre-fetch phase and trust to have passed (ADR-022 §3; DoD
// TS-014-04-03), and every trust rejection reason is appended to
// Errors. A failed pre-fetch result combined here can never become
// Valid: Valid derives exclusively from the sub-results, so a bypassed
// compatibility gate can never yield a passing adoption.
func CompleteAdoption(before AdoptionResult, trust TrustResult) AdoptionResult {
	result := before
	result.Trust = &trust
	result.Errors = append(result.Errors, trust.Errors...)
	result.Valid = before.Valid && trust.Valid
	return result
}

// checkLifecycle runs the lifecycle gate (TS-014-01-03; ADR-023 §3,
// ADR-027 §3): published and deprecated releases are adoptable;
// deprecated releases remain adoptable with a warning surfaced by the
// consuming flow (LifecycleWarning). A retired release is not offered
// for fresh adoption — it is explicitly distinguished from a release
// that is not in the registry — and an unknown state (defensive guard;
// the parse layer rejects unknown states) gets its own message. Every
// rejection is actionable: what failed and how to resolve it.
func checkLifecycle(result *AdoptionResult, md Metadata) {
	if LifecycleAdoptable(md.Lifecycle.State) {
		result.Adoptable = true
		return
	}

	label := md.ID
	if label == "" {
		label = "<standard>"
	}
	if md.Lifecycle.State == LifecycleStateRetired {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"standard %q version %q is retired and is not offered for fresh adoption; retired standards are removed from the registry and are not resolvable for fresh adoption (ADR-027 §3), which is different from a standard not being in the index. Follow the documented migration path of the retired standard, or choose another standard offered for adoption.",
			label, md.Version))
		return
	}

	state := md.Lifecycle.State
	if state == "" {
		state = "<unknown>"
	}
	result.Errors = append(result.Errors, fmt.Sprintf(
		"standard %q version %q declares unknown lifecycle state %q and is not offered for adoption; the lifecycle state is not one of the governed machine values (published, deprecated, retired — ADR-023 §3, ADR-027 §3). Fix the metadata document's lifecycle.state, or choose another standard offered for adoption.",
		label, md.Version, state))
}

// ── Compatibility matrix reader (T-010 reviewer finding G4) ──────────

// EnvCompatibilityMatrix names the environment variable that overrides
// the default compatibility matrix file path.
//
// Reference: TS-014-04-03 (PM binding decision 3; index/anchors
// conventions TS-014-02-02 / TS-014-04-02)
const EnvCompatibilityMatrix = "ANVIL_COMPATIBILITY_MATRIX"

// DefaultCompatibilityMatrixRelativePath is the compatibility matrix
// file path relative to the repository root: the corpus file that
// records the runtime's supported contract majors
// (docs/specification-corpus/compatibility-matrix.json, TS-013-05-02;
// ADR-029 §3). The corpus is co-located with the engine in the Core
// repository (ADR-029 §5.2), so running the engine from the repository
// root locates the matrix without configuration; operators running the
// engine from elsewhere point it at the corpus file with
// ANVIL_COMPATIBILITY_MATRIX. A matrix that cannot be located is an
// actionable error, never a silent default (PM binding decision 3).
const DefaultCompatibilityMatrixRelativePath = "docs/specification-corpus/compatibility-matrix.json"

// MaxCompatibilityMatrixSize caps the size of the compatibility matrix
// file (1 MiB). The matrix record holds a handful of fields — a file
// beyond the cap is a broken artifact and fails load with a precise,
// actionable error instead of unbounded memory use (mirrors
// MaxTrustAnchorsSize).
const MaxCompatibilityMatrixSize = 1 << 20

// MatrixDocumentID is the corpus identity of the compatibility matrix
// record, pinned by compatibility-matrix.md §5: the reader requires the
// file to declare exactly this identity, so pointing the reader at the
// wrong document fails with an actionable error instead of silently
// adopting whatever the file declares.
const MatrixDocumentID = "compatibility-matrix"

// ErrCompatibilityMatrixNotFound reports that the compatibility matrix
// file does not exist. Consumers match it with errors.Is on the error
// returned by LoadCompatibilityMatrix.
var ErrCompatibilityMatrixNotFound = errors.New("compatibility matrix file not found")

// CompatibilityMatrix is the machine-readable compatibility matrix
// record (docs/specification-corpus/compatibility-matrix.json,
// TS-013-05-02): the recorded statement of which contract versions the
// runtime implements, and the reference a standard's declared target
// contract version is checked against at adoption (PRD-002 §5.8;
// compatibility-matrix.md §1). The record is kept minimal — only the
// facts the corpus records, no derived or invented fields
// (compatibility-matrix.md §5).
type CompatibilityMatrix struct {
	// DocumentID is the corpus identity of the record,
	// "compatibility-matrix" (compatibility-matrix.md §5).
	DocumentID string `json:"document_id"`

	// ContractVersion is the current contract version of the delivery
	// lifecycle specification the runtime implements, recorded from the
	// version-line declaration (version-line.md; ADR-024 §3.2).
	ContractVersion string `json:"contract_version"`

	// SupportedContractMajors is the runtime's supported contract major
	// set, recorded from the version-line declaration; the set the
	// compatibility validation checks a standard's declared contract
	// version against (ADR-024 §3.1, §3.4).
	SupportedContractMajors []int `json:"supported_majors"`

	// MaintainedUnder is the maintenance-path note of the record.
	MaintainedUnder string `json:"maintained_under,omitempty"`
}

// LoadCompatibilityMatrix reads and validates the compatibility matrix
// record at path (TS-014-04-03; T-010 reviewer finding G4 — must-fix):
//
//   - the file must exist (wrapped ErrCompatibilityMatrixNotFound) and
//     be readable;
//   - the file must not exceed MaxCompatibilityMatrixSize;
//   - the file must be decodable JSON declaring exactly the record
//     shape of compatibility-matrix.md §5: document_id
//     "compatibility-matrix", a non-empty contract_version, and a
//     supported_majors set that is non-empty, unique, and composed of
//     representable contract majors (>= 1). Unknown fields and trailing
//     content are rejected — the record shape is pinned by the corpus
//     maintenance process, and a broken record must never be partially
//     applied;
//   - the record's bounds consistency with the version line (at most
//     two concurrent majors, consecutive generations — ADR-024 §3.4)
//     is validated by the corpus version tooling (TS-013-05-01/02),
//     not re-implemented here: the engine reads the recorded set
//     faithfully.
//
// Loading is purely local: the matrix is read from disk at adoption
// time — the runtime's supported majors are read, never hardcoded, so
// the engine and the corpus cannot drift (ADR-029 §3). A matrix that
// cannot be read — missing, corrupt, or structurally invalid — is an
// actionable error naming the file and the fix; supported majors are
// never silently defaulted (PM binding decision 3).
//
// Reference: TS-014-04-03, TS-013-05-02, ADR-024 §3.1, §3.4, ADR-029 §3
func LoadCompatibilityMatrix(path string) (*CompatibilityMatrix, error) {
	if path == DefaultCompatibilityMatrixRelativePath {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			// Fallback to embedded matrix (ADR-039) — fresh-machine without checkout
			return loadEmbeddedCompatibilityMatrix()
		}
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Also fallback to embedded for default path missing (covers ENOENT after stat race)
			if path == DefaultCompatibilityMatrixRelativePath {
				return loadEmbeddedCompatibilityMatrix()
			}
			return nil, fmt.Errorf("%w: %s", ErrCompatibilityMatrixNotFound, path)
		}
		return nil, fmt.Errorf("compatibility matrix: open %s: %w", path, err)
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, MaxCompatibilityMatrixSize+1))
	if err != nil {
		return nil, fmt.Errorf("compatibility matrix: read %s: %w", path, err)
	}
	if len(raw) > MaxCompatibilityMatrixSize {
		return nil, fmt.Errorf(
			"compatibility matrix: %s: file exceeds the %d-byte size cap",
			path, MaxCompatibilityMatrixSize,
		)
	}

	var matrix CompatibilityMatrix
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&matrix); err != nil {
		return nil, fmt.Errorf(
			"compatibility matrix: %s: not decodable JSON: %v — the matrix file must declare exactly the record shape of compatibility-matrix.md §5 (document_id, contract_version, supported_majors)",
			path, err)
	}
	// Strict trailing-content rejection: ANY token after the record —
	// including a stray `]` or `}` that dec.More() would miss — makes
	// the file a broken artifact. The record shape is pinned by the
	// corpus maintenance process; trailing content is never tolerated,
	// so a truncated record concatenated with garbage cannot be
	// partially applied (QA F1).
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("compatibility matrix: %s: unexpected content after the record", path)
	}

	if matrix.DocumentID != MatrixDocumentID {
		return nil, fmt.Errorf(
			"compatibility matrix: %s: document_id %q is not %q — the file is not the compatibility matrix record; point %s at the corpus file docs/specification-corpus/compatibility-matrix.json",
			path, matrix.DocumentID, MatrixDocumentID, EnvCompatibilityMatrix)
	}
	if strings.TrimSpace(matrix.ContractVersion) == "" {
		return nil, fmt.Errorf(
			"compatibility matrix: %s: missing required field \"contract_version\" — the record must declare the current contract version (compatibility-matrix.md §5); fix the matrix file",
			path)
	}
	if len(matrix.SupportedContractMajors) == 0 {
		return nil, fmt.Errorf(
			"compatibility matrix: %s: missing required field \"supported_majors\" — the record must declare the supported contract major set (ADR-024 §3.4); fix the matrix file",
			path)
	}

	seen := make(map[int]bool, len(matrix.SupportedContractMajors))
	for _, major := range matrix.SupportedContractMajors {
		if major < 1 {
			return nil, fmt.Errorf(
				"compatibility matrix: %s: supported major %d is invalid — contract majors are >= 1; fix the matrix file",
				path, major)
		}
		if seen[major] {
			return nil, fmt.Errorf(
				"compatibility matrix: %s: supported major %d is declared more than once; the supported set is unique (ADR-024 §3.4); fix the matrix file",
				path, major)
		}
		seen[major] = true
	}

	return &matrix, nil
}

// ResolveCompatibilityMatrixPath resolves the compatibility matrix file
// path, in order:
//
//  1. the explicit path argument (non-empty);
//  2. the ANVIL_COMPATIBILITY_MATRIX environment variable (non-empty);
//  3. the documented default: the corpus file relative to the working
//     directory (DefaultCompatibilityMatrixRelativePath) — the corpus
//     is co-located with the engine in the repository (ADR-029 §5.2),
//     so running the engine from the repository root locates the matrix
//     without configuration.
//
// getenv is injected for testability. This mirrors the trust anchors
// path convention (anchors.go, TS-014-04-02). Loading happens later, at
// adoption time (LoadCompatibilityMatrix); a matrix that cannot be
// resolved or read is an actionable error — never a silent default
// (PM binding decision 3).

// loadEmbeddedCompatibilityMatrix parses the embedded matrix (ADR-039).
func loadEmbeddedCompatibilityMatrix() (*CompatibilityMatrix, error) {
	raw := EmbeddedCompatibilityMatrix()
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: embedded compatibility matrix is empty", ErrCompatibilityMatrixNotFound)
	}
	if len(raw) > MaxCompatibilityMatrixSize {
		return nil, fmt.Errorf("compatibility matrix: embedded: file exceeds the %d-byte size cap", MaxCompatibilityMatrixSize)
	}
	var matrix CompatibilityMatrix
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&matrix); err != nil {
		return nil, fmt.Errorf("compatibility matrix: embedded: not decodable JSON: %v", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("compatibility matrix: embedded: unexpected content after the record")
	}
	if matrix.DocumentID != MatrixDocumentID {
		return nil, fmt.Errorf("compatibility matrix: embedded: document_id %q is not %q", matrix.DocumentID, MatrixDocumentID)
	}
	if len(matrix.SupportedContractMajors) == 0 {
		return nil, fmt.Errorf("compatibility matrix: embedded: missing supported_majors")
	}
	return &matrix, nil
}

func ResolveCompatibilityMatrixPath(explicit string, getenv func(string) string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if value := getenv(EnvCompatibilityMatrix); value != "" {
		return value, nil
	}
	return DefaultCompatibilityMatrixRelativePath, nil
}
