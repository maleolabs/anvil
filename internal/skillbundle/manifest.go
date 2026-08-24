package skillbundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The skill bundle manifest document (TS-021-01; ADR-037 D1/D2;
// skill-bundle-format.md §4).
//
// A skill bundle is an `anvil-skill-<name>-<version>.tar.gz` archive
// (ADR-037 D2) carrying exactly:
//
//  1. "manifest.json"  — the manifest document described in this file
//     (first entry, exactly once);
//  2. the skill content tree — regular files under "<name>/", the content
//     root, exactly the inventory declared by the manifest's files[].
//
// The manifest is the bundle's identity card and inventory: name and
// version (the bundle identity, also encoded in the asset file name),
// source (the standard-id the skill ships with — "anvil" for core skills,
// the standard id otherwise), contractVersion (the skill-bundle-format
// contract the bundle targets), description, and files[] (the exact
// content inventory). ParseManifest is the only manifest consumption
// entry point; every bundle passes the same strict parse.

// Manifest format constants (skill-bundle-format.md §4).
const (
	// ManifestFileName is the archive entry carrying the manifest
	// document. It must be the first archive entry, exactly once.
	ManifestFileName = "manifest.json"

	// MaxManifestSize caps the manifest document at 1 MiB, mirroring the
	// registry caps (MaxIndexDocumentSize, MaxBundleMetadataSize): a
	// manifest beyond the cap is a broken or hostile artifact, not a
	// valid bundle.
	MaxManifestSize = 1 << 20

	// MaxNameLength caps the manifest name and source identifiers.
	MaxNameLength = 64

	// MaxDescriptionLength caps the human-readable description.
	MaxDescriptionLength = 512

	// MaxFilePathLength caps one manifest files[] entry in bytes.
	MaxFilePathLength = 256

	// SupportedContractMajor is the skill-bundle-format contract major
	// this implementation consumes. Following ADR-024 §3.1, the contract
	// major is the unit of compatibility: a manifest declaring another
	// major was produced for a different format contract and is rejected
	// with an actionable message.
	SupportedContractMajor = 1
)

// Identifier and version patterns (skill-bundle-format.md §4.1, §4.2).
// They mirror the registry metadata conventions (registry-metadata.md
// §4.1 id, §4.3 version) so skill bundle identities are interchangeable
// with registry identities.
var (
	// reName matches the identifier convention: lowercase alphanumeric
	// with hyphens (^[a-z0-9][a-z0-9-]*$).
	reName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

	// reSemver matches semver without leading zeros
	// (^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$) — the exact
	// pattern of registry-metadata §4.3.
	reSemver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

// Manifest is the parsed skill bundle manifest: the bundle identity
// (name, version), the source standard-id, the format contract version,
// a human-readable description, and the exact content inventory.
type Manifest struct {
	// Name is the skill name: lowercase alphanumeric with hyphens
	// (^[a-z0-9][a-z0-9-]*$). It is both the bundle identity and the
	// content root directory name ("<name>/").
	Name string `json:"name"`

	// Version is the bundle version, semver without leading zeros
	// (major.minor.patch).
	Version string `json:"version"`

	// Source is the standard-id the skill ships with: "anvil" for core
	// skills, the standard id (e.g. "anvil-standard-laravel") otherwise.
	// It becomes the provenance header on the installed SKILL.md.
	Source string `json:"source"`

	// ContractVersion is the skill-bundle-format contract version the
	// bundle targets, semver; its major must be SupportedContractMajor.
	ContractVersion string `json:"contractVersion"`

	// Description is a human-readable summary of the skill.
	Description string `json:"description"`

	// Files is the exact content inventory: every content file of the
	// bundle, all under "<name>/", including "<name>/SKILL.md". The
	// archive must carry exactly this set (no extra, no missing).
	Files []string `json:"files"`
}

// SkillRoot returns the content root directory of the bundle — the
// directory that must contain SKILL.md — which is the manifest name.
func (m Manifest) SkillRoot() string { return m.Name }

// SkillMarkdownPath returns the canonical path of the skill's SKILL.md
// inside the bundle content tree.
func (m Manifest) SkillMarkdownPath() string { return m.Name + "/SKILL.md" }

// ManifestValidationErrorKind classifies one manifest rejection into
// malformed (not decodable JSON or a wrong field type) and invalid
// (decodable but violating a format rule).
type ManifestValidationErrorKind string

const (
	// ManifestValidationErrorKindMalformed marks a document that cannot be
	// decoded or carries a field of the wrong type.
	ManifestValidationErrorKindMalformed ManifestValidationErrorKind = "malformed"

	// ManifestValidationErrorKindInvalid marks a decodable document that
	// violates one or more format rules.
	ManifestValidationErrorKindInvalid ManifestValidationErrorKind = "invalid"
)

// ManifestValidationError identifies one rejection of a manifest
// document: the offending field, the violated rule, and an actionable
// message.
type ManifestValidationError struct {
	// Kind classifies the failure as malformed or invalid.
	Kind ManifestValidationErrorKind

	// Field is the path of the offending value inside the document.
	Field string

	// Rule identifies the violated rule.
	Rule string

	// Message is a human-readable, actionable explanation.
	Message string
}

// Error implements the error interface.
func (e *ManifestValidationError) Error() string {
	return fmt.Sprintf("%s: %s (rule %s, %s)", e.Field, e.Message, e.Rule, e.Kind)
}

// ManifestError reports that a manifest document was rejected. It
// aggregates every rejection so the caller can fix all problems in one
// pass.
type ManifestError struct {
	// Errors lists every rejection found in the document, in a stable
	// document order.
	Errors []ManifestValidationError
}

// Error implements the error interface.
func (e *ManifestError) Error() string {
	if len(e.Errors) == 0 {
		return "skill bundle manifest is invalid"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "skill bundle manifest rejected (%d problem%s):", len(e.Errors), plural(len(e.Errors)))
	for _, ve := range e.Errors {
		sb.WriteString("\n  - ")
		sb.WriteString(ve.Error())
	}
	return sb.String()
}

// Is allows errors.Is matching for ManifestError: any *ManifestError
// value matches the target type.
func (e *ManifestError) Is(target error) bool {
	_, ok := target.(*ManifestError)
	return ok
}

// ParseManifest parses and validates one manifest document. It returns
// the parsed manifest, or a *ManifestError describing every malformed or
// invalid problem found — all actionable, all fixable in one pass.
//
// Validation surface (skill-bundle-format.md §4):
//
//   - required fields name, version, source, contractVersion,
//     description, files; additionalProperties: false (unknown fields
//     rejected);
//   - name ^[a-z0-9][a-z0-9-]*$ (max 64), version semver without leading
//     zeros (max 64), source ^[a-z0-9][a-z0-9-]*$ (max 64),
//     contractVersion semver (max 64, supported major), description
//     non-empty (max 512);
//   - files[]: at least one entry, unique, each a safe relative path
//     (validateEntryPath) within "<name>/" of bounded length and depth;
//     exactly one entry must be "<name>/SKILL.md".
func ParseManifest(data []byte) (*Manifest, error) {
	root, errs := decodeManifestRoot(data)
	if root == nil {
		return nil, &ManifestError{Errors: errs}
	}

	md := &Manifest{}

	// Root additionalProperties: the format declares exactly these fields.
	rejectManifestUnknownFields(root, []string{"name", "version", "source", "contractVersion", "description", "files"}, "", &errs)

	// Required identity, version, and format contract (§4.1–§4.3).
	name := requiredManifestString(root, "name", "name", &errs)
	md.Name = name.value
	nameValid := false
	if name.ok {
		if !reName.MatchString(name.value) {
			errs = append(errs, ManifestValidationError{
				Kind:    ManifestValidationErrorKindInvalid,
				Field:   "name",
				Rule:    "pattern",
				Message: fmt.Sprintf("does not match the required pattern: the skill name convention, lowercase alphanumeric with hyphens (^[a-z0-9][a-z0-9-]*$), found %q", name.value),
			})
		} else {
			nameValid = true
		}
		errs = append(errs, manifestMaxLengthErrors("name", name.value, MaxNameLength)...)
	}

	version := requiredManifestString(root, "version", "version", &errs)
	md.Version = version.value
	if version.ok {
		checkManifestPattern(version.value, reSemver, "version", "semver without leading zeros (^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$)", &errs)
		errs = append(errs, manifestMaxLengthErrors("version", version.value, 64)...)
	}

	source := requiredManifestString(root, "source", "source", &errs)
	md.Source = source.value
	if source.ok {
		checkManifestPattern(source.value, reName, "source", "the source standard-id convention: lowercase alphanumeric with hyphens (^[a-z0-9][a-z0-9-]*$)", &errs)
		errs = append(errs, manifestMaxLengthErrors("source", source.value, MaxNameLength)...)
	}

	contractVersion := requiredManifestString(root, "contractVersion", "contractVersion", &errs)
	md.ContractVersion = contractVersion.value
	if contractVersion.ok {
		checkManifestPattern(contractVersion.value, reSemver, "contractVersion", "semver without leading zeros (^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$)", &errs)
		errs = append(errs, manifestMaxLengthErrors("contractVersion", contractVersion.value, 64)...)
		if major, ok := semverMajor(contractVersion.value); ok && major != SupportedContractMajor {
			errs = append(errs, ManifestValidationError{
				Kind:  ManifestValidationErrorKindInvalid,
				Field: "contractVersion",
				Rule:  "supported-major",
				Message: fmt.Sprintf("declares format contract major %d, but this implementation supports major %d (skill-bundle-format.md §4.3; ADR-024 §3.1 — the contract major is the unit of compatibility) — obtain a bundle produced for contract version %d.x",
					major, SupportedContractMajor, SupportedContractMajor),
			})
		}
	}

	description := requiredManifestString(root, "description", "description", &errs)
	md.Description = description.value
	if description.ok {
		if strings.TrimSpace(description.value) == "" {
			errs = append(errs, ManifestValidationError{
				Kind:    ManifestValidationErrorKindInvalid,
				Field:   "description",
				Rule:    "minLength",
				Message: "must not be empty — the manifest needs a human-readable description of the skill",
			})
		}
		errs = append(errs, manifestMaxLengthErrors("description", description.value, MaxDescriptionLength)...)
	}

	// Content inventory (§4.4).
	if items, present := manifestArrayField(root, "files", "files", &errs); present {
		if len(items) == 0 {
			errs = append(errs, ManifestValidationError{
				Kind:    ManifestValidationErrorKindInvalid,
				Field:   "files",
				Rule:    "minItems",
				Message: "must contain at least one entry — a skill bundle carries at least <name>/SKILL.md",
			})
		}
		seen := make(map[string]bool, len(items))
		for i, item := range items {
			path := itemPath("files", i)
			var value string
			if err := json.Unmarshal(item, &value); err != nil {
				errs = append(errs, manifestTypeError(path, "string", err))
				continue
			}
			if len(value) > MaxFilePathLength {
				errs = append(errs, ManifestValidationError{
					Kind:    ManifestValidationErrorKindInvalid,
					Field:   path,
					Rule:    "maxLength",
					Message: fmt.Sprintf("is %d bytes, exceeding the %d-byte cap on one content path", len(value), MaxFilePathLength),
				})
			}
			if seen[value] {
				errs = append(errs, ManifestValidationError{
					Kind:    ManifestValidationErrorKindInvalid,
					Field:   path,
					Rule:    "uniqueItems",
					Message: fmt.Sprintf("duplicate file entry %q — the content inventory is a set (uniqueItems)", value),
				})
			}
			seen[value] = true
		}
		md.Files = manifestStrings(items)
	}

	// Cross-field content-root rules (§4.4): every entry within
	// "<name>/", bounded depth, and "<name>/SKILL.md" present exactly
	// once. These run only when the name itself is valid, so a malformed
	// name is reported once and does not cascade.
	if nameValid {
		validateContentRoot(md, &errs)
	}

	if len(errs) > 0 {
		return nil, &ManifestError{Errors: errs}
	}
	return md, nil
}

// validateContentRoot enforces the content-inventory rules that depend on
// the (already validated) manifest name: every files[] entry must be a
// safe relative path within "<name>/" of bounded depth, and exactly one
// entry must be "<name>/SKILL.md".
func validateContentRoot(md *Manifest, errs *[]ManifestValidationError) {
	root := md.SkillRoot() + "/"
	for i, f := range md.Files {
		path := itemPath("files", i)
		if err := validateEntryPath(f); err != nil {
			*errs = append(*errs, ManifestValidationError{
				Kind:    ManifestValidationErrorKindInvalid,
				Field:   path,
				Rule:    "path",
				Message: err.Error(),
			})
			continue
		}
		if depth := pathDepth(f); depth > MaxPathDepth {
			*errs = append(*errs, ManifestValidationError{
				Kind:    ManifestValidationErrorKindInvalid,
				Field:   path,
				Rule:    "maxDepth",
				Message: fmt.Sprintf("is %d components deep, exceeding the %d-component depth cap", depth, MaxPathDepth),
			})
		}
		if !strings.HasPrefix(f, root) {
			*errs = append(*errs, ManifestValidationError{
				Kind:    ManifestValidationErrorKindInvalid,
				Field:   path,
				Rule:    "content-root",
				Message: fmt.Sprintf("file %q lies outside the skill content root %q — every content file must live under <name>/ (skill-bundle-format.md §4.4)", f, root),
			})
		}
	}
	skill := md.SkillMarkdownPath()
	if !slices.Contains(md.Files, skill) {
		*errs = append(*errs, ManifestValidationError{
			Kind:    ManifestValidationErrorKindInvalid,
			Field:   "files",
			Rule:    "required-file",
			Message: fmt.Sprintf("must contain %q — the skill content root contains SKILL.md (agentskills.io; skill-bundle-format.md §4.4)", skill),
		})
	}
}

// ── Manifest decode helpers (mirroring internal/registry/parse.go) ──

// manifestFieldValue is the outcome of reading one manifest field.
type manifestFieldValue struct {
	value string
	ok    bool
}

// decodeManifestRoot decodes the manifest document. Unlike a plain
// json.Unmarshal it also rejects trailing data after the JSON object: the
// manifest is exactly one JSON object (pinned format). The decode is
// otherwise structural — format rules are enforced by ParseManifest.
func decodeManifestRoot(data []byte) (map[string]json.RawMessage, []ManifestValidationError) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var root map[string]json.RawMessage
	if err := dec.Decode(&root); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return nil, []ManifestValidationError{{
				Kind:    ManifestValidationErrorKindMalformed,
				Field:   "document",
				Rule:    "type",
				Message: fmt.Sprintf("must be a JSON object, found %s", typeErr.Value),
			}}
		}
		return nil, []ManifestValidationError{{
			Kind:    ManifestValidationErrorKindMalformed,
			Field:   "document",
			Rule:    "json",
			Message: fmt.Sprintf("not decodable JSON: %v", err),
		}}
	}
	if root == nil {
		return nil, []ManifestValidationError{{
			Kind:    ManifestValidationErrorKindMalformed,
			Field:   "document",
			Rule:    "type",
			Message: "must be a JSON object, found null",
		}}
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, []ManifestValidationError{{
			Kind:    ManifestValidationErrorKindMalformed,
			Field:   "document",
			Rule:    "json",
			Message: "trailing data after the manifest document — manifest.json is exactly one JSON object (pinned format)",
		}}
	}
	return root, nil
}

// requiredManifestString reads a required string field, recording a
// "required" error when the field is missing and a malformed "type" error
// when the value is not a string.
func requiredManifestString(obj map[string]json.RawMessage, key, path string, errs *[]ManifestValidationError) manifestFieldValue {
	raw, present := obj[key]
	if !present {
		*errs = append(*errs, ManifestValidationError{
			Kind:    ManifestValidationErrorKindInvalid,
			Field:   path,
			Rule:    "required",
			Message: "required field is missing (skill-bundle-format.md §4)",
		})
		return manifestFieldValue{}
	}
	if isNull(raw) {
		*errs = append(*errs, manifestTypeError(path, "string", nil))
		return manifestFieldValue{}
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		*errs = append(*errs, manifestTypeError(path, "string", err))
		return manifestFieldValue{}
	}
	return manifestFieldValue{value: value, ok: true}
}

// manifestArrayField reads an array-typed field. A missing field yields
// ok == false with a "required" error; a present non-array value yields a
// malformed "type" error and ok == false.
func manifestArrayField(obj map[string]json.RawMessage, key, path string, errs *[]ManifestValidationError) ([]json.RawMessage, bool) {
	raw, present := obj[key]
	if !present {
		*errs = append(*errs, ManifestValidationError{
			Kind:    ManifestValidationErrorKindInvalid,
			Field:   path,
			Rule:    "required",
			Message: "required field is missing (skill-bundle-format.md §4)",
		})
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		*errs = append(*errs, manifestTypeError(path, "array", err))
		return nil, false
	}
	if items == nil {
		*errs = append(*errs, ManifestValidationError{
			Kind:    ManifestValidationErrorKindMalformed,
			Field:   path,
			Rule:    "type",
			Message: "must be an array, found null",
		})
		return nil, false
	}
	return items, true
}

// manifestStrings converts raw array items into strings, skipping items
// that are not strings (they already produced a type error upstream).
func manifestStrings(items []json.RawMessage) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		var value string
		if err := json.Unmarshal(item, &value); err == nil {
			out = append(out, value)
		}
	}
	return out
}

// rejectManifestUnknownFields enforces additionalProperties: false at one
// object level: every key must be declared by the format.
func rejectManifestUnknownFields(obj map[string]json.RawMessage, allowed []string, path string, errs *[]ManifestValidationError) {
	for _, key := range sortedKeys(obj) {
		if !slices.Contains(allowed, key) {
			*errs = append(*errs, ManifestValidationError{
				Kind:    ManifestValidationErrorKindInvalid,
				Field:   joinFieldPath(path, key),
				Rule:    "additionalProperties",
				Message: fmt.Sprintf("unknown field %q — the manifest format declares exactly %s (skill-bundle-format.md §4 additionalProperties: false)", key, strings.Join(allowed, ", ")),
			})
		}
	}
}

// checkManifestPattern records a "pattern" rejection when value does not
// match the required pattern.
func checkManifestPattern(value string, re *regexp.Regexp, path, description string, errs *[]ManifestValidationError) {
	if re.MatchString(value) {
		return
	}
	*errs = append(*errs, ManifestValidationError{
		Kind:    ManifestValidationErrorKindInvalid,
		Field:   path,
		Rule:    "pattern",
		Message: fmt.Sprintf("does not match the required pattern: %s", description),
	})
}

// manifestMaxLengthErrors builds a "maxLength" rejection when value
// exceeds max.
func manifestMaxLengthErrors(path, value string, max int) []ManifestValidationError {
	if len(value) <= max {
		return nil
	}
	return []ManifestValidationError{{
		Kind:    ManifestValidationErrorKindInvalid,
		Field:   path,
		Rule:    "maxLength",
		Message: fmt.Sprintf("is %d bytes, exceeding the %d-byte cap", len(value), max),
	}}
}

// manifestTypeError builds a malformed "type" rejection for a field whose
// value is not of the expected kind. A nil err means the value was the
// JSON null literal.
func manifestTypeError(path, want string, err error) ManifestValidationError {
	if err == nil {
		return ManifestValidationError{
			Kind:    ManifestValidationErrorKindMalformed,
			Field:   path,
			Rule:    "type",
			Message: fmt.Sprintf("must be a %s, found null", want),
		}
	}
	return ManifestValidationError{
		Kind:    ManifestValidationErrorKindMalformed,
		Field:   path,
		Rule:    "type",
		Message: fmt.Sprintf("must be a %s: %v", want, err),
	}
}

// isNull reports whether the raw value is the JSON null literal.
func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// sortedKeys returns the object keys in stable sorted order.
func sortedKeys(obj map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// joinFieldPath joins a parent path and a key with '.'.
func joinFieldPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

// itemPath renders the path of one array item: "files[3]".
func itemPath(base string, i int) string {
	return fmt.Sprintf("%s[%d]", base, i)
}

// plural returns "s" for n != 1, "" otherwise.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// semverMajor returns the major component of a well-formed semver
// major.minor.patch string.
func semverMajor(version string) (int, bool) {
	idx := strings.IndexByte(version, '.')
	if idx <= 0 {
		return 0, false
	}
	major, err := strconv.Atoi(version[:idx])
	if err != nil || major < 0 {
		return 0, false
	}
	return major, true
}

// ValidateName reports whether s is a valid skill/source identifier
// (^[a-z0-9][a-z0-9-]*$, bounded length).
func ValidateName(s string) bool {
	return len(s) <= MaxNameLength && reName.MatchString(s)
}

// ValidateVersion reports whether s is a valid semver without leading
// zeros, bounded length.
func ValidateVersion(s string) bool {
	return len(s) <= 64 && reSemver.MatchString(s)
}
