package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// schemaPath and fixturesRoot are relative to this package (internal/registry).
const (
	schemaPath  = "../../docs/specification-corpus/registry-metadata.schema.json"
	fixturesDir = "../../docs/specification-corpus/fixtures/registry-metadata"
)

// loadSchema reads and parses the registry metadata schema document.
func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("schema not present (EKA mode) — %v", err)
		}
		t.Fatalf("read schema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	return schema
}

// requiredList returns the string elements of a schema "required" array.
func requiredList(t *testing.T, node map[string]any, field string) []string {
	t.Helper()
	raw, ok := node[field].([]any)
	if !ok {
		t.Fatalf("schema node %q has no required array", field)
	}
	var out []string
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("schema node %q required array contains a non-string", field)
		}
		out = append(out, s)
	}
	return out
}

// TestSchemaDocumentIsValidJSON asserts the schema is a machine-readable
// JSON document declaring the registry metadata format (TS-014-01-01 DoD:
// schema is defined and machine-readable).
func TestSchemaDocumentIsValidJSON(t *testing.T) {
	schema := loadSchema(t)

	if schema["$id"] != SchemaID {
		t.Errorf("$id = %v, want %s", schema["$id"], SchemaID)
	}
	if schema["type"] != "object" {
		t.Errorf("type = %v, want object", schema["type"])
	}
	if schema["$schema"] != "http://json-schema.org/draft-07/schema#" {
		t.Errorf("$schema = %v, want draft-07", schema["$schema"])
	}
	// Root additionalProperties is the forward-compat section tolerance of
	// the recorded decision (TS-021-04, registry-metadata.md §4.8):
	// unknown-but-optional root sections (object- or array-valued keys)
	// are tolerated within the deprecation window, while unknown root keys
	// with scalar values stay rejected. Nested strictness
	// (additionalProperties: false inside every declared section) is
	// unchanged.
	ap, ok := schema["additionalProperties"].(map[string]any)
	if !ok {
		t.Fatalf("root additionalProperties = %v, want a section-tolerance subschema (TS-021-04)", schema["additionalProperties"])
	}
	types, ok := ap["type"].([]any)
	if !ok || len(types) != 2 || !containsAny(types, "object") || !containsAny(types, "array") {
		t.Errorf("root additionalProperties type = %v, want [object, array] (unknown optional sections tolerated, scalars rejected)", ap["type"])
	}
}

// containsAny reports whether a []any slice contains the given string.
func containsAny(list []any, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// TestSchemaRequiresAllDoDFields asserts the document surface carries every
// field the work item requires: identity, version, declared contract
// version, capability declaration, distribution location, lifecycle state,
// and trust fields (TS-014-01-01 §1; §3).
func TestSchemaRequiresAllDoDFields(t *testing.T) {
	schema := loadSchema(t)

	got := requiredList(t, schema, "required")
	want := []string{
		"id",              // identity
		"version",         // release version
		"contractVersion", // declared contract version
		"capability",      // capability declaration
		"distribution",    // distribution location
		"lifecycle",       // lifecycle state
		"trust",           // trust fields
	}
	for _, field := range want {
		if !contains(got, field) {
			t.Errorf("schema root required fields %v do not include %q", got, field)
		}
	}
}

// TestSchemaTrustFieldsRequiredFromDayOne asserts the trust fields are
// required, not optional: integrity verification material (content
// digests) and publisher attestation (signature + verification public key)
// (TS-014-01-01 DoD: trust fields present from day one — ADR-022 §3, §6.5;
// PM decision D-01).
func TestSchemaTrustFieldsRequiredFromDayOne(t *testing.T) {
	schema := loadSchema(t)

	defs := schema["definitions"].(map[string]any)
	trust := defs["trust"].(map[string]any)
	for _, field := range []string{"contentDigests", "attestation"} {
		if !contains(requiredList(t, trust, "required"), field) {
			t.Errorf("trust required fields %v do not include %q", requiredList(t, trust, "required"), field)
		}
	}

	attestation := defs["attestation"].(map[string]any)
	for _, field := range []string{"algorithm", "signature", "publicKey"} {
		if !contains(requiredList(t, attestation, "required"), field) {
			t.Errorf("attestation required fields %v do not include %q", requiredList(t, attestation, "required"), field)
		}
	}

	digest := defs["contentDigest"].(map[string]any)
	for _, field := range []string{"algorithm", "encoding", "digest"} {
		if !contains(requiredList(t, digest, "required"), field) {
			t.Errorf("contentDigest required fields %v do not include %q", requiredList(t, digest, "required"), field)
		}
	}
}

// TestSchemaLifecycleStates asserts the lifecycle state enum supports
// exactly the states Published, Deprecated, and Retired (machine values
// published, deprecated, retired) (TS-014-01-01 DoD; ADR-023 §3; ADR-027
// §3; PM decision D-03).
func TestSchemaLifecycleStates(t *testing.T) {
	schema := loadSchema(t)

	defs := schema["definitions"].(map[string]any)
	lifecycle := defs["lifecycle"].(map[string]any)
	if !contains(requiredList(t, lifecycle, "required"), "state") {
		t.Fatalf("lifecycle required fields %v do not include state", requiredList(t, lifecycle, "required"))
	}

	props := lifecycle["properties"].(map[string]any)
	state := props["state"].(map[string]any)
	values := state["enum"].([]any)

	var got []string
	for _, v := range values {
		got = append(got, v.(string))
	}
	want := []string{LifecycleStatePublished, LifecycleStateDeprecated, LifecycleStateRetired}
	if len(got) != len(want) {
		t.Fatalf("lifecycle state enum = %v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("lifecycle state enum = %v, want %v", got, want)
		}
	}

	// The deprecated state carries an optional announced removal date
	// (PM decision D-03).
	removalDate, ok := props["removalDate"]
	if !ok {
		t.Fatal("lifecycle schema has no removalDate property")
	}
	if format := removalDate.(map[string]any)["format"]; format != "date-time" {
		t.Errorf("removalDate format = %v, want date-time", format)
	}
	if contains(requiredList(t, lifecycle, "required"), "removalDate") {
		t.Error("removalDate must be optional per PM decision D-03")
	}
}

// TestSchemaCapabilityDeclarationPresent asserts the capability declaration
// carries the framework-version support scope (TS-014-01-01 DoD: declared
// contract version and capability declaration present — ADR-023 §3;
// ADR-021 §3.2; PRD-002 §5.8).
func TestSchemaCapabilityDeclarationPresent(t *testing.T) {
	schema := loadSchema(t)

	defs := schema["definitions"].(map[string]any)
	capability := defs["capability"].(map[string]any)
	if !contains(requiredList(t, capability, "required"), "frameworkVersion") {
		t.Errorf("capability required fields %v do not include frameworkVersion", requiredList(t, capability, "required"))
	}
}

// TestSchemaSkillsDeclarationPresent asserts the optional additive skills
// section is declared: a root skills array property and a skill definition
// requiring name, version, and asset, with description optional (TS-021-04;
// ADR-037 D2). The section must NOT be in the root required list — the
// extension is additive-only; a release without skills[] is valid.
func TestSchemaSkillsDeclarationPresent(t *testing.T) {
	schema := loadSchema(t)

	if contains(requiredList(t, schema, "required"), "skills") {
		t.Error("skills must be optional — the extension is additive-only (TS-021-04); a release without skills[] is valid")
	}

	props := schema["properties"].(map[string]any)
	skillsProp, ok := props["skills"].(map[string]any)
	if !ok {
		t.Fatal("schema has no skills root property")
	}
	if skillsProp["type"] != "array" {
		t.Errorf("skills type = %v, want array", skillsProp["type"])
	}
	if items, ok := skillsProp["items"].(map[string]any); !ok || items["$ref"] != "#/definitions/skill" {
		t.Errorf("skills items = %v, want $ref to the skill definition", skillsProp["items"])
	}

	defs := schema["definitions"].(map[string]any)
	skill, ok := defs["skill"].(map[string]any)
	if !ok {
		t.Fatal("schema has no skill definition")
	}
	for _, field := range []string{"name", "version", "asset"} {
		if !contains(requiredList(t, skill, "required"), field) {
			t.Errorf("skill required fields %v do not include %q", requiredList(t, skill, "required"), field)
		}
	}
	if contains(requiredList(t, skill, "required"), "description") {
		t.Error("skill description must be optional")
	}
	skillProps := skill["properties"].(map[string]any)
	if _, ok := skillProps["description"]; !ok {
		t.Error("skill definition has no description property")
	}
	name := skillProps["name"].(map[string]any)
	if name["pattern"] != "^[a-z0-9][a-z0-9-]*$" {
		t.Errorf("skill name pattern = %v, want the safe identifier pattern", name["pattern"])
	}
	if name["maxLength"] != float64(64) {
		t.Errorf("skill name maxLength = %v, want 64", name["maxLength"])
	}
	asset := skillProps["asset"].(map[string]any)
	if asset["maxLength"] != float64(128) {
		t.Errorf("skill asset maxLength = %v, want 128", asset["maxLength"])
	}
}

// goRuntimeTokenPatterns are regexes that would match a Go implementation
// leaking into the format. The registry metadata format is
// runtime-agnostic: no Go-specific fields, no Go types (TS-014-01-01 DoD;
// ADR-023 §3; Transition Plan §5.10). Word boundaries keep prose such as
// "structurally" from false-positiving on the Go keyword "struct".
var goRuntimeTokenPatterns = []string{
	`\bstruct\b`,
	`map\[`,
	`interface\{\}`,
	`\bint64\b`,
	`\bint32\b`,
	`\[\]string`,
	`golang`,
	`maleolabs\.com/anvil`,
}

// TestSchemaContainsNoRuntimeSpecificFields asserts the schema document
// itself contains none of the Go-specific tokens, keeping the format
// consumable by independent runtime implementations (TS-014-01-01 DoD;
// ADR-023 §3; ADR-029 §3).
func TestSchemaContainsNoRuntimeSpecificFields(t *testing.T) {
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("schema not present (EKA mode) — %v", err)
		}
		t.Fatalf("read schema: %v", err)
	}
	text := string(raw)
	for _, pattern := range goRuntimeTokenPatterns {
		if regexp.MustCompile(pattern).MatchString(text) {
			t.Errorf("schema contains Go runtime token matching %q — the format must remain runtime-agnostic (ADR-023 §3, §5.10)", pattern)
		}
	}
}

// TestConstantsMatchSchema asserts the Go constants mirror the schema's
// machine values exactly, so runtime code and the schema cannot drift. The
// pinned enums (digest algorithm, attestation algorithm, distribution
// type) must have exactly one value each — the trust baseline fixes
// SHA-256 and Ed25519 (PM decision D-01), and ADR-030 fixes the
// github-releases channel; an additional value would require a governed
// schema evolution.
func TestConstantsMatchSchema(t *testing.T) {
	schema := loadSchema(t)
	defs := schema["definitions"].(map[string]any)

	lifecycle := defs["lifecycle"].(map[string]any)
	state := lifecycle["properties"].(map[string]any)["state"].(map[string]any)
	for _, v := range state["enum"].([]any) {
		switch v {
		case LifecycleStatePublished, LifecycleStateDeprecated, LifecycleStateRetired:
		default:
			t.Errorf("schema lifecycle enum value %q has no matching Go constant", v)
		}
	}

	digest := defs["contentDigest"].(map[string]any)
	alg := digest["properties"].(map[string]any)["algorithm"].(map[string]any)
	if !sameEnum(alg["enum"].([]any), []string{DigestAlgorithmSHA256}) {
		t.Errorf("schema digest algorithm enum = %v, want exactly [%s]", alg["enum"], DigestAlgorithmSHA256)
	}
	encoding := digest["properties"].(map[string]any)["encoding"].(map[string]any)
	wantEnc := []string{DigestEncodingBase16, DigestEncodingBase32, DigestEncodingBase64}
	if !sameEnum(encoding["enum"].([]any), wantEnc) {
		t.Errorf("schema digest encoding enum = %v, want %v", encoding["enum"], wantEnc)
	}

	attestation := defs["attestation"].(map[string]any)
	sigAlg := attestation["properties"].(map[string]any)["algorithm"].(map[string]any)
	if !sameEnum(sigAlg["enum"].([]any), []string{AttestationAlgorithmEd25519}) {
		t.Errorf("schema attestation algorithm enum = %v, want exactly [%s]", sigAlg["enum"], AttestationAlgorithmEd25519)
	}

	distribution := defs["distribution"].(map[string]any)
	distType := distribution["properties"].(map[string]any)["type"].(map[string]any)
	if !sameEnum(distType["enum"].([]any), []string{DistributionTypeGitHubReleases}) {
		t.Errorf("schema distribution type enum = %v, want exactly [%s]", distType["enum"], DistributionTypeGitHubReleases)
	}
}

// attestationPayloadComposition is the canonical signed payload of the
// publisher attestation: it binds the release claims (id, version)
// together with the content digest(s), so the signature cannot be
// detached and replayed against a different release of the same content
// (registry-metadata.schema.json; registry-metadata.md §4.7; PM decision:
// attestation payload scope).
// Each contentDigests entry contributes its decoded digest bytes,
// prefixed by utf8(name) || 0x00 when the entry carries a name — the
// asset binding is signed material (security review F-2).
const attestationPayloadComposition = "utf8(id) || 0x00 || utf8(version) || 0x00 || concat(entry bytes in contentDigests array order)"

// TestSchemaAttestationPayloadDefinition asserts the canonical attestation
// payload composition is documented consistently in the schema, the
// corpus document, and this package — the schema text is authoritative,
// and the docs must not drift from it (ADR-029 §3).
func TestSchemaAttestationPayloadDefinition(t *testing.T) {
	schemaRaw, err := os.ReadFile(schemaPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("schema not present (EKA mode) — %v", err)
		}
		t.Fatalf("read schema: %v", err)
	}
	if !strings.Contains(string(schemaRaw), attestationPayloadComposition) {
		t.Errorf("schema does not document the attestation payload composition %q", attestationPayloadComposition)
	}

	mdRaw, err := os.ReadFile("../../docs/specification-corpus/registry-metadata.md")
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("registry metadata doc not present (EKA mode) — %v", err)
		}
		t.Fatalf("read registry-metadata.md: %v", err)
	}
	if !strings.Contains(string(mdRaw), attestationPayloadComposition) {
		t.Errorf("registry-metadata.md does not document the attestation payload composition %q", attestationPayloadComposition)
	}
}

// sameEnum reports whether a schema enum array matches the expected string
// values in order and with the same length.
func sameEnum(got []any, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestPositiveFixturesUnmarshal asserts every positive conformance fixture
// parses into the Go mirror with all required sections populated — the
// runtime-side counterpart of the schema conformance fixtures (scripts/
// validate-schemas.sh).
func TestPositiveFixturesUnmarshal(t *testing.T) {
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
			var md Metadata
			if err := json.Unmarshal(raw, &md); err != nil {
				t.Fatalf("fixture does not parse: %v", err)
			}

			assertRequired(t, "id", md.ID)
			assertRequired(t, "version", md.Version)
			assertRequired(t, "contractVersion", md.ContractVersion)
			if len(md.Capability.FrameworkVersion) == 0 {
				t.Error("capability.frameworkVersion is empty")
			}
			assertRequired(t, "distribution.type", md.Distribution.Type)
			assertRequired(t, "distribution.location", md.Distribution.Location)
			assertRequired(t, "lifecycle.state", md.Lifecycle.State)
			switch md.Lifecycle.State {
			case LifecycleStatePublished, LifecycleStateDeprecated, LifecycleStateRetired:
			default:
				t.Errorf("lifecycle.state = %q, want one of published/deprecated/retired", md.Lifecycle.State)
			}
			if len(md.Trust.ContentDigests) == 0 {
				t.Error("trust.contentDigests is empty — trust fields are required (ADR-022)")
			}
			for _, digest := range md.Trust.ContentDigests {
				assertRequired(t, "trust.contentDigests[].algorithm", digest.Algorithm)
				assertRequired(t, "trust.contentDigests[].encoding", digest.Encoding)
				assertRequired(t, "trust.contentDigests[].digest", digest.Digest)
			}
			assertRequired(t, "trust.attestation.algorithm", md.Trust.Attestation.Algorithm)
			assertRequired(t, "trust.attestation.signature", md.Trust.Attestation.Signature)
			assertRequired(t, "trust.attestation.publicKey", md.Trust.Attestation.PublicKey)
		})
	}
}

// TestMetadataJSONRoundTrip asserts the Go mirror marshals to the schema
// field names (camelCase) and unmarshals back losslessly, keeping the
// runtime mirror aligned with the runtime-agnostic format.
func TestMetadataJSONRoundTrip(t *testing.T) {
	md := Metadata{
		Schema:          SchemaID,
		ID:              "anvil-standard-laravel",
		Version:         "1.2.3",
		ContractVersion: "1.0.0",
		Capability: Capability{
			FrameworkVersion: []string{"5.1.0", "5.2.0"},
		},
		Distribution: Distribution{
			Type:     DistributionTypeGitHubReleases,
			Location: "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/anvil-standard-laravel.tar.gz",
		},
		Lifecycle: Lifecycle{
			State:       LifecycleStateDeprecated,
			RemovalDate: "2027-01-31T00:00:00Z",
		},
		Trust: Trust{
			ContentDigests: []ContentDigest{{
				Algorithm: DigestAlgorithmSHA256,
				Encoding:  DigestEncodingBase16,
				Digest:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			}},
			Attestation: Attestation{
				Algorithm: AttestationAlgorithmEd25519,
				Signature: "c2lnbmF0dXJlLXZhbHVl",
				PublicKey: "cHVibGljLWtleS12YWx1ZQ==",
			},
		},
	}

	raw, err := json.Marshal(md)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Metadata
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(decoded, md) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", decoded, md)
	}

	// The wire names must be the schema field names, not Go field names.
	for _, field := range []string{`"contractVersion"`, `"contentDigests"`, `"frameworkVersion"`, `"publicKey"`, `"removalDate"`} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("marshaled document does not use schema field name %s", field)
		}
	}
	if strings.Contains(string(raw), "ContractVersion") {
		t.Error("marshaled document leaks the Go field name ContractVersion — the format must stay runtime-agnostic")
	}
}

func assertRequired(t *testing.T, field, value string) {
	t.Helper()
	if value == "" {
		t.Errorf("%s is empty — field is required by the schema", field)
	}
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
