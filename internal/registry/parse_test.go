package registry

import (
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validMetadataDoc returns a minimal valid registry metadata document:
// every required section populated with schema-conformant values, no
// optional fields, published state (no advisory warning).
func validMetadataDoc() string {
	return `{
		"$schema": "urn:anvil:spec:registry-metadata:1.0.0",
		"id": "anvil-standard-laravel",
		"version": "1.2.3",
		"contractVersion": "1.0.0",
		"capability": {
			"frameworkVersion": ["5.1.0", "5.2.0"]
		},
		"distribution": {
			"type": "github-releases",
			"location": "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/anvil-standard-laravel.tar.gz"
		},
		"lifecycle": {
			"state": "published"
		},
		"trust": {
			"contentDigests": [
				{
					"algorithm": "sha-256",
					"encoding": "base16",
					"digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
				}
			],
			"attestation": {
				"algorithm": "ed25519",
				"signature": "c2lnbmF0dXJlLXZhbHVlLWJhc2U2NC1lbmNvZGVkLWRlbW8tc2lnbmF0dXJlLTEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5",
				"publicKey": "cHVibGljLWtleS1iYXNlNjQtZW5jb2RlZC1kZW1vLXB1YmxpYy1rZXktMTIzNDU2Nzg5MA=="
			}
		}
	}`
}

// docMap returns the valid base document as a generic map for mutation.
func docMap() map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(validMetadataDoc()), &m); err != nil {
		panic("test document is not valid JSON: " + err.Error())
	}
	return m
}

// docJSON serializes a mutated document map.
func docJSON(t *testing.T, m map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("re-serialize test document: %v", err)
	}
	return string(raw)
}

// requireParseError asserts the document is rejected and returns the
// aggregated parse error.
func requireParseError(t *testing.T, doc string) *ParseError {
	t.Helper()
	res, err := Parse([]byte(doc))
	if err == nil {
		t.Fatal("Parse succeeded, want rejection")
	}
	if res != nil {
		t.Fatal("Parse returned a result alongside an error")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("error is %T, want *ParseError: %v", err, err)
	}
	return pe
}

// assertValidationError asserts the rejection list contains an error for
// the given field and rule.
func assertValidationError(t *testing.T, pe *ParseError, field, rule string) {
	t.Helper()
	for _, ve := range pe.Errors {
		if ve.Field == field && ve.Rule == rule {
			return
		}
	}
	t.Errorf("no validation error for %s (rule %s); got:\n%s", field, rule, pe.Error())
}

// assertNoValidationError asserts the rejection list contains no error for
// the given field and rule.
func assertNoValidationError(t *testing.T, pe *ParseError, field, rule string) {
	t.Helper()
	for _, ve := range pe.Errors {
		if ve.Field == field && ve.Rule == rule {
			t.Errorf("unexpected validation error for %s (rule %s): %s", field, rule, ve.Message)
		}
	}
}

// requireParseSuccess asserts the document parses and validates, returning
// the result.
func requireParseSuccess(t *testing.T, doc string) *ParseResult {
	t.Helper()
	res, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse rejected a valid document: %v", err)
	}
	if res == nil || res.Metadata == nil {
		t.Fatal("Parse returned no metadata")
	}
	return res
}

// assertWarning asserts the result carries exactly the expected advisory
// warning (field + rule) and nothing else.
func assertWarning(t *testing.T, res *ParseResult, field, rule string) {
	t.Helper()
	if len(res.Warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one (%s, %s)", res.Warnings, field, rule)
	}
	if res.Warnings[0].Field != field || res.Warnings[0].Rule != rule {
		t.Errorf("warning = %+v, want field %q rule %q", res.Warnings[0], field, rule)
	}
}

// assertNoWarnings asserts the result carries no advisory warnings.
func assertNoWarnings(t *testing.T, res *ParseResult) {
	t.Helper()
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", res.Warnings)
	}
}

// setPath sets a nested value in a copy of the valid document, supporting
// array indexes in the path (for example "trust.contentDigests[0].digest"),
// and returns the serialized document. Creating any missing intermediate
// objects/arrays follows the shape of the base document.
func setPath(t *testing.T, path string, value any) string {
	t.Helper()
	m := docMap()
	parts := strings.Split(path, ".")
	var cur any = m
	for i, part := range parts {
		last := i == len(parts)-1
		key, index := splitPathPart(part)
		switch node := cur.(type) {
		case map[string]any:
			if last {
				if index >= 0 {
					arr, ok := node[key].([]any)
					if !ok {
						arr = []any{}
						node[key] = arr
					}
					for len(arr) <= index {
						arr = append(arr, map[string]any{})
					}
					arr[index] = value
					node[key] = arr
					return docJSON(t, m)
				}
				node[key] = value
				return docJSON(t, m)
			}
			if index >= 0 {
				arr, ok := node[key].([]any)
				if !ok {
					arr = []any{}
					node[key] = arr
				}
				for len(arr) <= index {
					arr = append(arr, map[string]any{})
				}
				node[key] = arr
				cur = arr[index]
			} else {
				next, ok := node[key].(map[string]any)
				if !ok {
					next = map[string]any{}
					node[key] = next
				}
				cur = next
			}
		case []any:
			if index < 0 {
				t.Fatalf("path part %q in %q is not an array index", part, path)
			}
			for len(node) <= index {
				node = append(node, map[string]any{})
			}
			cur = node[index]
		default:
			t.Fatalf("cannot descend through %q in %q: %T", part, path, cur)
		}
	}
	t.Fatalf("path %q did not terminate", path)
	return ""
}

// removePath deletes a nested value from a copy of the valid document and
// returns the serialized document. The field must exist in the base
// document (or be creatable by setPath).
func removePath(t *testing.T, path string) string {
	t.Helper()
	m := docMap()
	parts := strings.Split(path, ".")
	var cur any = m
	for i, part := range parts {
		last := i == len(parts)-1
		key, index := splitPathPart(part)
		switch node := cur.(type) {
		case map[string]any:
			if last {
				delete(node, key)
				return docJSON(t, m)
			}
			if index >= 0 {
				arr, ok := node[key].([]any)
				if !ok {
					t.Fatalf("path %q: %q is not an array", path, key)
				}
				if index >= len(arr) {
					t.Fatalf("path %q: index %d out of range", path, index)
				}
				cur = arr[index]
			} else {
				next, ok := node[key].(map[string]any)
				if !ok {
					t.Fatalf("path %q: %q is not an object", path, key)
				}
				cur = next
			}
		default:
			t.Fatalf("cannot descend through %q in %q: %T", part, path, cur)
		}
	}
	t.Fatalf("path %q did not terminate", path)
	return ""
}

// splitPathPart splits a path part like "contentDigests[0]" into its key
// and array index (-1 when the part is not indexed).
func splitPathPart(part string) (string, int) {
	open := strings.IndexByte(part, '[')
	if open < 0 || !strings.HasSuffix(part, "]") {
		return part, -1
	}
	index := -1
	_, err := fmt.Sscanf(part[open+1:len(part)-1], "%d", &index)
	if err != nil {
		return part, -1
	}
	return part[:open], index
}

// TestParseValidDocument asserts a minimal valid document parses and
// validates without errors and without warnings (DoD: valid metadata
// parses and validates without errors).
func TestParseValidDocument(t *testing.T) {
	res := requireParseSuccess(t, validMetadataDoc())
	assertNoWarnings(t, res)

	md := res.Metadata
	if md.Schema != SchemaID {
		t.Errorf("$schema = %q, want %q", md.Schema, SchemaID)
	}
	if md.ID != "anvil-standard-laravel" {
		t.Errorf("id = %q, want anvil-standard-laravel", md.ID)
	}
	if md.Version != "1.2.3" || md.ContractVersion != "1.0.0" {
		t.Errorf("version/contractVersion = %q/%q", md.Version, md.ContractVersion)
	}
	if len(md.Capability.FrameworkVersion) != 2 {
		t.Errorf("frameworkVersion = %v, want 2 entries", md.Capability.FrameworkVersion)
	}
	if md.Distribution.Type != DistributionTypeGitHubReleases {
		t.Errorf("distribution.type = %q", md.Distribution.Type)
	}
	if md.Lifecycle.State != LifecycleStatePublished {
		t.Errorf("lifecycle.state = %q", md.Lifecycle.State)
	}
	if len(md.Trust.ContentDigests) != 1 {
		t.Fatalf("contentDigests = %v, want 1 entry", md.Trust.ContentDigests)
	}
	if md.Trust.ContentDigests[0].Algorithm != DigestAlgorithmSHA256 ||
		md.Trust.ContentDigests[0].Encoding != DigestEncodingBase16 ||
		md.Trust.ContentDigests[0].Digest != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("contentDigests[0] = %+v", md.Trust.ContentDigests[0])
	}
	if md.Trust.Attestation.Algorithm != AttestationAlgorithmEd25519 ||
		md.Trust.Attestation.Signature == "" || md.Trust.Attestation.PublicKey == "" {
		t.Errorf("attestation = %+v", md.Trust.Attestation)
	}
}

// TestParseValidDocumentOptionalFields asserts a document carrying every
// optional field — annotations, deprecated state with a removal date, and
// multi-encoding digests (different encodings of the same digest, all-match
// semantics) — parses without errors and without warnings.
func TestParseValidDocumentOptionalFields(t *testing.T) {
	doc := `{
		"$schema": "urn:anvil:spec:registry-metadata:1.0.0",
		"title": "Anvil Standard Laravel 1.2.3",
		"description": "A valid document exercising every optional field.",
		"id": "anvil-standard-laravel",
		"version": "1.2.3",
		"contractVersion": "1.0.0",
		"capability": {
			"frameworkVersion": ["5.1.0", "5.2.0", "5.3.0"]
		},
		"distribution": {
			"type": "github-releases",
			"location": "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/anvil-standard-laravel.tar.gz"
		},
		"lifecycle": {
			"state": "deprecated",
			"removalDate": "2027-01-31T00:00:00Z"
		},
		"trust": {
			"contentDigests": [
				{
					"algorithm": "sha-256",
					"encoding": "base64",
					"digest": "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="
				},
				{
					"algorithm": "sha-256",
					"encoding": "base16",
					"digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
				}
			],
			"attestation": {
				"algorithm": "ed25519",
				"signature": "c2lnbmF0dXJlLXZhbHVlLWJhc2U2NC1lbmNvZGVkLWRlbW8tc2lnbmF0dXJlLTEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5",
				"publicKey": "cHVibGljLWtleS1iYXNlNjQtZW5jb2RlZC1kZW1vLXB1YmxpYy1rZXktMTIzNDU2Nzg5MA=="
			}
		}
	}`
	res := requireParseSuccess(t, doc)
	assertNoWarnings(t, res)
	if res.Metadata.Title != "Anvil Standard Laravel 1.2.3" || res.Metadata.Description == "" {
		t.Errorf("annotations not parsed: %+v", res.Metadata)
	}
	if res.Metadata.Lifecycle.State != LifecycleStateDeprecated || res.Metadata.Lifecycle.RemovalDate != "2027-01-31T00:00:00Z" {
		t.Errorf("lifecycle = %+v", res.Metadata.Lifecycle)
	}
	if len(res.Metadata.Trust.ContentDigests) != 2 {
		t.Errorf("contentDigests = %v, want 2 entries", res.Metadata.Trust.ContentDigests)
	}
}

// TestParseDeprecatedWithoutRemovalDateWarns asserts a deprecated release
// without a removal date parses but carries the advisory warning (PM
// decision D-03: the field SHOULD be present once announced — warning, not
// error).
func TestParseDeprecatedWithoutRemovalDateWarns(t *testing.T) {
	m := docMap()
	life := m["lifecycle"].(map[string]any)
	life["state"] = LifecycleStateDeprecated

	res := requireParseSuccess(t, docJSON(t, m))
	assertWarning(t, res, "lifecycle.removalDate", "advisory")
}

// TestParseMalformedDocument asserts not-decodable JSON and non-object
// roots are rejected as malformed with an actionable error (DoD: malformed
// metadata is rejected with an actionable error identifying the problem).
func TestParseMalformedDocument(t *testing.T) {
	cases := []struct {
		name    string
		doc     string
		rule    string
		message string
	}{
		{name: "unterminated object", doc: `{"id": "anvil"`, rule: "json", message: "not decodable JSON"},
		{name: "array root", doc: `[1, 2, 3]`, rule: "type", message: "must be a JSON object, found array"},
		{name: "string root", doc: `"a document"`, rule: "type", message: "must be a JSON object, found string"},
		{name: "number root", doc: `42`, rule: "type", message: "must be a JSON object, found number"},
		{name: "null root", doc: `null`, rule: "type", message: "must be a JSON object, found null"},
		{name: "trailing garbage", doc: `{"id": "anvil"} trailing`, rule: "json", message: "not decodable JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pe := requireParseError(t, tc.doc)
			assertValidationError(t, pe, "document", tc.rule)
			for _, ve := range pe.Errors {
				if ve.Kind != ValidationErrorKindMalformed {
					t.Errorf("error kind = %s, want malformed: %s", ve.Kind, ve.Error())
				}
				if !strings.Contains(ve.Message, tc.message) {
					t.Errorf("message %q does not contain %q", ve.Message, tc.message)
				}
			}
		})
	}
}

// TestParseWrongTypes asserts a present field of the wrong type is
// rejected as malformed (wrong types are malformed per the work item) with
// an error naming the field path and the found type.
func TestParseWrongTypes(t *testing.T) {
	cases := []struct {
		name  string
		field string
		value any
	}{
		{name: "id number", field: "id", value: 123},
		{name: "version null", field: "version", value: nil},
		{name: "contractVersion bool", field: "contractVersion", value: true},
		{name: "schema number", field: "$schema", value: 5},
		{name: "title number", field: "title", value: 5},
		{name: "description array", field: "description", value: []any{"x"}},
		{name: "capability string", field: "capability", value: "x"},
		{name: "distribution array", field: "distribution", value: []any{}},
		{name: "lifecycle number", field: "lifecycle", value: 5},
		{name: "trust string", field: "trust", value: "x"},
		{name: "frameworkVersion string", field: "capability.frameworkVersion", value: "5.1.0"},
		{name: "frameworkVersion item number", field: "capability.frameworkVersion[0]", value: 5},
		{name: "distribution type number", field: "distribution.type", value: 1},
		{name: "location null", field: "distribution.location", value: nil},
		{name: "state array", field: "lifecycle.state", value: []any{}},
		{name: "removalDate number", field: "lifecycle.removalDate", value: 5},
		{name: "contentDigests object", field: "trust.contentDigests", value: map[string]any{}},
		{name: "digest algorithm number", field: "trust.contentDigests[0].algorithm", value: 5},
		{name: "digest digest object", field: "trust.contentDigests[0].digest", value: map[string]any{}},
		{name: "attestation array", field: "trust.attestation", value: []any{}},
		{name: "signature number", field: "trust.attestation.signature", value: 5},
		{name: "publicKey null", field: "trust.attestation.publicKey", value: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := setPath(t, tc.field, tc.value)
			pe := requireParseError(t, doc)
			assertValidationError(t, pe, tc.field, "type")
			for _, ve := range pe.Errors {
				if ve.Kind != ValidationErrorKindMalformed {
					t.Errorf("error kind = %s, want malformed: %s", ve.Kind, ve.Error())
				}
			}
		})
	}
}

// TestParseMissingRequiredFields asserts every field the schema requires is
// enforced at parse time: top-level sections and nested required fields
// (TS-014-01-01 required rules).
func TestParseMissingRequiredFields(t *testing.T) {
	cases := []struct {
		name  string
		field string
	}{
		{name: "id", field: "id"},
		{name: "version", field: "version"},
		{name: "contractVersion", field: "contractVersion"},
		{name: "capability", field: "capability"},
		{name: "distribution", field: "distribution"},
		{name: "lifecycle", field: "lifecycle"},
		{name: "trust", field: "trust"},
		{name: "frameworkVersion", field: "capability.frameworkVersion"},
		{name: "distribution type", field: "distribution.type"},
		{name: "distribution location", field: "distribution.location"},
		{name: "lifecycle state", field: "lifecycle.state"},
		{name: "contentDigests", field: "trust.contentDigests"},
		{name: "attestation", field: "trust.attestation"},
		{name: "digest algorithm", field: "trust.contentDigests[0].algorithm"},
		{name: "digest encoding", field: "trust.contentDigests[0].encoding"},
		{name: "digest digest", field: "trust.contentDigests[0].digest"},
		{name: "attestation algorithm", field: "trust.attestation.algorithm"},
		{name: "attestation signature", field: "trust.attestation.signature"},
		{name: "attestation publicKey", field: "trust.attestation.publicKey"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pe := requireParseError(t, removePath(t, tc.field))
			assertValidationError(t, pe, tc.field, "required")
		})
	}
}

// TestParseUnknownFields asserts additionalProperties: false is enforced at
// every object level: unknown fields are rejected and the error names the
// undeclared field (TS-014-01-01 additionalProperties; the format stays
// runtime-agnostic — no runtime fields leak in).
func TestParseUnknownFields(t *testing.T) {
	cases := []struct {
		name  string
		field string
	}{
		{name: "root runtime field", field: "golangVersion"},
		{name: "trust keyId", field: "trust.keyId"},
		{name: "capability extra", field: "capability.extra"},
		{name: "distribution extra", field: "distribution.extra"},
		{name: "lifecycle extra", field: "lifecycle.extra"},
		{name: "attestation extra", field: "trust.attestation.extra"},
		{name: "digest extra", field: "trust.contentDigests[0].extra"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := setPath(t, tc.field, "boom")
			pe := requireParseError(t, doc)
			assertValidationError(t, pe, tc.field, "additionalProperties")
		})
	}
}

// TestParseIDConstraints asserts the identity pattern and length bound
// (^[a-z0-9][a-z0-9-]*$, maxLength 64 — the corpus id convention; traversal
// separators and leading uppercase are rejected).
func TestParseIDConstraints(t *testing.T) {
	valid := []string{"a", "anvil-standard-laravel", strings.Repeat("a", 64), "anvil-"}
	for _, id := range valid {
		m := docMap()
		m["id"] = id
		requireParseSuccess(t, docJSON(t, m))
	}

	cases := []struct {
		name string
		id   string
		rule string
	}{
		{name: "uppercase", id: "Anvil-Standard", rule: "pattern"},
		{name: "underscore", id: "anvil_standard", rule: "pattern"},
		{name: "dot", id: "anvil.standard", rule: "pattern"},
		{name: "slash", id: "anvil/standard", rule: "pattern"},
		{name: "leading hyphen", id: "-anvil", rule: "pattern"},
		{name: "too long", id: strings.Repeat("a", 65), rule: "maxLength"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := docMap()
			m["id"] = tc.id
			pe := requireParseError(t, docJSON(t, m))
			assertValidationError(t, pe, "id", tc.rule)
		})
	}
}

// TestParseVersionConstraints asserts the semver pattern without leading
// zeros and the length bound (^[0-9]+\.\.[0-9]+$ semantics of
// ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$, maxLength 64).
func TestParseVersionConstraints(t *testing.T) {
	for _, version := range []string{"0.0.0", "1.2.3", "10.20.30"} {
		m := docMap()
		m["version"] = version
		requireParseSuccess(t, docJSON(t, m))
	}

	cases := []struct {
		name    string
		version string
	}{
		{name: "leading zero major", version: "01.2.3"},
		{name: "leading zero minor", version: "1.02.3"},
		{name: "two segments", version: "1.2"},
		{name: "four segments", version: "1.2.3.4"},
		{name: "v prefix", version: "v1.2.3"},
		{name: "prerelease", version: "1.2.3-beta"},
		{name: "trailing space", version: "1.2.3 "},
		{name: "too long", version: strings.Repeat("1.", 32) + "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := docMap()
			m["version"] = tc.version
			pe := requireParseError(t, docJSON(t, m))
			assertValidationError(t, pe, "version", "pattern")
		})
	}
}

// TestParseContractVersionConstraints asserts contractVersion follows the
// same semver pattern as version (ADR-024 §3.1).
func TestParseContractVersionConstraints(t *testing.T) {
	m := docMap()
	m["contractVersion"] = "2.0"
	pe := requireParseError(t, docJSON(t, m))
	assertValidationError(t, pe, "contractVersion", "pattern")
}

// TestParseFrameworkVersionConstraints asserts the capability declaration:
// at least one framework version, all unique, each in the command-contract
// convention (^[0-9]+\.[0-9]+\.[0-9]+$).
func TestParseFrameworkVersionConstraints(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		m := docMap()
		m["capability"].(map[string]any)["frameworkVersion"] = []any{}
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "capability.frameworkVersion", "minItems")
	})
	t.Run("duplicate", func(t *testing.T) {
		m := docMap()
		m["capability"].(map[string]any)["frameworkVersion"] = []any{"5.1.0", "5.1.0"}
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "capability.frameworkVersion[1]", "uniqueItems")
	})
	t.Run("two segments", func(t *testing.T) {
		m := docMap()
		m["capability"].(map[string]any)["frameworkVersion"] = []any{"5.1"}
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "capability.frameworkVersion[0]", "pattern")
	})
	t.Run("four segments", func(t *testing.T) {
		m := docMap()
		m["capability"].(map[string]any)["frameworkVersion"] = []any{"5.1.0.1"}
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "capability.frameworkVersion[0]", "pattern")
	})
}

// TestParseDistributionTypeEnum asserts the distribution channel pattern is
// exactly github-releases (ADR-030 §3, §5).
func TestParseDistributionTypeEnum(t *testing.T) {
	for _, distType := range []string{"gitlab-releases", "GitHub-Releases", "github_releases"} {
		m := docMap()
		m["distribution"].(map[string]any)["type"] = distType
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "distribution.type", "enum")
	}
}

// TestParseLocationHTTPSOnly asserts the distribution location is pinned to
// https — the schema pattern plus the strict URL check (the enforceable
// constraint of §4.5; ADR-030 §3): http, ftp, file, and degenerate https
// values are rejected.
func TestParseLocationHTTPSOnly(t *testing.T) {
	cases := []struct {
		name     string
		location string
		rule     string
	}{
		{name: "http scheme", location: "http://github.com/maleolabs/x/releases/download/v1/x.tar.gz", rule: "pattern"},
		{name: "ftp scheme", location: "ftp://github.com/maleolabs/x.tar.gz", rule: "pattern"},
		{name: "file scheme", location: "file:///tmp/x.tar.gz", rule: "pattern"},
		{name: "no host", location: "https://", rule: "format"},
		{name: "empty host", location: "https:///path/x.tar.gz", rule: "format"},
		{name: "trailing space", location: "https://github.com/maleolabs/x/releases/download/v1.2.3/x.tar.gz ", rule: "format"},
		{name: "internal space", location: "https://github.com/ma leolabs/x.tar.gz", rule: "format"},
		{name: "tab", location: "https://github.com/maleolabs/x\ty.tar.gz", rule: "format"},
		{name: "newline", location: "https://github.com/maleolabs/x\n.tar.gz", rule: "format"},
		{name: "unicode space", location: "https://github.com/maleolabs/\u00a0x.tar.gz", rule: "format"},
		{name: "userinfo username only", location: "https://alice@github.com/maleolabs/x.tar.gz", rule: "format"},
		{name: "userinfo username and password", location: "https://alice:secret@github.com/maleolabs/x.tar.gz", rule: "format"},
		{name: "userinfo empty username", location: "https://:secret@github.com/maleolabs/x.tar.gz", rule: "format"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := docMap()
			m["distribution"].(map[string]any)["location"] = tc.location
			pe := requireParseError(t, docJSON(t, m))
			assertValidationError(t, pe, "distribution.location", tc.rule)
		})
	}
}

// TestParseLifecycleStateEnum asserts the lifecycle state machine values
// are exactly published, deprecated, and retired (ADR-023 §3; ADR-027 §3).
func TestParseLifecycleStateEnum(t *testing.T) {
	for _, state := range []string{"draft", "Published", "published ", "proposed"} {
		m := docMap()
		m["lifecycle"].(map[string]any)["state"] = state
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "lifecycle.state", "enum")
	}
}

// TestParseRemovalDateFormat asserts removalDate is parsed strictly as an
// RFC 3339 date-time (ISO 8601) — the strict layer for the advisory
// format: date-time annotation (TS-014-01-02).
func TestParseRemovalDateFormat(t *testing.T) {
	for _, date := range []string{
		"2027-01-31",
		"2027-13-01T00:00:00Z",
		"2027-01-31T00:00:00",
		"2027-01-31T25:00:00Z",
		"not a date",
		"2027-01-31T00:00:00+07",
	} {
		m := docMap()
		life := m["lifecycle"].(map[string]any)
		life["state"] = LifecycleStateDeprecated
		life["removalDate"] = date
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "lifecycle.removalDate", "format")
	}

	for _, date := range []string{
		"2027-01-31T00:00:00Z",
		"2027-01-31T00:00:00.123Z",
		"2027-01-31T23:59:59+07:00",
	} {
		m := docMap()
		life := m["lifecycle"].(map[string]any)
		life["state"] = LifecycleStateDeprecated
		life["removalDate"] = date
		requireParseSuccess(t, docJSON(t, m))
	}
}

// TestParseRemovalDateStateConstraint asserts the cross-field rule of the
// schema (if/then): removalDate is only allowed when lifecycle.state is
// deprecated (ADR-023 §3; ADR-027 §3; PM decision D-03).
func TestParseRemovalDateStateConstraint(t *testing.T) {
	for _, state := range []string{LifecycleStatePublished, LifecycleStateRetired} {
		t.Run(state, func(t *testing.T) {
			m := docMap()
			life := m["lifecycle"].(map[string]any)
			life["state"] = state
			life["removalDate"] = "2027-01-31T00:00:00Z"
			pe := requireParseError(t, docJSON(t, m))
			assertValidationError(t, pe, "lifecycle.removalDate", "if-then")
		})
	}
}

// TestParseContentDigestsConstraints asserts the digest array shape: at
// least one entry, unique entries, and entries as different encodings of
// the same digest value (all-match semantics, §4.7).
func TestParseContentDigestsConstraints(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		m := docMap()
		m["trust"].(map[string]any)["contentDigests"] = []any{}
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.contentDigests", "minItems")
	})
	t.Run("duplicate entry", func(t *testing.T) {
		m := docMap()
		entry := m["trust"].(map[string]any)["contentDigests"].([]any)[0]
		m["trust"].(map[string]any)["contentDigests"] = []any{entry, entry}
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.contentDigests[1]", "uniqueItems")
	})
	t.Run("different encodings allowed", func(t *testing.T) {
		m := docMap()
		m["trust"].(map[string]any)["contentDigests"] = []any{
			map[string]any{
				"algorithm": DigestAlgorithmSHA256,
				"encoding":  DigestEncodingBase16,
				"digest":    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
			map[string]any{
				"algorithm": DigestAlgorithmSHA256,
				"encoding":  DigestEncodingBase64,
				"digest":    "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=",
			},
		}
		requireParseSuccess(t, docJSON(t, m))
	})
}

// TestParseDigestAlgorithmEnum asserts the trust baseline fixes SHA-256
// (PM decision D-01): no other algorithm is accepted.
func TestParseDigestAlgorithmEnum(t *testing.T) {
	for _, algorithm := range []string{"sha-512", "md5", "sha-256 "} {
		m := docMap()
		m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)["algorithm"] = algorithm
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.contentDigests[0].algorithm", "enum")
	}
}

// TestParseDigestEncodingEnum asserts the digest encodings are exactly
// base16, base32, and base64 (artifact-manifest encoding convention).
func TestParseDigestEncodingEnum(t *testing.T) {
	for _, encoding := range []string{"base8", "hex", "base64url"} {
		m := docMap()
		m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)["encoding"] = encoding
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.contentDigests[0].encoding", "enum")
	}
}

// TestParseDigestValueBase16 asserts base16 digests are exactly 64
// lowercase hex characters (^[0-9a-f]{64}$).
func TestParseDigestValueBase16(t *testing.T) {
	valid := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	cases := []struct {
		name   string
		digest string
	}{
		{name: "uppercase hex", digest: "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855"},
		{name: "63 characters", digest: valid[1:]},
		{name: "65 characters", digest: valid + "0"},
		{name: "non-hex alphabet", digest: "zz" + valid[2:]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := docMap()
			m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)["digest"] = tc.digest
			pe := requireParseError(t, docJSON(t, m))
			assertValidationError(t, pe, "trust.contentDigests[0].digest", "pattern")
		})
	}
	t.Run("empty", func(t *testing.T) {
		m := docMap()
		m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)["digest"] = ""
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.contentDigests[0].digest", "minLength")
	})
}

// TestParseDigestValueBase32 asserts base32 digests are the strict
// RFC-4648 base32 encoding of a 32-byte digest: the schema shape pattern,
// strict decoding (padding enforced), and a decoded length of exactly 32
// bytes.
func TestParseDigestValueBase32(t *testing.T) {
	valid := "4OYMIQUY7QOBJGX36TEJS35ZEQT24QPEMSNZGTFESWMRW6CSXBKQ===="

	t.Run("valid", func(t *testing.T) {
		m := docMap()
		digest := m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)
		digest["encoding"] = DigestEncodingBase32
		digest["digest"] = valid
		requireParseSuccess(t, docJSON(t, m))
	})
	t.Run("lowercase", func(t *testing.T) {
		m := docMap()
		digest := m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)
		digest["encoding"] = DigestEncodingBase32
		digest["digest"] = strings.ToLower(valid)
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.contentDigests[0].digest", "pattern")
	})
	t.Run("missing padding", func(t *testing.T) {
		m := docMap()
		digest := m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)
		digest["encoding"] = DigestEncodingBase32
		digest["digest"] = strings.TrimRight(valid, "=")
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.contentDigests[0].digest", "digest-encoding")
	})
	t.Run("excess padding", func(t *testing.T) {
		m := docMap()
		digest := m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)
		digest["encoding"] = DigestEncodingBase32
		digest["digest"] = valid + "="
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.contentDigests[0].digest", "digest-encoding")
	})
	t.Run("decodes to 35 bytes", func(t *testing.T) {
		m := docMap()
		digest := m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)
		digest["encoding"] = DigestEncodingBase32
		digest["digest"] = base32.StdEncoding.EncodeToString(make([]byte, 35))
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.contentDigests[0].digest", "digest-length")
	})
}

// TestParseDigestValueBase64 asserts base64 digests are the strict
// RFC-4648 standard (padded) encoding of a 32-byte digest.
func TestParseDigestValueBase64(t *testing.T) {
	valid := "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="

	t.Run("valid", func(t *testing.T) {
		m := docMap()
		digest := m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)
		digest["encoding"] = DigestEncodingBase64
		digest["digest"] = valid
		requireParseSuccess(t, docJSON(t, m))
	})
	t.Run("missing padding", func(t *testing.T) {
		m := docMap()
		digest := m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)
		digest["encoding"] = DigestEncodingBase64
		digest["digest"] = strings.TrimRight(valid, "=")
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.contentDigests[0].digest", "pattern")
	})
	t.Run("non-standard alphabet", func(t *testing.T) {
		m := docMap()
		digest := m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)
		digest["encoding"] = DigestEncodingBase64
		digest["digest"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE="
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.contentDigests[0].digest", "pattern")
	})
	t.Run("44 data characters without padding", func(t *testing.T) {
		m := docMap()
		digest := m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)
		digest["encoding"] = DigestEncodingBase64
		digest["digest"] = base64.StdEncoding.EncodeToString(make([]byte, 33))
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.contentDigests[0].digest", "pattern")
	})
	t.Run("non-zero pad bits", func(t *testing.T) {
		// The last data character of the canonical encoding is 'U'
		// (010100, pad bits 00). 'V' (010101) and 'S' (010010) decode to
		// the same 32 bytes with non-zero pad bits — Go's decoder
		// tolerates them, the strict RFC-4648 layer must not.
		for _, variant := range []string{"V", "S"} {
			m := docMap()
			digest := m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)
			digest["encoding"] = DigestEncodingBase64
			digest["digest"] = valid[:42] + variant + "="
			pe := requireParseError(t, docJSON(t, m))
			assertValidationError(t, pe, "trust.contentDigests[0].digest", "digest-encoding")
		}
	})
	t.Run("lowercase variant rejected", func(t *testing.T) {
		// Base64 is case-sensitive: a lowercase rendering is a different
		// digest value, so it cannot re-encode to the declared string and
		// is rejected as non-canonical.
		m := docMap()
		digest := m["trust"].(map[string]any)["contentDigests"].([]any)[0].(map[string]any)
		digest["encoding"] = DigestEncodingBase64
		digest["digest"] = strings.ToLower(valid)
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.contentDigests[0].digest", "digest-encoding")
	})
}

// TestParseAttestationConstraints asserts the attestation shape: the
// ed25519 algorithm (PM decision D-01) and non-empty signature and
// publicKey.
func TestParseAttestationConstraints(t *testing.T) {
	t.Run("algorithm enum", func(t *testing.T) {
		m := docMap()
		m["trust"].(map[string]any)["attestation"].(map[string]any)["algorithm"] = "rsa"
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.attestation.algorithm", "enum")
	})
	t.Run("empty signature", func(t *testing.T) {
		m := docMap()
		m["trust"].(map[string]any)["attestation"].(map[string]any)["signature"] = ""
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.attestation.signature", "minLength")
	})
	t.Run("empty publicKey", func(t *testing.T) {
		m := docMap()
		m["trust"].(map[string]any)["attestation"].(map[string]any)["publicKey"] = ""
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.attestation.publicKey", "minLength")
	})
}

// TestParseAttestationBase64Shape asserts the attestation signature and
// publicKey are strict RFC-4648 base64 (standard alphabet, exact padding,
// canonical pad bits) — a fail-fast parse-time check so a malformed
// signature or key never reaches trust verification (registry-metadata.md
// §4.7).
func TestParseAttestationBase64Shape(t *testing.T) {
	t.Run("signature not base64", func(t *testing.T) {
		m := docMap()
		m["trust"].(map[string]any)["attestation"].(map[string]any)["signature"] = "!!!not base64!!!"
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.attestation.signature", "encoding")
	})
	t.Run("publicKey not base64", func(t *testing.T) {
		m := docMap()
		m["trust"].(map[string]any)["attestation"].(map[string]any)["publicKey"] = "not base64!"
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.attestation.publicKey", "encoding")
	})
	t.Run("signature non-zero pad bits", func(t *testing.T) {
		// "eQ==" is the canonical encoding of one byte 0x79 ('Q' = 010000,
		// pad bits 00); 'R' (010001) and 'S' (010010) decode to the same
		// byte with non-zero pad bits and must be rejected.
		for _, variant := range []string{"eR==", "eS=="} {
			m := docMap()
			m["trust"].(map[string]any)["attestation"].(map[string]any)["signature"] = variant
			pe := requireParseError(t, docJSON(t, m))
			assertValidationError(t, pe, "trust.attestation.signature", "encoding")
		}
	})
	t.Run("publicKey non-zero pad bits", func(t *testing.T) {
		// The fixture key ends in the canonical quantum "MA==" ('A' =
		// 000000, pad bits zero). 'B' (000001) decodes to the same final
		// byte with non-zero pad bits and must be rejected.
		pub := "cHVibGljLWtleS1iYXNlNjQtZW5jb2RlZC1kZW1vLXB1YmxpYy1rZXktMTIzNDU2Nzg5MA=="
		m := docMap()
		m["trust"].(map[string]any)["attestation"].(map[string]any)["publicKey"] = pub[:len(pub)-4] + "MB=="
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "trust.attestation.publicKey", "encoding")
	})
	t.Run("lowercase fixture values accepted", func(t *testing.T) {
		// The corpus fixtures carry lowercase RFC-4648 base64 for the
		// signature and publicKey; the shape check must accept them
		// (case does not change the decoded material, pad bits are zero).
		sig := "c2lnbmF0dXJlLXZhbHVlLWJhc2U2NC1lbmNvZGVkLWRlbW8tc2lnbmF0dXJlLTEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5"
		pub := "cHVibGljLWtleS1iYXNlNjQtZW5jb2RlZC1kZW1vLXB1YmxpYy1rZXktMTIzNDU2Nzg5MA=="
		m := docMap()
		att := m["trust"].(map[string]any)["attestation"].(map[string]any)
		att["signature"] = sig
		att["publicKey"] = pub
		requireParseSuccess(t, docJSON(t, m))
	})
}

// TestParseErrorsAggregated asserts Parse reports every problem in one
// pass — all errors are collected, not just the first.
func TestParseErrorsAggregated(t *testing.T) {
	m := docMap()
	m["id"] = "Bad_Id"
	m["version"] = "1.2"
	m["distribution"].(map[string]any)["location"] = "http://github.com/maleolabs/x/releases/download/v1.2.3/x.tar.gz"
	m["lifecycle"].(map[string]any)["state"] = "draft"

	pe := requireParseError(t, docJSON(t, m))
	assertValidationError(t, pe, "id", "pattern")
	assertValidationError(t, pe, "version", "pattern")
	assertValidationError(t, pe, "distribution.location", "pattern")
	assertValidationError(t, pe, "lifecycle.state", "enum")
}

// TestParseErrorIsMatchable asserts ParseError supports errors.Is, the
// codebase convention for aggregated validation errors
// (ValidationBlockedError, internal/project/enforcement.go).
func TestParseErrorIsMatchable(t *testing.T) {
	_, err := Parse([]byte(`{"id": 1}`))
	if !errors.Is(err, &ParseError{}) {
		t.Fatalf("errors.Is(err, &ParseError{}) = false for %v", err)
	}
}

// TestParseErrorMessagesAreActionable asserts error output identifies the
// failing field and the violated rule in plain text.
func TestParseErrorMessagesAreActionable(t *testing.T) {
	m := docMap()
	m["distribution"].(map[string]any)["location"] = "http://github.com/x"
	pe := requireParseError(t, docJSON(t, m))
	text := pe.Error()
	for _, fragment := range []string{"distribution.location", "pattern", "https"} {
		if !strings.Contains(text, fragment) {
			t.Errorf("error text does not mention %q:\n%s", fragment, text)
		}
	}
}

// TestParsePositiveFixtures asserts every positive conformance fixture
// parses and validates without errors — the parser must accept exactly the
// documents the schema accepts (scripts/validate-schemas.sh counterpart).
func TestParsePositiveFixtures(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join(fixturesDir, "positive", "*.json"))
	if err != nil {
		t.Fatalf("glob positive fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Skipf("positive fixtures not present (EKA mode) — no fixtures under %s", filepath.Join(fixturesDir, "positive"))
	}

	for _, fixture := range fixtures {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			raw, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			res := requireParseSuccess(t, string(raw))

			if res.Metadata.ID == "" || res.Metadata.Version == "" || res.Metadata.ContractVersion == "" {
				t.Error("parsed metadata is missing identity/version/contractVersion")
			}
			if len(res.Metadata.Trust.ContentDigests) == 0 {
				t.Error("parsed metadata is missing content digests")
			}

			switch filepath.Base(fixture) {
			case "deprecated-without-removal-date.json":
				assertWarning(t, res, "lifecycle.removalDate", "advisory")
			default:
				assertNoWarnings(t, res)
			}
		})
	}
}

// TestParseNegativeFixtures asserts every negative conformance fixture is
// rejected — the parser must reject exactly the documents the schema
// rejects — with actionable errors identifying the problem (DoD: malformed
// and schema-invalid metadata is rejected with an actionable error).
func TestParseNegativeFixtures(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join(fixturesDir, "negative", "*.json"))
	if err != nil {
		t.Fatalf("glob negative fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Skipf("negative fixtures not present (EKA mode) — no fixtures under %s", filepath.Join(fixturesDir, "negative"))
	}

	for _, fixture := range fixtures {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			raw, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			pe := requireParseError(t, string(raw))
			if len(pe.Errors) == 0 {
				t.Fatal("rejection carries no validation errors")
			}
			for _, ve := range pe.Errors {
				if ve.Field == "" {
					t.Errorf("validation error has no field: %s", ve.Error())
				}
				if ve.Rule == "" {
					t.Errorf("validation error has no rule: %s", ve.Error())
				}
				if ve.Message == "" {
					t.Errorf("validation error has no message: %s", ve.Error())
				}
			}
			if !strings.Contains(pe.Error(), pe.Errors[0].Field) {
				t.Errorf("error text does not identify the failing field:\n%s", pe.Error())
			}
		})
	}
}

// TestParseHostileInput asserts Parse fails cleanly — no panic, a
// *ParseError with an actionable entry — on hostile documents: extreme
// nesting, huge strings, invalid UTF-8, and non-object roots (reviewer
// finding 4: hostile-input regression coverage).
func TestParseHostileInput(t *testing.T) {
	t.Run("deep nesting", func(t *testing.T) {
		// encoding/json bails out at its nesting depth limit; Parse must
		// surface that as a malformed document error, never a panic.
		doc := strings.Repeat("[", 20000) + strings.Repeat("]", 20000)
		pe := requireParseError(t, doc)
		assertValidationError(t, pe, "document", "json")
	})
	t.Run("one megabyte string", func(t *testing.T) {
		m := docMap()
		m["description"] = strings.Repeat("a", 1<<20)
		res := requireParseSuccess(t, docJSON(t, m))
		if len(res.Metadata.Description) != 1<<20 {
			t.Errorf("description length = %d, want %d", len(res.Metadata.Description), 1<<20)
		}
	})
	t.Run("invalid utf-8", func(t *testing.T) {
		// encoding/json replaces invalid bytes with U+FFFD instead of
		// erroring; the replaced id then fails the identity pattern.
		doc := `{"id": "` + string([]byte{0xff, 0xfe, 0xfd}) + `"}`
		pe := requireParseError(t, doc)
		assertValidationError(t, pe, "id", "pattern")
	})
	t.Run("root bool", func(t *testing.T) {
		for _, doc := range []string{"true", "false"} {
			pe := requireParseError(t, doc)
			assertValidationError(t, pe, "document", "type")
		}
	})
	t.Run("empty input", func(t *testing.T) {
		pe := requireParseError(t, "")
		assertValidationError(t, pe, "document", "json")
	})
}

// FuzzParse asserts Parse never panics on arbitrary input and always
// returns either a parsed result or a *ParseError — hostile or malformed
// input must fail cleanly, never crash the caller. The fuzz harness runs
// the seed corpus on every `go test`; `go test -fuzz=FuzzParse` extends it
// with generated inputs.
func FuzzParse(f *testing.F) {
	f.Add([]byte(validMetadataDoc()))
	f.Add([]byte(`{"id": 1}`))
	f.Add([]byte(`{`))
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xfe, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := Parse(data)
		if err != nil {
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("error is %T, want *ParseError: %v", err, err)
			}
			if res != nil {
				t.Fatal("Parse returned a result alongside an error")
			}
			return
		}
		if res == nil || res.Metadata == nil {
			t.Fatal("Parse returned neither a result nor an error")
		}
	})
}

// ── Named asset digest entries (TS-014-04-04) ───────────────────────

// TestParseNamedAssetDigest asserts contentDigests entries may carry an
// optional name binding the entry to a named release asset (e.g. an
// adapter binary); the parsed document preserves it.
func TestParseNamedAssetDigest(t *testing.T) {
	m := docMap()
	trust := m["trust"].(map[string]any)
	digests := trust["contentDigests"].([]any)
	digests = append(digests, map[string]any{
		"algorithm": "sha-256",
		"encoding":  "base16",
		"digest":    "1111111111111111111111111111111111111111111111111111111111111111",
		"name":      "anvil-adapter-laravel-linux-amd64",
	})
	trust["contentDigests"] = digests

	res, err := Parse([]byte(docJSON(t, m)))
	if err != nil {
		t.Fatalf("Parse rejected a document with a named asset digest: %v", err)
	}
	if len(res.Metadata.Trust.ContentDigests) != 2 {
		t.Fatalf("ContentDigests = %d entries, want 2", len(res.Metadata.Trust.ContentDigests))
	}
	got := res.Metadata.Trust.ContentDigests[1]
	if got.Name != "anvil-adapter-laravel-linux-amd64" {
		t.Errorf("Name = %q, want the declared asset name", got.Name)
	}
}

// TestParseNamedAssetDigestRejectsUnsafeName asserts the name pattern
// rejects anything that could escape the release channel as a path
// component or break exact asset matching (slashes, dots, uppercase,
// leading digits are fine but slashes and dots are not).
func TestParseNamedAssetDigestRejectsUnsafeName(t *testing.T) {
	for _, name := range []string{
		"binaries/anvil-adapter-laravel-linux-amd64", // path separator
		"../anvil-adapter",                           // traversal
		"Anvil-Adapter",                              // uppercase
		"anvil.adapter",                              // dot
	} {
		m := docMap()
		trust := m["trust"].(map[string]any)
		digests := trust["contentDigests"].([]any)
		digests = append(digests, map[string]any{
			"algorithm": "sha-256",
			"encoding":  "base16",
			"digest":    "1111111111111111111111111111111111111111111111111111111111111111",
			"name":      name,
		})
		trust["contentDigests"] = digests

		_, err := Parse([]byte(docJSON(t, m)))
		if err == nil {
			t.Errorf("Parse accepted name %q, want a pattern rejection", name)
			continue
		}
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Fatalf("name %q: err = %v, want a *ParseError", name, err)
		}
		assertValidationError(t, pe, "trust.contentDigests[1].name", "pattern")
	}
}

// TestParseNamedAssetDigestRejectsDuplicateName asserts two entries can
// never bind the same asset: duplicate names are rejected.
func TestParseNamedAssetDigestRejectsDuplicateName(t *testing.T) {
	m := docMap()
	trust := m["trust"].(map[string]any)
	digests := trust["contentDigests"].([]any)
	digests = append(digests,
		map[string]any{
			"algorithm": "sha-256",
			"encoding":  "base16",
			"digest":    "1111111111111111111111111111111111111111111111111111111111111111",
			"name":      "anvil-adapter-laravel-linux-amd64",
		},
		map[string]any{
			"algorithm": "sha-256",
			"encoding":  "base16",
			"digest":    "2222222222222222222222222222222222222222222222222222222222222222",
			"name":      "anvil-adapter-laravel-linux-amd64",
		},
	)
	trust["contentDigests"] = digests

	_, err := Parse([]byte(docJSON(t, m)))
	if err == nil {
		t.Fatal("Parse accepted duplicate asset names, want a rejection")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a *ParseError", err)
	}
	assertValidationError(t, pe, "trust.contentDigests[2].name", "uniqueItems")
}

// ── Skills section (TS-021-04; ADR-037 D2) ───────────────────

// skillsDoc returns a valid document carrying the optional additive skills
// section: two skills whose assets are covered by attested named digest
// entries in trust.contentDigests (TS-014-04-04).
func skillsDoc() string {
	return `{
		"$schema": "urn:anvil:spec:registry-metadata:1.0.0",
		"id": "anvil-standard-laravel",
		"version": "1.2.3",
		"contractVersion": "1.0.0",
		"capability": {
			"frameworkVersion": ["5.1.0", "5.2.0"]
		},
		"distribution": {
			"type": "github-releases",
			"location": "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/anvil-standard-laravel.tar.gz"
		},
		"lifecycle": {
			"state": "published"
		},
		"trust": {
			"contentDigests": [
				{
					"algorithm": "sha-256",
					"encoding": "base16",
					"digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
				},
				{
					"algorithm": "sha-256",
					"encoding": "base16",
					"digest": "1111111111111111111111111111111111111111111111111111111111111111",
					"name": "anvil-skill-overview-1-0-0"
				},
				{
					"algorithm": "sha-256",
					"encoding": "base16",
					"digest": "2222222222222222222222222222222222222222222222222222222222222222",
					"name": "anvil-skill-lifecycle-1-0-0"
				}
			],
			"attestation": {
				"algorithm": "ed25519",
				"signature": "c2lnbmF0dXJlLXZhbHVlLWJhc2U2NC1lbmNvZGVkLWRlbW8tc2lnbmF0dXJlLTEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5",
				"publicKey": "cHVibGljLWtleS1iYXNlNjQtZW5jb2RlZC1kZW1vLXB1YmxpYy1rZXktMTIzNDU2Nzg5MA=="
			}
		},
		"skills": [
			{
				"name": "overview",
				"version": "1.0.0",
				"asset": "anvil-skill-overview-1-0-0",
				"description": "What Anvil is and how to use it inside a Laravel project."
			},
			{
				"name": "lifecycle",
				"version": "1.0.0",
				"asset": "anvil-skill-lifecycle-1-0-0"
			}
		]
	}`
}

// skillsDocMap returns the valid skills document as a generic map for
// mutation.
func skillsDocMap() map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(skillsDoc()), &m); err != nil {
		panic("skills test document is not valid JSON: " + err.Error())
	}
	return m
}

// skillsItems returns the skills array of a mutated skills document map.
func skillsItems(m map[string]any) []any {
	items, _ := m["skills"].([]any)
	return items
}

// TestParseSkillsForwardCompatible asserts the old-parses-new direction of
// the forward-compat decision: a metadata document carrying the new
// additive skills[] section parses and validates, and the parsed document
// preserves every skill (TS-021-04 DoD).
func TestParseSkillsForwardCompatible(t *testing.T) {
	res := requireParseSuccess(t, skillsDoc())
	assertNoWarnings(t, res)

	skills := res.Metadata.Skills
	if len(skills) != 2 {
		t.Fatalf("Skills = %d entries, want 2", len(skills))
	}
	overview := skills[0]
	if overview.Name != "overview" || overview.Version != "1.0.0" ||
		overview.Asset != "anvil-skill-overview-1-0-0" ||
		overview.Description != "What Anvil is and how to use it inside a Laravel project." {
		t.Errorf("skills[0] = %+v", overview)
	}
	lifecycle := skills[1]
	if lifecycle.Name != "lifecycle" || lifecycle.Version != "1.0.0" ||
		lifecycle.Asset != "anvil-skill-lifecycle-1-0-0" || lifecycle.Description != "" {
		t.Errorf("skills[1] = %+v", lifecycle)
	}
}

// TestParseSkillsAbsentBehaviorUnchanged asserts a document WITHOUT the
// skills section behaves exactly as before: Skills is nil and the parse
// succeeds without warnings — no behavior change for metadata without
// skills[] (TS-021-04 DoD).
func TestParseSkillsAbsentBehaviorUnchanged(t *testing.T) {
	res := requireParseSuccess(t, validMetadataDoc())
	assertNoWarnings(t, res)
	if res.Metadata.Skills != nil {
		t.Errorf("Skills = %v, want nil for a document without skills[]", res.Metadata.Skills)
	}
}

// TestParseSkillsEmptyArrayAllowed asserts an empty skills array parses
// like an absent one: the section is optional and an empty declaration is
// equivalent to none (the old-parser tolerance keeps new parsers from
// being stricter than old parsers on the same document).
func TestParseSkillsEmptyArrayAllowed(t *testing.T) {
	m := docMap()
	m["skills"] = []any{}
	res := requireParseSuccess(t, docJSON(t, m))
	assertNoWarnings(t, res)
	if res.Metadata.Skills != nil {
		t.Errorf("Skills = %v, want nil for an empty skills array", res.Metadata.Skills)
	}
}

// TestParseSkillsStrictRejection asserts malformed skill declarations are
// rejected with actionable errors (TS-021-04 DoD): invalid name, missing
// name/version/asset, invalid version, wrong types, undeclared fields, and
// duplicate names. Core-field strictness is unchanged — the same document
// minus the skills problem parses.
func TestParseSkillsStrictRejection(t *testing.T) {
	t.Run("invalid name pattern", func(t *testing.T) {
		for _, name := range []string{"Bad-Name", "bad.name", "bad/name", "bad_name", "-bad"} {
			m := skillsDocMap()
			skillsItems(m)[0].(map[string]any)["name"] = name
			pe := requireParseError(t, docJSON(t, m))
			assertValidationError(t, pe, "skills[0].name", "pattern")
		}
	})
	t.Run("name too long", func(t *testing.T) {
		m := skillsDocMap()
		skillsItems(m)[0].(map[string]any)["name"] = strings.Repeat("a", 65)
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "skills[0].name", "maxLength")
	})
	t.Run("missing name", func(t *testing.T) {
		m := skillsDocMap()
		delete(skillsItems(m)[0].(map[string]any), "name")
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "skills[0].name", "required")
	})
	t.Run("missing version", func(t *testing.T) {
		m := skillsDocMap()
		delete(skillsItems(m)[0].(map[string]any), "version")
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "skills[0].version", "required")
	})
	t.Run("missing asset", func(t *testing.T) {
		m := skillsDocMap()
		delete(skillsItems(m)[0].(map[string]any), "asset")
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "skills[0].asset", "required")
	})
	t.Run("invalid version pattern", func(t *testing.T) {
		for _, version := range []string{"1.0", "v1.0.0", "01.0.0", "1.0.0-beta"} {
			m := skillsDocMap()
			skillsItems(m)[0].(map[string]any)["version"] = version
			pe := requireParseError(t, docJSON(t, m))
			assertValidationError(t, pe, "skills[0].version", "pattern")
		}
	})
	t.Run("unbound asset", func(t *testing.T) {
		// The asset is not covered by any named digest entry in
		// trust.contentDigests — the cross-field binding fails (ADR-037
		// D2; TS-014-04-04).
		m := skillsDocMap()
		skillsItems(m)[0].(map[string]any)["asset"] = "anvil-skill-unbound-1-0-0"
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "skills[0].asset", "binding")
	})
	t.Run("asset covered only by an unnamed digest", func(t *testing.T) {
		// The content digest has no name; a skill asset must be bound to a
		// NAMED entry — an unnamed content digest does not cover it.
		m := skillsDocMap()
		skillsItems(m)[0].(map[string]any)["asset"] = "anvil-skill-overview-1-0-0"
		trust := m["trust"].(map[string]any)
		digests := trust["contentDigests"].([]any)
		// drop the named overview entry, keep only the unnamed content digest
		var kept []any
		for _, d := range digests {
			if d.(map[string]any)["name"] == "anvil-skill-overview-1-0-0" {
				continue
			}
			kept = append(kept, d)
		}
		trust["contentDigests"] = kept
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "skills[0].asset", "binding")
	})
	t.Run("duplicate skill name", func(t *testing.T) {
		m := skillsDocMap()
		first := skillsItems(m)[0].(map[string]any)
		// Replace the second skill with a second declaration of the same
		// name (different version, so the two entries are not identical —
		// the parser's per-name uniqueness check is what must fire). The
		// duplicated name's asset is bound, otherwise the binding error
		// would mask the uniqueness assertion.
		trust := m["trust"].(map[string]any)
		trust["contentDigests"] = append(trust["contentDigests"].([]any), map[string]any{
			"algorithm": "sha-256",
			"encoding":  "base16",
			"digest":    "3333333333333333333333333333333333333333333333333333333333333333",
			"name":      "anvil-skill-overview-2-0-0",
		})
		skillsItems(m)[1] = map[string]any{
			"name":    first["name"],
			"version": "2.0.0",
			"asset":   "anvil-skill-overview-2-0-0",
		}
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "skills[1].name", "uniqueItems")
	})
	t.Run("skills as string", func(t *testing.T) {
		m := docMap()
		m["skills"] = "not-an-array"
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "skills", "type")
	})
	t.Run("skills item as string", func(t *testing.T) {
		m := docMap()
		m["skills"] = []any{"overview"}
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "skills[0]", "type")
	})
	t.Run("skill name wrong type", func(t *testing.T) {
		m := docMap()
		m["skills"] = []any{map[string]any{"name": 5, "version": "1.0.0", "asset": "anvil-skill-overview-1-0-0"}}
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "skills[0].name", "type")
	})
	t.Run("undeclared field in skill", func(t *testing.T) {
		m := skillsDocMap()
		skillsItems(m)[0].(map[string]any)["author"] = "maleolabs"
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "skills[0].author", "additionalProperties")
	})
	t.Run("description wrong type", func(t *testing.T) {
		m := skillsDocMap()
		skillsItems(m)[0].(map[string]any)["description"] = []any{"not a string"}
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "skills[0].description", "type")
	})
}

// TestParseUnknownRootSectionTolerated asserts the forward-compat
// tolerance of the recorded decision (TS-021-04, registry-metadata.md
// §4.8): unknown-but-optional root SECTIONS — keys the schema does not
// declare whose value is an object or an array — are accepted and ignored,
// so an older parser meeting a newer additive section does not reject an
// otherwise-valid document. Unknown root keys with scalar values remain
// rejected (runtime-agnostic requirement), and unknown fields inside
// declared sections remain rejected (nested additionalProperties: false is
// unchanged).
func TestParseUnknownRootSectionTolerated(t *testing.T) {
	t.Run("unknown object section ignored", func(t *testing.T) {
		m := docMap()
		m["futureSection"] = map[string]any{"option": "value"}
		res := requireParseSuccess(t, docJSON(t, m))
		assertNoWarnings(t, res)
	})
	t.Run("unknown array section ignored", func(t *testing.T) {
		m := docMap()
		m["futureList"] = []any{"a", "b"}
		res := requireParseSuccess(t, docJSON(t, m))
		assertNoWarnings(t, res)
	})
	t.Run("unknown scalar field rejected", func(t *testing.T) {
		// A scalar value cannot be a section — the runtime-agnostic
		// requirement stays enforceable (a runtime-typed scalar field
		// still fails).
		m := docMap()
		m["golangVersion"] = "1.25.12"
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "golangVersion", "additionalProperties")
	})
	t.Run("unknown null field rejected", func(t *testing.T) {
		m := docMap()
		m["futureSection"] = nil
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "futureSection", "additionalProperties")
	})
	t.Run("unknown field in declared section rejected", func(t *testing.T) {
		// Nested strictness is unchanged: only the root tolerates unknown
		// optional sections within the deprecation window.
		m := docMap()
		m["lifecycle"].(map[string]any)["extra"] = map[string]any{"nested": true}
		pe := requireParseError(t, docJSON(t, m))
		assertValidationError(t, pe, "lifecycle.extra", "additionalProperties")
	})
}
