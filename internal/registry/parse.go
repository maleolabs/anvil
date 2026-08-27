// Registry metadata document parsing and validation (TS-014-01-02).
//
// Parse reads one registry metadata document and validates it against the
// machine-readable authority, docs/specification-corpus/registry-metadata.
// schema.json (TS-014-01-01; ADR-029 §3: the schema governs). The Go mirror
// types in metadata.go only shape the document; this file enforces the
// schema's rules — required fields, types, patterns, enums, bounds,
// additionalProperties: false, uniqueItems, and the if/then cross-field
// constraints — and the strict format layer the schema deliberately leaves
// advisory (registry-metadata.schema.json $comment): the https-only
// distribution scheme, RFC-3339 date-time removalDate values, strict
// RFC-4648 digest decoding with a decoded length of exactly 32 bytes
// (SHA-256) and canonical pad bits, and the strict RFC-4648 base64 shape
// of the attestation signature and publicKey (registry-metadata.md §4.7:
// both are base64-encoded RFC-4648 standard with padding).
//
// Failure classes (work item): malformed documents — not decodable JSON or
// wrong field types — and schema-invalid documents — decodable but
// violating a TS-014-01-01 rule — are both rejected with a ParseError whose
// ValidationError entries identify the offending field path and the
// violated rule. Advisory guidance that must not fail the document (PM
// decision D-03: a deprecated release SHOULD carry removalDate once the
// removal date is announced) is reported as a Warning, not an error.
//
// Forward compatibility (TS-021-04; PM-approved decision, recorded in
// registry-metadata.md §4.8): the skills section is additive-only and
// optional, so the parser meets it as a declared field. Unknown-but-
// optional root sections (object- or array-valued keys the schema does
// not declare) are accepted and ignored within the deprecation window —
// an older parser meeting a newer additive section must not reject an
// otherwise-valid document (ADR-024 spirit; ADR-037 §6). Core fields
// remain strict: unknown root keys with scalar values are still rejected
// (the runtime-agnostic requirement stays enforceable), nested
// additionalProperties: false is unchanged, and malformed skill
// declarations are rejected with actionable errors.
//
// Parsing is format-level only: it treats the document as runtime-agnostic
// data and introduces no assumptions tied to this implementation (ADR-023
// §3; Transition Plan §5.10). Signature verification is out of scope — the
// canonical attestation payload understanding is consumed by later trust
// work (TS-014-01-04; T-011); this file parses and validates the trust
// fields' format only.
package registry

import (
	"bytes"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

// ValidationErrorKind classifies a rejected document into the two failure
// classes of TS-014-01-02: malformed (not decodable JSON, wrong types) and
// schema-invalid (violates a TS-014-01-01 rule).
type ValidationErrorKind string

const (
	// ValidationErrorKindMalformed marks a document that cannot be decoded
	// or carries a field of the wrong type.
	ValidationErrorKindMalformed ValidationErrorKind = "malformed"

	// ValidationErrorKindInvalid marks a decodable document that violates
	// one or more schema rules (TS-014-01-01).
	ValidationErrorKindInvalid ValidationErrorKind = "invalid"
)

// ValidationError identifies one rejection of a registry metadata
// document: the offending field path, the violated rule, and an actionable
// message. Field paths follow the document structure (for example
// "trust.contentDigests[0].digest"); Rule identifies the schema rule or
// strict-format check (for example "pattern", "enum", "if-then",
// "format", "digest-encoding").
type ValidationError struct {
	// Kind classifies the failure as malformed or schema-invalid.
	Kind ValidationErrorKind

	// Field is the path of the offending value inside the document.
	Field string

	// Rule is the identifier of the violated rule (schema keyword or
	// strict-format check).
	Rule string

	// Message is a human-readable, actionable explanation.
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s (rule %s, %s)", e.Field, e.Message, e.Rule, e.Kind)
}

// ParseError reports that a registry metadata document was rejected. It
// aggregates every ValidationError found in the document so the caller can
// fix all problems in one pass.
type ParseError struct {
	// Errors lists every rejection found in the document, in a stable
	// document order.
	Errors []ValidationError
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	if len(e.Errors) == 0 {
		return "registry metadata document is invalid"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "registry metadata document rejected (%d problem%s):",
		len(e.Errors), plural(len(e.Errors)))
	for _, ve := range e.Errors {
		sb.WriteString("\n  - ")
		sb.WriteString(ve.Error())
	}
	return sb.String()
}

// Is allows errors.Is matching for ParseError: any *ParseError value
// matches the target type.
func (e *ParseError) Is(target error) bool {
	_, ok := target.(*ParseError)
	return ok
}

// Warning is an advisory note on a document that parses and validates but
// SHOULD be corrected. Warnings never fail the document (PM decision D-03).
type Warning struct {
	// Field is the path of the value the warning concerns.
	Field string

	// Rule identifies the advisory.
	Rule string

	// Message is a human-readable explanation.
	Message string
}

// ParseResult is the outcome of a successful Parse: the parsed metadata
// and any advisory warnings.
type ParseResult struct {
	// Metadata is the parsed metadata document.
	Metadata *Metadata

	// Warnings lists advisory notes (for example a deprecated release
	// without an announced removal date, PM decision D-03).
	Warnings []Warning
}

// Parse parses and validates one registry metadata document. It returns
// the parsed metadata and any advisory warnings, or a *ParseError
// describing every malformed or schema-invalid problem found.
//
// Validation surface (TS-014-01-01, registry-metadata.schema.json):
//
//   - required fields, field types, patterns, max/min lengths,
//     additionalProperties: false, enums, minItems/uniqueItems, and the
//     lifecycle if/then constraint (removalDate only on deprecated);
//   - the strict format layer the schema marks advisory: https-only
//     distribution.location (no whitespace, control characters, or
//     non-absolute URLs), RFC-3339 date-time lifecycle.removalDate,
//     strict RFC-4648 decoding of trust.contentDigests entries with a
//     decoded length of exactly 32 bytes (SHA-256) and canonical pad
//     bits, and strict RFC-4648 base64 shape (alphabet, padding, pad
//     bits) for trust.attestation.signature and
//     trust.attestation.publicKey;
//   - the optional additive skills section (TS-021-04; ADR-037 D2):
//     per-skill name/version/asset shape, name uniqueness, and the
//     asset↔named-digest binding (every skill asset must be covered by
//     an attested named entry in trust.contentDigests, TS-014-04-04);
//   - forward compatibility: unknown-but-optional root sections
//     (object- or array-valued keys the schema does not declare) are
//     accepted and ignored within the deprecation window; unknown root
//     keys with scalar values remain rejected (PM-approved decision,
//     registry-metadata.md §4.8).
func Parse(data []byte) (*ParseResult, error) {
	root, errs := decodeRoot(data)
	if root == nil {
		return nil, &ParseError{Errors: errs}
	}

	md := &Metadata{}
	var warnings []Warning

	// Root additionalProperties: core fields are declared exactly; unknown
	// root keys with scalar values are rejected, while unknown-but-optional
	// root SECTIONS (object- or array-valued keys) are tolerated within the
	// deprecation window per the recorded forward-compat decision
	// (TS-021-04; registry-metadata.md §4.8).
	rejectUnknownRootFields(root, rootFieldNames, &errs)

	// Optional annotation fields (§4: they carry no validation semantics).
	md.Schema = optionalString(root, "$schema", "$schema", &errs)
	md.Title = optionalString(root, "title", "title", &errs)
	md.Description = optionalString(root, "description", "description", &errs)

	// Required identity, version, and declared contract version (§4.1–§4.3).
	id := requiredString(root, "id", "id", &errs)
	md.ID = id.value
	if id.ok {
		checkPattern(id.value, reID, "id", "the corpus id convention: lowercase alphanumeric with hyphens (^[a-z0-9][a-z0-9-]*$)", &errs)
		checkMaxLength(id.value, 64, "id", &errs)
	}
	version := requiredString(root, "version", "version", &errs)
	md.Version = version.value
	if version.ok {
		checkPattern(version.value, reSemver, "version", "semver without leading zeros (^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$)", &errs)
		checkMaxLength(version.value, 64, "version", &errs)
	}
	contractVersion := requiredString(root, "contractVersion", "contractVersion", &errs)
	md.ContractVersion = contractVersion.value
	if contractVersion.ok {
		checkPattern(contractVersion.value, reSemver, "contractVersion", "semver without leading zeros (^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$)", &errs)
		checkMaxLength(contractVersion.value, 64, "contractVersion", &errs)
	}

	// Capability declaration (§4.4): the framework-version support scope.
	if capObj, ok := objectField(root, "capability", "capability", &errs); ok {
		rejectUnknownFields(capObj, []string{"frameworkVersion"}, "capability", &errs)
		if items, present := arrayField(capObj, "frameworkVersion", "capability.frameworkVersion", &errs); present {
			if len(items) == 0 {
				errs = append(errs, ValidationError{
					Kind:    ValidationErrorKindInvalid,
					Field:   "capability.frameworkVersion",
					Rule:    "minItems",
					Message: "must contain at least one supported framework version (ADR-021 §3.2; PRD-002 §5.8)",
				})
			}
			seen := make(map[string]bool, len(items))
			for i, item := range items {
				path := itemPath("capability.frameworkVersion", i)
				var value string
				if err := json.Unmarshal(item, &value); err != nil {
					errs = append(errs, typeError(path, "string", err))
					continue
				}
				checkPattern(value, reFrameworkVersion, path, "a framework version in the command-contract convention (^[0-9]+\\.[0-9]+\\.[0-9]+$)", &errs)
				if seen[value] {
					errs = append(errs, ValidationError{
						Kind:    ValidationErrorKindInvalid,
						Field:   path,
						Rule:    "uniqueItems",
						Message: fmt.Sprintf("duplicate framework version %q — the support scope is a set (uniqueItems)", value),
					})
				}
				seen[value] = true
			}
			md.Capability.FrameworkVersion = stringsSlice(items)
		}
	}

	// Distribution location (§4.5): channel pattern + https-only location.
	if distObj, ok := objectField(root, "distribution", "distribution", &errs); ok {
		rejectUnknownFields(distObj, []string{"type", "location"}, "distribution", &errs)
		distType := requiredString(distObj, "type", "distribution.type", &errs)
		md.Distribution.Type = distType.value
		if distType.ok {
			if distType.value != DistributionTypeGitHubReleases {
				errs = append(errs, enumError("distribution.type", distType.value, []string{DistributionTypeGitHubReleases}))
			}
		}
		location := requiredString(distObj, "location", "distribution.location", &errs)
		md.Distribution.Location = location.value
		if location.ok {
			checkPattern(location.value, reHTTPSLocation, "distribution.location", "an https URL — the scheme is pinned to https (^https://), no plaintext or other scheme (ADR-030 §3)", &errs)
			checkHTTPSURL(location.value, "distribution.location", &errs)
		}
	}

	// Lifecycle state (§4.6): governed states + removal date cross-field.
	if lifeObj, ok := objectField(root, "lifecycle", "lifecycle", &errs); ok {
		rejectUnknownFields(lifeObj, []string{"state", "removalDate"}, "lifecycle", &errs)
		state := requiredString(lifeObj, "state", "lifecycle.state", &errs)
		md.Lifecycle.State = state.value
		if state.ok {
			if !slices.Contains(lifecycleStates, state.value) {
				errs = append(errs, enumError("lifecycle.state", state.value, lifecycleStates))
			}
		}
		md.Lifecycle.RemovalDate = optionalString(lifeObj, "removalDate", "lifecycle.removalDate", &errs)
		if removal := fieldString(lifeObj, "removalDate"); removal.ok {
			if !state.ok || state.value != LifecycleStateDeprecated {
				errs = append(errs, ValidationError{
					Kind:    ValidationErrorKindInvalid,
					Field:   "lifecycle.removalDate",
					Rule:    "if-then",
					Message: "removalDate is only allowed when lifecycle.state is \"deprecated\" — it announces the removal of a Deprecated standard and has no meaning for a published or retired entry (TS-014-01-01 if/then; ADR-027 §3)",
				})
			}
			if _, err := time.Parse(time.RFC3339Nano, removal.value); err != nil {
				errs = append(errs, ValidationError{
					Kind:    ValidationErrorKindInvalid,
					Field:   "lifecycle.removalDate",
					Rule:    "format",
					Message: "must be an RFC 3339 date-time (ISO 8601), e.g. \"2027-01-31T00:00:00Z\" (strict format check — the schema annotation is advisory)",
				})
			}
		}
		if state.ok && state.value == LifecycleStateDeprecated {
			if _, present := lifeObj["removalDate"]; !present {
				warnings = append(warnings, Warning{
					Field:   "lifecycle.removalDate",
					Rule:    "advisory",
					Message: "deprecated release SHOULD carry removalDate once the removal date is announced (PM decision D-03)",
				})
			}
		}
	}

	// Trust fields (§4.7): content digests + publisher attestation.
	// namedAssets collects the names of attested named digest entries
	// (TS-014-04-04); the optional skills section binds every declared
	// skill asset to one of them (TS-021-04; ADR-037 D2).
	namedAssets := make(map[string]bool)
	if trustObj, ok := objectField(root, "trust", "trust", &errs); ok {
		rejectUnknownFields(trustObj, []string{"contentDigests", "attestation"}, "trust", &errs)
		if items, present := arrayField(trustObj, "contentDigests", "trust.contentDigests", &errs); present {
			if len(items) == 0 {
				errs = append(errs, ValidationError{
					Kind:    ValidationErrorKindInvalid,
					Field:   "trust.contentDigests",
					Rule:    "minItems",
					Message: "must contain at least one digest — a release without integrity material cannot be verified and is rejected (ADR-022 §3; ADR-023 §3)",
				})
			}
			seen := make(map[string]bool, len(items))
			seenNames := make(map[string]bool, len(items))
			for i, item := range items {
				path := itemPath("trust.contentDigests", i)
				digestObj, ok := decodeObject(item, path, &errs)
				if !ok {
					continue
				}
				rejectUnknownFields(digestObj, []string{"algorithm", "encoding", "digest", "name"}, path, &errs)
				algorithm := requiredString(digestObj, "algorithm", path+".algorithm", &errs)
				if algorithm.ok && algorithm.value != DigestAlgorithmSHA256 {
					errs = append(errs, enumError(path+".algorithm", algorithm.value, []string{DigestAlgorithmSHA256}))
				}
				encoding := requiredString(digestObj, "encoding", path+".encoding", &errs)
				if encoding.ok && !slices.Contains(digestEncodings, encoding.value) {
					errs = append(errs, enumError(path+".encoding", encoding.value, digestEncodings))
				}
				digest := requiredString(digestObj, "digest", path+".digest", &errs)
				if digest.ok && digest.value == "" {
					errs = append(errs, minLengthError(path+".digest", "a missing or empty digest means the release has no integrity material (ADR-022 §3)"))
				}
				if digest.ok && encoding.ok && digest.value != "" && slices.Contains(digestEncodings, encoding.value) {
					checkDigestValue(digest.value, encoding.value, path+".digest", &errs)
				}
				// name binds the entry to a named release asset of the
				// same release (TS-014-04-04): optional, restricted to
				// safe asset identifiers, and unique — two entries can
				// never claim the same asset.
				name := optionalString(digestObj, "name", path+".name", &errs)
				if name != "" {
					checkPattern(name, reAssetName, path+".name", "a release asset name: lowercase alphanumeric with hyphens (^[a-z0-9][a-z0-9-]*$) — the name must never escape the release channel as a path component", &errs)
					checkMaxLength(name, 128, path+".name", &errs)
					if seenNames[name] {
						errs = append(errs, ValidationError{
							Kind:    ValidationErrorKindInvalid,
							Field:   path + ".name",
							Rule:    "uniqueItems",
							Message: fmt.Sprintf("duplicate asset name %q — two entries cannot bind the same release asset (each named entry declares the digest of one asset)", name),
						})
					}
					seenNames[name] = true
					// The name is collected even when the digest value is
					// malformed: the malformed-digest error already names
					// the real problem, and the skills binding must not
					// double-report on it.
					namedAssets[name] = true
				}
				key := algorithm.value + "\x00" + encoding.value + "\x00" + digest.value
				if algorithm.ok && encoding.ok && digest.ok {
					if seen[key] {
						errs = append(errs, ValidationError{
							Kind:    ValidationErrorKindInvalid,
							Field:   path,
							Rule:    "uniqueItems",
							Message: "duplicate digest entry — contentDigests entries must be unique (uniqueItems)",
						})
					}
					seen[key] = true
				}
				md.Trust.ContentDigests = append(md.Trust.ContentDigests, ContentDigest{
					Algorithm: algorithm.value,
					Encoding:  encoding.value,
					Digest:    digest.value,
					Name:      name,
				})
			}
		}
		if attObj, ok := objectField(trustObj, "attestation", "trust.attestation", &errs); ok {
			rejectUnknownFields(attObj, []string{"algorithm", "signature", "publicKey"}, "trust.attestation", &errs)
			algorithm := requiredString(attObj, "algorithm", "trust.attestation.algorithm", &errs)
			md.Trust.Attestation.Algorithm = algorithm.value
			if algorithm.ok && algorithm.value != AttestationAlgorithmEd25519 {
				errs = append(errs, enumError("trust.attestation.algorithm", algorithm.value, []string{AttestationAlgorithmEd25519}))
			}
			signature := requiredString(attObj, "signature", "trust.attestation.signature", &errs)
			md.Trust.Attestation.Signature = signature.value
			if signature.ok {
				if signature.value == "" {
					errs = append(errs, minLengthError("trust.attestation.signature", "a missing or empty signature means the release carries no publisher attestation (ADR-022 §3; ADR-023 §3)"))
				} else {
					checkAttestationBase64(signature.value, "trust.attestation.signature", &errs)
				}
			}
			publicKey := requiredString(attObj, "publicKey", "trust.attestation.publicKey", &errs)
			md.Trust.Attestation.PublicKey = publicKey.value
			if publicKey.ok {
				if publicKey.value == "" {
					errs = append(errs, minLengthError("trust.attestation.publicKey", "without a verification public key the attestation is unverifiable and the release is invalid (ADR-022 §3)"))
				} else {
					checkAttestationBase64(publicKey.value, "trust.attestation.publicKey", &errs)
				}
			}
		}
	}

	// Skills declaration (§4.8; optional additive section, TS-021-04;
	// ADR-037 D2): the standard's per-skill assets. The section is
	// additive-only — a release without skills[] behaves exactly as
	// before — and its shape is strict: malformed declarations (invalid
	// name, missing asset, invalid or unbound digest) are rejected with
	// actionable errors. Each skill's asset must be covered by an attested
	// named digest entry in trust.contentDigests (TS-014-04-04), so skill
	// content is bound to the publisher attestation like every other named
	// release asset.
	if items, present := optionalArrayField(root, "skills", "skills", &errs); present && len(items) > 0 {
		seenNames := make(map[string]bool, len(items))
		for i, item := range items {
			path := itemPath("skills", i)
			skillObj, ok := decodeObject(item, path, &errs)
			if !ok {
				continue
			}
			rejectUnknownFields(skillObj, []string{"name", "version", "asset", "description"}, path, &errs)
			name := requiredString(skillObj, "name", path+".name", &errs)
			if name.ok {
				checkPattern(name.value, reSkillName, path+".name", "a skill name: lowercase alphanumeric with hyphens (^[a-z0-9][a-z0-9-]*$), the corpus id convention — the name is the install target of anvil skill install and the namespace component of skills/<standard-id>/<name> (ADR-037 §7)", &errs)
				checkMaxLength(name.value, 64, path+".name", &errs)
				if seenNames[name.value] {
					errs = append(errs, ValidationError{
						Kind:    ValidationErrorKindInvalid,
						Field:   path + ".name",
						Rule:    "uniqueItems",
						Message: fmt.Sprintf("duplicate skill name %q — one release declares each skill name at most once (anvil skill install targets the name)", name.value),
					})
				}
				seenNames[name.value] = true
			}
			version := requiredString(skillObj, "version", path+".version", &errs)
			if version.ok {
				checkPattern(version.value, reSemver, path+".version", "semver without leading zeros (^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$) — skills are versioned content; the version is part of the per-skill asset identifier (anvil-skill-<name>-<version>, dots normalized to hyphens)", &errs)
				checkMaxLength(version.value, 64, path+".version", &errs)
			}
			asset := requiredString(skillObj, "asset", path+".asset", &errs)
			if asset.ok {
				checkPattern(asset.value, reAssetName, path+".asset", "a release asset name: lowercase alphanumeric with hyphens (^[a-z0-9][a-z0-9-]*$, no dots or slashes) — the skill content asset identifier (e.g. anvil-skill-overview-1-0-0) must never escape the release channel as a path component", &errs)
				checkMaxLength(asset.value, 128, path+".asset", &errs)
				// Cross-field binding (TS-014-04-04; ADR-037 D2): the asset
				// must be covered by an attested named digest entry in
				// trust.contentDigests. Draft-07 cannot express an array
				// cross-reference, so the parser enforces the binding —
				// the same precedent as the contentDigests name-uniqueness
				// rule.
				if !namedAssets[asset.value] {
					errs = append(errs, ValidationError{
						Kind:    ValidationErrorKindInvalid,
						Field:   path + ".asset",
						Rule:    "binding",
						Message: fmt.Sprintf("skill asset %q is not covered by an attested named digest — trust.contentDigests must carry a named entry (TS-014-04-04) for every asset declared in skills[] (ADR-037 D2)", asset.value),
					})
				}
			}
			description := optionalString(skillObj, "description", path+".description", &errs)
			md.Skills = append(md.Skills, Skill{
				Name:        name.value,
				Version:     version.value,
				Asset:       asset.value,
				Description: description,
			})
		}
	}

	if len(errs) > 0 {
		return nil, &ParseError{Errors: errs}
	}
	return &ParseResult{Metadata: md, Warnings: warnings}, nil
}

// Root schema field names and machine-value sets (TS-014-01-01).
var (
	rootFieldNames = []string{
		"$schema", "title", "description",
		"id", "version", "contractVersion",
		"capability", "distribution", "lifecycle", "trust",
		// skills is the optional additive section (TS-021-04; ADR-037 D2).
		"skills",
	}

	lifecycleStates = []string{
		LifecycleStatePublished,
		LifecycleStateDeprecated,
		LifecycleStateRetired,
	}

	digestEncodings = []string{
		DigestEncodingBase16,
		DigestEncodingBase32,
		DigestEncodingBase64,
	}

	// Schema patterns (registry-metadata.schema.json).
	reID               = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	reSemver           = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	reFrameworkVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	reHTTPSLocation    = regexp.MustCompile(`^https://`)
	reDigestBase16     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	reDigestBase32     = regexp.MustCompile(`^[A-Z2-7]{52,56}=*$`)
	reDigestBase64     = regexp.MustCompile(`^[A-Za-z0-9+/]{43}=$`)
	// reAssetName is the optional contentDigest.name pattern
	// (TS-014-04-04): safe asset identifiers only, so a name can never
	// escape the release channel as a path component.
	reAssetName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	// reSkillName is the optional skills[].name pattern (TS-021-04;
	// ADR-037 D2): safe skill names following the corpus id convention —
	// the install target of anvil skill install and the namespace
	// component of skills/<standard-id>/<name> (ADR-037 §7).
	reSkillName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// decodeRoot decodes the document into a field map. A nil map with a
// non-empty error list means the document was rejected before parsing.
func decodeRoot(data []byte) (map[string]json.RawMessage, []ValidationError) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return nil, []ValidationError{{
				Kind:    ValidationErrorKindMalformed,
				Field:   "document",
				Rule:    "type",
				Message: fmt.Sprintf("must be a JSON object, found %s", typeErr.Value),
			}}
		}
		return nil, []ValidationError{{
			Kind:    ValidationErrorKindMalformed,
			Field:   "document",
			Rule:    "json",
			Message: fmt.Sprintf("not decodable JSON: %v", err),
		}}
	}
	if root == nil {
		return nil, []ValidationError{{
			Kind:    ValidationErrorKindMalformed,
			Field:   "document",
			Rule:    "type",
			Message: "must be a JSON object, found null",
		}}
	}
	return root, nil
}

// fieldValue is the outcome of reading one document field.
type fieldValue struct {
	value string
	ok    bool
}

// isNull reports whether the raw value is the JSON null literal.
func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// fieldString reads a field as a string. A missing or null field yields ok
// == false without an error; a present field of another type also yields ok
// == false (the caller records the malformed type error through
// requiredString or optionalString).
func fieldString(obj map[string]json.RawMessage, key string) fieldValue {
	raw, present := obj[key]
	if !present || isNull(raw) {
		return fieldValue{}
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fieldValue{}
	}
	return fieldValue{value: value, ok: true}
}

// requiredString reads a required string field, recording a "required"
// error when the field is missing and a malformed "type" error when the
// value is not a string.
func requiredString(obj map[string]json.RawMessage, key, path string, errs *[]ValidationError) fieldValue {
	raw, present := obj[key]
	if !present {
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindInvalid,
			Field:   path,
			Rule:    "required",
			Message: "required field is missing (TS-014-01-01)",
		})
		return fieldValue{}
	}
	if isNull(raw) {
		*errs = append(*errs, nullTypeError(path, "string"))
		return fieldValue{}
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		*errs = append(*errs, typeError(path, "string", err))
		return fieldValue{}
	}
	return fieldValue{value: value, ok: true}
}

// optionalString reads an optional string field, recording a malformed
// "type" error only when a present value is not a string.
func optionalString(obj map[string]json.RawMessage, key, path string, errs *[]ValidationError) string {
	raw, present := obj[key]
	if !present {
		return ""
	}
	if isNull(raw) {
		*errs = append(*errs, nullTypeError(path, "string"))
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		*errs = append(*errs, typeError(path, "string", err))
		return ""
	}
	return value
}

// objectField reads an object-typed field. A missing field yields ok ==
// false with a "required" error; a present non-object value yields a
// malformed "type" error and ok == false.
func objectField(obj map[string]json.RawMessage, key, path string, errs *[]ValidationError) (map[string]json.RawMessage, bool) {
	raw, present := obj[key]
	if !present {
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindInvalid,
			Field:   path,
			Rule:    "required",
			Message: "required field is missing (TS-014-01-01)",
		})
		return nil, false
	}
	return decodeObject(raw, path, errs)
}

// decodeObject decodes one raw JSON value into a field map, recording a
// malformed "type" error when the value is not an object.
func decodeObject(raw json.RawMessage, path string, errs *[]ValidationError) (map[string]json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		*errs = append(*errs, typeError(path, "object", err))
		return nil, false
	}
	if obj == nil {
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindMalformed,
			Field:   path,
			Rule:    "type",
			Message: fmt.Sprintf("must be an object, found null"),
		})
		return nil, false
	}
	return obj, true
}

// arrayField reads an array-typed field. A missing field yields ok == false
// with a "required" error; a present non-array value yields a malformed
// "type" error and ok == false.
func arrayField(obj map[string]json.RawMessage, key, path string, errs *[]ValidationError) ([]json.RawMessage, bool) {
	raw, present := obj[key]
	if !present {
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindInvalid,
			Field:   path,
			Rule:    "required",
			Message: "required field is missing (TS-014-01-01)",
		})
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		*errs = append(*errs, typeError(path, "array", err))
		return nil, false
	}
	if items == nil {
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindMalformed,
			Field:   path,
			Rule:    "type",
			Message: fmt.Sprintf("must be an array, found null"),
		})
		return nil, false
	}
	return items, true
}

// optionalArrayField reads an optional array-typed field. A missing field
// yields present == false with no error — the field is optional, so absence
// is not a problem (TS-021-04: the skills section). A present non-array
// value yields a malformed "type" error and present == false.
func optionalArrayField(obj map[string]json.RawMessage, key, path string, errs *[]ValidationError) ([]json.RawMessage, bool) {
	raw, present := obj[key]
	if !present {
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		*errs = append(*errs, typeError(path, "array", err))
		return nil, false
	}
	if items == nil {
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindMalformed,
			Field:   path,
			Rule:    "type",
			Message: fmt.Sprintf("must be an array, found null"),
		})
		return nil, false
	}
	return items, true
}

// stringsSlice converts raw array items into strings, skipping items that
// are not strings (they already produced a type error upstream).
func stringsSlice(items []json.RawMessage) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		var value string
		if err := json.Unmarshal(item, &value); err == nil {
			out = append(out, value)
		}
	}
	return out
}

// rejectUnknownFields enforces additionalProperties: false at one object
// level: every key must be declared by the schema.
func rejectUnknownFields(obj map[string]json.RawMessage, allowed []string, path string, errs *[]ValidationError) {
	for _, key := range sortedKeys(obj) {
		if !slices.Contains(allowed, key) {
			*errs = append(*errs, ValidationError{
				Kind:    ValidationErrorKindInvalid,
				Field:   joinFieldPath(path, key),
				Rule:    "additionalProperties",
				Message: fmt.Sprintf("unknown field %q — the schema declares exactly %s (TS-014-01-01 additionalProperties: false)", key, strings.Join(allowed, ", ")),
			})
		}
	}
}

// rejectUnknownRootFields enforces the root additionalProperties of the
// schema (TS-021-04 forward-compat decision, registry-metadata.md §4.8):
// core fields stay strict, and unknown root keys with scalar values are
// rejected — the runtime-agnostic requirement stays enforceable (a
// Go-runtime-typed scalar such as golangVersion must still fail). Unknown
// root keys whose value is an object or an array are accepted and ignored:
// they are unknown-but-optional additive sections, tolerated within the
// deprecation window so an older parser meeting a newer additive section
// does not reject an otherwise-valid document (ADR-024 spirit; ADR-037
// §6). Nested strictness (rejectUnknownFields) is unchanged.
func rejectUnknownRootFields(obj map[string]json.RawMessage, allowed []string, errs *[]ValidationError) {
	for _, key := range sortedKeys(obj) {
		if slices.Contains(allowed, key) {
			continue
		}
		if isSectionValue(obj[key]) {
			continue
		}
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindInvalid,
			Field:   key,
			Rule:    "additionalProperties",
			Message: fmt.Sprintf("unknown field %q — the schema declares exactly %s; unknown optional sections (object- or array-valued keys) are tolerated within the deprecation window, scalar values cannot be a section (TS-014-01-01 additionalProperties; TS-021-04 forward-compat decision)", key, strings.Join(allowed, ", ")),
		})
	}
}

// isSectionValue reports whether a raw JSON value has the shape of an
// additive optional section: an object or an array (TS-021-04). Scalar
// values — strings, numbers, booleans, null — are fields, not sections.
func isSectionValue(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	switch trimmed[0] {
	case '{', '[':
		return true
	default:
		return false
	}
}

// checkPattern records a "pattern" error when the value does not match the
// schema pattern.
func checkPattern(value string, re *regexp.Regexp, path, description string, errs *[]ValidationError) {
	if !re.MatchString(value) {
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindInvalid,
			Field:   path,
			Rule:    "pattern",
			Message: fmt.Sprintf("does not match the required pattern: %s", description),
		})
	}
}

// checkMaxLength records a "maxLength" error when the value exceeds the
// schema bound.
func checkMaxLength(value string, max int, path string, errs *[]ValidationError) {
	if len(value) > max {
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindInvalid,
			Field:   path,
			Rule:    "maxLength",
			Message: fmt.Sprintf("must be at most %d characters", max),
		})
	}
}

// checkHTTPSURL records a "format" error when the location is not a
// well-formed absolute https URL: no whitespace or control characters, the
// scheme pinned to https, and a non-empty host (the strict enforcement of
// the advisory format: uri annotation; ADR-030 §3).
func checkHTTPSURL(location, path string, errs *[]ValidationError) {
	for _, r := range location {
		if r < 0x20 || unicode.IsSpace(r) {
			*errs = append(*errs, ValidationError{
				Kind:    ValidationErrorKindInvalid,
				Field:   path,
				Rule:    "format",
				Message: "must not contain whitespace or control characters — the location is a resolvable https URL, not free text (strict format: uri check; ADR-030 §3)",
			})
			return
		}
	}
	u, err := url.Parse(location)
	if err != nil || u.Scheme != "https" || u.Host == "" || !u.IsAbs() {
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindInvalid,
			Field:   path,
			Rule:    "format",
			Message: "must be a well-formed absolute https URL with a host — the scheme is pinned to https, no plaintext or other scheme (strict format: uri check; ADR-030 §3)",
		})
		return
	}
	// Userinfo (https://user:pass@host/...) is rejected: credentials would
	// be sent as Basic auth on fetch, echoed in error messages, and
	// persisted in the installed-standard record (ADR-022 §3 — the
	// resolution is explicit and recorded). The schema pattern ^https://
	// does not exclude userinfo, so this is a parse-layer strict rule like
	// the other strict format rules (TS-014-01-02).
	if u.User != nil {
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindInvalid,
			Field:   path,
			Rule:    "format",
			Message: "must not contain userinfo (username or password) — credentials embedded in the location would be sent as Basic auth and recorded in the installed-standard record; publish the release content at a location without userinfo (strict format: uri check; ADR-030 §3)",
		})
	}
}

// checkDigestValue enforces the per-encoding digest constraints of
// registry-metadata.schema.json §contentDigest: the schema pattern for the
// declared encoding, strict RFC-4648 decoding, and a decoded length of
// exactly 32 bytes (SHA-256).
func checkDigestValue(value, encoding, path string, errs *[]ValidationError) {
	switch encoding {
	case DigestEncodingBase16:
		if !reDigestBase16.MatchString(value) {
			checkPattern(value, reDigestBase16, path, "exactly 64 lowercase hex characters (^[0-9a-f]{64}$)", errs)
			return
		}
		decoded, err := hex.DecodeString(value)
		if err != nil {
			*errs = append(*errs, digestEncodingError(path, "base16", err))
			return
		}
		checkDecodedDigestLength(decoded, path, errs)
	case DigestEncodingBase32:
		if !reDigestBase32.MatchString(value) {
			checkPattern(value, reDigestBase32, path, "the RFC-4648 base32 encoding of a 32-byte digest: 52 data characters plus up to 4 padding '=' characters (^[A-Z2-7]{52,56}=*$)", errs)
			return
		}
		decoded, err := base32.StdEncoding.DecodeString(value)
		if err != nil {
			*errs = append(*errs, digestEncodingError(path, "base32", err))
			return
		}
		checkDecodedDigestLength(decoded, path, errs)
		// Go's base32 decoder tolerates excess padding (the schema pattern
		// allows any number of '='); strict RFC-4648 requires the exact
		// canonical padding, so the value must re-encode to itself.
		if base32.StdEncoding.EncodeToString(decoded) != value {
			*errs = append(*errs, ValidationError{
				Kind:    ValidationErrorKindInvalid,
				Field:   path,
				Rule:    "digest-encoding",
				Message: "not the canonical RFC-4648 base32 encoding of a 32-byte digest (52 data characters plus exactly 4 padding '=' characters)",
			})
		}
	case DigestEncodingBase64:
		if !reDigestBase64.MatchString(value) {
			checkPattern(value, reDigestBase64, path, "the RFC-4648 standard (padded) base64 encoding of a 32-byte digest: 43 data characters plus one '=' padding (^[A-Za-z0-9+/]{43}=$)", errs)
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			*errs = append(*errs, digestEncodingError(path, "base64", err))
			return
		}
		checkDecodedDigestLength(decoded, path, errs)
		// Symmetric with base32: the decoded bytes must re-encode to
		// exactly the declared value. This rejects non-canonical
		// encodings — non-zero pad bits (Go's decoder tolerates them) and
		// case variants (base64 is case-sensitive: a lowercase rendering
		// is a different digest value).
		if base64.StdEncoding.EncodeToString(decoded) != value {
			*errs = append(*errs, ValidationError{
				Kind:    ValidationErrorKindInvalid,
				Field:   path,
				Rule:    "digest-encoding",
				Message: "not the canonical RFC-4648 base64 encoding of a 32-byte digest — the pad bits must be zero (43 data characters plus one '=' padding)",
			})
		}
	}
}

// checkAttestationBase64 enforces the strict RFC-4648 base64 shape of an
// attestation value (registry-metadata.md §4.7: signature and publicKey
// are base64-encoded, RFC-4648 standard with padding): the standard
// alphabet with exact padding, and canonical pad bits — the decoded bytes
// must re-encode to the value up to letter case. Case is tolerated here
// (unlike digests) because the corpus fixtures and the schema pattern
// permit lowercase RFC-4648 base64 and case does not change the decoded
// material; non-zero pad bits (which Go's decoder tolerates) are rejected.
// This is a parse-time fail-fast check: a malformed signature or key makes
// the attestation unverifiable, so the document is rejected here, before
// any trust verification consumes it.
func checkAttestationBase64(value, path string, errs *[]ValidationError) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindInvalid,
			Field:   path,
			Rule:    "encoding",
			Message: fmt.Sprintf("must be strict RFC-4648 base64 (standard alphabet with padding): %v", err),
		})
		return
	}
	if !strings.EqualFold(base64.StdEncoding.EncodeToString(decoded), value) {
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindInvalid,
			Field:   path,
			Rule:    "encoding",
			Message: "not the canonical RFC-4648 base64 encoding — the pad bits must be zero",
		})
	}
}

// checkDecodedDigestLength records a "digest-length" error when a decoded
// digest is not exactly 32 bytes (SHA-256).
func checkDecodedDigestLength(decoded []byte, path string, errs *[]ValidationError) {
	if len(decoded) != 32 {
		*errs = append(*errs, ValidationError{
			Kind:    ValidationErrorKindInvalid,
			Field:   path,
			Rule:    "digest-length",
			Message: fmt.Sprintf("decodes to %d bytes, want exactly 32 bytes (SHA-256)", len(decoded)),
		})
	}
}

// digestEncodingError builds the strict RFC-4648 decoding failure error.
func digestEncodingError(path, encoding string, err error) ValidationError {
	return ValidationError{
		Kind:    ValidationErrorKindInvalid,
		Field:   path,
		Rule:    "digest-encoding",
		Message: fmt.Sprintf("not decodable as strict RFC-4648 %s: %v", encoding, err),
	}
}

// enumError builds the "enum" error for a value outside the allowed set.
func enumError(path, value string, allowed []string) ValidationError {
	return ValidationError{
		Kind:    ValidationErrorKindInvalid,
		Field:   path,
		Rule:    "enum",
		Message: fmt.Sprintf("%q is not a supported value — must be one of %s (TS-014-01-01 enum)", value, strings.Join(allowed, ", ")),
	}
}

// minLengthError builds the "minLength" error for an empty value.
func minLengthError(path, why string) ValidationError {
	return ValidationError{
		Kind:    ValidationErrorKindInvalid,
		Field:   path,
		Rule:    "minLength",
		Message: fmt.Sprintf("must not be empty — %s", why),
	}
}

// typeError builds the malformed "type" error for a field of the wrong
// type, naming the type actually found when the decoder reports it.
func typeError(path, want string, err error) ValidationError {
	message := fmt.Sprintf("must be %s", article(want))
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Value != "" {
		message += fmt.Sprintf(", found %s", typeErr.Value)
	}
	return ValidationError{
		Kind:    ValidationErrorKindMalformed,
		Field:   path,
		Rule:    "type",
		Message: message,
	}
}

// nullTypeError builds the malformed "type" error for a field holding the
// JSON null literal.
func nullTypeError(path, want string) ValidationError {
	return ValidationError{
		Kind:    ValidationErrorKindMalformed,
		Field:   path,
		Rule:    "type",
		Message: fmt.Sprintf("must be %s, found null", article(want)),
	}
}

// article renders a type name with its indefinite article for messages.
func article(want string) string {
	switch want {
	case "object", "array":
		return "an " + want
	default:
		return "a " + want
	}
}

// joinFieldPath joins a section path and a field key into a document path.
func joinFieldPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// itemPath formats the path of an array item.
func itemPath(base string, i int) string {
	return fmt.Sprintf("%s[%d]", base, i)
}

// sortedKeys returns the map keys in lexical order, keeping reported
// errors deterministic.
func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// plural returns "s" for counts other than one.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
