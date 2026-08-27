package registry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ── Pre-fetch phase: lifecycle gate ──────────────────────────────────

// TestValidateAdoptionBeforeFetchValid asserts a published release with a
// supported contract major and a capability declaration covering the
// project's framework version passes the pre-fetch phase: adoptable,
// compatibility valid, trust not yet evaluated, no errors, and the
// result self-identifies the adoption (id, version — T-010 product note
// G1).
func TestValidateAdoptionBeforeFetchValid(t *testing.T) {
	md := sampleMetadata()
	result := ValidateAdoptionBeforeFetch(md, []int{1}, "5.2.3")

	if !result.Valid {
		t.Errorf("Valid = false, want true; errors: %v", result.Errors)
	}
	if !result.Adoptable {
		t.Error("Adoptable = false, want true (published)")
	}
	if result.ID != md.ID || result.Version != md.Version {
		t.Errorf("result identity = %s %s, want %s %s", result.ID, result.Version, md.ID, md.Version)
	}
	if !result.Compatibility.Valid || !result.Compatibility.FrameworkVersionChecked {
		t.Errorf("Compatibility = %+v, want valid with framework version checked", result.Compatibility)
	}
	if result.Trust != nil {
		t.Error("Trust != nil after the pre-fetch phase, want nil (trust runs after the content is fetched)")
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want none", result.Errors)
	}
}

// TestValidateAdoptionBeforeFetchRetiredRejected asserts a retired
// release is not offered for fresh adoption: the lifecycle gate fails
// with an actionable message distinguishing retired from not-found, and
// compatibility is not evaluated — the adoption aborts at the gate.
func TestValidateAdoptionBeforeFetchRetiredRejected(t *testing.T) {
	md := sampleMetadata()
	md.Lifecycle.State = LifecycleStateRetired

	result := ValidateAdoptionBeforeFetch(md, []int{1}, "5.2.3")

	if result.Valid {
		t.Error("Valid = true, want false (retired)")
	}
	if result.Adoptable {
		t.Error("Adoptable = true, want false (retired)")
	}
	if !hasMessage(result.Errors, "retired") || !hasMessage(result.Errors, "not offered for fresh adoption") {
		t.Errorf("Errors = %v, want the retired rejection naming the state", result.Errors)
	}
	if !hasMessage(result.Errors, "migration path") {
		t.Errorf("Errors = %v, want a resolution hint (migration path)", result.Errors)
	}
	// The adoption aborted at the lifecycle gate: compatibility is not
	// evaluated for a release that cannot be adopted.
	if result.Compatibility.Valid || result.Compatibility.ContractVersionCompatible {
		t.Errorf("Compatibility = %+v, want not evaluated after the lifecycle gate failure", result.Compatibility)
	}
	if result.Trust != nil {
		t.Error("Trust != nil after a lifecycle gate failure, want nil")
	}
}

// TestValidateAdoptionBeforeFetchUnknownStateRejected asserts an unknown
// lifecycle state (defensive guard; the parse layer rejects unknown
// states) is not adoptable and gets its own actionable message naming
// the state.
func TestValidateAdoptionBeforeFetchUnknownStateRejected(t *testing.T) {
	md := sampleMetadata()
	md.Lifecycle.State = "unknown-state"

	result := ValidateAdoptionBeforeFetch(md, []int{1}, "5.2.3")

	if result.Valid || result.Adoptable {
		t.Errorf("Valid/Adoptable = %v/%v, want false for an unknown state", result.Valid, result.Adoptable)
	}
	if !hasMessage(result.Errors, "unknown lifecycle state") || !hasMessage(result.Errors, "unknown-state") {
		t.Errorf("Errors = %v, want the unknown-state rejection naming the state", result.Errors)
	}
}

// TestValidateAdoptionBeforeFetchDeprecatedAdoptable asserts a
// deprecated release remains adoptable (ADR-027 §3): the lifecycle gate
// passes and compatibility is evaluated; the deprecation warning is the
// consuming flow's surface (LifecycleWarning), not a gate.
func TestValidateAdoptionBeforeFetchDeprecatedAdoptable(t *testing.T) {
	md := sampleMetadata()
	md.Lifecycle.State = LifecycleStateDeprecated
	md.Lifecycle.RemovalDate = "2027-01-01T00:00:00Z"

	result := ValidateAdoptionBeforeFetch(md, []int{1}, "5.2.3")

	if !result.Valid || !result.Adoptable {
		t.Errorf("Valid/Adoptable = %v/%v, want true for a deprecated release", result.Valid, result.Adoptable)
	}
	if result.Lifecycle.State != LifecycleStateDeprecated || result.Lifecycle.RemovalDate != "2027-01-01T00:00:00Z" {
		t.Errorf("Lifecycle = %+v, want the declared deprecated state recorded", result.Lifecycle)
	}
}

// ── Pre-fetch phase: compatibility ───────────────────────────────────

// TestValidateAdoptionBeforeFetchCompatibilityFailure asserts a
// compatibility failure fails the pre-fetch phase with the aggregated
// actionable compatibility messages — and that the post-fetch phase
// then refuses to evaluate trust: an aborted adoption can never become
// Valid, and a compatibility failure means zero fetches (the pinned
// adoption order).
func TestValidateAdoptionBeforeFetchCompatibilityFailure(t *testing.T) {
	md := sampleMetadata()
	md.ContractVersion = "2.0.0"

	before := ValidateAdoptionBeforeFetch(md, []int{1}, "5.2.3")
	if before.Valid {
		t.Fatal("Valid = true, want false (unsupported contract major)")
	}
	if !before.Adoptable {
		t.Error("Adoptable = false, want true (the lifecycle gate passed; compatibility failed)")
	}
	if !hasMessage(before.Errors, "does not support") || !hasMessage(before.Errors, "Migrate the standard") {
		t.Errorf("Errors = %v, want the actionable incompatible-major messages", before.Errors)
	}

	// The post-fetch phase must not evaluate trust over an aborted
	// adoption: content is never fetched after a compatibility failure.
	after := ValidateAdoptionAfterFetch(md, testContent(), nil, before)
	if after.Valid {
		t.Error("post-fetch Valid = true, want false — the adoption aborted at the compatibility gate")
	}
	if after.Trust != nil {
		t.Error("Trust != nil after an aborted adoption, want nil — trust is not evaluated when compatibility failed")
	}
	if !hasMessage(after.Errors, "does not support") {
		t.Errorf("post-fetch Errors = %v, want the compatibility rejection preserved", after.Errors)
	}
}

// ── Post-fetch phase: trust + combined record ────────────────────────

// TestValidateAdoptionAfterFetchBothPass asserts the full adoption: the
// pre-fetch phase passes, the trust phase verifies every dimension over
// the fetched content, the combined record is Valid with both embedded
// results, no errors, and the record self-identifies the adoption.
func TestValidateAdoptionAfterFetchBothPass(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv)
	anchors := testAnchors(t, pub)

	before := ValidateAdoptionBeforeFetch(md, []int{1}, "5.1.0")
	if !before.Valid {
		t.Fatalf("pre-fetch Valid = false, want true; errors: %v", before.Errors)
	}

	after := ValidateAdoptionAfterFetch(md, content, anchors, before)
	if !after.Valid {
		t.Fatalf("Valid = false, want true; errors: %v", after.Errors)
	}
	if after.Trust == nil || !after.Trust.Valid {
		t.Fatalf("Trust = %+v, want a valid embedded trust result", after.Trust)
	}
	if !after.Trust.IntegrityVerified || !after.Trust.AttestationVerified || !after.Trust.AnchorMatched {
		t.Errorf("Trust = %+v, want every trust dimension verified", after.Trust)
	}
	if !after.Compatibility.Valid {
		t.Errorf("Compatibility = %+v, want valid", after.Compatibility)
	}
	if len(after.Errors) != 0 {
		t.Errorf("Errors = %v, want none", after.Errors)
	}
	if after.ID != md.ID || after.Version != md.Version {
		t.Errorf("result identity = %s %s, want %s %s", after.ID, after.Version, md.ID, md.Version)
	}
}

// TestValidateAdoptionAfterFetchTrustFailure asserts a trust failure
// fails the combined record with the aggregated actionable trust
// messages: a failure in either validation aborts the adoption
// (DoD TS-014-04-03).
func TestValidateAdoptionAfterFetchTrustFailure(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv)
	anchors := testAnchors(t, pub)

	before := ValidateAdoptionBeforeFetch(md, []int{1}, "5.1.0")
	if !before.Valid {
		t.Fatalf("pre-fetch Valid = false, want true; errors: %v", before.Errors)
	}

	// The server content is not what the release claims: integrity must
	// fail.
	after := ValidateAdoptionAfterFetch(md, []byte("tampered content — not what the release claims"), anchors, before)
	if after.Valid {
		t.Fatal("Valid = true with tampered content, want false")
	}
	if after.Trust == nil || after.Trust.Valid {
		t.Fatalf("Trust = %+v, want a failed embedded trust result", after.Trust)
	}
	if after.Trust.IntegrityVerified {
		t.Error("IntegrityVerified = true with tampered content, want false")
	}
	if !hasMessage(after.Errors, "content digest mismatch") || !hasMessage(after.Errors, "re-fetch") {
		t.Errorf("Errors = %v, want the actionable integrity mismatch messages", after.Errors)
	}
}

// TestValidateAdoptionResultRecordHelpers asserts the extraction helpers
// expose the embedded results in the shape the installed-standard record
// persists (T-009), and the completed record is JSON-serializable with
// its self-identifying fields and embedded results.
func TestValidateAdoptionResultRecordHelpers(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv)
	anchors := testAnchors(t, pub)

	before := ValidateAdoptionBeforeFetch(md, []int{1}, "5.1.0")
	after := ValidateAdoptionAfterFetch(md, content, anchors, before)

	compatPtr := after.CompatibilityRecord()
	if compatPtr == nil || !reflect.DeepEqual(*compatPtr, after.Compatibility) {
		t.Errorf("CompatibilityRecord() = %+v, want the embedded compatibility result", compatPtr)
	}
	trustPtr := after.TrustRecord()
	if trustPtr == nil || !reflect.DeepEqual(*trustPtr, *after.Trust) {
		t.Errorf("TrustRecord() = %+v, want the embedded trust result", trustPtr)
	}

	raw, err := json.Marshal(after)
	if err != nil {
		t.Fatalf("marshal adoption result: %v", err)
	}
	var decoded struct {
		ID            string          `json:"id"`
		Version       string          `json:"version"`
		Lifecycle     json.RawMessage `json:"lifecycle"`
		Adoptable     bool            `json:"adoptable"`
		Compatibility json.RawMessage `json:"compatibility"`
		Trust         json.RawMessage `json:"trust"`
		Valid         bool            `json:"valid"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode adoption result: %v", err)
	}
	if decoded.ID != md.ID || decoded.Version != md.Version || !decoded.Adoptable || !decoded.Valid {
		t.Errorf("decoded record = %+v, want identity %s %s, adoptable and valid", decoded, md.ID, md.Version)
	}
	for _, section := range []struct {
		name string
		data json.RawMessage
	}{{"lifecycle", decoded.Lifecycle}, {"compatibility", decoded.Compatibility}, {"trust", decoded.Trust}} {
		if len(section.data) == 0 || string(section.data) == "null" {
			t.Errorf("decoded record is missing the %s section", section.name)
		}
	}
}

// ── Compatibility matrix reader ──────────────────────────────────────

// writeTestMatrix writes the given content as a compatibility matrix
// file and returns its path.
func writeTestMatrix(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "compatibility-matrix.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write matrix fixture: %v", err)
	}
	return path
}

// TestLoadCompatibilityMatrixValid asserts the corpus matrix record
// (docs/specification-corpus/compatibility-matrix.json) loads with its
// recorded facts — supported contract majors and contract version.
func TestLoadCompatibilityMatrixValid(t *testing.T) {
	path := writeTestMatrix(t, `{
  "document_id": "compatibility-matrix",
  "contract_version": "1.0.0",
  "supported_majors": [1],
  "maintained_under": "maintained as part of the corpus release process"
}`)

	matrix, err := LoadCompatibilityMatrix(path)
	if err != nil {
		t.Fatalf("LoadCompatibilityMatrix: %v", err)
	}
	if matrix.DocumentID != MatrixDocumentID {
		t.Errorf("DocumentID = %q, want %q", matrix.DocumentID, MatrixDocumentID)
	}
	if matrix.ContractVersion != "1.0.0" {
		t.Errorf("ContractVersion = %q, want 1.0.0", matrix.ContractVersion)
	}
	if len(matrix.SupportedContractMajors) != 1 || matrix.SupportedContractMajors[0] != 1 {
		t.Errorf("SupportedContractMajors = %v, want [1]", matrix.SupportedContractMajors)
	}
}

// TestLoadCompatibilityMatrixMultipleMajors asserts a multi-major set
// (the ADR-024 §3.4 deprecation window: {current, current-1}) is read
// faithfully — the engine reads the recorded set, it does not hardcode
// it.
func TestLoadCompatibilityMatrixMultipleMajors(t *testing.T) {
	path := writeTestMatrix(t, `{"document_id":"compatibility-matrix","contract_version":"2.0.0","supported_majors":[1,2]}`)

	matrix, err := LoadCompatibilityMatrix(path)
	if err != nil {
		t.Fatalf("LoadCompatibilityMatrix: %v", err)
	}
	if len(matrix.SupportedContractMajors) != 2 || matrix.SupportedContractMajors[0] != 1 || matrix.SupportedContractMajors[1] != 2 {
		t.Errorf("SupportedContractMajors = %v, want [1 2]", matrix.SupportedContractMajors)
	}
}

// TestLoadCompatibilityMatrixMissing asserts a missing matrix file is an
// actionable wrapped ErrCompatibilityMatrixNotFound — supported majors
// are never silently defaulted (PM binding decision 3).
func TestLoadCompatibilityMatrixMissing(t *testing.T) {
	_, err := LoadCompatibilityMatrix(filepath.Join(t.TempDir(), "no-matrix.json"))
	if !errors.Is(err, ErrCompatibilityMatrixNotFound) {
		t.Fatalf("err = %v, want wrapped %v", err, ErrCompatibilityMatrixNotFound)
	}
}

// TestLoadCompatibilityMatrixCorrupt asserts a matrix file that is not
// decodable JSON is an actionable error naming the file and the fix.
func TestLoadCompatibilityMatrixCorrupt(t *testing.T) {
	path := writeTestMatrix(t, "this is not json {")

	_, err := LoadCompatibilityMatrix(path)
	if err == nil {
		t.Fatal("LoadCompatibilityMatrix accepted corrupt JSON, want error")
	}
	if !strings.Contains(err.Error(), "not decodable JSON") || !strings.Contains(err.Error(), path) {
		t.Errorf("err = %v, want an actionable decode error naming the file", err)
	}
}

// TestLoadCompatibilityMatrixUnknownField asserts the pinned record
// shape rejects unknown fields — a broken record is never partially
// applied.
func TestLoadCompatibilityMatrixUnknownField(t *testing.T) {
	path := writeTestMatrix(t, `{"document_id":"compatibility-matrix","contract_version":"1.0.0","supported_majors":[1],"extra":true}`)

	_, err := LoadCompatibilityMatrix(path)
	if err == nil {
		t.Fatal("LoadCompatibilityMatrix accepted an unknown field, want error")
	}
	if !strings.Contains(err.Error(), "not decodable JSON") {
		t.Errorf("err = %v, want the unknown-field rejection", err)
	}
}

// TestLoadCompatibilityMatrixWrongDocumentID asserts a file that is not
// the compatibility matrix record is rejected with an actionable error
// naming the expected identity.
func TestLoadCompatibilityMatrixWrongDocumentID(t *testing.T) {
	path := writeTestMatrix(t, `{"document_id":"version-line","contract_version":"1.0.0","supported_majors":[1]}`)

	_, err := LoadCompatibilityMatrix(path)
	if err == nil {
		t.Fatal("LoadCompatibilityMatrix accepted a wrong document_id, want error")
	}
	if !strings.Contains(err.Error(), MatrixDocumentID) || !strings.Contains(err.Error(), "document_id") {
		t.Errorf("err = %v, want the document_id rejection naming %q", err, MatrixDocumentID)
	}
}

// TestLoadCompatibilityMatrixInvalidRecords asserts the record's
// required fields are enforced: a missing contract version, an empty,
// duplicate, or non-positive supported major set all fail with
// actionable errors.
func TestLoadCompatibilityMatrixInvalidRecords(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "missing contract version",
			content: `{"document_id":"compatibility-matrix","supported_majors":[1]}`,
			want:    "contract_version",
		},
		{
			name:    "whitespace-only contract version",
			content: `{"document_id":"compatibility-matrix","contract_version":"   ","supported_majors":[1]}`,
			want:    "contract_version",
		},
		{
			name:    "empty supported majors",
			content: `{"document_id":"compatibility-matrix","contract_version":"1.0.0","supported_majors":[]}`,
			want:    "supported_majors",
		},
		{
			name:    "zero major",
			content: `{"document_id":"compatibility-matrix","contract_version":"1.0.0","supported_majors":[0]}`,
			want:    "contract majors are >= 1",
		},
		{
			name:    "negative major",
			content: `{"document_id":"compatibility-matrix","contract_version":"1.0.0","supported_majors":[-1]}`,
			want:    "contract majors are >= 1",
		},
		{
			name:    "duplicate major",
			content: `{"document_id":"compatibility-matrix","contract_version":"1.0.0","supported_majors":[1,1]}`,
			want:    "more than once",
		},
		{
			name:    "trailing content",
			content: `{"document_id":"compatibility-matrix","contract_version":"1.0.0","supported_majors":[1]} trailing`,
			want:    "unexpected content",
		},
		{
			name:    "trailing array bracket",
			content: `{"document_id":"compatibility-matrix","contract_version":"1.0.0","supported_majors":[1]}]`,
			want:    "unexpected content",
		},
		{
			name:    "trailing object brace",
			content: `{"document_id":"compatibility-matrix","contract_version":"1.0.0","supported_majors":[1]}}`,
			want:    "unexpected content",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTestMatrix(t, tc.content)
			_, err := LoadCompatibilityMatrix(path)
			if err == nil {
				t.Fatal("LoadCompatibilityMatrix accepted the invalid record, want error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want a message containing %q", err, tc.want)
			}
		})
	}
}

// TestLoadCompatibilityMatrixOversize asserts a matrix file exceeding the
// size cap is rejected with a precise, actionable error — a file beyond
// the cap is a broken artifact, never read unbounded.
func TestLoadCompatibilityMatrixOversize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversize-matrix.json")
	if err := os.WriteFile(path, make([]byte, MaxCompatibilityMatrixSize+1), 0o644); err != nil {
		t.Fatalf("write oversize matrix fixture: %v", err)
	}

	_, err := LoadCompatibilityMatrix(path)
	if err == nil {
		t.Fatal("LoadCompatibilityMatrix accepted an oversize file, want error")
	}
	if !strings.Contains(err.Error(), "size cap") || !strings.Contains(err.Error(), path) {
		t.Errorf("err = %v, want an actionable size-cap error naming the file", err)
	}
}

// TestResolveCompatibilityMatrixPath asserts the resolution order:
// explicit path, then the ANVIL_COMPATIBILITY_MATRIX environment
// variable, then the documented repo-relative default (mirrors the
// trust anchors convention).
func TestResolveCompatibilityMatrixPath(t *testing.T) {
	t.Run("explicit path wins", func(t *testing.T) {
		path, err := ResolveCompatibilityMatrixPath("/explicit/matrix.json", func(string) string { return "/env/matrix.json" })
		if err != nil {
			t.Fatalf("ResolveCompatibilityMatrixPath: %v", err)
		}
		if path != "/explicit/matrix.json" {
			t.Errorf("path = %q, want the explicit path", path)
		}
	})

	t.Run("environment variable", func(t *testing.T) {
		path, err := ResolveCompatibilityMatrixPath("", func(key string) string {
			if key == EnvCompatibilityMatrix {
				return "/env/matrix.json"
			}
			return ""
		})
		if err != nil {
			t.Fatalf("ResolveCompatibilityMatrixPath: %v", err)
		}
		if path != "/env/matrix.json" {
			t.Errorf("path = %q, want the environment path", path)
		}
	})

	t.Run("documented default", func(t *testing.T) {
		path, err := ResolveCompatibilityMatrixPath("", func(string) string { return "" })
		if err != nil {
			t.Fatalf("ResolveCompatibilityMatrixPath: %v", err)
		}
		if path != DefaultCompatibilityMatrixRelativePath {
			t.Errorf("path = %q, want %q", path, DefaultCompatibilityMatrixRelativePath)
		}
	})
}
